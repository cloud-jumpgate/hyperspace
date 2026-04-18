// Package quictr provides Hyperspace's QUIC connection abstraction.
// Package name "quictr" avoids collision with quic-go's "quic" package name.
package quictr

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// ALPN protocol identifier for Hyperspace.
const alpnHyperspace = "hyperspace/1"

// ErrBackPressure is returned when a stream buffer is full.
var ErrBackPressure = errors.New("quictr: back pressure — stream buffer full")

// ErrConnectionClosed is returned when attempting to use a closed connection.
var ErrConnectionClosed = errors.New("quictr: connection is closed")

// ErrWrongALPN is returned when the peer does not speak hyperspace/1.
var ErrWrongALPN = errors.New("quictr: peer does not support hyperspace/1 ALPN")

// nextConnID is a process-level atomic counter for connection IDs.
var nextConnID atomic.Uint64

// ConnectionStats holds current connection statistics.
type ConnectionStats struct {
	RTT              time.Duration
	Loss             float64 // 0.0–1.0
	Throughput       int64   // bytes/second
	BytesInFlight    int
	CongestionWindow int
}

// Connection is the Hyperspace abstraction over a single QUIC connection.
// One pool may hold 2–8 of these between two peers.
type Connection interface {
	// ID returns a unique identifier for this connection within a pool.
	ID() uint64
	// RemoteAddr returns the peer's address.
	RemoteAddr() net.Addr
	// Send writes bytes to the given data stream (stream ID >= 2).
	// Never blocks; returns ErrBackPressure if the stream buffer is full.
	Send(streamID uint64, data []byte) error
	// SendControl writes bytes to the control stream (stream 0).
	SendControl(data []byte) error
	// SendProbe writes bytes to the probe stream (stream 1).
	SendProbe(data []byte) error
	// RecvData returns the next available data from data streams.
	// Returns (streamID, data, error). Non-blocking; returns nil data if nothing available.
	RecvData(ctx context.Context) (uint64, []byte, error)
	// RecvControl returns the next control frame. Non-blocking.
	RecvControl(ctx context.Context) ([]byte, error)
	// RecvProbe returns the next probe frame. Non-blocking.
	RecvProbe(ctx context.Context) ([]byte, error)
	// RTT returns the latest smoothed RTT from the QUIC stack.
	RTT() time.Duration
	// Stats returns the current connection statistics.
	Stats() ConnectionStats
	// Close closes the connection gracefully.
	Close() error
	// IsClosed returns true if the connection has been closed.
	IsClosed() bool
}

// biStreamSlot caches an open bidirectional stream pointer.
type biStreamSlot struct {
	mu     sync.Mutex
	stream *quic.Stream // nil until opened
}

// QUICConnection implements Connection over quic-go.
type QUICConnection struct {
	id   uint64
	conn *quic.Conn

	// control and probe slots (streams 0 and 1) — opened lazily.
	control biStreamSlot
	probe   biStreamSlot

	// data streams keyed by Hyperspace stream ID (>= 2).
	dataMu  sync.Mutex
	dataOut map[uint64]*quic.SendStream

	// incoming data channel — goroutine drains AcceptUniStream.
	dataIn chan incomingData

	// closed state.
	closedMu sync.Mutex
	closed   bool
	closeErr error

	// context for background goroutines.
	ctx    context.Context
	cancel context.CancelFunc
}

type incomingData struct {
	streamID uint64
	data     []byte
	err      error
}

// quicConfig returns the standard QUIC transport config for Hyperspace.
func quicConfig() *quic.Config {
	return &quic.Config{
		MaxIncomingStreams:    4096,
		MaxIncomingUniStreams: 4096,
		KeepAlivePeriod:       5 * time.Second,
		Allow0RTT:             true,
	}
}

// validateTLSConfig enforces Hyperspace TLS requirements.
func validateTLSConfig(tlsConf *tls.Config) error {
	if tlsConf == nil {
		return fmt.Errorf("quictr: tls.Config must not be nil")
	}
	if tlsConf.MinVersion != 0 && tlsConf.MinVersion < tls.VersionTLS13 {
		return fmt.Errorf("quictr: MinVersion must be tls.VersionTLS13 or unset, got 0x%04x", tlsConf.MinVersion)
	}
	return nil
}

// enforceALPN ensures the TLS config has the Hyperspace ALPN and TLS 1.3 minimum.
func enforceALPN(tlsConf *tls.Config) *tls.Config {
	clone := tlsConf.Clone()
	clone.MinVersion = tls.VersionTLS13
	for _, p := range clone.NextProtos {
		if p == alpnHyperspace {
			return clone
		}
	}
	clone.NextProtos = append([]string{alpnHyperspace}, clone.NextProtos...)
	return clone
}

// Dial opens a new outgoing QUIC connection to addr using the given TLS config.
// ccName determines which congestion controller to use (currently informational;
// future: passed to cc.New for custom CC integration).
func Dial(ctx context.Context, addr string, tlsConf *tls.Config, ccName string) (Connection, error) {
	if err := validateTLSConfig(tlsConf); err != nil {
		return nil, err
	}
	cfg := enforceALPN(tlsConf)

	conn, err := quic.DialAddr(ctx, addr, cfg, quicConfig())
	if err != nil {
		return nil, fmt.Errorf("quictr: dial %s: %w", addr, err)
	}
	return newQUICConnection(conn), nil
}

// Accept wraps an incoming quic-go connection.
func Accept(conn *quic.Conn) Connection {
	return newQUICConnection(conn)
}

// newQUICConnection initialises a QUICConnection and starts background goroutines.
func newQUICConnection(conn *quic.Conn) *QUICConnection {
	ctx, cancel := context.WithCancel(context.Background())
	qc := &QUICConnection{
		id:      nextConnID.Add(1),
		conn:    conn,
		dataOut: make(map[uint64]*quic.SendStream),
		dataIn:  make(chan incomingData, 256),
		ctx:     ctx,
		cancel:  cancel,
	}
	go qc.acceptUniStreams()
	return qc
}

// acceptUniStreams drains incoming unidirectional streams (data plane, stream ID >= 2).
func (qc *QUICConnection) acceptUniStreams() {
	for {
		stream, err := qc.conn.AcceptUniStream(qc.ctx)
		if err != nil {
			return
		}
		go qc.readUniStream(stream)
	}
}

// readUniStream reads all data from a unidirectional stream and pushes to dataIn.
func (qc *QUICConnection) readUniStream(stream *quic.ReceiveStream) {
	sid := uint64(stream.StreamID())
	buf := make([]byte, 65536)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case qc.dataIn <- incomingData{streamID: sid, data: data}:
			case <-qc.ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// ID returns the connection's unique ID.
func (qc *QUICConnection) ID() uint64 {
	return qc.id
}

// RemoteAddr returns the peer's network address.
func (qc *QUICConnection) RemoteAddr() net.Addr {
	return qc.conn.RemoteAddr()
}

// getOrOpenBiStream returns the opened bidirectional stream for the given slot,
// opening it if not yet open.
func (qc *QUICConnection) getOrOpenBiStream(slot *biStreamSlot) (*quic.Stream, error) {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.stream != nil {
		return slot.stream, nil
	}
	stream, err := qc.conn.OpenStreamSync(qc.ctx)
	if err != nil {
		return nil, fmt.Errorf("quictr: open stream: %w", err)
	}
	slot.stream = stream
	return slot.stream, nil
}

// SendControl writes bytes to the control stream (stream 0).
func (qc *QUICConnection) SendControl(data []byte) error {
	if qc.IsClosed() {
		return ErrConnectionClosed
	}
	stream, err := qc.getOrOpenBiStream(&qc.control)
	if err != nil {
		return err
	}
	_, err = stream.Write(data)
	return err
}

// SendProbe writes bytes to the probe stream (stream 1).
func (qc *QUICConnection) SendProbe(data []byte) error {
	if qc.IsClosed() {
		return ErrConnectionClosed
	}
	stream, err := qc.getOrOpenBiStream(&qc.probe)
	if err != nil {
		return err
	}
	_, err = stream.Write(data)
	return err
}

// Send writes bytes to a data stream (stream ID >= 2).
// Opens a new unidirectional SendStream if one doesn't exist for the given ID.
func (qc *QUICConnection) Send(streamID uint64, data []byte) error {
	if qc.IsClosed() {
		return ErrConnectionClosed
	}
	if streamID < 2 {
		return fmt.Errorf("quictr: data stream ID must be >= 2, got %d", streamID)
	}

	qc.dataMu.Lock()
	s, ok := qc.dataOut[streamID]
	qc.dataMu.Unlock()

	if !ok {
		ns, err := qc.conn.OpenUniStreamSync(qc.ctx)
		if err != nil {
			return fmt.Errorf("quictr: open uni stream: %w", err)
		}
		s = ns
		qc.dataMu.Lock()
		qc.dataOut[streamID] = s
		qc.dataMu.Unlock()
	}

	_, err := s.Write(data)
	if err != nil {
		return fmt.Errorf("quictr: send on stream %d: %w", streamID, err)
	}
	return nil
}

// readBiSlot reads from a bidirectional stream slot with a short deadline for non-blocking poll.
func readBiSlot(slot *biStreamSlot) ([]byte, error) {
	slot.mu.Lock()
	s := slot.stream
	slot.mu.Unlock()
	if s == nil {
		return nil, nil
	}

	// Set a short deadline for non-blocking poll.
	if err := s.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
		return nil, err
	}
	defer func() { _ = s.SetReadDeadline(time.Time{}) }()

	buf := make([]byte, 65536)
	n, err := s.Read(buf)
	if n > 0 {
		data := make([]byte, n)
		copy(data, buf[:n])
		return data, nil
	}
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, nil
		}
		return nil, err
	}
	return nil, nil
}

// tryAcceptStream attempts to accept a stream from the peer within a very short timeout.
func (qc *QUICConnection) tryAcceptStream(slot *biStreamSlot) {
	slot.mu.Lock()
	if slot.stream != nil {
		slot.mu.Unlock()
		return
	}
	slot.mu.Unlock()

	timeoutCtx, cancel := context.WithTimeout(qc.ctx, time.Millisecond)
	defer cancel()
	stream, err := qc.conn.AcceptStream(timeoutCtx)
	if err == nil {
		slot.mu.Lock()
		if slot.stream == nil {
			slot.stream = stream
		}
		slot.mu.Unlock()
	}
}

// RecvControl returns the next control frame. Non-blocking.
func (qc *QUICConnection) RecvControl(ctx context.Context) ([]byte, error) {
	if qc.IsClosed() {
		return nil, ErrConnectionClosed
	}
	qc.tryAcceptStream(&qc.control)
	return readBiSlot(&qc.control)
}

// RecvProbe returns the next probe frame. Non-blocking.
func (qc *QUICConnection) RecvProbe(ctx context.Context) ([]byte, error) {
	if qc.IsClosed() {
		return nil, ErrConnectionClosed
	}
	qc.tryAcceptStream(&qc.probe)
	return readBiSlot(&qc.probe)
}

// RecvData returns the next available data from data streams. Non-blocking.
func (qc *QUICConnection) RecvData(ctx context.Context) (uint64, []byte, error) {
	if qc.IsClosed() {
		return 0, nil, ErrConnectionClosed
	}
	select {
	case item := <-qc.dataIn:
		return item.streamID, item.data, item.err
	default:
		return 0, nil, nil
	}
}

// RTT returns the latest smoothed RTT from the QUIC stack.
func (qc *QUICConnection) RTT() time.Duration {
	stats := qc.conn.ConnectionStats()
	return stats.SmoothedRTT
}

// Stats returns the current connection statistics.
func (qc *QUICConnection) Stats() ConnectionStats {
	qs := qc.conn.ConnectionStats()
	var loss float64
	if qs.PacketsSent > 0 {
		loss = float64(qs.PacketsLost) / float64(qs.PacketsSent)
		if loss > 1.0 {
			loss = 1.0
		}
	}
	return ConnectionStats{
		RTT:  qs.SmoothedRTT,
		Loss: loss,
	}
}

// Close closes the connection gracefully.
func (qc *QUICConnection) Close() error {
	qc.closedMu.Lock()
	defer qc.closedMu.Unlock()
	if qc.closed {
		return qc.closeErr
	}
	qc.closed = true
	qc.cancel()
	qc.closeErr = qc.conn.CloseWithError(0, "shutdown")
	return qc.closeErr
}

// IsClosed returns true if the connection has been closed.
func (qc *QUICConnection) IsClosed() bool {
	qc.closedMu.Lock()
	defer qc.closedMu.Unlock()
	return qc.closed
}

// ConnectionState returns the QUIC connection state, including TLS information.
func (qc *QUICConnection) ConnectionState() quic.ConnectionState {
	return qc.conn.ConnectionState()
}
