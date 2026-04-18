package identity

import "time"

// NewWithClient exposes newWithClient for testing.
func NewWithClient(client workloadClient) *SPIFFESource {
	return newWithClient(client)
}

// SetWatchInterval overrides the watch interval for testing.
func (s *SPIFFESource) SetWatchInterval(d time.Duration) {
	s.watchIntervalOverride = d
}
