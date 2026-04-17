package atomic

import (
	"sync"
	syncatomic "sync/atomic"
	"testing"
)

func makeBuffer(size int) *AtomicBuffer {
	return NewAtomicBuffer(make([]byte, size))
}

// --- Construction and basic accessors ---

func TestNewAtomicBuffer(t *testing.T) {
	raw := make([]byte, 64)
	ab := NewAtomicBuffer(raw)
	if ab.Capacity() != 64 {
		t.Fatalf("expected capacity 64, got %d", ab.Capacity())
	}
	if len(ab.Bytes()) != 64 {
		t.Fatalf("expected bytes length 64, got %d", len(ab.Bytes()))
	}
}

func TestCapacityAndBytes(t *testing.T) {
	ab := makeBuffer(128)
	if ab.Capacity() != 128 {
		t.Errorf("capacity: expected 128, got %d", ab.Capacity())
	}
	if &ab.Bytes()[0] != &ab.buf[0] {
		t.Errorf("Bytes() should return underlying slice")
	}
}

// --- GetBytes / PutBytes ---

func TestGetPutBytes(t *testing.T) {
	ab := makeBuffer(64)
	src := []byte("hello, hyperspace")
	ab.PutBytes(0, src)

	dst := make([]byte, len(src))
	ab.GetBytes(0, dst)
	if string(dst) != string(src) {
		t.Errorf("GetBytes: expected %q, got %q", src, dst)
	}
}

func TestGetPutBytesAtOffset(t *testing.T) {
	ab := makeBuffer(64)
	src := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	ab.PutBytes(16, src)

	dst := make([]byte, 4)
	ab.GetBytes(16, dst)
	for i, v := range dst {
		if v != src[i] {
			t.Errorf("byte %d: expected 0x%02X, got 0x%02X", i, src[i], v)
		}
	}
}

func TestGetBytesOutOfBounds(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for out-of-bounds GetBytes")
		}
	}()
	ab := makeBuffer(8)
	dst := make([]byte, 16)
	ab.GetBytes(0, dst)
}

func TestPutBytesOutOfBounds(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for out-of-bounds PutBytes")
		}
	}()
	ab := makeBuffer(8)
	ab.PutBytes(0, make([]byte, 16))
}

func TestPutBytesNegativeOffset(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative offset")
		}
	}()
	ab := makeBuffer(16)
	ab.PutBytes(-1, []byte{1})
}

// --- LE helpers ---

func TestInt32LE(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt32LE(0, -12345)
	if v := ab.GetInt32LE(0); v != -12345 {
		t.Errorf("expected -12345, got %d", v)
	}
	ab.PutInt32LE(4, 0x7FFFFFFF)
	if v := ab.GetInt32LE(4); v != 0x7FFFFFFF {
		t.Errorf("expected 0x7FFFFFFF, got %d", v)
	}
}

func TestInt64LE(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64LE(0, -9876543210)
	if v := ab.GetInt64LE(0); v != -9876543210 {
		t.Errorf("expected -9876543210, got %d", v)
	}
	ab.PutInt64LE(8, 0x7FFFFFFFFFFFFFFF)
	if v := ab.GetInt64LE(8); v != 0x7FFFFFFFFFFFFFFF {
		t.Errorf("expected max int64, got %d", v)
	}
}

func TestGetInt32LEOutOfBounds(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	ab := makeBuffer(4)
	ab.GetInt32LE(2) // would read bytes 2-5, only 4 bytes available
}

func TestGetInt64LEOutOfBounds(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	ab := makeBuffer(8)
	ab.GetInt64LE(4) // would read bytes 4-11
}

func TestUint8(t *testing.T) {
	ab := makeBuffer(8)
	ab.PutUint8(3, 0xFF)
	if v := ab.GetUint8(3); v != 0xFF {
		t.Errorf("expected 0xFF, got 0x%02X", v)
	}
	ab.PutUint8(0, 0x00)
	if v := ab.GetUint8(0); v != 0x00 {
		t.Errorf("expected 0x00, got 0x%02X", v)
	}
}

func TestUint8OutOfBounds(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	ab := makeBuffer(4)
	ab.GetUint8(4)
}

func TestUint16LE(t *testing.T) {
	ab := makeBuffer(8)
	ab.PutUint16LE(0, 0xABCD)
	if v := ab.GetUint16LE(0); v != 0xABCD {
		t.Errorf("expected 0xABCD, got 0x%04X", v)
	}
	// Verify little-endian byte order
	if ab.buf[0] != 0xCD || ab.buf[1] != 0xAB {
		t.Errorf("expected LE bytes [0xCD, 0xAB], got [0x%02X, 0x%02X]", ab.buf[0], ab.buf[1])
	}
}

func TestUint16LEOutOfBounds(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	ab := makeBuffer(4)
	ab.GetUint16LE(4)
}

// --- Atomic int64 operations ---

func TestGetAndAddInt64(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64LE(0, 100)
	// force atomic store so volatile read picks it up
	ab.PutInt64Ordered(0, 100)

	old := ab.GetAndAddInt64(0, 42)
	if old != 100 {
		t.Errorf("GetAndAddInt64 returned old=%d, expected 100", old)
	}
	if v := ab.GetInt64Volatile(0); v != 142 {
		t.Errorf("after GetAndAddInt64, expected 142, got %d", v)
	}
}

func TestGetInt64Volatile(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64Ordered(0, 999)
	if v := ab.GetInt64Volatile(0); v != 999 {
		t.Errorf("expected 999, got %d", v)
	}
}

func TestPutInt64Ordered(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64Ordered(8, -1)
	if v := ab.GetInt64Volatile(8); v != -1 {
		t.Errorf("expected -1, got %d", v)
	}
}

func TestCompareAndSetInt64(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64Ordered(0, 50)

	// Successful CAS
	if !ab.CompareAndSetInt64(0, 50, 99) {
		t.Error("CAS should have succeeded")
	}
	if v := ab.GetInt64Volatile(0); v != 99 {
		t.Errorf("after successful CAS, expected 99, got %d", v)
	}

	// Failed CAS (expected no longer matches)
	if ab.CompareAndSetInt64(0, 50, 200) {
		t.Error("CAS should have failed (expected=50, actual=99)")
	}
	if v := ab.GetInt64Volatile(0); v != 99 {
		t.Errorf("after failed CAS, value should remain 99, got %d", v)
	}
}

func TestInt64MisalignedPanics(t *testing.T) {
	ab := makeBuffer(16)
	tests := []struct {
		name   string
		offset int
		fn     func()
	}{
		{"GetAndAddInt64", 1, func() { ab.GetAndAddInt64(1, 0) }},
		{"GetInt64Volatile", 3, func() { ab.GetInt64Volatile(3) }},
		{"PutInt64Ordered", 5, func() { ab.PutInt64Ordered(5, 0) }},
		{"CompareAndSetInt64", 7, func() { ab.CompareAndSetInt64(7, 0, 0) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s: expected panic for misaligned offset", tt.name)
				}
			}()
			tt.fn()
		})
	}
}

// --- Atomic int32 operations ---

func TestGetAndAddInt32(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt32Ordered(0, 10)

	old := ab.GetAndAddInt32(0, 5)
	if old != 10 {
		t.Errorf("GetAndAddInt32 returned old=%d, expected 10", old)
	}
	if v := ab.GetInt32Volatile(0); v != 15 {
		t.Errorf("after GetAndAddInt32, expected 15, got %d", v)
	}
}

func TestGetInt32Volatile(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt32Ordered(4, 42)
	if v := ab.GetInt32Volatile(4); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestPutInt32Ordered(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt32Ordered(0, -999)
	if v := ab.GetInt32Volatile(0); v != -999 {
		t.Errorf("expected -999, got %d", v)
	}
}

func TestCompareAndSetInt32(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt32Ordered(0, 7)

	if !ab.CompareAndSetInt32(0, 7, 13) {
		t.Error("CAS should have succeeded")
	}
	if v := ab.GetInt32Volatile(0); v != 13 {
		t.Errorf("after successful CAS, expected 13, got %d", v)
	}
	if ab.CompareAndSetInt32(0, 7, 100) {
		t.Error("CAS should have failed")
	}
}

func TestInt32MisalignedPanics(t *testing.T) {
	ab := makeBuffer(16)
	tests := []struct {
		name string
		fn   func()
	}{
		{"GetAndAddInt32", func() { ab.GetAndAddInt32(1, 0) }},
		{"GetInt32Volatile", func() { ab.GetInt32Volatile(1) }},
		{"PutInt32Ordered", func() { ab.PutInt32Ordered(1, 0) }},
		{"CompareAndSetInt32", func() { ab.CompareAndSetInt32(1, 0, 0) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s: expected panic for misaligned offset", tt.name)
				}
			}()
			tt.fn()
		})
	}
}

func TestInt32OutOfBoundsPanics(t *testing.T) {
	ab := makeBuffer(4)
	tests := []struct {
		name string
		fn   func()
	}{
		{"GetAndAddInt32", func() { ab.GetAndAddInt32(4, 0) }},
		{"GetInt32Volatile", func() { ab.GetInt32Volatile(4) }},
		{"PutInt32Ordered", func() { ab.PutInt32Ordered(4, 0) }},
		{"CompareAndSetInt32", func() { ab.CompareAndSetInt32(4, 0, 0) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s: expected panic for out-of-bounds", tt.name)
				}
			}()
			tt.fn()
		})
	}
}

func TestInt64OutOfBoundsPanics(t *testing.T) {
	ab := makeBuffer(8)
	tests := []struct {
		name string
		fn   func()
	}{
		{"GetAndAddInt64", func() { ab.GetAndAddInt64(8, 0) }},
		{"GetInt64Volatile", func() { ab.GetInt64Volatile(8) }},
		{"PutInt64Ordered", func() { ab.PutInt64Ordered(8, 0) }},
		{"CompareAndSetInt64", func() { ab.CompareAndSetInt64(8, 0, 0) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s: expected panic for out-of-bounds", tt.name)
				}
			}()
			tt.fn()
		})
	}
}

// --- Concurrency: GetAndAddInt64 with multiple goroutines ---

func TestGetAndAddInt64Concurrent(t *testing.T) {
	const goroutines = 16
	const increments = 1000
	const expected = int64(goroutines * increments)

	ab := makeBuffer(64)
	ab.PutInt64Ordered(0, 0)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				ab.GetAndAddInt64(0, 1)
			}
		}()
	}
	wg.Wait()

	result := ab.GetInt64Volatile(0)
	if result != expected {
		t.Errorf("concurrent GetAndAddInt64: expected %d, got %d (lost %d updates)", expected, result, expected-result)
	}
}

func TestGetAndAddInt32Concurrent(t *testing.T) {
	const goroutines = 8
	const increments = 500
	const expected = int32(goroutines * increments)

	ab := makeBuffer(64)
	ab.PutInt32Ordered(0, 0)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				ab.GetAndAddInt32(0, 1)
			}
		}()
	}
	wg.Wait()

	result := ab.GetInt32Volatile(0)
	if result != expected {
		t.Errorf("concurrent GetAndAddInt32: expected %d, got %d", expected, result)
	}
}

// --- CompareAndSet concurrency: only one goroutine wins ---

func TestCompareAndSetInt64Concurrent(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64Ordered(0, 0)

	var winners int64
	var wg sync.WaitGroup
	const goroutines = 20

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if ab.CompareAndSetInt64(0, 0, 1) {
				// exactly one goroutine should win
				syncatomic.AddInt64(&winners, 1)
			}
		}()
	}
	wg.Wait()

	if winners != 1 {
		t.Errorf("expected exactly 1 CAS winner, got %d", winners)
	}
}

// --- LE wire format byte-order correctness ---

func TestInt32LEByteOrder(t *testing.T) {
	ab := makeBuffer(8)
	ab.PutInt32LE(0, 0x01020304)
	// little-endian: LSB first
	if ab.buf[0] != 0x04 || ab.buf[1] != 0x03 || ab.buf[2] != 0x02 || ab.buf[3] != 0x01 {
		t.Errorf("LE byte order wrong: got [%02X %02X %02X %02X]",
			ab.buf[0], ab.buf[1], ab.buf[2], ab.buf[3])
	}
}

func TestInt64LEByteOrder(t *testing.T) {
	ab := makeBuffer(16)
	ab.PutInt64LE(0, 0x0102030405060708)
	expected := []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}
	for i, b := range expected {
		if ab.buf[i] != b {
			t.Errorf("byte %d: expected 0x%02X, got 0x%02X", i, b, ab.buf[i])
		}
	}
}

// --- Zero-length slice edge cases ---

func TestGetBytesZeroLength(t *testing.T) {
	ab := makeBuffer(8)
	ab.GetBytes(0, []byte{}) // should not panic
}

func TestPutBytesZeroLength(t *testing.T) {
	ab := makeBuffer(8)
	ab.PutBytes(0, []byte{}) // should not panic
}

// --- Full capacity boundary ---

func TestPutBytesFullCapacity(t *testing.T) {
	ab := makeBuffer(8)
	src := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	ab.PutBytes(0, src)
	dst := make([]byte, 8)
	ab.GetBytes(0, dst)
	for i := range src {
		if dst[i] != src[i] {
			t.Errorf("byte %d: expected %d, got %d", i, src[i], dst[i])
		}
	}
}
