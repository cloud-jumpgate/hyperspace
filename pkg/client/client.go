// Package client provides the public API for the Hyperspace pub/sub system.
// Application code uses Client to add publications and subscriptions, which
// are backed by an embedded or external driver.
//
// Embedded mode runs all driver goroutines in-process (suitable for tests and
// single-binary deployments). External mode connects to a running hsd daemon
// via CnC shared memory (not yet implemented — reserved for Sprint 9).
package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
	"github.com/cloud-jumpgate/hyperspace/pkg/driver/conductor"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/broadcast"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
)

// broadcastMaxPayload must match the conductor's broadcastMaxPayload (512).
const broadcastMaxPayload = 512

// responseTimeout is the maximum time to wait for a conductor response.
const responseTimeout = 5 * time.Second

// ErrTimeout is returned when the conductor does not respond within responseTimeout.
var ErrTimeout = errors.New("client: timed out waiting for conductor response")

// ErrClosed is returned when an operation is attempted on a closed Client.
var ErrClosed = errors.New("client: client is closed")

// ErrConductorError is returned when the conductor broadcasts an error response.
var ErrConductorError = errors.New("client: conductor returned error")

// pendingRequest tracks a in-flight command waiting for a conductor response.
type pendingRequest struct {
	ch chan response
}

// response carries the conductor's reply for a single correlation ID.
type response struct {
	msgTypeID int32
	payload   []byte
}

// Client manages the connection to the driver (embedded or external hsd).
// It is safe to call AddPublication, AddSubscription, and Close from
// multiple goroutines concurrently.
type Client struct {
	drv         *driver.Driver // non-nil when embedded
	ring        *ringbuffer.ManyToOneRingBuffer
	rx          *broadcast.Receiver
	nextCorrID  atomic.Int64
	mu          sync.Mutex
	pending     map[int64]*pendingRequest
	publications  []*Publication
	subscriptions []*Subscription
	closed      atomic.Bool
	stopPoll    context.CancelFunc
	pollDone    chan struct{}
	clientID    int64
}

// NewEmbedded creates a Client backed by an embedded driver. All driver
// goroutines are started in-process. The driver is stopped when Close is called.
func NewEmbedded(ctx context.Context, cfg *driver.Config) (*Client, error) {
	if cfg == nil {
		def := driver.DefaultConfig()
		cfg = &def
	}

	d, err := driver.NewEmbedded(*cfg)
	if err != nil {
		return nil, fmt.Errorf("client: create embedded driver: %w", err)
	}

	if err := d.Start(ctx); err != nil {
		return nil, fmt.Errorf("client: start embedded driver: %w", err)
	}

	toDriverAtomic := atomicbuf.NewAtomicBuffer(d.ToDriverBuffer())
	fromDriverAtomic := atomicbuf.NewAtomicBuffer(d.FromDriverBuffer())

	ring, err := ringbuffer.NewManyToOneRingBuffer(toDriverAtomic)
	if err != nil {
		d.Stop()
		return nil, fmt.Errorf("client: create ring buffer view: %w", err)
	}

	// Use NewReceiverFromStart so we catch responses to commands issued
	// immediately after creation. The conductor writes responses after
	// processing commands, so starting at the current tail is safe — any
	// response to our first command will be newer than the tail at creation.
	rx, err := broadcast.NewReceiver(fromDriverAtomic, broadcastMaxPayload)
	if err != nil {
		d.Stop()
		return nil, fmt.Errorf("client: create broadcast receiver: %w", err)
	}

	c := &Client{
		drv:      d,
		ring:     ring,
		rx:       rx,
		pending:  make(map[int64]*pendingRequest),
		pollDone: make(chan struct{}),
		clientID: time.Now().UnixNano(),
	}

	pollCtx, cancel := context.WithCancel(context.Background())
	c.stopPoll = cancel
	go c.pollBroadcast(pollCtx)

	slog.Info("client: embedded driver started", "client_id", c.clientID)
	return c, nil
}

// NewExternal connects to an external hsd daemon via CnC shared memory.
// This mode is reserved for Sprint 9 (AWS + Identity). It returns an
// error in the current sprint.
func NewExternal(_ context.Context, _ string) (*Client, error) {
	return nil, errors.New("client: external mode not yet implemented (Sprint 9)")
}

// AddPublication registers a publication for channel+streamID.
// Blocks until the Conductor replies PublicationReady or ctx is cancelled.
// Returns ErrTimeout if the conductor does not respond within responseTimeout.
func (c *Client) AddPublication(ctx context.Context, channel string, streamID int32) (*Publication, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}

	corrID := c.nextCorrID.Add(1)

	payload := buildAddPublicationPayload(corrID, streamID, channel)

	req := &pendingRequest{ch: make(chan response, 1)}
	c.mu.Lock()
	c.pending[corrID] = req
	c.mu.Unlock()

	if !c.ring.Write(conductor.CmdAddPublication, payload) {
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
		return nil, errors.New("client: command ring full — back pressure on AddPublication")
	}

	slog.Debug("client: sent CmdAddPublication",
		"corr_id", corrID,
		"stream_id", streamID,
		"channel", channel,
	)

	select {
	case rsp := <-req.ch:
		return c.handlePublicationReady(rsp, corrID, channel, streamID)
	case <-time.After(responseTimeout):
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
		return nil, ErrTimeout
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) handlePublicationReady(rsp response, corrID int64, channel string, streamID int32) (*Publication, error) {
	if rsp.msgTypeID == conductor.RspError {
		msg := ""
		if len(rsp.payload) > 8 {
			msg = string(rsp.payload[8:])
		}
		return nil, fmt.Errorf("%w: %s", ErrConductorError, msg)
	}
	if rsp.msgTypeID != conductor.RspPublicationReady {
		return nil, fmt.Errorf("client: unexpected response type %d for AddPublication", rsp.msgTypeID)
	}

	// Payload: correlationID(8) + sessionID(4) + streamID(4)
	if len(rsp.payload) < 16 {
		return nil, fmt.Errorf("client: RspPublicationReady payload too short: %d", len(rsp.payload))
	}
	sessionID := int32(binary.LittleEndian.Uint32(rsp.payload[8:]))

	// Retrieve the log buffer from the conductor's publication state.
	pubState := c.findPublicationState(corrID)
	if pubState == nil {
		return nil, fmt.Errorf("client: no publication state for corr_id %d", corrID)
	}

	pub := newPublication(corrID, channel, streamID, sessionID, pubState.LogBuf, c)

	c.mu.Lock()
	c.publications = append(c.publications, pub)
	c.mu.Unlock()

	slog.Info("client: publication ready",
		"corr_id", corrID,
		"session_id", sessionID,
		"stream_id", streamID,
		"channel", channel,
	)
	return pub, nil
}

// AddSubscription registers a subscription for channel+streamID.
// Blocks until the Conductor replies SubscriptionReady or ctx is cancelled.
func (c *Client) AddSubscription(ctx context.Context, channel string, streamID int32) (*Subscription, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}

	corrID := c.nextCorrID.Add(1)
	payload := buildAddSubscriptionPayload(corrID, streamID, channel)

	req := &pendingRequest{ch: make(chan response, 1)}
	c.mu.Lock()
	c.pending[corrID] = req
	c.mu.Unlock()

	if !c.ring.Write(conductor.CmdAddSubscription, payload) {
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
		return nil, errors.New("client: command ring full — back pressure on AddSubscription")
	}

	slog.Debug("client: sent CmdAddSubscription",
		"corr_id", corrID,
		"stream_id", streamID,
		"channel", channel,
	)

	select {
	case rsp := <-req.ch:
		return c.handleSubscriptionReady(rsp, corrID, channel, streamID)
	case <-time.After(responseTimeout):
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
		return nil, ErrTimeout
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, corrID)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) handleSubscriptionReady(rsp response, corrID int64, channel string, streamID int32) (*Subscription, error) {
	if rsp.msgTypeID == conductor.RspError {
		msg := ""
		if len(rsp.payload) > 8 {
			msg = string(rsp.payload[8:])
		}
		return nil, fmt.Errorf("%w: %s", ErrConductorError, msg)
	}
	if rsp.msgTypeID != conductor.RspSubscriptionReady {
		return nil, fmt.Errorf("client: unexpected response type %d for AddSubscription", rsp.msgTypeID)
	}

	sub := newSubscription(corrID, channel, streamID, c)

	c.mu.Lock()
	c.subscriptions = append(c.subscriptions, sub)
	c.mu.Unlock()

	slog.Info("client: subscription ready",
		"corr_id", corrID,
		"stream_id", streamID,
		"channel", channel,
	)
	return sub, nil
}

// removePublication sends CmdRemovePublication and removes it from the client's list.
func (c *Client) removePublication(pub *Publication) error {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, uint64(pub.publicationID))
	c.ring.Write(conductor.CmdRemovePublication, payload)

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, p := range c.publications {
		if p == pub {
			c.publications = append(c.publications[:i], c.publications[i+1:]...)
			break
		}
	}
	return nil
}

// removeSubscription sends CmdRemoveSubscription and removes it from the client's list.
func (c *Client) removeSubscription(sub *Subscription) error {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, uint64(sub.subscriptionID))
	c.ring.Write(conductor.CmdRemoveSubscription, payload)

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.subscriptions {
		if s == sub {
			c.subscriptions = append(c.subscriptions[:i], c.subscriptions[i+1:]...)
			break
		}
	}
	return nil
}

// Close cleans up all publications and subscriptions and stops the driver.
// Close is safe to call multiple times; subsequent calls return nil.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Signal the broadcast poll goroutine to stop.
	c.stopPoll()
	<-c.pollDone

	// Close all active publications and subscriptions.
	c.mu.Lock()
	pubs := make([]*Publication, len(c.publications))
	copy(pubs, c.publications)
	subs := make([]*Subscription, len(c.subscriptions))
	copy(subs, c.subscriptions)
	c.mu.Unlock()

	for _, p := range pubs {
		_ = p.Close()
	}
	for _, s := range subs {
		_ = s.Close()
	}

	if c.drv != nil {
		c.drv.Stop()
	}

	slog.Info("client: closed", "client_id", c.clientID)
	return nil
}

// Adaptive backoff constants for pollBroadcast (A-05 fix).
const (
	pollMinSleep       = 100 * time.Microsecond // initial/active sleep
	pollMaxSleep       = 1 * time.Millisecond   // maximum idle sleep
	pollIdleThreshold  = 10                     // idle cycles before increasing sleep
)

// pollBroadcast continuously reads from the broadcast receiver and dispatches
// responses to waiting requests. Runs in its own goroutine until ctx is cancelled.
// A-05 fix: adaptive backoff -- starts at 100us, increases to 1ms after 10 idle cycles.
func (c *Client) pollBroadcast(ctx context.Context) {
	defer close(c.pollDone)

	idleCount := 0
	sleepDur := pollMinSleep

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		got, err := c.rx.Receive(func(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
			c.dispatchResponse(msgTypeID, buf, offset, length)
		})
		if err != nil {
			// ErrLapped: we missed messages; log and reconcile (A-04 fix).
			slog.Warn("client: broadcast receiver lapped -- messages missed", "error", err)
			c.reconcileAfterLap()
			idleCount = 0
			sleepDur = pollMinSleep
			continue
		}
		if got {
			// Reset backoff on any received message.
			idleCount = 0
			sleepDur = pollMinSleep
		} else {
			// Nothing available; adaptive yield (A-05 fix).
			idleCount++
			if idleCount >= pollIdleThreshold && sleepDur < pollMaxSleep {
				sleepDur = pollMaxSleep
			}
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(sleepDur)
			}
		}
	}
}

// dispatchResponse reads the correlation ID from a broadcast payload and
// routes it to the matching pending request channel.
func (c *Client) dispatchResponse(msgTypeID int32, buf *atomicbuf.AtomicBuffer, offset, length int) {
	if length < 8 {
		slog.Warn("client: broadcast message too short to contain corr_id", "length", length)
		return
	}

	raw := make([]byte, length)
	buf.GetBytes(offset, raw)

	corrID := int64(binary.LittleEndian.Uint64(raw[0:]))

	c.mu.Lock()
	req, ok := c.pending[corrID]
	if ok {
		delete(c.pending, corrID)
	}
	c.mu.Unlock()

	if ok {
		select {
		case req.ch <- response{msgTypeID: msgTypeID, payload: raw}:
		default:
		}
	}
}

// reconcileAfterLap re-queries conductor state to detect missed RspPublicationReady
// or RspSubscriptionReady responses after the broadcast receiver was lapped (A-04 fix).
// For each pending request whose correlationID matches a conductor publication or
// subscription, we synthesise the response as if the broadcast had been received.
func (c *Client) reconcileAfterLap() {
	if c.drv == nil {
		return
	}

	cond := c.drv.Conductor()
	pubs := cond.Publications()
	subs := cond.Subscriptions()

	c.mu.Lock()
	// Build a map of pending correlation IDs for fast lookup.
	pendingIDs := make(map[int64]*pendingRequest, len(c.pending))
	for id, req := range c.pending {
		pendingIDs[id] = req
	}
	c.mu.Unlock()

	reconciled := 0

	// Check publications: if conductor has a publication for a pending correlationID,
	// the RspPublicationReady was missed. Synthesise it.
	for _, pub := range pubs {
		req, ok := pendingIDs[pub.PublicationID]
		if !ok {
			continue
		}
		// Synthesise RspPublicationReady payload: correlationID(8) + sessionID(4) + streamID(4).
		payload := make([]byte, 16)
		binary.LittleEndian.PutUint64(payload[0:], uint64(pub.PublicationID))
		binary.LittleEndian.PutUint32(payload[8:], uint32(pub.SessionID))
		binary.LittleEndian.PutUint32(payload[12:], uint32(pub.StreamID))

		c.mu.Lock()
		delete(c.pending, pub.PublicationID)
		c.mu.Unlock()

		select {
		case req.ch <- response{msgTypeID: conductor.RspPublicationReady, payload: payload}:
			reconciled++
		default:
		}
	}

	// Check subscriptions: if conductor has a subscription for a pending correlationID,
	// the RspSubscriptionReady was missed.
	for _, sub := range subs {
		req, ok := pendingIDs[sub.SubscriptionID]
		if !ok {
			continue
		}
		payload := make([]byte, 12)
		binary.LittleEndian.PutUint64(payload[0:], uint64(sub.SubscriptionID))
		binary.LittleEndian.PutUint32(payload[8:], uint32(sub.StreamID))

		c.mu.Lock()
		delete(c.pending, sub.SubscriptionID)
		c.mu.Unlock()

		select {
		case req.ch <- response{msgTypeID: conductor.RspSubscriptionReady, payload: payload}:
			reconciled++
		default:
		}
	}

	if reconciled > 0 {
		slog.Info("client: reconciled missed responses after broadcast lap",
			"reconciled", reconciled,
		)
	}
}

// findPublicationState queries the embedded conductor for the publication state
// matching the given correlation/publication ID.
func (c *Client) findPublicationState(publicationID int64) *conductor.PublicationState {
	if c.drv == nil {
		return nil
	}
	pubs := c.drv.Conductor().Publications()
	for _, p := range pubs {
		if p.PublicationID == publicationID {
			return p
		}
	}
	return nil
}

// buildAddPublicationPayload encodes a CmdAddPublication payload.
// Layout: correlationID(8) + streamID(4) + channelLen(4) + channel(n).
func buildAddPublicationPayload(corrID int64, streamID int32, channel string) []byte {
	ch := []byte(channel)
	payload := make([]byte, 16+len(ch))
	binary.LittleEndian.PutUint64(payload[0:], uint64(corrID))
	binary.LittleEndian.PutUint32(payload[8:], uint32(streamID))
	binary.LittleEndian.PutUint32(payload[12:], uint32(len(ch)))
	copy(payload[16:], ch)
	return payload
}

// buildAddSubscriptionPayload encodes a CmdAddSubscription payload.
// Layout: correlationID(8) + streamID(4) + channelLen(4) + channel(n).
func buildAddSubscriptionPayload(corrID int64, streamID int32, channel string) []byte {
	ch := []byte(channel)
	payload := make([]byte, 16+len(ch))
	binary.LittleEndian.PutUint64(payload[0:], uint64(corrID))
	binary.LittleEndian.PutUint32(payload[8:], uint32(streamID))
	binary.LittleEndian.PutUint32(payload[12:], uint32(len(ch)))
	copy(payload[16:], ch)
	return payload
}
