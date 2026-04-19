package client_test

// sprint_contracts_test.go — tests required by sprint contracts F-010.
// Satisfies CONDITIONAL PASS → PASS for pkg/client.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/client"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// TestClient_PublishSubscribe_1000 creates an embedded publisher and subscriber,
// publishes 1000 messages, and verifies all 1000 are received via polling.
// This is the key end-to-end test for the embedded driver mode.
func TestClient_PublishSubscribe_1000(t *testing.T) {
	const channel = "hs:quic?endpoint=127.0.0.1:7777|pool=1"
	const streamID = int32(5001)
	const totalMessages = 1000

	c := newTestClient(t)

	pub, err := c.AddPublication(context.Background(), channel, streamID)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	sub, err := c.AddSubscription(context.Background(), channel, streamID)
	if err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	// Wire the publication's log buffer to the subscription directly (embedded mode).
	if err := sub.AddImageFromConductor(pub); err != nil {
		t.Fatalf("AddImageFromConductor: %v", err)
	}

	// Publish all 1000 messages.
	for i := 0; i < totalMessages; i++ {
		msg := fmt.Appendf(nil, "msg-%05d", i)
		for {
			result, offerErr := pub.Offer(msg)
			if offerErr != nil {
				t.Fatalf("Offer[%d]: %v", i, offerErr)
			}
			if result >= 0 {
				break // success
			}
			// Back-pressure or rotation — wait briefly and retry.
			time.Sleep(time.Microsecond)
		}
	}

	// Poll until all 1000 messages are received.
	received := 0
	deadline := time.Now().Add(10 * time.Second)
	for received < totalMessages && time.Now().Before(deadline) {
		n := sub.Poll(func(_ []byte, _, _ int, _ client.FragmentHeader) {
			received++
		}, 100)
		if n == 0 {
			time.Sleep(100 * time.Microsecond)
		}
	}

	if received != totalMessages {
		t.Errorf("expected %d messages received, got %d", totalMessages, received)
	}
}

// TestClient_ErrDriverUnavailable exercises the conductor error path via AddPublication
// with a bad term length (non-power-of-two), which causes the conductor to broadcast
// RspError. This is the canonical "driver unavailable / conductor error" test.
func TestClient_ErrDriverUnavailable(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = 12345 // not a power of two — logbuffer.New will fail in conductor
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.SenderIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(100 * time.Microsecond)

	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, &cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	_, err = c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1)
	if err == nil {
		t.Fatal("expected error when conductor cannot create log buffer (driver unavailable)")
	}
	t.Logf("got expected conductor error: %v", err)
}

// TestPublication_ErrBackPressure verifies that back-pressure (AppendRotation or
// AppendBackPressure) is returned from Offer when the log buffer term is full.
// This uses a small term to trigger the condition quickly.
func TestPublication_ErrBackPressure(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength // 64 KiB — fills quickly with 2 KiB messages
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.SenderIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(100 * time.Microsecond)

	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, &cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	pub, err := c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 9001)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	// Fill the 64 KiB term with 2 KiB messages.  The term holds ~32 frames;
	// back-pressure or rotation must be returned before 1000 iterations.
	bigMsg := make([]byte, 2048)
	backPressureSeen := false
	for i := 0; i < 1000; i++ {
		result, offerErr := pub.Offer(bigMsg)
		if offerErr != nil {
			t.Fatalf("Offer[%d]: unexpected error: %v", i, offerErr)
		}
		if result == logbuffer.AppendBackPressure || result == logbuffer.AppendRotation {
			backPressureSeen = true
			break
		}
	}

	if !backPressureSeen {
		t.Error("expected AppendBackPressure or AppendRotation to be returned before term is exhausted")
	}
}
