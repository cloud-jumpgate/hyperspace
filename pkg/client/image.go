package client

import (
	"log/slog"
	"sync/atomic"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// FragmentHeader carries metadata about a received frame, extracted from
// the logbuffer Header flyweight.
type FragmentHeader struct {
	SessionID  int32
	StreamID   int32
	TermID     int32
	TermOffset int32
	Flags      byte
	FrameType  uint16
}

// FragmentHandler is the callback invoked for each received data fragment.
// buf contains the raw payload bytes starting at offset with the given length.
// header provides the frame metadata.
type FragmentHandler func(buf []byte, offset, length int, header FragmentHeader)

// Image represents a received stream from a specific publisher (identified
// by sessionID). Each Image maintains its own read position within the
// publisher's log buffer.
//
// Poll is safe to call from a single goroutine; it is not concurrency-safe
// for multiple simultaneous callers on the same Image.
type Image struct {
	sessionID  int32
	streamID   int32
	logBuf     *logbuffer.LogBuffer
	termOffset int32 // current read position within the active term
	partIdx    int   // which of the three partitions we are reading
	position   atomic.Int64
	closed     atomic.Bool
}

// newImage constructs an Image for a given log buffer.
func newImage(sessionID, streamID int32, lb *logbuffer.LogBuffer) *Image {
	return &Image{
		sessionID: sessionID,
		streamID:  streamID,
		logBuf:    lb,
	}
}

// Poll reads up to fragmentLimit data frames from the image and invokes
// handler for each one. Returns the number of fragments delivered.
func (img *Image) Poll(handler FragmentHandler, fragmentLimit int) int {
	if img.closed.Load() {
		return 0
	}

	reader := img.logBuf.Reader(img.partIdx)
	prevOffset := img.termOffset
	framesRead, nextOffset := reader.Read(
		func(buf *atomicbuf.AtomicBuffer, offset, length int, hdr *logbuffer.Header) {
			fh := FragmentHeader{
				SessionID:  hdr.SessionID(),
				StreamID:   hdr.StreamID(),
				TermID:     hdr.TermID(),
				TermOffset: hdr.TermOffset(),
				Flags:      hdr.Flags(),
				FrameType:  hdr.FrameType(),
			}
			// Deliver the payload slice from the term buffer bytes.
			// Clamp length to the actual buffer capacity to prevent OOB reads.
			raw := buf.Bytes()
			payloadStart := offset + logbuffer.HeaderLength
			if payloadStart+length > len(raw) {
				length = len(raw) - payloadStart
			}
			handler(raw, payloadStart, length, fh)
		},
		img.termOffset,
		fragmentLimit,
	)

	if nextOffset > prevOffset {
		advanced := int64(nextOffset - prevOffset)
		img.position.Add(advanced)
		img.termOffset = nextOffset
	}

	// Check if the current term is exhausted and rotation is needed.
	if img.termOffset >= int32(img.logBuf.TermLength()) { // #nosec G115 -- TermLength() validated at construction to be within MaxTermLength, fits in int32
		img.partIdx = (img.partIdx + 1) % logbuffer.NumPartitions
		img.termOffset = 0
		slog.Debug("image: term rotation",
			"session_id", img.sessionID,
			"stream_id", img.streamID,
			"new_partition", img.partIdx,
		)
	}

	return framesRead
}

// SessionID returns the publisher's session identifier.
func (img *Image) SessionID() int32 { return img.sessionID }

// Position returns the current read position (bytes consumed from this image).
func (img *Image) Position() int64 { return img.position.Load() }

// IsClosed reports whether this Image has been closed.
func (img *Image) IsClosed() bool { return img.closed.Load() }

// close marks the image as closed. Called by Subscription.Close.
func (img *Image) close() {
	img.closed.Store(true)
}
