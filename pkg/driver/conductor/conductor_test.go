package conductor_test

import (
	"context"
	"encoding/binary"
	"testing"

	"go.uber.org/goleak"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/broadcast"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// bufferSizes for tests — small but valid.
// Ring buffer: power-of-2 capacity + 128 trailer.
const testRingSize = (1 << 12) + 128 // 4096 + 128 = 4224

// Broadcast buffer: slotCount * slotSize + TrailerLength
// broadcastMaxPayload in conductor is 512; slotSize = 512+8 = 520.
// 8 slots: 8 * 520 + 128 = 4288.
const testBroadcastSize = 8*520 + 128

const testTermLength = logbuffer.MinTermLength // 64 KiB

// newTestConductor creates a Conductor backed by fresh in-memory buffers and
// returns the Conductor, the ring writer (to inject commands), and the broadcast receiver.
func newTestConductor(t *testing.T) (
	*conductor.Conductor,
	*ringbuffer.ManyToOneRingBuffer,
	*broadcast.Receiver,
) {
	t.Helper()

	toDriverRaw := make([]byte, testRingSize)
	fromDriverRaw := make([]byte, testBroadcastSize)

	toDriverAtomic := atomicbuf.NewAtomicBuffer(toDriverRaw)
	fromDriverAtomic := atomicbuf.NewAtomicBuffer(fromDriverRaw)

	cond, err := conductor.New(toDriverAtomic, fromDriverAtomic, testTermLength)
	if err != nil {
		t.Fatalf("conductor.New: %v", err)
	}

	ring, err := ringbuffer.NewManyToOneRingBuffer(toDriverAtomic)
	if err != nil {
		t.Fatalf("NewManyToOneRingBuffer: %v", err)
	}

	rx, err := broadcast.NewReceiverFromStart(fromDriverAtomic, 512)
	if err != nil {
		t.Fatalf("broadcast.NewReceiverFromStart: %v", err)
	}

	return cond, ring, rx
}

// writeAddPublication writes a CmdAddPublication payload to the ring.
func writeAddPublication(t *testing.T, ring *ringbuffer.ManyToOneRingBuffer, correlationID int64, streamID int32, channel string) {
	t.Helper()
	ch := []byte(channel)
	payload := make([]byte, 16+len(ch))
	binary.LittleEndian.PutUint64(payload[0:], uint64(correlationID))
	binary.LittleEndian.PutUint32(payload[8:], uint32(streamID))
	binary.LittleEndian.PutUint32(payload[12:], uint32(len(ch)))
	copy(payload[16:], ch)
	if !ring.Write(conductor.CmdAddPublication, payload) {
		t.Fatal("ring.Write failed for CmdAddPublication")
	}
}

// writeRemovePublication writes a CmdRemovePublication payload.
func writeRemovePublication(t *testing.T, ring *ringbuffer.ManyToOneRingBuffer, publicationID int64) {
	t.Helper()
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload[0:], uint64(publicationID))
	if !ring.Write(conductor.CmdRemovePublication, payload) {
		t.Fatal("ring.Write failed for CmdRemovePublication")
	}
}

// writeAddSubscription writes a CmdAddSubscription payload.
func writeAddSubscription(t *testing.T, ring *ringbuffer.ManyToOneRingBuffer, correlationID int64, streamID int32, channel string) {
	t.Helper()
	ch := []byte(channel)
	payload := make([]byte, 16+len(ch))
	binary.LittleEndian.PutUint64(payload[0:], uint64(correlationID))
	binary.LittleEndian.PutUint32(payload[8:], uint32(streamID))
	binary.LittleEndian.PutUint32(payload[12:], uint32(len(ch)))
	copy(payload[16:], ch)
	if !ring.Write(conductor.CmdAddSubscription, payload) {
		t.Fatal("ring.Write failed for CmdAddSubscription")
	}
}

// writeRemoveSubscription writes a CmdRemoveSubscription payload.
func writeRemoveSubscription(t *testing.T, ring *ringbuffer.ManyToOneRingBuffer, subscriptionID int64) {
	t.Helper()
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload[0:], uint64(subscriptionID))
	if !ring.Write(conductor.CmdRemoveSubscription, payload) {
		t.Fatal("ring.Write failed for CmdRemoveSubscription")
	}
}

// writeClientKeepalive writes a CmdClientKeepalive payload.
func writeClientKeepalive(t *testing.T, ring *ringbuffer.ManyToOneRingBuffer, clientID int64) {
	t.Helper()
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload[0:], uint64(clientID))
	if !ring.Write(conductor.CmdClientKeepalive, payload) {
		t.Fatal("ring.Write failed for CmdClientKeepalive")
	}
}

// drainBroadcast reads all pending broadcast messages into a slice of (typeID, payload) pairs.
func drainBroadcast(rx *broadcast.Receiver) []struct {
	typeID  int32
	payload []byte
} {
	var msgs []struct {
		typeID  int32
		payload []byte
	}
	for {
		got, err := rx.Receive(func(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
			p := make([]byte, length)
			buf.GetBytes(offset, p)
			msgs = append(msgs, struct {
				typeID  int32
				payload []byte
			}{typeID: msgTypeID, payload: p})
		})
		if err != nil || !got {
			break
		}
	}
	return msgs
}

// --- Tests ---

func TestDoWork_ReturnsZeroWhenRingEmpty(t *testing.T) {
	cond, _, _ := newTestConductor(t)
	ctx := context.Background()
	n := cond.DoWork(ctx)
	if n != 0 {
		t.Fatalf("expected 0 when ring empty, got %d", n)
	}
}

func TestDoWork_ReturnsCommandCountWhenCommandsPresent(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	writeAddPublication(t, ring, 100, 1001, "aeron:udp?endpoint=localhost:20000")
	writeAddPublication(t, ring, 200, 1002, "aeron:udp?endpoint=localhost:20001")

	ctx := context.Background()
	n := cond.DoWork(ctx)
	if n != 2 {
		t.Fatalf("expected 2 commands processed, got %d", n)
	}
}

func TestCmdAddPublication_CreatesPublicationState(t *testing.T) {
	cond, ring, rx := newTestConductor(t)

	writeAddPublication(t, ring, 42, 1001, "aeron:udp?endpoint=localhost:20000")

	ctx := context.Background()
	n := cond.DoWork(ctx)
	if n != 1 {
		t.Fatalf("expected 1 command, got %d", n)
	}

	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication, got %d", len(pubs))
	}
	pub := pubs[0]
	if pub.PublicationID != 42 {
		t.Errorf("PublicationID: got %d, want 42", pub.PublicationID)
	}
	if pub.StreamID != 1001 {
		t.Errorf("StreamID: got %d, want 1001", pub.StreamID)
	}
	if pub.Channel != "aeron:udp?endpoint=localhost:20000" {
		t.Errorf("Channel: got %q, want expected URI", pub.Channel)
	}
	if pub.LogBuf == nil {
		t.Error("expected LogBuf to be non-nil")
	}

	// Verify RspPublicationReady broadcast.
	msgs := drainBroadcast(rx)
	if len(msgs) == 0 {
		t.Fatal("expected at least one broadcast message")
	}
	found := false
	for _, msg := range msgs {
		if msg.typeID == conductor.RspPublicationReady {
			found = true
			// Payload layout: correlationID(8) + sessionID(4) + streamID(4)
			corrID := int64(binary.LittleEndian.Uint64(msg.payload[0:]))
			streamID := int32(binary.LittleEndian.Uint32(msg.payload[12:]))
			if corrID != 42 {
				t.Errorf("RspPublicationReady correlationID: got %d, want 42", corrID)
			}
			if streamID != 1001 {
				t.Errorf("RspPublicationReady streamID: got %d, want 1001", streamID)
			}
		}
	}
	if !found {
		t.Error("expected RspPublicationReady broadcast")
	}
}

func TestCmdAddPublication_LogBufferIsValid(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	writeAddPublication(t, ring, 99, 2002, "")
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication")
	}
	pub := pubs[0]
	if pub.LogBuf.TermLength() != testTermLength {
		t.Errorf("TermLength: got %d, want %d", pub.LogBuf.TermLength(), testTermLength)
	}
	if pub.SessionID == 0 {
		t.Error("expected non-zero SessionID")
	}
}

func TestCmdRemovePublication_RemovesState(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	ctx := context.Background()

	writeAddPublication(t, ring, 10, 100, "test")
	cond.DoWork(ctx)

	if len(cond.Publications()) != 1 {
		t.Fatal("expected 1 publication after add")
	}

	writeRemovePublication(t, ring, 10)
	cond.DoWork(ctx)

	if len(cond.Publications()) != 0 {
		t.Fatalf("expected 0 publications after remove, got %d", len(cond.Publications()))
	}
}

func TestCmdRemovePublication_UnknownIDIsNoError(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	// Remove a publication that was never added — should not panic.
	writeRemovePublication(t, ring, 9999)
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestCmdAddSubscription_CreatesSubscriptionState(t *testing.T) {
	cond, ring, rx := newTestConductor(t)

	writeAddSubscription(t, ring, 77, 3003, "aeron:udp?endpoint=localhost:30000")
	cond.DoWork(context.Background())

	subs := cond.Subscriptions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	sub := subs[0]
	if sub.SubscriptionID != 77 {
		t.Errorf("SubscriptionID: got %d, want 77", sub.SubscriptionID)
	}
	if sub.StreamID != 3003 {
		t.Errorf("StreamID: got %d, want 3003", sub.StreamID)
	}
	if sub.Channel != "aeron:udp?endpoint=localhost:30000" {
		t.Errorf("Channel: got %q", sub.Channel)
	}

	// Verify RspSubscriptionReady broadcast.
	msgs := drainBroadcast(rx)
	found := false
	for _, msg := range msgs {
		if msg.typeID == conductor.RspSubscriptionReady {
			found = true
			corrID := int64(binary.LittleEndian.Uint64(msg.payload[0:]))
			if corrID != 77 {
				t.Errorf("RspSubscriptionReady correlationID: got %d, want 77", corrID)
			}
		}
	}
	if !found {
		t.Error("expected RspSubscriptionReady broadcast")
	}
}

func TestCmdRemoveSubscription_RemovesState(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	ctx := context.Background()

	writeAddSubscription(t, ring, 55, 500, "test")
	cond.DoWork(ctx)

	if len(cond.Subscriptions()) != 1 {
		t.Fatal("expected 1 subscription after add")
	}

	writeRemoveSubscription(t, ring, 55)
	cond.DoWork(ctx)

	if len(cond.Subscriptions()) != 0 {
		t.Fatalf("expected 0 subscriptions after remove, got %d", len(cond.Subscriptions()))
	}
}

func TestCmdRemoveSubscription_UnknownIDIsNoError(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	writeRemoveSubscription(t, ring, 8888)
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestCmdClientKeepalive_NoError(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	writeClientKeepalive(t, ring, 12345)
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed, got %d", n)
	}
}

func TestCmdAddCounter_NoOpNoError(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	// CmdAddCounter is a no-op in sprint 3.
	payload := make([]byte, 8)
	if !ring.Write(conductor.CmdAddCounter, payload) {
		t.Fatal("ring.Write failed")
	}
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed, got %d", n)
	}
}

func TestPublications_ThreadSafeSnapshot(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	ctx := context.Background()

	// Add multiple publications.
	for i := int64(1); i <= 5; i++ {
		writeAddPublication(t, ring, i, int32(i*100), "chan")
		cond.DoWork(ctx)
	}

	pubs := cond.Publications()
	if len(pubs) != 5 {
		t.Fatalf("expected 5 publications, got %d", len(pubs))
	}
}

func TestSubscriptions_ThreadSafeSnapshot(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	ctx := context.Background()

	for i := int64(1); i <= 3; i++ {
		writeAddSubscription(t, ring, i, int32(i*100), "chan")
		cond.DoWork(ctx)
	}

	subs := cond.Subscriptions()
	if len(subs) != 3 {
		t.Fatalf("expected 3 subscriptions, got %d", len(subs))
	}
}

func TestDoWork_ProcessesUpTo10Commands(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	ctx := context.Background()

	// Write 15 commands.
	for i := int64(1); i <= 15; i++ {
		writeClientKeepalive(t, ring, i)
	}

	// First DoWork should process up to 10.
	n := cond.DoWork(ctx)
	if n != 10 {
		t.Fatalf("expected 10 commands in first cycle, got %d", n)
	}

	// Second DoWork should process the remaining 5.
	n = cond.DoWork(ctx)
	if n != 5 {
		t.Fatalf("expected 5 commands in second cycle, got %d", n)
	}
}

func TestName_ReturnsConductor(t *testing.T) {
	cond, _, _ := newTestConductor(t)
	if cond.Name() != "conductor" {
		t.Errorf("Name: got %q, want %q", cond.Name(), "conductor")
	}
}

func TestClose_ClearsState(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	ctx := context.Background()

	writeAddPublication(t, ring, 1, 100, "test")
	cond.DoWork(ctx)

	if err := cond.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(cond.Publications()) != 0 {
		t.Error("expected 0 publications after Close")
	}
}

func TestCmdAddPublication_ShortPayloadHandled(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	// Write a too-short payload (less than 16 bytes).
	if !ring.Write(conductor.CmdAddPublication, []byte{1, 2, 3}) {
		t.Fatal("ring.Write failed")
	}
	// Should not panic.
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed even with short payload")
	}
}

func TestCmdAddSubscription_ShortPayloadHandled(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	if !ring.Write(conductor.CmdAddSubscription, []byte{1, 2, 3}) {
		t.Fatal("ring.Write failed")
	}
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestCmdRemovePublication_ShortPayloadHandled(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	if !ring.Write(conductor.CmdRemovePublication, []byte{1, 2}) {
		t.Fatal("ring.Write failed")
	}
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestCmdRemoveSubscription_ShortPayloadHandled(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	if !ring.Write(conductor.CmdRemoveSubscription, []byte{1, 2}) {
		t.Fatal("ring.Write failed")
	}
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestCmdClientKeepalive_ShortPayloadHandled(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	if !ring.Write(conductor.CmdClientKeepalive, []byte{1, 2}) {
		t.Fatal("ring.Write failed")
	}
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestUnknownCommand_IsHandled(t *testing.T) {
	cond, ring, _ := newTestConductor(t)
	// Write an unknown command type (e.g. 99).
	payload := make([]byte, 8)
	if !ring.Write(99, payload) {
		t.Fatal("ring.Write failed")
	}
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
}

func TestNew_InvalidRingBufferReturnsError(t *testing.T) {
	// Pass a buffer that's not a valid ring buffer size (not power-of-2 + 128).
	invalidBuf := atomicbuf.NewAtomicBuffer(make([]byte, 100)) // too small / not valid
	fromDriverRaw := make([]byte, testBroadcastSize)
	fromDriverAtomic := atomicbuf.NewAtomicBuffer(fromDriverRaw)

	_, err := conductor.New(invalidBuf, fromDriverAtomic, testTermLength)
	if err == nil {
		t.Fatal("expected error for invalid ring buffer size")
	}
}

func TestNew_InvalidBroadcastBufferReturnsError(t *testing.T) {
	toDriverRaw := make([]byte, testRingSize)
	toDriverAtomic := atomicbuf.NewAtomicBuffer(toDriverRaw)
	// Pass a broadcast buffer that is too small / invalid.
	invalidBroadcast := atomicbuf.NewAtomicBuffer(make([]byte, 100))

	_, err := conductor.New(toDriverAtomic, invalidBroadcast, testTermLength)
	if err == nil {
		t.Fatal("expected error for invalid broadcast buffer size")
	}
}

func TestCmdAddPublication_WithChannel(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	// Write a publication with a non-empty channel string.
	ch := "aeron:udp?endpoint=localhost:20000"
	writeAddPublication(t, ring, 1, 100, ch)
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
	pubs := cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication")
	}
	if pubs[0].Channel != ch {
		t.Errorf("Channel mismatch: got %q, want %q", pubs[0].Channel, ch)
	}
}

func TestCmdAddSubscription_WithChannel(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	ch := "aeron:udp?endpoint=localhost:30000"
	writeAddSubscription(t, ring, 1, 200, ch)
	n := cond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command processed")
	}
	subs := cond.Subscriptions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription")
	}
	if subs[0].Channel != ch {
		t.Errorf("Channel mismatch: got %q, want %q", subs[0].Channel, ch)
	}
}

func TestBroadcastError_TriggeredOnInvalidTermLength(t *testing.T) {
	// Create a conductor with a valid term length first.
	cond, ring, rx := newTestConductor(t)

	// Inject a CmdAddPublication with an invalid term length by hijacking termLength.
	// We can't do this directly, so instead we test the broadcast error path
	// by triggering it through an invalid configuration at the conductor level.
	// The easiest way: create a conductor with an invalid term length (but New will fail).
	// Instead, verify broadcastError is called when logbuffer creation fails.
	// This requires us to set up a conductor whose termLength is invalid.
	// Since New validates the ring and broadcast buffers but not termLength directly,
	// we inject a valid publication command and then use a separate conductor with
	// bad termLength to trigger the error path.

	// Create conductor with an invalid term length (not power of 2).
	toDriverRaw2 := make([]byte, testRingSize)
	fromDriverRaw2 := make([]byte, testBroadcastSize)
	toDriverAtomic2 := atomicbuf.NewAtomicBuffer(toDriverRaw2)
	fromDriverAtomic2 := atomicbuf.NewAtomicBuffer(fromDriverRaw2)

	// term length 12345 is not a power of two.
	badCond, err := conductor.New(toDriverAtomic2, fromDriverAtomic2, 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ring2, err := ringbuffer.NewManyToOneRingBuffer(toDriverAtomic2)
	if err != nil {
		t.Fatalf("ring: %v", err)
	}

	rx2, err := broadcast.NewReceiverFromStart(fromDriverAtomic2, 512)
	if err != nil {
		t.Fatalf("rx: %v", err)
	}

	writeAddPublication(t, ring2, 1, 100, "test")
	n := badCond.DoWork(context.Background())
	if n != 1 {
		t.Fatalf("expected 1 command, got %d", n)
	}

	// Should NOT have created a publication (logbuffer.New would fail).
	pubs := badCond.Publications()
	if len(pubs) != 0 {
		t.Fatalf("expected no publications with bad term length, got %d", len(pubs))
	}

	// Should have broadcast an error response.
	msgs := drainBroadcast(rx2)
	hasError := false
	for _, msg := range msgs {
		if msg.typeID == conductor.RspError {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Error("expected RspError to be broadcast when logbuffer creation fails")
	}

	// The original conductor/ring/rx are unused here but created by newTestConductor.
	_ = cond
	_ = ring
	_ = rx
}

// --- P-03: Lock-Free Conductor Reads ---

func TestPublications_LockFreeAfterAdd(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	// Initially empty.
	pubs := cond.Publications()
	if len(pubs) != 0 {
		t.Fatalf("expected 0 publications initially, got %d", len(pubs))
	}

	// Add a publication via the ring buffer.
	writeAddPublication(t, ring, 1, 100, "")
	cond.DoWork(context.Background())

	// Publications should now have 1 entry (lock-free read).
	pubs = cond.Publications()
	if len(pubs) != 1 {
		t.Fatalf("expected 1 publication, got %d", len(pubs))
	}
}

func TestSubscriptions_LockFreeAfterAdd(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	// Initially empty.
	subs := cond.Subscriptions()
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions initially, got %d", len(subs))
	}

	// Add a subscription via the ring buffer.
	writeAddSubscription(t, ring, 1, 200, "")
	cond.DoWork(context.Background())

	// Subscriptions should now have 1 entry.
	subs = cond.Subscriptions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
}

func TestPublications_EmptyAfterRemove(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	writeAddPublication(t, ring, 1, 100, "")
	cond.DoWork(context.Background())

	if len(cond.Publications()) != 1 {
		t.Fatal("expected 1 publication after add")
	}

	// Remove the publication.
	writeRemovePublication(t, ring, 1)
	cond.DoWork(context.Background())

	pubs := cond.Publications()
	if len(pubs) != 0 {
		t.Fatalf("expected 0 publications after remove, got %d", len(pubs))
	}
}

func TestPublications_ConcurrentRead(t *testing.T) {
	cond, ring, _ := newTestConductor(t)

	writeAddPublication(t, ring, 1, 100, "")
	cond.DoWork(context.Background())

	// Read publications concurrently from multiple goroutines.
	done := make(chan struct{})
	for range 10 {
		go func() {
			for range 100 {
				pubs := cond.Publications()
				_ = pubs
			}
			done <- struct{}{}
		}()
	}
	for range 10 {
		<-done
	}
}
