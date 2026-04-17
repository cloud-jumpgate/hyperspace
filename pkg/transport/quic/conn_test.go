package quictr_test

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestServer starts a QUIC server on 127.0.0.1:0 and returns the listener.
func startTestServer(t *testing.T) *quic.Listener {
	t.Helper()
	tlsConf := serverTLSConfig(t)

	udpConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err, "listen udp")

	ln, err := quic.Listen(udpConn, tlsConf, &quic.Config{
		MaxIncomingStreams:    4096,
		MaxIncomingUniStreams: 4096,
		KeepAlivePeriod:      5 * time.Second,
		Allow0RTT:            true,
	})
	require.NoError(t, err, "start quic listener")

	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// TestDial_ValidCert verifies a connection is established over loopback.
func TestDial_ValidCert(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err, "dial should succeed")
	require.NotNil(t, conn)

	assert.NotEqual(t, uint64(0), conn.ID(), "ID should be non-zero")
	assert.NotNil(t, conn.RemoteAddr())
	assert.False(t, conn.IsClosed())

	require.NoError(t, conn.Close())
	assert.True(t, conn.IsClosed())
}

// TestDial_TLS12Rejected verifies MinVersion enforcement rejects TLS 1.2 configs.
// Our Dial validates the TLS config before attempting the network connection.
func TestDial_TLS12Rejected(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	tls12Conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only
		NextProtos:         []string{"hyperspace/1"},
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS12,
	}
	_, err := quictr.Dial(context.Background(), addr, tls12Conf, "bbr")
	assert.Error(t, err, "TLS 1.2 config should be rejected by Dial validation")
}

// TestDial_NilTLSRejected verifies nil TLS config is rejected.
func TestDial_NilTLSRejected(t *testing.T) {
	_, err := quictr.Dial(context.Background(), "127.0.0.1:9999", nil, "bbr")
	assert.Error(t, err, "nil TLS config should be rejected")
}

// TestDial_EnforcesMinTLS13 verifies that Dial rejects configs with explicit
// MinVersion below TLS 1.3.
func TestDial_EnforcesMinTLS13(t *testing.T) {
	tests := []struct {
		name    string
		minVer  uint16
		wantErr bool
	}{
		{"tls12 explicit", tls.VersionTLS12, true},
		{"tls13 explicit", tls.VersionTLS13, false}, // ok
		{"zero (unset)", 0, false},                  // ok — Dial will set TLS 1.3
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.wantErr {
				// For valid configs, actually connect.
				ln := startTestServer(t)
				go func() { _, _ = ln.Accept(context.Background()) }()
				conf := &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // test-only
					NextProtos:         []string{"hyperspace/1"},
					MinVersion:         tc.minVer,
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				conn, err := quictr.Dial(ctx, ln.Addr().String(), conf, "bbr")
				require.NoError(t, err)
				_ = conn.Close()
				return
			}
			conf := &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // test-only
				NextProtos:         []string{"hyperspace/1"},
				MinVersion:         tc.minVer,
			}
			_, err := quictr.Dial(context.Background(), "127.0.0.1:1", conf, "bbr")
			assert.Error(t, err, "expected error for %s", tc.name)
		})
	}
}

// TestConnectionState_ReturnsHyperspaceALPN verifies that a valid connection
// negotiates the hyperspace/1 ALPN.
func TestConnectionState_ReturnsHyperspaceALPN(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	state := conn.ConnectionState()
	assert.Equal(t, "hyperspace/1", state.TLS.NegotiatedProtocol,
		"valid connection should negotiate hyperspace/1")
}

// TestSendControl_RecvControl verifies the control stream round-trip.
func TestSendControl_RecvControl(t *testing.T) {
	ln := startTestServer(t)

	// Server: accept connection and echo control data back.
	serverEchoed := make(chan struct{})
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC := quictr.Accept(conn)
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			data, err := serverQC.RecvControl(ctx)
			if err != nil || ctx.Err() != nil {
				return
			}
			if data != nil {
				_ = serverQC.SendControl(data)
				close(serverEchoed)
				// Keep connection alive until the test is done.
				<-ctx.Done()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	msg := []byte("control-frame-hello")
	err = client.SendControl(msg)
	require.NoError(t, err, "SendControl should succeed")

	// Wait for server to echo back.
	select {
	case <-serverEchoed:
	case <-time.After(5 * time.Second):
		t.Fatal("server never echoed control frame")
	}

	// Read echoed response.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []byte
	for {
		data, recvErr := client.RecvControl(ctx)
		if recvErr != nil {
			if got != nil {
				break
			}
			t.Fatalf("RecvControl error before data received: %v", recvErr)
		}
		if data != nil {
			got = data
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for control echo")
		case <-time.After(5 * time.Millisecond):
		}
	}
	assert.Equal(t, msg, got)
}

// TestSendProbe_RecvProbe verifies the probe stream round-trip.
func TestSendProbe_RecvProbe(t *testing.T) {
	ln := startTestServer(t)

	serverEchoed := make(chan struct{})
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC := quictr.Accept(conn)
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			data, err := serverQC.RecvProbe(ctx)
			if err != nil || ctx.Err() != nil {
				return
			}
			if data != nil {
				_ = serverQC.SendProbe(data)
				close(serverEchoed)
				<-ctx.Done()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	msg := []byte("probe-ping")
	err = client.SendProbe(msg)
	require.NoError(t, err, "SendProbe should succeed")

	select {
	case <-serverEchoed:
	case <-time.After(5 * time.Second):
		t.Fatal("server never echoed probe frame")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []byte
	for {
		data, recvErr := client.RecvProbe(ctx)
		if recvErr != nil {
			if got != nil {
				break
			}
			t.Fatalf("RecvProbe error: %v", recvErr)
		}
		if data != nil {
			got = data
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for probe echo")
		case <-time.After(5 * time.Millisecond):
		}
	}
	assert.Equal(t, msg, got)
}

// TestSend_RecvData verifies data is received on the server from the client.
func TestSend_RecvData(t *testing.T) {
	ln := startTestServer(t)

	serverGot := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC := quictr.Accept(conn)
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			_, data, err := serverQC.RecvData(ctx)
			if err != nil || ctx.Err() != nil {
				return
			}
			if data != nil {
				serverGot <- data
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Give server time to accept connection and start goroutines.
	time.Sleep(50 * time.Millisecond)

	msg := []byte("data-payload")
	err = client.Send(2, msg)
	require.NoError(t, err, "Send should succeed")

	select {
	case got := <-serverGot:
		assert.Equal(t, msg, got)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server to receive data")
	}
}

// TestRecvData_NonBlocking verifies RecvData returns (0, nil, nil) when nothing available.
func TestRecvData_NonBlocking(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	sid, data, err := client.RecvData(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, data, "no data available should return nil")
	assert.Equal(t, uint64(0), sid)
}

// TestSend_InvalidStreamID verifies stream IDs < 2 are rejected.
func TestSend_InvalidStreamID(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	err = client.Send(0, []byte("bad"))
	assert.Error(t, err, "stream ID 0 should be rejected for Send")

	err = client.Send(1, []byte("bad"))
	assert.Error(t, err, "stream ID 1 should be rejected for Send")
}

// TestIsClosed_AfterClose verifies IsClosed returns true after Close.
func TestIsClosed_AfterClose(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)

	assert.False(t, conn.IsClosed())
	require.NoError(t, conn.Close())
	assert.True(t, conn.IsClosed())

	// Second close should not panic.
	_ = conn.Close()
}

// TestRTT verifies RTT returns a non-negative duration after connection.
func TestRTT(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send something to trigger RTT measurement.
	_ = conn.SendControl([]byte("ping"))
	time.Sleep(50 * time.Millisecond)

	rtt := conn.RTT()
	assert.GreaterOrEqual(t, rtt, time.Duration(0), "RTT should be non-negative")
}

// TestStats verifies Stats returns valid fields.
func TestStats(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	stats := conn.Stats()
	assert.GreaterOrEqual(t, stats.Loss, 0.0)
	assert.LessOrEqual(t, stats.Loss, 1.0)
}

// TestAccept wraps an incoming connection correctly.
func TestAccept(t *testing.T) {
	ln := startTestServer(t)

	connCh := make(chan *quictr.QUICConnection, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err == nil {
			connCh <- quictr.Accept(conn)
		}
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	select {
	case serverConn := <-connCh:
		assert.NotNil(t, serverConn)
		assert.False(t, serverConn.IsClosed())
		_ = serverConn.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server to accept connection")
	}
}

// TestSendControl_OnClosedConn verifies SendControl returns ErrConnectionClosed.
func TestSendControl_OnClosedConn(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	err = conn.SendControl([]byte("x"))
	assert.ErrorIs(t, err, quictr.ErrConnectionClosed)
}

// TestSendProbe_OnClosedConn verifies SendProbe returns ErrConnectionClosed.
func TestSendProbe_OnClosedConn(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	err = conn.SendProbe([]byte("x"))
	assert.ErrorIs(t, err, quictr.ErrConnectionClosed)
}

// TestSend_OnClosedConn verifies Send returns ErrConnectionClosed.
func TestSend_OnClosedConn(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	err = conn.Send(2, []byte("x"))
	assert.ErrorIs(t, err, quictr.ErrConnectionClosed)
}

// TestRecvControl_OnClosedConn verifies RecvControl returns ErrConnectionClosed.
func TestRecvControl_OnClosedConn(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = conn.RecvControl(context.Background())
	assert.ErrorIs(t, err, quictr.ErrConnectionClosed)
}

// TestRecvProbe_OnClosedConn verifies RecvProbe returns ErrConnectionClosed.
func TestRecvProbe_OnClosedConn(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = conn.RecvProbe(context.Background())
	assert.ErrorIs(t, err, quictr.ErrConnectionClosed)
}

// TestRecvData_OnClosedConn verifies RecvData returns ErrConnectionClosed.
func TestRecvData_OnClosedConn(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, _, err = conn.RecvData(context.Background())
	assert.ErrorIs(t, err, quictr.ErrConnectionClosed)
}

// TestDial_EnforcesALPN verifies that Dial adds hyperspace/1 ALPN when not present.
// The connection negotiates hyperspace/1 even when the client config omits it.
func TestDial_EnforcesALPN(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	// Config without NextProtos set — Dial should inject hyperspace/1.
	conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only
		// NextProtos intentionally omitted
		MinVersion: tls.VersionTLS13,
	}
	conn, err := quictr.Dial(context.Background(), addr, conf, "bbr")
	require.NoError(t, err, "Dial should succeed and inject ALPN")
	defer func() { _ = conn.Close() }()

	state := conn.ConnectionState()
	assert.Equal(t, "hyperspace/1", state.TLS.NegotiatedProtocol,
		"Dial should inject hyperspace/1 ALPN when not present in config")
}

// TestStats_LossComputed verifies Stats returns valid loss values.
// Loss = 0 when no packets sent; loss in [0, 1] range always.
func TestStats_Fields(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	addr := ln.Addr().String()
	conn, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send some data to generate stats.
	_ = conn.SendControl([]byte("stats-test"))
	time.Sleep(20 * time.Millisecond)

	stats := conn.Stats()
	assert.GreaterOrEqual(t, stats.Loss, 0.0, "loss must be >= 0")
	assert.LessOrEqual(t, stats.Loss, 1.0, "loss must be <= 1")
	assert.GreaterOrEqual(t, stats.RTT, time.Duration(0), "RTT must be >= 0")
}

// TestSend_MultipleStreamIDs verifies multiple sends on different stream IDs
// are handled independently (exercises the dataOut map path for known streams).
func TestSend_MultipleStreamIDs(t *testing.T) {
	ln := startTestServer(t)

	// Collect all data bytes received — stream-2 and stream-3 are separate
	// unidirectional streams, so server receives them separately.
	serverGot := make(chan []byte, 10)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC := quictr.Accept(conn)
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		count := 0
		for count < 2 { // two distinct streams: 2 and 3
			_, data, err := serverQC.RecvData(ctx)
			if err != nil || ctx.Err() != nil {
				return
			}
			if data != nil {
				serverGot <- data
				count++
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	time.Sleep(50 * time.Millisecond)

	// Send on two different stream IDs — these open two separate unidirectional streams.
	require.NoError(t, client.Send(2, []byte("stream-2")))
	require.NoError(t, client.Send(3, []byte("stream-3")))
	// Send again on stream 2 — reuses the cached stream (covers the !ok path in dataOut map).
	require.NoError(t, client.Send(2, []byte("stream-2-continued")))

	received := make([][]byte, 0, 2)
	timeout := time.After(10 * time.Second)
	for len(received) < 2 {
		select {
		case d := <-serverGot:
			received = append(received, d)
		case <-timeout:
			t.Fatalf("timed out waiting, got %d/2 messages", len(received))
		}
	}
	assert.Len(t, received, 2)
}
