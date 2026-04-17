// Package sender implements the Sender, the outbound data plane agent.
// The Sender reads from publication log buffers and sends frames over QUIC.
package sender

import (
	"context"
	"log/slog"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/arbitrator"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// fragmentsPerBatch is the maximum frames read from a log buffer per DoWork call.
const fragmentsPerBatch = 32

// Sender is the outbound data plane agent.
type Sender struct {
	conductor *conductor.Conductor
	pools     map[string]*pool.Pool // peer → pool
	arb       arbitrator.Arbitrator
	mtu       int // max payload per QUIC write (default 1200)
	// per-publication send position tracking
	positions map[int64]int64 // publicationID → log position (as nextOffset from reader)
}

// New creates a Sender.
func New(cond *conductor.Conductor, arb arbitrator.Arbitrator, mtu int) *Sender {
	if mtu <= 0 {
		mtu = 1200
	}
	return &Sender{
		conductor: cond,
		pools:     make(map[string]*pool.Pool),
		arb:       arb,
		mtu:       mtu,
		positions: make(map[int64]int64),
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
func (s *Sender) sendPublication(_ context.Context, pub *conductor.PublicationState) int {
	if pub.LogBuf == nil {
		return 0
	}

	// Determine which partition to read from.
	activePartIdx := int(pub.LogBuf.ActivePartitionIndex())
	if activePartIdx < 0 || activePartIdx >= logbuffer.NumPartitions {
		activePartIdx = 0
	}

	reader := pub.LogBuf.Reader(activePartIdx)
	termOffset := int32(s.positions[pub.PublicationID])

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

		// Build frame bytes (full frame: header + payload).
		frameBytes := make([]byte, frameLen)
		buf.GetBytes(offset, frameBytes)

		streamID := uint64(hdr.StreamID())
		if streamID < 2 {
			streamID = 2
		}

		if sendErr := conn.Send(streamID, frameBytes); sendErr != nil {
			slog.Warn("sender: failed to send frame",
				"publication_id", pub.PublicationID,
				"stream_id", streamID,
				"error", sendErr,
			)
			return
		}
		totalSent++
	}, termOffset, fragmentsPerBatch)

	// Advance tracked position to where the reader left off.
	s.positions[pub.PublicationID] = int64(nextOffset)

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
	s.positions = make(map[int64]int64)
	return nil
}
