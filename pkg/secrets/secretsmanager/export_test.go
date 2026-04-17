package secretsmanager

// NewWithClient exposes newWithClient for testing.
func NewWithClient(client smGetClient, secretID string) *SecretsManagerProvider {
	return newWithClient(client, secretID)
}
