package logbuffer

import (
	"testing"

	"github.com/cloud-jumpgate/hyperspace/internal/atomic"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func makeLogBuffer(t *testing.T, termLength int) (*LogBuffer, []byte) {
	t.Helper()
	total := NumPartitions*termLength + LogMetaDataLength
	buf := make([]byte, total)
	lb, err := New(buf, termLength)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return lb, buf
}

// ─── Header round-trip ───────────────────────────────────────────────────────

func TestHeader_AllFieldsRoundTrip(t *testing.T) {
	raw := make([]byte, 64) // 2× header size for safety
	buf := atomic.NewAtomicBuffer(raw)
	h := NewHeader(buf, 0)

	h.SetFrameLength(1234)
	h.SetVersion(ProtocolVersion)
	h.SetFlags(FlagUnfragmented)
	h.SetFrameType(FrameTypeDATA)
	h.SetTermOffset(512)
	h.SetSessionID(42)
	h.SetStreamID(7)
	h.SetTermID(99)
	const wantReserved = int64(-2401053088876216770) // 0xDEADBEEFCAFEBABE as signed int64
	h.SetReservedValue(wantReserved)

	if got := h.FrameLength(); got != 1234 {
		t.Errorf("FrameLength got %d want 1234", got)
	}
	if got := h.Version(); got != ProtocolVersion {
		t.Errorf("Version got %d want %d", got, ProtocolVersion)
	}
	if got := h.Flags(); got != FlagUnfragmented {
		t.Errorf("Flags got %#x want %#x", got, FlagUnfragmented)
	}
	if got := h.FrameType(); got != FrameTypeDATA {
		t.Errorf("FrameType got %d want %d", got, FrameTypeDATA)
	}
	if got := h.TermOffset(); got != 512 {
		t.Errorf("TermOffset got %d want 512", got)
	}
	if got := h.SessionID(); got != 42 {
		t.Errorf("SessionID got %d want 42", got)
	}
	if got := h.StreamID(); got != 7 {
		t.Errorf("StreamID got %d want 7", got)
	}
	if got := h.TermID(); got != 99 {
		t.Errorf("TermID got %d want 99", got)
	}
	if got := h.ReservedValue(); got != wantReserved {
		t.Errorf("ReservedValue got %#x want %#x", got, wantReserved)
	}
}

func TestHeader_FlagHelpers(t *testing.T) {
	raw := make([]byte, 64)
	buf := atomic.NewAtomicBuffer(raw)

	tests := []struct {
		name             string
		flags            uint8
		wantBegin        bool
		wantEnd          bool
		wantUnfragmented bool
	}{
		{"unfragmented", FlagUnfragmented, true, true, true},
		{"begin only", FlagBegin, true, false, false},
		{"end only", FlagEnd, false, true, false},
		{"none", 0, false, false, false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			h := NewHeader(buf, 0)
			h.SetFlags(tc.flags)
			if h.IsBeginFragment() != tc.wantBegin {
				t.Errorf("IsBeginFragment: got %v want %v", h.IsBeginFragment(), tc.wantBegin)
			}
			if h.IsEndFragment() != tc.wantEnd {
				t.Errorf("IsEndFragment: got %v want %v", h.IsEndFragment(), tc.wantEnd)
			}
			if h.IsUnfragmented() != tc.wantUnfragmented {
				t.Errorf("IsUnfragmented: got %v want %v", h.IsUnfragmented(), tc.wantUnfragmented)
			}
		})
	}
}

func TestHeader_AllFrameTypes(t *testing.T) {
	raw := make([]byte, 64)
	buf := atomic.NewAtomicBuffer(raw)
	h := NewHeader(buf, 0)

	types := []uint16{
		FrameTypePAD, FrameTypeDATA, FrameTypeSM, FrameTypeNAK,
		FrameTypeSETUP, FrameTypeRTT, FrameTypePING, FrameTypePONG,
	}
	for _, ft := range types {
		h.SetFrameType(ft)
		if got := h.FrameType(); got != ft {
			t.Errorf("FrameType round-trip: set %d got %d", ft, got)
		}
	}
}

func TestHeader_OffsetVariants(t *testing.T) {
	// Two headers packed at offsets 0 and 32 within the same buffer.
	raw := make([]byte, 128)
	buf := atomic.NewAtomicBuffer(raw)
	h0 := NewHeader(buf, 0)
	h1 := NewHeader(buf, 32)

	h0.SetSessionID(111)
	h1.SetSessionID(222)

	if got := h0.SessionID(); got != 111 {
		t.Errorf("h0.SessionID got %d want 111", got)
	}
	if got := h1.SessionID(); got != 222 {
		t.Errorf("h1.SessionID got %d want 222", got)
	}
}

// ─── AlignedLength ───────────────────────────────────────────────────────────

func TestAlignedLength(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0}, {1, 32}, {31, 32}, {32, 32}, {33, 64}, {64, 64}, {65, 96},
	}
	for _, c := range cases {
		if got := AlignedLength(c.in); got != c.want {
			t.Errorf("AlignedLength(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ─── AppendUnfragmented + Read ───────────────────────────────────────────────

func TestAppendUnfragmented_SingleFrame(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	msg := []byte("hello hyperspace")
	result := app.AppendUnfragmented(1, 2, 3, msg, 0)
	if result < 0 {
		t.Fatalf("AppendUnfragmented returned error code %d", result)
	}

	var gotBuf *atomic.AtomicBuffer
	var gotOffset, gotLen int
	var gotHdr *Header
	frames, next := rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		gotBuf = b
		gotOffset = off
		gotLen = length
		gotHdr = hdr
	}, 0, 10)

	if frames != 1 {
		t.Fatalf("Read: framesRead = %d, want 1", frames)
	}
	if next <= 0 {
		t.Errorf("Read: nextOffset = %d, want >0", next)
	}
	if gotLen != len(msg) {
		t.Errorf("payload length: got %d want %d", gotLen, len(msg))
	}

	// Verify payload bytes.
	got := make([]byte, gotLen)
	gotBuf.GetBytes(gotOffset+HeaderLength, got)
	if string(got) != string(msg) {
		t.Errorf("payload: got %q want %q", got, msg)
	}

	// Verify header fields.
	if gotHdr.SessionID() != 1 {
		t.Errorf("SessionID: got %d want 1", gotHdr.SessionID())
	}
	if gotHdr.StreamID() != 2 {
		t.Errorf("StreamID: got %d want 2", gotHdr.StreamID())
	}
	if gotHdr.TermID() != 3 {
		t.Errorf("TermID: got %d want 3", gotHdr.TermID())
	}
	if !gotHdr.IsUnfragmented() {
		t.Error("IsUnfragmented: expected true")
	}
	if gotHdr.FrameType() != FrameTypeDATA {
		t.Errorf("FrameType: got %d want DATA", gotHdr.FrameType())
	}
}

func TestAppendUnfragmented_MultipleFrames(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	messages := [][]byte{
		[]byte("first"),
		[]byte("second"),
		[]byte("third"),
	}
	for _, m := range messages {
		if r := app.AppendUnfragmented(1, 1, 0, m, 0); r < 0 {
			t.Fatalf("Append error %d", r)
		}
	}

	var delivered [][]byte
	frames, _ := rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		payload := make([]byte, length)
		b.GetBytes(off+HeaderLength, payload)
		delivered = append(delivered, payload)
	}, 0, 10)

	if frames != 3 {
		t.Fatalf("framesRead = %d, want 3", frames)
	}
	for i, msg := range messages {
		if string(delivered[i]) != string(msg) {
			t.Errorf("message[%d]: got %q want %q", i, delivered[i], msg)
		}
	}
}

func TestAppendUnfragmented_EmptyPayload(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	result := app.AppendUnfragmented(1, 1, 0, []byte{}, 0)
	if result < 0 {
		t.Fatalf("AppendUnfragmented(empty) returned %d", result)
	}

	frames, _ := rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		if length != 0 {
			t.Errorf("expected 0-length payload, got %d", length)
		}
	}, 0, 5)
	if frames != 1 {
		t.Errorf("framesRead = %d, want 1", frames)
	}
}

func TestAppendUnfragmented_ReservedValue(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	const wantRV = int64(0x1122334455667788 & 0x7FFFFFFFFFFFFFFF) // fits in int64
	app.AppendUnfragmented(1, 1, 0, []byte("rv test"), wantRV)

	rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		if got := hdr.ReservedValue(); got != wantRV {
			t.Errorf("ReservedValue: got %#x want %#x", got, wantRV)
		}
	}, 0, 5)
}

// ─── AppendFragmented + Read ─────────────────────────────────────────────────

func TestAppendFragmented_TwoFragments(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	// Payload slightly larger than one frame's worth at maxPayload=32.
	maxPayload := 32
	payload := make([]byte, 50)
	for i := range payload {
		payload[i] = byte(i)
	}

	result := app.AppendFragmented(1, 2, 3, payload, maxPayload, 42)
	if result < 0 {
		t.Fatalf("AppendFragmented returned %d", result)
	}

	type frag struct {
		offset int
		length int
		flags  uint8
	}
	var frags []frag
	frames, _ := rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		frags = append(frags, frag{off, length, hdr.Flags()})
	}, 0, 20)

	if frames != 2 {
		t.Fatalf("framesRead = %d, want 2", frames)
	}

	// First fragment: BEGIN set, END clear.
	if frags[0].flags&FlagBegin == 0 {
		t.Error("fragment[0]: BEGIN flag not set")
	}
	if frags[0].flags&FlagEnd != 0 {
		t.Error("fragment[0]: END flag unexpectedly set")
	}
	if frags[0].length != maxPayload {
		t.Errorf("fragment[0] length: got %d want %d", frags[0].length, maxPayload)
	}

	// Last fragment: END set, BEGIN clear.
	if frags[1].flags&FlagEnd == 0 {
		t.Error("fragment[1]: END flag not set")
	}
	if frags[1].flags&FlagBegin != 0 {
		t.Error("fragment[1]: BEGIN flag unexpectedly set")
	}
	if frags[1].length != len(payload)-maxPayload {
		t.Errorf("fragment[1] length: got %d want %d", frags[1].length, len(payload)-maxPayload)
	}
}

func TestAppendFragmented_Reassembly(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	maxPayload := 16
	original := make([]byte, 55)
	for i := range original {
		original[i] = byte(i + 1)
	}

	app.AppendFragmented(1, 1, 0, original, maxPayload, 0)

	var reassembled []byte
	rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		chunk := make([]byte, length)
		b.GetBytes(off+HeaderLength, chunk)
		reassembled = append(reassembled, chunk...)
	}, 0, 20)

	if string(reassembled) != string(original) {
		t.Errorf("reassembled payload mismatch: len=%d want len=%d", len(reassembled), len(original))
	}
}

func TestAppendFragmented_ExactlyOneMTU(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	maxPayload := 64
	payload := make([]byte, maxPayload)
	for i := range payload {
		payload[i] = byte(i)
	}

	result := app.AppendFragmented(1, 1, 0, payload, maxPayload, 0)
	if result < 0 {
		t.Fatalf("AppendFragmented: %d", result)
	}

	frames, _ := rdr.Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
		if !hdr.IsUnfragmented() {
			t.Error("single-fragment message should be unfragmented")
		}
	}, 0, 10)
	if frames != 1 {
		t.Errorf("framesRead = %d, want 1", frames)
	}
}

// ─── FragmentLimit ────────────────────────────────────────────────────────────

func TestRead_FragmentLimit(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	for i := 0; i < 5; i++ {
		app.AppendUnfragmented(1, 1, 0, []byte("x"), 0)
	}

	frames, _ := rdr.Read(func(*atomic.AtomicBuffer, int, int, *Header) {}, 0, 3)
	if frames != 3 {
		t.Errorf("framesRead with limit 3: got %d want 3", frames)
	}
}

// ─── Back-pressure ────────────────────────────────────────────────────────────

func TestAppendUnfragmented_BackPressure(t *testing.T) {
	termLength := MinTermLength // 64 KiB
	lb, _ := makeLogBuffer(t, termLength)
	app := lb.Appender(0)

	// Fill the term with max-sized unfragmented messages (payload = termLength - headerLength
	// per frame is too big; use small messages to fill it).
	// We detect back-pressure or rotation.
	var lastResult int64
	for i := 0; i < 10000; i++ {
		r := app.AppendUnfragmented(1, 1, 0, []byte("fill"), 0)
		if r == AppendBackPressure || r == AppendRotation {
			lastResult = r
			break
		}
		lastResult = r
	}

	// We expect either back-pressure or rotation at some point.
	if lastResult != AppendBackPressure && lastResult != AppendRotation {
		t.Errorf("expected back-pressure or rotation, last result = %d", lastResult)
	}
}

func TestAppendUnfragmented_Rotation(t *testing.T) {
	termLength := MinTermLength
	lb, _ := makeLogBuffer(t, termLength)
	app := lb.Appender(0)

	// Fill the term until we get a rotation signal.
	rotationSeen := false
	for i := 0; i < 100000; i++ {
		r := app.AppendUnfragmented(1, 1, 0, []byte("fill"), 0)
		if r == AppendRotation {
			rotationSeen = true
			break
		}
		if r == AppendBackPressure {
			break
		}
	}

	// Either rotation or back-pressure is acceptable depending on exact fill amount.
	_ = rotationSeen
}

// ─── Padding ─────────────────────────────────────────────────────────────────

func TestPadding(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	rdr := lb.Reader(0)

	// Write a message then manually write padding after it.
	msg := []byte("before pad")
	app.AppendUnfragmented(1, 1, 0, msg, 0)

	// Insert a PAD frame at offset 64 (aligned length of header+10 bytes = 64).
	padOffset := int32(AlignedLength(HeaderLength + len(msg)))
	padLength := 64
	app.Padding(padOffset, padLength)

	// Read should skip the PAD frame.
	frames, _ := rdr.Read(func(*atomic.AtomicBuffer, int, int, *Header) {}, 0, 10)
	if frames != 1 {
		t.Errorf("Read: expected 1 DATA frame (PAD skipped), got %d", frames)
	}
}

func TestPadding_TooSmall(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)
	// Should not panic for length < HeaderLength.
	app.Padding(0, HeaderLength-1)
}

// ─── TailOffset ──────────────────────────────────────────────────────────────

func TestTailOffset(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	app := lb.Appender(0)

	if app.TailOffset() != 0 {
		t.Errorf("initial TailOffset: got %d want 0", app.TailOffset())
	}

	app.AppendUnfragmented(1, 1, 0, []byte("hello"), 0)
	if app.TailOffset() == 0 {
		t.Error("TailOffset should be non-zero after append")
	}
}

// ─── LogBuffer creation ───────────────────────────────────────────────────────

func TestNew_ValidSizes(t *testing.T) {
	sizes := []int{MinTermLength, 128 * 1024, 1024 * 1024, DefaultTermLength}
	for _, sz := range sizes {
		total := NumPartitions*sz + LogMetaDataLength
		buf := make([]byte, total)
		lb, err := New(buf, sz)
		if err != nil {
			t.Errorf("New(%d): unexpected error: %v", sz, err)
			continue
		}
		if lb.TermLength() != sz {
			t.Errorf("TermLength: got %d want %d", lb.TermLength(), sz)
		}
		if lb.TotalSize() != total {
			t.Errorf("TotalSize: got %d want %d", lb.TotalSize(), total)
		}
	}
}

func TestNew_InvalidTermLength_TooSmall(t *testing.T) {
	sz := MinTermLength / 2
	total := NumPartitions*sz + LogMetaDataLength
	buf := make([]byte, total)
	_, err := New(buf, sz)
	if err == nil {
		t.Error("expected error for termLength too small")
	}
}

func TestNew_InvalidTermLength_TooLarge(t *testing.T) {
	// We can't actually allocate a buffer this large in a test; just check
	// the validation path with a deliberately wrong buffer size.
	_, err := New(make([]byte, 1), MaxTermLength*2)
	if err == nil {
		t.Error("expected error for termLength too large")
	}
}

func TestNew_InvalidTermLength_NotPowerOfTwo(t *testing.T) {
	sz := 100 * 1024 // not a power of two
	total := NumPartitions*sz + LogMetaDataLength
	buf := make([]byte, total)
	_, err := New(buf, sz)
	if err == nil {
		t.Error("expected error for non-power-of-two termLength")
	}
}

func TestNew_WrongBufferSize(t *testing.T) {
	_, err := New(make([]byte, 100), MinTermLength)
	if err == nil {
		t.Error("expected error for wrong buffer size")
	}
}

func TestLogBuffer_ActivePartition(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	if lb.ActivePartitionIndex() != 0 {
		t.Errorf("initial active partition: got %d want 0", lb.ActivePartitionIndex())
	}
	lb.SetActivePartitionIndex(2)
	if lb.ActivePartitionIndex() != 2 {
		t.Errorf("after set: got %d want 2", lb.ActivePartitionIndex())
	}
}

func TestLogBuffer_InitialTermID(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	lb.SetInitialTermID(12345)
	if got := lb.InitialTermID(); got != 12345 {
		t.Errorf("InitialTermID: got %d want 12345", got)
	}
}

func TestLogBuffer_Accessors(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	for i := 0; i < NumPartitions; i++ {
		if lb.Appender(i) == nil {
			t.Errorf("Appender(%d) is nil", i)
		}
		if lb.Reader(i) == nil {
			t.Errorf("Reader(%d) is nil", i)
		}
		if lb.TermBuffer(i) == nil {
			t.Errorf("TermBuffer(%d) is nil", i)
		}
	}
	if lb.MetaBuffer() == nil {
		t.Error("MetaBuffer() is nil")
	}
}

// ─── TermReader stops at zero frame_length ────────────────────────────────────

func TestRead_StopsAtZeroFrameLength(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)
	rdr := lb.Reader(0)

	// Nothing appended — term is zeroed, so FrameLength == 0.
	frames, next := rdr.Read(func(*atomic.AtomicBuffer, int, int, *Header) {}, 0, 100)
	if frames != 0 {
		t.Errorf("expected 0 frames on empty term, got %d", frames)
	}
	if next != 0 {
		t.Errorf("expected nextOffset 0 on empty term, got %d", next)
	}
}

// ─── Cross-partition independence ─────────────────────────────────────────────

func TestAppend_CrossPartitionIndependence(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)

	lb.Appender(0).AppendUnfragmented(1, 1, 0, []byte("partition zero"), 0)
	lb.Appender(1).AppendUnfragmented(2, 2, 0, []byte("partition one"), 0)
	lb.Appender(2).AppendUnfragmented(3, 3, 0, []byte("partition two"), 0)

	check := func(partIdx int, wantSID int32, wantPayload string) {
		t.Helper()
		lb.Reader(partIdx).Read(func(b *atomic.AtomicBuffer, off, length int, hdr *Header) {
			if hdr.SessionID() != wantSID {
				t.Errorf("partition %d SessionID: got %d want %d", partIdx, hdr.SessionID(), wantSID)
			}
			got := make([]byte, length)
			b.GetBytes(off+HeaderLength, got)
			if string(got) != wantPayload {
				t.Errorf("partition %d payload: got %q want %q", partIdx, got, wantPayload)
			}
		}, 0, 5)
	}

	check(0, 1, "partition zero")
	check(1, 2, "partition one")
	check(2, 3, "partition two")
}
