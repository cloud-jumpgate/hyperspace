package driver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/receiver"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/sender"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
)

// ThreadingMode controls how agents are mapped to goroutines.
type ThreadingMode int

const (
	// ThreadingModeDedicated: each agent on its own goroutine.
	ThreadingModeDedicated ThreadingMode = iota
	// ThreadingModeDense: Sender+Receiver share one goroutine, Conductor alone.
	ThreadingModeDense
	// ThreadingModeShared: all agents on one goroutine (dev/test only).
	ThreadingModeShared
)

// defaultToDriverBufSize is the default ring buffer size for client→driver commands.
// Must be a power-of-2 capacity + 128 bytes trailer.
const defaultToDriverBufSize = (1 << 16) + 128 // 65664 bytes

// defaultFromDriverBufSize is the default broadcast buffer size for driver→client responses.
// Must satisfy broadcast.validateBuffer: slotCount * slotBytes + TrailerLength.
// With maxPayload=512: slotSize=520. We need slotCount (power-of-2) * 520 + 128.
// 128 * 520 + 128 = 66688. Use 64 slots: 64 * 520 + 128 = 33408.
const defaultFromDriverBufSize = 64*520 + 128 // 33408 bytes

// Config holds all driver configuration.
type Config struct {
	TermLength    int           // log buffer term length (default 16 MiB)
	MTU           int           // max frame payload (default 1200)
	Threading     ThreadingMode // default Dedicated
	ConductorIdle IdleStrategy  // default SleepIdle(1ms)
	SenderIdle    IdleStrategy  // default BackoffIdle
	ReceiverIdle  IdleStrategy  // default BackoffIdle
	// ToDriverBufSize and FromDriverBufSize: ring/broadcast buffer sizes in bytes.
	ToDriverBufSize   int // default defaultToDriverBufSize
	FromDriverBufSize int // default defaultFromDriverBufSize
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TermLength:        16 * 1024 * 1024,
		MTU:               1200,
		Threading:         ThreadingModeDedicated,
		ConductorIdle:     NewSleepIdle(time.Millisecond),
		SenderIdle:        NewBackoffIdle(),
		ReceiverIdle:      NewBackoffIdle(),
		ToDriverBufSize:   defaultToDriverBufSize,
		FromDriverBufSize: defaultFromDriverBufSize,
	}
}

// Driver is the composition root for all agents.
// In EMBEDDED mode all agents run in the same process (no mmap).
type Driver struct {
	cfg           Config
	cond          *conductor.Conductor
	snd           *sender.Sender
	rcv           *receiver.Receiver
	toDriverBuf   []byte
	fromDriverBuf []byte
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// NewEmbedded creates a Driver with in-process (embedded) shared memory.
// This is the mode used by tests and examples; mmap is not used.
func NewEmbedded(cfg Config) (*Driver, error) {
	if cfg.TermLength == 0 {
		cfg.TermLength = 16 * 1024 * 1024
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1200
	}
	if cfg.ToDriverBufSize == 0 {
		cfg.ToDriverBufSize = defaultToDriverBufSize
	}
	if cfg.FromDriverBufSize == 0 {
		cfg.FromDriverBufSize = defaultFromDriverBufSize
	}
	if cfg.ConductorIdle == nil {
		cfg.ConductorIdle = NewSleepIdle(time.Millisecond)
	}
	if cfg.SenderIdle == nil {
		cfg.SenderIdle = NewBackoffIdle()
	}
	if cfg.ReceiverIdle == nil {
		cfg.ReceiverIdle = NewBackoffIdle()
	}

	toDriverBuf := make([]byte, cfg.ToDriverBufSize)
	fromDriverBuf := make([]byte, cfg.FromDriverBufSize)

	toDriverAtomic := atomicbuf.NewAtomicBuffer(toDriverBuf)
	fromDriverAtomic := atomicbuf.NewAtomicBuffer(fromDriverBuf)

	cond, err := conductor.New(toDriverAtomic, fromDriverAtomic, cfg.TermLength)
	if err != nil {
		return nil, fmt.Errorf("driver: create conductor: %w", err)
	}

	arb := arbitrator.NewRandom(nil)
	snd := sender.New(cond, arb, cfg.MTU)
	rcv := receiver.New(cond, cfg.MTU)

	return &Driver{
		cfg:           cfg,
		cond:          cond,
		snd:           snd,
		rcv:           rcv,
		toDriverBuf:   toDriverBuf,
		fromDriverBuf: fromDriverBuf,
	}, nil
}

// Start launches all agent goroutines. Returns immediately.
func (d *Driver) Start(ctx context.Context) error {
	agentCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	switch d.cfg.Threading {
	case ThreadingModeDedicated:
		d.startDedicated(agentCtx)
	case ThreadingModeDense:
		d.startDense(agentCtx)
	case ThreadingModeShared:
		d.startShared(agentCtx)
	default:
		d.startDedicated(agentCtx)
	}

	slog.Info("driver: started", "threading", d.cfg.Threading)
	return nil
}

// startDedicated launches each agent on its own goroutine.
func (d *Driver) startDedicated(ctx context.Context) {
	d.launchAgent(ctx, d.cond, d.cfg.ConductorIdle)
	d.launchAgent(ctx, d.snd, d.cfg.SenderIdle)
	d.launchAgent(ctx, d.rcv, d.cfg.ReceiverIdle)
}

// startDense: Sender+Receiver share one goroutine, Conductor alone.
func (d *Driver) startDense(ctx context.Context) {
	d.launchAgent(ctx, d.cond, d.cfg.ConductorIdle)
	// Sender and Receiver share a goroutine via round-robin.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		strategy := NewBackoffIdle()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			work := d.snd.DoWork(ctx) + d.rcv.DoWork(ctx)
			if work > 0 {
				strategy.Reset()
			} else {
				strategy.Idle(0)
			}
		}
	}()
}

// startShared: all agents on one goroutine (dev/test only).
func (d *Driver) startShared(ctx context.Context) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		strategy := NewBackoffIdle()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			work := d.cond.DoWork(ctx) + d.snd.DoWork(ctx) + d.rcv.DoWork(ctx)
			if work > 0 {
				strategy.Reset()
			} else {
				strategy.Idle(0)
			}
		}
	}()
}

// launchAgent starts a single agent goroutine.
func (d *Driver) launchAgent(ctx context.Context, agent Agent, strategy IdleStrategy) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		RunAgent(ctx, agent, strategy)
	}()
}

// Stop cancels all agents and waits for them to finish.
func (d *Driver) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
	_ = d.cond.Close()
	_ = d.snd.Close()
	_ = d.rcv.Close()
	slog.Info("driver: stopped")
}

// Conductor returns the Conductor for external command injection.
func (d *Driver) Conductor() *conductor.Conductor {
	return d.cond
}

// ToDriverBuffer returns the ring buffer backing for sending commands to the driver.
func (d *Driver) ToDriverBuffer() []byte {
	return d.toDriverBuf
}

// FromDriverBuffer returns the broadcast buffer backing for reading driver responses.
func (d *Driver) FromDriverBuffer() []byte {
	return d.fromDriverBuf
}
