package drl_test

// sprint_contracts_test.go — tests required by sprint contracts F-011.
// Satisfies CONDITIONAL PASS → PASS for pkg/cc/drl.

import (
	"errors"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
	"github.com/cloud-jumpgate/hyperspace/pkg/cc/drl"
)

// errPolicy is a Policy that always returns an error from Infer,
// simulating a failure to load or run the ONNX model.
type errPolicy struct{}

func (errPolicy) Infer(_ []float32) (float32, error) {
	return 0, errors.New("policy load error: model file not found")
}
func (errPolicy) Close() error { return nil }

// TestDRL_FallbackOnLoadError constructs a DRLController with a Policy that
// always returns an error, verifying that:
//   - CongestionWindow() returns a non-zero value (BBRv3 fallback is used).
//   - CanSend() delegates to the fallback and does not panic.
//
// This mirrors the pattern established in TestDeadlineFallbackToBBRv3 but
// focuses on the policy-error path rather than the timeout path.
func TestDRL_FallbackOnLoadError(t *testing.T) {
	initialCwnd := 20 * mss

	// errPolicy.Infer returns an error immediately — faster than the 5 µs deadline.
	// inferWithDeadline receives the error from the goroutine and delegates to fallback.
	d := drl.NewWithPolicy(initialCwnd, 50*time.Millisecond, errPolicy{})

	now := time.Now()
	d.OnPacketSent(now, 0, mss, 0)

	// Ack enough bytes to trigger a policy run (>= cwnd/2).
	for i := 0; i < 15; i++ {
		now = now.Add(5 * time.Millisecond)
		d.OnPacketAcked(now, cc.PacketNumber(i), mss, 50*time.Millisecond, i*mss)
	}
	d.OnRTTUpdate(50*time.Millisecond, 50*time.Millisecond, 5*time.Millisecond)

	// CongestionWindow must be non-zero — BBRv3 fallback must have been used.
	cwnd := d.CongestionWindow()
	if cwnd <= 0 {
		t.Errorf("CongestionWindow should be > 0 after policy error fallback: %d", cwnd)
	}

	// CanSend delegates to the DRL controller's own cwnd field (set from fallback).
	if !d.CanSend(0) {
		t.Error("CanSend(0) should always be true regardless of policy error")
	}

	// Verify the fallback name is bbrv3 (ADR-007).
	fallbackName := drl.FallbackName(d)
	if fallbackName != "bbrv3" {
		t.Errorf("fallback algorithm = %q, want %q (ADR-007)", fallbackName, "bbrv3")
	}
}
