// Package pool manages a set of QUIC connections to a single remote peer.
package pool

import (
	"fmt"
	"sync"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)

// Pool manages N concurrent QUIC connections to one remote peer.
// All methods are safe for concurrent use.
type Pool struct {
	mu      sync.RWMutex
	conns   []quictr.Connection // live connections
	peer    string              // remote addr
	minSize int                 // minimum connections (default 2)
	maxSize int                 // maximum connections (default 8)
}

// New creates a Pool for the given peer with min/max connection bounds.
// minSize must be >= 1; maxSize must be >= minSize.
func New(peer string, minSize, maxSize int) *Pool {
	if minSize < 1 {
		minSize = 1
	}
	if maxSize < minSize {
		maxSize = minSize
	}
	return &Pool{
		conns:   make([]quictr.Connection, 0, maxSize),
		peer:    peer,
		minSize: minSize,
		maxSize: maxSize,
	}
}

// Add adds a connection to the pool.
// If the pool is already at maxSize the connection is not added and an error is returned.
func (p *Pool) Add(c quictr.Connection) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.conns) >= p.maxSize {
		return fmt.Errorf("pool: at capacity (%d/%d)", len(p.conns), p.maxSize)
	}
	p.conns = append(p.conns, c)
	return nil
}

// Remove closes and removes a connection by ID.
// Returns an error if no connection with the given ID exists.
func (p *Pool) Remove(id uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, c := range p.conns {
		if c.ID() == id {
			// Close connection (ignore error — may already be closed).
			_ = c.Close()
			// Remove from slice maintaining order.
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("pool: connection %d not found", id)
}

// Connections returns a snapshot of live (non-closed) connections.
// Closed connections are pruned from the pool during this call.
// Callers must not modify the returned slice.
func (p *Pool) Connections() []quictr.Connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneClosedLocked()
	// Return a copy so callers cannot modify the pool's internal slice.
	snap := make([]quictr.Connection, len(p.conns))
	copy(snap, p.conns)
	return snap
}

// pruneClosedLocked removes closed connections. Must be called with p.mu held.
func (p *Pool) pruneClosedLocked() {
	live := p.conns[:0]
	for _, c := range p.conns {
		if !c.IsClosed() {
			live = append(live, c)
		}
	}
	// Zero out vacated slots to avoid memory leaks.
	for i := len(live); i < len(p.conns); i++ {
		p.conns[i] = nil
	}
	p.conns = live
}

// Size returns the current live (non-closed) connection count.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneClosedLocked()
	return len(p.conns)
}

// Peer returns the remote address string.
func (p *Pool) Peer() string {
	return p.peer
}

// IsFull returns true if Size() >= maxSize.
func (p *Pool) IsFull() bool {
	return p.Size() >= p.maxSize
}

// IsUnderMin returns true if Size() < minSize.
func (p *Pool) IsUnderMin() bool {
	return p.Size() < p.minSize
}
