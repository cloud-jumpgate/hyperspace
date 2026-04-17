// Package driver provides the cooperatively scheduled agent framework for Hyperspace.
// Each agent runs a DoWork() duty cycle governed by an IdleStrategy.
package driver

import "context"

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

// RunAgent runs the given Agent in a loop until ctx is cancelled.
// Calls strategy.Idle(0) when DoWork returns 0, strategy.Reset() when it returns > 0.
func RunAgent(ctx context.Context, agent Agent, strategy IdleStrategy) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		work := agent.DoWork(ctx)
		if work > 0 {
			strategy.Reset()
		} else {
			strategy.Idle(0)
		}
	}
}
