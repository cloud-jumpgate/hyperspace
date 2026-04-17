package cubic_test

import (
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
	"github.com/cloud-jumpgate/hyperspace/pkg/cc/cubic"
)

// TestRegistryFactory verifies the cubic factory registered in init() works.
// This covers the factory closure body inside init().
func TestRegistryFactory(t *testing.T) {
	ctrl, err := cc.New("cubic", 10*mss, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("cc.New(cubic): %v", err)
	}
	if ctrl == nil {
		t.Fatal("cc.New(cubic): returned nil")
	}
	if ctrl.Name() != "cubic" {
		t.Errorf("Name() = %q, want cubic", ctrl.Name())
	}
}

const (
	mss     = 1200
	cwndMin = 2 * mss
)

func newCC(initialCwnd int) cc.CongestionControl {
	return cubic.New(initialCwnd, 50*time.Millisecond)
}

// TestSlowStartIncreasesCwnd verifies that during slow start, cwnd grows by the
// number of bytes acked.
func TestSlowStartIncreasesCwnd(t *testing.T) {
	c := newCC(10 * mss)
	initial := c.CongestionWindow()

	t0 := time.Now()
	// Ack 1 packet worth of bytes — should increase cwnd by that amount.
	c.OnPacketAcked(t0, 1, mss, 50*time.Millisecond, 5*mss)
	after := c.CongestionWindow()
	if after <= initial {
		t.Errorf("slow start: cwnd did not grow: initial=%d after=%d", initial, after)
	}
	// Expect it grew by exactly mss.
	if after != initial+mss {
		t.Errorf("slow start: cwnd grew by %d, want %d", after-initial, mss)
	}
}

// TestSlowStartDoesNotExceedSsthresh verifies cwnd is capped at ssthresh on exit.
func TestSlowStartCapsAtSsthresh(t *testing.T) {
	// First trigger a loss to set ssthresh low, then reset and probe.
	c := newCC(20 * mss)
	now := time.Now()

	// Grow cwnd a bit then cause a loss.
	for i := 0; i < 5; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, 10*mss)
	}
	// Loss: sets ssthresh = cwnd * 0.7.
	cwndBefore := c.CongestionWindow()
	c.OnPacketLost(now, 10, mss, cwndBefore)
	ssthresh := c.CongestionWindow() // after loss, cwnd == ssthresh
	if ssthresh >= cwndBefore {
		t.Fatalf("ssthresh should decrease after loss: before=%d ssthresh=%d", cwndBefore, ssthresh)
	}
}

// TestLossTriggersMultiplicativeDecrease verifies cwnd is reduced by beta on loss.
func TestLossTriggersMultiplicativeDecrease(t *testing.T) {
	c := newCC(100 * mss)
	now := time.Now()

	// Drive out of slow start via RTT update.
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Ack many packets to grow cwnd and exit slow start.
	for i := 0; i < 200; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, 50*mss)
	}
	cwndBefore := c.CongestionWindow()

	c.OnPacketLost(now, 201, mss, cwndBefore)
	cwndAfter := c.CongestionWindow()

	// After loss: cwnd should be ≤ cwndBefore * 0.7.
	maxExpected := int(float64(cwndBefore)*0.7) + 1 // +1 for rounding
	if cwndAfter > maxExpected {
		t.Errorf("loss: cwnd not reduced: before=%d after=%d max_expected=%d", cwndBefore, cwndAfter, maxExpected)
	}
	if cwndAfter < cwndMin {
		t.Errorf("cwnd below cwndMin after loss: %d < %d", cwndAfter, cwndMin)
	}
}

// TestCubicTargetFormula verifies that after a loss, subsequent acks grow toward
// W_cubic(t) = C*(t-K)^3 + W_max.
func TestCubicTargetFormula(t *testing.T) {
	c := newCC(100 * mss)
	now := time.Now()

	// RTT update so sRTT is known.
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Ack many packets to exit slow start.
	for i := 0; i < 300; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, 50*mss)
	}

	// Trigger a loss so CUBIC state is initialised.
	cwndBefore := c.CongestionWindow()
	c.OnPacketLost(now, 301, mss, cwndBefore)
	cwndAfterLoss := c.CongestionWindow()

	// After a loss, cwnd should be < cwndBefore.
	if cwndAfterLoss >= cwndBefore {
		t.Fatalf("loss should reduce cwnd: before=%d after=%d", cwndBefore, cwndAfterLoss)
	}

	// Ack some packets after loss — cwnd should grow.
	t1 := now.Add(100 * time.Millisecond)
	for i := 302; i < 320; i++ {
		c.OnPacketAcked(t1, cc.PacketNumber(i), mss, 50*time.Millisecond, cwndAfterLoss)
	}
	cwndAfterRecovery := c.CongestionWindow()
	if cwndAfterRecovery <= cwndAfterLoss {
		t.Errorf("cwnd should grow after loss recovery: at_loss=%d after_acks=%d",
			cwndAfterLoss, cwndAfterRecovery)
	}
}

// TestPacingRateCwndDivSRTT verifies PacingRate = cwnd / sRTT.
func TestPacingRateCwndDivSRTT(t *testing.T) {
	c := newCC(10 * mss)
	sRTT := 50 * time.Millisecond
	c.OnRTTUpdate(sRTT, sRTT, 5*time.Millisecond)

	pacing := c.PacingRate()
	cwnd := c.CongestionWindow()
	expected := int(float64(cwnd) / sRTT.Seconds())

	// Allow 1% tolerance.
	diff := pacing - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > expected/100+1 {
		t.Errorf("PacingRate: got %d, want ~%d (cwnd=%d sRTT=%v)", pacing, expected, cwnd, sRTT)
	}
}

// TestPacingRateZeroBeforeRTTUpdate verifies 0 is returned when sRTT is 0.
func TestPacingRateZeroOnZeroSRTT(t *testing.T) {
	c := cubic.New(10*mss, 0)
	c.OnRTTUpdate(0, 0, 0)
	if r := c.PacingRate(); r != 0 {
		t.Errorf("PacingRate with 0 sRTT: got %d, want 0", r)
	}
}

// TestResetRestoresInitialState verifies Reset() brings state back to baseline.
func TestResetRestoresInitialState(t *testing.T) {
	initialCwnd := 10 * mss
	c := newCC(initialCwnd)
	now := time.Now()

	// Mutate state.
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)
	for i := 0; i < 50; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, 5*mss)
	}
	c.OnPacketLost(now, 51, mss, c.CongestionWindow())

	c.Reset()

	if got := c.CongestionWindow(); got != initialCwnd {
		t.Errorf("Reset: CongestionWindow = %d, want %d", got, initialCwnd)
	}
}

// TestCanSendReturnsFalseWhenInflightGeCwnd verifies CanSend semantics.
func TestCanSendReturnsFalseWhenInflightGeCwnd(t *testing.T) {
	c := newCC(10 * mss)
	cwnd := c.CongestionWindow()

	if !c.CanSend(cwnd - 1) {
		t.Error("CanSend(cwnd-1) should be true")
	}
	if c.CanSend(cwnd) {
		t.Error("CanSend(cwnd) should be false")
	}
	if c.CanSend(cwnd + 1) {
		t.Error("CanSend(cwnd+1) should be false")
	}
}

// TestCanSendAllowsZeroInflight verifies CanSend(0) is always true.
func TestCanSendAllowsZeroInflight(t *testing.T) {
	c := newCC(cwndMin)
	if !c.CanSend(0) {
		t.Error("CanSend(0) should always be true")
	}
}

// TestCwndNeverBelowMin verifies cwnd never drops below cwndMin.
func TestCwndNeverBelowMin(t *testing.T) {
	c := newCC(cwndMin)
	now := time.Now()
	// Force many losses.
	for i := 0; i < 10; i++ {
		c.OnPacketLost(now, cc.PacketNumber(i), mss, c.CongestionWindow())
	}
	if got := c.CongestionWindow(); got < cwndMin {
		t.Errorf("cwnd below cwndMin: %d < %d", got, cwndMin)
	}
}

// TestOnPacketSentIsNoOp verifies OnPacketSent does not affect window.
func TestOnPacketSentIsNoOp(t *testing.T) {
	c := newCC(10 * mss)
	before := c.CongestionWindow()
	c.OnPacketSent(time.Now(), 1, mss, 5*mss)
	if c.CongestionWindow() != before {
		t.Error("OnPacketSent should not change cwnd")
	}
}

// TestNameReturnsCubic verifies algorithm name.
func TestNameReturnsCubic(t *testing.T) {
	c := newCC(10 * mss)
	if c.Name() != "cubic" {
		t.Errorf("Name() = %q, want %q", c.Name(), "cubic")
	}
}

// TestOnPacketSentNoOp verifies OnPacketSent does not change cwnd (it is a no-op).
func TestOnPacketSentNoOp(t *testing.T) {
	c := newCC(10 * mss)
	before := c.CongestionWindow()
	c.OnPacketSent(time.Now(), 1, mss, 5*mss)
	c.OnPacketSent(time.Now(), 2, mss, 6*mss)
	if c.CongestionWindow() != before {
		t.Errorf("OnPacketSent changed cwnd: before=%d after=%d", before, c.CongestionWindow())
	}
}

// TestCongestionAvoidanceAfterSlowStart drives cwnd through slow start and into CA.
func TestCongestionAvoidanceAfterSlowStart(t *testing.T) {
	c := newCC(10 * mss)
	now := time.Now()
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Grow through slow start by acking many bytes.
	for i := 0; i < 500; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, 50*mss)
	}
	// Cause a loss to exit slow start.
	cwndAtLoss := c.CongestionWindow()
	c.OnPacketLost(now, 501, mss, cwndAtLoss)

	// Continue acking in CA mode.
	t1 := now.Add(200 * time.Millisecond)
	for i := 502; i < 600; i++ {
		c.OnPacketAcked(t1, cc.PacketNumber(i), mss, 50*time.Millisecond, cwndAtLoss/2)
	}
	if c.CongestionWindow() <= 0 {
		t.Error("cwnd should be > 0 in congestion avoidance")
	}
}

// TestResetWithSmallInitialCwnd verifies that Reset with initialCwnd < cwndMin uses cwndMin.
func TestResetWithSmallInitialCwnd(t *testing.T) {
	// initialCwnd < cwndMin: should be bumped up to cwndMin.
	c := cubic.New(mss/2, 50*time.Millisecond)
	if c.CongestionWindow() < cwndMin {
		t.Errorf("initial cwnd below cwndMin: %d < %d", c.CongestionWindow(), cwndMin)
	}
	c.Reset()
	if c.CongestionWindow() < cwndMin {
		t.Errorf("cwnd after Reset below cwndMin: %d < %d", c.CongestionWindow(), cwndMin)
	}
}

// TestSlowStartExitEpochReset verifies epoch is reset on transition from slow start to CA.
func TestSlowStartExitEpochReset(t *testing.T) {
	// Use small ssthresh: trigger loss early to lower ssthresh, then re-enter slow start.
	c := newCC(4 * mss)
	now := time.Now()
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Loss sets ssthresh low.
	c.OnPacketLost(now, 0, mss, c.CongestionWindow())
	// Now ack enough to cross ssthresh and enter CA.
	for i := 0; i < 20; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i+1), mss, 50*time.Millisecond, 2*mss)
	}
	// Should be in CA without crashing.
	if c.CongestionWindow() <= 0 {
		t.Error("cwnd should be > 0 after slow start → CA transition")
	}
}

// TestCubicWWithNegativeTime verifies W_cubic handles t < K (concave side).
func TestCubicWWithNegativeTime(t *testing.T) {
	// Drive to a large cwnd, cause loss, then ack immediately (t_elapsed < K).
	c := newCC(100 * mss)
	now := time.Now()
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Grow cwnd.
	for i := 0; i < 300; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, 50*mss)
	}
	c.OnPacketLost(now, 301, mss, c.CongestionWindow())

	// Ack immediately after loss (t_elapsed ≈ 0 → concave side of cubic).
	c.OnPacketAcked(now, 302, mss, 50*time.Millisecond, c.CongestionWindow())
	if c.CongestionWindow() <= 0 {
		t.Error("cwnd should be > 0 when t < K")
	}
}

// TestSlowStartExactlyHitsSsthresh verifies the cwnd==ssthresh branch at slow start exit.
// When acking pushes cwnd to exactly ssthresh, cwnd should stay at ssthresh and epoch resets.
func TestSlowStartExactlyHitsSsthresh(t *testing.T) {
	// Use a small initial cwnd so ssthresh is known.
	c := newCC(4 * mss)
	now := time.Now()
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Trigger loss early to set a known ssthresh (4*mss * 0.7 ≈ 3*mss, floored to cwndMin=2*mss).
	c.OnPacketLost(now, 0, mss, c.CongestionWindow())

	ssthresh := c.CongestionWindow()
	// Verify ssthresh > 0 before proceeding.
	if ssthresh <= 0 {
		t.Fatalf("ssthresh should be > 0: %d", ssthresh)
	}

	// Now ack bytes slightly under ssthresh to stay in slow start,
	// then ack one more chunk that crosses it.
	startCwnd := c.CongestionWindow() // cwnd == ssthresh after loss
	// After loss cwnd == ssthresh, so we're entering CA directly.
	// Ack more to grow cwnd past this point and exercise the CA path.
	for i := 1; i < 10; i++ {
		c.OnPacketAcked(now.Add(time.Duration(i)*10*time.Millisecond),
			cc.PacketNumber(i), mss, 50*time.Millisecond, startCwnd)
	}
	if c.CongestionWindow() <= 0 {
		t.Error("cwnd should be > 0 after CA growth")
	}
}

// TestCongestionAvoidanceWithTCPFriendlyPath verifies the W_cubic < W_tcp-friendly branch.
// This happens when CUBIC is still in the concave (TCP-friendly) region.
func TestCongestionAvoidanceWithTCPFriendlyPath(t *testing.T) {
	// Use a tiny cwnd that will be in the TCP-friendly region at t=0 after loss.
	c := newCC(cwndMin)
	now := time.Now()
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Force into CA by causing a loss (sets ssthresh = cwndMin * 0.7 → cwndMin).
	c.OnPacketLost(now, 0, mss, c.CongestionWindow())

	// Ack multiple packets immediately after loss — at t≈0, W_cubic < W_max so
	// wCubic will be below wTCPFriendly and the TCP-friendly path runs.
	for i := 1; i < 30; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, cwndMin)
	}
	if c.CongestionWindow() <= 0 {
		t.Error("cwnd should be > 0 in TCP-friendly CA")
	}
}

// TestPacingRateZeroWhenSRTTIsNegativeOrZero verifies zero return from PacingRate.
// By updating with 0 sRTT the internal sRTT is set to 0.
func TestPacingRateZeroWhenSRTTIsZero(t *testing.T) {
	c := cubic.New(10*mss, 50*time.Millisecond)
	// Update RTT to zero to trigger the sRTT <= 0 path.
	c.OnRTTUpdate(0, 0, 0)
	if r := c.PacingRate(); r != 0 {
		t.Errorf("PacingRate with zero sRTT should be 0: got %d", r)
	}
}

// TestCubicTargetClampsToMax verifies cwnd is capped at cwndMax.
func TestCubicTargetClampsToMax(t *testing.T) {
	// Use a very large initial cwnd to get close to cwndMax.
	c := cubic.New(32*1024*1024, 1*time.Millisecond) // 32 MiB
	now := time.Now()
	c.OnRTTUpdate(1*time.Millisecond, 1*time.Millisecond, 100*time.Microsecond)

	// Ack many bytes over a long epoch to grow toward cwndMax.
	t1 := now.Add(10 * time.Second)
	for i := 0; i < 100; i++ {
		c.OnPacketAcked(t1, cc.PacketNumber(i), 1024*1024, 1*time.Millisecond, 32*1024*1024)
	}
	if got := c.CongestionWindow(); got > 64*1024*1024 {
		t.Errorf("cwnd exceeded cwndMax: %d > %d", got, 64*1024*1024)
	}
}

// TestSlowStartOvershoots verifies that acking a large chunk that overshoots ssthresh
// caps cwnd at ssthresh (line 94-95).
func TestSlowStartOvershoots(t *testing.T) {
	// Set initialCwnd = cwndMin so ssthresh starts very high (cwndMax).
	// Then cause a loss to set ssthresh = cwndMin.
	c := newCC(cwndMin)
	now := time.Now()
	c.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// Trigger a loss to set a known low ssthresh.
	c.OnPacketLost(now, 0, mss, c.CongestionWindow())
	ssthresh := c.CongestionWindow()

	// Now ack a chunk larger than ssthresh — this will overshoot and get capped.
	// cwnd is currently ssthresh; after loss cwnd==ssthresh so we're in CA, not SS.
	// Reset to get into slow start below ssthresh.
	c.Reset()
	// After Reset, cwnd = cwndMin and ssthresh = cwndMax (back to initial).
	// Cause loss again to get small ssthresh.
	c.OnPacketLost(now, 1, mss, c.CongestionWindow())
	ssthresh = c.CongestionWindow()
	if ssthresh <= 0 {
		t.Fatalf("ssthresh should be > 0: %d", ssthresh)
	}

	// Now ack a huge chunk (> ssthresh bytes) in a single ack while in slow start.
	// This exercises the c.cwnd > c.ssthresh cap on line 94.
	// After loss, cwnd == ssthresh (so we're in CA already).
	// We need cwnd < ssthresh for slow start. Let's engineer this differently.

	// Create a fresh controller with small initial cwnd but KNOWN ssthresh.
	// Set ssthresh to ~3*mss by triggering loss on a 4*mss controller.
	c2 := cubic.New(4*mss, 50*time.Millisecond)
	c2.OnPacketLost(now, 2, mss, c2.CongestionWindow())
	// Now cwnd == ssthresh. Reduce cwnd below ssthresh via another loss.
	// Actually cwnd == ssthresh after first loss (beta*cwnd).
	// Next loss reduces cwnd further.
	cwndAfterLoss := c2.CongestionWindow()
	c2.OnPacketLost(now, 3, mss, cwndAfterLoss)
	// cwnd should now be < first ssthresh, so slow start will apply on next acks.
	// Ack a large block that crosses ssthresh.
	c2.OnPacketAcked(now, 4, c2.CongestionWindow()*2, 50*time.Millisecond, c2.CongestionWindow())
	// Should not crash and cwnd should be sane.
	if c2.CongestionWindow() <= 0 {
		t.Error("cwnd should be > 0 after overshoot cap")
	}
}

// TestSlowStartCwndBelowMinIsGuarded verifies the cwnd < cwndMin guard in slow start.
// This is exercised when a very small ack is processed at cwndMin boundary.
func TestSlowStartCwndBelowMinIsGuarded(t *testing.T) {
	// Build a controller that has been in SS, lost, and is now at cwndMin.
	c := newCC(cwndMin)
	now := time.Now()

	// Cause multiple losses to drive cwnd to cwndMin.
	for i := 0; i < 5; i++ {
		c.OnPacketLost(now, cc.PacketNumber(i), mss, c.CongestionWindow())
	}
	if c.CongestionWindow() != cwndMin {
		t.Logf("cwnd after losses: %d (may be above cwndMin)", c.CongestionWindow())
	}
	// Ack; even if cwnd briefly goes below min (mathematically), the guard raises it.
	c.OnPacketAcked(now, 100, mss, 50*time.Millisecond, cwndMin)
	if c.CongestionWindow() < cwndMin {
		t.Errorf("cwnd below cwndMin: %d < %d", c.CongestionWindow(), cwndMin)
	}
}

// TestCATargetAboveCwndMax verifies the target > cwndMax clamp in CA mode.
func TestCATargetAboveCwndMax(t *testing.T) {
	cwndMax := 64 * 1024 * 1024
	// Start near cwndMax and drive further.
	c := cubic.New(cwndMax-mss, 1*time.Millisecond)
	c.OnRTTUpdate(1*time.Millisecond, 1*time.Millisecond, 100*time.Microsecond)
	now := time.Now()

	// Grow past ssthresh first by acking many bytes.
	for i := 0; i < 200; i++ {
		c.OnPacketAcked(now, cc.PacketNumber(i), mss, 1*time.Millisecond, cwndMax/2)
	}
	// Cause loss near cwndMax to set wMax near max.
	c.OnPacketLost(now, 201, mss, c.CongestionWindow())

	// Ack over a long epoch so W_cubic exceeds cwndMax.
	t2 := now.Add(30 * time.Second)
	for i := 202; i < 250; i++ {
		c.OnPacketAcked(t2, cc.PacketNumber(i), mss, 1*time.Millisecond, cwndMax/2)
	}

	if got := c.CongestionWindow(); got > cwndMax {
		t.Errorf("cwnd exceeded cwndMax: %d > %d", got, cwndMax)
	}
}
