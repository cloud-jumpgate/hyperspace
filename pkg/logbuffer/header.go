// Package logbuffer implements the three-term rotating log buffer that is the
// core data structure of the Hyperspace pub/sub engine.
//
// Wire format constants and the Header flyweight are defined here. All
// multi-byte values are little-endian, matching the Hyperspace wire format.
package logbuffer

import "github.com/cloud-jumpgate/hyperspace/internal/atomic"

// HeaderLength is the fixed size of every frame header in bytes.
const HeaderLength = 32

// Frame flag bits.
const (
	FlagBegin        = uint8(0x80)
	FlagEnd          = uint8(0x40)
	FlagUnfragmented = FlagBegin | FlagEnd
)

// Frame type constants (uint16 little-endian at header offset 6).
const (
	FrameTypePAD   = uint16(0)
	FrameTypeDATA  = uint16(1)
	FrameTypeSM    = uint16(3)
	FrameTypeNAK   = uint16(4)
	FrameTypeSETUP = uint16(5)
	FrameTypeRTT   = uint16(6)
	FrameTypePING  = uint16(7)
	FrameTypePONG  = uint16(8)
)

// Header field offsets (relative to frame start).
const (
	hdrOffFrameLength   = 0
	hdrOffVersion       = 4
	hdrOffFlags         = 5
	hdrOffFrameType     = 6
	hdrOffTermOffset    = 8
	hdrOffSessionID     = 12
	hdrOffStreamID      = 16
	hdrOffTermID        = 20
	hdrOffReservedValue = 24
)

// Header is a flyweight that reads and writes frame header fields on an
// AtomicBuffer at a fixed offset. It allocates nothing; it is a thin view
// over existing buffer bytes.
type Header struct {
	buffer *atomic.AtomicBuffer
	offset int
}

// NewHeader returns a Header flyweight positioned at offset within buffer.
func NewHeader(buffer *atomic.AtomicBuffer, offset int) *Header {
	return &Header{buffer: buffer, offset: offset}
}

// FrameLength returns the frame_length field (volatile load).
func (h *Header) FrameLength() int32 {
	return h.buffer.GetInt32Volatile(h.offset + hdrOffFrameLength)
}

// SetFrameLength sets the frame_length field (volatile store for reader visibility).
func (h *Header) SetFrameLength(v int32) {
	h.buffer.PutInt32Ordered(h.offset+hdrOffFrameLength, v)
}

// Version returns the version byte.
func (h *Header) Version() uint8 {
	return h.buffer.GetUint8(h.offset + hdrOffVersion)
}

// SetVersion writes the version byte.
func (h *Header) SetVersion(v uint8) {
	h.buffer.PutUint8(h.offset+hdrOffVersion, v)
}

// Flags returns the flags byte.
func (h *Header) Flags() uint8 {
	return h.buffer.GetUint8(h.offset + hdrOffFlags)
}

// SetFlags writes the flags byte.
func (h *Header) SetFlags(v uint8) {
	h.buffer.PutUint8(h.offset+hdrOffFlags, v)
}

// FrameType returns the frame_type field (uint16 LE).
func (h *Header) FrameType() uint16 {
	return h.buffer.GetUint16LE(h.offset + hdrOffFrameType)
}

// SetFrameType writes the frame_type field (uint16 LE).
func (h *Header) SetFrameType(v uint16) {
	h.buffer.PutUint16LE(h.offset+hdrOffFrameType, v)
}

// TermOffset returns the term_offset field (int32 LE).
func (h *Header) TermOffset() int32 {
	return h.buffer.GetInt32LE(h.offset + hdrOffTermOffset)
}

// SetTermOffset writes the term_offset field (int32 LE).
func (h *Header) SetTermOffset(v int32) {
	h.buffer.PutInt32LE(h.offset+hdrOffTermOffset, v)
}

// SessionID returns the session_id field (int32 LE).
func (h *Header) SessionID() int32 {
	return h.buffer.GetInt32LE(h.offset + hdrOffSessionID)
}

// SetSessionID writes the session_id field (int32 LE).
func (h *Header) SetSessionID(v int32) {
	h.buffer.PutInt32LE(h.offset+hdrOffSessionID, v)
}

// StreamID returns the stream_id field (int32 LE).
func (h *Header) StreamID() int32 {
	return h.buffer.GetInt32LE(h.offset + hdrOffStreamID)
}

// SetStreamID writes the stream_id field (int32 LE).
func (h *Header) SetStreamID(v int32) {
	h.buffer.PutInt32LE(h.offset+hdrOffStreamID, v)
}

// TermID returns the term_id field (int32 LE).
func (h *Header) TermID() int32 {
	return h.buffer.GetInt32LE(h.offset + hdrOffTermID)
}

// SetTermID writes the term_id field (int32 LE).
func (h *Header) SetTermID(v int32) {
	h.buffer.PutInt32LE(h.offset+hdrOffTermID, v)
}

// ReservedValue returns the reserved_val field (int64 LE).
func (h *Header) ReservedValue() int64 {
	return h.buffer.GetInt64LE(h.offset + hdrOffReservedValue)
}

// SetReservedValue writes the reserved_val field (int64 LE).
func (h *Header) SetReservedValue(v int64) {
	h.buffer.PutInt64LE(h.offset+hdrOffReservedValue, v)
}

// IsBeginFragment returns true if the BEGIN flag is set.
func (h *Header) IsBeginFragment() bool {
	return h.Flags()&FlagBegin != 0
}

// IsEndFragment returns true if the END flag is set.
func (h *Header) IsEndFragment() bool {
	return h.Flags()&FlagEnd != 0
}

// IsUnfragmented returns true if both BEGIN and END flags are set.
func (h *Header) IsUnfragmented() bool {
	return h.Flags()&FlagUnfragmented == FlagUnfragmented
}
