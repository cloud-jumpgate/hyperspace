// Package atomic provides a raw byte-slice wrapper that exposes atomic read/write
// operations over a contiguous memory region. This is the Go counterpart to Aeron's
// AtomicBuffer / Agrona's UnsafeBuffer.
//
// All multi-byte values are little-endian, matching the Aeron wire format.
// The buffer may back a memory-mapped file or a plain heap allocation.
package atomic

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"unsafe"
)

// AtomicBuffer wraps a byte slice and exposes atomic and volatile operations.
// All multi-byte values are little-endian (same as Aeron wire format).
// The buffer may back a memory-mapped file or a plain heap allocation.
//
//nolint:revive // stutter is intentional: atomic.AtomicBuffer is the established public API name
type AtomicBuffer struct {
	buf []byte
}

// NewAtomicBuffer creates a new AtomicBuffer wrapping the given byte slice.
func NewAtomicBuffer(b []byte) *AtomicBuffer {
	return &AtomicBuffer{buf: b}
}

// Bytes returns the underlying byte slice.
func (b *AtomicBuffer) Bytes() []byte {
	return b.buf
}

// Capacity returns the total number of bytes in the buffer.
func (b *AtomicBuffer) Capacity() int {
	return len(b.buf)
}

// checkBounds panics if [offset, offset+size) is out of range.
func (b *AtomicBuffer) checkBounds(offset, size int) {
	if offset < 0 || offset+size > len(b.buf) {
		panic(fmt.Sprintf("atomic.AtomicBuffer: offset %d size %d out of bounds (capacity %d)", offset, size, len(b.buf)))
	}
}

// checkAlignment panics if offset is not aligned to the given alignment.
func checkAlignment(offset, alignment int) {
	if offset%alignment != 0 {
		panic(fmt.Sprintf("atomic.AtomicBuffer: offset %d is not %d-byte aligned", offset, alignment))
	}
}

// ptr64 returns an unsafe pointer to the int64 at the given offset.
func (b *AtomicBuffer) ptr64(offset int) *int64 {
	b.checkBounds(offset, 8)
	checkAlignment(offset, 8)
	return (*int64)(unsafe.Pointer(&b.buf[offset]))
}

// ptr32 returns an unsafe pointer to the int32 at the given offset.
func (b *AtomicBuffer) ptr32(offset int) *int32 {
	b.checkBounds(offset, 4)
	checkAlignment(offset, 4)
	return (*int32)(unsafe.Pointer(&b.buf[offset]))
}

// GetAndAddInt64 atomically adds delta to the int64 at offset and returns the old value.
// offset must be 8-byte aligned.
func (b *AtomicBuffer) GetAndAddInt64(offset int, delta int64) int64 {
	return atomic.AddInt64(b.ptr64(offset), delta) - delta
}

// GetInt64Volatile atomically loads the int64 at offset.
// offset must be 8-byte aligned.
func (b *AtomicBuffer) GetInt64Volatile(offset int) int64 {
	return atomic.LoadInt64(b.ptr64(offset))
}

// PutInt64Ordered atomically stores v at offset with release semantics.
// offset must be 8-byte aligned.
func (b *AtomicBuffer) PutInt64Ordered(offset int, v int64) {
	atomic.StoreInt64(b.ptr64(offset), v)
}

// CompareAndSetInt64 atomically compares the int64 at offset to expected and,
// if equal, sets it to desired. Returns true if the swap occurred.
// offset must be 8-byte aligned.
func (b *AtomicBuffer) CompareAndSetInt64(offset int, expected, desired int64) bool {
	return atomic.CompareAndSwapInt64(b.ptr64(offset), expected, desired)
}

// GetAndAddInt32 atomically adds delta to the int32 at offset and returns the old value.
// offset must be 4-byte aligned.
func (b *AtomicBuffer) GetAndAddInt32(offset int, delta int32) int32 {
	return atomic.AddInt32(b.ptr32(offset), delta) - delta
}

// GetInt32Volatile atomically loads the int32 at offset.
// offset must be 4-byte aligned.
func (b *AtomicBuffer) GetInt32Volatile(offset int) int32 {
	return atomic.LoadInt32(b.ptr32(offset))
}

// PutInt32Ordered atomically stores v at offset with release semantics.
// offset must be 4-byte aligned.
func (b *AtomicBuffer) PutInt32Ordered(offset int, v int32) {
	atomic.StoreInt32(b.ptr32(offset), v)
}

// CompareAndSetInt32 atomically compares the int32 at offset to expected and,
// if equal, sets it to desired. Returns true if the swap occurred.
// offset must be 4-byte aligned.
func (b *AtomicBuffer) CompareAndSetInt32(offset int, expected, desired int32) bool {
	return atomic.CompareAndSwapInt32(b.ptr32(offset), expected, desired)
}

// GetBytes copies len(dst) bytes from the buffer starting at offset into dst.
func (b *AtomicBuffer) GetBytes(offset int, dst []byte) {
	b.checkBounds(offset, len(dst))
	copy(dst, b.buf[offset:])
}

// PutBytes copies src into the buffer starting at offset.
func (b *AtomicBuffer) PutBytes(offset int, src []byte) {
	b.checkBounds(offset, len(src))
	copy(b.buf[offset:], src)
}

// GetInt32LE reads a little-endian int32 at offset (plain, non-atomic).
func (b *AtomicBuffer) GetInt32LE(offset int) int32 {
	b.checkBounds(offset, 4)
	return int32(binary.LittleEndian.Uint32(b.buf[offset:])) // #nosec G115 -- Aeron wire format reinterpret: uint32 bytes to int32 signed representation
}

// PutInt32LE writes a little-endian int32 at offset (plain, non-atomic).
func (b *AtomicBuffer) PutInt32LE(offset int, v int32) {
	b.checkBounds(offset, 4)
	binary.LittleEndian.PutUint32(b.buf[offset:], uint32(v)) // #nosec G115 -- Aeron wire format: int32 to uint32 binary encoding, bit pattern preserved
}

// GetInt64LE reads a little-endian int64 at offset (plain, non-atomic).
func (b *AtomicBuffer) GetInt64LE(offset int) int64 {
	b.checkBounds(offset, 8)
	return int64(binary.LittleEndian.Uint64(b.buf[offset:])) // #nosec G115 -- Aeron wire format reinterpret: uint64 bytes to int64 signed representation
}

// PutInt64LE writes a little-endian int64 at offset (plain, non-atomic).
func (b *AtomicBuffer) PutInt64LE(offset int, v int64) {
	b.checkBounds(offset, 8)
	binary.LittleEndian.PutUint64(b.buf[offset:], uint64(v)) // #nosec G115 -- Aeron wire format: int64 to uint64 binary encoding, bit pattern preserved
}

// GetUint8 reads a single byte at offset.
func (b *AtomicBuffer) GetUint8(offset int) uint8 {
	b.checkBounds(offset, 1)
	return b.buf[offset]
}

// PutUint8 writes a single byte at offset.
func (b *AtomicBuffer) PutUint8(offset int, v uint8) {
	b.checkBounds(offset, 1)
	b.buf[offset] = v
}

// GetUint16LE reads a little-endian uint16 at offset.
func (b *AtomicBuffer) GetUint16LE(offset int) uint16 {
	b.checkBounds(offset, 2)
	return binary.LittleEndian.Uint16(b.buf[offset:])
}

// PutUint16LE writes a little-endian uint16 at offset.
func (b *AtomicBuffer) PutUint16LE(offset int, v uint16) {
	b.checkBounds(offset, 2)
	binary.LittleEndian.PutUint16(b.buf[offset:], v)
}
