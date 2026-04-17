package cc

import (
	"fmt"
	"sort"
	"time"
)

// PacketNumber is a QUIC packet number.
type PacketNumber uint64

// CongestionControl is the interface all CC implementations satisfy.
// Callbacks are called by the QUIC stack; queries are called by the sender path.
type CongestionControl interface {
	// Event callbacks — called on the ack/loss path.
	OnPacketSent(t time.Time, pn PacketNumber, size int, inFlight int)
	OnPacketAcked(t time.Time, pn PacketNumber, size int, rttSample time.Duration, inFlight int)
	OnPacketLost(t time.Time, pn PacketNumber, size int, inFlight int)
	OnRTTUpdate(rttSample, sRTT, rttVar time.Duration)

	// State queries — called by sender path (must be fast, no alloc).
	CongestionWindow() int  // bytes
	PacingRate() int        // bytes per second; 0 = unlimited
	CanSend(inFlight int) bool

	// Lifecycle.
	Name() string
	Reset()
}

// Factory creates a new CongestionControl for a connection.
type Factory func(initialCwnd int, minRTT time.Duration) CongestionControl

// registry maps CC names to factories.
var registry = map[string]Factory{}

// Register registers a factory under name. Panics on duplicate.
func Register(name string, f Factory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("cc: duplicate registration for %q", name))
	}
	registry[name] = f
}

// New creates a CC by name. Returns error if not registered.
func New(name string, initialCwnd int, minRTT time.Duration) (CongestionControl, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("cc: unknown congestion control %q", name)
	}
	return f(initialCwnd, minRTT), nil
}

// Names returns all registered CC names sorted alphabetically.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
