package client_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/client"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// testConfig returns a small-footprint driver config suitable for unit tests.
func testConfig() *driver.Config {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength // 64 KiB
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(500 * time.Microsecond)
	cfg.SenderIdle = driver.NewSleepIdle(500 * time.Microsecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(500 * time.Microsecond)
	return &cfg
}

// newTestClient creates an embedded Client for tests and registers a cleanup
// function to close it.
func newTestClient(t *testing.T) *client.Client {
	t.Helper()
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, testConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// --- NewEmbedded ---

func TestNewEmbedded_Succeeds(t *testing.T) {
	c := newTestClient(t)
	if c == nil {
		t.Fatal("expected non-nil Client")
	}
}

func TestNewEmbedded_NilConfigUsesDefaults(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, nil)
	if err != nil {
		t.Fatalf("NewEmbedded with nil config: %v", err)
	}
	defer func() { _ = c.Close() }()
}

// --- NewExternal ---

func TestNewExternal_ReturnsNotImplemented(t *testing.T) {
	_, err := client.NewExternal(context.Background(), "/tmp/test-cnc.dat")
	if err == nil {
		t.Fatal("expected error for NewExternal (not yet implemented)")
	}
}

// --- AddPublication ---

func TestAddPublication_ReturnsPublication(t *testing.T) {
	c := newTestClient(t)

	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	if pub == nil {
		t.Fatal("expected non-nil Publication")
	}
}

func TestAddPublication_CorrectChannelAndStreamID(t *testing.T) {
	c := newTestClient(t)
	channel := "hs:quic?endpoint=127.0.0.1:7777|pool=1"
	streamID := int32(42)

	pub, err := c.AddPublication(context.Background(), channel, streamID)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	if pub.Channel() != channel {
		t.Errorf("Channel: got %q, want %q", pub.Channel(), channel)
	}
	if pub.StreamID() != streamID {
		t.Errorf("StreamID: got %d, want %d", pub.StreamID(), streamID)
	}
	if pub.SessionID() == 0 {
		t.Error("expected non-zero SessionID")
	}
}

func TestAddPublication_NotClosed(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	if pub.IsClosed() {
		t.Error("expected Publication to not be closed after creation")
	}
}

func TestAddPublication_ContextCancelled(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err == nil {
		t.Fatal("expected error when context is already cancelled")
	}
}

func TestAddPublication_MultiplePublications(t *testing.T) {
	c := newTestClient(t)
	const n = 5

	pubs := make([]*client.Publication, n)
	for i := range pubs {
		pub, err := c.AddPublication(
			context.Background(),
			fmt.Sprintf("hs:quic?endpoint=127.0.0.1:%d|pool=1", 7000+i),
			int32(i+1),
		)
		if err != nil {
			t.Fatalf("AddPublication[%d]: %v", i, err)
		}
		pubs[i] = pub
	}

	// All session IDs should be non-zero.
	for i, pub := range pubs {
		if pub.SessionID() == 0 {
			t.Errorf("pubs[%d]: expected non-zero SessionID", i)
		}
	}
}

// --- AddSubscription ---

func TestAddSubscription_ReturnsSubscription(t *testing.T) {
	c := newTestClient(t)

	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil Subscription")
	}
}

func TestAddSubscription_CorrectChannelAndStreamID(t *testing.T) {
	c := newTestClient(t)
	channel := "hs:quic?endpoint=127.0.0.1:7777|pool=1"
	streamID := int32(99)

	sub, err := c.AddSubscription(context.Background(), channel, streamID)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if sub.Channel() != channel {
		t.Errorf("Channel: got %q, want %q", sub.Channel(), channel)
	}
	if sub.StreamID() != streamID {
		t.Errorf("StreamID: got %d, want %d", sub.StreamID(), streamID)
	}
}

func TestAddSubscription_NotClosed(t *testing.T) {
	c := newTestClient(t)
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	if sub.IsClosed() {
		t.Error("expected Subscription to not be closed after creation")
	}
}

func TestAddSubscription_ContextCancelled(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.AddSubscription(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

// --- Close ---

func TestClose_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, testConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestClose_RejectsNewOperations(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, testConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	_ = c.Close()

	_, err = c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err == nil {
		t.Error("expected error after Close")
	}

	_, err = c.AddSubscription(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err == nil {
		t.Error("expected error after Close")
	}
}

// --- Publication ---

func TestPublication_Offer_Success(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	result, err := pub.Offer([]byte("hello world"))
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if result < 0 {
		t.Errorf("Offer returned negative result %d (back pressure or rotation)", result)
	}
}

func TestPublication_Offer_EmptyPayload(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	result, err := pub.Offer([]byte{})
	if err != nil {
		t.Fatalf("Offer empty: %v", err)
	}
	_ = result
}

func TestPublication_Offer_ClosedReturnsError(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	_ = pub.Close()
	_, err = pub.Offer([]byte("data"))
	if err == nil {
		t.Error("expected error offering on closed publication")
	}
}

func TestPublication_Close_IsIdempotent(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	if err := pub.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !pub.IsClosed() {
		t.Error("expected IsClosed to be true")
	}
}

// --- Subscription ---

func TestSubscription_Poll_EmptyReturnsZero(t *testing.T) {
	c := newTestClient(t)
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	n := sub.Poll(func(_ []byte, _, _ int, _ client.FragmentHeader) {}, 10)
	if n != 0 {
		t.Errorf("expected 0 fragments on empty subscription, got %d", n)
	}
}

func TestSubscription_Images_EmptyInitially(t *testing.T) {
	c := newTestClient(t)
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	images := sub.Images()
	if len(images) != 0 {
		t.Errorf("expected 0 images initially, got %d", len(images))
	}
}

func TestSubscription_AddImage_AddsImage(t *testing.T) {
	c := newTestClient(t)

	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	images := sub.Images()
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].SessionID() != pub.SessionID() {
		t.Errorf("Image SessionID: got %d, want %d", images[0].SessionID(), pub.SessionID())
	}
}

func TestSubscription_Poll_DeliversMessages(t *testing.T) {
	c := newTestClient(t)

	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	msg := []byte("test message")
	if _, err := pub.Offer(msg); err != nil {
		t.Fatalf("Offer: %v", err)
	}

	var received []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n := sub.Poll(func(buf []byte, offset, length int, _ client.FragmentHeader) {
			received = make([]byte, length)
			copy(received, buf[offset:offset+length])
		}, 10)
		if n > 0 {
			break
		}
		time.Sleep(100 * time.Microsecond)
	}

	if string(received) != string(msg) {
		t.Errorf("Poll: got %q, want %q", received, msg)
	}
}

func TestSubscription_Close_IsIdempotent(t *testing.T) {
	c := newTestClient(t)
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !sub.IsClosed() {
		t.Error("expected IsClosed to be true")
	}
}

func TestSubscription_Poll_ClosedReturnsZero(t *testing.T) {
	c := newTestClient(t)
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	_ = sub.Close()

	n := sub.Poll(func(_ []byte, _, _ int, _ client.FragmentHeader) {}, 10)
	if n != 0 {
		t.Errorf("expected 0 from Poll on closed subscription, got %d", n)
	}
}

// --- Image ---

func TestImage_Properties(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	images := sub.Images()
	if len(images) != 1 {
		t.Fatalf("expected 1 image")
	}
	img := images[0]
	if img.SessionID() != pub.SessionID() {
		t.Errorf("Image.SessionID: got %d, want %d", img.SessionID(), pub.SessionID())
	}
	if img.IsClosed() {
		t.Error("expected Image to not be closed")
	}
	pos := img.Position()
	if pos < 0 {
		t.Errorf("Position should be >= 0, got %d", pos)
	}
}

func TestImage_Poll_ClosedReturnsZero(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	_ = sub.Close() // closes images

	images := sub.Images()
	if len(images) != 1 {
		t.Fatalf("expected 1 image")
	}
	n := images[0].Poll(func(_ []byte, _, _ int, _ client.FragmentHeader) {}, 10)
	if n != 0 {
		t.Errorf("expected 0 from closed image Poll, got %d", n)
	}
}

// --- Concurrent safety ---

func TestOffer_ConcurrentSafe(t *testing.T) {
	c := newTestClient(t)
	pub, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	const goroutines = 8
	const offersPerGoroutine = 50

	done := make(chan struct{}, goroutines)
	for g := range goroutines {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for i := range offersPerGoroutine {
				msg := fmt.Appendf(nil, "goroutine %d message %d", id, i)
				_, _ = pub.Offer(msg)
			}
		}(g)
	}

	for range goroutines {
		<-done
	}
}

func TestPoll_ConcurrentSafe(t *testing.T) {
	c := newTestClient(t)
	_, err := c.AddPublication(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	sub, err := c.AddSubscription(context.Background(), "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	const goroutines = 4
	done := make(chan struct{}, goroutines)
	for range goroutines {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 50 {
				sub.Poll(func(_ []byte, _, _ int, _ client.FragmentHeader) {}, 10)
				time.Sleep(time.Microsecond)
			}
		}()
	}

	for range goroutines {
		<-done
	}
}

func TestAddPublication_ConcurrentSafe(t *testing.T) {
	c := newTestClient(t)
	const goroutines = 5

	errs := make(chan error, goroutines)
	for i := range goroutines {
		go func(id int) {
			_, err := c.AddPublication(
				context.Background(),
				fmt.Sprintf("hs:quic?endpoint=127.0.0.1:%d|pool=1", 8000+id),
				int32(id+1),
			)
			errs <- err
		}(i)
	}

	for range goroutines {
		if err := <-errs; err != nil {
			t.Errorf("concurrent AddPublication error: %v", err)
		}
	}
}
