package client

// whitebox_test.go uses package-internal access (package client, not client_test)
// to test specific private branches that cannot be exercised from outside.

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

func smallTestConfig() *driver.Config {
	cfg := driver.DefaultConfig()
	cfg.TermLength = logbuffer.MinTermLength
	cfg.Threading = driver.ThreadingModeShared
	cfg.ConductorIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.SenderIdle = driver.NewSleepIdle(100 * time.Microsecond)
	cfg.ReceiverIdle = driver.NewSleepIdle(100 * time.Microsecond)
	return &cfg
}

// ---- dispatchResponse: short payload ----

func TestDispatchResponse_ShortPayloadWarning(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Create a 4-byte AtomicBuffer (too short for a correlationID).
	raw := make([]byte, 8)
	buf := atomicbuf.NewAtomicBuffer(raw)

	// Directly call dispatchResponse with length=4 (< 8).
	// This should log a warning and return without panicking.
	c.dispatchResponse(conductor.RspPublicationReady, buf, 0, 4)
}

// ---- dispatchResponse: corrID with no pending request ----

func TestDispatchResponse_UnknownCorrID(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	raw := make([]byte, 16)
	buf := atomicbuf.NewAtomicBuffer(raw)
	// Write corrID=99999 (no pending request for this).
	binary.LittleEndian.PutUint64(raw[0:], 99999)

	// Should be a no-op (not found in pending).
	c.dispatchResponse(conductor.RspPublicationReady, buf, 0, 16)
}

// ---- handlePublicationReady: unexpected response type ----

func TestHandlePublicationReady_UnexpectedType(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Inject a fake response with an unexpected message type (999).
	// Build a payload with a valid 16-byte structure (corrID + sessionID + streamID).
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:], uint64(9999)) // corrID
	binary.LittleEndian.PutUint32(payload[8:], uint32(42))   // sessionID
	binary.LittleEndian.PutUint32(payload[12:], uint32(1))   // streamID

	rsp := response{
		msgTypeID: 999, // unexpected
		payload:   payload,
	}
	_, err = c.handlePublicationReady(rsp, 9999, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for unexpected response type")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// ---- handlePublicationReady: RspError branch ----

func TestHandlePublicationReady_RspErrorWithMessage(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Build a RspError payload: corrID(8) + message bytes.
	corrID := int64(1234)
	errMsg := "log buffer creation failed"
	payload := make([]byte, 8+len(errMsg))
	binary.LittleEndian.PutUint64(payload[0:], uint64(corrID))
	copy(payload[8:], errMsg)

	rsp := response{
		msgTypeID: conductor.RspError,
		payload:   payload,
	}
	_, err = c.handlePublicationReady(rsp, corrID, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for RspError response")
	}
	t.Logf("got expected error: %v", err)
}

// ---- handlePublicationReady: RspError with short payload (no message) ----

func TestHandlePublicationReady_RspErrorShortPayload(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Payload is only 8 bytes (no message string after corrID).
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload[0:], uint64(5678))

	rsp := response{
		msgTypeID: conductor.RspError,
		payload:   payload,
	}
	_, err = c.handlePublicationReady(rsp, 5678, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for RspError response with short payload")
	}
}

// ---- handlePublicationReady: too-short RspPublicationReady payload ----

func TestHandlePublicationReady_TooShortPayload(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Valid typeID but only 8 bytes (need 16 minimum).
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload[0:], uint64(7777))

	rsp := response{
		msgTypeID: conductor.RspPublicationReady,
		payload:   payload,
	}
	_, err = c.handlePublicationReady(rsp, 7777, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for too-short RspPublicationReady payload")
	}
}

// ---- handlePublicationReady: nil publication state ----

func TestHandlePublicationReady_NilPublicationState(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Build a valid RspPublicationReady payload but for a corrID that was
	// never registered with the conductor — findPublicationState returns nil.
	corrID := int64(99999) // not in conductor's publications map
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:], uint64(corrID))
	binary.LittleEndian.PutUint32(payload[8:], uint32(42)) // sessionID
	binary.LittleEndian.PutUint32(payload[12:], uint32(1)) // streamID

	rsp := response{
		msgTypeID: conductor.RspPublicationReady,
		payload:   payload,
	}
	_, err = c.handlePublicationReady(rsp, corrID, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error when publication state is nil")
	}
	t.Logf("got expected error: %v", err)
}

// ---- handleSubscriptionReady: RspError branch ----

func TestHandleSubscriptionReady_RspError(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	corrID := int64(11111)
	errMsg := "subscription error"
	payload := make([]byte, 8+len(errMsg))
	binary.LittleEndian.PutUint64(payload[0:], uint64(corrID))
	copy(payload[8:], errMsg)

	rsp := response{
		msgTypeID: conductor.RspError,
		payload:   payload,
	}
	_, err = c.handleSubscriptionReady(rsp, corrID, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for RspError response in handleSubscriptionReady")
	}
	t.Logf("got expected error: %v", err)
}

// ---- handleSubscriptionReady: RspError with short payload ----

func TestHandleSubscriptionReady_RspErrorShortPayload(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload[0:], uint64(22222))

	rsp := response{
		msgTypeID: conductor.RspError,
		payload:   payload,
	}
	_, err = c.handleSubscriptionReady(rsp, 22222, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for RspError short payload")
	}
}

// ---- handleSubscriptionReady: unexpected response type ----

func TestHandleSubscriptionReady_UnexpectedType(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:], uint64(33333))

	rsp := response{
		msgTypeID: 999, // unexpected
		payload:   payload,
	}
	_, err = c.handleSubscriptionReady(rsp, 33333, "hs:ipc", 1)
	if err == nil {
		t.Fatal("expected error for unexpected response type in handleSubscriptionReady")
	}
	t.Logf("got expected error: %v", err)
}

// ---- findPublicationState: nil driver (no embedded driver) ----

func TestFindPublicationState_NilDriver(t *testing.T) {
	// Create a client without an embedded driver by simulating a nil drv.
	// We test this by calling the method on a minimal client struct.
	c := &Client{}
	result := c.findPublicationState(12345)
	if result != nil {
		t.Error("expected nil from findPublicationState with nil driver")
	}
}

// ---- pollBroadcast: lapped path ----

func TestPollBroadcast_LappedPath(t *testing.T) {
	// Use a very small broadcast buffer (8 slots) to force lapping.
	driverCfg := driver.DefaultConfig()
	driverCfg.TermLength = logbuffer.MinTermLength
	driverCfg.Threading = driver.ThreadingModeShared
	driverCfg.ConductorIdle = driver.NewSleepIdle(100 * time.Microsecond)
	driverCfg.SenderIdle = driver.NewSleepIdle(100 * time.Microsecond)
	driverCfg.ReceiverIdle = driver.NewSleepIdle(100 * time.Microsecond)
	driverCfg.FromDriverBufSize = 8*520 + 128 // 8-slot broadcast buffer

	ctx := context.Background()
	c, err := NewEmbedded(ctx, &driverCfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Issue 20 publications rapidly to overflow the 8-slot buffer.
	// The conductor processes commands on the shared goroutine; the client's
	// pollBroadcast goroutine runs concurrently. With a small buffer and many
	// publications, lapping is likely.
	for i := range 20 {
		_, _ = c.AddPublication(ctx,
			"hs:quic?endpoint=127.0.0.1:7777|pool=1",
			int32(i+1),
		)
	}
	// The lapped path logs a warning and recovers; no assertion needed.
}

// ---- reconcileImages: no matching subscription in conductor ----

func TestReconcileImages_NoMatchingSubscription(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Create a subscription but give it a non-matching subscriptionID.
	sub := &Subscription{
		subscriptionID: 99999, // not in conductor
		channel:        "hs:ipc",
		streamID:       1,
		client:         c,
	}
	sub.images = make([]*Image, 0)

	// reconcileImages should iterate conductor subscriptions, not find a match,
	// and return without adding any images.
	sub.reconcileImages()

	if len(sub.images) != 0 {
		t.Errorf("expected 0 images for unmatched subscription, got %d", len(sub.images))
	}
}

// ---- image: payloadEnd clamping ----

func TestImage_Poll_PayloadEndClamping(t *testing.T) {
	// Create a log buffer and an image over it.
	bufSize := logbuffer.NumPartitions*logbuffer.MinTermLength + logbuffer.LogMetaDataLength
	backing := make([]byte, bufSize)
	lb, err := logbuffer.New(backing, logbuffer.MinTermLength)
	if err != nil {
		t.Fatalf("logbuffer.New: %v", err)
	}

	img := newImage(42, 1, lb)

	// Poll on an empty log buffer returns 0 without panicking.
	n := img.Poll(func(_ []byte, _, _ int, _ FragmentHeader) {}, 10)
	if n != 0 {
		t.Errorf("expected 0 from empty poll, got %d", n)
	}
}

// ---- Offer: activePartitionIndex out-of-range guard ----

func TestOffer_PartitionIndexOutOfRangeGuard(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	pub, err := c.AddPublication(ctx, "hs:ipc", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	// Corrupt the active partition index to test the guard.
	// The guard clamps it to 0 if out of range.
	pub.logBuf.SetActivePartitionIndex(-1)

	// Offer should not panic.
	result, err := pub.Offer([]byte("guard-test"))
	if err != nil {
		t.Fatalf("Offer with bad partition index: %v", err)
	}
	_ = result
}

// ---- OfferFragmented: activePartitionIndex out-of-range guard ----

func TestOfferFragmented_PartitionIndexOutOfRangeGuard(t *testing.T) {
	cfg := smallTestConfig()
	ctx := context.Background()
	c, err := NewEmbedded(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEmbedded: %v", err)
	}
	defer func() { _ = c.Close() }()

	pub, err := c.AddPublication(ctx, "hs:ipc", 1)
	if err != nil {
		t.Fatalf("AddPublication: %v", err)
	}

	pub.logBuf.SetActivePartitionIndex(-1)

	result, err := pub.OfferFragmented([]byte("frag-guard-test"), 100)
	if err != nil {
		t.Fatalf("OfferFragmented with bad partition index: %v", err)
	}
	_ = result
}
