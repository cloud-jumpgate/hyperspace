package broadcast

import (
	"errors"
	"testing"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
)

const testMaxPayload = 64

// makeBuf creates a buffer sized for the given number of slots (must be power of 2).
func makeBuf(slotCount int) *atomicbuf.AtomicBuffer {
	slotBytes := RecordHeaderLength + testMaxPayload
	total := slotBytes*slotCount + TrailerLength
	return atomicbuf.NewAtomicBuffer(make([]byte, total))
}

// --- Construction ---

func TestNewTransmitterValid(t *testing.T) {
	buf := makeBuf(8)
	tx, err := NewTransmitter(buf, testMaxPayload)
	if err != nil {
		t.Fatalf("NewTransmitter: %v", err)
	}
	if tx == nil {
		t.Fatal("NewTransmitter returned nil")
	}
}

func TestNewTransmitterInvalidBuffer(t *testing.T) {
	// Buffer too small
	buf := atomicbuf.NewAtomicBuffer(make([]byte, 10))
	_, err := NewTransmitter(buf, testMaxPayload)
	if err == nil {
		t.Error("expected error for undersized buffer")
	}
}

func TestNewTransmitterNonPowerOfTwoSlots(t *testing.T) {
	// 3 slots * slotBytes + trailer — not power of 2
	slotBytes := RecordHeaderLength + testMaxPayload
	buf := atomicbuf.NewAtomicBuffer(make([]byte, 3*slotBytes+TrailerLength))
	_, err := NewTransmitter(buf, testMaxPayload)
	if err == nil {
		t.Error("expected error for non-power-of-2 slot count")
	}
}

func TestNewReceiverValid(t *testing.T) {
	buf := makeBuf(8)
	rx, err := NewReceiver(buf, testMaxPayload)
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	if rx == nil {
		t.Fatal("NewReceiver returned nil")
	}
}

func TestNewReceiverInvalidBuffer(t *testing.T) {
	buf := atomicbuf.NewAtomicBuffer(make([]byte, 4))
	_, err := NewReceiver(buf, testMaxPayload)
	if err == nil {
		t.Error("expected error for undersized buffer")
	}
}

func TestNewReceiverFromStartValid(t *testing.T) {
	buf := makeBuf(8)
	rx, err := NewReceiverFromStart(buf, testMaxPayload)
	if err != nil {
		t.Fatalf("NewReceiverFromStart: %v", err)
	}
	if rx == nil {
		t.Fatal("NewReceiverFromStart returned nil")
	}
}

func TestNewReceiverFromStartInvalidBuffer(t *testing.T) {
	buf := atomicbuf.NewAtomicBuffer(make([]byte, 4))
	_, err := NewReceiverFromStart(buf, testMaxPayload)
	if err == nil {
		t.Error("expected error for undersized buffer")
	}
}

// --- Transmit/Receive round-trip ---

func TestTransmitReceiveBasic(t *testing.T) {
	buf := makeBuf(8)
	tx, _ := NewTransmitter(buf, testMaxPayload)
	rx, _ := NewReceiverFromStart(buf, testMaxPayload)

	msg := []byte("hello broadcast")
	if err := tx.Transmit(1, msg); err != nil {
		t.Fatalf("Transmit: %v", err)
	}

	var received []byte
	var receivedTypeID int32
	ok, err := rx.Receive(func(msgTypeID int32, b *atomicbuf.AtomicBuffer, offset, length int) {
		received = make([]byte, length)
		b.GetBytes(offset, received)
		receivedTypeID = msgTypeID
	})

	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if !ok {
		t.Fatal("Receive returned false, expected message")
	}
	if string(received) != string(msg) {
		t.Errorf("expected %q, got %q", msg, received)
	}
	if receivedTypeID != 1 {
		t.Errorf("expected msgTypeID=1, got %d", receivedTypeID)
	}
}

func TestReceiveOnEmptyBuffer(t *testing.T) {
	buf := makeBuf(8)
	rx, _ := NewReceiver(buf, testMaxPayload)

	ok, err := rx.Receive(func(int32, *atomicbuf.AtomicBuffer, int, int) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false on empty buffer")
	}
}

func TestTransmitMessageTooLarge(t *testing.T) {
	buf := makeBuf(8)
	tx, _ := NewTransmitter(buf, testMaxPayload)

	huge := make([]byte, testMaxPayload+1)
	err := tx.Transmit(1, huge)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Errorf("expected ErrMessageTooLarge, got %v", err)
	}
}

func TestTransmitZeroLengthMessage(t *testing.T) {
	buf := makeBuf(8)
	tx, _ := NewTransmitter(buf, testMaxPayload)
	rx, _ := NewReceiverFromStart(buf, testMaxPayload)

	if err := tx.Transmit(42, []byte{}); err != nil {
		t.Fatalf("Transmit zero-length: %v", err)
	}

	var gotTypeID int32
	var gotLen int
	ok, err := rx.Receive(func(msgTypeID int32, b *atomicbuf.AtomicBuffer, offset, length int) {
		gotTypeID = msgTypeID
		gotLen = length
	})

	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if !ok {
		t.Fatal("expected message, got false")
	}
	if gotTypeID != 42 {
		t.Errorf("expected typeID=42, got %d", gotTypeID)
	}
	if gotLen != 0 {
		t.Errorf("expected length=0, got %d", gotLen)
	}
}

// --- Multiple messages ---

func TestMultipleMessages(t *testing.T) {
	buf := makeBuf(16)
	tx, _ := NewTransmitter(buf, testMaxPayload)
	rx, _ := NewReceiverFromStart(buf, testMaxPayload)

	messages := []string{"one", "two", "three", "four", "five"}
	for i, m := range messages {
		if err := tx.Transmit(int32(i+1), []byte(m)); err != nil {
			t.Fatalf("Transmit(%d): %v", i, err)
		}
	}

	var received []string
	for {
		ok, err := rx.Receive(func(_ int32, b *atomicbuf.AtomicBuffer, offset, length int) {
			data := make([]byte, length)
			b.GetBytes(offset, data)
			received = append(received, string(data))
		})
		if err != nil {
			t.Fatalf("Receive error: %v", err)
		}
		if !ok {
			break
		}
	}

	if len(received) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(received))
	}
	for i, m := range messages {
		if received[i] != m {
			t.Errorf("message %d: expected %q, got %q", i, m, received[i])
		}
	}
}

// --- Multiple receivers ---

func TestMultipleReceivers(t *testing.T) {
	buf := makeBuf(16)
	tx, _ := NewTransmitter(buf, testMaxPayload)
	rx1, _ := NewReceiverFromStart(buf, testMaxPayload)
	rx2, _ := NewReceiverFromStart(buf, testMaxPayload)

	messages := []string{"alpha", "beta", "gamma"}
	for i, m := range messages {
		if err := tx.Transmit(int32(i+1), []byte(m)); err != nil {
			t.Fatalf("Transmit: %v", err)
		}
	}

	readAll := func(rx *Receiver) []string {
		var got []string
		for {
			ok, err := rx.Receive(func(_ int32, b *atomicbuf.AtomicBuffer, offset, length int) {
				data := make([]byte, length)
				b.GetBytes(offset, data)
				got = append(got, string(data))
			})
			if err != nil {
				t.Errorf("Receive error: %v", err)
				break
			}
			if !ok {
				break
			}
		}
		return got
	}

	got1 := readAll(rx1)
	got2 := readAll(rx2)

	if len(got1) != len(messages) {
		t.Errorf("receiver1: expected %d, got %d", len(messages), len(got1))
	}
	if len(got2) != len(messages) {
		t.Errorf("receiver2: expected %d, got %d", len(messages), len(got2))
	}
	for i, m := range messages {
		if got1[i] != m {
			t.Errorf("rx1 message %d: expected %q, got %q", i, m, got1[i])
		}
		if got2[i] != m {
			t.Errorf("rx2 message %d: expected %q, got %q", i, m, got2[i])
		}
	}
}

// --- Four receivers (F-002 contract requirement) ---
//
// TestBroadcast_MultipleReceivers verifies that exactly 4 independent receivers
// each receive every message published by the transmitter, as specified in the
// F-002 sprint contract.

func TestBroadcast_MultipleReceivers(t *testing.T) {
	const receiverCount = 4

	buf := makeBuf(16)
	tx, err := NewTransmitter(buf, testMaxPayload)
	if err != nil {
		t.Fatalf("NewTransmitter: %v", err)
	}

	receivers := make([]*Receiver, receiverCount)
	for i := 0; i < receiverCount; i++ {
		rx, err := NewReceiverFromStart(buf, testMaxPayload)
		if err != nil {
			t.Fatalf("NewReceiverFromStart[%d]: %v", i, err)
		}
		receivers[i] = rx
	}

	messages := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for i, m := range messages {
		if err := tx.Transmit(int32(i+1), []byte(m)); err != nil {
			t.Fatalf("Transmit[%d]: %v", i, err)
		}
	}

	readAll := func(rxIndex int, rx *Receiver) []string {
		var got []string
		for {
			ok, err := rx.Receive(func(_ int32, b *atomicbuf.AtomicBuffer, offset, length int) {
				data := make([]byte, length)
				b.GetBytes(offset, data)
				got = append(got, string(data))
			})
			if err != nil {
				t.Errorf("receiver[%d] Receive error: %v", rxIndex, err)
				break
			}
			if !ok {
				break
			}
		}
		return got
	}

	for i, rx := range receivers {
		got := readAll(i, rx)
		if len(got) != len(messages) {
			t.Errorf("receiver[%d]: expected %d messages, got %d", i, len(messages), len(got))
			continue
		}
		for j, m := range messages {
			if got[j] != m {
				t.Errorf("receiver[%d] message[%d]: expected %q, got %q", i, j, m, got[j])
			}
		}
	}
}

// --- Receiver that starts after some messages ---

func TestReceiverMissesEarlierMessages(t *testing.T) {
	buf := makeBuf(8)
	tx, _ := NewTransmitter(buf, testMaxPayload)

	// Send messages before creating receiver
	if err := tx.Transmit(1, []byte("old")); err != nil {
		t.Fatalf("Transmit: %v", err)
	}
	if err := tx.Transmit(2, []byte("older")); err != nil {
		t.Fatalf("Transmit: %v", err)
	}

	// Create receiver after messages already sent
	rx, _ := NewReceiver(buf, testMaxPayload)

	// Receiver should see nothing (cursor starts at current head)
	ok, err := rx.Receive(func(int32, *atomicbuf.AtomicBuffer, int, int) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("new receiver should not see old messages")
	}

	// Now transmit a new message — receiver should see it
	if err := tx.Transmit(3, []byte("new")); err != nil {
		t.Fatalf("Transmit: %v", err)
	}
	ok, err = rx.Receive(func(_ int32, b *atomicbuf.AtomicBuffer, offset, length int) {
		data := make([]byte, length)
		b.GetBytes(offset, data)
		if string(data) != "new" {
			t.Errorf("expected 'new', got %q", string(data))
		}
	})
	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if !ok {
		t.Error("expected new message to be received")
	}
}

// --- Lapping detection ---

func TestLappingDetection(t *testing.T) {
	// Use a tiny ring (4 slots) so it's easy to wrap
	buf := makeBuf(4)
	tx, _ := NewTransmitter(buf, testMaxPayload)
	rx, _ := NewReceiverFromStart(buf, testMaxPayload)

	// Transmit more messages than the ring can hold, without consuming
	for i := 0; i < 8; i++ {
		if err := tx.Transmit(int32(i+1), []byte("data")); err != nil {
			t.Fatalf("Transmit[%d]: %v", i, err)
		}
	}

	// Receiver should detect lapping
	var lapped bool
	for {
		ok, err := rx.Receive(func(int32, *atomicbuf.AtomicBuffer, int, int) {})
		if errors.Is(err, ErrLapped) {
			lapped = true
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			break
		}
	}

	if !lapped {
		t.Error("expected ErrLapped when receiver falls behind")
	}
}

// --- isPowerOfTwo ---

func TestIsPowerOfTwo(t *testing.T) {
	cases := []struct {
		n    int
		want bool
	}{
		{0, false},
		{1, true},
		{2, true},
		{3, false},
		{4, true},
		{8, true},
		{7, false},
		{1024, true},
	}
	for _, c := range cases {
		if got := isPowerOfTwo(c.n); got != c.want {
			t.Errorf("isPowerOfTwo(%d) = %v, want %v", c.n, got, c.want)
		}
	}
}

// --- Ring wrap-around (transmitter writes more than slotCount) ---

func TestTransmitWrapAround(t *testing.T) {
	buf := makeBuf(8)
	tx, _ := NewTransmitter(buf, testMaxPayload)
	rx, _ := NewReceiverFromStart(buf, testMaxPayload)

	// Fill ring and read in cycles
	for round := 0; round < 4; round++ {
		for i := 0; i < 8; i++ {
			payload := []byte{byte(round*8 + i)}
			if err := tx.Transmit(1, payload); err != nil {
				t.Fatalf("round %d write %d: %v", round, i, err)
			}
		}
		var count int
		for {
			ok, err := rx.Receive(func(int32, *atomicbuf.AtomicBuffer, int, int) { count++ })
			if err != nil {
				t.Fatalf("round %d Receive: %v", round, err)
			}
			if !ok {
				break
			}
		}
		if count != 8 {
			t.Errorf("round %d: expected 8, got %d", round, count)
		}
	}
}

// --- NewReceiverFromStart with empty buffer ---

func TestNewReceiverFromStartOnEmptyBuffer(t *testing.T) {
	buf := makeBuf(8)
	rx, err := NewReceiverFromStart(buf, testMaxPayload)
	if err != nil {
		t.Fatalf("NewReceiverFromStart: %v", err)
	}

	ok, err := rx.Receive(func(int32, *atomicbuf.AtomicBuffer, int, int) {})
	if err != nil {
		t.Fatalf("Receive on empty: %v", err)
	}
	if ok {
		t.Error("expected false on empty buffer")
	}
}
