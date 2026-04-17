package receiver_test

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/receiver"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// --- mock connection that delivers frames ---

type mockConn struct {
	mu      sync.Mutex
	id      uint64
	frames  [][]byte
	pos     int
	closed  bool
	recvErr error // if set, RecvData returns this error
}

func newMockConn(id uint64) *mockConn {
	return &mockConn{id: id}
}

// EnqueueFrame adds a raw frame to be returned by RecvData.
func (m *mockConn) EnqueueFrame(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.frames = append(m.frames, cp)
}

func (m *mockConn) ID() uint64 { return m.id }
func (m *mockConn) RemoteAddr() net.Addr {
	a, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:9999")
	return a
}
func (m *mockConn) Send(_ uint64, _ []byte) error   { return nil }
func (m *mockConn) SendControl(_ []byte) error      { return nil }
func (m *mockConn) SendProbe(_ []byte) error        { return nil }
func (m *mockConn) RecvControl(_ context.Context) ([]byte, error) { return nil, nil }
func (m *mockConn) RecvProbe(_ context.Context) ([]byte, error)   { return nil, nil }
func (m *mockConn) RTT() time.Duration                            { return time.Millisecond }
func (m *mockConn) Stats() quictr.ConnectionStats {
	return quictr.ConnectionStats{CongestionWindow: 65536}
}
func (m *mockConn) Close() error   { m.closed = true; return nil }
func (m *mockConn) IsClosed() bool { return m.closed }

func (m *mockConn) RecvData(_ context.Context) (uint64, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvErr != nil {
		return 0, nil, m.recvErr
	}
	if m.pos >= len(m.frames) {
		return 0, nil, nil
	}
	frame := m.frames[m.pos]
	m.pos++
	return 2, frame, nil
}

// --- frame builder ---

// buildFrame builds a valid Hyperspace frame (32-byte header + payload).
// Frame layout (all little-endian):
//
//	offset 0:  frameLength (int32) — total bytes including header
//	offset 4:  version (uint8)
//	offset 5:  flags (uint8)
//	offset 6:  frameType (uint16) — 1 = DATA
//	offset 8:  termOffset (int32)
//	offset 12: sessionID (int32)
//	offset 16: streamID (int32)
//	offset 20: termID (int32)
//	offset 24: reservedValue (int64)
//	offset 32: payload bytes
func buildFrame(sessionID, streamID, termOffset int32, payload []byte) []byte {
	frameLen := logbuffer.HeaderLength + len(payload)
	frame := make([]byte, frameLen)

	binary.LittleEndian.PutUint32(frame[0:], uint32(frameLen)) // frameLength
	frame[4] = 1                                               // version
	frame[5] = logbuffer.FlagBegin | logbuffer.FlagEnd         // unfragmented
	binary.LittleEndian.PutUint16(frame[6:], uint16(logbuffer.FrameTypeDATA)) // frameType
	binary.LittleEndian.PutUint32(frame[8:], uint32(termOffset))  // termOffset
	binary.LittleEndian.PutUint32(frame[12:], uint32(sessionID))  // sessionID
	binary.LittleEndian.PutUint32(frame[16:], uint32(streamID))   // streamID
	// termID, reservedValue = 0
	if len(payload) > 0 {
		copy(frame[logbuffer.HeaderLength:], payload)
	}
	return frame
}

// --- test helpers ---

const testRingSize = (1 << 12) + 128
const testBroadcastSize = 8*520 + 128

func newTestConductor(t *testing.T) (*conductor.Conductor, *ringbuffer.ManyToOneRingBuffer) {
	t.Helper()
	toDriverRaw := make([]byte, testRingSize)
	fromDriverRaw := make([]byte, testBroadcastSize)
	toDriverAtomic := atomicbuf.NewAtomicBuffer(toDriverRaw)
	fromDriverAtomic := atomicbuf.NewAtomicBuffer(fromDriverRaw)

	cond, err := conductor.New(toDriverAtomic, fromDriverAtomic, logbuffer.MinTermLength)
	if err != nil {
		t.Fatalf("conductor.New: %v", err)
	}
	ring, err := ringbuffer.NewManyToOneRingBuffer(toDriverAtomic)
	if err != nil {
		t.Fatalf("NewManyToOneRingBuffer: %v", err)
	}
	return cond, ring
}

// --- Tests ---

func TestDoWork_ReturnsZeroWhenNoPools(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 when no pools, got %d", n)
	}
}

func TestAddPool_RegistersPool(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)
	p := pool.New("peer1", 1, 4)
	rcv.AddPool("peer1", p) // should not panic
}

func TestDoWork_ReturnsZeroWhenPoolIsEmpty(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	p := pool.New("peer1", 1, 4)
	rcv.AddPool("peer1", p) // pool with no connections

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 when pool empty, got %d", n)
	}
}

func TestDoWork_ReturnsZeroWhenConnectionHasNoData(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 when no data, got %d", n)
	}
}

func TestDoWork_WritesReceivedFrameToImageLogBuffer(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	payload := []byte("hello hyperspace")
	frame := buildFrame(42, 100, 0, payload)
	mc.EnqueueFrame(frame)

	n := rcv.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 frame received, got %d", n)
	}
}

func TestDoWork_UnknownSessionIDCreatesNewImageLogBuffer(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Two frames with different session IDs — each should create a new image buffer.
	frame1 := buildFrame(11, 100, 0, []byte("session 11"))
	frame2 := buildFrame(22, 100, 0, []byte("session 22"))
	mc.EnqueueFrame(frame1)
	mc.EnqueueFrame(frame2)

	n := rcv.DoWork(context.Background())
	if n != 2 {
		t.Fatalf("expected 2 frames received, got %d", n)
	}
}

func TestDoWork_SameSessionIDReusesImageLogBuffer(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Two frames with the same session ID at sequential offsets.
	frame1 := buildFrame(99, 100, 0, []byte("msg1"))
	mc.EnqueueFrame(frame1)

	n1 := rcv.DoWork(context.Background())
	if n1 != 1 {
		t.Fatalf("expected 1 frame on first DoWork, got %d", n1)
	}

	aligned := logbuffer.AlignedLength(logbuffer.HeaderLength + len([]byte("msg1")))
	frame2 := buildFrame(99, 100, int32(aligned), []byte("msg2"))
	mc.EnqueueFrame(frame2)

	n2 := rcv.DoWork(context.Background())
	if n2 != 1 {
		t.Fatalf("expected 1 frame on second DoWork, got %d", n2)
	}
}

func TestDoWork_ShortFrameIsDiscarded(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Frame shorter than HeaderLength (32 bytes).
	mc.EnqueueFrame([]byte{1, 2, 3})

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (short frame discarded), got %d", n)
	}
}

func TestDoWork_InvalidFrameLengthIsDiscarded(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Build a frame with frame_length field > actual data length.
	frame := buildFrame(1, 100, 0, []byte("small"))
	// Overwrite frame_length to a huge value.
	binary.LittleEndian.PutUint32(frame[0:], 99999)
	mc.EnqueueFrame(frame)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (invalid frame length), got %d", n)
	}
}

func TestDoWork_PayloadExceedingMTUIsDiscarded(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 10) // very small MTU

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Build a frame with payload larger than MTU=10.
	frame := buildFrame(5, 100, 0, make([]byte, 50))
	mc.EnqueueFrame(frame)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (payload exceeds MTU), got %d", n)
	}
}

func TestName_ReturnsReceiver(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)
	if rcv.Name() != "receiver" {
		t.Errorf("Name: got %q, want %q", rcv.Name(), "receiver")
	}
}

func TestClose_ReturnsNil(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)
	if err := rcv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClose_ClearsImages(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Receive a frame to create an image.
	frame := buildFrame(42, 100, 0, []byte("test"))
	mc.EnqueueFrame(frame)
	rcv.DoWork(context.Background())

	// Close should clear the images.
	if err := rcv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDoWork_NegativeFrameLengthIsDiscarded(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Build a frame with zero frame_length.
	frame := buildFrame(1, 100, 0, []byte("test"))
	binary.LittleEndian.PutUint32(frame[0:], 0) // frameLength = 0
	mc.EnqueueFrame(frame)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (zero frame length), got %d", n)
	}
}

func TestDoWork_FrameOOBTermIsDiscarded(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Build a frame at termOffset so large it won't fit in the term buffer.
	// The term buffer is MinTermLength = 64KiB. Use termOffset = MinTermLength - 1.
	hugeOffset := int32(logbuffer.MinTermLength - 1)
	frame := buildFrame(77, 100, hugeOffset, []byte("oob"))
	mc.EnqueueFrame(frame)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (OOB frame discarded), got %d", n)
	}
}

func TestDoWork_RecvDataError_IsHandled(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	mc.recvErr = net.ErrClosed // simulate recv error
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Should not panic; error should be handled gracefully.
	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 on recv error, got %d", n)
	}
}

func TestNew_DefaultMTU(t *testing.T) {
	cond, _ := newTestConductor(t)
	// Zero MTU should use default of 1200.
	rcv := receiver.New(cond, 0)
	if rcv == nil {
		t.Fatal("expected non-nil receiver")
	}
}

func TestDoWork_MultiplePoolsPolled(t *testing.T) {
	cond, _ := newTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc1 := newMockConn(1)
	mc2 := newMockConn(2)

	p1 := pool.New("peer1", 1, 4)
	if err := p1.Add(mc1); err != nil {
		t.Fatalf("pool.Add p1: %v", err)
	}
	p2 := pool.New("peer2", 1, 4)
	if err := p2.Add(mc2); err != nil {
		t.Fatalf("pool.Add p2: %v", err)
	}

	rcv.AddPool("peer1", p1)
	rcv.AddPool("peer2", p2)

	// Enqueue one frame in each pool connection.
	frame1 := buildFrame(10, 100, 0, []byte("from peer1"))
	frame2 := buildFrame(20, 100, 0, []byte("from peer2"))
	mc1.EnqueueFrame(frame1)
	mc2.EnqueueFrame(frame2)

	n := rcv.DoWork(context.Background())
	if n != 2 {
		t.Fatalf("expected 2 frames from two pools, got %d", n)
	}
}
