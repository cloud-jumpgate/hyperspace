// Package drl implements a Deep Reinforcement Learning congestion control policy.
// The inference backend is pluggable via the Policy interface.
// The default backend is NullPolicy (returns action 0.0), which is equivalent
// to a no-op multiplicative update (cwnd unchanged).
// Operators may swap in an ONNXPolicy (build tag: onnx) via SetPolicy.
package drl

import (
	"math"
	"sync"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
	"github.com/cloud-jumpgate/hyperspace/pkg/cc/cubic"
)

const (
	mss     = 1200
	cwndMin = 2 * mss
	cwndMax = 64 * 1024 * 1024

	// inferDeadline is the maximum time allowed for a Policy.Infer call.
	inferDeadline = 5 * time.Microsecond

	// lossSpikeThreshold: if loss rate exceeds this in one RTT, apply MD.
	lossSpikeThreshold = 0.10
	// lossSpikeMD is the multiplicative decrease on a loss spike.
	lossSpikeMD = 0.7

	// obsLen is the observation vector length (padded to 10).
	obsLen = 10
)

// Policy is the inference backend.
// Infer takes the observation vector and returns action in [-1, 1].
// Must complete in ≤ 5 µs; caller falls back to CUBIC on deadline miss.
type Policy interface {
	Infer(obs []float32) (float32, error)
	Close() error
}

// NullPolicy always returns 0.0 — no cwnd change. Acts as a safe no-op.
type NullPolicy struct{}

func (NullPolicy) Infer(_ []float32) (float32, error) { return 0, nil }
func (NullPolicy) Close() error                       { return nil }

// globalPolicy is the shared policy instance for the "drl" registered controller.
// Operators may call SetPolicy to swap it at runtime.
var (
	globalPolicyMu sync.RWMutex
	globalPolicy   Policy = NullPolicy{}
)

// SetPolicy replaces the global DRL policy. Thread-safe.
func SetPolicy(p Policy) {
	globalPolicyMu.Lock()
	defer globalPolicyMu.Unlock()
	globalPolicy = p
}

func init() {
	cc.Register("drl", func(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
		globalPolicyMu.RLock()
		p := globalPolicy
		globalPolicyMu.RUnlock()
		return NewWithPolicy(initialCwnd, minRTT, p)
	})
}

// DRLController implements cc.CongestionControl backed by a Policy.
//
//nolint:revive // stutter is intentional: drl.DRLController is the established public API name
type DRLController struct {
	initialCwnd int
	minRTT      time.Duration

	policy Policy

	// Embedded CUBIC for fallback.
	fallback *cubicWrapper

	cwnd int

	// RTT state.
	sRTT     time.Duration
	prevSRTT time.Duration
	rttVar   time.Duration

	// Loss tracking (per RTT).
	ackedThisRTT int
	lostThisRTT  int

	// Throughput tracking.
	delivered     int
	deliveredTime time.Time
	firstSentTime time.Time

	// Previous loss rate for d_loss.
	prevLossRate float64

	// normalisation denominators (set from first measurements)
	baseRTT time.Duration
	baseBW  float64
}

// cubicWrapper wraps cubic.CubicCC to expose it as a cc.CongestionControl
// and access its window for fallback decisions.
type cubicWrapper struct {
	cc.CongestionControl
}

// NewWithPolicy creates a DRLController with the given policy.
func NewWithPolicy(initialCwnd int, minRTT time.Duration, policy Policy) cc.CongestionControl {
	if minRTT <= 0 {
		minRTT = 100 * time.Millisecond
	}
	d := &DRLController{
		initialCwnd:   initialCwnd,
		minRTT:        minRTT,
		policy:        policy,
		cwnd:          initialCwnd,
		sRTT:          minRTT,
		prevSRTT:      minRTT,
		baseRTT:       minRTT,
		deliveredTime: time.Now(),
		firstSentTime: time.Now(),
	}
	if d.cwnd < cwndMin {
		d.cwnd = cwndMin
	}
	d.fallback = &cubicWrapper{cubic.New(initialCwnd, minRTT)}
	return d
}

// Name returns the algorithm name.
func (d *DRLController) Name() string { return "drl" }

// Reset restores DRL to initial state.
func (d *DRLController) Reset() {
	d.cwnd = d.initialCwnd
	if d.cwnd < cwndMin {
		d.cwnd = cwndMin
	}
	d.sRTT = d.minRTT
	d.prevSRTT = d.minRTT
	d.rttVar = 0
	d.ackedThisRTT = 0
	d.lostThisRTT = 0
	d.delivered = 0
	d.prevLossRate = 0
	d.firstSentTime = time.Now()
	d.deliveredTime = time.Now()
	d.fallback.Reset()
}

// OnPacketSent records a sent packet.
func (d *DRLController) OnPacketSent(t time.Time, pn cc.PacketNumber, size int, inFlight int) {
	if d.firstSentTime.IsZero() {
		d.firstSentTime = t
	}
	d.fallback.OnPacketSent(t, pn, size, inFlight)
}

// OnPacketAcked handles an acked packet and runs the policy every RTT.
func (d *DRLController) OnPacketAcked(t time.Time, pn cc.PacketNumber, size int, rttSample time.Duration, inFlight int) {
	d.ackedThisRTT += size
	d.delivered += size
	d.deliveredTime = t
	d.fallback.OnPacketAcked(t, pn, size, rttSample, inFlight)

	// Run policy update every sRTT-worth of acked bytes.
	threshold := d.sRTT
	if threshold <= 0 {
		threshold = d.minRTT
	}
	// Trigger roughly once per RTT based on delivered bytes.
	if d.ackedThisRTT >= d.cwnd/2 || d.ackedThisRTT >= d.initialCwnd {
		d.runPolicy(t, inFlight)
		d.ackedThisRTT = 0
		d.lostThisRTT = 0
	}
	_ = threshold
}

// OnPacketLost handles packet loss.
func (d *DRLController) OnPacketLost(t time.Time, pn cc.PacketNumber, size int, inFlight int) {
	d.lostThisRTT += size
	d.fallback.OnPacketLost(t, pn, size, inFlight)
}

// OnRTTUpdate updates RTT state.
func (d *DRLController) OnRTTUpdate(rttSample, sRTT, rttVar time.Duration) {
	d.prevSRTT = d.sRTT
	d.sRTT = sRTT
	d.rttVar = rttVar
	if d.baseRTT <= 0 || rttSample < d.baseRTT {
		d.baseRTT = rttSample
	}
	d.fallback.OnRTTUpdate(rttSample, sRTT, rttVar)
}

// CongestionWindow returns the current congestion window.
func (d *DRLController) CongestionWindow() int { return d.cwnd }

// PacingRate returns pacing rate as cwnd / sRTT.
func (d *DRLController) PacingRate() int {
	if d.sRTT <= 0 {
		return 0
	}
	return int(float64(d.cwnd) / d.sRTT.Seconds())
}

// CanSend returns true when inflight is below the congestion window.
func (d *DRLController) CanSend(inFlight int) bool {
	return inFlight < d.cwnd
}

// runPolicy builds the observation vector, calls the policy, and updates cwnd.
func (d *DRLController) runPolicy(t time.Time, inFlight int) {
	obs := d.buildObs(t, inFlight)

	// Compute current loss rate for spike detection.
	total := d.ackedThisRTT + d.lostThisRTT
	var lossRate float64
	if total > 0 {
		lossRate = float64(d.lostThisRTT) / float64(total)
	}

	// Safety rail: loss spike → multiplicative decrease, skip policy.
	if lossRate > lossSpikeThreshold {
		newCwnd := int(float64(d.cwnd) * lossSpikeMD)
		if newCwnd < cwndMin {
			newCwnd = cwndMin
		}
		d.cwnd = newCwnd
		d.prevLossRate = lossRate
		return
	}

	// Run policy with deadline.
	action, err := d.inferWithDeadline(obs)
	if err != nil {
		// Fallback to CUBIC window.
		d.cwnd = d.fallback.CongestionWindow()
		if d.cwnd < cwndMin {
			d.cwnd = cwndMin
		}
		if d.cwnd > cwndMax {
			d.cwnd = cwndMax
		}
		d.prevLossRate = lossRate
		return
	}

	// Apply action: cwnd = clamp(cwnd * exp(0.1 * action), cwndMin, cwndMax)
	multiplier := math.Exp(0.1 * float64(action))
	newCwnd := int(float64(d.cwnd) * multiplier)
	if newCwnd < cwndMin {
		newCwnd = cwndMin
	}
	if newCwnd > cwndMax {
		newCwnd = cwndMax
	}
	d.cwnd = newCwnd
	d.prevLossRate = lossRate
}

// buildObs constructs the 10-element observation vector.
// [sRTT_norm, rttVar_norm, loss_rate, throughput_norm, inflight_ratio, d_sRTT, d_loss, 0, 0, 0]
func (d *DRLController) buildObs(_ time.Time, inFlight int) []float32 {
	obs := make([]float32, obsLen)

	baseRTT := d.baseRTT.Seconds()
	if baseRTT <= 0 {
		baseRTT = 0.001
	}
	sRTT := d.sRTT.Seconds()
	if sRTT <= 0 {
		sRTT = baseRTT
	}

	// 0: sRTT_norm = sRTT / baseRTT
	obs[0] = float32(sRTT / baseRTT)

	// 1: rttVar_norm = rttVar / baseRTT
	rttVar := d.rttVar.Seconds()
	obs[1] = float32(rttVar / baseRTT)

	// 2: loss_rate
	total := d.ackedThisRTT + d.lostThisRTT
	if total > 0 {
		obs[2] = float32(d.lostThisRTT) / float32(total)
	}

	// 3: throughput_norm = (delivered / elapsed) / baseBW
	// We approximate using cwnd / sRTT as throughput proxy.
	throughput := float64(d.cwnd) / sRTT
	if d.baseBW <= 0 || throughput > d.baseBW {
		d.baseBW = throughput
	}
	if d.baseBW > 0 {
		obs[3] = float32(throughput / d.baseBW)
	}

	// 4: inflight_ratio = inFlight / cwnd
	if d.cwnd > 0 {
		obs[4] = float32(inFlight) / float32(d.cwnd)
	}

	// 5: d_sRTT = (sRTT - prevSRTT) / baseRTT
	prevSRTT := d.prevSRTT.Seconds()
	obs[5] = float32((sRTT - prevSRTT) / baseRTT)

	// 6: d_loss = lossRate - prevLossRate
	var lossRate float64
	if total > 0 {
		lossRate = float64(d.lostThisRTT) / float64(total)
	}
	obs[6] = float32(lossRate - d.prevLossRate)

	// 7-9: padding zeros (already zero-initialised).
	return obs
}

// inferWithDeadline calls Policy.Infer with a 5 µs deadline.
// Returns ErrDeadline on timeout. The inference goroutine is always allowed to
// complete (buffered channel) to avoid goroutine leaks.
func (d *DRLController) inferWithDeadline(obs []float32) (float32, error) {
	type result struct {
		action float32
		err    error
	}
	// Buffered channel ensures the goroutine can always send without blocking,
	// even if the caller has already returned on timeout.
	ch := make(chan result, 1)
	go func() {
		action, err := d.policy.Infer(obs)
		ch <- result{action, err}
	}()

	timer := time.NewTimer(inferDeadline)
	defer timer.Stop()

	select {
	case r := <-ch:
		return r.action, r.err
	case <-timer.C:
		return 0, errDeadline
	}
}

// errDeadline is returned when policy inference exceeds the deadline.
var errDeadline = deadlineError("inference deadline exceeded")

type deadlineError string

func (e deadlineError) Error() string { return string(e) }
