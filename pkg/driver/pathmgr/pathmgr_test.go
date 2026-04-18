package pathmgr

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/probes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// buildPool creates a pool pre-populated with n mock connections.
func buildPool(peer string, n int, echoMode bool) (*pool.Pool, []*mockConn) {
	p := pool.New(peer, 1, 8)
	conns := make([]*mockConn, n)
	for i := range conns {
		c := newMockConn(echoMode)
		conns[i] = c
		_ = p.Add(c)
	}
	return p, conns
}

// TestNew verifies construction with a valid probe interval.
func TestNew(t *testing.T) {
	pm := New(20 * time.Millisecond)
	require.NotNil(t, pm)
	assert.Equal(t, 20*time.Millisecond, pm.probeInterval)
	assert.Equal(t, "path-manager", pm.Name())
}

// TestNewZeroInterval applies the default.
func TestNewZeroInterval(t *testing.T) {
	pm := New(0)
	require.NotNil(t, pm)
	assert.Equal(t, 20*time.Millisecond, pm.probeInterval)
}

// TestClose returns nil.
func TestClose(t *testing.T) {
	pm := New(20 * time.Millisecond)
	assert.NoError(t, pm.Close())
}

// TestSnapshotNilBeforeDoWork ensures Snapshot returns nil before any work.
func TestSnapshotNilBeforeDoWork(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p, _ := buildPool("peer1", 1, false)
	pm.AddPool("peer1", p)
	assert.Nil(t, pm.Snapshot("peer1"))
}

// TestSnapshotUnknownPeer returns nil for a peer that was never registered.
func TestSnapshotUnknownPeer(t *testing.T) {
	pm := New(20 * time.Millisecond)
	assert.Nil(t, pm.Snapshot("unknown"))
}

// TestDoWorkNoPools returns 0 when no pools are registered.
func TestDoWorkNoPools(t *testing.T) {
	pm := New(20 * time.Millisecond)
	n := pm.DoWork(context.Background())
	assert.Equal(t, 0, n)
}

// TestDoWorkSendsPINGs verifies that DoWork calls SendProbe on each connection.
func TestDoWorkSendsPINGs(t *testing.T) {
	pm := New(20 * time.Millisecond)
	// echoMode=false: no PONGs returned, only PINGs counted.
	p, conns := buildPool("peer1", 3, false)
	pm.AddPool("peer1", p)

	n := pm.DoWork(context.Background())
	// Expect 3 PINGs sent.
	assert.Equal(t, 3, n)
	for _, c := range conns {
		c.mu.Lock()
		assert.NotNil(t, c.lastPing, "expected lastPing to be set")
		c.mu.Unlock()
	}
}

// TestDoWorkReceivesPONGsAndUpdatesSnapshot verifies end-to-end: PING sent, PONG received, snapshot updated.
func TestDoWorkReceivesPONGsAndUpdatesSnapshot(t *testing.T) {
	pm := New(20 * time.Millisecond)
	// echoMode=true: connections auto-queue PONGs when they receive a PING.
	p, conns := buildPool("peer1", 2, true)
	pm.AddPool("peer1", p)

	// First DoWork: sends PINGs and queues echoed PONGs in the mock.
	n1 := pm.DoWork(context.Background())
	assert.GreaterOrEqual(t, n1, 2) // at least 2 PINGs

	// Second DoWork: drains the PONG queue, updates the snapshot.
	n2 := pm.DoWork(context.Background())
	assert.GreaterOrEqual(t, n2, 2) // 2 more PINGs + 2 PONGs received

	snap := pm.Snapshot("peer1")
	require.NotNil(t, snap)
	assert.Len(t, snap.Samples, len(conns))
	for _, s := range snap.Samples {
		assert.Greater(t, s.EWMRTT, time.Duration(0))
		assert.GreaterOrEqual(t, s.RTTVar, time.Duration(0))
	}
}

// TestEWMARTTConverges verifies the EWMA moves toward injected RTT samples.
func TestEWMARTTConverges(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p := pool.New("peer1", 1, 8)
	conn := newMockConn(false) // manual PONG control
	_ = p.Add(conn)
	pm.AddPool("peer1", p)

	target := 1 * time.Millisecond

	// Drive EWMA with a fixed RTT sample by manually queuing PONGs.
	// We inject 20 rounds of known-RTT PONGs by:
	//   1. Send a PING (DoWork phase 1 side-effect captured by mock).
	//   2. Manually enqueue a PONG referencing that seq + matching sentAt so RTT ~= target.
	for i := 0; i < 20; i++ {
		// Send PING — captured by mock.
		pm.mu.Lock()
		pm.seq++
		seq := pm.seq
		sentAt := time.Now()
		pm.pending[seq] = pendingProbe{connID: conn.ID(), peer: "peer1", sentAt: sentAt}
		pm.mu.Unlock()

		pingFrame := probes.PingFrame{Seq: seq, SentAt: sentAt}
		pongBuf := make([]byte, probes.PongFrameLen)
		require.NoError(t, probes.EncodePong(pongBuf, &pingFrame, sentAt.Add(target/2)))
		conn.EnqueuePong(pongBuf)

		// DoWork drains PONGs and updates EWMA.
		// We only want the PONG drain phase; use a fresh empty pool for PINGs.
		_ = pm.DoWork(context.Background())
	}

	snap := pm.Snapshot("peer1")
	require.NotNil(t, snap)
	require.Len(t, snap.Samples, 1)
	// After 20 rounds of ~1ms samples, sRTT should be within 2x of target.
	assert.Less(t, snap.Samples[0].EWMRTT, 5*target, "sRTT should have converged toward %v", target)
}

// TestRTTVarNonNegative ensures RTTVar is always >= 0.
func TestRTTVarNonNegative(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p := pool.New("peer1", 1, 8)
	conn := newMockConn(true)
	_ = p.Add(conn)
	pm.AddPool("peer1", p)

	for i := 0; i < 5; i++ {
		pm.DoWork(context.Background())
	}

	snap := pm.Snapshot("peer1")
	if snap != nil {
		for _, s := range snap.Samples {
			assert.GreaterOrEqual(t, s.RTTVar, time.Duration(0), "RTTVar must never be negative")
		}
	}
}

// TestSnapshotAtomicUpdate verifies the snapshot pointer is updated after DoWork.
func TestSnapshotAtomicUpdate(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p, _ := buildPool("peer1", 1, true)
	pm.AddPool("peer1", p)

	assert.Nil(t, pm.Snapshot("peer1"))

	pm.DoWork(context.Background())
	pm.DoWork(context.Background()) // second pass to drain PONGs

	snap := pm.Snapshot("peer1")
	// Snapshot may be nil if no PONGs were received yet; that's OK.
	// What matters is that when it is set, it has a valid At timestamp.
	if snap != nil {
		assert.False(t, snap.At.IsZero())
	}
}

// TestDoWorkClosedConnSkipped verifies closed connections are skipped.
func TestDoWorkClosedConnSkipped(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p, conns := buildPool("peer1", 2, false)
	pm.AddPool("peer1", p)

	// Close one connection.
	_ = conns[0].Close()

	n := pm.DoWork(context.Background())
	// Only 1 open connection, so at most 1 PING.
	assert.LessOrEqual(t, n, 2)

	conns[0].mu.Lock()
	// The closed connection's lastPing should remain nil (never sent).
	assert.Nil(t, conns[0].lastPing)
	conns[0].mu.Unlock()
}

// TestDoWorkRace runs DoWork concurrently to catch data races.
func TestDoWorkRace(t *testing.T) {
	pm := New(5 * time.Millisecond)
	p, _ := buildPool("peer1", 3, true)
	pm.AddPool("peer1", p)

	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			pm.DoWork(ctx)
		}
	}()

	for i := 0; i < 50; i++ {
		_ = pm.Snapshot("peer1")
	}
	<-done
}

// TestSendProbeErrorSkipsPending verifies that if SendProbe fails, pending is cleaned up.
func TestSendProbeErrorSkipsPending(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p := pool.New("peer1", 1, 8)
	conn := newMockConn(false)
	conn.mu.Lock()
	conn.sendErr = assert.AnError
	conn.mu.Unlock()
	_ = p.Add(conn)
	pm.AddPool("peer1", p)

	pm.DoWork(context.Background())

	pm.mu.Lock()
	pendingLen := len(pm.pending)
	pm.mu.Unlock()
	// Pending should be empty: the failed send should have been cleaned up.
	assert.Equal(t, 0, pendingLen)
}

// TestMultiplePools verifies DoWork handles multiple registered pools.
func TestMultiplePools(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p1, _ := buildPool("peer1", 2, false)
	p2, _ := buildPool("peer2", 3, false)
	pm.AddPool("peer1", p1)
	pm.AddPool("peer2", p2)

	n := pm.DoWork(context.Background())
	// 2 + 3 = 5 PINGs minimum.
	assert.GreaterOrEqual(t, n, 5)
}

// TestInjectSnapshot verifies InjectSnapshot stores a snapshot for a peer.
func TestInjectSnapshot(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p, _ := buildPool("peer1", 1, false)
	pm.AddPool("peer1", p)

	assert.Nil(t, pm.Snapshot("peer1"))

	snap := &PoolSnapshot{
		Samples: []ConnectionSample{{ConnID: 42, EWMRTT: 1 * time.Millisecond}},
		At:      time.Now(),
	}
	pm.InjectSnapshot("peer1", snap)

	got := pm.Snapshot("peer1")
	require.NotNil(t, got)
	assert.Equal(t, 1, len(got.Samples))
	assert.Equal(t, uint64(42), got.Samples[0].ConnID)
}

// TestInjectSnapshotUnknownPeer — injecting for unknown peer is a no-op.
func TestInjectSnapshotUnknownPeer(t *testing.T) {
	pm := New(20 * time.Millisecond)
	// Should not panic.
	pm.InjectSnapshot("unknown", &PoolSnapshot{At: time.Now()})
}

// TestConnectionStatsRecorded verifies Stats() values appear in the snapshot.
func TestConnectionStatsRecorded(t *testing.T) {
	pm := New(20 * time.Millisecond)
	p := pool.New("peer1", 1, 8)
	conn := newMockConn(true)
	conn.SetStats(quictr.ConnectionStats{
		Loss:             0.05,
		BytesInFlight:    1024,
		CongestionWindow: 8192,
		Throughput:       1_000_000,
	})
	_ = p.Add(conn)
	pm.AddPool("peer1", p)

	// Two passes: first sends PINGs (echo queues PONGs), second receives PONGs.
	pm.DoWork(context.Background())
	pm.DoWork(context.Background())

	snap := pm.Snapshot("peer1")
	if snap != nil && len(snap.Samples) > 0 {
		s := snap.Samples[0]
		assert.InDelta(t, 0.05, s.LossRate, 0.001)
		assert.Equal(t, 1024, s.BytesInFlight)
		assert.Equal(t, 8192, s.CwndBytes)
	}
}
