// Package sender implements the Sender, the outbound data plane agent.
// The Sender reads from publication log buffers and sends frames over QUIC.
package sender

import (
	"context"
	"log/slog"
	"sync"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// DefaultFragmentsPerBatch is the default maximum frames read from a log buffer per DoWork call.
const DefaultFragmentsPerBatch = 32

// sendPosition tracks the sender's read position within a publication's log buffer.
// S-03 fix: track (partitionIndex, termOffset) together so that position resets
// to 0 when the active partition changes (term rotation).
type sendPosition struct {
	partitionIndex int
	termOffset     int32
}

// Sender is the outbound data plane agent.
type Sender struct {
	conductor         *conductor.Conductor
	pools             map[string]*pool.Pool // peer -> pool
	arb               arbitrator.Arbitrator
	mtu               int // max payload per QUIC write (default 1200)
	fragmentsPerBatch int // max frames per DoWork call (F-02 fix: configurable)
	// per-publication send position tracking (S-03 fix: term-aware)
	positions map[int64]*sendPosition // publicationID -> (partitionIndex, termOffset)
	// framePool provides reusable byte buffers for frame serialization (P-01 fix).
	framePool sync.Pool
}

// New creates a Sender.
// fragmentsPerBatch: max frames per DoWork call (0 = DefaultFragmentsPerBatch).
func New(cond *conductor.Conductor, arb arbitrator.Arbitrator, mtu int, opts ...SenderOption) *Sender {
	if mtu <= 0 {
		mtu = 1200
	}
	maxFrameSize := mtu + logbuffer.HeaderLength
	s := &Sender{
		conductor:         cond,
		pools:             make(map[string]*pool.Pool),
		arb:               arb,
		mtu:               mtu,
		fragmentsPerBatch: DefaultFragmentsPerBatch,
		positions:         make(map[int64]*sendPosition),
		framePool: sync.Pool{
			New: func() any {
				buf := make([]byte, maxFrameSize)
				return &buf
			},
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SenderOption configures a Sender.
//
//nolint:revive // stutter is intentional: sender.SenderOption is the established public API name
type SenderOption func(*Sender)

// WithFragmentsPerBatch sets the maximum frames read from a log buffer per DoWork call.
func WithFragmentsPerBatch(n int) SenderOption {
	return func(s *Sender) {
		if n > 0 {
			s.fragmentsPerBatch = n
		}
	}
}

// AddPool registers a QUIC pool for a peer.
func (s *Sender) AddPool(peer string, p *pool.Pool) {
	s.pools[peer] = p
}

// DoWork for each active publication:
//  1. Get current publication state from Conductor.
//  2. Read up to fragmentsPerBatch frames from the log buffer at the tracked position.
//  3. For each frame: pick a connection via Arbitrator, call conn.Send(streamID, frameBytes).
//  4. Advance tracked position.
//
// Returns total frames sent.
func (s *Sender) DoWork(ctx context.Context) int {
	pubs := s.conductor.Publications()
	if len(pubs) == 0 {
		return 0
	}

	totalSent := 0
	for _, pub := range pubs {
		sent := s.sendPublication(ctx, pub)
		totalSent += sent
	}
	return totalSent
}

// sendPublication reads frames from a publication's log buffer and sends them.
// S-03 fix: position tracking is term-aware -- when the active partition changes
// (term rotation), the termOffset resets to 0 for the new partition.
func (s *Sender) sendPublication(_ context.Context, pub *conductor.PublicationState) int {
	if pub.LogBuf == nil {
		return 0
	}

	// Determine which partition to read from.
	activePartIdx := int(pub.LogBuf.ActivePartitionIndex())
	if activePartIdx < 0 || activePartIdx >= logbuffer.NumPartitions {
		activePartIdx = 0
	}

	// Get or initialize position for this publication.
	pos := s.positions[pub.PublicationID]
	if pos == nil {
		pos = &sendPosition{}
		s.positions[pub.PublicationID] = pos
	}

	// S-03 fix: if the active partition has changed, reset termOffset to 0.
	if pos.partitionIndex != activePartIdx {
		pos.partitionIndex = activePartIdx
		pos.termOffset = 0
	}

	reader := pub.LogBuf.Reader(activePartIdx)
	termOffset := pos.termOffset

	totalSent := 0
	_, nextOffset := reader.Read(func(buf *atomicbuf.AtomicBuffer, offset, payloadLen int, hdr *logbuffer.Header) {
		frameLen := int(hdr.FrameLength())
		if frameLen <= 0 {
			return
		}

		// Validate frame length against MTU before reading payload.
		if payloadLen > s.mtu {
			slog.Warn("sender: payload exceeds MTU, dropping",
				"payload_len", payloadLen,
				"mtu", s.mtu,
			)
			return
		}

		// Gather connections for arbitration.
		conns := s.gatherConnections()
		if len(conns) == 0 {
			return
		}

		conn, err := s.arb.Pick(conns, pub.PublicationID, payloadLen)
		if err != nil {
			slog.Debug("sender: no connection available", "error", err)
			return
		}

		// Build frame bytes using pooled buffer (P-01 fix).
		// Get a reusable buffer from the pool to avoid allocation per frame.
		bufPtr := s.framePool.Get().(*[]byte)
		frameBytes := (*bufPtr)[:frameLen]
		buf.GetBytes(offset, frameBytes)

		streamID := uint64(hdr.StreamID())
		if streamID < 2 {
			streamID = 2
		}

		if sendErr := conn.Send(streamID, frameBytes); sendErr != nil {
			s.framePool.Put(bufPtr)
			slog.Warn("sender: failed to send frame",
				"publication_id", pub.PublicationID,
				"stream_id", streamID,
				"error", sendErr,
			)
			return
		}
		// Return buffer to pool after successful send.
		s.framePool.Put(bufPtr)
		totalSent++
	}, termOffset, s.fragmentsPerBatch)

	// Advance tracked position to where the reader left off.
	pos.termOffset = nextOffset

	return totalSent
}

// gatherConnections collects all live connections from all registered pools.
func (s *Sender) gatherConnections() []quictr.Connection {
	var conns []quictr.Connection
	for _, p := range s.pools {
		conns = append(conns, p.Connections()...)
	}
	return conns
}

// Name returns "sender".
func (s *Sender) Name() string { return "sender" }

// Close releases sender resources.
func (s *Sender) Close() error {
	s.positions = make(map[int64]*sendPosition)
	return nil
}
