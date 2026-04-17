// Package bbrv3 implements BBRv3 congestion control.
// BBRv3 improves on BBRv1 with:
//  1. Loss tolerance: loss < 2% in PROBE_BW does NOT re-enter STARTUP.
//  2. Improved startup exit: exits on BtlBw plateau OR when loss > lossThreshold.
//  3. ECN support: stubbed (OnECN method exists, not in cc.CongestionControl interface).
//  4. PROBE_UP/PROBE_DOWN sub-phases: stubbed.
package bbrv3

import (
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
)

const (
	mss     = 1200
	cwndMin = 4 * mss

	startupGain = 2.89
	drainGain   = 1.0 / startupGain

	probeRTTInterval = 10 * time.Second
	probeRTTDuration = 200 * time.Millisecond
	probeRTTCwnd     = 4 * mss

	bwWindowRTTs    = 10
	rtpropWindowSec = 10.0

	startupRoundsWithoutGrowth = 3
	startupGrowthThreshold     = 0.25

	// BBRv3: loss tolerance threshold in PROBE_BW (2%).
	probeBWLossThreshold = 0.02
	// BBRv3: startup loss threshold — exit STARTUP early on high loss.
	startupLossThreshold = 0.02
)

var probeGains = [8]float64{1.25, 0.75, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}

type phase int

const (
	phaseStartup  phase = iota
	phaseDrain
	phaseProbeBW
	phaseProbeRTT
)

func init() {
	cc.Register("bbrv3", func(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
		return New(initialCwnd, minRTT)
	})
}

// bwSample holds a bandwidth measurement.
type bwSample struct {
	bw float64
}

// BBRv3CC implements BBRv3 congestion control.
type BBRv3CC struct {
	initialCwnd int
	initialRTT  time.Duration

	phase phase

	btlBw     float64
	bwSamples []bwSample

	rtProp      time.Duration
	rtPropStamp time.Time

	pacingGain float64
	cwndGain   float64

	cwnd       int
	pacingRate float64

	roundCount         int
	startupRoundsSame  int
	lastBtlBw          float64

	probeBWRound int

	lastProbeRTTTime time.Time
	probeRTTEnd      time.Time
	inProbeRTT       bool
	savedPhase       phase

	sRTT time.Duration

	delivered     int
	firstSentTime time.Time

	roundDelivered int

	startTime time.Time

	// Loss tracking per round (BBRv3).
	lostThisRound int
	ackedThisRound int
}

// New returns a BBRv3 CongestionControl.
func New(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
	b := &BBRv3CC{
		initialCwnd: initialCwnd,
		initialRTT:  minRTT,
	}
	b.reset(time.Now())
	return b
}

func (b *BBRv3CC) reset(now time.Time) {
	b.phase = phaseStartup
	b.btlBw = 0
	b.bwSamples = nil
	b.rtProp = b.initialRTT
	if b.rtProp <= 0 {
		b.rtProp = 100 * time.Millisecond
	}
	b.rtPropStamp = now
	b.pacingGain = startupGain
	b.cwndGain = startupGain
	b.cwnd = b.initialCwnd
	if b.cwnd < cwndMin {
		b.cwnd = cwndMin
	}
	b.pacingRate = 0
	b.roundCount = 0
	b.startupRoundsSame = 0
	b.lastBtlBw = 0
	b.probeBWRound = 0
	b.lastProbeRTTTime = now
	b.inProbeRTT = false
	b.sRTT = b.initialRTT
	if b.sRTT <= 0 {
		b.sRTT = 100 * time.Millisecond
	}
	b.delivered = 0
	b.startTime = now
	b.roundDelivered = 0
	b.lostThisRound = 0
	b.ackedThisRound = 0
}

// Name returns the algorithm name.
func (b *BBRv3CC) Name() string { return "bbrv3" }

// Reset restores BBRv3 to its initial state.
func (b *BBRv3CC) Reset() {
	b.reset(time.Now())
}

// OnPacketSent records a sent packet.
func (b *BBRv3CC) OnPacketSent(t time.Time, _ cc.PacketNumber, _ int, _ int) {
	if b.firstSentTime.IsZero() {
		b.firstSentTime = t
	}
}

// OnPacketAcked handles an acked packet.
func (b *BBRv3CC) OnPacketAcked(t time.Time, _ cc.PacketNumber, size int, rttSample time.Duration, inFlight int) {
	b.delivered += size
	b.ackedThisRound += size

	if rttSample > 0 {
		b.updateRTProp(t, rttSample)
	}

	elapsed := t.Sub(b.firstSentTime).Seconds()
	if elapsed > 0 {
		sample := float64(b.delivered) / elapsed
		b.updateBtlBw(sample)
	}

	b.roundDelivered += size
	if b.roundDelivered >= b.initialCwnd {
		b.roundCount++
		b.onNewRound(t, inFlight)
		b.roundDelivered = 0
		b.lostThisRound = 0
		b.ackedThisRound = 0
	}

	b.updateCwndAndPacing(inFlight)
	b.checkProbeRTT(t, inFlight)
}

// onNewRound is called at round boundaries.
func (b *BBRv3CC) onNewRound(t time.Time, inFlight int) {
	switch b.phase {
	case phaseStartup:
		b.checkStartupExit()
	case phaseDrain:
		bdp := b.bdp()
		if inFlight <= bdp {
			b.enterProbeBW(t)
		}
	case phaseProbeBW:
		// BBRv3: do NOT re-enter STARTUP on loss < 2%.
		// Loss is handled in OnPacketLost; we just cycle phases here.
		b.probeBWRound = (b.probeBWRound + 1) % 8
		b.pacingGain = probeGains[b.probeBWRound]
		b.cwndGain = 2.0
	case phaseProbeRTT:
		// handled by checkProbeRTT
	}
}

// checkStartupExit determines if STARTUP should end.
// BBRv3: exits when BtlBw plateaus for 3 rounds OR loss > lossThreshold.
func (b *BBRv3CC) checkStartupExit() {
	// Loss-based exit (BBRv3).
	if b.ackedThisRound > 0 && b.lostThisRound > 0 {
		lossRate := float64(b.lostThisRound) / float64(b.lostThisRound+b.ackedThisRound)
		if lossRate > startupLossThreshold {
			b.phase = phaseDrain
			b.pacingGain = drainGain
			b.cwndGain = startupGain
			return
		}
	}

	// Bandwidth plateau exit.
	if b.btlBw <= 0 {
		return
	}
	if b.lastBtlBw > 0 {
		growth := (b.btlBw - b.lastBtlBw) / b.lastBtlBw
		if growth < startupGrowthThreshold {
			b.startupRoundsSame++
		} else {
			b.startupRoundsSame = 0
		}
	}
	b.lastBtlBw = b.btlBw

	if b.startupRoundsSame >= startupRoundsWithoutGrowth {
		b.phase = phaseDrain
		b.pacingGain = drainGain
		b.cwndGain = startupGain
	}
}

func (b *BBRv3CC) enterProbeBW(_ time.Time) {
	b.phase = phaseProbeBW
	b.probeBWRound = 0
	b.pacingGain = probeGains[0]
	b.cwndGain = 2.0
}

func (b *BBRv3CC) checkProbeRTT(t time.Time, inFlight int) {
	if b.inProbeRTT {
		if t.After(b.probeRTTEnd) {
			b.inProbeRTT = false
			b.lastProbeRTTTime = t
			b.phase = b.savedPhase
			if b.phase == phaseProbeBW {
				b.pacingGain = probeGains[b.probeBWRound]
			} else {
				b.pacingGain = startupGain
			}
			b.cwndGain = 2.0
		}
		_ = inFlight
		return
	}

	if b.phase != phaseStartup && t.Sub(b.lastProbeRTTTime) >= probeRTTInterval {
		b.savedPhase = b.phase
		b.inProbeRTT = true
		b.probeRTTEnd = t.Add(probeRTTDuration)
		b.pacingGain = 1.0
		b.cwndGain = 1.0
		b.phase = phaseProbeRTT
	}
}

func (b *BBRv3CC) updateCwndAndPacing(_ int) {
	if b.inProbeRTT {
		b.cwnd = probeRTTCwnd
		if b.cwnd < cwndMin {
			b.cwnd = cwndMin
		}
		b.pacingRate = b.btlBw * b.pacingGain
		return
	}

	bdp := b.bdp()
	newCwnd := int(b.cwndGain * float64(bdp))
	if newCwnd < cwndMin {
		newCwnd = cwndMin
	}
	b.cwnd = newCwnd
	b.pacingRate = b.btlBw * b.pacingGain
}

func (b *BBRv3CC) bdp() int {
	rtt := b.rtProp.Seconds()
	if rtt <= 0 {
		rtt = 0.001
	}
	return int(b.btlBw * rtt)
}

func (b *BBRv3CC) updateRTProp(t time.Time, rtt time.Duration) {
	if rtt < b.rtProp || t.Sub(b.rtPropStamp).Seconds() > rtpropWindowSec {
		b.rtProp = rtt
		b.rtPropStamp = t
	}
}

func (b *BBRv3CC) updateBtlBw(sample float64) {
	b.bwSamples = append(b.bwSamples, bwSample{bw: sample})
	if len(b.bwSamples) > bwWindowRTTs*10 {
		b.bwSamples = b.bwSamples[len(b.bwSamples)-bwWindowRTTs*10:]
	}
	max := 0.0
	for _, s := range b.bwSamples {
		if s.bw > max {
			max = s.bw
		}
	}
	b.btlBw = max
}

// OnPacketLost handles packet loss.
// BBRv3: in PROBE_BW, loss < 2% per round does NOT trigger STARTUP re-entry.
func (b *BBRv3CC) OnPacketLost(_ time.Time, _ cc.PacketNumber, size int, _ int) {
	b.lostThisRound += size
}

// OnRTTUpdate updates the smoothed RTT.
func (b *BBRv3CC) OnRTTUpdate(rttSample, sRTT, _ time.Duration) {
	b.sRTT = sRTT
	if sRTT <= 0 {
		b.sRTT = rttSample
	}
	if rttSample > 0 {
		b.updateRTProp(time.Now(), rttSample)
	}
}

// CongestionWindow returns the current congestion window in bytes.
func (b *BBRv3CC) CongestionWindow() int { return b.cwnd }

// PacingRate returns the target pacing rate in bytes/second.
func (b *BBRv3CC) PacingRate() int {
	if b.pacingRate <= 0 {
		return 0
	}
	return int(b.pacingRate)
}

// CanSend returns true when inflight is below the congestion window.
func (b *BBRv3CC) CanSend(inFlight int) bool {
	return inFlight < b.cwnd
}

// OnECN is a BBRv3-specific stub for ECN signals (not in cc.CongestionControl interface).
func (b *BBRv3CC) OnECN(_ int) {
	// Stub: ECN signal handling not yet implemented.
}
