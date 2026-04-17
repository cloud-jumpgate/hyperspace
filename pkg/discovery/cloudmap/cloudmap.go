// Package cloudmap provides a CloudMapProvider that resolves endpoints from
// AWS Cloud Map (Service Discovery). It calls DiscoverInstances with the
// configured namespace and returns "ipv4:port" for each healthy instance.
package cloudmap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery"
	"github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"
)

// discoverClient is the subset of the servicediscovery client that
// CloudMapProvider requires. Defined as an interface so tests can inject mocks.
type discoverClient interface {
	DiscoverInstances(ctx context.Context, params *servicediscovery.DiscoverInstancesInput, optFns ...func(*servicediscovery.Options)) (*servicediscovery.DiscoverInstancesOutput, error)
}

// CloudMapProvider resolves endpoints from AWS Cloud Map.
type CloudMapProvider struct {
	client    discoverClient
	namespace string
}

// New creates a CloudMapProvider using the supplied aws.Config and namespace.
// The namespace is the AWS Cloud Map namespace name (e.g. "hyperspace.local").
func New(cfg aws.Config, namespace string) *CloudMapProvider {
	return &CloudMapProvider{
		client:    servicediscovery.NewFromConfig(cfg),
		namespace: namespace,
	}
}

// newWithClient creates a CloudMapProvider with an injected client (for testing).
func newWithClient(client discoverClient, namespace string) *CloudMapProvider {
	return &CloudMapProvider{client: client, namespace: namespace}
}

// Resolve calls DiscoverInstances for the given service name within the
// configured namespace. Returns "ipv4:port" for each healthy instance that
// exposes both AWS_INSTANCE_IPV4 and AWS_INSTANCE_PORT attributes.
func (p *CloudMapProvider) Resolve(ctx context.Context, name string) ([]string, error) {
	input := &servicediscovery.DiscoverInstancesInput{
		NamespaceName: aws.String(p.namespace),
		ServiceName:   aws.String(name),
		HealthStatus:  types.HealthStatusFilterHealthy,
	}

	out, err := p.client.DiscoverInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("discovery/cloudmap: DiscoverInstances(%s/%s): %w", p.namespace, name, err)
	}

	endpoints := make([]string, 0, len(out.Instances))
	for _, inst := range out.Instances {
		ip, hasIP := inst.Attributes["AWS_INSTANCE_IPV4"]
		port, hasPort := inst.Attributes["AWS_INSTANCE_PORT"]
		if !hasIP || !hasPort {
			slog.Warn("discovery/cloudmap: instance missing required attributes",
				"instance_id", aws.ToString(inst.InstanceId),
				"namespace", p.namespace,
				"service", name,
			)
			continue
		}
		endpoints = append(endpoints, ip+":"+port)
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("discovery/cloudmap: no healthy instances for %s/%s", p.namespace, name)
	}

	slog.Debug("discovery/cloudmap: resolved",
		"namespace", p.namespace,
		"service", name,
		"count", len(endpoints),
	)
	return endpoints, nil
}

// Close is a no-op; the underlying HTTP client manages its own lifecycle.
func (p *CloudMapProvider) Close() error { return nil }
