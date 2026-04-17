// Package otel_test provides mock OTel instrumentation for testing the callback path.
package otel_test

import (
	"context"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
	"go.opentelemetry.io/otel/metric/noop"
)

// callbackCapture is a mock MeterProvider that invokes registered callbacks
// synchronously when Flush is called, enabling coverage of the observation path.
type callbackCapture struct {
	embedded.MeterProvider
	meter *captureMeter
}

func newCallbackCapture() *callbackCapture {
	return &callbackCapture{meter: &captureMeter{}}
}

func (c *callbackCapture) Meter(_ string, _ ...metric.MeterOption) metric.Meter {
	return c.meter
}

// Flush calls all registered callbacks with a recording observer.
func (c *callbackCapture) Flush(ctx context.Context) {
	for _, cb := range c.meter.callbacks {
		obs := &recordingObserver{}
		_ = cb(ctx, obs)
	}
}

// Observations returns all recorded observations from Flush calls.
func (c *callbackCapture) Observations() []observation {
	return c.meter.observations
}

type observation struct {
	value int64
}

// captureMeter is a mock metric.Meter that stores callbacks and replays them.
type captureMeter struct {
	embedded.Meter
	callbacks    []metric.Callback
	observations []observation
}

func (m *captureMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	// Delegate to noop for the actual instrument.
	nm := noop.NewMeterProvider().Meter("")
	return nm.Int64ObservableGauge(name, opts...)
}

func (m *captureMeter) RegisterCallback(f metric.Callback, instruments ...metric.Observable) (metric.Registration, error) {
	m.callbacks = append(m.callbacks, f)
	return &captureRegistration{meter: m, f: f}, nil
}

// Embed all other Meter methods via noop to satisfy the interface.
func (m *captureMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return noop.NewMeterProvider().Meter("").Int64Counter(name, opts...)
}
func (m *captureMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	return noop.NewMeterProvider().Meter("").Int64UpDownCounter(name, opts...)
}
func (m *captureMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	return noop.NewMeterProvider().Meter("").Int64Histogram(name, opts...)
}
func (m *captureMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	return noop.NewMeterProvider().Meter("").Int64Gauge(name, opts...)
}
func (m *captureMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	return noop.NewMeterProvider().Meter("").Float64Counter(name, opts...)
}
func (m *captureMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	return noop.NewMeterProvider().Meter("").Float64UpDownCounter(name, opts...)
}
func (m *captureMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return noop.NewMeterProvider().Meter("").Float64Histogram(name, opts...)
}
func (m *captureMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return noop.NewMeterProvider().Meter("").Float64Gauge(name, opts...)
}
func (m *captureMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	return noop.NewMeterProvider().Meter("").Float64ObservableCounter(name, opts...)
}
func (m *captureMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	return noop.NewMeterProvider().Meter("").Float64ObservableUpDownCounter(name, opts...)
}
func (m *captureMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	return noop.NewMeterProvider().Meter("").Float64ObservableGauge(name, opts...)
}
func (m *captureMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	return noop.NewMeterProvider().Meter("").Int64ObservableCounter(name, opts...)
}
func (m *captureMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	return noop.NewMeterProvider().Meter("").Int64ObservableUpDownCounter(name, opts...)
}

// captureRegistration tracks one callback registration so Unregister works.
type captureRegistration struct {
	embedded.Registration
	meter *captureMeter
	f     metric.Callback
}

func (r *captureRegistration) Unregister() error {
	updated := r.meter.callbacks[:0]
	for _, cb := range r.meter.callbacks {
		if &cb != &r.f {
			updated = append(updated, cb)
		}
	}
	r.meter.callbacks = updated
	return nil
}

// recordingObserver records ObserveInt64 calls.
type recordingObserver struct {
	embedded.Observer
	observations []observation
}

func (o *recordingObserver) ObserveInt64(_ metric.Int64Observable, v int64, _ ...metric.ObserveOption) {
	o.observations = append(o.observations, observation{value: v})
}

func (o *recordingObserver) ObserveFloat64(_ metric.Float64Observable, _ float64, _ ...metric.ObserveOption) {
}
