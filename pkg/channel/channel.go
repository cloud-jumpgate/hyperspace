// Package channel parses and represents Hyperspace channel URIs.
//
// URI format:
//
//	hs:<transport>[?<key>=<value>(|<key>=<value>)*]
//
// Examples:
//
//	hs:quic?endpoint=10.0.0.5:7777|pool=4|cc=bbrv3
//	hs:ipc
//	hs:mdc?control=10.0.0.5:7778|destinations=dynamic
package channel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Transport identifies the underlying transport mechanism.
type Transport int

const (
	TransportQUIC Transport = iota
	TransportIPC
	TransportMDC
)

// String returns the transport name as it appears in a channel URI.
func (t Transport) String() string {
	switch t {
	case TransportQUIC:
		return "quic"
	case TransportIPC:
		return "ipc"
	case TransportMDC:
		return "mdc"
	default:
		return "unknown"
	}
}

// Channel represents a parsed Hyperspace channel URI.
type Channel struct {
	Transport Transport
	// Endpoint is the primary host:port for QUIC and MDC transports.
	// It is empty for IPC.
	Endpoint string
	// Params holds all key=value parameters from the URI query section.
	Params map[string]string
}

const scheme = "hs:"

// Parse parses a Hyperspace channel URI string and returns a Channel.
// Returns an error if the URI scheme is wrong, the transport is unknown, or
// a parameter value is structurally invalid.
func Parse(uri string) (*Channel, error) {
	if !strings.HasPrefix(uri, scheme) {
		return nil, fmt.Errorf("channel: URI %q does not start with %q", uri, scheme)
	}
	rest := uri[len(scheme):]

	var transportStr, query string
	if idx := strings.IndexByte(rest, '?'); idx >= 0 {
		transportStr = rest[:idx]
		query = rest[idx+1:]
	} else {
		transportStr = rest
	}

	var transport Transport
	switch transportStr {
	case "quic":
		transport = TransportQUIC
	case "ipc":
		transport = TransportIPC
	case "mdc":
		transport = TransportMDC
	default:
		return nil, fmt.Errorf("channel: unknown transport %q", transportStr)
	}

	params := map[string]string{}
	if query != "" {
		pairs := strings.Split(query, "|")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			eqIdx := strings.IndexByte(pair, '=')
			if eqIdx < 0 {
				return nil, fmt.Errorf("channel: invalid parameter %q (no '=')", pair)
			}
			key := strings.TrimSpace(pair[:eqIdx])
			val := strings.TrimSpace(pair[eqIdx+1:])
			if key == "" {
				return nil, errors.New("channel: empty parameter key")
			}
			params[key] = val
		}
	}

	// Validate pool size eagerly so callers get an error at parse time.
	if poolStr, ok := params["pool"]; ok && poolStr != "auto" {
		n, err := strconv.Atoi(poolStr)
		if err != nil || n < 1 || n > 16 {
			return nil, fmt.Errorf("channel: pool=%q is invalid (must be 1–16 or \"auto\")", poolStr)
		}
	}

	// Determine endpoint.
	endpoint := params["endpoint"]
	if endpoint == "" {
		endpoint = params["control"]
	}

	return &Channel{
		Transport: transport,
		Endpoint:  endpoint,
		Params:    params,
	}, nil
}

// String reconstructs the canonical URI from the Channel.
func (c *Channel) String() string {
	var sb strings.Builder
	sb.WriteString(scheme)
	sb.WriteString(c.Transport.String())

	if len(c.Params) == 0 {
		return sb.String()
	}

	sb.WriteByte('?')
	// Build a stable output: endpoint/control first, then remaining keys in
	// insertion order is not preserved in maps; for canonical form we sort by
	// well-known keys first then remaining alphabetically.
	written := 0
	writeParam := func(k, v string) {
		if written > 0 {
			sb.WriteByte('|')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(v)
		written++
	}

	// Ordered priority keys.
	priority := []string{"endpoint", "control", "destinations", "pool", "cc", "mtu",
		"probe-interval", "identity", "reliable"}
	seen := map[string]bool{}
	for _, k := range priority {
		if v, ok := c.Params[k]; ok {
			writeParam(k, v)
			seen[k] = true
		}
	}
	// Remaining keys in sorted order for determinism.
	remaining := make([]string, 0, len(c.Params))
	for k := range c.Params {
		if !seen[k] {
			remaining = append(remaining, k)
		}
	}
	// Simple insertion sort for small maps.
	for i := 1; i < len(remaining); i++ {
		for j := i; j > 0 && remaining[j] < remaining[j-1]; j-- {
			remaining[j], remaining[j-1] = remaining[j-1], remaining[j]
		}
	}
	for _, k := range remaining {
		writeParam(k, c.Params[k])
	}

	return sb.String()
}

// PoolSize returns the configured pool size and whether it is set to "auto".
//
//   - (n, false) — explicit pool size 1–16
//   - (0, true)  — "auto"
//   - (4, false) — default (key absent)
func (c *Channel) PoolSize() (int, bool) {
	v, ok := c.Params["pool"]
	if !ok {
		return 4, false
	}
	if v == "auto" {
		return 0, true
	}
	n, _ := strconv.Atoi(v)
	return n, false
}

// CCName returns the congestion control algorithm name.
// Recognised values: "cubic", "bbr", "bbrv3", "drl". Default: "bbrv3".
func (c *Channel) CCName() string {
	if v, ok := c.Params["cc"]; ok && v != "" {
		return v
	}
	return "bbrv3"
}

// IsReliable returns true if reliable delivery is requested (default true).
// Set reliable=false to opt into unreliable delivery.
func (c *Channel) IsReliable() bool {
	v, ok := c.Params["reliable"]
	if !ok {
		return true
	}
	return strings.EqualFold(v, "true")
}

// MTU returns the configured MTU in bytes, or 0 if not set.
func (c *Channel) MTU() int {
	v, ok := c.Params["mtu"]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ProbeInterval returns the configured probe interval, or 0 if not set.
// The value must be parseable by time.ParseDuration.
func (c *Channel) ProbeInterval() time.Duration {
	v, ok := c.Params["probe-interval"]
	if !ok {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}

// Identity returns the identity= URI parameter, or empty string if absent.
func (c *Channel) Identity() string {
	return c.Params["identity"]
}
