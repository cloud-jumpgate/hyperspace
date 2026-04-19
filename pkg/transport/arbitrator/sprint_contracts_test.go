package arbitrator_test

// sprint_contracts_test.go — tests required by sprint contracts F-005.
// Satisfies CONDITIONAL PASS → PASS for pkg/transport/arbitrator.

import (
	"testing"

	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArbitrator_Sticky_FallsBack verifies the removed-from-pool scenario:
// a publicationID is pinned to conn A, then Pick is called with a candidates
// slice that does NOT include conn A. The arbitrator must fall back to selecting
// from the available candidates (no panic, no ErrNoConnections), and the pin
// must be updated to the new connection.
func TestArbitrator_Sticky_FallsBack(t *testing.T) {
	a := arbitrator.NewSticky(0.5)

	connA := newMockConn(1, ms(5), 0, 100_000, 0) // will be pinned
	connB := newMockConn(2, ms(10), 0, 100_000, 0)
	connC := newMockConn(3, ms(15), 0, 100_000, 0)

	const pubID = int64(42)

	// First Pick: pins pubID to connA (it has the lowest RTT).
	first, err := a.Pick(conns(connA, connB, connC), pubID, 0)
	require.NoError(t, err, "first Pick should succeed")
	assert.Equal(t, connA.ID(), first.ID(), "first Pick should select lowest-RTT conn (A)")
	assert.Equal(t, 1, a.PinCount(), "pin should be registered")

	// Now call Pick with a candidates slice that does NOT include connA.
	// This simulates connA being removed from the pool.
	fallbackCandidates := []quictr.Connection{connB, connC}
	second, err := a.Pick(fallbackCandidates, pubID, 0)
	require.NoError(t, err, "Pick without pinned conn should not return error")
	require.NotNil(t, second, "Pick should return a non-nil connection on fallback")

	// The arbitrator must NOT return connA (it is not in the candidate list).
	assert.NotEqual(t, connA.ID(), second.ID(),
		"fallback should not return the removed connA")

	// The pin should be updated to the new connection (connB or connC).
	assert.Equal(t, 1, a.PinCount(), "pin count should remain 1 after re-pin")

	// Subsequent Pick with same candidates must return the same connection (pinned).
	third, err := a.Pick(fallbackCandidates, pubID, 0)
	require.NoError(t, err, "subsequent Pick should succeed")
	assert.Equal(t, second.ID(), third.ID(),
		"subsequent Pick should return the same re-pinned connection")
}

// BenchmarkArbitrator_LowestRTT benchmarks the LowestRTT Pick with 10 connections.
// The benchmark exists for profiling purposes; no ns/op target is enforced.
func BenchmarkArbitrator_LowestRTT(b *testing.B) {
	a := arbitrator.NewLowestRTT()

	// Build 10 mock connections with varying RTT values.
	cs := make([]quictr.Connection, 10)
	for i := range cs {
		rtt := ms((i + 1) * 5) // 5ms, 10ms, 15ms, ..., 50ms
		cs[i] = newMockConn(uint64(i+1), rtt, 0, 100_000, 0)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = a.Pick(cs, 0, 0)
	}
}
