package cc_test

import (
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
)

func TestNewAdapter_DefaultsCubic(t *testing.T) {
	adapter, err := cc.NewAdapter("", 0, 0)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if adapter.Name() != "cubic" {
		t.Errorf("expected cubic default, got %q", adapter.Name())
	}
}

func TestNewAdapter_UnknownFallsToCubic(t *testing.T) {
	adapter, err := cc.NewAdapter("nonexistent-cc", 0, 0)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if adapter.Name() != "cubic" {
		t.Errorf("expected cubic fallback, got %q", adapter.Name())
	}
}

func TestNewAdapter_BBR(t *testing.T) {
	adapter, err := cc.NewAdapter("bbr", 12000, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if adapter.Name() != "bbr" {
		t.Errorf("expected bbr, got %q", adapter.Name())
	}
}

func TestCCAdapter_DelegatesToHyperspaceCC(t *testing.T) {
	adapter, err := cc.NewAdapter("cubic", 12000, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	// Verify initial state.
	cwnd := adapter.CongestionWindow()
	if cwnd <= 0 {
		t.Errorf("expected positive initial cwnd, got %d", cwnd)
	}

	// Should be able to send initially.
	if !adapter.CanSend(0) {
		t.Error("expected CanSend(0) to be true")
	}

	// OnPacketSent should not panic.
	now := time.Now()
	adapter.OnPacketSent(now, 1, 1200, 0)

	// OnPacketAcked should not panic.
	adapter.OnPacketAcked(now.Add(10*time.Millisecond), 1, 1200, 10*time.Millisecond, 0)

	// OnPacketLost should not panic.
	adapter.OnPacketLost(now.Add(20*time.Millisecond), 2, 1200, 0)

	// OnRTTUpdate should not panic.
	adapter.OnRTTUpdate(10*time.Millisecond, 12*time.Millisecond, 2*time.Millisecond)

	// PacingRate should return a value.
	_ = adapter.PacingRate()

	// Reset should not panic.
	adapter.Reset()
}

func TestCCAdapter_Registry(t *testing.T) {
	adapter, err := cc.NewAdapter("cubic", 12000, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	connID := uint64(42)

	// Register.
	cc.RegisterAdapter(connID, adapter)

	// Get.
	got := cc.GetAdapter(connID)
	if got != adapter {
		t.Error("expected to get registered adapter")
	}

	// Unregister.
	cc.UnregisterAdapter(connID)
	got = cc.GetAdapter(connID)
	if got != nil {
		t.Error("expected nil after unregister")
	}
}

func TestCCAdapter_CC_ReturnsUnderlying(t *testing.T) {
	adapter, err := cc.NewAdapter("cubic", 12000, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	underlying := adapter.CC()
	if underlying == nil {
		t.Error("expected non-nil underlying CC")
	}
	if underlying.Name() != "cubic" {
		t.Errorf("expected cubic, got %q", underlying.Name())
	}
}
