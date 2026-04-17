// Package cubic implements the CUBIC congestion control algorithm.
package cubic

import (
	"math"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
)

const (
	// MSS is the assumed maximum segment size in bytes.
	MSS = 1200
	// cwndMin is the minimum congestion window (2 * MSS).
	cwndMin = 2 * MSS
	// cwndMax is the practical ceiling (64 MiB).
	cwndMax = 64 * 1024 * 1024
	// cubicC is the CUBIC scaling constant.
	cubicC = 0.4
	// cubicBeta is the multiplicative decrease factor.
	cubicBeta = 0.7
)

func init() {
	cc.Register("cubic", func(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
		return New(initialCwnd, minRTT)
	})
}

// CubicCC implements CUBIC congestion control.
type CubicCC struct {
	initialCwnd int
	minRTT      time.Duration

	cwnd     int // current congestion window in bytes
	ssthresh int // slow start threshold in bytes

	// CUBIC state
	wMax    float64   // window size just before the last reduction (bytes)
	tEpoch  time.Time // start of the current congestion avoidance epoch
	k       float64   // time to reach wMax (seconds)
	inSlowStart bool

	// RTT state (updated via OnRTTUpdate)
	sRTT   time.Duration
	rttVar time.Duration
}

// New returns a CUBIC CongestionControl.
// initialCwnd: starting cwnd in bytes.
// minRTT: initial minimum RTT estimate.
func New(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
	c := &CubicCC{
		initialCwnd: initialCwnd,
		minRTT:      minRTT,
		cwnd:        initialCwnd,
		ssthresh:    cwndMax, // start with high ssthresh so we begin in slow start
		wMax:        float64(initialCwnd),
		inSlowStart: true,
		sRTT:        minRTT,
	}
	if c.cwnd < cwndMin {
		c.cwnd = cwndMin
	}
	return c
}

// Name returns the algorithm name.
func (c *CubicCC) Name() string { return "cubic" }

// Reset restores the controller to its initial state.
func (c *CubicCC) Reset() {
	c.cwnd = c.initialCwnd
	if c.cwnd < cwndMin {
		c.cwnd = cwndMin
	}
	c.ssthresh = cwndMax
	c.wMax = float64(c.initialCwnd)
	c.tEpoch = time.Time{}
	c.k = 0
	c.inSlowStart = true
	c.sRTT = c.minRTT
	c.rttVar = 0
}

// OnPacketSent is called when a packet is sent.
func (c *CubicCC) OnPacketSent(_ time.Time, _ cc.PacketNumber, _ int, _ int) {}

// OnPacketAcked handles an acked packet. Implements slow start and CUBIC CA.
func (c *CubicCC) OnPacketAcked(t time.Time, _ cc.PacketNumber, size int, _ time.Duration, _ int) {
	if c.cwnd < c.ssthresh {
		// Slow start: increase cwnd by bytes acked.
		c.cwnd += size
		if c.cwnd > c.ssthresh {
			c.cwnd = c.ssthresh
		}
		if c.cwnd < cwndMin {
			c.cwnd = cwndMin
		}
		// Reset epoch when leaving slow start.
		if c.cwnd >= c.ssthresh {
			c.inSlowStart = false
			c.tEpoch = t
			c.k = c.computeK()
		}
		return
	}

	c.inSlowStart = false

	// Congestion avoidance: CUBIC update.
	if c.tEpoch.IsZero() {
		c.tEpoch = t
		c.k = c.computeK()
	}

	elapsed := t.Sub(c.tEpoch).Seconds()
	wCubic := c.cubicW(elapsed)

	// TCP-friendly target: W_tcp ≈ cwnd + MSS²/cwnd per ACK.
	// This approximates the Reno-equivalent increase rate.
	wTCPFriendly := float64(c.cwnd) + float64(MSS)*float64(MSS)/float64(c.cwnd)

	var target float64
	if wCubic < wTCPFriendly {
		target = wTCPFriendly
	} else {
		target = wCubic
	}

	// Clamp target.
	if target > cwndMax {
		target = cwndMax
	}

	// Per-ack increment toward target.
	if target > float64(c.cwnd) {
		increment := int(math.Ceil((target - float64(c.cwnd)) * float64(size) / float64(c.cwnd)))
		if increment < 1 {
			increment = 1
		}
		c.cwnd += increment
	}

	if c.cwnd > cwndMax {
		c.cwnd = cwndMax
	}
}

// OnPacketLost handles a lost packet. Applies CUBIC multiplicative decrease.
func (c *CubicCC) OnPacketLost(t time.Time, _ cc.PacketNumber, _ int, _ int) {
	// Record W_max before reduction.
	c.wMax = float64(c.cwnd)

	// Reduce cwnd.
	newCwnd := int(float64(c.cwnd) * cubicBeta)
	if newCwnd < cwndMin {
		newCwnd = cwndMin
	}
	c.cwnd = newCwnd
	c.ssthresh = newCwnd

	// Start new epoch.
	c.tEpoch = t
	c.k = c.computeK()
	c.inSlowStart = false
}

// OnRTTUpdate updates the smoothed RTT and variance.
func (c *CubicCC) OnRTTUpdate(_, sRTT, rttVar time.Duration) {
	c.sRTT = sRTT
	c.rttVar = rttVar
}

// CongestionWindow returns the current congestion window in bytes.
func (c *CubicCC) CongestionWindow() int { return c.cwnd }

// PacingRate returns bytes per second; computed as cwnd / sRTT.
func (c *CubicCC) PacingRate() int {
	if c.sRTT <= 0 {
		return 0
	}
	return int(float64(c.cwnd) / c.sRTT.Seconds())
}

// CanSend returns true when inflight is below the congestion window.
func (c *CubicCC) CanSend(inFlight int) bool {
	return inFlight < c.cwnd
}

// computeK computes K = cbrt(W_max * (1 - beta) / C).
// If wMax is 0 or negative, cbrt returns 0 or a negative value which is handled by the caller.
func (c *CubicCC) computeK() float64 {
	return math.Cbrt(c.wMax * (1 - cubicBeta) / cubicC)
}

// cubicW computes the CUBIC window target at time t seconds from epoch.
// W_cubic(t) = C * (t - K)^3 + W_max
func (c *CubicCC) cubicW(t float64) float64 {
	dt := t - c.k
	return cubicC*dt*dt*dt + c.wMax
}
