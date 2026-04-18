// Package benchmarks_test measures Hyperspace pub/sub throughput using an
// embedded driver. No network is required — the embedded driver runs all
// agent goroutines in-process.
//
// Run with:
//
//	go test -bench=. -benchmem ./benchmarks/
//
// These benchmarks establish the baseline for on-EC2 validation (P99 < 100 µs
// at 1 M msg/s on a c7gn.4xlarge).
package benchmarks_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/client"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// benchConfig returns a driver config tuned for benchmark workloads.
// ThreadingModeShared puts all agents on a single goroutine to keep the
// benchmark focused on the log buffer path rather than scheduler overhead.
func benchConfig() *driver.Config {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength // 64 KiB — small to force rotation
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.SenderIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(100 * time.Microsecond)
	return &cfg
}

// newBenchClient creates an embedded Client and pre-allocates a Publication.
// The returned cleanup function must be deferred by the caller.
func newBenchClient(b *testing.B) (*client.Client, *client.Publication, func()) {
	b.Helper()

	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, benchConfig())
	if err != nil {
		b.Fatalf("NewEmbedded: %v", err)
	}

	pub, err := c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:7777|pool=1", 1001)
	if err != nil {
		_ = c.Close()
		b.Fatalf("AddPublication: %v", err)
	}

	cleanup := func() { _ = c.Close() }
	return c, pub, cleanup
}

// newBenchSubscriptionClient creates an embedded Client with both a Publication
// and a Subscription on the same channel/stream, so messages written by Offer
// can be read back via Poll. The log buffer is pre-seeded before the benchmark
// loop begins.
func newBenchSubscriptionClient(b *testing.B) (*client.Client, *client.Subscription, func()) {
	b.Helper()

	ctx := context.Background()
	c, err := client.NewEmbedded(ctx, benchConfig())
	if err != nil {
		b.Fatalf("NewEmbedded: %v", err)
	}

	pub, err := c.AddPublication(ctx, "hs:quic?endpoint=127.0.0.1:7778|pool=1", 2001)
	if err != nil {
		_ = c.Close()
		b.Fatalf("AddPublication: %v", err)
	}

	sub, err := c.AddSubscription(ctx, "hs:quic?endpoint=127.0.0.1:7778|pool=1", 2001)
	if err != nil {
		_ = c.Close()
		b.Fatalf("AddSubscription: %v", err)
	}

	// Pre-seed the log buffer with messages so Poll has work to do.
	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i & 0xFF)
	}
	for i := 0; i < 64; i++ {
		_, _ = pub.Offer(payload)
	}

	cleanup := func() { _ = c.Close() }
	return c, sub, cleanup
}

// BenchmarkPublication_Offer measures single-goroutine offer throughput.
// This exercises the atomic tail-claim path inside TermAppender.Append.
func BenchmarkPublication_Offer(b *testing.B) {
	_, pub, cleanup := newBenchClient(b)
	defer cleanup()

	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i & 0xFF)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	for i := 0; i < b.N; i++ {
		result, err := pub.Offer(payload)
		if err != nil {
			b.Fatalf("Offer: %v", err)
		}
		// Back-pressure and rotation are expected when the term fills up;
		// they are not errors — the benchmark simply measures the offer path.
		_ = result
	}
}

// BenchmarkPublication_OfferParallel measures concurrent offer throughput
// across multiple goroutines. Uses the same Publication, which is safe for
// concurrent Offer calls via lock-free atomic tail-claiming.
func BenchmarkPublication_OfferParallel(b *testing.B) {
	_, pub, cleanup := newBenchClient(b)
	defer cleanup()

	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i & 0xFF)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result, err := pub.Offer(payload)
			if err != nil {
				b.Errorf("Offer: %v", err)
				return
			}
			_ = result
		}
	})
}

// BenchmarkSubscription_Poll measures poll throughput against a pre-seeded
// image buffer. Each iteration calls Poll with a fragment limit of 10.
func BenchmarkSubscription_Poll(b *testing.B) {
	_, sub, cleanup := newBenchSubscriptionClient(b)
	defer cleanup()

	handler := func(_ []byte, _, _ int, _ client.FragmentHeader) {}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sub.Poll(handler, 10)
	}
}
