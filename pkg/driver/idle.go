package driver

import (
	"runtime"
	"time"
)

// BusySpinIdle never yields — maximum responsiveness, 100% core utilisation.
type BusySpinIdle struct{}

// Idle is a no-op for BusySpinIdle.
func (b *BusySpinIdle) Idle(_ int) {}

// Reset is a no-op for BusySpinIdle.
func (b *BusySpinIdle) Reset() {}

// YieldIdle calls runtime.Gosched() between idle cycles.
type YieldIdle struct{}

// Idle calls runtime.Gosched().
func (y *YieldIdle) Idle(_ int) {
	runtime.Gosched()
}

// Reset is a no-op for YieldIdle.
func (y *YieldIdle) Reset() {}

// BackoffIdle spins N times, then yields N times, then sleeps progressively longer.
// Good default for Sender/Receiver.
type BackoffIdle struct {
	maxSpins  int           // default 10
	maxYields int           // default 5
	maxSleep  time.Duration // default 1ms
	// internal state
	spins  int
	yields int
	sleep  time.Duration
}

// NewBackoffIdle creates a BackoffIdle with default parameters.
func NewBackoffIdle() *BackoffIdle {
	return &BackoffIdle{
		maxSpins:  10,
		maxYields: 5,
		maxSleep:  time.Millisecond,
		sleep:     time.Microsecond,
	}
}

// Idle implements the backoff strategy: spin first, then yield, then sleep with doubling.
func (b *BackoffIdle) Idle(_ int) {
	if b.spins < b.maxSpins {
		b.spins++
		// busy spin
		return
	}
	if b.yields < b.maxYields {
		b.yields++
		runtime.Gosched()
		return
	}
	// Sleep with exponential backoff up to maxSleep.
	time.Sleep(b.sleep)
	b.sleep *= 2
	if b.sleep > b.maxSleep {
		b.sleep = b.maxSleep
	}
}

// Reset resets all accumulated idle state.
func (b *BackoffIdle) Reset() {
	b.spins = 0
	b.yields = 0
	b.sleep = time.Microsecond
}

// SleepIdle always sleeps a fixed duration. Good for low-priority agents.
type SleepIdle struct {
	d time.Duration
}

// NewSleepIdle creates a SleepIdle that sleeps d between each idle cycle.
func NewSleepIdle(d time.Duration) *SleepIdle {
	return &SleepIdle{d: d}
}

// Idle sleeps for the configured duration.
func (s *SleepIdle) Idle(_ int) {
	time.Sleep(s.d)
}

// Reset is a no-op for SleepIdle.
func (s *SleepIdle) Reset() {}
