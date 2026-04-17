package static_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-jumpgate/hyperspace/pkg/discovery/static"
)

func TestStaticProvider_Resolve_HappyPath(t *testing.T) {
	peers := map[string][]string{
		"service-a": {"10.0.0.1:4000", "10.0.0.2:4000"},
		"service-b": {"192.168.1.1:5000"},
	}
	p := static.NewStatic(peers)

	got, err := p.Resolve(context.Background(), "service-a")
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1:4000", "10.0.0.2:4000"}, got)

	got2, err := p.Resolve(context.Background(), "service-b")
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.1:5000"}, got2)
}

func TestStaticProvider_Resolve_UnknownName(t *testing.T) {
	p := static.NewStatic(map[string][]string{
		"service-a": {"10.0.0.1:4000"},
	})
	_, err := p.Resolve(context.Background(), "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestStaticProvider_Resolve_EmptyEndpoints(t *testing.T) {
	p := static.NewStatic(map[string][]string{
		"service-empty": {},
	})
	_, err := p.Resolve(context.Background(), "service-empty")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestStaticProvider_MutationIsolation(t *testing.T) {
	// Caller mutating their map after construction must not affect provider.
	peers := map[string][]string{
		"service-a": {"10.0.0.1:4000"},
	}
	p := static.NewStatic(peers)
	peers["service-a"] = []string{"evil:9999"}
	peers["service-b"] = []string{"evil:9999"}

	got, err := p.Resolve(context.Background(), "service-a")
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1:4000"}, got)

	_, err = p.Resolve(context.Background(), "service-b")
	require.Error(t, err)
}

func TestStaticProvider_ReturnedSliceIsolation(t *testing.T) {
	// Mutating the returned slice must not affect the provider's internal state.
	p := static.NewStatic(map[string][]string{
		"service-a": {"10.0.0.1:4000"},
	})
	got, err := p.Resolve(context.Background(), "service-a")
	require.NoError(t, err)
	got[0] = "mutated:9999"

	got2, err := p.Resolve(context.Background(), "service-a")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1:4000", got2[0])
}

func TestStaticProvider_Close(t *testing.T) {
	p := static.NewStatic(map[string][]string{})
	assert.NoError(t, p.Close())
}

func TestStaticProvider_ContextCancelled(t *testing.T) {
	// StaticProvider ignores context but must not panic on a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := static.NewStatic(map[string][]string{
		"svc": {"10.0.0.1:4000"},
	})
	got, err := p.Resolve(ctx, "svc")
	require.NoError(t, err) // static does not block; context not checked
	assert.Equal(t, []string{"10.0.0.1:4000"}, got)
}
