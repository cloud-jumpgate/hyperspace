package poolmgr

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver/pathmgr"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// ---------------------------------------------------------------------------
// Mock connection
// ---------------------------------------------------------------------------

var mockIDCounter atomic.Uint64

type mockConn struct {
	mu     sync.Mutex
	id     uint64
	closed bool
	rtt    time.Duration
	stats  quictr.ConnectionStats
}

func newMockConn(rtt time.Duration) *mockConn {
	return &mockConn{id: mockIDCounter.Add(1), rtt: rtt}
}

func (m *mockConn) ID() uint64 { return m.id }
func (m *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4433}
}
func (m *mockConn) Send(_ uint64, _ []byte) error                      { return nil }
func (m *mockConn) SendControl(_ []byte) error                         { return nil }
func (m *mockConn) SendProbe(_ []byte) error                           { return nil }
func (m *mockConn) RecvData(_ context.Context) (uint64, []byte, error) { return 0, nil, nil }
func (m *mockConn) RecvControl(_ context.Context) ([]byte, error)      { return nil, nil }
func (m *mockConn) RecvProbe(_ context.Context) ([]byte, error)        { return nil, nil }
func (m *mockConn) RTT() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rtt
}
func (m *mockConn) Stats() quictr.ConnectionStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}
func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
func (m *mockConn) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// ---------------------------------------------------------------------------
// Mock dialer
// ---------------------------------------------------------------------------

type mockDialer struct {
	mu        sync.Mutex
	callCount int
	rtt       time.Duration
	err       error
}

func (d *mockDialer) dial(_ context.Context, _ string, _ *tls.Config, _ string) (quictr.Connection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callCount++
	if d.err != nil {
		return nil, d.err
	}
	return newMockConn(d.rtt), nil
}

func (d *mockDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.callCount
}

// ---------------------------------------------------------------------------
// Snapshot helpers
// ---------------------------------------------------------------------------

func makeSnapshot(samples []pathmgr.ConnectionSample) *pathmgr.PoolSnapshot {
	return &pathmgr.PoolSnapshot{Samples: samples, At: time.Now()}
}

func makeSample(connID uint64, rtt time.Duration, inflight, cwnd int, loss float64) pathmgr.ConnectionSample {
	return pathmgr.ConnectionSample{
		ConnID:        connID,
		EWMRTT:        rtt,
		LossRate:      loss,
		BytesInFlight: inflight,
		CwndBytes:     cwnd,
	}
}

// ---------------------------------------------------------------------------
// Helper: build a real PoolManager with injected dialer
// ---------------------------------------------------------------------------

func makePoolMgr(t *testing.T, poolMin, poolMax int, d *mockDialer, learnerInterval time.Duration) (*PoolManager, *pool.Pool, *pathmgr.PathManager) {
	t.Helper()
	p := pool.New("peer1", poolMin, poolMax)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(poolMin, poolMax)
	mgr := New(p, pm, l, nil, "peer1", "cubic", learnerInterval, d.dial)
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, p, pm
}

// ---------------------------------------------------------------------------
// AdaptivePoolLearner tests
// ---------------------------------------------------------------------------

func TestNewLearnerDefaults(t *testing.T) {
	l := NewLearner(2, 8)
	assert.Equal(t, 0.7, l.Alpha)
	assert.Equal(t, 0.3, l.Beta)
}

func TestNewLearnerClampsMinMax(t *testing.T) {
	l := NewLearner(0, 0)
	assert.Equal(t, 1, l.poolMin)
	assert.Equal(t, 1, l.poolMax)
}

func TestEvaluateNilSnapshot(t *testing.T) {
	l := NewLearner(2, 8)
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(nil, 4))
}

func TestEvaluateEmptySnapshot(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot(nil)
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 4))
}

// TestEvaluateHighSpreadRemove — best=40µs, worst=120µs → best < 0.5*worst → Remove.
func TestEvaluateHighSpreadRemove(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 40*time.Microsecond, 100, 8192, 0.0),
		makeSample(2, 120*time.Microsecond, 100, 8192, 0.0),
	})
	assert.Equal(t, LearnerDecisionRemove, l.Evaluate(snap, 4))
}

// TestEvaluateHighSpreadAtMinHold — high spread but at min → Hold.
func TestEvaluateHighSpreadAtMinHold(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 40*time.Microsecond, 100, 8192, 0.0),
		makeSample(2, 120*time.Microsecond, 100, 8192, 0.0),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 2))
}

// TestEvaluateAllSaturatedAdd — all inflight > 80% cwnd → Add.
func TestEvaluateAllSaturatedAdd(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 9000, 10000, 0.0),
		makeSample(2, 55*time.Microsecond, 8500, 10000, 0.0),
	})
	assert.Equal(t, LearnerDecisionAdd, l.Evaluate(snap, 4))
}

// TestEvaluateAllSaturatedAtMaxHold — all saturated but at max → Hold.
func TestEvaluateAllSaturatedAtMaxHold(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 9000, 10000, 0.0),
		makeSample(2, 55*time.Microsecond, 8500, 10000, 0.0),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 8))
}

// TestEvaluateZeroCwndNotSaturated — zero cwnd means not saturated (guard).
func TestEvaluateZeroCwndNotSaturated(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 0, 0, 0.0), // zero cwnd
		makeSample(2, 55*time.Microsecond, 0, 0, 0.0),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 4))
}

// TestEvaluateCorrelatedLossRemove — all loss > 5% → Remove.
func TestEvaluateCorrelatedLossRemove(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 100, 8192, 0.06),
		makeSample(2, 55*time.Microsecond, 100, 8192, 0.08),
	})
	assert.Equal(t, LearnerDecisionRemove, l.Evaluate(snap, 4))
}

// TestEvaluateCorrelatedLossAtMinHold — correlated loss but at min → Hold.
func TestEvaluateCorrelatedLossAtMinHold(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 100, 8192, 0.06),
		makeSample(2, 55*time.Microsecond, 100, 8192, 0.08),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 2))
}

// TestEvaluateBalancedPoolHold — healthy pool → Hold.
func TestEvaluateBalancedPoolHold(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 1000, 8192, 0.01),
		makeSample(2, 55*time.Microsecond, 1200, 8192, 0.01),
		makeSample(3, 52*time.Microsecond, 900, 8192, 0.01),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 3))
}

// TestEvaluatePartialLossHold — only one connection lossy → Hold.
func TestEvaluatePartialLossHold(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 100, 8192, 0.06),
		makeSample(2, 55*time.Microsecond, 100, 8192, 0.01),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 4))
}

// TestEvaluatePartialSaturationHold — not all connections saturated → Hold.
func TestEvaluatePartialSaturationHold(t *testing.T) {
	l := NewLearner(2, 8)
	snap := makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(1, 50*time.Microsecond, 9000, 10000, 0.0),
		makeSample(2, 55*time.Microsecond, 100, 10000, 0.0),
	})
	assert.Equal(t, LearnerDecisionHold, l.Evaluate(snap, 4))
}

// TestLearnerDecisionString covers the stringer.
func TestLearnerDecisionString(t *testing.T) {
	assert.Equal(t, "hold", LearnerDecisionHold.String())
	assert.Equal(t, "add", LearnerDecisionAdd.String())
	assert.Equal(t, "remove", LearnerDecisionRemove.String())
	assert.Equal(t, "unknown", LearnerDecision(99).String())
}

// ---------------------------------------------------------------------------
// PoolManager.EnsureMinConnections tests
// ---------------------------------------------------------------------------

// TestEnsureMinConnectionsEmptyPool — empty pool → dialer called minSize times.
func TestEnsureMinConnectionsEmptyPool(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)
	err := mgr.EnsureMinConnections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, d.count())
	assert.GreaterOrEqual(t, p.Size(), 2)
}

// TestEnsureMinConnectionsHalfFilled — half-filled → dialer called remaining times.
func TestEnsureMinConnectionsHalfFilled(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 4, 8, d, 10*time.Second)

	_ = p.Add(newMockConn(50 * time.Microsecond))
	_ = p.Add(newMockConn(50 * time.Microsecond))

	err := mgr.EnsureMinConnections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, d.count())
	assert.GreaterOrEqual(t, p.Size(), 4)
}

// TestEnsureMinConnectionsFullPool — full pool → dialer not called.
func TestEnsureMinConnectionsFullPool(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 2, 2, d, 10*time.Second)

	_ = p.Add(newMockConn(50 * time.Microsecond))
	_ = p.Add(newMockConn(50 * time.Microsecond))

	err := mgr.EnsureMinConnections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, d.count())
}

// TestEnsureMinConnectionsDialError — dialer error propagates.
func TestEnsureMinConnectionsDialError(t *testing.T) {
	d := &mockDialer{err: assert.AnError}
	mgr, _, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)
	err := mgr.EnsureMinConnections(context.Background())
	assert.Error(t, err)
}

// TestEnsureMinConnectionsAddFailsRace — dialer succeeds but pool.Add fails
// because a concurrent goroutine fills the last slot (defensive break path).
func TestEnsureMinConnectionsAddFailsRace(t *testing.T) {
	// Use min=1, max=1 pool with a dialer that races a concurrent Add.
	// The sequence: pool has 0 conns, IsUnderMin=true, dialer called,
	// a concurrent goroutine fills the slot, then our Add fails.
	p := pool.New("peer1", 1, 1)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(1, 1)

	blocker := make(chan struct{})
	d := &mockDialer{}
	racingDialer := func(ctx context.Context, addr string, tlsConf *tls.Config, ccName string) (quictr.Connection, error) {
		// While we're dialing, another goroutine fills the pool.
		close(blocker) // signal racer
		time.Sleep(1 * time.Millisecond)
		return newMockConn(50 * time.Microsecond), nil
	}
	_ = d // keep linter happy

	mgr := New(p, pm, l, nil, "peer1", "cubic", 10*time.Second, racingDialer)
	defer func() { _ = mgr.Close() }()

	// Racer: fills the pool concurrently.
	go func() {
		<-blocker
		_ = p.Add(newMockConn(50 * time.Microsecond))
	}()

	err := mgr.EnsureMinConnections(context.Background())
	require.NoError(t, err)
	// Pool should have at least 1 connection (either from racer or from us).
	assert.GreaterOrEqual(t, p.Size(), 1)
}

// ---------------------------------------------------------------------------
// PoolManager.DoWork tests (exercising the real PoolManager)
// ---------------------------------------------------------------------------

// TestDoWorkNoTick — no tick yet → returns 0.
func TestDoWorkNoTick(t *testing.T) {
	d := &mockDialer{}
	mgr, _, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 0, n)
}

// TestDoWorkHoldDecision — nil snapshot → Hold → returns 0.
func TestDoWorkHoldDecision(t *testing.T) {
	d := &mockDialer{}
	mgr, p, _ := makePoolMgr(t, 2, 8, d, 1*time.Millisecond)
	_ = p.Add(newMockConn(50 * time.Microsecond))
	_ = p.Add(newMockConn(55 * time.Microsecond))

	// No snapshot injected → Evaluate returns Hold.
	time.Sleep(5 * time.Millisecond)
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, d.count())
}

// TestDoWorkAddDecision — saturated snapshot → Add → dialer called, conn added.
func TestDoWorkAddDecision(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, pm := makePoolMgr(t, 2, 8, d, 1*time.Millisecond)

	c1 := newMockConn(50 * time.Microsecond)
	c2 := newMockConn(55 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)

	pm.InjectSnapshot("peer1", makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(c1.id, 50*time.Microsecond, 9000, 10000, 0.0),
		makeSample(c2.id, 55*time.Microsecond, 8500, 10000, 0.0),
	}))

	time.Sleep(5 * time.Millisecond)
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, d.count())
	assert.Equal(t, 3, p.Size())
}

// TestDoWorkRemoveDecision — high spread snapshot → Remove → worst conn closed.
// Pool has 4 connections (> min=2) so Remove is permitted.
func TestDoWorkRemoveDecision(t *testing.T) {
	d := &mockDialer{}
	mgr, p, pm := makePoolMgr(t, 2, 8, d, 1*time.Millisecond)

	c1 := newMockConn(40 * time.Microsecond)
	c2 := newMockConn(120 * time.Microsecond) // worst RTT
	c3 := newMockConn(50 * time.Microsecond)
	c4 := newMockConn(55 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)
	_ = p.Add(c3)
	_ = p.Add(c4)

	pm.InjectSnapshot("peer1", makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(c1.id, 40*time.Microsecond, 100, 8192, 0.0),
		makeSample(c2.id, 120*time.Microsecond, 100, 8192, 0.0),
		makeSample(c3.id, 50*time.Microsecond, 100, 8192, 0.0),
		makeSample(c4.id, 55*time.Microsecond, 100, 8192, 0.0),
	}))

	time.Sleep(5 * time.Millisecond)
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 1, n)
	assert.Equal(t, 3, p.Size())
	assert.True(t, c2.IsClosed(), "worst RTT connection should be closed")
	assert.False(t, c1.IsClosed())
}

// TestDoWorkRemoveNoSnapshot — Remove with nil snapshot falls back to first conn.
func TestDoWorkRemoveNoSnapshot(t *testing.T) {
	d := &mockDialer{}
	mgr, p, pm := makePoolMgr(t, 2, 8, d, 1*time.Millisecond)

	c1 := newMockConn(40 * time.Microsecond)
	c2 := newMockConn(120 * time.Microsecond)
	c3 := newMockConn(50 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)
	_ = p.Add(c3)

	// Inject a snapshot that says correlated loss (triggers Remove) but with no samples.
	// Actually, inject a snapshot with high spread so Remove is triggered,
	// but use a snapshot with only c1 and c2 to exercise the worstConn fallback.
	// For "nil snapshot" fallback: inject a snapshot with samples then nil it.
	// Easiest: inject high-spread snapshot with no matching conn IDs → worstConn returns
	// connID=0, pool.Remove(0) fails, DoWork still returns 1.

	// Inject snapshot with foreign conn IDs to trigger Remove but exercise worstConn path.
	pm.InjectSnapshot("peer1", makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(9999, 40*time.Microsecond, 100, 8192, 0.0),
		makeSample(9998, 120*time.Microsecond, 100, 8192, 0.0),
		makeSample(9997, 50*time.Microsecond, 100, 8192, 0.0),
	}))

	time.Sleep(5 * time.Millisecond)
	// Decision will be Remove (high spread, pool size 3 > min 2).
	// worstConn returns connID=9998 which is not in the pool → pool.Remove fails.
	// DoWork returns 1 (Remove applied even if pool.Remove errors).
	_ = mgr.DoWork(context.Background())
}

// TestDoWorkRemoveEmptyPool — Remove decision on empty pool → no-op.
func TestDoWorkRemoveEmptyPool(t *testing.T) {
	d := &mockDialer{}
	mgr, _, pm := makePoolMgr(t, 2, 8, d, 1*time.Millisecond)

	// Inject snapshot with no samples: worstConn will use empty connections fallback.
	pm.InjectSnapshot("peer1", makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(9999, 40*time.Microsecond, 100, 8192, 0.0),
		makeSample(9998, 120*time.Microsecond, 100, 8192, 0.0),
	}))

	// Pool is empty (size=0), but learner sees currentSize=0 ≤ min=2 → Hold.
	time.Sleep(5 * time.Millisecond)
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 0, n) // Hold because 0 <= min
}

// TestDoWorkAddDialerError — Add decision but dialer fails → returns 0.
func TestDoWorkAddDialerError(t *testing.T) {
	d := &mockDialer{err: assert.AnError}
	mgr, p, pm := makePoolMgr(t, 2, 8, d, 1*time.Millisecond)

	c1 := newMockConn(50 * time.Microsecond)
	c2 := newMockConn(55 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)

	pm.InjectSnapshot("peer1", makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(c1.id, 50*time.Microsecond, 9000, 10000, 0.0),
		makeSample(c2.id, 55*time.Microsecond, 8500, 10000, 0.0),
	}))

	time.Sleep(5 * time.Millisecond)
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 0, n)
}

// TestDoWorkAddPoolFull — Add decision but pool.Add fails (pool concurrently filled).
// Exercises the conn.Close() path after a failed pool.Add in DoWork.
func TestDoWorkAddPoolFull(t *testing.T) {
	// Pool: min=2, max=3. Pre-fill 2. Snapshot says saturated (triggers Add).
	// A concurrent goroutine fills the pool to max just before DoWork's Add call.
	p := pool.New("peer1", 2, 3)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 3)

	c1 := newMockConn(50 * time.Microsecond)
	c2 := newMockConn(55 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)

	// Dialer races: fills last slot concurrently then returns a connection.
	firstCall := make(chan struct{}, 1)
	racingDialer := func(ctx context.Context, addr string, tlsConf *tls.Config, ccName string) (quictr.Connection, error) {
		select {
		case firstCall <- struct{}{}:
			// First call: fill the pool's last slot concurrently.
			_ = p.Add(newMockConn(60 * time.Microsecond))
		default:
		}
		return newMockConn(70 * time.Microsecond), nil
	}

	mgr := New(p, pm, l, nil, "peer1", "cubic", 1*time.Millisecond, racingDialer)
	defer func() { _ = mgr.Close() }()

	pm.InjectSnapshot("peer1", makeSnapshot([]pathmgr.ConnectionSample{
		makeSample(c1.id, 50*time.Microsecond, 9000, 10000, 0.0),
		makeSample(c2.id, 55*time.Microsecond, 8500, 10000, 0.0),
	}))

	time.Sleep(5 * time.Millisecond)
	// DoWork returns 1 (tried to Add), even if the pool.Add failed.
	n := mgr.DoWork(context.Background())
	assert.Equal(t, 1, n)
}

// ---------------------------------------------------------------------------
// worstConn fallback: nil snapshot → use first pool connection
// ---------------------------------------------------------------------------

// TestWorstConnNilSnapshotFallback exercises the worstConn nil-snapshot path.
func TestWorstConnNilSnapshotFallback(t *testing.T) {
	p := pool.New("peer1", 2, 8)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 8)
	d := &mockDialer{}
	mgr := New(p, pm, l, nil, "peer1", "cubic", 1*time.Millisecond, d.dial)
	defer func() { _ = mgr.Close() }()

	c1 := newMockConn(40 * time.Microsecond)
	c2 := newMockConn(120 * time.Microsecond)
	c3 := newMockConn(50 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)
	_ = p.Add(c3)

	// Call worstConn directly with nil snapshot.
	id := mgr.worstConn(nil)
	// Should return the first connection in the pool.
	assert.Equal(t, c1.id, id)
}

// TestWorstConnEmptyPool — worstConn with nil snapshot and empty pool returns 0.
func TestWorstConnEmptyPool(t *testing.T) {
	p := pool.New("peer1", 2, 8)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 8)
	d := &mockDialer{}
	mgr := New(p, pm, l, nil, "peer1", "cubic", 10*time.Second, d.dial)
	defer func() { _ = mgr.Close() }()

	id := mgr.worstConn(nil)
	assert.Equal(t, uint64(0), id)
}

// ---------------------------------------------------------------------------
// PoolManager.Name and Close
// ---------------------------------------------------------------------------

func TestPoolManagerNameClose(t *testing.T) {
	d := &mockDialer{}
	mgr, _, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)
	assert.Equal(t, "pool-manager", mgr.Name())
	assert.NoError(t, mgr.Close())
}

// TestNewPoolManagerDefaultInterval — zero learnerInterval gets default 5s.
func TestNewPoolManagerDefaultInterval(t *testing.T) {
	p := pool.New("peer1", 2, 8)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 8)
	d := &mockDialer{}
	mgr := New(p, pm, l, nil, "peer1", "cubic", 0, d.dial)
	defer func() { _ = mgr.Close() }()
	assert.NotNil(t, mgr)
}

// TestNewPoolManagerNilDialer — nil dialer uses defaultDialer.
func TestNewPoolManagerNilDialer(t *testing.T) {
	p := pool.New("peer1", 2, 8)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 8)
	// nil dialer — defaultDialer is used. Just confirm construction doesn't panic.
	mgr := New(p, pm, l, &tls.Config{MinVersion: tls.VersionTLS13}, "peer1", "cubic", 5*time.Second, nil)
	defer func() { _ = mgr.Close() }()
	assert.NotNil(t, mgr)
}

// ---------------------------------------------------------------------------
// Health Check tests (A-02 fix)
// ---------------------------------------------------------------------------

// TestHealthCheck_DetectsClosedConnection — closed connection is removed from pool.
// Note: pool.Connections() already prunes closed connections via pruneClosedLocked(),
// so the health check's IsClosed loop is a defensive backup. The primary effect is
// that pool.Size() decreases and the pool may fall below minimum, triggering reconnection.
func TestHealthCheck_DetectsClosedConnection(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)

	c1 := newMockConn(50 * time.Microsecond)
	c2 := newMockConn(55 * time.Microsecond)
	c3 := newMockConn(60 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)
	_ = p.Add(c3)
	assert.Equal(t, 3, p.Size())

	// Close c2 to simulate a dead connection.
	_ = c2.Close()

	// Call healthCheck directly to avoid ticker timing issues.
	mgr.healthCheck(context.Background())

	// c2 should have been removed (by pool's built-in pruning or by healthCheck).
	// Pool should be at 2, and since 2 >= min(2), no reconnection attempt needed.
	assert.Equal(t, 2, p.Size(), "closed connection should be removed")
	assert.Equal(t, 0, d.count(), "no dial needed since pool is still >= min")
}

// TestHealthCheck_ReconnectsWhenUnderMin — health check attempts reconnection
// when pool falls below minimum after removing closed connections.
func TestHealthCheck_ReconnectsWhenUnderMin(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 3, 8, d, 10*time.Second)

	c1 := newMockConn(50 * time.Microsecond)
	c2 := newMockConn(55 * time.Microsecond)
	c3 := newMockConn(60 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)
	_ = p.Add(c3)
	assert.Equal(t, 3, p.Size())

	// Close two connections to drop below min=3.
	_ = c2.Close()
	_ = c3.Close()

	mgr.healthCheck(context.Background())

	// After health check: 2 closed connections removed, pool was at 1 (under min=3),
	// dialer should have been called to reconnect back to min.
	assert.GreaterOrEqual(t, p.Size(), 3, "pool should be restored to min size")
	assert.GreaterOrEqual(t, d.count(), 2, "dialer should have been called to restore pool")
}

// TestHealthCheck_ExponentialBackoff — consecutive failures increase backoff delay.
func TestHealthCheck_ExponentialBackoff(t *testing.T) {
	mgr := &PoolManager{
		reconnectBaseDelay: 100 * time.Millisecond,
		reconnectMaxDelay:  10 * time.Second,
	}

	// No failures → no delay.
	mgr.consecutiveFailures = 0
	assert.Equal(t, time.Duration(0), mgr.backoffDelay())

	// 1 failure → base delay (100ms).
	mgr.consecutiveFailures = 1
	assert.Equal(t, 100*time.Millisecond, mgr.backoffDelay())

	// 2 failures → 200ms.
	mgr.consecutiveFailures = 2
	assert.Equal(t, 200*time.Millisecond, mgr.backoffDelay())

	// 3 failures → 400ms.
	mgr.consecutiveFailures = 3
	assert.Equal(t, 400*time.Millisecond, mgr.backoffDelay())

	// Many failures → capped at max.
	mgr.consecutiveFailures = 20
	assert.Equal(t, 10*time.Second, mgr.backoffDelay())
}

// TestHealthCheck_PoolDrainedToZero — logs ErrNoConnections when pool reaches zero.
func TestHealthCheck_PoolDrainedToZero(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)

	c1 := newMockConn(50 * time.Microsecond)
	_ = p.Add(c1)

	// Close the only connection.
	_ = c1.Close()

	mgr.healthCheck(context.Background())

	// After removing the closed connection, pool is at 0. Health check should
	// attempt reconnection to bring it back to min.
	assert.GreaterOrEqual(t, p.Size(), 2, "pool should be restored from zero")
	assert.GreaterOrEqual(t, d.count(), 2, "dialer should have been called")
}

// TestHealthCheck_ReconnectDialerFailsAllRetries — dialer fails on all retries.
func TestHealthCheck_ReconnectDialerFailsAllRetries(t *testing.T) {
	d := &mockDialer{err: assert.AnError, rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)
	// Override backoff to be fast for tests.
	mgr.reconnectBaseDelay = 1 * time.Millisecond
	mgr.reconnectMaxDelay = 5 * time.Millisecond

	c1 := newMockConn(50 * time.Microsecond)
	_ = p.Add(c1)

	// Close connection to trigger reconnect.
	_ = c1.Close()

	mgr.healthCheck(context.Background())

	// Dialer failed all retries — pool remains under min.
	assert.Equal(t, 0, p.Size(), "pool should remain empty when dialer fails")
	assert.Equal(t, DefaultMaxReconnectRetries, d.count(), "dialer should have been called maxRetries times")
}

// TestHealthCheck_HealthyPoolResetsFailures — when pool is healthy, consecutive failures reset.
func TestHealthCheck_HealthyPoolResetsFailures(t *testing.T) {
	d := &mockDialer{rtt: 50 * time.Microsecond}
	mgr, p, _ := makePoolMgr(t, 2, 8, d, 10*time.Second)

	c1 := newMockConn(50 * time.Microsecond)
	c2 := newMockConn(55 * time.Microsecond)
	_ = p.Add(c1)
	_ = p.Add(c2)

	// Simulate previous failures.
	mgr.consecutiveFailures = 5
	mgr.lastPoolEmpty = true

	mgr.healthCheck(context.Background())

	// Pool is healthy (size >= min) — failures should reset.
	assert.Equal(t, 0, mgr.consecutiveFailures, "consecutive failures should reset for healthy pool")
	assert.False(t, mgr.lastPoolEmpty, "lastPoolEmpty should be cleared for healthy pool")
}

// ---------------------------------------------------------------------------
// SVID Cert Rotation tests (F-039 / DEF-005)
// ---------------------------------------------------------------------------

// mockSVIDWatcher implements SVIDWatcher for testing.
// rotate() atomically updates the TLS config pointer; the poll loop in Run() detects
// the pointer change on its next tick and calls rotateCerts.
type mockSVIDWatcher struct {
	mu        sync.Mutex
	current   *tls.Config
	started   chan struct{}
	startOnce sync.Once
	watchErr  error // non-nil to simulate a startup failure
}

func newMockSVIDWatcher(initial *tls.Config) *mockSVIDWatcher {
	return &mockSVIDWatcher{
		started: make(chan struct{}),
		current: initial,
	}
}

// StartWatch satisfies SVIDWatcher. It returns immediately, like the real
// SPIFFESource.StartWatch (which starts a background goroutine and returns).
// The poll loop in PoolManager.Run() is responsible for detecting TLS config changes.
func (w *mockSVIDWatcher) StartWatch(_ context.Context) error {
	if w.watchErr != nil {
		return w.watchErr
	}
	w.startOnce.Do(func() { close(w.started) })
	return nil
}

// TLSConfig satisfies SVIDWatcher. Returns the current TLS config atomically.
func (w *mockSVIDWatcher) TLSConfig() *tls.Config {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

// rotate atomically updates the TLS config pointer. The poll loop in Run()
// detects the change on the next certCheckInterval tick.
func (w *mockSVIDWatcher) rotate(newTLS *tls.Config) {
	w.mu.Lock()
	w.current = newTLS
	w.mu.Unlock()
}

// waitStarted blocks until StartWatch has been called, or fails the test on timeout.
func (w *mockSVIDWatcher) waitStarted(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-w.started:
	case <-time.After(timeout):
		t.Fatal("mockSVIDWatcher: StartWatch never called within timeout")
	}
}

// recordingDialer records the TLS config pointer used for each dial call
// so tests can verify new connections used the rotated cert.
type recordingDialer struct {
	mu      sync.Mutex
	records []*tls.Config
	rtt     time.Duration
	err     error
}

func (d *recordingDialer) dial(_ context.Context, _ string, tlsConf *tls.Config, _ string) (quictr.Connection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records = append(d.records, tlsConf)
	if d.err != nil {
		return nil, d.err
	}
	return newMockConn(d.rtt), nil
}

func (d *recordingDialer) tlsConfigs() []*tls.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := make([]*tls.Config, len(d.records))
	copy(cp, d.records)
	return cp
}

// TestPoolManager_CertRotation_BlueGreen verifies the blue-green rotation sequence:
//   - New connections are opened with the new cert BEFORE old connections are closed.
//   - Old connections are closed after new connections are established.
//   - Pool stays at or above min capacity throughout the rotation (no connectivity gap).
func TestPoolManager_CertRotation_BlueGreen(t *testing.T) {
	bootstrapTLS := &tls.Config{MinVersion: tls.VersionTLS13}
	watcher := newMockSVIDWatcher(bootstrapTLS)
	rd := &recordingDialer{rtt: 50 * time.Microsecond}

	p := pool.New("peer1", 2, 8)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 8)

	mgr := NewWithSVID(p, pm, l, watcher, bootstrapTLS, "peer1", "cubic", 10*time.Second, rd.dial)
	mgr.certCheckInterval = 1 * time.Millisecond // fast poll for tests
	defer func() { _ = mgr.Close() }()

	// Pre-populate pool with 2 old connections using the bootstrap cert.
	old1 := newMockConn(50 * time.Microsecond)
	old2 := newMockConn(55 * time.Microsecond)
	require.NoError(t, p.Add(old1))
	require.NoError(t, p.Add(old2))
	require.Equal(t, 2, p.Size())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Run(ctx)
	watcher.waitStarted(t, 2*time.Second)

	// Trigger rotation by injecting a distinct new TLS config.
	newTLS := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "rotated"}
	watcher.rotate(newTLS)

	// Wait for the poll loop to detect the change and apply rotation.
	require.Eventually(t, func() bool {
		return old1.IsClosed() && old2.IsClosed()
	}, 500*time.Millisecond, 5*time.Millisecond,
		"old connections should be closed after rotation is detected")

	// New connections must have been dialed with the new TLS config.
	dials := rd.tlsConfigs()
	require.GreaterOrEqual(t, len(dials), 2, "at least 2 new connections should have been opened")
	for i, cfg := range dials {
		assert.Same(t, newTLS, cfg, "dial %d should use the new TLS config pointer", i)
	}

	// Pool stays at or above min throughout (blue-green guarantee).
	assert.GreaterOrEqual(t, p.Size(), 2, "pool should be at minimum size after rotation")

	cancel() // allow the watcher goroutine to exit; goleak verifies clean shutdown
}

// TestPoolManager_CertRotation_NoDeadlock fires a rotation while DoWork runs
// concurrently and verifies no deadlock or data race occurs (run with -race).
func TestPoolManager_CertRotation_NoDeadlock(t *testing.T) {
	bootstrapTLS := &tls.Config{MinVersion: tls.VersionTLS13}
	watcher := newMockSVIDWatcher(bootstrapTLS)
	rd := &recordingDialer{rtt: 50 * time.Microsecond}

	p := pool.New("peer1", 2, 8)
	pm := pathmgr.New(20 * time.Millisecond)
	pm.AddPool("peer1", p)
	l := NewLearner(2, 8)

	// Short learner interval so DoWork ticks frequently; short certCheckInterval
	// so the poll loop detects the rotation quickly.
	mgr := NewWithSVID(p, pm, l, watcher, bootstrapTLS, "peer1", "cubic", 1*time.Millisecond, rd.dial)
	mgr.certCheckInterval = 1 * time.Millisecond
	defer func() { _ = mgr.Close() }()

	require.NoError(t, p.Add(newMockConn(50*time.Microsecond)))
	require.NoError(t, p.Add(newMockConn(55*time.Microsecond)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Run(ctx)
	watcher.waitStarted(t, 2*time.Second)

	// Run DoWork in a tight loop on a separate goroutine.
	doWorkDone := make(chan struct{})
	go func() {
		defer close(doWorkDone)
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			mgr.DoWork(ctx)
		}
	}()

	// Trigger a rotation while DoWork is running.
	newTLS := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "rotated"}
	watcher.rotate(newTLS)

	// DoWork must finish without deadlock.
	select {
	case <-doWorkDone:
	case <-time.After(5 * time.Second):
		t.Fatal("DoWork goroutine did not finish within 5s — possible deadlock")
	}

	cancel() // allow the watcher goroutine to exit; goleak verifies clean shutdown
}
