// Package probes implements PING/PONG framing for the Hyperspace probe plane.
// Frames are fixed-size and little-endian.
//
// Wire formats:
//
//	PingFrame (17 bytes): [type:1][seq:8][sent_ns:8]
//	PongFrame (25 bytes): [type:1][seq:8][sent_ns:8][recv_ns:8]
package probes

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Frame type bytes on the probe stream.
const (
	FramePING = byte(0x01)
	FramePONG = byte(0x02)
)

// Frame lengths.
const (
	PingFrameLen = 17 // [type:1][seq:8][sent_ns:8]
	PongFrameLen = 25 // [type:1][seq:8][sent_ns:8][recv_ns:8]
)

// Sentinel errors.
var (
	ErrFrameTooShort  = errors.New("probes: frame too short")
	ErrWrongFrameType = errors.New("probes: wrong frame type")
)

// PingFrame represents a PING probe sent by a client.
type PingFrame struct {
	Seq    uint64
	SentAt time.Time
}

// PongFrame represents a PONG response to a PingFrame.
type PongFrame struct {
	Seq        uint64
	SentAt     time.Time
	ReceivedAt time.Time
}

// EncodePing serialises a PING frame into dst (must be >= PingFrameLen bytes).
// All integers are little-endian.
func EncodePing(dst []byte, seq uint64, sentAt time.Time) error {
	if len(dst) < PingFrameLen {
		return fmt.Errorf("probes: dst too small (%d < %d)", len(dst), PingFrameLen)
	}
	dst[0] = FramePING
	binary.LittleEndian.PutUint64(dst[1:9], seq)
	binary.LittleEndian.PutUint64(dst[9:17], uint64(sentAt.UnixNano()))
	return nil
}

// DecodePing deserialises a PING frame.
func DecodePing(src []byte) (*PingFrame, error) {
	if len(src) < PingFrameLen {
		return nil, ErrFrameTooShort
	}
	if src[0] != FramePING {
		return nil, fmt.Errorf("%w: got 0x%02x, want 0x%02x", ErrWrongFrameType, src[0], FramePING)
	}
	seq := binary.LittleEndian.Uint64(src[1:9])
	sentNs := int64(binary.LittleEndian.Uint64(src[9:17]))
	return &PingFrame{
		Seq:    seq,
		SentAt: time.Unix(0, sentNs).UTC(),
	}, nil
}

// EncodePong serialises a PONG frame into dst (must be >= PongFrameLen bytes).
// ping is the original PING frame being responded to.
// receivedAt is when the PING was received by the responder.
func EncodePong(dst []byte, ping *PingFrame, receivedAt time.Time) error {
	if len(dst) < PongFrameLen {
		return fmt.Errorf("probes: dst too small (%d < %d)", len(dst), PongFrameLen)
	}
	if ping == nil {
		return errors.New("probes: ping must not be nil")
	}
	dst[0] = FramePONG
	binary.LittleEndian.PutUint64(dst[1:9], ping.Seq)
	binary.LittleEndian.PutUint64(dst[9:17], uint64(ping.SentAt.UnixNano()))
	binary.LittleEndian.PutUint64(dst[17:25], uint64(receivedAt.UnixNano()))
	return nil
}

// DecodePong deserialises a PONG frame.
func DecodePong(src []byte) (*PongFrame, error) {
	if len(src) < PongFrameLen {
		return nil, ErrFrameTooShort
	}
	if src[0] != FramePONG {
		return nil, fmt.Errorf("%w: got 0x%02x, want 0x%02x", ErrWrongFrameType, src[0], FramePONG)
	}
	seq := binary.LittleEndian.Uint64(src[1:9])
	sentNs := int64(binary.LittleEndian.Uint64(src[9:17]))
	recvNs := int64(binary.LittleEndian.Uint64(src[17:25]))
	return &PongFrame{
		Seq:        seq,
		SentAt:     time.Unix(0, sentNs).UTC(),
		ReceivedAt: time.Unix(0, recvNs).UTC(),
	}, nil
}

// RTT computes the round-trip time from a PONG frame.
// rtt = now - ping.SentAt
func (p *PongFrame) RTT(now time.Time) time.Duration {
	return now.Sub(p.SentAt)
}
