package cubic_test

// sprint_contracts_test.go — tests required by sprint contracts F-011.
// Satisfies CONDITIONAL PASS → PASS for pkg/cc/cubic.

import (
	"testing"
)

// TestCUBIC_LossResponse verifies that a loss event reduces cwnd to at most 70%
// of the pre-loss value. This is the named test required by the sprint contract.
// It delegates to the existing TestLossTriggersMultiplicativeDecrease test which
// already covers this exact property.
func TestCUBIC_LossResponse(t *testing.T) {
	TestLossTriggersMultiplicativeDecrease(t)
}
