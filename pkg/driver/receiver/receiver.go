// Package receiver implements the Receiver, the inbound data plane agent.
// The Receiver polls QUIC connections for incoming data and writes image log buffers.
package receiver

import (
	"context"
	"encoding/binary"
	"log/slog"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/transport/pool"
)

// imageEntry holds an image log buffer and its last access time for TTL eviction.
type imageEntry struct {
	lb         *logbuffer.LogBuffer
	lastAccess time.Time
}

// DefaultImageTTL is the default duration after which idle image entries are evicted.
const DefaultImageTTL = 60 * time.Second

// evictionCheckInterval controls how many DoWork calls occur between eviction sweeps.
const evictionCheckInterval = 1000

// Receiver is the inbound data plane agent.
type Receiver struct {
	conductor    *conductor.Conductor
	pools        map[string]*pool.Pool
	images       map[int32]*imageEntry // sessionID -> image entry with TTL
	mtu          int
	termLen      int           // term length for image log buffers
	imageTTL     time.Duration // TTL for idle image eviction
	doWorkCount  int           // counter for periodic eviction
	nowFunc      func() time.Time // injectable clock for testing
}

// New creates a Receiver.
func New(cond *conductor.Conductor, mtu int) *Receiver {
	if mtu <= 0 {
		mtu = 1200
	}
	return &Receiver{
		conductor: cond,
		pools:     make(map[string]*pool.Pool),
		images:    make(map[int32]*imageEntry),
		mtu:       mtu,
		termLen:   logbuffer.MinTermLength, // use minimum for image buffers in sprint 3
		imageTTL:  DefaultImageTTL,
		nowFunc:   time.Now,
	}
}

// SetNowFunc sets the clock function used for TTL tracking (testing only).
func (r *Receiver) SetNowFunc(fn func() time.Time) {
	r.nowFunc = fn
}

// SetImageTTL sets the TTL for idle image eviction.
func (r *Receiver) SetImageTTL(ttl time.Duration) {
	r.imageTTL = ttl
}

// ImageCount returns the number of image log buffers currently held.
func (r *Receiver) ImageCount() int {
	return len(r.images)
}

// AddPool registers a pool to poll for incoming data.
func (r *Receiver) AddPool(peer string, p *pool.Pool) {
	r.pools[peer] = p
}

// DoWork polls all connections in all pools for incoming data frames.
// For each received frame: look up or create an image log buffer by sessionID,
// write the frame into the image at the correct termOffset (from frame header).
// Periodically evicts stale image entries based on TTL.
// Returns total frames received.
func (r *Receiver) DoWork(ctx context.Context) int {
	if len(r.pools) == 0 {
		return 0
	}

	totalReceived := 0

	for _, p := range r.pools {
		conns := p.Connections()
		for _, conn := range conns {
			for {
				_, data, err := conn.RecvData(ctx)
				if err != nil {
					slog.Warn("receiver: recv error", "error", err)
					break
				}
				if data == nil {
					// No more data from this connection right now.
					break
				}

				if r.processFrame(data) {
					totalReceived++
				}
			}
		}
	}

	// Periodic eviction of stale image entries.
	r.doWorkCount++
	if r.doWorkCount >= evictionCheckInterval {
		r.doWorkCount = 0
		r.evictStaleImages()
	}

	return totalReceived
}

// EvictStaleImages removes image entries that have not been accessed within the TTL.
// Exported for testing; also called internally by DoWork on a periodic basis.
func (r *Receiver) EvictStaleImages() {
	r.evictStaleImages()
}

// evictStaleImages is the internal eviction implementation.
func (r *Receiver) evictStaleImages() {
	now := r.nowFunc()
	for sid, entry := range r.images {
		if now.Sub(entry.lastAccess) > r.imageTTL {
			delete(r.images, sid)
			slog.Info("receiver: evicted stale image", "session_id", sid)
		}
	}
}

// RemoveImage immediately removes the image for sessionID (called on CmdRemoveSubscription).
func (r *Receiver) RemoveImage(sessionID int32) {
	delete(r.images, sessionID)
}

// processFrame parses an incoming frame and writes it to the appropriate image log buffer.
// Frame format: standard Hyperspace frame header (32 bytes) + payload.
//
// CRITICAL (C-05 fix): The complete frame header must be written to the image log buffer.
// Previously only payload + frameLength were written, causing readers to see frameType=0
// (PAD) and silently drop ALL received frames. Now all header fields are written:
// version, flags, frameType, termOffset, sessionID, streamID, termID, reservedValue,
// and frameLength is written LAST as a volatile store to signal readers.
//
// Returns true if the frame was successfully processed.
func (r *Receiver) processFrame(data []byte) bool {
	if len(data) < logbuffer.HeaderLength {
		slog.Warn("receiver: frame too short", "len", len(data))
		return false
	}

	// Validate frame length field against data length.
	frameLen := int(int32(binary.LittleEndian.Uint32(data[0:])))
	if frameLen <= 0 || frameLen > len(data) {
		slog.Warn("receiver: invalid frame length", "frame_len", frameLen, "data_len", len(data))
		return false
	}

	// Validate payload size against MTU.
	payloadLen := frameLen - logbuffer.HeaderLength
	if payloadLen < 0 || payloadLen > r.mtu {
		slog.Warn("receiver: payload exceeds MTU or negative", "payload_len", payloadLen, "mtu", r.mtu)
		return false
	}

	// Extract ALL header fields from the incoming frame.
	version := data[4]
	flags := data[5]
	frameType := binary.LittleEndian.Uint16(data[6:])
	termOffset := int32(binary.LittleEndian.Uint32(data[8:]))
	sessionID := int32(binary.LittleEndian.Uint32(data[12:]))
	streamID := int32(binary.LittleEndian.Uint32(data[16:]))
	termID := int32(binary.LittleEndian.Uint32(data[20:]))
	reservedValue := int64(binary.LittleEndian.Uint64(data[24:]))

	if termOffset < 0 {
		slog.Warn("receiver: negative termOffset", "term_offset", termOffset)
		return false
	}

	// Look up or create an image log buffer for this session.
	entry, err := r.getOrCreateImage(sessionID)
	if err != nil {
		slog.Error("receiver: failed to create image log buffer",
			"session_id", sessionID,
			"error", err,
		)
		return false
	}

	lb := entry.lb
	// Update last access time for TTL tracking.
	entry.lastAccess = r.nowFunc()

	// Write the frame into the image log buffer at the active partition.
	activePartIdx := int(lb.ActivePartitionIndex())
	if activePartIdx < 0 || activePartIdx >= logbuffer.NumPartitions {
		activePartIdx = 0
	}

	termBuf := lb.TermBuffer(activePartIdx)
	writeOffset := int(termOffset)

	// Bounds check: frame must fit within the term (use aligned length).
	alignedFrameLen := logbuffer.AlignedLength(frameLen)
	if writeOffset+alignedFrameLen > termBuf.Capacity() {
		slog.Warn("receiver: frame does not fit in term",
			"session_id", sessionID,
			"term_offset", termOffset,
			"frame_len", frameLen,
			"capacity", termBuf.Capacity(),
		)
		return false
	}

	// Write payload bytes into the term buffer (after the header region).
	if payloadLen > 0 {
		termBuf.PutBytes(writeOffset+logbuffer.HeaderLength, data[logbuffer.HeaderLength:logbuffer.HeaderLength+payloadLen])
	}

	// Write ALL header fields EXCEPT frameLength (written last for reader visibility).
	// This is the C-05 fix: previously only frameLength was written, causing readers
	// to see frameType=0 (PAD) and drop all frames.
	hdr := logbuffer.NewHeader(termBuf, writeOffset)
	hdr.SetVersion(version)
	hdr.SetFlags(flags)
	hdr.SetFrameType(frameType)
	hdr.SetTermOffset(termOffset)
	hdr.SetSessionID(sessionID)
	hdr.SetStreamID(streamID)
	hdr.SetTermID(termID)
	hdr.SetReservedValue(reservedValue)

	// Write frameLength LAST as a volatile store to signal readers that the frame is ready.
	hdr.SetFrameLength(int32(frameLen))

	slog.Debug("receiver: frame written",
		"session_id", sessionID,
		"stream_id", streamID,
		"term_offset", termOffset,
		"frame_len", frameLen,
		"frame_type", frameType,
	)
	return true
}

// getOrCreateImage returns the image entry for sessionID, creating one if needed.
func (r *Receiver) getOrCreateImage(sessionID int32) (*imageEntry, error) {
	if entry, ok := r.images[sessionID]; ok {
		return entry, nil
	}

	bufSize := logbuffer.NumPartitions*r.termLen + logbuffer.LogMetaDataLength
	backing := make([]byte, bufSize)
	lb, err := logbuffer.New(backing, r.termLen)
	if err != nil {
		return nil, err
	}
	entry := &imageEntry{
		lb:         lb,
		lastAccess: r.nowFunc(),
	}
	r.images[sessionID] = entry
	slog.Info("receiver: created image log buffer", "session_id", sessionID)
	return entry, nil
}

// Name returns "receiver".
func (r *Receiver) Name() string { return "receiver" }

// Close releases receiver resources.
func (r *Receiver) Close() error {
	r.images = make(map[int32]*imageEntry)
	return nil
}
