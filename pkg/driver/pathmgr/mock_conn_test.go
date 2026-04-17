package pathmgr

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/probes"
)

// mockConn implements quictr.Connection for testing.
type mockConn struct {
	mu         sync.Mutex
	id         uint64
	closed     bool
	lastPing   []byte // last PING received via SendProbe
	pongQueue  [][]byte
	stats      quictr.ConnectionStats
	sendErr    error
	recvErr    error
	// echoMode: if true, RecvProbe auto-echoes lastPing as PONG
	echoMode bool
}

var mockConnIDCounter atomic.Uint64

func newMockConn(echoMode bool) *mockConn {
	return &mockConn{
		id:       mockConnIDCounter.Add(1),
		echoMode: echoMode,
	}
}

func (m *mockConn) ID() uint64 {
	return m.id
}

func (m *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4433}
}

func (m *mockConn) Send(_ uint64, _ []byte) error { return nil }
func (m *mockConn) SendControl(_ []byte) error    { return nil }

// SendProbe stores the last PING and optionally prepares a PONG.
func (m *mockConn) SendProbe(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.lastPing = cp

	if m.echoMode && len(data) >= probes.PingFrameLen {
		ping, err := probes.DecodePing(data)
		if err == nil {
			pongBuf := make([]byte, probes.PongFrameLen)
			if encErr := probes.EncodePong(pongBuf, ping, time.Now()); encErr == nil {
				m.pongQueue = append(m.pongQueue, pongBuf)
			}
		}
	}
	return nil
}

func (m *mockConn) RecvData(_ context.Context) (uint64, []byte, error) {
	return 0, nil, nil
}

func (m *mockConn) RecvControl(_ context.Context) ([]byte, error) {
	return nil, nil
}

// RecvProbe returns the next queued PONG (non-blocking).
func (m *mockConn) RecvProbe(_ context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if len(m.pongQueue) == 0 {
		return nil, nil
	}
	pong := m.pongQueue[0]
	m.pongQueue = m.pongQueue[1:]
	return pong, nil
}

// EnqueuePong manually adds a PONG to the receive queue.
func (m *mockConn) EnqueuePong(pong []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(pong))
	copy(cp, pong)
	m.pongQueue = append(m.pongQueue, cp)
}

func (m *mockConn) RTT() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats.RTT
}

func (m *mockConn) Stats() quictr.ConnectionStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

func (m *mockConn) SetStats(s quictr.ConnectionStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats = s
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
