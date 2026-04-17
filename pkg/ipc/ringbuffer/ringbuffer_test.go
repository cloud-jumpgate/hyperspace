package ringbuffer

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
)

// makeRingBuffer creates a ring with the given capacity (must be power of 2).
// Total buffer size = capacity + 128 (trailer).
func makeRingBuffer(capacity int) *atomicbuf.AtomicBuffer {
	return atomicbuf.NewAtomicBuffer(make([]byte, capacity+trailerLength))
}

// collectMessages is a helper that collects messages into a slice.
func collectMessages(rb interface {
	Read(handler MessageHandler, maxMessages int) int
}, maxMessages int) [][]byte {
	var results [][]byte
	rb.Read(func(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
		data := make([]byte, length)
		buf.GetBytes(offset, data)
		results = append(results, data)
	}, maxMessages)
	return results
}

// --- Helpers and validation ---

func TestIsPowerOfTwo(t *testing.T) {
	cases := []struct {
		n    int
		want bool
	}{
		{0, false},
		{1, true},
		{2, true},
		{3, false},
		{4, true},
		{1024, true},
		{1023, false},
	}
	for _, c := range cases {
		if got := isPowerOfTwo(c.n); got != c.want {
			t.Errorf("isPowerOfTwo(%d) = %v, want %v", c.n, got, c.want)
		}
	}
}

func TestAlignedLength(t *testing.T) {
	cases := []struct {
		n, want int
	}{
		{0, 0},
		{1, 8},
		{8, 8},
		{9, 16},
		{16, 16},
		{17, 24},
	}
	for _, c := range cases {
		if got := alignedLength(c.n); got != c.want {
			t.Errorf("alignedLength(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

// --- OneToOneRingBuffer construction ---

func TestNewOneToOneRingBufferValid(t *testing.T) {
	buf := makeRingBuffer(1024)
	rb, err := NewOneToOneRingBuffer(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rb.Capacity() != 1024 {
		t.Errorf("expected capacity 1024, got %d", rb.Capacity())
	}
}

func TestNewOneToOneRingBufferInvalidSize(t *testing.T) {
	// Invalid: size 0, 100 (< trailer), 127 (< trailer), 255 (capacity=127, not power-of-2),
	// 256+128-1=383 (capacity=255, not power-of-2).
	// Note: 129 = 1+128 is VALID (capacity=1 which is 2^0).
	cases := []int{0, 100, 127, 255, 256 + 128 - 1}
	for _, size := range cases {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			buf := atomicbuf.NewAtomicBuffer(make([]byte, size))
			_, err := NewOneToOneRingBuffer(buf)
			if err == nil {
				t.Errorf("expected error for size %d", size)
			}
		})
	}
}

func TestNewManyToOneRingBufferInvalidSize(t *testing.T) {
	buf := atomicbuf.NewAtomicBuffer(make([]byte, 300))
	_, err := NewManyToOneRingBuffer(buf)
	if err == nil {
		t.Error("expected error for non-power-of-2 capacity")
	}
}

// --- OneToOneRingBuffer basic write/read ---

func TestOneToOneWriteRead(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(1024))

	msg := []byte("hello, hyperspace!")
	if !rb.Write(1, msg) {
		t.Fatal("Write failed")
	}
	if rb.Size() == 0 {
		t.Error("Size should be > 0 after write")
	}

	results := collectMessages(rb, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 message, got %d", len(results))
	}
	if string(results[0]) != string(msg) {
		t.Errorf("expected %q, got %q", msg, results[0])
	}
}

func TestOneToOneMultipleMessages(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(4096))

	messages := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for i, m := range messages {
		if !rb.Write(int32(i+1), []byte(m)) {
			t.Fatalf("Write(%d) failed", i)
		}
	}

	var got []string
	rb.Read(func(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
		data := make([]byte, length)
		buf.GetBytes(offset, data)
		got = append(got, string(data))
	}, 100)

	if len(got) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(got))
	}
	for i, m := range messages {
		if got[i] != m {
			t.Errorf("message %d: expected %q, got %q", i, m, got[i])
		}
	}
}

func TestOneToOneEmptyRead(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(1024))
	n := rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) {}, 10)
	if n != 0 {
		t.Errorf("expected 0 messages from empty buffer, got %d", n)
	}
}

func TestOneToOneSize(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(1024))

	if rb.Size() != 0 {
		t.Errorf("initial size should be 0, got %d", rb.Size())
	}

	rb.Write(1, []byte("test"))
	if rb.Size() == 0 {
		t.Error("size should be > 0 after write")
	}

	rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) {}, 10)
	if rb.Size() != 0 {
		t.Errorf("size should return to 0 after read, got %d", rb.Size())
	}
}

// --- Back-pressure: buffer full ---

func TestOneToOneBackPressure(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(128))

	// Each write uses at least RecordDescriptorHeaderLength + payload aligned to 8 bytes.
	// With capacity=128 we can fit a limited number.
	msg := make([]byte, 56) // 8 + 56 = 64 bytes per record (aligned)

	if !rb.Write(1, msg) {
		t.Fatal("first write should succeed")
	}
	if !rb.Write(2, msg) {
		t.Fatal("second write should succeed")
	}
	// Third write should fail (128 bytes used)
	if rb.Write(3, msg) {
		t.Error("third write should fail (buffer full)")
	}
}

func TestOneToOneMessageTooLarge(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(256))
	huge := make([]byte, 512)
	if rb.Write(1, huge) {
		t.Error("write of oversized message should fail")
	}
}

// --- Ring wrap-around ---

func TestOneToOneWrapAround(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(512))

	const msgSize = 48 // 8 + 48 = 56 -> aligned to 64

	// Fill roughly half the ring, drain it, then fill again to force wrap
	for round := 0; round < 3; round++ {
		for i := 0; i < 4; i++ {
			payload := make([]byte, msgSize)
			payload[0] = byte(round*10 + i)
			if !rb.Write(int32(i+1), payload) {
				t.Fatalf("round %d write %d failed", round, i)
			}
		}
		var count int
		rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) { count++ }, 100)
		if count != 4 {
			t.Errorf("round %d: expected 4 messages, got %d", round, count)
		}
	}
}

// --- maxMessages limit in Read ---

func TestOneToOneMaxMessages(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(4096))

	for i := 0; i < 10; i++ {
		rb.Write(1, []byte("x"))
	}

	n := rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) {}, 3)
	if n != 3 {
		t.Errorf("expected 3 messages (maxMessages=3), got %d", n)
	}
}

// --- Zero-length messages ---

func TestOneToOneZeroLengthMessage(t *testing.T) {
	rb, _ := NewOneToOneRingBuffer(makeRingBuffer(256))
	if !rb.Write(42, []byte{}) {
		t.Fatal("zero-length write should succeed")
	}

	var msgTypeReceived int32
	var msgLen int
	rb.Read(func(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
		msgTypeReceived = msgTypeID
		msgLen = length
	}, 10)

	if msgTypeReceived != 42 {
		t.Errorf("expected msgTypeID 42, got %d", msgTypeReceived)
	}
	if msgLen != 0 {
		t.Errorf("expected zero-length message, got length %d", msgLen)
	}
}

// --- ManyToOneRingBuffer construction ---

func TestNewManyToOneRingBufferValid(t *testing.T) {
	buf := makeRingBuffer(2048)
	rb, err := NewManyToOneRingBuffer(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rb.Capacity() != 2048 {
		t.Errorf("expected capacity 2048, got %d", rb.Capacity())
	}
}

// --- ManyToOneRingBuffer basic write/read ---

func TestManyToOneBasicWriteRead(t *testing.T) {
	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(1024))

	if !rb.Write(7, []byte("mpsc test")) {
		t.Fatal("Write failed")
	}

	results := collectMessages(rb, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 message, got %d", len(results))
	}
	if string(results[0]) != "mpsc test" {
		t.Errorf("expected 'mpsc test', got %q", string(results[0]))
	}
}

func TestManyToOneEmptyRead(t *testing.T) {
	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(1024))
	n := rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) {}, 10)
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestManyToOneSize(t *testing.T) {
	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(1024))
	if rb.Size() != 0 {
		t.Errorf("initial size should be 0")
	}
	rb.Write(1, []byte("data"))
	if rb.Size() == 0 {
		t.Error("size should be > 0 after write")
	}
}

func TestManyToOneBackPressure(t *testing.T) {
	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(128))
	msg := make([]byte, 56) // 64 bytes aligned

	if !rb.Write(1, msg) {
		t.Fatal("first write should succeed")
	}
	if !rb.Write(2, msg) {
		t.Fatal("second write should succeed")
	}
	if rb.Write(3, msg) {
		t.Error("third write should fail (buffer full)")
	}
}

func TestManyToOneMessageTooLarge(t *testing.T) {
	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(256))
	huge := make([]byte, 512)
	if rb.Write(1, huge) {
		t.Error("oversized message should fail")
	}
}

// --- ManyToOneRingBuffer concurrent writes (10 goroutines × 1000 messages) ---
//
// Strategy: all producers write into a large ring (no wrap, no back-pressure),
// then wg.Wait() ensures all producers have committed, then a single consumer
// drains. This avoids the consumer spinning on uncommitted records.

func TestManyToOneConcurrentWrites(t *testing.T) {
	const goroutines = 10
	const msgsPerGoroutine = 1000
	const totalMessages = goroutines * msgsPerGoroutine

	// Each record: alignedLength(8 + 8) = 16 bytes.
	// Total: 10 * 1000 * 16 = 160_000 bytes. Use 256 KB ring (262144) + trailer.
	rb, err := NewManyToOneRingBuffer(makeRingBuffer(1 << 18)) // 256 KB ring
	if err != nil {
		t.Fatalf("NewManyToOneRingBuffer: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			payload := make([]byte, 8)
			for i := 0; i < msgsPerGoroutine; i++ {
				payload[0] = byte(g)
				payload[1] = byte(i >> 8)
				payload[2] = byte(i & 0xFF)
				for !rb.Write(int32(g+1), payload) {
					// Back-pressure spin: shouldn't trigger with 256KB ring for 160KB data
					runtime.Gosched()
				}
			}
		}()
	}

	// Wait for ALL producers to finish committing before consuming.
	// This guarantees no uncommitted slots when we start reading.
	wg.Wait()

	// Single-consumer drain (no concurrent Read calls).
	var received int
	for received < totalMessages {
		n := rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) {
			received++
		}, 200)
		if n == 0 && received < totalMessages {
			// No progress — yield and retry; all producers are done so this resolves quickly
			runtime.Gosched()
		}
	}

	if received != totalMessages {
		t.Errorf("concurrent write/read: expected %d messages, got %d (lost %d)", totalMessages, received, totalMessages-received)
	}
}

// --- ManyToOneRingBuffer no duplicate messages ---

func TestManyToOneNoDuplicates(t *testing.T) {
	const goroutines = 5
	const msgsPerGoroutine = 200
	const totalMessages = goroutines * msgsPerGoroutine

	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(1 << 18))

	type msgKey struct{ goroutine, index int }
	seen := make(map[msgKey]int)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			payload := make([]byte, 8)
			for i := 0; i < msgsPerGoroutine; i++ {
				payload[0] = byte(g)
				payload[1] = byte(i >> 8)
				payload[2] = byte(i & 0xFF)
				for !rb.Write(int32(g+1), payload) {
				}
			}
		}()
	}
	wg.Wait()

	// Single-threaded drain
	var total int
	for total < totalMessages {
		rb.Read(func(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
			data := make([]byte, length)
			buf.GetBytes(offset, data)
			key := msgKey{int(data[0]), int(data[1])<<8 | int(data[2])}
			seen[key]++
			total++
		}, 100)
	}

	for key, count := range seen {
		if count != 1 {
			t.Errorf("message {goroutine=%d, index=%d} seen %d times (want 1)", key.goroutine, key.index, count)
		}
	}
	if len(seen) != totalMessages {
		t.Errorf("expected %d unique messages, got %d", totalMessages, len(seen))
	}
}

// --- ManyToOne wrap-around ---

func TestManyToOneWrapAround(t *testing.T) {
	rb, _ := NewManyToOneRingBuffer(makeRingBuffer(512))
	const msgSize = 48

	for round := 0; round < 5; round++ {
		for i := 0; i < 4; i++ {
			payload := make([]byte, msgSize)
			payload[0] = byte(round*10 + i)
			for !rb.Write(1, payload) {
			}
		}
		var count int
		rb.Read(func(int32, *atomicbuf.AtomicBuffer, int, int) { count++ }, 100)
		if count != 4 {
			t.Errorf("round %d: expected 4 messages, got %d", round, count)
		}
	}
}
