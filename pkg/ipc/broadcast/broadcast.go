// Package broadcast implements a one-to-many broadcast transmitter and receiver,
// mirroring the role of Aeron's BroadcastTransmitter and BroadcastReceiver.
//
// A single Transmitter writes to a shared AtomicBuffer (typically mmap'd).
// Multiple independent Receivers each maintain their own read cursor and read
// from the same shared buffer.
//
// Late subscribers may miss messages if the transmitter has wrapped past their
// cursor (lapping). The Receiver detects this and returns ErrLapped.
//
// Buffer layout:
//   - Ring region: [0, capacity)    — fixed-size record slots
//   - Trailer (128 bytes): [capacity, capacity+128)
//     - offset+0:  latest_counter (int64) — sequence number of last transmitted message
//     - offset+8:  descriptor     (int64) — unused / reserved
//
// Each record slot in the ring:
//   - offset 0: length   (int32 LE) — payload length (0 = empty/padding)
//   - offset 4: typeID   (int32 LE) — message type
//   - offset 8: payload bytes
//
// Slots are RecordDescriptorLength bytes each (fixed size = maxMsgLength + 8).
// The buffer size must satisfy:
//
//	total = recordLength * power-of-2 slotCount + trailerLength
//
// For simplicity this package uses a fixed record slot size where each slot
// can hold up to maxMsgLength bytes of payload (plus the 8-byte header).
// The total buffer size passed to New* must be:
//
//	(RecordHeaderLength + MaxPayloadLength) * power-of-2 + TrailerLength
package broadcast

import (
	"errors"
	"fmt"
	"runtime"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
)

// RecordHeaderLength is the size in bytes of the per-record header.
const RecordHeaderLength = 8

// TrailerLength is the number of bytes reserved at the end of the buffer for counters.
const TrailerLength = 128

// Trailer counter offsets (relative to capacity).
const (
	latestCounterOffset = 0
	descriptorOffset    = 8
)

// ErrLapped is returned when the receiver has been lapped by the transmitter.
var ErrLapped = errors.New("broadcast: receiver lapped — messages were missed")

// ErrMessageTooLarge is returned when a message exceeds the slot payload capacity.
var ErrMessageTooLarge = errors.New("broadcast: message too large for slot")

// ErrBufferTooSmall is returned when the buffer cannot fit at least one slot.
var ErrBufferTooSmall = errors.New("broadcast: buffer too small")

// MessageHandler is called for each received message.
type MessageHandler func(msgTypeID int32, buffer *atomicbuf.AtomicBuffer, offset, length int)

// slotSize computes the total bytes per slot given max payload length.
func slotSize(maxPayload int) int {
	return RecordHeaderLength + maxPayload
}

// isPowerOfTwo returns true iff n > 0 and is a power of two.
func isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// validateBuffer checks the buffer size and returns the number of slots and max payload per slot.
// Buffer layout: slotCount * slotBytes + TrailerLength
// slotBytes must equal (total - TrailerLength) / slotCount; slotCount must be power of 2.
// We infer slotBytes from the payload hinted by the caller, or auto-detect.
func validateBuffer(bufSize, maxPayload int) (slotCount, slotBytes int, err error) {
	if bufSize <= TrailerLength {
		return 0, 0, fmt.Errorf("%w: got %d", ErrBufferTooSmall, bufSize)
	}
	ringSize := bufSize - TrailerLength
	sb := slotSize(maxPayload)
	if ringSize%sb != 0 {
		return 0, 0, fmt.Errorf("broadcast: ring size %d not evenly divisible by slot size %d", ringSize, sb)
	}
	sc := ringSize / sb
	if !isPowerOfTwo(sc) {
		return 0, 0, fmt.Errorf("broadcast: slot count %d must be a power of 2", sc)
	}
	return sc, sb, nil
}

// Transmitter sends messages to all mapped receivers via a shared AtomicBuffer.
type Transmitter struct {
	buf        *atomicbuf.AtomicBuffer
	capacity   int   // ring region size in bytes
	slotCount  int   // number of fixed-size slots
	slotBytes  int   // bytes per slot (RecordHeaderLength + maxPayload)
	maxPayload int   // max bytes of payload per message
	mask       int   // slotCount - 1
	sequence   int64 // monotonically increasing message sequence; kept in trailer
}

// NewTransmitter creates a Transmitter over the given AtomicBuffer.
// maxPayload specifies the maximum payload bytes per message.
// The buffer must be sized as (slotCount * (maxPayload+8)) + 128 where slotCount is a power of 2.
func NewTransmitter(buf *atomicbuf.AtomicBuffer, maxPayload int) (*Transmitter, error) {
	sc, sb, err := validateBuffer(buf.Capacity(), maxPayload)
	if err != nil {
		return nil, err
	}
	cap := buf.Capacity() - TrailerLength

	t := &Transmitter{
		buf:        buf,
		capacity:   cap,
		slotCount:  sc,
		slotBytes:  sb,
		maxPayload: maxPayload,
		mask:       sc - 1,
		sequence:   buf.GetInt64Volatile(cap + latestCounterOffset),
	}
	return t, nil
}

// Transmit sends a message to all receivers. Returns ErrMessageTooLarge if src exceeds maxPayload.
//
// The slot stores the total record length (RecordHeaderLength + payload length) so that
// zero-length payloads are distinguishable from empty slots. The in-progress sentinel is -1.
// The receiver decodes msgLen = storedLen - RecordHeaderLength.
func (t *Transmitter) Transmit(msgTypeID int32, src []byte) error {
	if len(src) > t.maxPayload {
		return fmt.Errorf("%w: payload %d > max %d", ErrMessageTooLarge, len(src), t.maxPayload)
	}

	seq := t.sequence + 1
	slotIndex := int(seq) & t.mask
	slotOffset := slotIndex * t.slotBytes

	// Signal in-progress write to readers by storing sentinel -1
	t.buf.PutInt32Ordered(slotOffset, -1)

	// Write payload and type
	t.buf.PutInt32LE(slotOffset+4, msgTypeID)
	if len(src) > 0 {
		t.buf.PutBytes(slotOffset+RecordHeaderLength, src)
	}

	// Publish: store total record length (always >= RecordHeaderLength, so never 0)
	// This distinguishes a committed zero-payload record from an empty slot (0).
	recordLen := int32(RecordHeaderLength + len(src))
	t.buf.PutInt32Ordered(slotOffset, recordLen)

	// Update the latest counter in the trailer
	t.buf.PutInt64Ordered(t.capacity+latestCounterOffset, seq)
	t.sequence = seq
	return nil
}

// Receiver reads messages from a shared broadcast buffer.
// Each Receiver maintains its own local cursor.
// Multiple Receivers can share the same AtomicBuffer independently.
type Receiver struct {
	buf        *atomicbuf.AtomicBuffer
	capacity   int
	slotCount  int
	slotBytes  int
	maxPayload int
	mask       int
	cursor     int64 // next sequence number to read
}

// NewReceiver creates a Receiver that starts reading from the current tail of the buffer.
func NewReceiver(buf *atomicbuf.AtomicBuffer, maxPayload int) (*Receiver, error) {
	sc, sb, err := validateBuffer(buf.Capacity(), maxPayload)
	if err != nil {
		return nil, err
	}
	cap := buf.Capacity() - TrailerLength

	// Start cursor at the current latest so we only see new messages
	latest := buf.GetInt64Volatile(cap + latestCounterOffset)

	return &Receiver{
		buf:        buf,
		capacity:   cap,
		slotCount:  sc,
		slotBytes:  sb,
		maxPayload: maxPayload,
		mask:       sc - 1,
		cursor:     latest,
	}, nil
}

// NewReceiverFromStart creates a Receiver that starts reading from the beginning of
// whatever messages are still in the ring (sequence 0 up to wrap window).
func NewReceiverFromStart(buf *atomicbuf.AtomicBuffer, maxPayload int) (*Receiver, error) {
	sc, sb, err := validateBuffer(buf.Capacity(), maxPayload)
	if err != nil {
		return nil, err
	}
	cap := buf.Capacity() - TrailerLength
	latest := buf.GetInt64Volatile(cap + latestCounterOffset)

	// Start as far back as the ring allows (at most slotCount messages ago)
	start := latest - int64(sc)
	if start < 0 {
		start = 0
	}

	return &Receiver{
		buf:        buf,
		capacity:   cap,
		slotCount:  sc,
		slotBytes:  sb,
		maxPayload: maxPayload,
		mask:       sc - 1,
		cursor:     start,
	}, nil
}

// Receive delivers the next message to the handler if one is available.
// Returns (true, nil) if a message was delivered.
// Returns (false, nil) if the receiver is caught up.
// Returns (false, ErrLapped) if the receiver fell too far behind (messages missed).
func (r *Receiver) Receive(handler MessageHandler) (bool, error) {
	latest := r.buf.GetInt64Volatile(r.capacity + latestCounterOffset)
	if r.cursor >= latest {
		return false, nil
	}

	// Check for lapping: if transmitter has wrapped more than slotCount times past cursor
	if latest-r.cursor > int64(r.slotCount) {
		// We've been lapped — advance cursor to catch up
		r.cursor = latest - int64(r.slotCount)
		return false, ErrLapped
	}

	nextSeq := r.cursor + 1
	slotIndex := int(nextSeq) & r.mask
	slotOffset := slotIndex * r.slotBytes

	// Spin-wait for the record to be committed.
	// -1 = in-progress (transmitter is mid-write)
	//  0 = empty slot (never written)
	// >0 = committed record (total record length including header)
	var storedLen int32
	for {
		storedLen = r.buf.GetInt32Volatile(slotOffset)
		if storedLen > 0 {
			break
		}
		if storedLen == 0 {
			// Slot is empty — transmitter hasn't started writing here yet
			return false, nil
		}
		// storedLen == -1: in-progress, spin
		runtime.Gosched()
	}

	// Verify we haven't been lapped during the read
	latestAfter := r.buf.GetInt64Volatile(r.capacity + latestCounterOffset)
	if latestAfter-nextSeq >= int64(r.slotCount) {
		r.cursor = latestAfter - int64(r.slotCount)
		return false, ErrLapped
	}

	msgTypeID := r.buf.GetInt32LE(slotOffset + 4)
	msgOffset := slotOffset + RecordHeaderLength
	msgLen := int(storedLen) - RecordHeaderLength
	if msgLen < 0 {
		msgLen = 0
	}

	handler(msgTypeID, r.buf, msgOffset, msgLen)
	r.cursor = nextSeq
	return true, nil
}
