package logbuffer

import "github.com/cloud-jumpgate/hyperspace/internal/atomic"

// FragmentHandler is invoked for each complete frame delivered by TermReader.Read.
// buffer is the term's AtomicBuffer; offset is the start of the frame header;
// length is the payload byte count (frame_length minus HeaderLength);
// header is the flyweight over the frame header.
type FragmentHandler func(buffer *atomic.AtomicBuffer, offset, length int, header *Header)

// TermReader reads frames from a term buffer by walking contiguous frames from
// a known offset until it reaches a frame whose frame_length is zero (not yet
// written by the producer) or exhausts the fragment limit.
type TermReader struct {
	termBuffer *atomic.AtomicBuffer
}

// NewTermReader creates a TermReader for the given term buffer.
func NewTermReader(termBuf *atomic.AtomicBuffer) *TermReader {
	return &TermReader{termBuffer: termBuf}
}

// Read delivers up to fragmentLimit data frames to handler, starting at
// termOffset. PAD frames are skipped (consumed but not delivered).
//
// Returns:
//
//	framesRead — number of DATA frames delivered
//	nextOffset — byte offset to resume on the next call
func (r *TermReader) Read(
	handler FragmentHandler,
	termOffset int32,
	fragmentLimit int,
) (int, int32) {
	capacity := int32(r.termBuffer.Capacity())
	framesRead := 0
	offset := termOffset

	for framesRead < fragmentLimit && offset < capacity {
		hdr := NewHeader(r.termBuffer, int(offset))
		frameLen := hdr.FrameLength() // volatile load
		if frameLen <= 0 {
			// No frame written here yet — stop.
			break
		}

		alignedLen := int32(AlignedLength(int(frameLen)))

		frameType := hdr.FrameType()
		if frameType != FrameTypePAD {
			// Deliver to handler.
			payloadLen := int(frameLen) - HeaderLength
			if payloadLen < 0 {
				payloadLen = 0
			}
			handler(r.termBuffer, int(offset), payloadLen, hdr)
			framesRead++
		}

		offset += alignedLen
	}

	return framesRead, offset
}
