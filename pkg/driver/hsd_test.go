package driver_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

func TestDefaultConfig_SensibleValues(t *testing.T) {
	cfg := driver.DefaultConfig()

	if cfg.TermLength != 16*1024*1024 {
		t.Errorf("TermLength: got %d, want 16MiB", cfg.TermLength)
	}
	if cfg.MTU != 1200 {
		t.Errorf("MTU: got %d, want 1200", cfg.MTU)
	}
	if cfg.Threading != driver.ThreadingModeDedicated {
		t.Errorf("Threading: got %d, want Dedicated", cfg.Threading)
	}
	if cfg.ConductorIdle == nil {
		t.Error("ConductorIdle should not be nil")
	}
	if cfg.SenderIdle == nil {
		t.Error("SenderIdle should not be nil")
	}
	if cfg.ReceiverIdle == nil {
		t.Error("ReceiverIdle should not be nil")
	}
	if cfg.ToDriverBufSize == 0 {
		t.Error("ToDriverBufSize should not be 0")
	}
	if cfg.FromDriverBufSize == 0 {
		t.Error("FromDriverBufSize should not be 0")
	}
}

func TestNewEmbedded_CreatesDriverWithValidBuffers(t *testing.T) {
	cfg := driver.DefaultConfig()
	// Use small term length for tests.
	cfg.TermLength = logbuffer.MinTermLength

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	if d.ToDriverBuffer() == nil {
		t.Error("ToDriverBuffer should not be nil")
	}
	if d.FromDriverBuffer() == nil {
		t.Error("FromDriverBuffer should not be nil")
	}
	if len(d.ToDriverBuffer()) != cfg.ToDriverBufSize {
		t.Errorf("ToDriverBuffer length: got %d, want %d",
			len(d.ToDriverBuffer()), cfg.ToDriverBufSize)
	}
	if len(d.FromDriverBuffer()) != cfg.FromDriverBufSize {
		t.Errorf("FromDriverBuffer length: got %d, want %d",
			len(d.FromDriverBuffer()), cfg.FromDriverBufSize)
	}
}

func TestNewEmbedded_ConductorIsAccessible(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	if d.Conductor() == nil {
		t.Error("Conductor() should not be nil")
	}
}

func TestStartStop_Dedicated(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength
	cfg.Threading = driver.ThreadingModeDedicated
	cfg.ConductorIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.SenderIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(time.Millisecond)

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Let it run briefly.
	time.Sleep(20 * time.Millisecond)

	// Stop must complete without hanging.
	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within 2s")
	}
}

func TestStartStop_Dense(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength
	cfg.Threading = driver.ThreadingModeDense
	cfg.ConductorIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.SenderIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(time.Millisecond)

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within 2s")
	}
}

func TestStartStop_Shared(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.SenderIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(time.Millisecond)

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within 2s")
	}
}

func TestStartStop_ContextCancellation(t *testing.T) {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength
	cfg.Threading = driver.ThreadingModeDedicated
	cfg.ConductorIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.SenderIdle = driver.NewSleepIdle(time.Millisecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(time.Millisecond)

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	cancel()

	// Stop should still work after context cancel.
	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within 2s after context cancel")
	}
}

func TestNewEmbedded_ZeroConfigUsesDefaults(t *testing.T) {
	// Pass a zero Config — NewEmbedded should fill in defaults.
	d, err := driver.NewEmbedded(driver.Config{})
	if err != nil {
		t.Fatalf("NewEmbedded with zero config: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil Driver")
	}
}

func TestThreadingModeConstants(t *testing.T) {
	// Verify the threading mode constants are distinct.
	if driver.ThreadingModeDedicated == driver.ThreadingModeDense {
		t.Error("Dedicated and Dense must be different")
	}
	if driver.ThreadingModeDedicated == driver.ThreadingModeShared {
		t.Error("Dedicated and Shared must be different")
	}
	if driver.ThreadingModeDense == driver.ThreadingModeShared {
		t.Error("Dense and Shared must be different")
	}
}
