// Package cc provides congestion control algorithms and adapters for Hyperspace.
//
// The CCAdapter bridges Hyperspace's CongestionControl interface with quic-go's
// internal congestion control. Since quic-go v0.59.0 does not expose a public
// CongestionControlAlgorithm interface, this adapter operates at the Hyperspace
// transport layer: the Sender consults cc.CongestionControl.CanSend() and
// PacingRate() before writing frames, providing application-level congestion
// control that works with quic-go's built-in CC (which handles QUIC-level CC).
//
// F-03 fix: CC algorithms in pkg/cc/ (CUBIC, BBR, BBRv3, DRL) were implemented
// but never wired into any connection. This adapter makes them active.
package cc

import (
	"fmt"
	"sync"
	"time"
)

// CCAdapter wraps a CongestionControl instance and provides thread-safe access
// for use by the Sender's hot path. The adapter is created per-connection and
// delegates all CC decisions to the underlying Hyperspace CC algorithm.
type CCAdapter struct {
	mu        sync.Mutex
	cc        CongestionControl
	ccName    string
	connected bool
}

// NewAdapter creates a CCAdapter for the given CC algorithm name.
// If ccName is empty or unknown, defaults to "cubic".
// initialCwnd: initial congestion window in bytes (default 10 * 1200 = 12000).
// minRTT: initial minimum RTT estimate.
func NewAdapter(ccName string, initialCwnd int, minRTT time.Duration) (*CCAdapter, error) {
	if ccName == "" {
		ccName = "cubic"
	}
	if initialCwnd <= 0 {
		initialCwnd = 10 * 1200 // 10 segments * MTU
	}
	if minRTT <= 0 {
		minRTT = 10 * time.Millisecond
	}

	cc, err := New(ccName, initialCwnd, minRTT)
	if err != nil {
		// Fall back to cubic if the requested algorithm is unknown.
		cc, err = New("cubic", initialCwnd, minRTT)
		if err != nil {
			return nil, fmt.Errorf("cc: failed to create fallback cubic: %w", err)
		}
		ccName = "cubic"
	}

	return &CCAdapter{
		cc:        cc,
		ccName:    ccName,
		connected: true,
	}, nil
}

// Name returns the name of the underlying CC algorithm.
func (a *CCAdapter) Name() string {
	return a.ccName
}

// CC returns the underlying CongestionControl instance.
func (a *CCAdapter) CC() CongestionControl {
	return a.cc
}

// OnPacketSent notifies the CC that a packet was sent.
func (a *CCAdapter) OnPacketSent(t time.Time, pn PacketNumber, size int, inFlight int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cc.OnPacketSent(t, pn, size, inFlight)
}

// OnPacketAcked notifies the CC that a packet was acknowledged.
func (a *CCAdapter) OnPacketAcked(t time.Time, pn PacketNumber, size int, rttSample time.Duration, inFlight int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cc.OnPacketAcked(t, pn, size, rttSample, inFlight)
}

// OnPacketLost notifies the CC that a packet was lost.
func (a *CCAdapter) OnPacketLost(t time.Time, pn PacketNumber, size int, inFlight int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cc.OnPacketLost(t, pn, size, inFlight)
}

// OnRTTUpdate notifies the CC of an RTT measurement.
func (a *CCAdapter) OnRTTUpdate(rttSample, sRTT, rttVar time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cc.OnRTTUpdate(rttSample, sRTT, rttVar)
}

// CanSend returns true if the CC allows sending given the current in-flight bytes.
func (a *CCAdapter) CanSend(inFlight int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cc.CanSend(inFlight)
}

// CongestionWindow returns the current congestion window in bytes.
func (a *CCAdapter) CongestionWindow() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cc.CongestionWindow()
}

// PacingRate returns the current pacing rate in bytes/second. 0 = unlimited.
func (a *CCAdapter) PacingRate() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cc.PacingRate()
}

// Reset resets the CC state.
func (a *CCAdapter) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cc.Reset()
}

// adapterRegistry holds per-connection CC adapters keyed by connection ID.
var (
	adaptersMu sync.Mutex
	adapters   = make(map[uint64]*CCAdapter)
)

// RegisterAdapter associates a CCAdapter with a connection ID.
// Called at connection creation time to wire CC into the connection.
func RegisterAdapter(connID uint64, adapter *CCAdapter) {
	adaptersMu.Lock()
	adapters[connID] = adapter
	adaptersMu.Unlock()
}

// UnregisterAdapter removes the CCAdapter for a connection ID.
// Called when a connection is closed.
func UnregisterAdapter(connID uint64) {
	adaptersMu.Lock()
	delete(adapters, connID)
	adaptersMu.Unlock()
}

// GetAdapter returns the CCAdapter for a connection ID, or nil if not registered.
func GetAdapter(connID uint64) *CCAdapter {
	adaptersMu.Lock()
	a := adapters[connID]
	adaptersMu.Unlock()
	return a
}
