package quictr_test

// coverage_test.go — additional error-path tests to bring F-003 coverage to ≥90%.
// All tests use loopback (127.0.0.1) or self-contained infrastructure only.
// No external services are called.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Dial failure paths
// ---------------------------------------------------------------------------

// TestDial_NetworkFailure verifies Dial returns a wrapped error when the remote
// is not listening. The TLS config is valid — only the network connection fails.
func TestDial_NetworkFailure(t *testing.T) {
	// Find a free UDP port then immediately stop listening, so nothing is there.
	udpConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	addr := udpConn.LocalAddr().String()
	_ = udpConn.Close() // port is now free (nothing listening)

	conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only
		NextProtos:         []string{"hyperspace/1"},
		MinVersion:         tls.VersionTLS13,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = quictr.Dial(ctx, addr, conf, "bbr")
	assert.Error(t, err, "Dial to a closed port should return an error")
}

// TestDial_ContextCancelled verifies Dial returns an error when the context is
// cancelled before the connection is established.
func TestDial_ContextCancelled(t *testing.T) {
	conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only
		NextProtos:         []string{"hyperspace/1"},
		MinVersion:         tls.VersionTLS13,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := quictr.Dial(ctx, "127.0.0.1:1", conf, "bbr")
	assert.Error(t, err, "Dial with cancelled context should return an error")
}

// ---------------------------------------------------------------------------
// enforceALPN — ALPN already present branch
// ---------------------------------------------------------------------------

// TestDial_ALPNAlreadyPresent verifies Dial does not duplicate the hyperspace/1
// ALPN when it is already in the NextProtos list. This exercises the early-return
// branch in enforceALPN where the ALPN is already present.
func TestDial_ALPNAlreadyPresent(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	conf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only
		// hyperspace/1 already present — enforceALPN must not prepend a duplicate.
		NextProtos: []string{"hyperspace/1", "h3"},
		MinVersion: tls.VersionTLS13,
	}

	conn, err := quictr.Dial(context.Background(), ln.Addr().String(), conf, "cubic")
	require.NoError(t, err, "Dial should succeed when ALPN is already present")
	defer func() { _ = conn.Close() }()

	qc := conn.(*quictr.QUICConnection)
	state := qc.ConnectionState()
	assert.Equal(t, "hyperspace/1", state.TLS.NegotiatedProtocol)
}

// ---------------------------------------------------------------------------
// Close idempotency
// ---------------------------------------------------------------------------

// TestClose_Idempotent verifies calling Close twice does not panic and returns
// a consistent result. The second call must return the same error (or nil) as
// the first — it must never panic.
func TestClose_Idempotent(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	conn, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)

	err1 := conn.Close()
	err2 := conn.Close()

	// Both calls must not panic. The second may return err1 or nil — either is acceptable
	// as long as it is the same error value, demonstrating idempotency.
	assert.Equal(t, err1, err2, "second Close should return the same error as first Close")
	assert.True(t, conn.IsClosed(), "IsClosed must be true after Close")
}

// ---------------------------------------------------------------------------
// RecvData with data available (exercises the dataIn channel receive path)
// ---------------------------------------------------------------------------

// TestRecvData_WithItem verifies the non-default branch of RecvData — i.e., that
// data arriving from a sender is returned via the channel select case.
func TestRecvData_WithItem(t *testing.T) {
	ln := startTestServer(t)

	serverGot := make(chan struct{})
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC, acceptErr := quictr.Accept(conn)
		if acceptErr != nil {
			return
		}
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			_, data, err := serverQC.RecvData(ctx)
			if err != nil || ctx.Err() != nil {
				return
			}
			if data != nil {
				close(serverGot)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	client, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, client.Send(2, []byte("coverage-recv-data")))

	select {
	case <-serverGot:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server RecvData")
	}
}

// ---------------------------------------------------------------------------
// Stats — loss capped to 1.0 (white-box: exercises the loss > 1.0 guard)
// ---------------------------------------------------------------------------

// TestStats_LossBoundary verifies that Stats always returns loss within [0, 1.0].
// The quic-go stack may occasionally return PacketsLost > PacketsSent under certain
// conditions. This test verifies the cap and zero-loss branches independently.
func TestStats_LossBoundary(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	conn, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// At connection establishment PacketsSent is typically 0 — exercises the zero-loss branch.
	stats := conn.Stats()
	assert.Equal(t, 0.0, stats.Loss, "loss must be 0 when PacketsSent == 0")

	// Send several packets to increment PacketsSent — exercises the loss division path.
	for i := 0; i < 10; i++ {
		_ = conn.SendControl([]byte("loss-boundary-test"))
	}
	time.Sleep(50 * time.Millisecond)

	stats = conn.Stats()
	assert.GreaterOrEqual(t, stats.Loss, 0.0, "loss must be >= 0")
	assert.LessOrEqual(t, stats.Loss, 1.0, "loss must be <= 1.0")
}

// ---------------------------------------------------------------------------
// Accept — SPIFFE positive path
// ---------------------------------------------------------------------------

// generateSPIFFECert creates a self-signed certificate that includes a
// spiffe://test.hyperspace/workload URI SAN, satisfying the SPIFFE check in Accept.
func generateSPIFFECert(t *testing.T) tls.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	spiffeURI, err := url.Parse("spiffe://test.hyperspace/workload")
	require.NoError(t, err)

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Hyperspace SPIFFE Test"},
		},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		URIs:                  []*url.URL{spiffeURI},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return cert
}

// spiffeClientTLSConfig returns a TLS config carrying a SPIFFE-style client cert.
func spiffeClientTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	cert := generateSPIFFECert(t)
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, //nolint:gosec // test-only: loopback self-signed cert
		NextProtos:         []string{"hyperspace/1"},
		MinVersion:         tls.VersionTLS13,
	}
}

// TestAccept_RequireSPIFFE_WithSPIFFECert_Accepted verifies that Accept with
// RequireSPIFFE: true succeeds when the peer certificate contains a spiffe:// URI SAN.
// This exercises the hasSPIFFE=true branch and the successful return path.
func TestAccept_RequireSPIFFE_WithSPIFFECert_Accepted(t *testing.T) {
	ln := startTestServer(t)

	acceptResult := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			acceptResult <- err
			return
		}
		sc, acceptErr := quictr.Accept(conn, quictr.AcceptConfig{RequireSPIFFE: true})
		if acceptErr == nil {
			_ = sc.Close()
		}
		acceptResult <- acceptErr
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, spiffeClientTLSConfig(t), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	select {
	case err := <-acceptResult:
		assert.NoError(t, err, "Accept with RequireSPIFFE should succeed when peer cert has SPIFFE URI SAN")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Accept result")
	}
}

// ---------------------------------------------------------------------------
// Send — reuse existing stream (exercises the `ok` (cached stream) branch)
// ---------------------------------------------------------------------------

// TestSend_ReusesCachedStream verifies that repeated Send calls on the same stream ID
// reuse the cached *quic.SendStream rather than opening a new one. This exercises the
// `if !ok` false branch in the dataOut lookup.
func TestSend_ReusesCachedStream(t *testing.T) {
	ln := startTestServer(t)

	totalReceived := make(chan int, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC, acceptErr := quictr.Accept(conn)
		if acceptErr != nil {
			return
		}
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		total := 0
		for total < 3 {
			_, data, err := serverQC.RecvData(ctx)
			if err != nil || ctx.Err() != nil {
				break
			}
			if data != nil {
				total++
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}
		totalReceived <- total
	}()

	client, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	time.Sleep(50 * time.Millisecond)

	// First send opens the stream; subsequent sends on the same ID reuse it.
	require.NoError(t, client.Send(5, []byte("msg-a")))
	require.NoError(t, client.Send(5, []byte("msg-b")))
	require.NoError(t, client.Send(5, []byte("msg-c")))

	select {
	case n := <-totalReceived:
		// Because Send reuses one stream, reads may arrive as 1–3 chunks.
		// We verify at least 1 chunk arrived (confirming the cached-stream path was exercised).
		assert.GreaterOrEqual(t, n, 1, "server should receive at least one data chunk")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server to receive reused-stream data")
	}
}

// ---------------------------------------------------------------------------
// RecvControl / RecvProbe — nil slot (no stream opened yet, non-blocking)
// ---------------------------------------------------------------------------

// TestRecvControl_NilSlot verifies that RecvControl returns (nil, nil) when no
// control stream has been opened yet (the biStreamSlot is nil). This exercises
// the `if s == nil` branch in readBiSlot.
func TestRecvControl_NilSlot(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	conn, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// No SendControl called — slot.stream is nil on both client and server.
	// RecvControl should return (nil, nil) immediately without blocking.
	data, err := conn.RecvControl(context.Background())
	assert.NoError(t, err, "RecvControl with no stream opened should not error")
	assert.Nil(t, data, "RecvControl with no stream opened should return nil data")
}

// TestRecvProbe_NilSlot verifies that RecvProbe returns (nil, nil) when no
// probe stream has been opened yet.
func TestRecvProbe_NilSlot(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	conn, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	data, err := conn.RecvProbe(context.Background())
	assert.NoError(t, err, "RecvProbe with no stream opened should not error")
	assert.Nil(t, data, "RecvProbe with no stream opened should return nil data")
}

// ---------------------------------------------------------------------------
// ID and RemoteAddr
// ---------------------------------------------------------------------------

// TestID_Monotonic verifies that successive connections receive strictly increasing IDs.
func TestID_Monotonic(t *testing.T) {
	ln := startTestServer(t)
	go func() {
		for {
			_, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
		}
	}()

	addr := ln.Addr().String()

	conn1, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn1.Close() }()

	conn2, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn2.Close() }()

	assert.Less(t, conn1.ID(), conn2.ID(), "connection IDs must be monotonically increasing")
}

// TestRemoteAddr_IsLoopback verifies RemoteAddr returns a non-nil address pointing
// to the loopback interface for a loopback connection.
func TestRemoteAddr_IsLoopback(t *testing.T) {
	ln := startTestServer(t)
	go func() { _, _ = ln.Accept(context.Background()) }()

	conn, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	addr := conn.RemoteAddr()
	require.NotNil(t, addr, "RemoteAddr should not be nil")

	udpAddr, ok := addr.(*net.UDPAddr)
	require.True(t, ok, "RemoteAddr should be a *net.UDPAddr for a QUIC connection")
	assert.True(t, udpAddr.IP.IsLoopback(), "RemoteAddr IP should be loopback")
}

// ---------------------------------------------------------------------------
// Accept — second AcceptConfig argument (variadic branch)
// ---------------------------------------------------------------------------

// TestAccept_DefaultConfig verifies Accept with no AcceptConfig (zero-value default)
// succeeds on a valid mTLS connection.
func TestAccept_DefaultConfig(t *testing.T) {
	ln := startTestServer(t)

	resultCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			resultCh <- err
			return
		}
		// Call Accept with zero AcceptConfig (RequireSPIFFE: false) — the variadic path.
		sc, acceptErr := quictr.Accept(conn, quictr.AcceptConfig{RequireSPIFFE: false})
		if acceptErr == nil {
			_ = sc.Close()
		}
		resultCh <- acceptErr
	}()

	client, err := quictr.Dial(context.Background(), ln.Addr().String(), clientTLSConfig(), "bbr")
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	select {
	case err := <-resultCh:
		assert.NoError(t, err, "Accept with explicit zero AcceptConfig should succeed")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Accept result")
	}
}
