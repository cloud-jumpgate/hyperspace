// Package bbr implements BBRv1 congestion control.
package bbr

import (
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
)

const (
	mss     = 1200
	cwndMin = 4 * mss

	startupGain = 2.89
	drainGain   = 1.0 / startupGain

	// probeRTTInterval is how often we enter PROBE_RTT.
	probeRTTInterval = 10 * time.Second
	// probeRTTDuration is how long we stay in PROBE_RTT.
	probeRTTDuration = 200 * time.Millisecond
	// probeRTTCwnd is the window during PROBE_RTT.
	probeRTTCwnd = 4 * mss

	// bwWindowRTTs is the bandwidth estimation window in RTTs.
	bwWindowRTTs = 10
	// rtpropWindow is the RTprop estimation window in seconds.
	rtpropWindowSec = 10.0

	// startup exits after this many rounds without 25% BtlBw improvement.
	startupRoundsWithoutGrowth = 3
	startupGrowthThreshold     = 0.25
)

// probeGains is the 8-round pacing gain cycle for PROBE_BW.
var probeGains = [8]float64{1.25, 0.75, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}

type phase int

const (
	phaseStartup  phase = iota
	phaseDrain
	phaseProbeBW
	phaseProbeRTT
)

func init() {
	cc.Register("bbr", func(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
		return New(initialCwnd, minRTT)
	})
}

// bwSample holds a bandwidth measurement.
type bwSample struct {
	bw float64 // bytes/second
}

// BBRCC implements BBRv1 congestion control.
type BBRCC struct {
	initialCwnd int
	initialRTT  time.Duration

	phase phase

	// Bandwidth estimation: windowed max over last bwWindowRTTs RTTs.
	btlBw     float64 // bytes/second
	bwSamples []bwSample
	bwRound   int

	// RTprop estimation: windowed min over last rtpropWindowSec.
	rtProp       time.Duration
	rtPropStamp  time.Time // when rtProp was last updated

	// Pacing and cwnd gains.
	pacingGain float64
	cwndGain   float64

	// Current cwnd and pacing rate.
	cwnd       int
	pacingRate float64 // bytes/second

	// Round tracking: used for STARTUP exit logic.
	roundCount         int
	startupRoundsSame  int
	lastBtlBw          float64

	// PROBE_BW cycle index.
	probeBWRound int

	// PROBE_RTT bookkeeping.
	lastProbeRTTTime time.Time
	probeRTTEnd      time.Time
	inProbeRTT       bool
	savedPhase       phase

	// sRTT from OnRTTUpdate.
	sRTT time.Duration

	// Delivery tracking (bytes delivered per round for bandwidth estimation).
	delivered     int
	deliveredTime time.Time
	firstSentTime time.Time
	lastAckedTime time.Time

	// For bandwidth sampling: bytes in flight at send time.
	lastRoundDelivered int
	roundDelivered     int

	startTime time.Time
}

// New returns a BBRv1 CongestionControl.
func New(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
	b := &BBRCC{
		initialCwnd: initialCwnd,
		initialRTT:  minRTT,
	}
	b.reset(time.Now())
	return b
}

func (b *BBRCC) reset(now time.Time) {
	b.phase = phaseStartup
	b.btlBw = 0
	b.bwSamples = nil
	b.bwRound = 0
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
	b.deliveredTime = now
	b.startTime = now
	b.lastRoundDelivered = 0
	b.roundDelivered = 0
}

// Name returns the algorithm name.
func (b *BBRCC) Name() string { return "bbr" }

// Reset restores BBR to its initial state.
func (b *BBRCC) Reset() {
	b.reset(time.Now())
}

// OnPacketSent records a sent packet.
func (b *BBRCC) OnPacketSent(t time.Time, _ cc.PacketNumber, _ int, _ int) {
	if b.firstSentTime.IsZero() {
		b.firstSentTime = t
	}
}

// OnPacketAcked handles an acked packet and updates BBR state.
func (b *BBRCC) OnPacketAcked(t time.Time, _ cc.PacketNumber, size int, rttSample time.Duration, inFlight int) {
	b.delivered += size
	b.deliveredTime = t

	// Update RTprop (windowed min over 10 seconds).
	if rttSample > 0 {
		b.updateRTProp(t, rttSample)
	}

	// Update bandwidth sample.
	elapsed := t.Sub(b.firstSentTime).Seconds()
	if elapsed > 0 {
		bwSample := float64(b.delivered) / elapsed
		b.updateBtlBw(bwSample)
	}

	// Advance round counter when we have acked at least one round of data.
	b.roundDelivered += size
	if b.roundDelivered >= b.initialCwnd {
		b.roundCount++
		b.roundDelivered = 0
		b.onNewRound(t, inFlight)
	}

	b.updateCwndAndPacing(inFlight)
	b.checkProbeRTT(t, inFlight)
}

// onNewRound is called at the start of each new RTT round.
func (b *BBRCC) onNewRound(t time.Time, inFlight int) {
	switch b.phase {
	case phaseStartup:
		b.checkStartupExit()
	case phaseDrain:
		bdp := b.bdp()
		if inFlight <= bdp {
			b.enterProbeBW(t)
		}
	case phaseProbeBW:
		b.probeBWRound = (b.probeBWRound + 1) % 8
		b.pacingGain = probeGains[b.probeBWRound]
		b.cwndGain = 2.0
	case phaseProbeRTT:
		// handled by checkProbeRTT
	}
}

// checkStartupExit decides if STARTUP should end.
func (b *BBRCC) checkStartupExit() {
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

// enterProbeBW transitions to PROBE_BW.
func (b *BBRCC) enterProbeBW(_ time.Time) {
	b.phase = phaseProbeBW
	b.probeBWRound = 0
	b.pacingGain = probeGains[0]
	b.cwndGain = 2.0
}

// checkProbeRTT checks if we need to enter PROBE_RTT.
func (b *BBRCC) checkProbeRTT(t time.Time, inFlight int) {
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

// updateCwndAndPacing sets cwnd and pacing rate based on BDP.
func (b *BBRCC) updateCwndAndPacing(_ int) {
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

// bdp returns the estimated bandwidth-delay product in bytes.
func (b *BBRCC) bdp() int {
	rtt := b.rtProp.Seconds()
	if rtt <= 0 {
		rtt = 0.001
	}
	return int(b.btlBw * rtt)
}

// updateRTProp updates the windowed-min RTT estimate.
func (b *BBRCC) updateRTProp(t time.Time, rtt time.Duration) {
	if rtt < b.rtProp || t.Sub(b.rtPropStamp).Seconds() > rtpropWindowSec {
		b.rtProp = rtt
		b.rtPropStamp = t
	}
}

// updateBtlBw updates the windowed-max bandwidth estimate.
func (b *BBRCC) updateBtlBw(sample float64) {
	b.bwSamples = append(b.bwSamples, bwSample{bw: sample})
	// Keep only the last bwWindowRTTs samples (approximated per-ack here).
	if len(b.bwSamples) > bwWindowRTTs*10 {
		b.bwSamples = b.bwSamples[len(b.bwSamples)-bwWindowRTTs*10:]
	}
	// Recompute max.
	max := 0.0
	for _, s := range b.bwSamples {
		if s.bw > max {
			max = s.bw
		}
	}
	b.btlBw = max
}

// OnPacketLost handles packet loss (BBR is not loss-based, but records for bandwidth).
func (b *BBRCC) OnPacketLost(_ time.Time, _ cc.PacketNumber, _ int, _ int) {
	// BBR does not react to individual losses directly; bandwidth estimation
	// implicitly degrades when throughput drops.
}

// OnRTTUpdate updates the smoothed RTT.
func (b *BBRCC) OnRTTUpdate(rttSample, sRTT, _ time.Duration) {
	b.sRTT = sRTT
	if sRTT <= 0 {
		b.sRTT = rttSample
	}
	// Also update RTprop.
	if rttSample > 0 {
		b.updateRTProp(time.Now(), rttSample)
	}
}

// CongestionWindow returns the current congestion window in bytes.
func (b *BBRCC) CongestionWindow() int { return b.cwnd }

// PacingRate returns the target pacing rate in bytes/second.
func (b *BBRCC) PacingRate() int {
	if b.pacingRate <= 0 {
		return 0
	}
	return int(b.pacingRate)
}

// CanSend returns true when inflight is below the congestion window.
func (b *BBRCC) CanSend(inFlight int) bool {
	return inFlight < b.cwnd
}
