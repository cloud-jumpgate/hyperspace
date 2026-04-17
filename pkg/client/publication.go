package client

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// Publication sends messages on a specific channel and stream.
// Multiple goroutines may call Offer concurrently; the underlying
// TermAppender uses lock-free atomic tail-claiming.
type Publication struct {
	publicationID int64
	channel       string
	streamID      int32
	sessionID     int32
	logBuf        *logbuffer.LogBuffer
	client        *Client
	closed        atomic.Bool
}

// newPublication creates a Publication. Called only by Client.handlePublicationReady.
func newPublication(
	publicationID int64,
	channel string,
	streamID, sessionID int32,
	lb *logbuffer.LogBuffer,
	client *Client,
) *Publication {
	return &Publication{
		publicationID: publicationID,
		channel:       channel,
		streamID:      streamID,
		sessionID:     sessionID,
		logBuf:        lb,
		client:        client,
	}
}

// Offer writes data to the publication's log buffer. Non-blocking.
//
// Returns:
//
//	>= 0  new tail position (bytes written — success)
//	logbuffer.AppendBackPressure (-1) — no space in current term; caller should yield
//	logbuffer.AppendRotation     (-2) — term is full; rotation pending
//
// Returns an error if the Publication is closed or if data would require
// fragmentation beyond a single frame (fragmented sends are not yet supported).
func (p *Publication) Offer(data []byte) (int64, error) {
	if p.closed.Load() {
		return 0, errors.New("publication: offer on closed publication")
	}

	partIdx := int(p.logBuf.ActivePartitionIndex())
	if partIdx < 0 || partIdx >= logbuffer.NumPartitions {
		partIdx = 0
	}
	appender := p.logBuf.Appender(partIdx)

	result := appender.AppendUnfragmented(
		p.sessionID,
		p.streamID,
		p.logBuf.InitialTermID()+int32(partIdx),
		data,
		0, // reservedValue
	)

	if result == logbuffer.AppendRotation {
		// Advance to next partition.
		nextPartIdx := (partIdx + 1) % logbuffer.NumPartitions
		p.logBuf.SetActivePartitionIndex(int32(nextPartIdx))
		slog.Debug("publication: term rotation",
			"publication_id", p.publicationID,
			"session_id", p.sessionID,
			"stream_id", p.streamID,
			"next_partition", nextPartIdx,
		)
		return logbuffer.AppendRotation, nil
	}

	return result, nil
}

// OfferFragmented writes data that may be split across multiple frames.
// Uses the driver MTU to determine the max payload per fragment.
func (p *Publication) OfferFragmented(data []byte, maxPayloadLength int) (int64, error) {
	if p.closed.Load() {
		return 0, errors.New("publication: offer on closed publication")
	}
	if maxPayloadLength <= 0 {
		return 0, fmt.Errorf("publication: maxPayloadLength must be > 0, got %d", maxPayloadLength)
	}

	partIdx := int(p.logBuf.ActivePartitionIndex())
	if partIdx < 0 || partIdx >= logbuffer.NumPartitions {
		partIdx = 0
	}
	appender := p.logBuf.Appender(partIdx)

	result := appender.AppendFragmented(
		p.sessionID,
		p.streamID,
		p.logBuf.InitialTermID()+int32(partIdx),
		data,
		maxPayloadLength,
		0,
	)

	if result == logbuffer.AppendRotation {
		nextPartIdx := (partIdx + 1) % logbuffer.NumPartitions
		p.logBuf.SetActivePartitionIndex(int32(nextPartIdx))
		return logbuffer.AppendRotation, nil
	}

	return result, nil
}

// Channel returns the channel URI string.
func (p *Publication) Channel() string { return p.channel }

// StreamID returns the stream identifier.
func (p *Publication) StreamID() int32 { return p.streamID }

// SessionID returns the session identifier assigned by the conductor.
func (p *Publication) SessionID() int32 { return p.sessionID }

// LogBuf returns the underlying log buffer for this publication.
// This is used by the integration test to wire the publication into a
// subscription directly, bypassing the network receiver path.
func (p *Publication) LogBuf() *logbuffer.LogBuffer { return p.logBuf }

// IsClosed reports whether this Publication has been closed.
func (p *Publication) IsClosed() bool { return p.closed.Load() }

// Close releases the publication and notifies the conductor.
// Safe to call multiple times; subsequent calls are no-ops.
func (p *Publication) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	slog.Info("publication: closing",
		"publication_id", p.publicationID,
		"session_id", p.sessionID,
		"stream_id", p.streamID,
	)
	return p.client.removePublication(p)
}
