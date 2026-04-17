// Package otel provides a thin OpenTelemetry bridge that maps Hyperspace driver
// counters to OTel observable gauge instruments.
//
// Usage:
//
//	reader := counters.NewCountersReader(buf)
//	p, err := otel.NewProvider(reader)
//	if err != nil { ... }
//	if err := p.Start(ctx, meterProvider); err != nil { ... }
//	defer p.Stop()
package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/cloud-jumpgate/hyperspace/pkg/counters"
)

// instrumentDef defines the mapping from a Hyperspace counter ID to an OTel
// instrument name and description.
type instrumentDef struct {
	counterID   int
	name        string
	description string
}

// instruments defines all Hyperspace counter → OTel gauge mappings.
var instruments = []instrumentDef{
	{counters.CtrBytesSent, "hyperspace.bytes_sent", "Total bytes sent by the driver"},
	{counters.CtrBytesReceived, "hyperspace.bytes_received", "Total bytes received by the driver"},
	{counters.CtrMsgSent, "hyperspace.messages_sent", "Total messages sent"},
	{counters.CtrMsgReceived, "hyperspace.messages_received", "Total messages received"},
	{counters.CtrConnectionsActive, "hyperspace.connections_active", "Number of currently active connections"},
	{counters.CtrConnectionOpens, "hyperspace.connection_opens", "Total connection open events"},
	{counters.CtrConnectionCloses, "hyperspace.connection_closes", "Total connection close events"},
	{counters.CtrPingsSent, "hyperspace.pings_sent", "Total PING frames sent"},
	{counters.CtrPongsReceived, "hyperspace.pongs_received", "Total PONG frames received"},
	{counters.CtrLostFrames, "hyperspace.lost_frames", "Total lost frames detected"},
	{counters.CtrBackPressureEvents, "hyperspace.backpressure_events", "Total back-pressure events"},
	{counters.CtrRotationEvents, "hyperspace.rotation_events", "Total certificate rotation events"},
}

// option is a functional option for Provider.
type option func(*Provider)

// WithMeterName sets the meter name used to register instruments.
// Defaults to "hyperspace.driver".
func WithMeterName(name string) option {
	return func(p *Provider) {
		p.meterName = name
	}
}

// Provider registers Hyperspace counters as OTel observable gauges and reports
// their values during each OTel collection cycle.
type Provider struct {
	reader    *counters.CountersReader
	meterName string

	// registration is the OTel callback registration; non-nil after Start.
	registration metric.Registration
}

// NewProvider creates a Provider that reads from reader.
// Call Start to register the OTel instruments and begin reporting.
func NewProvider(reader *counters.CountersReader, opts ...option) (*Provider, error) {
	if reader == nil {
		return nil, fmt.Errorf("otel: counters reader must not be nil")
	}
	p := &Provider{
		reader:    reader,
		meterName: "hyperspace.driver",
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Start registers the OTel meter instruments and begins reporting counter values.
// mp must be a non-nil metric.MeterProvider.
// Start may only be called once; call Stop before calling Start again.
func (p *Provider) Start(ctx context.Context, mp metric.MeterProvider) error {
	if mp == nil {
		return fmt.Errorf("otel: MeterProvider must not be nil")
	}
	if p.registration != nil {
		return fmt.Errorf("otel: provider already started")
	}

	meter := mp.Meter(p.meterName)

	// Create all gauge instruments.
	gauges := make([]metric.Int64ObservableGauge, len(instruments))
	observables := make([]metric.Observable, len(instruments))
	for i, def := range instruments {
		g, err := meter.Int64ObservableGauge(
			def.name,
			metric.WithDescription(def.description),
		)
		if err != nil {
			return fmt.Errorf("otel: failed to create gauge %q: %w", def.name, err)
		}
		gauges[i] = g
		observables[i] = g
	}

	// Register a single callback that observes all gauges.
	reg, err := meter.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		for i, def := range instruments {
			obs.ObserveInt64(gauges[i], p.reader.Get(def.counterID))
		}
		return nil
	}, observables...)
	if err != nil {
		return fmt.Errorf("otel: failed to register callback: %w", err)
	}

	p.registration = reg
	return nil
}

// Stop unregisters the OTel callback and releases resources.
// It is safe to call Stop if Start has not been called.
func (p *Provider) Stop() error {
	if p.registration == nil {
		return nil
	}
	if err := p.registration.Unregister(); err != nil {
		return fmt.Errorf("otel: failed to unregister callback: %w", err)
	}
	p.registration = nil
	return nil
}
