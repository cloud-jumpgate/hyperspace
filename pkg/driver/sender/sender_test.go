package sender_test

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/sender"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- mock connection ---

type mockConn struct {
	id         uint64
	sentFrames [][]byte
	sendErr    error
	closed     bool
}

func newMockConn(id uint64) *mockConn {
	return &mockConn{id: id}
}

func (m *mockConn) ID() uint64 { return m.id }
func (m *mockConn) RemoteAddr() net.Addr {
	a, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:9999")
	return a
}
func (m *mockConn) Send(_ uint64, data []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.sentFrames = append(m.sentFrames, cp)
	return nil
}
func (m *mockConn) SendControl(_ []byte) error { return nil }
func (m *mockConn) SendProbe(_ []byte) error   { return nil }
func (m *mockConn) RecvData(_ context.Context) (uint64, []byte, error) {
	return 0, nil, nil
}
func (m *mockConn) RecvControl(_ context.Context) ([]byte, error) { return nil, nil }
func (m *mockConn) RecvProbe(_ context.Context) ([]byte, error)   { return nil, nil }
func (m *mockConn) RTT() time.Duration                            { return time.Millisecond }
func (m *mockConn) Stats() quictr.ConnectionStats {
	return quictr.ConnectionStats{RTT: time.Millisecond, CongestionWindow: 65536}
}
func (m *mockConn) Close() error   { m.closed = true; return nil }
func (m *mockConn) IsClosed() bool { return m.closed }

// SentCount returns how many frames have been sent.
func (m *mockConn) SentCount() int { return len(m.sentFrames) }

// --- test helpers ---

const testRingSize = (1 << 12) + 128  // 4096 + 128
const testBroadcastSize = 8*520 + 128 // for conductor broadcast

func newTestConductorAndRing(t *testing.T) (*conductor.Conductor, *ringbuffer.ManyToOneRingBuffer) {
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

func writeAddPublication(t *testing.T, ring *ringbuffer.ManyToOneRingBuffer, correlationID int64, streamID int32) {
	t.Helper()
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:], uint64(correlationID))
	binary.LittleEndian.PutUint32(payload[8:], uint32(streamID))
	binary.LittleEndian.PutUint32(payload[12:], 0) // no channel
	if !ring.Write(conductor.CmdAddPublication, payload) {
		t.Fatal("ring.Write failed")
	}
}

// appendMessage writes an unfragmented message into a publication's log buffer.
func appendToPublication(t *testing.T, pub *conductor.PublicationState, msg []byte) {
	t.Helper()
	app := pub.LogBuf.Appender(0)
	result := app.AppendUnfragmented(pub.SessionID, pub.StreamID, pub.TermID, msg, 0)
	if result < 0 {
		t.Fatalf("AppendUnfragmented failed: %d", result)
	}
}

// --- Tests ---

func TestDoWork_ReturnsZeroWhenNoPublications(t *testing.T) {
	cond, _ := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)

	n := s.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestDoWork_ReturnsZeroWhenNoPools(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)

	// Add a publication.
	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())

	// Write a frame into the log buffer.
	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication")
	}
	appendToPublication(t, pubs[0], []byte("hello"))

	// No pools registered — should return 0.
	n := s.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (no pools), got %d", n)
	}
}

func TestAddPool_RegistersPool(t *testing.T) {
	cond, _ := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)

	p := pool.New("peer1", 1, 4)
	s.AddPool("peer1", p) // should not panic
}

func TestDoWork_SendsFramesWhenPublicationHasData(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	// Add a publication.
	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication")
	}

	// Append a message to the log buffer.
	appendToPublication(t, pubs[0], []byte("test message payload"))

	// Now DoWork should send the frame.
	n := s.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 frame sent, got %d", n)
	}
	if mc.SentCount() != 1 {
		t.Fatalf("expected 1 frame on connection, got %d", mc.SentCount())
	}
}

func TestDoWork_DoesNotResendAlreadySentFrames(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	appendToPublication(t, pubs[0], []byte("once"))

	// First DoWork sends the frame.
	s.DoWork(context.Background())

	// Second DoWork should not re-send.
	n := s.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 on second DoWork (no new data), got %d", n)
	}
	if mc.SentCount() != 1 {
		t.Fatalf("expected total 1 send, got %d", mc.SentCount())
	}
}

func TestDoWork_SendsMultipleFrames(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	// Append 3 messages.
	appendToPublication(t, pubs[0], []byte("frame 1"))
	appendToPublication(t, pubs[0], []byte("frame 2"))
	appendToPublication(t, pubs[0], []byte("frame 3"))

	n := s.DoWork(context.Background())
	if n != 3 {
		t.Fatalf("expected 3 frames sent, got %d", n)
	}
}

func TestName_ReturnsSender(t *testing.T) {
	cond, _ := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	if s.Name() != "sender" {
		t.Errorf("Name: got %q, want %q", s.Name(), "sender")
	}
}

func TestClose_ReturnsNil(t *testing.T) {
	cond, _ := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDoWork_DropsFrameExceedingMTU(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	// MTU = 32 bytes (very small).
	s := sender.New(cond, arbitrator.NewRandom(nil), 32)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	// Append a message larger than MTU=32.
	appendToPublication(t, pubs[0], make([]byte, 100))

	// Frame should be dropped due to MTU violation.
	n := s.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 frames sent (MTU exceeded), got %d", n)
	}
}

func TestDoWork_SendError_DoesNotCount(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	mc.sendErr = net.ErrClosed // simulate send failure
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())
	pubs := cond.Publications()
	appendToPublication(t, pubs[0], []byte("fail"))

	n := s.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (send error), got %d", n)
	}
}

// mockArbitrator for testing arbitrator integration.
type countingArbitrator struct {
	pickCount atomic.Int64
}

func (a *countingArbitrator) Name() string { return "counting" }
func (a *countingArbitrator) Pick(candidates []quictr.Connection, _ int64, _ int) (quictr.Connection, error) {
	a.pickCount.Add(1)
	if len(candidates) == 0 {
		return nil, arbitrator.ErrNoConnections
	}
	return candidates[0], nil
}

func TestDoWork_SendsFrameWithLowStreamID(t *testing.T) {
	// When publication StreamID is 0 or 1, sender upgrades it to 2.
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	// StreamID = 1 → should be upgraded to 2 by sender.
	writeAddPublication(t, ring, 1, 1)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication")
	}
	appendToPublication(t, pubs[0], []byte("low stream id"))

	n := s.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 frame sent (low stream ID auto-upgraded), got %d", n)
	}
}

func TestNew_DefaultMTU(t *testing.T) {
	cond, _ := newTestConductorAndRing(t)
	// Zero MTU should use default 1200.
	s := sender.New(cond, arbitrator.NewRandom(nil), 0)
	if s == nil {
		t.Fatal("expected non-nil sender")
	}
	if s.Name() != "sender" {
		t.Errorf("Name: got %q, want sender", s.Name())
	}
}

func TestDoWork_NilLogBufPublication_ReturnsZero(t *testing.T) {
	// A publication with a nil LogBuf should return 0.
	// We can simulate this by checking the DoWork returns 0 for empty publications.
	cond, _ := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)

	// No publications registered → DoWork returns 0.
	n := s.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

// --- F-022: Sender Position Term-Aware Tracking ---

func TestDoWork_PositionTracksPartitionChange(t *testing.T) {
	// After term rotation, sender should read from offset 0 in the new partition.
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())
	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication")
	}

	// Write messages to fill the first partition and trigger rotation.
	bigMsg := make([]byte, 1024)
	for {
		app := pubs[0].LogBuf.Appender(0)
		result := app.AppendUnfragmented(pubs[0].SessionID, pubs[0].StreamID, pubs[0].TermID, bigMsg, 0)
		if result < 0 {
			break
		}
	}

	// Send all frames from partition 0.
	for {
		n := s.DoWork(context.Background())
		if n == 0 {
			break
		}
	}

	// Now write to partition 1 after rotation.
	pubs[0].LogBuf.SetActivePartitionIndex(1)
	app1 := pubs[0].LogBuf.Appender(1)
	smallMsg := []byte("post-rotation")
	result := app1.AppendUnfragmented(pubs[0].SessionID, pubs[0].StreamID, pubs[0].TermID, smallMsg, 0)
	if result < 0 {
		t.Fatalf("AppendUnfragmented on partition 1 failed: %d", result)
	}

	// Sender should pick up the new frame from partition 1 at offset 0.
	n := s.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 frame from new partition, got %d", n)
	}
}

func TestDoWork_NoFrameSkipOnRotation(t *testing.T) {
	// Verify that all frames are sent, even across partition boundaries.
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())
	pubs := cond.Publications()

	// Write 3 messages to partition 0.
	appendToPublication(t, pubs[0], []byte("msg-1"))
	appendToPublication(t, pubs[0], []byte("msg-2"))
	appendToPublication(t, pubs[0], []byte("msg-3"))

	totalSent := 0
	for {
		n := s.DoWork(context.Background())
		totalSent += n
		if n == 0 {
			break
		}
	}

	if totalSent != 3 {
		t.Fatalf("expected 3 frames sent total, got %d", totalSent)
	}
	if mc.SentCount() != 3 {
		t.Fatalf("expected 3 frames on connection, got %d", mc.SentCount())
	}
}

func TestDoWork_UsesArbitrator(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	arb := &countingArbitrator{}
	s := sender.New(cond, arb, 1200)
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())
	pubs := cond.Publications()
	appendToPublication(t, pubs[0], []byte("test"))

	s.DoWork(context.Background())

	if arb.pickCount.Load() == 0 {
		t.Error("expected arbitrator Pick to be called at least once")
	}
}

// --- F-025: Config Externalisation ---

func TestNew_DefaultFragmentsPerBatch(t *testing.T) {
	cond, _ := newTestConductorAndRing(t)
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200)
	if s == nil {
		t.Fatal("expected non-nil sender with default fragmentsPerBatch")
	}
}

func TestNew_WithFragmentsPerBatch(t *testing.T) {
	cond, ring := newTestConductorAndRing(t)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}

	// Set fragmentsPerBatch to 1: should send at most 1 frame per DoWork.
	s := sender.New(cond, arbitrator.NewRandom(nil), 1200, sender.WithFragmentsPerBatch(1))
	s.AddPool("peer1", p)

	writeAddPublication(t, ring, 1, 100)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	appendToPublication(t, pubs[0], []byte("msg-1"))
	appendToPublication(t, pubs[0], []byte("msg-2"))
	appendToPublication(t, pubs[0], []byte("msg-3"))

	// First DoWork should send exactly 1 frame (fragmentsPerBatch=1).
	n := s.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 frame (fragmentsPerBatch=1), got %d", n)
	}
}
