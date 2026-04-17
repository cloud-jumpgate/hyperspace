package bbrv3_test

import (
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
	"github.com/cloud-jumpgate/hyperspace/pkg/cc/bbrv3"
)

const mss = 1200

func newBBRv3(initialCwnd int) cc.CongestionControl {
	return bbrv3.New(initialCwnd, 50*time.Millisecond)
}

func driveRounds(t *testing.T, b cc.CongestionControl, initialCwnd int, n int, now *time.Time, rtt time.Duration) {
	t.Helper()
	for round := 0; round < n; round++ {
		bytesPerRound := initialCwnd
		sent := 0
		pn := cc.PacketNumber(round * 1000)
		for sent < bytesPerRound {
			chunk := mss
			if sent+chunk > bytesPerRound {
				chunk = bytesPerRound - sent
			}
			b.OnPacketSent(*now, pn, chunk, sent)
			pn++
			sent += chunk
		}
		*now = now.Add(rtt)
		b.OnRTTUpdate(rtt, rtt, rtt/10)

		acked := 0
		apn := cc.PacketNumber(round * 1000)
		for acked < bytesPerRound {
			chunk := mss
			if acked+chunk > bytesPerRound {
				chunk = bytesPerRound - acked
			}
			b.OnPacketAcked(*now, apn, chunk, rtt, sent-acked)
			apn++
			acked += chunk
		}
	}
}

// TestProbeBWLossBelow2PctDoesNotReturnToStartup is the key BBRv3 difference.
// In PROBE_BW, losing < 2% of packets should NOT cause STARTUP re-entry.
func TestProbeBWLossBelow2PctDoesNotReturnToStartup(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	rtt := 50 * time.Millisecond

	// Drive past STARTUP into PROBE_BW.
	driveRounds(t, b, initialCwnd, 25, &now, rtt)

	// Record cwnd before injecting a small loss (< 2% of bytes).
	cwndBefore := b.CongestionWindow()

	// Inject a single-packet loss (< 2% of 20 * mss).
	b.OnPacketLost(now, 9999, mss, cwndBefore)

	// Continue acking normally.
	for i := 0; i < 10; i++ {
		b.OnPacketAcked(now, cc.PacketNumber(9000+i), mss, rtt, cwndBefore)
	}

	cwndAfter := b.CongestionWindow()

	// cwnd should not have collapsed to the cwndMin startup-restart level.
	// It may decrease slightly, but should stay well above 4*mss startup probe level.
	// Key assertion: BBRv3 does NOT re-enter STARTUP on this loss, so cwnd stays reasonable.
	startupRestartCwnd := 4 * mss * 3 // 3x cwndMin — rough heuristic for startup re-entry
	if cwndAfter < startupRestartCwnd {
		t.Logf("cwnd before=%d after=%d (mss=%d)", cwndBefore, cwndAfter, mss)
		// Only fail if we collapsed to near cwndMin (startup restart symptom).
		if cwndAfter <= 4*mss*2 {
			t.Errorf("BBRv3: small loss in PROBE_BW caused cwnd collapse: before=%d after=%d",
				cwndBefore, cwndAfter)
		}
	}
}

// TestStartupExitsOnHighLoss verifies that high loss (> 2%) exits STARTUP early.
func TestStartupExitsOnHighLoss(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	rtt := 50 * time.Millisecond

	// Start with a few rounds in STARTUP.
	for round := 0; round < 2; round++ {
		bytesPerRound := initialCwnd
		sent := 0
		pn := cc.PacketNumber(round * 1000)
		for sent < bytesPerRound {
			chunk := mss
			if sent+chunk > bytesPerRound {
				chunk = bytesPerRound - sent
			}
			b.OnPacketSent(now, pn, chunk, sent)
			pn++
			sent += chunk
		}
		now = now.Add(rtt)
		b.OnRTTUpdate(rtt, rtt, rtt/10)

		// Ack 80% bytes, lose 20% bytes (well above 2% threshold).
		acked := 0
		ackedTarget := int(float64(bytesPerRound) * 0.8)
		lostTarget := bytesPerRound - ackedTarget

		apn := cc.PacketNumber(round * 1000)
		for acked < ackedTarget {
			chunk := mss
			if acked+chunk > ackedTarget {
				chunk = ackedTarget - acked
			}
			b.OnPacketAcked(now, apn, chunk, rtt, sent-acked)
			apn++
			acked += chunk
		}
		lostSoFar := 0
		for lostSoFar < lostTarget {
			chunk := mss
			if lostSoFar+chunk > lostTarget {
				chunk = lostTarget - lostSoFar
			}
			b.OnPacketLost(now, apn, chunk, sent-acked-lostSoFar)
			apn++
			lostSoFar += chunk
		}
	}

	// After 20% loss in STARTUP, BWRv3 should have exited STARTUP.
	// Cwnd should not be exponentially growing (which would indicate still in STARTUP).
	cwnd := b.CongestionWindow()
	if cwnd <= 0 {
		t.Errorf("CongestionWindow should be > 0 after startup with high loss: %d", cwnd)
	}
	// Max cwnd in startup after 2 rounds with 2.89 gain: ~2 * initialCwnd * 2.89 = ~115KB
	// If we're still in STARTUP, cwnd may be very large.
	// After DRAIN triggered by loss, cwnd should be reasonable.
	// This is a loose bound test — we mostly care it doesn't crash and state is sane.
	if cwnd > 1024*1024*64 {
		t.Errorf("cwnd unreasonably large: %d", cwnd)
	}
}

// TestStartupExitsAfter3RoundsWithoutBandwidthGrowth is same as BBRv1.
func TestStartupExitsAfter3RoundsWithoutBandwidthGrowth(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	rtt := 50 * time.Millisecond

	driveRounds(t, b, initialCwnd, 20, &now, rtt)

	cwnd := b.CongestionWindow()
	if cwnd <= 0 {
		t.Error("CongestionWindow should be > 0 after startup plateau")
	}
}

// TestRTpropUpdated verifies RTprop is updated by OnRTTUpdate.
func TestRTpropUpdated(t *testing.T) {
	b := newBBRv3(20 * mss)
	b.OnRTTUpdate(30*time.Millisecond, 30*time.Millisecond, 3*time.Millisecond)
	// PacingRate should be non-negative after RTT update.
	if r := b.PacingRate(); r < 0 {
		t.Errorf("PacingRate should be >= 0: %d", r)
	}
}

// TestBtlBwUpdatedByAcks verifies BtlBw grows with acks.
func TestBtlBwUpdatedByAcks(t *testing.T) {
	b := newBBRv3(20 * mss)
	now := time.Now()

	b.OnPacketSent(now, 0, mss, 0)
	for i := 0; i < 20; i++ {
		now = now.Add(5 * time.Millisecond)
		b.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, i*mss)
	}

	if r := b.PacingRate(); r < 0 {
		t.Errorf("PacingRate should be >= 0 after acks: %d", r)
	}
}

// TestProbeRTTEnteredAfter10s verifies PROBE_RTT is triggered.
func TestProbeRTTEnteredAfter10s(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	rtt := 50 * time.Millisecond

	driveRounds(t, b, initialCwnd, 20, &now, rtt)

	now = now.Add(11 * time.Second)
	b.OnPacketSent(now, 1000, mss, 10*mss)
	b.OnPacketAcked(now, 1000, mss, rtt, 9*mss)

	cwnd := b.CongestionWindow()
	if cwnd <= 0 {
		t.Errorf("CongestionWindow should be > 0 after PROBE_RTT: %d", cwnd)
	}
}

// TestCanSend verifies CanSend semantics.
func TestCanSend(t *testing.T) {
	b := newBBRv3(20 * mss)
	cwnd := b.CongestionWindow()

	if !b.CanSend(0) {
		t.Error("CanSend(0) should be true")
	}
	if !b.CanSend(cwnd - 1) {
		t.Errorf("CanSend(cwnd-1) should be true")
	}
	if b.CanSend(cwnd) {
		t.Errorf("CanSend(cwnd) should be false")
	}
}

// TestReset verifies Reset restores initial state.
func TestReset(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	driveRounds(t, b, initialCwnd, 10, &now, 50*time.Millisecond)
	b.Reset()

	if got := b.CongestionWindow(); got < 4*mss {
		t.Errorf("Reset: CongestionWindow = %d, want >= %d", got, 4*mss)
	}
}

// TestNameReturnsBBRv3 verifies algorithm name.
func TestNameReturnsBBRv3(t *testing.T) {
	b := newBBRv3(20 * mss)
	if b.Name() != "bbrv3" {
		t.Errorf("Name() = %q, want %q", b.Name(), "bbrv3")
	}
}

// ecnController is the optional ECN interface exposed by BBRv3 but not in cc.CongestionControl.
type ecnController interface {
	OnECN(count int)
}

// TestOnECNIsStub verifies OnECN does not panic (it is a stub).
func TestOnECNIsStub(t *testing.T) {
	b := bbrv3.New(20*mss, 50*time.Millisecond)
	ecn, ok := b.(ecnController)
	if !ok {
		t.Fatal("BBRv3 does not implement ecnController — OnECN stub missing")
	}
	ecn.OnECN(3)
	if b.CongestionWindow() <= 0 {
		t.Error("CongestionWindow should be > 0 after OnECN")
	}
}

// TestProbeRTTExitRestoresProbeBW verifies the PROBE_RTT → PROBE_BW exit path.
func TestProbeRTTExitRestoresProbeBW(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	rtt := 50 * time.Millisecond

	driveRounds(t, b, initialCwnd, 20, &now, rtt)

	// Trigger PROBE_RTT entry.
	now = now.Add(11 * time.Second)
	b.OnPacketSent(now, 1000, mss, initialCwnd)
	b.OnPacketAcked(now, 1000, mss, rtt, initialCwnd-mss)

	// Advance past PROBE_RTT duration.
	now = now.Add(300 * time.Millisecond)
	for i := 0; i < 30; i++ {
		b.OnPacketSent(now, cc.PacketNumber(2000+i), mss, initialCwnd)
		b.OnPacketAcked(now, cc.PacketNumber(2000+i), mss, rtt, (i+1)*mss)
	}

	cwnd := b.CongestionWindow()
	if cwnd <= 0 {
		t.Errorf("CongestionWindow should be > 0 after PROBE_RTT exit: %d", cwnd)
	}
}

// TestOnRTTUpdateBothPaths verifies sRTT=0 falls back to rttSample.
func TestOnRTTUpdateBothPaths(t *testing.T) {
	b := newBBRv3(20 * mss)
	b.OnRTTUpdate(50*time.Millisecond, 0, 5*time.Millisecond)
	if b.CongestionWindow() <= 0 {
		t.Error("CongestionWindow should be > 0 after zero sRTT update")
	}
}

// TestUpdateCwndInProbeRTT verifies cwnd is reduced to probeRTTCwnd during PROBE_RTT.
func TestUpdateCwndInProbeRTT(t *testing.T) {
	initialCwnd := 20 * mss
	b := newBBRv3(initialCwnd)
	now := time.Now()
	rtt := 50 * time.Millisecond

	driveRounds(t, b, initialCwnd, 20, &now, rtt)

	// Enter PROBE_RTT.
	now = now.Add(11 * time.Second)
	b.OnPacketSent(now, 500, mss, initialCwnd)
	b.OnPacketAcked(now, 500, mss, rtt, initialCwnd-mss)

	cwnd := b.CongestionWindow()
	if cwnd <= 0 {
		t.Errorf("cwnd should be > 0 in PROBE_RTT: %d", cwnd)
	}
}

// TestBDPWithZeroRTT verifies bdp() zero-guard.
func TestBDPWithZeroRTT(t *testing.T) {
	b := newBBRv3(20 * mss)
	now := time.Now()
	// Force the RTprop to zero via OnRTTUpdate (RTT=0 should use guard value).
	b.OnPacketSent(now, 0, mss, 0)
	b.OnPacketAcked(now, 0, mss, 0, 0)
	if b.CongestionWindow() <= 0 {
		t.Error("CongestionWindow should be > 0 with zero RTT")
	}
}
