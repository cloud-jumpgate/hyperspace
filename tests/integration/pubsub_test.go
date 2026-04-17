// Package integration contains golden-path integration tests for the Hyperspace
// pub/sub pipeline. Tests use an embedded driver so no external process is required.
package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/client"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// embeddedConfig returns a compact driver configuration for integration tests.
func embeddedConfig() *driver.Config {
	cfg := driver.DefaultConfig()
	// Use minimum term length to keep test memory footprint small.
	cfg.TermLength = logbuffer.MinTermLength // 64 KiB
	// Use Shared threading so all agents run on one goroutine and the conductor
	// processes commands immediately without scheduling jitter.
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.SenderIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(100 * time.Microsecond)
	return &cfg
}

// TestPubSub_GoldenPath_1000Messages is the canonical integration test for the
// Hyperspace client library. It exercises the full round-trip: embed a driver,
// create a client, add a publication and a subscription on the same channel and
// stream, publish 1000 messages, poll until all are received, verify contents,
// and close cleanly.
func TestPubSub_GoldenPath_1000Messages(t *testing.T) {
	ctx := context.Background()
	cfg := embeddedConfig()

	// 1. Create an embedded Client.
	c, err := client.NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	const channel = "hs:quic?endpoint=127.0.0.1:0|pool=1"
	const streamID = int32(1001)
	const messageCount = 1000

	// 2. Add a publication.
	pub, err := c.AddPublication(ctx, channel, streamID)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	t.Logf("publication ready: sessionID=%d streamID=%d", pub.SessionID(), pub.StreamID())

	// 3. Add a subscription.
	sub, err := c.AddSubscription(ctx, channel, streamID)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	t.Logf("subscription ready: streamID=%d", sub.StreamID())

	// Wire the publication's log buffer into the subscription. In the full stack
	// this is done by the Receiver when it processes incoming QUIC frames. In the
	// embedded integration path we wire it directly so we can test the full
	// offer→poll pipeline without a network stack.
	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	// 4. Publish messageCount messages.
	expected := make([][]byte, messageCount)
	for i := range messageCount {
		expected[i] = fmt.Appendf(nil, "hyperspace-message-%06d", i)
	}

	published := 0
	for published < messageCount {
		msg := expected[published]
		result, err := pub.Offer(msg)
		if err != nil {
			t.Fatalf("Offer[%d]: %v", published, err)
		}
		if result == logbuffer.AppendBackPressure {
			// Yield and retry.
			time.Sleep(10 * time.Microsecond)
			continue
		}
		if result == logbuffer.AppendRotation {
			// Rotation was triggered; retry the same message on the new term.
			time.Sleep(10 * time.Microsecond)
			continue
		}
		published++
	}
	t.Logf("published %d messages", published)

	// 5. Poll until all messageCount messages are received, with a timeout.
	received := make([][]byte, 0, messageCount)
	var receivedCount atomic.Int32

	handler := func(buf []byte, offset, length int, _ client.FragmentHeader) {
		payload := make([]byte, length)
		copy(payload, buf[offset:offset+length])
		received = append(received, payload)
		receivedCount.Add(1)
	}

	deadline := time.Now().Add(10 * time.Second)
	for int(receivedCount.Load()) < messageCount {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: received %d/%d messages after 10s",
				receivedCount.Load(), messageCount)
		}
		sub.Poll(handler, 100)
		if int(receivedCount.Load()) < messageCount {
			time.Sleep(100 * time.Microsecond)
		}
	}

	t.Logf("received %d messages", receivedCount.Load())

	// 6. Verify message contents.
	if len(received) != messageCount {
		t.Fatalf("received %d messages, want %d", len(received), messageCount)
	}
	for i, msg := range received {
		if !bytes.Equal(msg, expected[i]) {
			t.Errorf("message[%d]: got %q, want %q", i, msg, expected[i])
			if i > 10 {
				t.Logf("... stopping mismatch log at message 10")
				break
			}
		}
	}

	// 7. Close cleanly.
	if err := pub.Close(); err != nil {
		t.Errorf("pub.Close: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Errorf("sub.Close: %v", err)
	}
}

// TestPubSub_MultipleStreams verifies that independent stream IDs do not interfere.
func TestPubSub_MultipleStreams(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, embeddedConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	const channel = "hs:quic?endpoint=127.0.0.1:0|pool=1"
	const numStreams = 3
	const msgsPerStream = 100

	pubs := make([]*client.Publication, numStreams)
	subs := make([]*client.Subscription, numStreams)

	for i := range numStreams {
		streamID := int32(2000 + i)
		pub, err := c.AddPublication(ctx, channel, streamID)
		if err != nil {
			t.Fatalf("stream %d AddPublication: %v", i, err)
		}
		sub, err := c.AddSubscription(ctx, channel, streamID)
		if err != nil {
			t.Fatalf("stream %d AddSubscription: %v", i, err)
		}
		if err := sub.AddImageFromConductor(pub); err != nil {
			t.Fatalf("stream %d AddImageFromConductor: %v", i, err)
		}
		pubs[i] = pub
		subs[i] = sub
	}

	// Publish to each stream.
	for i, pub := range pubs {
		for j := range msgsPerStream {
			msg := fmt.Appendf(nil, "stream-%d-msg-%d", i, j)
			for {
				result, err := pub.Offer(msg)
				if err != nil {
					t.Fatalf("stream %d Offer: %v", i, err)
				}
				if result >= 0 {
					break
				}
				time.Sleep(10 * time.Microsecond)
			}
		}
	}

	// Poll each stream.
	for i, sub := range subs {
		counts := 0
		deadline := time.Now().Add(5 * time.Second)
		for counts < msgsPerStream {
			if time.Now().After(deadline) {
				t.Fatalf("stream %d: timeout after receiving %d/%d", i, counts, msgsPerStream)
			}
			n := sub.Poll(func(_ []byte, _, _ int, _ client.FragmentHeader) {
				counts++
			}, msgsPerStream)
			if n == 0 {
				time.Sleep(100 * time.Microsecond)
			}
		}
		t.Logf("stream %d: received %d messages", i, counts)
	}
}

// TestPubSub_CloseWhilePublishing verifies that closing a publication while
// goroutines are still calling Offer does not panic.
func TestPubSub_CloseWhilePublishing(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, embeddedConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	pub, err := c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:0|pool=1", 3001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = pub.Offer([]byte("racing"))
			}
		}
	}()

	time.Sleep(5 * time.Millisecond)
	_ = pub.Close()
	stop <- struct{}{} // signal goroutine to stop
	<-stop
}

// TestPubSub_SubscriptionImages verifies that Images() returns the correct set.
func TestPubSub_SubscriptionImages(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, embeddedConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	pub, err := c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:0|pool=1", 4001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	sub, err := c.AddSubscription(ctx, "hs:quic?endpoint=127.0.0.1:0|pool=1", 4001)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if len(sub.Images()) != 0 {
		t.Fatalf("expected 0 images before wire, got %d", len(sub.Images()))
	}

	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	images := sub.Images()
	if len(images) != 1 {
		t.Fatalf("expected 1 image after wire, got %d", len(images))
	}
	if images[0].SessionID() != pub.SessionID() {
		t.Errorf("image sessionID: got %d, want %d", images[0].SessionID(), pub.SessionID())
	}
	if images[0].IsClosed() {
		t.Error("image should not be closed")
	}
}

// TestPubSub_LargeMessages verifies that messages near the term capacity work.
func TestPubSub_LargeMessages(t *testing.T) {
	ctx := context.Background()
	cfg := embeddedConfig()
	c, err := client.NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	const channel = "hs:quic?endpoint=127.0.0.1:0|pool=1"
	const streamID = int32(5001)

	pub, err := c.AddPublication(ctx, channel, streamID)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}
	sub, err := c.AddSubscription(ctx, channel, streamID)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	// 1 KiB message — well within a 64 KiB term but large enough to exercise framing.
	largeMsg := bytes.Repeat([]byte("X"), 1024)

	const count = 10
	for i := range count {
		for {
			result, err := pub.Offer(largeMsg)
			if err != nil {
				t.Fatalf("Offer[%d]: %v", i, err)
			}
			if result >= 0 {
				break
			}
			time.Sleep(10 * time.Microsecond)
		}
	}

	received := 0
	deadline := time.Now().Add(5 * time.Second)
	for received < count {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: received %d/%d large messages", received, count)
		}
		n := sub.Poll(func(buf []byte, offset, length int, _ client.FragmentHeader) {
			if length != len(largeMsg) {
				t.Errorf("large message length: got %d, want %d", length, len(largeMsg))
			}
			received++
		}, 10)
		if n == 0 {
			time.Sleep(100 * time.Microsecond)
		}
	}
}

// TestPubSub_ClientCloseStopsAll verifies that closing the client cleans up
// all publications and subscriptions and does not hang.
func TestPubSub_ClientCloseStopsAll(t *testing.T) {
	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, embeddedConfig())
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	// Create several publications and subscriptions.
	for i := range 3 {
		if _, err := c.AddPublication(ctx, fmt.Sprintf("hs:quic?endpoint=127.0.0.1:%d|pool=1", 6000+i), int32(i+1)); err != nil {
			t.Fatalf("AddPublication %d: %v", i, err)
		}
		if _, err := c.AddSubscription(ctx, fmt.Sprintf("hs:quic?endpoint=127.0.0.1:%d|pool=1", 6000+i), int32(i+1)); err != nil {
			t.Fatalf("AddSubscription %d: %v", i, err)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Close()
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("Client.Close did not complete within 5s")
	}
}
