// Package counters implements driver metrics counters stored in a shared buffer.
// Each counter is an int64 at a fixed 8-byte aligned offset. Counters live in
// the CnC buffer (memory-mapped in production, plain slice in tests).
//
// Atomic operations follow the same pattern as internal/atomic.AtomicBuffer,
// using sync/atomic via unsafe.Pointer for lock-free access.
package counters

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// Counter IDs — each is an int64 at a fixed offset in the counters buffer.
const (
	CtrBytesSent         = 0
	CtrBytesReceived     = 1
	CtrMsgSent           = 2
	CtrMsgReceived       = 3
	CtrConnectionsActive = 4
	CtrConnectionOpens   = 5
	CtrConnectionCloses  = 6
	CtrPingsSent         = 7
	CtrPongsReceived     = 8
	CtrLostFrames        = 9
	CtrBackPressureEvents = 10
	CtrRotationEvents    = 11
	NumCounters          = 12
)

// counterNames maps counter ID to its human-readable name.
var counterNames = [NumCounters]string{
	"bytes_sent",
	"bytes_received",
	"messages_sent",
	"messages_received",
	"connections_active",
	"connection_opens",
	"connection_closes",
	"pings_sent",
	"pongs_received",
	"lost_frames",
	"backpressure_events",
	"rotation_events",
}

// minBufSize is the minimum number of bytes required: NumCounters * 8 bytes each.
const minBufSize = NumCounters * 8

// CounterName returns the human-readable name for a counter ID.
// Returns an empty string for unknown IDs.
func CounterName(id int) string {
	if id < 0 || id >= NumCounters {
		return ""
	}
	return counterNames[id]
}

// NewBuffer allocates a fresh zeroed byte slice large enough for all counters.
func NewBuffer() []byte {
	return make([]byte, minBufSize)
}

// ptr64 returns an *int64 pointing into buf at the 8-byte aligned slot for id.
// Panics if id is out of range or buf is too small.
func ptr64(buf []byte, id int) *int64 {
	if id < 0 || id >= NumCounters {
		panic(fmt.Sprintf("counters: id %d out of range [0, %d)", id, NumCounters))
	}
	offset := id * 8
	if offset+8 > len(buf) {
		panic(fmt.Sprintf("counters: buffer too small (need %d bytes, got %d)", offset+8, len(buf)))
	}
	return (*int64)(unsafe.Pointer(&buf[offset]))
}

// CountersReader reads counter values from a shared buffer (read-only).
// Safe to use from multiple goroutines concurrently.
type CountersReader struct {
	buf []byte
}

// NewCountersReader creates a CountersReader over buf.
// buf must be at least NumCounters*8 bytes.
func NewCountersReader(buf []byte) *CountersReader {
	if len(buf) < minBufSize {
		panic(fmt.Sprintf("counters: buffer too small: need %d, got %d", minBufSize, len(buf)))
	}
	return &CountersReader{buf: buf}
}

// Get atomically loads the counter value at id.
func (r *CountersReader) Get(id int) int64 {
	return atomic.LoadInt64(ptr64(r.buf, id))
}

// CountersWriter reads and writes counters (driver-side).
// Safe to use from multiple goroutines concurrently.
type CountersWriter struct {
	buf []byte
}

// NewCountersWriter creates a CountersWriter over buf.
// buf must be at least NumCounters*8 bytes.
func NewCountersWriter(buf []byte) *CountersWriter {
	if len(buf) < minBufSize {
		panic(fmt.Sprintf("counters: buffer too small: need %d, got %d", minBufSize, len(buf)))
	}
	return &CountersWriter{buf: buf}
}

// Add atomically adds delta to the counter at id.
func (w *CountersWriter) Add(id int, delta int64) {
	atomic.AddInt64(ptr64(w.buf, id), delta)
}

// Set atomically stores value at the counter for id.
func (w *CountersWriter) Set(id int, value int64) {
	atomic.StoreInt64(ptr64(w.buf, id), value)
}

// Get atomically loads the counter value at id.
func (w *CountersWriter) Get(id int) int64 {
	return atomic.LoadInt64(ptr64(w.buf, id))
}

// Reader returns a CountersReader backed by the same buffer as this writer.
// The reader sees live updates from the writer.
func (w *CountersWriter) Reader() *CountersReader {
	return NewCountersReader(w.buf)
}
