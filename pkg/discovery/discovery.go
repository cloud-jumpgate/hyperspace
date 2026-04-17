// Package discovery defines the Provider interface for resolving peer endpoints by name.
// Implementations include a static in-memory map and an AWS Cloud Map resolver.
package discovery

import "context"

// Provider resolves peer endpoints by name.
type Provider interface {
	// Resolve returns a slice of "host:port" strings for the named service.
	// Returns an error if the service is unknown or the lookup fails.
	Resolve(ctx context.Context, name string) ([]string, error)

	// Close releases any resources held by the provider.
	Close() error
}
