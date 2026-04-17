package probes_test

import (
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/transport/probes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Constants ---------------------------------------------------------------

func TestConstants(t *testing.T) {
	assert.Equal(t, byte(0x01), probes.FramePING)
	assert.Equal(t, byte(0x02), probes.FramePONG)
	assert.Equal(t, 17, probes.PingFrameLen)
	assert.Equal(t, 25, probes.PongFrameLen)
}

// --- EncodePing / DecodePing -------------------------------------------------

func TestEncodePing_DecodePing_RoundTrip(t *testing.T) {
	seq := uint64(42)
	sentAt := time.Date(2026, 4, 17, 12, 0, 0, 123456789, time.UTC)

	buf := make([]byte, probes.PingFrameLen)
	require.NoError(t, probes.EncodePing(buf, seq, sentAt))

	// First byte must be the PING frame type.
	assert.Equal(t, probes.FramePING, buf[0])

	// Decode and verify.
	frame, err := probes.DecodePing(buf)
	require.NoError(t, err)
	assert.Equal(t, seq, frame.Seq)
	assert.Equal(t, sentAt.UTC(), frame.SentAt.UTC())
}

func TestEncodePing_LargerBuffer(t *testing.T) {
	buf := make([]byte, 100) // larger than PingFrameLen — should be fine
	err := probes.EncodePing(buf, 1, time.Now())
	require.NoError(t, err)
}

func TestEncodePing_BufferTooSmall(t *testing.T) {
	buf := make([]byte, probes.PingFrameLen-1)
	err := probes.EncodePing(buf, 1, time.Now())
	assert.Error(t, err, "should error when dst is too small")
}

func TestEncodePing_ZeroSeq(t *testing.T) {
	buf := make([]byte, probes.PingFrameLen)
	require.NoError(t, probes.EncodePing(buf, 0, time.Now()))
	frame, err := probes.DecodePing(buf)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), frame.Seq)
}

func TestEncodePing_MaxSeq(t *testing.T) {
	buf := make([]byte, probes.PingFrameLen)
	require.NoError(t, probes.EncodePing(buf, ^uint64(0), time.Now()))
	frame, err := probes.DecodePing(buf)
	require.NoError(t, err)
	assert.Equal(t, ^uint64(0), frame.Seq)
}

func TestDecodePing_TruncatedFrame(t *testing.T) {
	buf := make([]byte, probes.PingFrameLen-1)
	buf[0] = probes.FramePING
	_, err := probes.DecodePing(buf)
	assert.ErrorIs(t, err, probes.ErrFrameTooShort, "truncated frame should return ErrFrameTooShort")
}

func TestDecodePing_WrongFrameType(t *testing.T) {
	buf := make([]byte, probes.PingFrameLen)
	buf[0] = probes.FramePONG // wrong type
	_, err := probes.DecodePing(buf)
	assert.ErrorIs(t, err, probes.ErrWrongFrameType, "wrong frame type should return ErrWrongFrameType")
}

func TestDecodePing_EmptyBuffer(t *testing.T) {
	_, err := probes.DecodePing(nil)
	assert.ErrorIs(t, err, probes.ErrFrameTooShort)

	_, err = probes.DecodePing([]byte{})
	assert.ErrorIs(t, err, probes.ErrFrameTooShort)
}

// --- EncodePong / DecodePong -------------------------------------------------

func TestEncodePong_DecodePong_RoundTrip(t *testing.T) {
	seq := uint64(99)
	sentAt := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	receivedAt := sentAt.Add(5 * time.Millisecond)

	ping := &probes.PingFrame{Seq: seq, SentAt: sentAt}
	buf := make([]byte, probes.PongFrameLen)
	require.NoError(t, probes.EncodePong(buf, ping, receivedAt))

	// First byte must be the PONG frame type.
	assert.Equal(t, probes.FramePONG, buf[0])

	// Decode and verify.
	pong, err := probes.DecodePong(buf)
	require.NoError(t, err)
	assert.Equal(t, seq, pong.Seq)
	assert.Equal(t, sentAt.UTC(), pong.SentAt.UTC())
	assert.Equal(t, receivedAt.UTC(), pong.ReceivedAt.UTC())
}

func TestEncodePong_BufferTooSmall(t *testing.T) {
	ping := &probes.PingFrame{Seq: 1, SentAt: time.Now()}
	buf := make([]byte, probes.PongFrameLen-1)
	err := probes.EncodePong(buf, ping, time.Now())
	assert.Error(t, err, "should error when dst is too small")
}

func TestEncodePong_NilPing(t *testing.T) {
	buf := make([]byte, probes.PongFrameLen)
	err := probes.EncodePong(buf, nil, time.Now())
	assert.Error(t, err, "nil ping should return error")
}

func TestDecodePong_TruncatedFrame(t *testing.T) {
	buf := make([]byte, probes.PongFrameLen-1)
	buf[0] = probes.FramePONG
	_, err := probes.DecodePong(buf)
	assert.ErrorIs(t, err, probes.ErrFrameTooShort)
}

func TestDecodePong_WrongFrameType(t *testing.T) {
	buf := make([]byte, probes.PongFrameLen)
	buf[0] = probes.FramePING // wrong type
	_, err := probes.DecodePong(buf)
	assert.ErrorIs(t, err, probes.ErrWrongFrameType)
}

func TestDecodePong_EmptyBuffer(t *testing.T) {
	_, err := probes.DecodePong(nil)
	assert.ErrorIs(t, err, probes.ErrFrameTooShort)
}

// --- RTT calculation ---------------------------------------------------------

func TestPongFrame_RTT(t *testing.T) {
	sentAt := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	receivedAt := sentAt.Add(3 * time.Millisecond)

	ping := &probes.PingFrame{Seq: 1, SentAt: sentAt}
	buf := make([]byte, probes.PongFrameLen)
	require.NoError(t, probes.EncodePong(buf, ping, receivedAt))

	pong, err := probes.DecodePong(buf)
	require.NoError(t, err)

	// RTT = now - sentAt
	now := sentAt.Add(10 * time.Millisecond)
	rtt := pong.RTT(now)
	assert.Equal(t, 10*time.Millisecond, rtt, "RTT should be now - SentAt")
}

func TestPongFrame_RTT_ZeroDuration(t *testing.T) {
	sentAt := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	ping := &probes.PingFrame{Seq: 1, SentAt: sentAt}
	buf := make([]byte, probes.PongFrameLen)
	require.NoError(t, probes.EncodePong(buf, ping, sentAt))

	pong, err := probes.DecodePong(buf)
	require.NoError(t, err)

	// RTT when now == sentAt should be 0.
	rtt := pong.RTT(sentAt)
	assert.Equal(t, time.Duration(0), rtt)
}

func TestPongFrame_RTT_NanosecondPrecision(t *testing.T) {
	sentAt := time.Date(2026, 4, 17, 12, 0, 0, 500, time.UTC) // 500 nanoseconds
	ping := &probes.PingFrame{Seq: 1, SentAt: sentAt}
	buf := make([]byte, probes.PongFrameLen)
	require.NoError(t, probes.EncodePong(buf, ping, sentAt))

	pong, err := probes.DecodePong(buf)
	require.NoError(t, err)

	now := sentAt.Add(1500 * time.Nanosecond)
	rtt := pong.RTT(now)
	assert.Equal(t, 1500*time.Nanosecond, rtt)
}

// --- Little-endian encoding verification ------------------------------------

func TestEncodePing_LittleEndian(t *testing.T) {
	// Verify that seq is stored little-endian.
	// seq = 0x0102030405060708 should appear as 08 07 06 05 04 03 02 01 in bytes [1:9].
	seq := uint64(0x0102030405060708)
	buf := make([]byte, probes.PingFrameLen)
	require.NoError(t, probes.EncodePing(buf, seq, time.Unix(0, 0)))

	assert.Equal(t, byte(0x08), buf[1])
	assert.Equal(t, byte(0x07), buf[2])
	assert.Equal(t, byte(0x06), buf[3])
	assert.Equal(t, byte(0x05), buf[4])
	assert.Equal(t, byte(0x04), buf[5])
	assert.Equal(t, byte(0x03), buf[6])
	assert.Equal(t, byte(0x02), buf[7])
	assert.Equal(t, byte(0x01), buf[8])
}
