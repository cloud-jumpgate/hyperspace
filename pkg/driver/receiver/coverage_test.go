package receiver_test

// coverage_test.go adds targeted tests for branches not covered by receiver_test.go.

import (
	"context"
	"testing"
	"time"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/receiver"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
)

// newCovTestConductor builds a minimal conductor for coverage tests.
func newCovTestConductor(t *testing.T) *conductor.Conductor {
	t.Helper()
	const ringSize = (1 << 12) + 128
	const bcastSize = 8*520 + 128
	toRaw := make([]byte, ringSize)
	fromRaw := make([]byte, bcastSize)
	toAtomic := atomicbuf.NewAtomicBuffer(toRaw)
	fromAtomic := atomicbuf.NewAtomicBuffer(fromRaw)
	cond, err := conductor.New(toAtomic, fromAtomic, logbuffer.MinTermLength)
	if err != nil {
		t.Fatalf("conductor.New: %v", err)
	}
	_, err = ringbuffer.NewManyToOneRingBuffer(toAtomic)
	if err != nil {
		t.Fatalf("NewManyToOneRingBuffer: %v", err)
	}
	return cond
}

// --- RemoveImageBySessionID ---

// TestRemoveImageBySessionID_RemovesAllStreamsForSession verifies that
// RemoveImageBySessionID deletes all images keyed by the given sessionID,
// regardless of streamID.
func TestRemoveImageBySessionID_RemovesAllStreamsForSession(t *testing.T) {
	cond := newCovTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Create three images: session 10 with streams 100, 200, and session 20 with stream 100.
	mc.EnqueueFrame(buildFrame(10, 100, 0, []byte("s10-str100")))
	mc.EnqueueFrame(buildFrame(10, 200, 0, []byte("s10-str200")))
	mc.EnqueueFrame(buildFrame(20, 100, 0, []byte("s20-str100")))
	rcv.DoWork(context.Background())

	if rcv.ImageCount() != 3 {
		t.Fatalf("expected 3 images, got %d", rcv.ImageCount())
	}

	// RemoveImageBySessionID(10) must remove both stream 100 and stream 200 for session 10.
	rcv.RemoveImageBySessionID(10)

	if rcv.ImageCount() != 1 {
		t.Fatalf("expected 1 image (session 20 only), got %d", rcv.ImageCount())
	}
}

// TestRemoveImageBySessionID_EmptyMap verifies no panic on an empty image map.
func TestRemoveImageBySessionID_EmptyMap(t *testing.T) {
	cond := newCovTestConductor(t)
	rcv := receiver.New(cond, 1200) // no images yet
	rcv.RemoveImageBySessionID(42)
	if rcv.ImageCount() != 0 {
		t.Fatalf("expected 0, got %d", rcv.ImageCount())
	}
}

// TestRemoveImageBySessionID_NonexistentSession verifies that removing a session
// that does not exist in the map leaves other sessions intact.
func TestRemoveImageBySessionID_NonexistentSession(t *testing.T) {
	cond := newCovTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	mc.EnqueueFrame(buildFrame(99, 100, 0, []byte("data")))
	rcv.DoWork(context.Background())

	// Removing a non-existent session should leave the existing image untouched.
	rcv.RemoveImageBySessionID(42)
	if rcv.ImageCount() != 1 {
		t.Fatalf("expected 1 image, got %d", rcv.ImageCount())
	}
}

// --- Periodic eviction via DoWork ---

// TestDoWork_PeriodicEvictionTriggeredByDoWorkCount verifies the branch inside
// DoWork that evicts stale images after evictionCheckInterval (1000) calls.
func TestDoWork_PeriodicEvictionTriggeredByDoWorkCount(t *testing.T) {
	cond := newCovTestConductor(t)
	rcv := receiver.New(cond, 1200)

	past := time.Now().Add(-2 * time.Hour)
	rcv.SetNowFunc(func() time.Time { return past })
	rcv.SetImageTTL(1 * time.Second)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Create an image that will be stale when the clock is restored to now.
	mc.EnqueueFrame(buildFrame(55, 100, 0, []byte("stale")))
	rcv.DoWork(context.Background())

	if rcv.ImageCount() != 1 {
		t.Fatalf("expected 1 image after first DoWork, got %d", rcv.ImageCount())
	}

	// Restore clock to now so images are stale relative to their creation time.
	now := time.Now()
	rcv.SetNowFunc(func() time.Time { return now })

	// Call DoWork 1000 times without enqueueing more frames.
	// The 1000th call will hit the evictionCheckInterval branch and evict the stale image.
	for range 1000 {
		rcv.DoWork(context.Background())
	}

	if rcv.ImageCount() != 0 {
		t.Fatalf("expected 0 images after periodic eviction, got %d", rcv.ImageCount())
	}
}

// --- processFrame: negative termOffset ---

// TestProcessFrame_NegativeTermOffsetIsDiscarded exercises the guard in
// processFrame that discards frames with negative termOffset values.
func TestProcessFrame_NegativeTermOffsetIsDiscarded(t *testing.T) {
	cond := newCovTestConductor(t)
	rcv := receiver.New(cond, 1200)

	mc := newMockConn(1)
	p := pool.New("peer1", 1, 4)
	if err := p.Add(mc); err != nil {
		t.Fatalf("pool.Add: %v", err)
	}
	rcv.AddPool("peer1", p)

	// Build a valid frame, then overwrite termOffset (bytes 8–11) with
	// 0xFFFFFFFF, which is -1 as int32.
	frame := buildFrame(11, 100, 0, []byte("data"))
	frame[8] = 0xFF
	frame[9] = 0xFF
	frame[10] = 0xFF
	frame[11] = 0xFF
	mc.EnqueueFrame(frame)

	n := rcv.DoWork(context.Background())
	if n != 0 {
		t.Fatalf("expected 0 (negative termOffset discarded), got %d", n)
	}
}
