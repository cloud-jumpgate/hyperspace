package ssm

// NewWithClient exposes newWithClient for testing.
func NewWithClient(client ssmGetClient, env, role string) *SSMLoader {
	return newWithClient(client, env, role)
}
