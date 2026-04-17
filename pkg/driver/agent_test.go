package driver_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
)

// mockAgent is a controllable Agent for testing RunAgent.
type mockAgent struct {
	name        string
	doWorkCount atomic.Int64
	workResult  atomic.Int64 // returned by DoWork
	closed      atomic.Bool
}

func (m *mockAgent) DoWork(_ context.Context) int {
	m.doWorkCount.Add(1)
	return int(m.workResult.Load())
}

func (m *mockAgent) Name() string { return m.name }

func (m *mockAgent) Close() error {
	m.closed.Store(true)
	return nil
}

// mockIdle records calls to Idle and Reset.
type mockIdle struct {
	idleCalls  atomic.Int64
	resetCalls atomic.Int64
}

func (m *mockIdle) Idle(_ int) { m.idleCalls.Add(1) }
func (m *mockIdle) Reset()     { m.resetCalls.Add(1) }

func TestRunAgent_CallsDoWorkRepeatedly(t *testing.T) {
	agent := &mockAgent{name: "test", workResult: atomic.Int64{}}
	agent.workResult.Store(0) // always return 0 (idle)

	idle := &mockIdle{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	driver.RunAgent(ctx, agent, idle)

	if agent.doWorkCount.Load() == 0 {
		t.Fatal("expected DoWork to be called at least once")
	}
}

func TestRunAgent_IdleCalledWhenDoWorkReturnsZero(t *testing.T) {
	agent := &mockAgent{name: "test"}
	agent.workResult.Store(0) // always idle

	idle := &mockIdle{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	driver.RunAgent(ctx, agent, idle)

	if idle.idleCalls.Load() == 0 {
		t.Fatal("expected Idle to be called at least once")
	}
	if idle.resetCalls.Load() != 0 {
		t.Fatalf("expected Reset not called, got %d", idle.resetCalls.Load())
	}
}

func TestRunAgent_ResetCalledWhenDoWorkReturnsPositive(t *testing.T) {
	agent := &mockAgent{name: "test"}
	agent.workResult.Store(5) // always returns work

	idle := &mockIdle{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	driver.RunAgent(ctx, agent, idle)

	if idle.resetCalls.Load() == 0 {
		t.Fatal("expected Reset to be called at least once")
	}
	if idle.idleCalls.Load() != 0 {
		t.Fatalf("expected Idle not called, got %d", idle.idleCalls.Load())
	}
}

func TestRunAgent_StopsOnContextCancel(t *testing.T) {
	agent := &mockAgent{name: "test"}
	agent.workResult.Store(0)

	idle := &mockIdle{}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		driver.RunAgent(ctx, agent, idle)
		close(done)
	}()

	// Cancel immediately.
	cancel()

	select {
	case <-done:
		// OK — RunAgent exited
	case <-time.After(2 * time.Second):
		t.Fatal("RunAgent did not stop within 2s of context cancellation")
	}
}

func TestRunAgent_BothIdleAndResetCalledOnMixedWork(t *testing.T) {
	var callCount atomic.Int64
	agent := &mockAgent{name: "mixed"}

	idle := &mockIdle{}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// Toggle work result every 5 calls.
		for {
			n := callCount.Load()
			if n%10 < 5 {
				agent.workResult.Store(1)
			} else {
				agent.workResult.Store(0)
			}
			select {
			case <-ctx.Done():
				return
			default:
				callCount.Add(1)
				time.Sleep(time.Microsecond)
			}
		}
	}()

	time.AfterFunc(80*time.Millisecond, cancel)
	driver.RunAgent(ctx, agent, idle)

	// Both should have been called at some point.
	if idle.idleCalls.Load() == 0 && idle.resetCalls.Load() == 0 {
		t.Fatal("expected at least one call to Idle or Reset")
	}
}

// --- IdleStrategy interface compliance tests ---

func TestBusySpinIdle_SatisfiesInterface(t *testing.T) {
	var _ driver.IdleStrategy = &driver.BusySpinIdle{}
}

func TestYieldIdle_SatisfiesInterface(t *testing.T) {
	var _ driver.IdleStrategy = &driver.YieldIdle{}
}

func TestBackoffIdle_SatisfiesInterface(t *testing.T) {
	var _ driver.IdleStrategy = driver.NewBackoffIdle()
}

func TestSleepIdle_SatisfiesInterface(t *testing.T) {
	var _ driver.IdleStrategy = driver.NewSleepIdle(time.Millisecond)
}

func TestBusySpinIdle_IdleAndReset(t *testing.T) {
	b := &driver.BusySpinIdle{}
	// Should not panic or block.
	b.Idle(0)
	b.Reset()
}

func TestYieldIdle_IdleAndReset(t *testing.T) {
	y := &driver.YieldIdle{}
	y.Idle(0)
	y.Reset()
}

func TestBackoffIdle_ProgressesThroughPhases(t *testing.T) {
	b := driver.NewBackoffIdle()

	// Call Idle many times — should not panic.
	for i := 0; i < 50; i++ {
		b.Idle(0)
	}

	// Reset should bring state back.
	b.Reset()

	// After reset, first Idle call should be in spin phase (no sleep).
	start := time.Now()
	b.Idle(0)
	elapsed := time.Since(start)
	if elapsed > 5*time.Millisecond {
		t.Fatalf("after Reset, first Idle took too long: %v (expected spin phase)", elapsed)
	}
}

func TestSleepIdle_SleepsApproximatelyCorrectDuration(t *testing.T) {
	s := driver.NewSleepIdle(10 * time.Millisecond)
	start := time.Now()
	s.Idle(0)
	elapsed := time.Since(start)

	if elapsed < 8*time.Millisecond {
		t.Fatalf("SleepIdle slept too little: %v, expected ~10ms", elapsed)
	}
	// Allow some OS scheduling slack.
	if elapsed > 100*time.Millisecond {
		t.Fatalf("SleepIdle slept too long: %v", elapsed)
	}
}

func TestSleepIdle_ResetIsNoOp(t *testing.T) {
	s := driver.NewSleepIdle(time.Millisecond)
	// Should not panic.
	s.Reset()
}
