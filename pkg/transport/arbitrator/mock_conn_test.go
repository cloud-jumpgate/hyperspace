package arbitrator_test

import (
	"context"
	"net"
	"time"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// mockConn is a test double for quictr.Connection with configurable stats.
type mockConn struct {
	id       uint64
	rtt      time.Duration
	loss     float64
	cwnd     int
	inflight int
	closed   bool
}

func newMockConn(id uint64, rtt time.Duration, loss float64, cwnd, inflight int) *mockConn {
	return &mockConn{
		id:       id,
		rtt:      rtt,
		loss:     loss,
		cwnd:     cwnd,
		inflight: inflight,
	}
}

// Implement quictr.Connection interface.

func (m *mockConn) ID() uint64 { return m.id }
func (m *mockConn) RemoteAddr() net.Addr {
	a, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:9999")
	return a
}
func (m *mockConn) Send(_ uint64, _ []byte) error             { return nil }
func (m *mockConn) SendControl(_ []byte) error                { return nil }
func (m *mockConn) SendProbe(_ []byte) error                  { return nil }
func (m *mockConn) RecvData(_ context.Context) (uint64, []byte, error) {
	return 0, nil, nil
}
func (m *mockConn) RecvControl(_ context.Context) ([]byte, error) { return nil, nil }
func (m *mockConn) RecvProbe(_ context.Context) ([]byte, error)   { return nil, nil }
func (m *mockConn) RTT() time.Duration                            { return m.rtt }
func (m *mockConn) Stats() quictr.ConnectionStats {
	return quictr.ConnectionStats{
		RTT:              m.rtt,
		Loss:             m.loss,
		CongestionWindow: m.cwnd,
		BytesInFlight:    m.inflight,
	}
}
func (m *mockConn) Close() error  { m.closed = true; return nil }
func (m *mockConn) IsClosed() bool { return m.closed }
