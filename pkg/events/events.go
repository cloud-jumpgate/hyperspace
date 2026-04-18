// Package events implements a structured event logger backed by a broadcast ring.
// Events are written by driver agents and consumed by diagnostic tools or external
// monitoring systems.
//
// The Event type has a fixed binary layout for zero-allocation encoding and
// direct mapping onto a shared memory region.
//
// Internally, EventLog wraps a broadcast.Transmitter and EventReader wraps a
// broadcast.Receiver.
package events

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/broadcast"
)

// EventType identifies the kind of structured event.
type EventType uint16

// Known event types.
const (
	EvtConnectionOpened    EventType = 1
	EvtConnectionClosed    EventType = 2
	EvtPublicationAdded    EventType = 3
	EvtPublicationRemoved  EventType = 4
	EvtSubscriptionAdded   EventType = 5
	EvtSubscriptionRemoved EventType = 6
	EvtPathProbeRTT        EventType = 7
	EvtPoolLearnerAction   EventType = 8 // Add/Remove/Hold
	EvtCCTransition        EventType = 9 // CC state machine transition
	EvtBackPressure        EventType = 10
)

// eventBroadcastTypeID is the fixed broadcast msgTypeID used for all events.
const eventBroadcastTypeID int32 = 1

// Event is a structured log entry with a fixed binary layout.
//
// Wire layout (little-endian, total 112 bytes):
//
//	[0:2]    Type        uint16
//	[2:8]    _pad        (6 bytes, reserved, always zero)
//	[8:16]   TimestampNs int64
//	[16:24]  ConnID      uint64
//	[24:28]  StreamID    int32
//	[28:32]  SessionID   int32
//	[32:40]  Value1      int64
//	[40:48]  Value2      int64
//	[48:112] Message     [64]byte  (null-terminated short label)
type Event struct {
	Type        EventType
	TimestampNs int64
	ConnID      uint64
	StreamID    int32
	SessionID   int32
	Value1      int64
	Value2      int64
	Message     [64]byte
}

// eventEncodedSize is the fixed wire size of an encoded Event.
const eventEncodedSize = 112

// now is a hook for tests to override time.Now.
var now = func() time.Time { return time.Now() }

// SetMessage copies s into evt.Message as a null-terminated string.
// Truncates to 63 bytes if s is longer.
func (evt *Event) SetMessage(s string) {
	n := copy(evt.Message[:63], s)
	evt.Message[n] = 0
}

// Message returns the null-terminated string from evt.Message.
func (evt *Event) GetMessage() string {
	for i, b := range evt.Message {
		if b == 0 {
			return string(evt.Message[:i])
		}
	}
	return string(evt.Message[:])
}

// encode serialises evt into dst. dst must be >= eventEncodedSize bytes.
func encode(dst []byte, evt Event) {
	binary.LittleEndian.PutUint16(dst[0:2], uint16(evt.Type))
	// bytes 2-7: padding — zero
	dst[2] = 0
	dst[3] = 0
	dst[4] = 0
	dst[5] = 0
	dst[6] = 0
	dst[7] = 0
	binary.LittleEndian.PutUint64(dst[8:16], uint64(evt.TimestampNs))
	binary.LittleEndian.PutUint64(dst[16:24], evt.ConnID)
	binary.LittleEndian.PutUint32(dst[24:28], uint32(evt.StreamID))
	binary.LittleEndian.PutUint32(dst[28:32], uint32(evt.SessionID))
	binary.LittleEndian.PutUint64(dst[32:40], uint64(evt.Value1))
	binary.LittleEndian.PutUint64(dst[40:48], uint64(evt.Value2))
	copy(dst[48:112], evt.Message[:])
}

// decode deserialises an Event from src. src must be >= eventEncodedSize bytes.
func decode(src []byte) Event {
	var evt Event
	evt.Type = EventType(binary.LittleEndian.Uint16(src[0:2]))
	evt.TimestampNs = int64(binary.LittleEndian.Uint64(src[8:16]))
	evt.ConnID = binary.LittleEndian.Uint64(src[16:24])
	evt.StreamID = int32(binary.LittleEndian.Uint32(src[24:28]))
	evt.SessionID = int32(binary.LittleEndian.Uint32(src[28:32]))
	evt.Value1 = int64(binary.LittleEndian.Uint64(src[32:40]))
	evt.Value2 = int64(binary.LittleEndian.Uint64(src[40:48]))
	copy(evt.Message[:], src[48:112])
	return evt
}

// broadcastBufSize returns the total bytes needed for the broadcast buffer
// given a ring capacity (number of slots; must be a power of 2).
func broadcastBufSize(capacity int) int {
	return (broadcast.RecordHeaderLength+eventEncodedSize)*capacity + broadcast.TrailerLength
}

// EventLog writes events to a broadcast ring.
// It is safe to call Log from multiple goroutines concurrently.
// The underlying broadcast.Transmitter is SPSC; EventLog serialises concurrent
// callers with a mutex. On the hot path a single producer uses the driver agent
// goroutine and pays no contention cost.
type EventLog struct {
	mu  sync.Mutex
	tx  *broadcast.Transmitter
	buf *atomicbuf.AtomicBuffer
}

// NewEventLog creates an EventLog backed by an in-memory broadcast ring.
// capacity is the number of event slots in the ring; it must be a positive power of 2.
func NewEventLog(capacity int) *EventLog {
	if capacity <= 0 || (capacity&(capacity-1)) != 0 {
		panic(fmt.Sprintf("events: capacity %d must be a positive power of 2", capacity))
	}
	raw := make([]byte, broadcastBufSize(capacity))
	buf := atomicbuf.NewAtomicBuffer(raw)
	tx, err := broadcast.NewTransmitter(buf, eventEncodedSize)
	if err != nil {
		panic(fmt.Sprintf("events: failed to create broadcast transmitter: %v", err))
	}
	return &EventLog{tx: tx, buf: buf}
}

// Log encodes evt and writes it to the broadcast ring.
// If TimestampNs is zero, it is set to the current wall-clock time in nanoseconds.
// Safe to call from multiple goroutines concurrently.
func (l *EventLog) Log(evt Event) {
	if evt.TimestampNs == 0 {
		evt.TimestampNs = now().UnixNano()
	}
	var encodeBuf [eventEncodedSize]byte
	encode(encodeBuf[:], evt)
	l.mu.Lock()
	// ErrMessageTooLarge cannot occur: eventEncodedSize == maxPayload.
	_ = l.tx.Transmit(eventBroadcastTypeID, encodeBuf[:])
	l.mu.Unlock()
}

// NewReader creates an EventReader positioned at the current tail of the ring.
// Events logged before this call are not visible to the returned reader.
func (l *EventLog) NewReader() *EventReader {
	rx, err := broadcast.NewReceiver(l.buf, eventEncodedSize)
	if err != nil {
		panic(fmt.Sprintf("events: failed to create broadcast receiver: %v", err))
	}
	return &EventReader{rx: rx}
}

// NewReaderFromStart creates an EventReader positioned at the earliest available
// slot in the ring. Useful for replaying recent history on startup.
func (l *EventLog) NewReaderFromStart() *EventReader {
	rx, err := broadcast.NewReceiverFromStart(l.buf, eventEncodedSize)
	if err != nil {
		panic(fmt.Sprintf("events: failed to create broadcast receiver from start: %v", err))
	}
	return &EventReader{rx: rx}
}

// EventReader reads events from the broadcast ring (non-blocking).
// Each EventReader maintains an independent cursor.
type EventReader struct {
	rx *broadcast.Receiver
}

// Poll delivers available events to handler in order.
// Returns the number of events processed.
// On lapping (receiver fell behind transmitter), the reader skips lost events
// and continues from the oldest available slot.
func (r *EventReader) Poll(handler func(Event)) int {
	count := 0
	for {
		ok, err := r.rx.Receive(func(_ int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
			if length < eventEncodedSize {
				return
			}
			var raw [eventEncodedSize]byte
			buf.GetBytes(offset, raw[:])
			handler(decode(raw[:]))
		})
		if err != nil {
			if err == broadcast.ErrLapped {
				// Advance cursor past gap, keep draining
				continue
			}
			break
		}
		if !ok {
			break
		}
		count++
	}
	return count
}
