package identity

// NewWithClient exposes newWithClient for testing.
func NewWithClient(client workloadClient) *SPIFFESource {
	return newWithClient(client)
}
