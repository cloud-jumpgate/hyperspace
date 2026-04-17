package cloudmap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-jumpgate/hyperspace/pkg/discovery/cloudmap"
)

// mockDiscoverClient implements the discoverClient interface for testing.
type mockDiscoverClient struct {
	out *servicediscovery.DiscoverInstancesOutput
	err error
}

func (m *mockDiscoverClient) DiscoverInstances(
	_ context.Context,
	_ *servicediscovery.DiscoverInstancesInput,
	_ ...func(*servicediscovery.Options),
) (*servicediscovery.DiscoverInstancesOutput, error) {
	return m.out, m.err
}

func TestCloudMapProvider_Resolve_HappyPath(t *testing.T) {
	mock := &mockDiscoverClient{
		out: &servicediscovery.DiscoverInstancesOutput{
			Instances: []types.HttpInstanceSummary{
				{
					InstanceId: aws.String("i-001"),
					Attributes: map[string]string{
						"AWS_INSTANCE_IPV4": "10.0.0.1",
						"AWS_INSTANCE_PORT": "4000",
					},
				},
				{
					InstanceId: aws.String("i-002"),
					Attributes: map[string]string{
						"AWS_INSTANCE_IPV4": "10.0.0.2",
						"AWS_INSTANCE_PORT": "4000",
					},
				},
			},
		},
	}

	p := cloudmap.NewWithClient(mock, "hyperspace.local")
	got, err := p.Resolve(context.Background(), "sender")
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1:4000", "10.0.0.2:4000"}, got)
}

func TestCloudMapProvider_Resolve_MissingAttributes(t *testing.T) {
	// Instances without required attributes are skipped; if ALL are missing → error.
	mock := &mockDiscoverClient{
		out: &servicediscovery.DiscoverInstancesOutput{
			Instances: []types.HttpInstanceSummary{
				{
					InstanceId: aws.String("i-001"),
					Attributes: map[string]string{
						// Missing AWS_INSTANCE_PORT
						"AWS_INSTANCE_IPV4": "10.0.0.1",
					},
				},
			},
		},
	}

	p := cloudmap.NewWithClient(mock, "hyperspace.local")
	_, err := p.Resolve(context.Background(), "sender")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy instances")
}

func TestCloudMapProvider_Resolve_PartialAttributes(t *testing.T) {
	// One instance with missing attrs, one complete — only the complete one returned.
	mock := &mockDiscoverClient{
		out: &servicediscovery.DiscoverInstancesOutput{
			Instances: []types.HttpInstanceSummary{
				{
					InstanceId: aws.String("i-bad"),
					Attributes: map[string]string{"AWS_INSTANCE_IPV4": "10.0.0.1"},
				},
				{
					InstanceId: aws.String("i-good"),
					Attributes: map[string]string{
						"AWS_INSTANCE_IPV4": "10.0.0.2",
						"AWS_INSTANCE_PORT": "4001",
					},
				},
			},
		},
	}

	p := cloudmap.NewWithClient(mock, "hyperspace.local")
	got, err := p.Resolve(context.Background(), "sender")
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.2:4001"}, got)
}

func TestCloudMapProvider_Resolve_EmptyInstances(t *testing.T) {
	mock := &mockDiscoverClient{
		out: &servicediscovery.DiscoverInstancesOutput{Instances: []types.HttpInstanceSummary{}},
	}

	p := cloudmap.NewWithClient(mock, "hyperspace.local")
	_, err := p.Resolve(context.Background(), "sender")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy instances")
}

func TestCloudMapProvider_Resolve_APIError(t *testing.T) {
	mock := &mockDiscoverClient{
		err: errors.New("network timeout"),
	}

	p := cloudmap.NewWithClient(mock, "hyperspace.local")
	_, err := p.Resolve(context.Background(), "sender")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network timeout")
}

func TestCloudMapProvider_Resolve_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockDiscoverClient{
		err: context.Canceled,
	}

	p := cloudmap.NewWithClient(mock, "hyperspace.local")
	_, err := p.Resolve(ctx, "sender")
	require.Error(t, err)
}

func TestCloudMapProvider_Close(t *testing.T) {
	p := cloudmap.NewWithClient(&mockDiscoverClient{}, "hyperspace.local")
	assert.NoError(t, p.Close())
}

func TestCloudMapProvider_New_Compiles(t *testing.T) {
	// Verify New() compiles with a zero aws.Config (no real calls made).
	cfg := aws.Config{Region: "us-east-1"}
	p := cloudmap.New(cfg, "hyperspace.local")
	assert.NotNil(t, p)
}
