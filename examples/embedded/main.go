// Package main demonstrates the minimal Hyperspace usage: an embedded driver
// with one publisher and one subscriber exchanging 10 messages on loopback.
//
// Run with:
//
//	go run ./examples/embedded
//
// The embedded driver runs all goroutines in-process; no external hsd daemon
// or infrastructure is required.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/client"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
)

const (
	channel  = "aeron:ipc"
	streamID = int32(1001)
	messages = 10
)

func main() {
	// Structured logging to stdout.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := driver.DefaultConfig()

	// Create the publisher client (embedded driver).
	pubClient, err := client.NewEmbedded(ctx, &cfg)
	if err != nil {
		slog.Error("failed to create publisher client", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := pubClient.Close(); err != nil {
			slog.Warn("publisher client close error", "error", err)
		}
	}()

	// Add a publication.
	pub, err := pubClient.AddPublication(ctx, channel, streamID)
	if err != nil {
		slog.Error("failed to add publication", "error", err)
		os.Exit(1)
	}
	defer func() { _ = pub.Close() }()

	slog.Info("publication registered", "channel", channel, "stream_id", streamID)

	// Add a subscription on the same embedded client. In a real deployment the
	// subscriber would be a separate process connecting via hsd.
	sub, err := pubClient.AddSubscription(ctx, channel, streamID)
	if err != nil {
		slog.Error("failed to add subscription", "error", err)
		os.Exit(1)
	}
	defer func() { _ = sub.Close() }()

	slog.Info("subscription registered", "channel", channel, "stream_id", streamID)

	// Send messages.
	for i := range messages {
		msg := fmt.Sprintf("hello hyperspace message %d", i)
		if _, err := pub.Offer([]byte(msg)); err != nil {
			slog.Warn("offer failed", "index", i, "error", err)
		} else {
			slog.Info("sent", "index", i, "payload", msg)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Poll the subscription to drain received messages.
	received := 0
	deadline := time.Now().Add(5 * time.Second)
	for received < messages && time.Now().Before(deadline) {
		count := sub.Poll(func(buf []byte, offset, length int, header client.FragmentHeader) {
			payload := buf[offset : offset+length]
			slog.Info("received", "index", received, "payload", string(payload))
			received++
		}, messages)
		if count == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}

	slog.Info("example complete", "sent", messages, "received", received)
}
