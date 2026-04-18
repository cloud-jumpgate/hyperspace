package logbuffer

import "github.com/cloud-jumpgate/hyperspace/internal/atomic"

// Result codes returned from Append* methods when no space is available.
const (
	AppendBackPressure = int64(-1) // no space in current term — caller should yield
	AppendRotation     = int64(-2) // term is full — rotation to next term is needed
)

// Protocol version written into every frame header.
const ProtocolVersion = uint8(1)

// tailCounterOffset returns the byte offset of the tail counter for partition
// idx within the log meta-data buffer. Each counter is an int64.
func tailCounterOffset(partIdx int) int {
	return partIdx * 8
}

// TermAppender appends frames to a single term buffer using a lock-free
// atomic tail-claim protocol. The term buffer occupies one-third of the full
// log buffer (termLength bytes). The meta buffer holds three tail counters
// (one per partition) plus additional metadata.
type TermAppender struct {
	termBuffer *atomic.AtomicBuffer
	metaBuffer *atomic.AtomicBuffer
	partIdx    int
}

// NewTermAppender creates a TermAppender for the given partition index.
func NewTermAppender(termBuf, metaBuf *atomic.AtomicBuffer, partIdx int) *TermAppender {
	return &TermAppender{
		termBuffer: termBuf,
		metaBuffer: metaBuf,
		partIdx:    partIdx,
	}
}

// AlignedLength rounds n up to the nearest 32-byte boundary.
func AlignedLength(n int) int { return (n + 31) &^ 31 }

// TailOffset returns the current raw tail value for this partition.
// The lower 32 bits are the byte offset within the term; upper bits are unused
// in this implementation (Aeron uses the upper 32 bits for the term ID but we
// keep the simpler form here).
func (a *TermAppender) TailOffset() int64 {
	return a.metaBuffer.GetInt64Volatile(tailCounterOffset(a.partIdx))
}

// termLength infers the term capacity from the termBuffer size.
func (a *TermAppender) termLength() int {
	return a.termBuffer.Capacity()
}

// claim atomically reserves alignedLen bytes in the term and returns the raw tail
// (i.e. the offset before the claim). If the claim would exceed the term length
// the raw tail is still returned so the caller can detect overflow.
func (a *TermAppender) claim(alignedLen int) int64 {
	return a.metaBuffer.GetAndAddInt64(tailCounterOffset(a.partIdx), int64(alignedLen))
}

// writeHeader writes all frame header fields at termOffset except frame_length
// (which is written last as a volatile store to signal readers).
func (a *TermAppender) writeHeader(
	termOffset int32,
	frameLen int32,
	flags uint8,
	frameType uint16,
	sessionID, streamID, termID int32,
	reservedValue int64,
) {
	off := int(termOffset)
	hdr := NewHeader(a.termBuffer, off)
	hdr.SetVersion(ProtocolVersion)
	hdr.SetFlags(flags)
	hdr.SetFrameType(frameType)
	hdr.SetTermOffset(termOffset)
	hdr.SetSessionID(sessionID)
	hdr.SetStreamID(streamID)
	hdr.SetTermID(termID)
	hdr.SetReservedValue(reservedValue)
	// frame_length written last — volatile store signals the frame is ready.
	hdr.SetFrameLength(frameLen)
}

// AppendUnfragmented writes a message that fits in a single frame.
//
// Return values:
//
//	>= 0  new tail position (success)
//	-1    AppendBackPressure — caller should yield / retry later
//	-2    AppendRotation     — term is full; rotate to next term
func (a *TermAppender) AppendUnfragmented(
	sessionID, streamID, termID int32,
	src []byte,
	reservedValue int64,
) int64 {
	frameLen := HeaderLength + len(src)
	alignedLen := AlignedLength(frameLen)
	termLen := a.termLength()

	rawTail := a.claim(alignedLen)
	termOffset := int32(rawTail & 0xFFFFFFFF)

	if int(termOffset)+alignedLen > termLen {
		// Term is full.  If the tail was already at the limit the term was
		// already full before this call so signal rotation.
		if int(termOffset) >= termLen {
			return AppendBackPressure
		}
		// Write a padding frame to fill the remaining space then signal rotation.
		a.Padding(termOffset, termLen-int(termOffset))
		return AppendRotation
	}

	off := int(termOffset)
	// Write payload first.
	if len(src) > 0 {
		a.termBuffer.PutBytes(off+HeaderLength, src)
	}
	// Write header fields (frame_length written last — volatile).
	a.writeHeader(termOffset, int32(frameLen), FlagUnfragmented, FrameTypeDATA,
		sessionID, streamID, termID, reservedValue)

	return rawTail + int64(alignedLen)
}

// AppendFragmented writes a large message as a sequence of B/M/E frames.
// maxPayloadLength is the maximum payload bytes per frame (mtu - HeaderLength).
//
// Return values follow the same convention as AppendUnfragmented.
func (a *TermAppender) AppendFragmented(
	sessionID, streamID, termID int32,
	src []byte,
	maxPayloadLength int,
	reservedValue int64,
) int64 {
	remaining := len(src)
	termLen := a.termLength()

	// Pre-calculate total bytes needed, accounting for the shorter last fragment.
	numFragments := (remaining + maxPayloadLength - 1) / maxPayloadLength
	lastPayload := remaining - (numFragments-1)*maxPayloadLength
	totalAligned := (numFragments-1)*AlignedLength(HeaderLength+maxPayloadLength) +
		AlignedLength(HeaderLength+lastPayload)

	rawTail := a.claim(totalAligned)
	termOffset := int32(rawTail & 0xFFFFFFFF)

	if int(termOffset)+totalAligned > termLen {
		if int(termOffset) >= termLen {
			return AppendBackPressure
		}
		a.Padding(termOffset, termLen-int(termOffset))
		return AppendRotation
	}

	srcPos := 0
	currentOffset := termOffset
	isFirst := true

	for remaining > 0 {
		chunkLen := remaining
		if chunkLen > maxPayloadLength {
			chunkLen = maxPayloadLength
		}
		alignedLen := AlignedLength(HeaderLength + chunkLen)

		// Determine flags.
		var flags uint8
		isLast := remaining == chunkLen
		if isFirst {
			flags |= FlagBegin
		}
		if isLast {
			flags |= FlagEnd
		}

		// Write payload.
		off := int(currentOffset)
		a.termBuffer.PutBytes(off+HeaderLength, src[srcPos:srcPos+chunkLen])

		// Write header (frame_length last — volatile).
		a.writeHeader(currentOffset, int32(HeaderLength+chunkLen), flags, FrameTypeDATA,
			sessionID, streamID, termID, reservedValue)

		srcPos += chunkLen
		remaining -= chunkLen
		currentOffset += int32(alignedLen)
		isFirst = false
	}

	return rawTail + int64(totalAligned)
}

// Padding writes a PAD frame at termOffset filling `length` bytes. This is used
// to fill the remainder of a term when it cannot hold the next message.
func (a *TermAppender) Padding(termOffset int32, length int) {
	if length < HeaderLength {
		return
	}
	off := int(termOffset)
	hdr := NewHeader(a.termBuffer, off)
	hdr.SetVersion(ProtocolVersion)
	hdr.SetFlags(FlagUnfragmented)
	hdr.SetFrameType(FrameTypePAD)
	hdr.SetTermOffset(termOffset)
	hdr.SetSessionID(0)
	hdr.SetStreamID(0)
	hdr.SetTermID(0)
	hdr.SetReservedValue(0)
	hdr.SetFrameLength(int32(length))
}
