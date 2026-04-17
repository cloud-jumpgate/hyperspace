package arbitrator_test

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func conns(cs ...*mockConn) []quictr.Connection {
	out := make([]quictr.Connection, len(cs))
	for i, c := range cs {
		out[i] = c
	}
	return out
}

// --- LowestRTT ---------------------------------------------------------------

func TestLowestRTT_Name(t *testing.T) {
	a := arbitrator.NewLowestRTT()
	assert.Equal(t, "lowest-rtt", a.Name())
}

func TestLowestRTT_EmptyPool(t *testing.T) {
	a := arbitrator.NewLowestRTT()
	_, err := a.Pick(nil, 0, 0)
	assert.ErrorIs(t, err, arbitrator.ErrNoConnections)

	_, err = a.Pick([]quictr.Connection{}, 0, 0)
	assert.ErrorIs(t, err, arbitrator.ErrNoConnections)
}

func TestLowestRTT_SingleConnection(t *testing.T) {
	a := arbitrator.NewLowestRTT()
	c := newMockConn(1, ms(10), 0, 100_000, 0)
	got, err := a.Pick(conns(c), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}

func TestLowestRTT_PicksLowest(t *testing.T) {
	a := arbitrator.NewLowestRTT()
	c1 := newMockConn(1, ms(50), 0, 100_000, 0)
	c2 := newMockConn(2, ms(10), 0, 100_000, 0) // lowest RTT
	c3 := newMockConn(3, ms(30), 0, 100_000, 0)

	got, err := a.Pick(conns(c1, c2, c3), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.ID(), "should pick connection with lowest RTT")
}

func TestLowestRTT_TieBreakByID(t *testing.T) {
	a := arbitrator.NewLowestRTT()
	// Both have same RTT; lower ID wins.
	c1 := newMockConn(3, ms(10), 0, 100_000, 0)
	c2 := newMockConn(1, ms(10), 0, 100_000, 0)

	got, err := a.Pick(conns(c1, c2), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.ID(), "tie should be broken by lowest ID")
}

// --- LeastOutstanding --------------------------------------------------------

func TestLeastOutstanding_Name(t *testing.T) {
	a := arbitrator.NewLeastOutstanding()
	assert.Equal(t, "least-outstanding", a.Name())
}

func TestLeastOutstanding_EmptyPool(t *testing.T) {
	a := arbitrator.NewLeastOutstanding()
	_, err := a.Pick(nil, 0, 0)
	assert.ErrorIs(t, err, arbitrator.ErrNoConnections)
}

func TestLeastOutstanding_SingleConnection(t *testing.T) {
	a := arbitrator.NewLeastOutstanding()
	c := newMockConn(1, ms(10), 0, 100_000, 5000)
	got, err := a.Pick(conns(c), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}

func TestLeastOutstanding_PicksMostFreeCwnd(t *testing.T) {
	a := arbitrator.NewLeastOutstanding()
	// c1: 80% utilisation (80k/100k)
	c1 := newMockConn(1, ms(10), 0, 100_000, 80_000)
	// c2: 10% utilisation (10k/100k) — should win
	c2 := newMockConn(2, ms(10), 0, 100_000, 10_000)
	// c3: 50% utilisation (50k/100k)
	c3 := newMockConn(3, ms(10), 0, 100_000, 50_000)

	got, err := a.Pick(conns(c1, c2, c3), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.ID(), "should pick connection with most free cwnd")
}

func TestLeastOutstanding_ZeroCwndLosesAlways(t *testing.T) {
	a := arbitrator.NewLeastOutstanding()
	// c1: zero cwnd (bad)
	c1 := newMockConn(1, ms(10), 0, 0, 0)
	// c2: has cwnd
	c2 := newMockConn(2, ms(10), 0, 100_000, 50_000)

	got, err := a.Pick(conns(c1, c2), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.ID(), "zero-cwnd connection should lose")
}

// --- Hybrid ------------------------------------------------------------------

func TestHybrid_Name(t *testing.T) {
	a := arbitrator.NewHybrid()
	assert.Equal(t, "hybrid", a.Name())
}

func TestHybrid_EmptyPool(t *testing.T) {
	a := arbitrator.NewHybrid()
	_, err := a.Pick(nil, 0, 0)
	assert.ErrorIs(t, err, arbitrator.ErrNoConnections)
}

func TestHybrid_SingleConnection(t *testing.T) {
	a := arbitrator.NewHybrid()
	c := newMockConn(1, ms(10), 0, 100_000, 5000)
	got, err := a.Pick(conns(c), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}

func TestHybrid_ScoringPicksBest(t *testing.T) {
	// c1: high RTT, high loss, low free cwnd — bad score
	c1 := newMockConn(1, ms(100), 0.5, 100_000, 90_000)
	// c2: low RTT, low loss, high free cwnd — best score
	c2 := newMockConn(2, ms(5), 0.01, 100_000, 5_000)
	// c3: medium
	c3 := newMockConn(3, ms(30), 0.1, 100_000, 40_000)

	a := arbitrator.NewHybrid()
	got, err := a.Pick(conns(c1, c2, c3), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.ID(), "hybrid should pick the clearly best connection")
}

func TestHybrid_CustomWeights(t *testing.T) {
	// With Alpha=1, Beta=0, Gamma=0: pure RTT scoring.
	w := arbitrator.HybridWeights{Alpha: 1.0, Beta: 0.0, Gamma: 0.0}
	a := arbitrator.NewHybridWeighted(w)

	c1 := newMockConn(1, ms(100), 0, 100_000, 0)
	c2 := newMockConn(2, ms(1), 0, 100_000, 0) // should win with pure RTT
	c3 := newMockConn(3, ms(50), 0, 100_000, 0)

	got, err := a.Pick(conns(c1, c2, c3), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.ID(), "pure RTT weighting should pick lowest RTT")
}

func TestHybrid_ZeroRTT(t *testing.T) {
	// Connection with zero RTT should not panic.
	a := arbitrator.NewHybrid()
	c1 := newMockConn(1, 0, 0, 100_000, 0) // RTT=0
	c2 := newMockConn(2, ms(10), 0, 100_000, 0)

	got, err := a.Pick(conns(c1, c2), 0, 0)
	require.NoError(t, err)
	// c2 should win since c1 has zero RTT (score contribution = 0)
	assert.Equal(t, uint64(2), got.ID())
}

// --- Sticky ------------------------------------------------------------------

func TestSticky_Name(t *testing.T) {
	a := arbitrator.NewSticky(0.5)
	assert.Equal(t, "sticky", a.Name())
}

func TestSticky_EmptyPool(t *testing.T) {
	a := arbitrator.NewSticky(0.5)
	_, err := a.Pick(nil, 1, 0)
	assert.ErrorIs(t, err, arbitrator.ErrNoConnections)
}

func TestSticky_SingleConnection(t *testing.T) {
	a := arbitrator.NewSticky(0.5)
	c := newMockConn(1, ms(10), 0, 100_000, 0)
	got, err := a.Pick(conns(c), 42, 0)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}

func TestSticky_PinsPublication(t *testing.T) {
	a := arbitrator.NewSticky(0.5)
	c1 := newMockConn(1, ms(5), 0, 100_000, 0)  // best
	c2 := newMockConn(2, ms(20), 0, 100_000, 0)

	// First pick pins to c1.
	got, err := a.Pick(conns(c1, c2), 100, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.ID())

	// Second pick returns same connection.
	got, err = a.Pick(conns(c1, c2), 100, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.ID())

	// Different publication can pin to a different connection.
	// Force different order so c2 looks best for pub 200 (can't — c1 is still best).
	got, err = a.Pick(conns(c1, c2), 200, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.ID()) // still c1, it's best
}

func TestSticky_RePinsOnDegradation(t *testing.T) {
	a := arbitrator.NewSticky(0.5) // 50% threshold

	c1 := newMockConn(1, ms(10), 0, 100_000, 0) // best initially
	c2 := newMockConn(2, ms(15), 0, 100_000, 0)

	// Pin publication 999 to c1.
	got, err := a.Pick(conns(c1, c2), 999, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.ID())

	// Now c1 degrades significantly: RTT goes from 10ms to 25ms.
	// Pool best is c2 at 15ms. Degradation = (25-15)/15 = 66% > 50% threshold.
	c1.rtt = ms(25)

	got, err = a.Pick(conns(c1, c2), 999, 0)
	require.NoError(t, err)
	// Should re-pin to c2.
	assert.Equal(t, uint64(2), got.ID(), "should re-pin to best connection on degradation")
}

func TestSticky_DoesNotRePinBelowThreshold(t *testing.T) {
	a := arbitrator.NewSticky(0.5) // 50% threshold

	c1 := newMockConn(1, ms(10), 0, 100_000, 0)
	c2 := newMockConn(2, ms(15), 0, 100_000, 0)

	// Pin to c1.
	_, err := a.Pick(conns(c1, c2), 777, 0)
	require.NoError(t, err)

	// c1 degrades slightly: 10ms → 13ms. Best (c2) is 15ms.
	// Degradation = (13-13)/... actually c1 is still < c2 so it stays best.
	// Let's set c2 as best: c2.rtt = 10ms, c1.rtt = 14ms (40% degradation < 50%).
	c2.rtt = ms(10)
	c1.rtt = ms(14) // 40% worse than best (10ms) — below threshold

	got, err := a.Pick(conns(c1, c2), 777, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), got.ID(), "should not re-pin when degradation is below threshold")
}

// --- Random ------------------------------------------------------------------

func TestRandom_Name(t *testing.T) {
	a := arbitrator.NewRandom(nil)
	assert.Equal(t, "random", a.Name())
}

func TestRandom_EmptyPool(t *testing.T) {
	a := arbitrator.NewRandom(nil)
	_, err := a.Pick(nil, 0, 0)
	assert.ErrorIs(t, err, arbitrator.ErrNoConnections)
}

func TestRandom_SingleConnection(t *testing.T) {
	a := arbitrator.NewRandom(nil)
	c := newMockConn(1, ms(10), 0, 100_000, 0)
	got, err := a.Pick(conns(c), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}

func TestRandom_StatisticalUniformity(t *testing.T) {
	const n = 10_000
	const numConns = 4

	// Deterministic seed for reproducibility.
	src := rand.NewSource(42)
	a := arbitrator.NewRandom(src)

	cs := make([]*mockConn, numConns)
	for i := range cs {
		cs[i] = newMockConn(uint64(i+1), ms(10), 0, 100_000, 0)
	}
	cList := conns(cs...)

	counts := make(map[uint64]int, numConns)
	for i := 0; i < n; i++ {
		got, err := a.Pick(cList, 0, 0)
		require.NoError(t, err)
		counts[got.ID()]++
	}

	// Chi-squared test for uniformity.
	// Expected count per bucket: n / numConns.
	expected := float64(n) / float64(numConns)
	chiSq := 0.0
	for i := uint64(1); i <= numConns; i++ {
		diff := float64(counts[i]) - expected
		chiSq += (diff * diff) / expected
	}

	// For numConns-1=3 degrees of freedom, critical value at p=0.01 is ~11.345.
	// We use a generous threshold of 20 to avoid flakiness.
	const criticalValue = 20.0
	assert.Less(t, chiSq, criticalValue,
		"chi-squared statistic %.2f exceeds critical value %.2f (counts: %v)",
		chiSq, criticalValue, counts)
}

// --- All strategies: common edge cases ---------------------------------------

func TestAllStrategies_EmptyPool(t *testing.T) {
	strategies := []arbitrator.Arbitrator{
		arbitrator.NewLowestRTT(),
		arbitrator.NewLeastOutstanding(),
		arbitrator.NewHybrid(),
		arbitrator.NewSticky(0.5),
		arbitrator.NewRandom(nil),
	}
	for _, a := range strategies {
		t.Run(a.Name(), func(t *testing.T) {
			_, err := a.Pick([]quictr.Connection{}, 0, 0)
			assert.ErrorIs(t, err, arbitrator.ErrNoConnections, "should return ErrNoConnections for empty pool")
		})
	}
}

func TestAllStrategies_SingleConnection(t *testing.T) {
	c := newMockConn(7, ms(10), 0.01, 100_000, 5000)
	strategies := []arbitrator.Arbitrator{
		arbitrator.NewLowestRTT(),
		arbitrator.NewLeastOutstanding(),
		arbitrator.NewHybrid(),
		arbitrator.NewSticky(0.5),
		arbitrator.NewRandom(nil),
	}
	for _, a := range strategies {
		t.Run(a.Name(), func(t *testing.T) {
			got, err := a.Pick(conns(c), 1, 100)
			require.NoError(t, err)
			assert.Equal(t, c.ID(), got.ID(), "single connection must always be picked")
		})
	}
}

// TestHybrid_DefaultWeightsSumToOne verifies that default weights are sane.
func TestHybrid_DefaultWeightsSumToOne(t *testing.T) {
	w := arbitrator.DefaultHybridWeights
	sum := w.Alpha + w.Beta + w.Gamma
	// Should be 1.0 ± floating point epsilon.
	assert.InDelta(t, 1.0, sum, 1e-9, "default weights should sum to 1.0")
}

// TestHybrid_NoPanic_ExtremeValues verifies no panic with extreme stats.
func TestHybrid_NoPanic_ExtremeValues(t *testing.T) {
	a := arbitrator.NewHybrid()
	// All zeros: should not panic.
	c := newMockConn(1, 0, 0, 0, 0)
	_, err := a.Pick(conns(c), 0, 0)
	require.NoError(t, err)

	// Max loss.
	c2 := newMockConn(2, ms(1), 1.0, math.MaxInt32, math.MaxInt32)
	_, err = a.Pick(conns(c2), 0, 0)
	require.NoError(t, err)
}
