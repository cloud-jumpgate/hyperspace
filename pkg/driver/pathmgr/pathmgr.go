// Package pathmgr implements the Hyperspace path intelligence agent.
// It sends PING probes on each connection's probe stream and maintains
// per-connection RTT statistics using EWMA smoothing.
package pathmgr

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/probes"
)

const (
	alphaEWMA  = 0.125 // EWMA smoothing factor for sRTT
	betaRTTVar = 0.25  // EWMA smoothing factor for rttVar
)

// ConnectionSample holds per-connection path statistics. Immutable once published.
type ConnectionSample struct {
	ConnID        uint64
	EWMRTT        time.Duration // smoothed RTT (α = 0.125)
	RTTVar        time.Duration // RTT variance (β = 0.25)
	LossRate      float64       // 0.0–1.0, rolling 1-second window
	Throughput    int64         // bytes/second, last 1s
	BytesInFlight int
	CwndBytes     int
	LastProbe     time.Time
}

// PoolSnapshot is an atomic-pointer-swapped snapshot of all connections in a pool.
// The Path Manager builds a new snapshot every probe cycle and swaps atomically.
// Readers (Arbitrator, Pool Learner) read the latest snapshot without locks.
type PoolSnapshot struct {
	Samples []ConnectionSample
	At      time.Time
}

// connState holds mutable per-connection RTT state protected by the PathManager mu.
type connState struct {
	sRTT   time.Duration
	rttVar time.Duration
}

// pendingProbe records a PING that has been sent but not yet acknowledged.
type pendingProbe struct {
	connID uint64
	peer   string
	sentAt time.Time
}

// PathManager is the path intelligence agent.
type PathManager struct {
	mu            sync.Mutex
	pools         map[string]*pool.Pool
	snapshots     map[string]*atomic.Pointer[PoolSnapshot] // peer → snapshot ptr
	connStates    map[uint64]*connState                    // connID → ewma state
	probeInterval time.Duration
	seq           uint64                  // monotonic ping sequence counter (protected by mu)
	pending       map[uint64]pendingProbe // seq → pending probe info
}

// New creates a PathManager with the given probe interval.
func New(probeInterval time.Duration) *PathManager {
	if probeInterval <= 0 {
		probeInterval = 20 * time.Millisecond
	}
	return &PathManager{
		pools:         make(map[string]*pool.Pool),
		snapshots:     make(map[string]*atomic.Pointer[PoolSnapshot]),
		connStates:    make(map[uint64]*connState),
		probeInterval: probeInterval,
		pending:       make(map[uint64]pendingProbe),
	}
}

// AddPool registers a pool. The PathManager will probe all its connections.
func (pm *PathManager) AddPool(peer string, p *pool.Pool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pools[peer] = p
	ptr := &atomic.Pointer[PoolSnapshot]{}
	pm.snapshots[peer] = ptr
}

// Snapshot returns the latest PoolSnapshot for a peer (nil if not yet available).
func (pm *PathManager) Snapshot(peer string) *PoolSnapshot {
	pm.mu.Lock()
	ptr, ok := pm.snapshots[peer]
	pm.mu.Unlock()
	if !ok {
		return nil
	}
	return ptr.Load()
}

// DoWork sends PINGs on all pool connections, then polls for PONGs.
// Returns: PINGs sent + PONGs received.
func (pm *PathManager) DoWork(ctx context.Context) int {
	pm.mu.Lock()
	// Collect peers and pool references under lock.
	type peerPool struct {
		peer string
		p    *pool.Pool
	}
	pairs := make([]peerPool, 0, len(pm.pools))
	for peer, p := range pm.pools {
		pairs = append(pairs, peerPool{peer: peer, p: p})
	}
	pm.mu.Unlock()

	if len(pairs) == 0 {
		return 0
	}

	total := 0

	// Phase 1: Send PINGs.
	for _, pp := range pairs {
		conns := pp.p.Connections()
		for _, conn := range conns {
			if conn.IsClosed() {
				continue
			}
			pm.mu.Lock()
			pm.seq++
			seq := pm.seq
			sentAt := time.Now()
			pm.pending[seq] = pendingProbe{
				connID: conn.ID(),
				peer:   pp.peer,
				sentAt: sentAt,
			}
			pm.mu.Unlock()

			buf := make([]byte, probes.PingFrameLen)
			if err := probes.EncodePing(buf, seq, sentAt); err == nil {
				if err := conn.SendProbe(buf); err == nil {
					total++
				} else {
					// Remove the pending probe if send failed.
					pm.mu.Lock()
					delete(pm.pending, seq)
					pm.mu.Unlock()
				}
			}
		}
	}

	// Phase 2: Receive PONGs (non-blocking).
	for _, pp := range pairs {
		conns := pp.p.Connections()
		pongCount := 0
		updatedSamples := make(map[uint64]ConnectionSample)

		for _, conn := range conns {
			if conn.IsClosed() {
				continue
			}
			data, err := conn.RecvProbe(ctx)
			if err != nil || len(data) == 0 {
				continue
			}
			pong, err := probes.DecodePong(data)
			if err != nil {
				continue
			}

			pm.mu.Lock()
			pp2, ok := pm.pending[pong.Seq]
			if ok {
				delete(pm.pending, pong.Seq)
			}
			pm.mu.Unlock()

			if !ok {
				continue
			}

			now := time.Now()
			rttSample := now.Sub(pp2.sentAt)

			pm.mu.Lock()
			state, exists := pm.connStates[pp2.connID]
			if !exists {
				state = &connState{
					sRTT:   rttSample,
					rttVar: rttSample / 2,
				}
				pm.connStates[pp2.connID] = state
			} else {
				// EWMA update:
				// sRTT = (1 - α) * sRTT + α * r
				// rttVar = (1 - β) * rttVar + β * |r - sRTT|
				diff := rttSample - state.sRTT
				if diff < 0 {
					diff = -diff
				}
				state.sRTT = time.Duration(float64(state.sRTT)*(1-alphaEWMA) + float64(rttSample)*alphaEWMA)
				state.rttVar = time.Duration(float64(state.rttVar)*(1-betaRTTVar) + float64(diff)*betaRTTVar)
			}
			sRTT := state.sRTT
			rttVar := state.rttVar
			pm.mu.Unlock()

			stats := conn.Stats()
			updatedSamples[conn.ID()] = ConnectionSample{
				ConnID:        conn.ID(),
				EWMRTT:        sRTT,
				RTTVar:        rttVar,
				LossRate:      stats.Loss,
				Throughput:    stats.Throughput,
				BytesInFlight: stats.BytesInFlight,
				CwndBytes:     stats.CongestionWindow,
				LastProbe:     now,
			}
			pongCount++
			total++
		}

		// Build and publish new snapshot if we got any PONGs.
		if pongCount > 0 {
			// Merge with existing snapshot samples for connections we didn't hear from.
			pm.mu.Lock()
			ptr := pm.snapshots[pp.peer]
			pm.mu.Unlock()

			existing := ptr.Load()
			var samples []ConnectionSample
			if existing != nil {
				for _, s := range existing.Samples {
					if _, updated := updatedSamples[s.ConnID]; !updated {
						samples = append(samples, s)
					}
				}
			}
			for _, s := range updatedSamples {
				samples = append(samples, s)
			}

			snap := &PoolSnapshot{
				Samples: samples,
				At:      time.Now(),
			}
			ptr.Store(snap)
		}
	}

	return total
}

// InjectSnapshot directly stores a snapshot for a peer.
// This is intended for testing only — production code must use DoWork.
func (pm *PathManager) InjectSnapshot(peer string, snap *PoolSnapshot) {
	pm.mu.Lock()
	ptr, ok := pm.snapshots[peer]
	pm.mu.Unlock()
	if !ok {
		return
	}
	ptr.Store(snap)
}

// Name returns the agent name.
func (pm *PathManager) Name() string {
	return "path-manager"
}

// Close performs cleanup. The PathManager itself has no goroutines.
func (pm *PathManager) Close() error {
	return nil
}
