package cloudmap

// NewWithClient exposes newWithClient for testing.
func NewWithClient(client discoverClient, namespace string) *CloudMapProvider {
	return newWithClient(client, namespace)
}
