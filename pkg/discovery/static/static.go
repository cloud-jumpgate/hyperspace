// Package static provides a StaticProvider that resolves peer endpoints from a
// fixed in-memory map. It requires no network calls and is suitable for
// development, testing, and static cluster configurations.
package static

import (
	"context"
	"fmt"
)

// StaticProvider resolves endpoints from a fixed map of name → []"host:port".
// It is safe for concurrent use.
//
//nolint:revive // stutter is intentional: static.StaticProvider is the established public API name
type StaticProvider struct {
	peers map[string][]string
}

// NewStatic creates a StaticProvider from the given peers map.
// The map is copied so that subsequent mutations to the caller's map have no effect.
func NewStatic(peers map[string][]string) *StaticProvider {
	cp := make(map[string][]string, len(peers))
	for k, v := range peers {
		endpoints := make([]string, len(v))
		copy(endpoints, v)
		cp[k] = endpoints
	}
	return &StaticProvider{peers: cp}
}

// Resolve returns the endpoints registered for name.
// Returns an error if name is not found.
func (p *StaticProvider) Resolve(_ context.Context, name string) ([]string, error) {
	endpoints, ok := p.peers[name]
	if !ok {
		return nil, fmt.Errorf("discovery/static: no endpoints for %q", name)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("discovery/static: empty endpoint list for %q", name)
	}
	result := make([]string, len(endpoints))
	copy(result, endpoints)
	return result, nil
}

// Close is a no-op; StaticProvider holds no external resources.
func (p *StaticProvider) Close() error { return nil }
