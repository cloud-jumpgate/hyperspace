// Package main demonstrates connecting to an external hsd daemon via
// client.NewExternal, adding a publication and subscription, and exchanging
// messages.
//
// Prerequisites:
//   - A running hsd daemon (go run ./cmd/hsd)
//   - The daemon's CnC shared memory socket path passed via --cnc flag
//
// Note: NewExternal is a Sprint 9 stub that returns an error until the full
// CnC shared-memory IPC is implemented. This example shows the intended API
// and compiles cleanly.
//
// Run with:
//
//	go run ./examples/hsd --cnc /tmp/hyperspace-cnc.sock
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/client"
)

const (
	channel  = "aeron:udp?endpoint=localhost:20121"
	streamID = int32(1001)
	messages = 10
)

func main() {
	cncPath := flag.String("cnc", "/tmp/hyperspace-cnc.sock", "path to hsd CnC shared memory socket")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("connecting to hsd daemon", "cnc", *cncPath)

	c, err := client.NewExternal(ctx, *cncPath)
	if err != nil {
		// NewExternal is a stub in this sprint — the daemon IPC is not yet implemented.
		// This is expected until the full CnC shared-memory transport is wired up.
		slog.Warn("NewExternal returned error (external mode not yet implemented)",
			"error", err,
			"cnc", *cncPath,
		)
		slog.Info("to run a functional demo, use examples/embedded instead")
		os.Exit(0)
	}
	defer func() {
		if err := c.Close(); err != nil {
			slog.Warn("client close error", "error", err)
		}
	}()

	// Register publication.
	pub, err := c.AddPublication(ctx, channel, streamID)
	if err != nil {
		slog.Error("failed to add publication", "error", err)
		os.Exit(1)
	}
	defer func() { _ = pub.Close() }()
	slog.Info("publication registered", "channel", channel, "stream_id", streamID)

	// Register subscription.
	sub, err := c.AddSubscription(ctx, channel, streamID)
	if err != nil {
		slog.Error("failed to add subscription", "error", err)
		os.Exit(1)
	}
	defer func() { _ = sub.Close() }()
	slog.Info("subscription registered", "channel", channel, "stream_id", streamID)

	// Publish messages.
	for i := range messages {
		msg := fmt.Sprintf("hsd example message %d", i)
		if _, err := pub.Offer([]byte(msg)); err != nil {
			slog.Warn("offer failed", "index", i, "error", err)
		} else {
			slog.Info("sent", "index", i, "payload", msg)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Poll received messages.
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

	slog.Info("hsd example complete", "sent", messages, "received", received)
}
