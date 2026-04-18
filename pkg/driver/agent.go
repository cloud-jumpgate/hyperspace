// Package driver provides the cooperatively scheduled agent framework for Hyperspace.
// Each agent runs a DoWork() duty cycle governed by an IdleStrategy.
package driver

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// DefaultPanicThreshold is the maximum number of panics an agent can recover from
// before it stops gracefully. Configurable via RunAgentWithConfig.
const DefaultPanicThreshold = 10

// Agent is a cooperatively scheduled unit of work.
type Agent interface {
	// DoWork performs one duty cycle. Returns the number of work items processed.
	// Returning 0 signals the IdleStrategy to apply its idle policy.
	DoWork(ctx context.Context) int
	// Name returns a human-readable agent name for logging/metrics.
	Name() string
	// Close releases agent resources (called once, after the run loop exits).
	Close() error
}

// IdleStrategy governs what a goroutine does between duty cycles.
type IdleStrategy interface {
	// Idle is called when DoWork returns 0.
	Idle(workCount int)
	// Reset resets accumulated idle state (called when DoWork returns > 0).
	Reset()
}

// AgentRunConfig holds configuration for RunAgentWithConfig.
type AgentRunConfig struct {
	// PanicThreshold is the maximum panics before the agent stops.
	// Default: DefaultPanicThreshold (10).
	PanicThreshold int
	// PanicCounter is incremented atomically on each recovered panic.
	// If nil, an internal counter is used. Expose for testing.
	PanicCounter *atomic.Int64
}

// RunAgent runs the given Agent in a loop until ctx is cancelled.
// Calls strategy.Idle(0) when DoWork returns 0, strategy.Reset() when it returns > 0.
// Each DoWork call is wrapped in a deferred recover() to catch panics (A-01 fix).
// If the agent panics more than DefaultPanicThreshold times, it stops gracefully.
func RunAgent(ctx context.Context, agent Agent, strategy IdleStrategy) {
	cfg := AgentRunConfig{PanicThreshold: DefaultPanicThreshold}
	RunAgentWithConfig(ctx, agent, strategy, cfg)
}

// RunAgentWithConfig runs the agent with the given configuration.
func RunAgentWithConfig(ctx context.Context, agent Agent, strategy IdleStrategy, cfg AgentRunConfig) {
	if cfg.PanicThreshold <= 0 {
		cfg.PanicThreshold = DefaultPanicThreshold
	}
	var counter atomic.Int64
	panicCounter := cfg.PanicCounter
	if panicCounter == nil {
		panicCounter = &counter
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		work, panicked := doWorkSafe(ctx, agent)
		if panicked {
			count := panicCounter.Add(1)
			slog.Error("driver: agent panic recovered",
				"agent", agent.Name(),
				"panic_count", count,
				"threshold", cfg.PanicThreshold,
			)
			if int(count) >= cfg.PanicThreshold {
				slog.Error("driver: agent panic threshold exceeded, stopping agent",
					"agent", agent.Name(),
					"panic_count", count,
				)
				return
			}
			// Continue the duty cycle after recovery.
			strategy.Reset()
			continue
		}

		if work > 0 {
			strategy.Reset()
		} else {
			strategy.Idle(0)
		}
	}
}

// doWorkSafe calls agent.DoWork and recovers from any panic.
// Returns (workDone, panicked).
func doWorkSafe(ctx context.Context, agent Agent) (work int, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			slog.Error("driver: DoWork panicked",
				"agent", agent.Name(),
				"panic", fmt.Sprint(r),
			)
		}
	}()
	work = agent.DoWork(ctx)
	return work, false
}
