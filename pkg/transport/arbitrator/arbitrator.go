// Package arbitrator provides connection-selection strategies for the Hyperspace transport pool.
package arbitrator

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// ErrNoConnections is returned when the pool is empty.
var ErrNoConnections = errors.New("arbitrator: no connections available")

// Arbitrator selects a connection for a send operation.
type Arbitrator interface {
	// Pick selects the best connection from candidates.
	// publicationID: the publication this send belongs to (for Sticky).
	// bytes: number of bytes to send (for LeastOutstanding).
	Pick(candidates []quictr.Connection, publicationID int64, bytes int) (quictr.Connection, error)

	// Name returns the arbitrator strategy name.
	Name() string
}

// --- LowestRTT ---------------------------------------------------------------

// lowestRTT picks the connection with the lowest smoothed RTT.
// Ties are broken by connection ID (deterministic, ascending).
type lowestRTT struct{}

// NewLowestRTT creates an arbitrator that picks the connection with the lowest RTT.
func NewLowestRTT() Arbitrator { return &lowestRTT{} }

func (*lowestRTT) Name() string { return "lowest-rtt" }

func (*lowestRTT) Pick(candidates []quictr.Connection, _ int64, _ int) (quictr.Connection, error) {
	if len(candidates) == 0 {
		return nil, ErrNoConnections
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		cr := c.RTT()
		br := best.RTT()
		if cr < br || (cr == br && c.ID() < best.ID()) {
			best = c
		}
	}
	return best, nil
}

// --- LeastOutstanding --------------------------------------------------------

// leastOutstanding picks the connection with the most free congestion window
// relative to bytes in flight (i.e., fewest bytes-in-flight / cwnd).
type leastOutstanding struct{}

// NewLeastOutstanding creates an arbitrator that picks the connection with the
// fewest bytes-in-flight relative to its congestion window.
func NewLeastOutstanding() Arbitrator { return &leastOutstanding{} }

func (*leastOutstanding) Name() string { return "least-outstanding" }

// utilisation returns bytes-in-flight / cwnd. Lower is better.
func utilisation(s quictr.ConnectionStats) float64 {
	if s.CongestionWindow <= 0 {
		return math.MaxFloat64
	}
	return float64(s.BytesInFlight) / float64(s.CongestionWindow)
}

func (*leastOutstanding) Pick(candidates []quictr.Connection, _ int64, _ int) (quictr.Connection, error) {
	if len(candidates) == 0 {
		return nil, ErrNoConnections
	}
	best := candidates[0]
	bestUtil := utilisation(best.Stats())
	for _, c := range candidates[1:] {
		u := utilisation(c.Stats())
		if u < bestUtil || (u == bestUtil && c.ID() < best.ID()) {
			best = c
			bestUtil = u
		}
	}
	return best, nil
}

// --- Hybrid ------------------------------------------------------------------

// HybridWeights controls the Hybrid arbitrator scoring formula.
// Score = Alpha*(1/rtt_ns) + Beta*(free_cwnd/cwnd) + Gamma*(1/(1+loss))
// Higher score = better connection.
type HybridWeights struct {
	Alpha float64 // weight for RTT component (default 0.5)
	Beta  float64 // weight for cwnd utilisation (default 0.3)
	Gamma float64 // weight for loss component (default 0.2)
}

// DefaultHybridWeights are the default weights.
var DefaultHybridWeights = HybridWeights{Alpha: 0.5, Beta: 0.3, Gamma: 0.2}

type hybrid struct {
	weights HybridWeights
}

// NewHybrid creates the default Hybrid arbitrator with DefaultHybridWeights.
func NewHybrid() Arbitrator { return &hybrid{weights: DefaultHybridWeights} }

// NewHybridWeighted creates a Hybrid arbitrator with custom weights.
func NewHybridWeighted(w HybridWeights) Arbitrator { return &hybrid{weights: w} }

func (*hybrid) Name() string { return "hybrid" }

// hybridScore computes the score for a connection. Higher is better.
func hybridScore(c quictr.Connection, w HybridWeights) float64 {
	stats := c.Stats()
	rttNs := float64(c.RTT().Nanoseconds())

	// RTT component: 1/rtt_ns. Guard against zero RTT.
	var rttScore float64
	if rttNs > 0 {
		rttScore = 1.0 / rttNs
	}

	// cwnd utilisation component: free_cwnd / cwnd. Guard against zero cwnd.
	var cwndScore float64
	if stats.CongestionWindow > 0 {
		freeCwnd := float64(stats.CongestionWindow - stats.BytesInFlight)
		if freeCwnd < 0 {
			freeCwnd = 0
		}
		cwndScore = freeCwnd / float64(stats.CongestionWindow)
	}

	// Loss component: 1 / (1 + loss).
	lossScore := 1.0 / (1.0 + stats.Loss)

	return w.Alpha*rttScore + w.Beta*cwndScore + w.Gamma*lossScore
}

func (h *hybrid) Pick(candidates []quictr.Connection, _ int64, _ int) (quictr.Connection, error) {
	if len(candidates) == 0 {
		return nil, ErrNoConnections
	}
	best := candidates[0]
	bestScore := hybridScore(best, h.weights)
	for _, c := range candidates[1:] {
		s := hybridScore(c, h.weights)
		if s > bestScore || (s == bestScore && c.ID() < best.ID()) {
			best = c
			bestScore = s
		}
	}
	return best, nil
}

// --- Sticky ------------------------------------------------------------------

// sticky pins each publicationID to one connection unless RTT degrades past threshold.
type sticky struct {
	mu               sync.Mutex
	pins             map[int64]uint64 // publicationID → connection ID
	degradeThreshold float64          // e.g. 0.5 = 50% worse than pool best
}

// NewSticky creates an arbitrator that pins each publicationID to one connection
// unless that connection's RTT degrades more than degradeThreshold (e.g. 0.5 = 50%
// worse than pool best). On degrade, re-pins to the current best.
func NewSticky(degradeThreshold float64) Arbitrator {
	return &sticky{
		pins:             make(map[int64]uint64),
		degradeThreshold: degradeThreshold,
	}
}

func (*sticky) Name() string { return "sticky" }

// bestByRTT finds the connection with the lowest RTT and returns (conn, bestRTT).
func bestByRTT(candidates []quictr.Connection) (quictr.Connection, time.Duration) {
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.RTT() < best.RTT() {
			best = c
		}
	}
	return best, best.RTT()
}

func (s *sticky) Pick(candidates []quictr.Connection, publicationID int64, _ int) (quictr.Connection, error) {
	if len(candidates) == 0 {
		return nil, ErrNoConnections
	}

	bestConn, bestRTT := bestByRTT(candidates)

	s.mu.Lock()
	defer s.mu.Unlock()

	pinnedID, hasPinned := s.pins[publicationID]
	if hasPinned {
		// Find the pinned connection.
		for _, c := range candidates {
			if c.ID() == pinnedID {
				// Check if the pinned connection's RTT has degraded.
				pinnedRTT := c.RTT()
				if bestRTT > 0 {
					degradation := float64(pinnedRTT-bestRTT) / float64(bestRTT)
					if degradation <= s.degradeThreshold {
						return c, nil
					}
				} else {
					// bestRTT == 0: no meaningful measurement, keep pinned.
					return c, nil
				}
				// Degraded — fall through to re-pin.
				break
			}
		}
	}

	// Pin to the best connection.
	s.pins[publicationID] = bestConn.ID()
	return bestConn, nil
}

// --- Random ------------------------------------------------------------------

// randomArbitrator picks uniformly at random from the candidates.
type randomArbitrator struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// NewRandom creates an arbitrator that picks uniformly at random.
// src: random source (pass nil to use crypto/rand-seeded default).
func NewRandom(src rand.Source) Arbitrator {
	if src == nil {
		// Use a time-based seed as a reasonable default for non-security use.
		src = rand.NewSource(time.Now().UnixNano())
	}
	return &randomArbitrator{
		rng: rand.New(src), //nolint:gosec // used for load balancing, not security
	}
}

func (*randomArbitrator) Name() string { return "random" }

func (r *randomArbitrator) Pick(candidates []quictr.Connection, _ int64, _ int) (quictr.Connection, error) {
	if len(candidates) == 0 {
		return nil, ErrNoConnections
	}
	r.mu.Lock()
	idx := r.rng.Intn(len(candidates))
	r.mu.Unlock()
	return candidates[idx], nil
}
