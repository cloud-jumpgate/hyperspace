// Package poolmgr implements the Hyperspace pool lifecycle agent.
// It owns connection lifecycle: opening and closing connections based on the
// Adaptive Pool Learner's decisions.
package poolmgr

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver/pathmgr"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// ErrNoConnections is returned/logged when the pool has drained to zero connections.
var ErrNoConnections = errors.New("poolmgr: pool has zero connections")

// LearnerDecision is the output of the Adaptive Pool Learner.
type LearnerDecision int

const (
	LearnerDecisionHold   LearnerDecision = iota // keep current pool size
	LearnerDecisionAdd                            // open one more connection
	LearnerDecisionRemove                         // close one connection
)

// String returns a human-readable decision name.
func (d LearnerDecision) String() string {
	switch d {
	case LearnerDecisionHold:
		return "hold"
	case LearnerDecisionAdd:
		return "add"
	case LearnerDecisionRemove:
		return "remove"
	default:
		return "unknown"
	}
}

// AdaptivePoolLearner evaluates pool performance and recommends size changes.
type AdaptivePoolLearner struct {
	poolMin int
	poolMax int
	// cost function weights: C(N) = alpha*P99_lat + beta*CPU_cost
	Alpha float64
	Beta  float64
}

// NewLearner creates a Learner with defaults (min=2, max=8, alpha=0.7, beta=0.3).
func NewLearner(min, max int) *AdaptivePoolLearner {
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return &AdaptivePoolLearner{
		poolMin: min,
		poolMax: max,
		Alpha:   0.7,
		Beta:    0.3,
	}
}

// Evaluate examines the pool snapshot and returns a decision.
// Policy (from §8.4 of the design doc):
//   - If spread is high (best RTT < 0.5 × worst RTT) AND pool size > min: Remove
//   - If all connections have bytes_in_flight > 0.8 × cwnd AND pool size < max: Add
//   - If loss events are correlated (all connections have loss > 0.05 simultaneously) AND pool size > min: Remove
//   - Otherwise: Hold
func (l *AdaptivePoolLearner) Evaluate(snap *pathmgr.PoolSnapshot, currentSize int) LearnerDecision {
	if snap == nil || len(snap.Samples) == 0 {
		return LearnerDecisionHold
	}

	samples := snap.Samples

	// Find best and worst RTT.
	bestRTT := samples[0].EWMRTT
	worstRTT := samples[0].EWMRTT
	for _, s := range samples[1:] {
		if s.EWMRTT < bestRTT {
			bestRTT = s.EWMRTT
		}
		if s.EWMRTT > worstRTT {
			worstRTT = s.EWMRTT
		}
	}

	// Rule 1: High RTT spread (best < 0.5 * worst) AND pool > min → Remove.
	// Only apply if worst > 0 to avoid division issues.
	if worstRTT > 0 && bestRTT < worstRTT/2 && currentSize > l.poolMin {
		return LearnerDecisionRemove
	}

	// Rule 2: All connections saturated (inflight > 0.8 * cwnd) AND pool < max → Add.
	allSaturated := true
	for _, s := range samples {
		if s.CwndBytes <= 0 {
			allSaturated = false
			break
		}
		threshold := float64(s.CwndBytes) * 0.8
		if float64(s.BytesInFlight) <= threshold {
			allSaturated = false
			break
		}
	}
	if allSaturated && currentSize < l.poolMax {
		return LearnerDecisionAdd
	}

	// Rule 3: Correlated loss (all connections loss > 0.05) AND pool > min → Remove.
	allLossy := true
	for _, s := range samples {
		if s.LossRate <= 0.05 {
			allLossy = false
			break
		}
	}
	if allLossy && currentSize > l.poolMin {
		return LearnerDecisionRemove
	}

	return LearnerDecisionHold
}

// Dialer dials a new QUIC connection. Injected at construction for testability.
type Dialer func(ctx context.Context, addr string, tlsConf *tls.Config, ccName string) (quictr.Connection, error)

// defaultDialer wraps quictr.Dial to satisfy the Dialer type.
func defaultDialer(ctx context.Context, addr string, tlsConf *tls.Config, ccName string) (quictr.Connection, error) {
	return quictr.Dial(ctx, addr, tlsConf, ccName)
}

// Default health check and reconnection parameters.
const (
	DefaultHealthCheckInterval = 500 * time.Millisecond
	DefaultMaxReconnectRetries = 5
	DefaultReconnectBaseDelay  = 100 * time.Millisecond
	DefaultReconnectMaxDelay   = 10 * time.Second
)

// PoolManager manages connection lifecycle for one pool.
type PoolManager struct {
	pool     *pool.Pool
	pathMgr  *pathmgr.PathManager
	learner  *AdaptivePoolLearner
	tlsConf  *tls.Config
	peerAddr string
	ccName   string
	dialer   Dialer
	ticker   *time.Ticker
	mu       sync.Mutex
	lastTick time.Time
	// Health check and reconnection (A-02 fix)
	healthTicker          *time.Ticker
	maxReconnectRetries   int
	reconnectBaseDelay    time.Duration
	reconnectMaxDelay     time.Duration
	consecutiveFailures   int
	lastPoolEmpty         bool // tracks whether we already logged ErrNoConnections
}

// New creates a PoolManager.
// peerAddr: remote address (host:port)
// tlsConf: TLS config for dialing new connections
// ccName: congestion control name for new connections ("cubic", "bbr", "bbrv3", "drl")
// learnerInterval: how often the Learner runs (default 5s)
// dialer: optional Dialer function (nil = use quictr.Dial)
func New(
	p *pool.Pool,
	pm *pathmgr.PathManager,
	learner *AdaptivePoolLearner,
	tlsConf *tls.Config,
	peerAddr, ccName string,
	learnerInterval time.Duration,
	dialer Dialer,
) *PoolManager {
	if learnerInterval <= 0 {
		learnerInterval = 5 * time.Second
	}
	if dialer == nil {
		dialer = defaultDialer
	}
	return &PoolManager{
		pool:                p,
		pathMgr:             pm,
		learner:             learner,
		tlsConf:             tlsConf,
		peerAddr:            peerAddr,
		ccName:              ccName,
		dialer:              dialer,
		ticker:              time.NewTicker(learnerInterval),
		healthTicker:        time.NewTicker(DefaultHealthCheckInterval),
		maxReconnectRetries: DefaultMaxReconnectRetries,
		reconnectBaseDelay:  DefaultReconnectBaseDelay,
		reconnectMaxDelay:   DefaultReconnectMaxDelay,
	}
}

// EnsureMinConnections opens connections until pool.Size() >= pool.MinSize.
// Called at startup to warm the pool.
func (mgr *PoolManager) EnsureMinConnections(ctx context.Context) error {
	for mgr.pool.IsUnderMin() {
		conn, err := mgr.dialer(ctx, mgr.peerAddr, mgr.tlsConf, mgr.ccName)
		if err != nil {
			return fmt.Errorf("poolmgr: dial %s: %w", mgr.peerAddr, err)
		}
		if err := mgr.pool.Add(conn); err != nil {
			_ = conn.Close()
			// Pool may have reached capacity between the IsUnderMin check and Add.
			// This is fine — we are no longer under min.
			break
		}
	}
	return nil
}

// DoWork runs health checks and the learner on their respective tick intervals.
// Health check (A-02 fix): detects closed connections, removes them, attempts reconnection.
// Learner: evaluates pool performance and adjusts size.
func (mgr *PoolManager) DoWork(ctx context.Context) int {
	work := 0

	// Health check tick (A-02 fix).
	select {
	case <-mgr.healthTicker.C:
		work += mgr.healthCheck(ctx)
	default:
	}

	// Learner tick.
	select {
	case <-mgr.ticker.C:
	default:
		return work
	}

	snap := mgr.pathMgr.Snapshot(mgr.peerAddr)
	currentSize := mgr.pool.Size()
	decision := mgr.learner.Evaluate(snap, currentSize)

	switch decision {
	case LearnerDecisionAdd:
		conn, err := mgr.dialer(ctx, mgr.peerAddr, mgr.tlsConf, mgr.ccName)
		if err != nil {
			return 0
		}
		if err := mgr.pool.Add(conn); err != nil {
			_ = conn.Close()
		}
		return 1

	case LearnerDecisionRemove:
		// Remove the worst connection (highest RTT) as measured from the snapshot.
		connToRemove := mgr.worstConn(snap)
		if connToRemove == 0 {
			return 0
		}
		_ = mgr.pool.Remove(connToRemove)
		return 1

	default:
		return 0
	}
}

// worstConn identifies the connection with the highest EWMA RTT from the snapshot.
// Returns 0 if no snapshot or no samples.
func (mgr *PoolManager) worstConn(snap *pathmgr.PoolSnapshot) uint64 {
	if snap == nil || len(snap.Samples) == 0 {
		// Fallback: remove the first connection in the pool.
		conns := mgr.pool.Connections()
		if len(conns) > 0 {
			return conns[0].ID()
		}
		return 0
	}
	worst := snap.Samples[0]
	for _, s := range snap.Samples[1:] {
		if s.EWMRTT > worst.EWMRTT {
			worst = s
		}
	}
	return worst.ConnID
}

// healthCheck detects closed connections, removes them, and attempts reconnection
// with exponential backoff. A-02 fix.
func (mgr *PoolManager) healthCheck(ctx context.Context) int {
	work := 0

	// Detect and remove closed connections.
	conns := mgr.pool.Connections()
	for _, conn := range conns {
		if conn.IsClosed() {
			_ = mgr.pool.Remove(conn.ID())
			slog.Warn("poolmgr: removed closed connection",
				"conn_id", conn.ID(),
				"peer", mgr.peerAddr,
			)
			work++
		}
	}

	// Check if pool is under minimum size and attempt reconnection.
	if mgr.pool.IsUnderMin() {
		if mgr.pool.Size() == 0 {
			if !mgr.lastPoolEmpty {
				slog.Error("poolmgr: pool drained to zero connections",
					"peer", mgr.peerAddr,
					"error", ErrNoConnections,
				)
				mgr.lastPoolEmpty = true
			}
		}

		// Attempt reconnection with exponential backoff.
		retries := 0
		for mgr.pool.IsUnderMin() && retries < mgr.maxReconnectRetries {
			delay := mgr.backoffDelay()
			if delay > 0 {
				select {
				case <-ctx.Done():
					return work
				case <-time.After(delay):
				}
			}

			conn, err := mgr.dialer(ctx, mgr.peerAddr, mgr.tlsConf, mgr.ccName)
			if err != nil {
				mgr.consecutiveFailures++
				slog.Warn("poolmgr: reconnect failed",
					"peer", mgr.peerAddr,
					"attempt", retries+1,
					"error", err,
				)
				retries++
				continue
			}

			if addErr := mgr.pool.Add(conn); addErr != nil {
				_ = conn.Close()
				retries++
				continue
			}

			mgr.consecutiveFailures = 0
			mgr.lastPoolEmpty = false
			slog.Info("poolmgr: reconnected",
				"peer", mgr.peerAddr,
				"pool_size", mgr.pool.Size(),
			)
			work++
			retries++
		}
	} else {
		mgr.consecutiveFailures = 0
		mgr.lastPoolEmpty = false
	}

	return work
}

// backoffDelay returns the exponential backoff delay based on consecutive failures.
func (mgr *PoolManager) backoffDelay() time.Duration {
	if mgr.consecutiveFailures == 0 {
		return 0
	}
	delay := mgr.reconnectBaseDelay
	for i := 1; i < mgr.consecutiveFailures; i++ {
		delay *= 2
		if delay > mgr.reconnectMaxDelay {
			delay = mgr.reconnectMaxDelay
			break
		}
	}
	return delay
}

// Name returns the agent name.
func (mgr *PoolManager) Name() string {
	return "pool-manager"
}

// Close stops the learner and health check tickers.
func (mgr *PoolManager) Close() error {
	mgr.ticker.Stop()
	mgr.healthTicker.Stop()
	return nil
}
