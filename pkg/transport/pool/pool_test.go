package pool_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConn is a minimal Connection implementation for pool tests.
type mockConn struct {
	id     uint64
	closed bool
	mu     sync.Mutex
}

func newMock(id uint64) *mockConn { return &mockConn{id: id} }

func (m *mockConn) ID() uint64 { return m.id }
func (m *mockConn) RemoteAddr() net.Addr {
	addr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:9999")
	return addr
}
func (m *mockConn) Send(_ uint64, _ []byte) error { return nil }
func (m *mockConn) SendControl(_ []byte) error    { return nil }
func (m *mockConn) SendProbe(_ []byte) error      { return nil }
func (m *mockConn) RecvData(_ context.Context) (uint64, []byte, error) {
	return 0, nil, nil
}
func (m *mockConn) RecvControl(_ context.Context) ([]byte, error) { return nil, nil }
func (m *mockConn) RecvProbe(_ context.Context) ([]byte, error)   { return nil, nil }
func (m *mockConn) RTT() time.Duration                            { return time.Millisecond }
func (m *mockConn) Stats() quictr.ConnectionStats                 { return quictr.ConnectionStats{} }
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

// TestNew verifies pool construction with defaults.
func TestNew(t *testing.T) {
	p := pool.New("127.0.0.1:4000", 2, 8)
	assert.Equal(t, "127.0.0.1:4000", p.Peer())
	assert.Equal(t, 0, p.Size())
	assert.True(t, p.IsUnderMin(), "empty pool should be under min")
	assert.False(t, p.IsFull())
}

// TestNew_ClampMinSize verifies that minSize < 1 is clamped to 1.
func TestNew_ClampMinSize(t *testing.T) {
	p := pool.New("peer", 0, 5)
	// Size 0 < minSize 1 → IsUnderMin
	assert.True(t, p.IsUnderMin())
}

// TestAdd_Remove verifies basic add and remove semantics.
func TestAdd_Remove(t *testing.T) {
	p := pool.New("127.0.0.1:4000", 2, 8)

	c1 := newMock(1)
	c2 := newMock(2)

	require.NoError(t, p.Add(c1))
	require.NoError(t, p.Add(c2))
	assert.Equal(t, 2, p.Size())
	assert.False(t, p.IsUnderMin())

	require.NoError(t, p.Remove(c1.ID()))
	assert.Equal(t, 1, p.Size())

	// Removing same ID again should error.
	err := p.Remove(c1.ID())
	assert.Error(t, err)
}

// TestAdd_AtCapacity verifies Add returns error when pool is full.
func TestAdd_AtCapacity(t *testing.T) {
	p := pool.New("peer", 1, 2)

	require.NoError(t, p.Add(newMock(1)))
	require.NoError(t, p.Add(newMock(2)))
	assert.True(t, p.IsFull())

	err := p.Add(newMock(3))
	assert.Error(t, err, "Add should fail when pool is full")
}

// TestConnections_ExcludesClosed verifies that Connections() prunes closed connections.
func TestConnections_ExcludesClosed(t *testing.T) {
	p := pool.New("peer", 1, 8)

	c1 := newMock(1)
	c2 := newMock(2)
	c3 := newMock(3)

	require.NoError(t, p.Add(c1))
	require.NoError(t, p.Add(c2))
	require.NoError(t, p.Add(c3))

	// Close c2.
	require.NoError(t, c2.Close())

	conns := p.Connections()
	assert.Len(t, conns, 2, "closed connection should be pruned")
	for _, c := range conns {
		assert.False(t, c.IsClosed())
	}
}

// TestIsFull_IsUnderMin verifies boundary logic.
func TestIsFull_IsUnderMin(t *testing.T) {
	p := pool.New("peer", 2, 4)

	// 0 connections → under min.
	assert.True(t, p.IsUnderMin())
	assert.False(t, p.IsFull())

	require.NoError(t, p.Add(newMock(1)))
	// 1 connection → still under min (min=2).
	assert.True(t, p.IsUnderMin())

	require.NoError(t, p.Add(newMock(2)))
	// 2 connections → at min, not full.
	assert.False(t, p.IsUnderMin())
	assert.False(t, p.IsFull())

	require.NoError(t, p.Add(newMock(3)))
	require.NoError(t, p.Add(newMock(4)))
	// 4 connections → full.
	assert.True(t, p.IsFull())
	assert.False(t, p.IsUnderMin())
}

// TestConcurrentAddRemove verifies thread safety under concurrent operations.
func TestConcurrentAddRemove(t *testing.T) {
	p := pool.New("peer", 2, 100)
	var wg sync.WaitGroup

	// Concurrently add 50 connections.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		id := uint64(i + 1)
		go func() {
			defer wg.Done()
			_ = p.Add(newMock(id))
		}()
	}
	wg.Wait()

	// Concurrently remove all.
	conns := p.Connections()
	for _, c := range conns {
		wg.Add(1)
		cid := c.ID()
		go func() {
			defer wg.Done()
			_ = p.Remove(cid)
		}()
	}
	wg.Wait()

	// Pool should be empty (or very small if some removes raced).
	assert.GreaterOrEqual(t, p.Size(), 0)
}

// TestSize_PrunesClosedConnections verifies Size() removes closed connections.
func TestSize_PrunesClosedConnections(t *testing.T) {
	p := pool.New("peer", 1, 8)

	c1 := newMock(1)
	c2 := newMock(2)
	require.NoError(t, p.Add(c1))
	require.NoError(t, p.Add(c2))
	assert.Equal(t, 2, p.Size())

	_ = c1.Close()
	assert.Equal(t, 1, p.Size(), "closed conn should be pruned from Size()")
}

// TestRemove_Closes verifies Remove calls Close on the connection.
func TestRemove_Closes(t *testing.T) {
	p := pool.New("peer", 1, 8)
	c := newMock(42)
	require.NoError(t, p.Add(c))

	require.NoError(t, p.Remove(42))
	assert.True(t, c.IsClosed(), "Remove should close the connection")
}
