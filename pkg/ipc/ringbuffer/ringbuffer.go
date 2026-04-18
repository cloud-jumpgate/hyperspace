// Package ringbuffer implements SPSC and MPSC ring buffers for Hyperspace IPC.
// These ring buffers are used for client↔driver command messaging, mirroring
// the role of Aeron's OneToOneRingBuffer and ManyToOneRingBuffer.
//
// They operate on an AtomicBuffer (which may be backed by a memory-mapped file).
//
// Buffer layout:
//   - Ring region: [0, capacity)
//   - Trailer (128 bytes): [capacity, capacity+128)
//   - offset+0:  tail_position       (int64) — producer write cursor
//   - offset+8:  head_cache_position (int64) — producer's cached head (avoids frequent atomic loads)
//   - offset+64: head_position       (int64) — consumer read cursor
//   - offset+72: correlation_id      (int64) — monotonic counter for correlation IDs
//
// The total buffer passed to New* must be exactly (power-of-2 capacity) + 128 bytes.
//
// Record layout inside the ring:
//   - offset 0: record_length (int32 LE) — total record bytes INCLUDING this 8-byte descriptor
//   - offset 4: msg_type_id  (int32 LE) — message type identifier (0 = padding)
//   - offset 8: msg bytes
//
// record_length > 0: valid record
// record_length < 0: padding (skip to next alignment boundary)
// record_length == 0: slot not yet committed (spin-wait on consumer side)
//
// Records are aligned to RecordDescriptorHeaderLength (8 bytes).
package ringbuffer

import (
	"errors"
	"fmt"
	"runtime"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
)

// RecordDescriptorHeaderLength is the size in bytes of the per-record header.
const RecordDescriptorHeaderLength = 8

// trailerLength is the number of bytes reserved at the end of the buffer for counters.
const trailerLength = 128

// Trailer offsets relative to capacity.
const (
	tailPositionOffset      = 0
	headCachePositionOffset = 8
	headPositionOffset      = 64
	correlationIDOffset     = 72
)

// msgTypePadding is the msg_type_id used for padding records.
const msgTypePadding = 0

// ErrInvalidBufferSize is returned when the buffer size is not a power-of-2 plus 128 bytes.
var ErrInvalidBufferSize = errors.New("ringbuffer: buffer size must be a power-of-2 capacity plus 128 bytes trailer")

// ErrMessageTooLarge is returned when the message is larger than the ring capacity.
var ErrMessageTooLarge = errors.New("ringbuffer: message too large for ring capacity")

// MessageHandler is called for each message during Read.
type MessageHandler func(msgTypeID int32, buffer *atomicbuf.AtomicBuffer, offset, length int)

// alignedLength rounds n up to the nearest multiple of RecordDescriptorHeaderLength.
func alignedLength(n int) int {
	const align = RecordDescriptorHeaderLength
	return (n + align - 1) &^ (align - 1)
}

// isPowerOfTwo returns true if n > 0 and n is a power of two.
func isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// validateBufferSize checks that bufSize is a power-of-2 plus trailerLength.
func validateBufferSize(bufSize int) (capacity int, err error) {
	if bufSize <= trailerLength {
		return 0, fmt.Errorf("%w: got %d", ErrInvalidBufferSize, bufSize)
	}
	cap := bufSize - trailerLength
	if !isPowerOfTwo(cap) {
		return 0, fmt.Errorf("%w: capacity portion %d is not a power of 2", ErrInvalidBufferSize, cap)
	}
	return cap, nil
}

// OneToOneRingBuffer is a single-producer, single-consumer ring buffer.
// Backed by an AtomicBuffer (may be mmap'd).
// NOT safe for concurrent writers.
type OneToOneRingBuffer struct {
	buf      *atomicbuf.AtomicBuffer
	capacity int
	mask     int
}

// NewOneToOneRingBuffer creates a SPSC ring buffer over the given AtomicBuffer.
// The buffer's capacity must be a power-of-2 plus 128 bytes for the trailer.
func NewOneToOneRingBuffer(buf *atomicbuf.AtomicBuffer) (*OneToOneRingBuffer, error) {
	cap, err := validateBufferSize(buf.Capacity())
	if err != nil {
		return nil, err
	}
	return &OneToOneRingBuffer{
		buf:      buf,
		capacity: cap,
		mask:     cap - 1,
	}, nil
}

// Capacity returns the usable message payload capacity (ring region size in bytes).
func (rb *OneToOneRingBuffer) Capacity() int {
	return rb.capacity
}

// Size returns the number of bytes of unread messages currently in the buffer.
func (rb *OneToOneRingBuffer) Size() int {
	head := rb.buf.GetInt64Volatile(rb.capacity + headPositionOffset)
	tail := rb.buf.GetInt64Volatile(rb.capacity + tailPositionOffset)
	return int(tail - head)
}

// Write appends a message to the ring buffer. Returns false if the buffer is full (back-pressure).
// Never blocks.
func (rb *OneToOneRingBuffer) Write(msgTypeID int32, src []byte) bool {
	recordLen := RecordDescriptorHeaderLength + len(src)
	required := alignedLength(recordLen)

	if required > rb.capacity {
		return false
	}

	tailOff := rb.capacity + tailPositionOffset
	headCacheOff := rb.capacity + headCachePositionOffset

	tail := rb.buf.GetInt64Volatile(tailOff)
	head := rb.buf.GetInt64Volatile(rb.capacity + headPositionOffset)
	rb.buf.PutInt64Ordered(headCacheOff, head)

	available := rb.capacity - int(tail-head)
	if required > available {
		return false
	}

	tailIndex := int(tail) & rb.mask
	toEnd := rb.capacity - tailIndex

	if toEnd < required {
		// Need to wrap — write a padding record to fill to end, then write from start
		if toEnd < RecordDescriptorHeaderLength {
			// Not even enough space for a padding header: check full available
			if required > available-toEnd {
				return false
			}
		}

		// Write padding
		if toEnd >= RecordDescriptorHeaderLength {
			rb.writePadding(tailIndex, toEnd)
		}
		tail += int64(toEnd)
		tailIndex = 0

		// Recheck space from the beginning
		newAvailable := rb.capacity - int(tail-head)
		if required > newAvailable {
			return false
		}
	}

	// Write the record header — write length LAST (as a store-release) so consumer sees complete record
	rb.buf.PutInt32LE(tailIndex+4, msgTypeID)
	if len(src) > 0 {
		rb.buf.PutBytes(tailIndex+RecordDescriptorHeaderLength, src)
	}
	// Store record_length last to signal record is ready
	rb.buf.PutInt32Ordered(tailIndex, int32(recordLen))

	// Advance tail
	rb.buf.PutInt64Ordered(tailOff, tail+int64(required))
	return true
}

// writePadding writes a padding record of exactly `length` bytes at `index`.
func (rb *OneToOneRingBuffer) writePadding(index, length int) {
	rb.buf.PutInt32LE(index+4, msgTypePadding)
	rb.buf.PutInt32Ordered(index, -int32(length)) // negative = padding
}

// Read delivers up to maxMessages messages to the handler. Returns the count delivered.
func (rb *OneToOneRingBuffer) Read(handler MessageHandler, maxMessages int) int {
	headOff := rb.capacity + headPositionOffset
	head := rb.buf.GetInt64Volatile(headOff)
	tail := rb.buf.GetInt64Volatile(rb.capacity + tailPositionOffset)

	bytesAvailable := int(tail - head)
	if bytesAvailable <= 0 {
		return 0
	}

	messagesRead := 0
	bytesRead := 0

	for messagesRead < maxMessages && bytesRead < bytesAvailable {
		index := int(head+int64(bytesRead)) & rb.mask

		// Spin until record_length is committed
		var recordLen int32
		for {
			recordLen = rb.buf.GetInt32Volatile(index)
			if recordLen != 0 {
				break
			}
			runtime.Gosched()
		}

		if recordLen < 0 {
			// Padding: skip to end of ring, advance head past this segment
			padLen := int(-recordLen)
			bytesRead += padLen
			continue
		}

		alignedLen := alignedLength(int(recordLen))
		msgTypeID := rb.buf.GetInt32LE(index + 4)
		msgOffset := index + RecordDescriptorHeaderLength
		msgLen := int(recordLen) - RecordDescriptorHeaderLength

		if msgTypeID != msgTypePadding {
			handler(msgTypeID, rb.buf, msgOffset, msgLen)
			messagesRead++
		}

		bytesRead += alignedLen
	}

	if bytesRead > 0 {
		// Zero out consumed region
		startIdx := int(head) & rb.mask
		endIdx := int(head+int64(bytesRead)) & rb.mask
		if endIdx > startIdx {
			rb.zeroRange(startIdx, bytesRead)
		} else {
			// Wrapped
			rb.zeroRange(startIdx, rb.capacity-startIdx)
			rb.zeroRange(0, endIdx)
		}
		rb.buf.PutInt64Ordered(headOff, head+int64(bytesRead))
	}

	return messagesRead
}

// zeroRange zeroes `length` bytes starting at `index` in the ring.
func (rb *OneToOneRingBuffer) zeroRange(index, length int) {
	b := rb.buf.Bytes()
	for i := 0; i < length && index+i < len(b); i++ {
		b[index+i] = 0
	}
}

// ManyToOneRingBuffer is a multi-producer, single-consumer ring buffer.
// Uses atomic claim (GetAndAddInt64) on tail so many goroutines can Write concurrently.
type ManyToOneRingBuffer struct {
	buf      *atomicbuf.AtomicBuffer
	capacity int
	mask     int
}

// NewManyToOneRingBuffer creates an MPSC ring buffer over the given AtomicBuffer.
// The buffer's total capacity must be a power-of-2 plus 128 bytes for the trailer.
func NewManyToOneRingBuffer(buf *atomicbuf.AtomicBuffer) (*ManyToOneRingBuffer, error) {
	cap, err := validateBufferSize(buf.Capacity())
	if err != nil {
		return nil, err
	}
	return &ManyToOneRingBuffer{
		buf:      buf,
		capacity: cap,
		mask:     cap - 1,
	}, nil
}

// Capacity returns the usable ring capacity in bytes.
func (rb *ManyToOneRingBuffer) Capacity() int {
	return rb.capacity
}

// Size returns the number of unread bytes in the buffer.
func (rb *ManyToOneRingBuffer) Size() int {
	head := rb.buf.GetInt64Volatile(rb.capacity + headPositionOffset)
	tail := rb.buf.GetInt64Volatile(rb.capacity + tailPositionOffset)
	return int(tail - head)
}

// Write appends a message using an atomic tail claim. Safe for concurrent producers.
// Returns false if the buffer is full.
func (rb *ManyToOneRingBuffer) Write(msgTypeID int32, src []byte) bool {
	recordLen := RecordDescriptorHeaderLength + len(src)
	required := alignedLength(recordLen)

	if required > rb.capacity {
		return false
	}

	tailOff := rb.capacity + tailPositionOffset
	headOff := rb.capacity + headPositionOffset

	// Spin to claim space
	var tail int64
	var padding int
	for {
		tail = rb.buf.GetInt64Volatile(tailOff)
		head := rb.buf.GetInt64Volatile(headOff)
		available := rb.capacity - int(tail-head)
		if required > available {
			return false
		}

		tailIndex := int(tail) & rb.mask
		toEnd := rb.capacity - tailIndex

		var claim int
		if toEnd < required {
			// Need wrap: claim toEnd (for padding) + required from start
			claim = toEnd + required
			if claim > available {
				return false
			}
		} else {
			claim = required
			padding = 0
		}

		// Atomically claim the space
		if rb.buf.CompareAndSetInt64(tailOff, tail, tail+int64(claim)) {
			padding = claim - required
			break
		}
		// CAS failed — another producer won; retry
		runtime.Gosched()
	}

	tailIndex := int(tail) & rb.mask
	toEnd := rb.capacity - tailIndex

	// If we wrapped, write padding at the old tail position
	if padding > 0 {
		rb.buf.PutInt32LE(tailIndex+4, msgTypePadding)
		rb.buf.PutInt32Ordered(tailIndex, -int32(toEnd))
		tailIndex = 0
	}

	// Write payload
	rb.buf.PutInt32LE(tailIndex+4, msgTypeID)
	if len(src) > 0 {
		rb.buf.PutBytes(tailIndex+RecordDescriptorHeaderLength, src)
	}
	// Publish by writing record_length last
	rb.buf.PutInt32Ordered(tailIndex, int32(recordLen))

	return true
}

// Read delivers up to maxMessages messages to the handler. Returns the count delivered.
// Must be called from a single consumer goroutine.
func (rb *ManyToOneRingBuffer) Read(handler MessageHandler, maxMessages int) int {
	headOff := rb.capacity + headPositionOffset
	head := rb.buf.GetInt64Volatile(headOff)
	tail := rb.buf.GetInt64Volatile(rb.capacity + tailPositionOffset)

	bytesAvailable := int(tail - head)
	if bytesAvailable <= 0 {
		return 0
	}

	messagesRead := 0
	bytesRead := 0

	for messagesRead < maxMessages && bytesRead < bytesAvailable {
		index := int(head+int64(bytesRead)) & rb.mask

		var recordLen int32
		for {
			recordLen = rb.buf.GetInt32Volatile(index)
			if recordLen != 0 {
				break
			}
			runtime.Gosched()
		}

		if recordLen < 0 {
			padLen := int(-recordLen)
			bytesRead += padLen
			continue
		}

		alignedLen := alignedLength(int(recordLen))
		msgTypeID := rb.buf.GetInt32LE(index + 4)
		msgOffset := index + RecordDescriptorHeaderLength
		msgLen := int(recordLen) - RecordDescriptorHeaderLength

		if msgTypeID != msgTypePadding {
			handler(msgTypeID, rb.buf, msgOffset, msgLen)
			messagesRead++
		}

		bytesRead += alignedLen
	}

	if bytesRead > 0 {
		startIdx := int(head) & rb.mask
		endIdx := int(head+int64(bytesRead)) & rb.mask
		if endIdx > startIdx {
			rb.zeroRange(startIdx, bytesRead)
		} else {
			rb.zeroRange(startIdx, rb.capacity-startIdx)
			rb.zeroRange(0, endIdx)
		}
		rb.buf.PutInt64Ordered(headOff, head+int64(bytesRead))
	}

	return messagesRead
}

// zeroRange zeroes `length` bytes starting at `index` in the ring.
func (rb *ManyToOneRingBuffer) zeroRange(index, length int) {
	b := rb.buf.Bytes()
	for i := 0; i < length && index+i < len(b); i++ {
		b[index+i] = 0
	}
}
