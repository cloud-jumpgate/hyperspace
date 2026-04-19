// Package conductor implements the Conductor, the driver control plane agent.
// The Conductor reads client commands from a ManyToOneRingBuffer and writes
// responses into a broadcast Transmitter.
package conductor

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"log/slog"
	"sync"
	syncatomic "sync/atomic"
	"time"

	"github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/broadcast"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer"
	"github.com/cloud-jumpgate/hyperspace/pkg/logbuffer"
)

// Command type IDs on the to-driver ring buffer.
const (
	CmdAddPublication     = int32(1)
	CmdRemovePublication  = int32(2)
	CmdAddSubscription    = int32(3)
	CmdRemoveSubscription = int32(4)
	CmdClientKeepalive    = int32(5)
	CmdAddCounter         = int32(6)
)

// Response type IDs on the from-driver broadcast ring.
const (
	RspPublicationReady   = int32(101)
	RspSubscriptionReady  = int32(102)
	RspOnAvailableImage   = int32(103)
	RspOnUnavailableImage = int32(104)
	RspError              = int32(105)
)

// DefaultMaxCommandsPerCycle is the default maximum commands processed per DoWork call.
const DefaultMaxCommandsPerCycle = 10

// DefaultBroadcastMaxPayload is the default maximum payload for broadcast messages.
// Sized to fit the largest response: correlationID(8) + sessionID(4) + streamID(4) +
// channel string (up to 256 bytes) = 272. Use 512 for headroom.
const DefaultBroadcastMaxPayload = 512

// PublicationState holds live state for one publication.
type PublicationState struct {
	PublicationID int64
	SessionID     int32
	StreamID      int32
	TermID        int32
	Channel       string
	LogBuf        *logbuffer.LogBuffer
}

// SubscriptionState holds live state for one subscription.
type SubscriptionState struct {
	SubscriptionID int64
	StreamID       int32
	Channel        string
	Images         []*ImageState
}

// ImageState tracks one remote publisher's stream (image log buffer).
type ImageState struct {
	SessionID int32
	TermID    int32
	LogBuf    *logbuffer.LogBuffer
	Position  int64
}

// Conductor is the driver control plane agent.
type Conductor struct {
	toDriverRing    *ringbuffer.ManyToOneRingBuffer
	fromDriverTx    *broadcast.Transmitter
	publications    map[int64]*PublicationState
	subscriptions   map[int64]*SubscriptionState
	mu              sync.Mutex // only for external Admin() calls; DoWork is single-threaded
	termLength      int
	maxCmdsPerCycle int                 // F-02 fix: configurable max commands per DoWork
	clientAlive     map[int64]time.Time // correlationID → last keepalive time
	// Lock-free publication/subscription snapshots (P-03 fix).
	pubSnap syncatomic.Pointer[[]*PublicationState]
	subSnap syncatomic.Pointer[[]*SubscriptionState]
}

// New creates a Conductor.
// toDriverBuf: the ring buffer clients write commands into.
// fromDriverBuf: the broadcast buffer the conductor writes responses into.
// termLength: log buffer term size in bytes.
func New(toDriverBuf, fromDriverBuf *atomic.AtomicBuffer, termLength int) (*Conductor, error) {
	ring, err := ringbuffer.NewManyToOneRingBuffer(toDriverBuf)
	if err != nil {
		return nil, err
	}
	tx, err := broadcast.NewTransmitter(fromDriverBuf, DefaultBroadcastMaxPayload)
	if err != nil {
		return nil, err
	}

	c := &Conductor{
		toDriverRing:    ring,
		fromDriverTx:    tx,
		publications:    make(map[int64]*PublicationState),
		subscriptions:   make(map[int64]*SubscriptionState),
		clientAlive:     make(map[int64]time.Time),
		termLength:      termLength,
		maxCmdsPerCycle: DefaultMaxCommandsPerCycle,
	}
	// Initialise lock-free snapshots (P-03 fix).
	emptyPubs := make([]*PublicationState, 0)
	emptySubs := make([]*SubscriptionState, 0)
	c.pubSnap.Store(&emptyPubs)
	c.subSnap.Store(&emptySubs)
	return c, nil
}

// DoWork reads up to 10 commands from the ring buffer and processes each.
// Returns the number of commands processed.
func (c *Conductor) DoWork(_ context.Context) int {
	count := 0
	c.toDriverRing.Read(func(msgTypeID int32, buf *atomic.AtomicBuffer, offset, length int) {
		c.processCommand(msgTypeID, buf, offset, length)
		count++
	}, c.maxCmdsPerCycle)
	return count
}

// Name returns "conductor".
func (c *Conductor) Name() string { return "conductor" }

// Close releases all log buffers and clears state.
func (c *Conductor) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publications = make(map[int64]*PublicationState)
	c.subscriptions = make(map[int64]*SubscriptionState)
	c.publishPubSnapshot()
	c.publishSubSnapshot()
	return nil
}

// Publications returns a snapshot of current publication states (for Sender).
// P-03 fix: lock-free read via atomic.Pointer. No mutex on the hot path.
func (c *Conductor) Publications() []*PublicationState {
	return *c.pubSnap.Load()
}

// Subscriptions returns a snapshot of current subscription states (for Receiver).
// P-03 fix: lock-free read via atomic.Pointer. No mutex on the hot path.
func (c *Conductor) Subscriptions() []*SubscriptionState {
	return *c.subSnap.Load()
}

// publishPubSnapshot rebuilds and atomically publishes the publication snapshot.
// Called after any mutation to c.publications (under c.mu).
func (c *Conductor) publishPubSnapshot() {
	snap := make([]*PublicationState, 0, len(c.publications))
	for _, p := range c.publications {
		snap = append(snap, p)
	}
	c.pubSnap.Store(&snap)
}

// publishSubSnapshot rebuilds and atomically publishes the subscription snapshot.
// Called after any mutation to c.subscriptions (under c.mu).
func (c *Conductor) publishSubSnapshot() {
	snap := make([]*SubscriptionState, 0, len(c.subscriptions))
	for _, s := range c.subscriptions {
		snap = append(snap, s)
	}
	c.subSnap.Store(&snap)
}

// processCommand dispatches a single command from the ring buffer.
func (c *Conductor) processCommand(msgTypeID int32, buf *atomic.AtomicBuffer, offset, length int) {
	switch msgTypeID {
	case CmdAddPublication:
		c.handleAddPublication(buf, offset, length)
	case CmdRemovePublication:
		c.handleRemovePublication(buf, offset, length)
	case CmdAddSubscription:
		c.handleAddSubscription(buf, offset, length)
	case CmdRemoveSubscription:
		c.handleRemoveSubscription(buf, offset, length)
	case CmdClientKeepalive:
		c.handleClientKeepalive(buf, offset, length)
	case CmdAddCounter:
		// no-op in Sprint 3; counters are Sprint 8
		slog.Debug("conductor: CmdAddCounter received (no-op in sprint 3)")
	default:
		slog.Warn("conductor: unknown command type", "type", msgTypeID)
	}
}

// --- Command wire format (all little-endian) ---
//
// CmdAddPublication:
//   offset+0:  correlationID (int64)
//   offset+8:  streamID      (int32)
//   offset+12: channelLen    (int32)
//   offset+16: channel       ([]byte, channelLen bytes)
//
// CmdRemovePublication:
//   offset+0:  publicationID (int64)
//
// CmdAddSubscription:
//   offset+0:  correlationID (int64)
//   offset+8:  streamID      (int32)
//   offset+12: channelLen    (int32)
//   offset+16: channel       ([]byte, channelLen bytes)
//
// CmdRemoveSubscription:
//   offset+0:  subscriptionID (int64)
//
// CmdClientKeepalive:
//   offset+0:  clientID (int64)

func readInt64LE(buf *atomic.AtomicBuffer, offset int) int64 {
	return buf.GetInt64LE(offset)
}

func readInt32LE(buf *atomic.AtomicBuffer, offset int) int32 {
	return buf.GetInt32LE(offset)
}

func readString(buf *atomic.AtomicBuffer, offset, length int) string {
	if length <= 0 {
		return ""
	}
	b := make([]byte, length)
	buf.GetBytes(offset, b)
	return string(b)
}

func (c *Conductor) handleAddPublication(buf *atomic.AtomicBuffer, offset, length int) {
	if length < 16 {
		slog.Warn("conductor: CmdAddPublication payload too short", "length", length)
		return
	}
	correlationID := readInt64LE(buf, offset)
	streamID := readInt32LE(buf, offset+8)
	channelLen := int(readInt32LE(buf, offset+12))
	channel := ""
	if channelLen > 0 && length >= 16+channelLen {
		channel = readString(buf, offset+16, channelLen)
	}

	// Allocate log buffer backing store.
	bufSize := logbuffer.NumPartitions*c.termLength + logbuffer.LogMetaDataLength
	backing := make([]byte, bufSize)
	lb, err := logbuffer.New(backing, c.termLength)
	if err != nil {
		slog.Error("conductor: failed to create log buffer", "error", err)
		c.broadcastError(correlationID, err.Error())
		return
	}

	var sessionIDBytes [4]byte
	if _, err := cryptorand.Read(sessionIDBytes[:]); err != nil {
		slog.Error("conductor: failed to generate session ID", "error", err)
		c.broadcastError(correlationID, "internal error: session ID generation failed")
		return
	}
	sessionID := int32(binary.LittleEndian.Uint32(sessionIDBytes[:])) // #nosec G115 -- crypto/rand bytes reinterpreted as int32; bit-pattern conversion is intentional for session ID
	pub := &PublicationState{
		PublicationID: correlationID,
		SessionID:     sessionID,
		StreamID:      streamID,
		Channel:       channel,
		LogBuf:        lb,
	}

	c.mu.Lock()
	c.publications[correlationID] = pub
	c.publishPubSnapshot()
	c.mu.Unlock()

	slog.Info("conductor: publication added",
		"publication_id", correlationID,
		"session_id", sessionID,
		"stream_id", streamID,
		"channel", channel,
	)

	c.broadcastPublicationReady(correlationID, sessionID, streamID)
}

func (c *Conductor) handleRemovePublication(buf *atomic.AtomicBuffer, offset, length int) {
	if length < 8 {
		slog.Warn("conductor: CmdRemovePublication payload too short", "length", length)
		return
	}
	pubID := readInt64LE(buf, offset)

	c.mu.Lock()
	_, ok := c.publications[pubID]
	if ok {
		delete(c.publications, pubID)
		c.publishPubSnapshot()
	}
	c.mu.Unlock()

	if ok {
		slog.Info("conductor: publication removed", "publication_id", pubID)
	} else {
		slog.Warn("conductor: CmdRemovePublication unknown id", "publication_id", pubID)
	}
}

func (c *Conductor) handleAddSubscription(buf *atomic.AtomicBuffer, offset, length int) {
	if length < 16 {
		slog.Warn("conductor: CmdAddSubscription payload too short", "length", length)
		return
	}
	correlationID := readInt64LE(buf, offset)
	streamID := readInt32LE(buf, offset+8)
	channelLen := int(readInt32LE(buf, offset+12))
	channel := ""
	if channelLen > 0 && length >= 16+channelLen {
		channel = readString(buf, offset+16, channelLen)
	}

	sub := &SubscriptionState{
		SubscriptionID: correlationID,
		StreamID:       streamID,
		Channel:        channel,
		Images:         make([]*ImageState, 0),
	}

	c.mu.Lock()
	c.subscriptions[correlationID] = sub
	c.publishSubSnapshot()
	c.mu.Unlock()

	slog.Info("conductor: subscription added",
		"subscription_id", correlationID,
		"stream_id", streamID,
		"channel", channel,
	)

	c.broadcastSubscriptionReady(correlationID, streamID)
}

func (c *Conductor) handleRemoveSubscription(buf *atomic.AtomicBuffer, offset, length int) {
	if length < 8 {
		slog.Warn("conductor: CmdRemoveSubscription payload too short", "length", length)
		return
	}
	subID := readInt64LE(buf, offset)

	c.mu.Lock()
	_, ok := c.subscriptions[subID]
	if ok {
		delete(c.subscriptions, subID)
		c.publishSubSnapshot()
	}
	c.mu.Unlock()

	if ok {
		slog.Info("conductor: subscription removed", "subscription_id", subID)
	} else {
		slog.Warn("conductor: CmdRemoveSubscription unknown id", "subscription_id", subID)
	}
}

func (c *Conductor) handleClientKeepalive(buf *atomic.AtomicBuffer, offset, length int) {
	if length < 8 {
		return
	}
	clientID := readInt64LE(buf, offset)
	c.mu.Lock()
	c.clientAlive[clientID] = time.Now()
	c.mu.Unlock()
	slog.Debug("conductor: client keepalive", "client_id", clientID)
}

// --- Broadcast helpers ---

// broadcastPublicationReady writes RspPublicationReady.
// Payload: correlationID(8) + sessionID(4) + streamID(4) = 16 bytes.
func (c *Conductor) broadcastPublicationReady(correlationID int64, sessionID, streamID int32) {
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:], uint64(correlationID)) // #nosec G115 -- protocol wire format: int64 correlationID to uint64 binary encoding
	binary.LittleEndian.PutUint32(payload[8:], uint32(sessionID))     // #nosec G115 -- protocol wire format: int32 sessionID to uint32 binary encoding
	binary.LittleEndian.PutUint32(payload[12:], uint32(streamID))     // #nosec G115 -- protocol wire format: int32 streamID to uint32 binary encoding
	if err := c.fromDriverTx.Transmit(RspPublicationReady, payload); err != nil {
		slog.Error("conductor: failed to broadcast publication ready", "error", err)
	}
}

// broadcastSubscriptionReady writes RspSubscriptionReady.
// Payload: correlationID(8) + streamID(4) = 12 bytes.
func (c *Conductor) broadcastSubscriptionReady(correlationID int64, streamID int32) {
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint64(payload[0:], uint64(correlationID)) // #nosec G115 -- protocol wire format: int64 correlationID to uint64 binary encoding
	binary.LittleEndian.PutUint32(payload[8:], uint32(streamID))      // #nosec G115 -- protocol wire format: int32 streamID to uint32 binary encoding
	if err := c.fromDriverTx.Transmit(RspSubscriptionReady, payload); err != nil {
		slog.Error("conductor: failed to broadcast subscription ready", "error", err)
	}
}

// broadcastError writes RspError.
func (c *Conductor) broadcastError(correlationID int64, msg string) {
	msgBytes := []byte(msg)
	payload := make([]byte, 8+len(msgBytes))
	binary.LittleEndian.PutUint64(payload[0:], uint64(correlationID)) // #nosec G115 -- protocol wire format: int64 correlationID to uint64 binary encoding
	copy(payload[8:], msgBytes)
	if err := c.fromDriverTx.Transmit(RspError, payload); err != nil {
		slog.Error("conductor: failed to broadcast error", "error", err)
	}
}
