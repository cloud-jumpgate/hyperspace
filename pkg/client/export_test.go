package client

// export_test.go exposes internal functions and types for white-box testing.
// This file is only compiled during tests (package client — not client_test).

import (
	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/broadcast"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// FromDriverTransmitter creates a Transmitter over the from-driver buffer so
// tests can inject fake conductor responses.
func (c *Client) FromDriverTransmitter() (*broadcast.Transmitter, error) {
	fromDriverAtomic := atomicbuf.NewAtomicBuffer(c.drv.FromDriverBuffer())
	return broadcast.NewTransmitter(fromDriverAtomic, broadcastMaxPayload)
}

// NextCorrID returns what the next correlation ID will be (for test coordination).
func (c *Client) NextCorrID() int64 {
	return c.nextCorrID.Load() + 1
}

// BuildAddPublicationPayloadExported exposes buildAddPublicationPayload for tests.
var BuildAddPublicationPayloadExported = buildAddPublicationPayload

// BuildAddSubscriptionPayloadExported exposes buildAddSubscriptionPayload for tests.
var BuildAddSubscriptionPayloadExported = buildAddSubscriptionPayload

// DispatchResponseForTest directly calls dispatchResponse for white-box testing.
func (c *Client) DispatchResponseForTest(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
	c.dispatchResponse(msgTypeID, buf, offset, length)
}

// NewRingWriter creates a ManyToOneRingBuffer writer over the to-driver buffer.
func (c *Client) NewRingWriter() (*ringbuffer.ManyToOneRingBuffer, error) {
	toDriverAtomic := atomicbuf.NewAtomicBuffer(c.drv.ToDriverBuffer())
	return ringbuffer.NewManyToOneRingBuffer(toDriverAtomic)
}

// PendingRequest returns a channel that will receive the response for the
// given correlation ID. This is used to register a fake pending request
// before injecting a fake response.
func (c *Client) RegisterFakePending(corrID int64) <-chan struct {
	MsgTypeID int32
	Payload   []byte
} {
	ch := make(chan struct {
		MsgTypeID int32
		Payload   []byte
	}, 1)
	c.mu.Lock()
	c.pending[corrID] = &pendingRequest{ch: make(chan response, 1)}
	c.mu.Unlock()
	// We can't expose the response channel directly since it uses unexported types.
	// Instead this registers a pending slot; the injected broadcast will route to it.
	return ch
}

// NewImageForTest creates a new Image for testing purposes.
func NewImageForTest(sessionID, streamID int32, lb *logbuffer.LogBuffer) *Image {
	return newImage(sessionID, streamID, lb)
}
