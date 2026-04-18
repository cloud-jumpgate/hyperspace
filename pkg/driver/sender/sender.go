// Package sender implements the Sender, the outbound data plane agent.
// The Sender reads from publication log buffers and sends frames over QUIC.
package sender

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/counters"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// DefaultFragmentsPerBatch is the default maximum frames read from a log buffer per DoWork call.
const DefaultFragmentsPerBatch = 32

// maxSendRetries is the maximum number of connection retries on Send failure (ADR-009).
const maxSendRetries = 3

// backPressureThreshold is the number of consecutive failed frames before
// back-pressure is signalled to the publication (ADR-009).
const backPressureThreshold = 3

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

	// ADR-009: per-publication consecutive failure counter for back-pressure.
	pubFailures map[int64]int

	// ctrBuf is the counters buffer for incrementing CtrLostFrames. May be nil.
	ctrBuf *counters.CountersWriter

	// lostFrames tracks total lost frames (ADR-009). Exposed for testing.
	LostFrames atomic.Int64

	// BackPressureFlags tracks publications under back-pressure (ADR-009).
	// Exposed for testing.
	BackPressureFlags map[int64]bool
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
		pubFailures:       make(map[int64]int),
		BackPressureFlags: make(map[int64]bool),
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

// WithCountersWriter sets the counters writer for incrementing CtrLostFrames (ADR-009).
func WithCountersWriter(w *counters.CountersWriter) SenderOption {
	return func(s *Sender) {
		s.ctrBuf = w
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

		// Build frame bytes using pooled buffer (P-01 fix).
		bufPtr := s.framePool.Get().(*[]byte)
		frameBytes := (*bufPtr)[:frameLen]
		buf.GetBytes(offset, frameBytes)

		streamID := uint64(hdr.StreamID())
		if streamID < 2 {
			streamID = 2
		}

		// ADR-009: retry on send failure, up to maxSendRetries attempts.
		sent := false
		var lastErr error
		triedIDs := make([]uint64, 0, maxSendRetries)
		for attempt := 0; attempt < maxSendRetries && len(conns) > 0; attempt++ {
			conn, pickErr := s.arb.Pick(conns, pub.PublicationID, payloadLen)
			if pickErr != nil {
				break
			}
			triedIDs = append(triedIDs, conn.ID())
			if sendErr := conn.Send(streamID, frameBytes); sendErr != nil {
				lastErr = sendErr
				// Remove the failed connection from candidates for next attempt.
				conns = removeConn(conns, conn.ID())
				continue
			}
			sent = true
			break
		}

		s.framePool.Put(bufPtr)

		if sent {
			// Reset consecutive failure counter on success.
			s.pubFailures[pub.PublicationID] = 0
			s.BackPressureFlags[pub.PublicationID] = false
			totalSent++
		} else {
			// All retries failed — increment lost frames counter (ADR-009).
			s.LostFrames.Add(1)
			if s.ctrBuf != nil {
				s.ctrBuf.Add(counters.CtrLostFrames, 1)
			}
			slog.Error("sender: frame lost after retries",
				"publication_id", pub.PublicationID,
				"stream_id", streamID,
				"attempts", len(triedIDs),
				"conn_ids", triedIDs,
				"last_error", lastErr,
			)

			// Track consecutive failures for back-pressure.
			s.pubFailures[pub.PublicationID]++
			if s.pubFailures[pub.PublicationID] >= backPressureThreshold {
				s.BackPressureFlags[pub.PublicationID] = true
			}
		}
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

// removeConn returns a new slice without the connection matching id.
func removeConn(conns []quictr.Connection, id uint64) []quictr.Connection {
	result := make([]quictr.Connection, 0, len(conns))
	for _, c := range conns {
		if c.ID() != id {
			result = append(result, c)
		}
	}
	return result
}

// Name returns "sender".
func (s *Sender) Name() string { return "sender" }

// Close releases sender resources.
func (s *Sender) Close() error {
	s.positions = make(map[int64]*sendPosition)
	s.pubFailures = make(map[int64]int)
	s.BackPressureFlags = make(map[int64]bool)
	return nil
}
