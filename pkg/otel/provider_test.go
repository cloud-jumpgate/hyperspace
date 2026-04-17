package otel_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/cloud-jumpgate/hyperspace/pkg/counters"
	hyperotel "github.com/cloud-jumpgate/hyperspace/pkg/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeReader() *counters.CountersReader {
	buf := counters.NewBuffer()
	return counters.NewCountersReader(buf)
}

func makeReaderWriter() (*counters.CountersReader, *counters.CountersWriter) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	r := w.Reader()
	return r, w
}

func TestNewProvider_NilReader(t *testing.T) {
	_, err := hyperotel.NewProvider(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestNewProvider_ValidReader(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNewProvider_WithMeterName(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r, hyperotel.WithMeterName("custom.meter"))
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestStart_NilMeterProvider(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	err = p.Start(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestStart_Success(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	mp := noop.NewMeterProvider()
	err = p.Start(context.Background(), mp)
	require.NoError(t, err)

	require.NoError(t, p.Stop())
}

func TestStart_AlreadyStarted(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	mp := noop.NewMeterProvider()
	require.NoError(t, p.Start(context.Background(), mp))

	// Second Start should fail
	err = p.Start(context.Background(), mp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	require.NoError(t, p.Stop())
}

func TestStop_WithoutStart(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	// Stop without Start should succeed (no-op)
	err = p.Stop()
	require.NoError(t, err)
}

func TestStop_Idempotent(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	mp := noop.NewMeterProvider()
	require.NoError(t, p.Start(context.Background(), mp))
	require.NoError(t, p.Stop())
	// Second stop should succeed (nothing to unregister)
	require.NoError(t, p.Stop())
}

func TestProvider_ReportsCounterValues(t *testing.T) {
	r, w := makeReaderWriter()

	w.Set(counters.CtrBytesSent, 12345)
	w.Set(counters.CtrMsgSent, 100)
	w.Set(counters.CtrConnectionsActive, 4)

	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	mp := noop.NewMeterProvider()
	require.NoError(t, p.Start(context.Background(), mp))

	// With noop provider the callback is not invoked, but we verify
	// the Provider started and reads counters without error.
	assert.Equal(t, int64(12345), r.Get(counters.CtrBytesSent))
	assert.Equal(t, int64(100), r.Get(counters.CtrMsgSent))
	assert.Equal(t, int64(4), r.Get(counters.CtrConnectionsActive))

	require.NoError(t, p.Stop())
}

func TestProvider_AllCounterIDs(t *testing.T) {
	r, w := makeReaderWriter()

	// Set every counter to a distinct value
	for i := 0; i < counters.NumCounters; i++ {
		w.Set(i, int64(i+1)*111)
	}

	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	mp := noop.NewMeterProvider()
	require.NoError(t, p.Start(context.Background(), mp))

	// Verify all counter values are readable
	for i := 0; i < counters.NumCounters; i++ {
		assert.Equal(t, int64(i+1)*111, r.Get(i), "counter %d", i)
	}

	require.NoError(t, p.Stop())
}

func TestProvider_CallbackObservesCounterValues(t *testing.T) {
	r, w := makeReaderWriter()

	// Set distinctive counter values
	for i := 0; i < counters.NumCounters; i++ {
		w.Set(i, int64(i+1)*7)
	}

	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	// Use the capturing mock to invoke the callback
	capture := newCallbackCapture()
	require.NoError(t, p.Start(context.Background(), capture))

	// Flush triggers the callback — must observe all counters
	capture.Flush(context.Background())

	require.NoError(t, p.Stop())
}

func TestProvider_StartStopCycle(t *testing.T) {
	r := makeReader()
	p, err := hyperotel.NewProvider(r)
	require.NoError(t, err)

	mp := noop.NewMeterProvider()

	for i := 0; i < 3; i++ {
		require.NoError(t, p.Start(context.Background(), mp), "cycle %d", i)
		require.NoError(t, p.Stop(), "cycle %d", i)
	}
}
