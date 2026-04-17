package channel

import (
	"strings"
	"testing"
	"time"
)

// ─── Parse happy paths ────────────────────────────────────────────────────────

func TestParse_QUICFull(t *testing.T) {
	ch, err := Parse("hs:quic?endpoint=10.0.0.5:7777|pool=4|cc=bbrv3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Transport != TransportQUIC {
		t.Errorf("Transport: got %v want QUIC", ch.Transport)
	}
	if ch.Endpoint != "10.0.0.5:7777" {
		t.Errorf("Endpoint: got %q want \"10.0.0.5:7777\"", ch.Endpoint)
	}
	if ch.Params["pool"] != "4" {
		t.Errorf("pool param: got %q want \"4\"", ch.Params["pool"])
	}
	if ch.Params["cc"] != "bbrv3" {
		t.Errorf("cc param: got %q want \"bbrv3\"", ch.Params["cc"])
	}
}

func TestParse_IPC(t *testing.T) {
	ch, err := Parse("hs:ipc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Transport != TransportIPC {
		t.Errorf("Transport: got %v want IPC", ch.Transport)
	}
	if ch.Endpoint != "" {
		t.Errorf("Endpoint: got %q want empty", ch.Endpoint)
	}
	if len(ch.Params) != 0 {
		t.Errorf("Params: got %v want empty", ch.Params)
	}
}

func TestParse_MDC(t *testing.T) {
	ch, err := Parse("hs:mdc?control=10.0.0.5:7778|destinations=dynamic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Transport != TransportMDC {
		t.Errorf("Transport: got %v want MDC", ch.Transport)
	}
	// control param becomes the Endpoint.
	if ch.Endpoint != "10.0.0.5:7778" {
		t.Errorf("Endpoint: got %q want \"10.0.0.5:7778\"", ch.Endpoint)
	}
	if ch.Params["destinations"] != "dynamic" {
		t.Errorf("destinations: got %q want \"dynamic\"", ch.Params["destinations"])
	}
}

func TestParse_QUICMinimal(t *testing.T) {
	ch, err := Parse("hs:quic?endpoint=127.0.0.1:9999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Transport != TransportQUIC {
		t.Errorf("Transport: got %v want QUIC", ch.Transport)
	}
	if ch.Endpoint != "127.0.0.1:9999" {
		t.Errorf("Endpoint: got %q", ch.Endpoint)
	}
}

func TestParse_AllParams(t *testing.T) {
	uri := "hs:quic?endpoint=1.2.3.4:5000|pool=8|cc=bbr|mtu=1400|probe-interval=500ms|identity=node-42|reliable=true"
	ch, err := Parse(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Params["mtu"] != "1400" {
		t.Errorf("mtu: got %q", ch.Params["mtu"])
	}
	if ch.Params["probe-interval"] != "500ms" {
		t.Errorf("probe-interval: got %q", ch.Params["probe-interval"])
	}
	if ch.Params["identity"] != "node-42" {
		t.Errorf("identity: got %q", ch.Params["identity"])
	}
}

func TestParse_PoolAuto(t *testing.T) {
	ch, err := Parse("hs:quic?endpoint=1.2.3.4:5000|pool=auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Params["pool"] != "auto" {
		t.Errorf("pool: got %q", ch.Params["pool"])
	}
}

// ─── Parse error cases ────────────────────────────────────────────────────────

func TestParse_MissingScheme(t *testing.T) {
	_, err := Parse("quic?endpoint=1.2.3.4:9000")
	if err == nil {
		t.Error("expected error for missing scheme")
	}
}

func TestParse_UnknownTransport(t *testing.T) {
	_, err := Parse("hs:tcp?endpoint=1.2.3.4:9000")
	if err == nil {
		t.Error("expected error for unknown transport")
	}
}

func TestParse_InvalidPoolTooLarge(t *testing.T) {
	_, err := Parse("hs:quic?endpoint=1.2.3.4:9000|pool=17")
	if err == nil {
		t.Error("expected error for pool > 16")
	}
}

func TestParse_InvalidPoolZero(t *testing.T) {
	_, err := Parse("hs:quic?endpoint=1.2.3.4:9000|pool=0")
	if err == nil {
		t.Error("expected error for pool=0")
	}
}

func TestParse_InvalidPoolNegative(t *testing.T) {
	_, err := Parse("hs:quic?endpoint=1.2.3.4:9000|pool=-1")
	if err == nil {
		t.Error("expected error for pool=-1")
	}
}

func TestParse_InvalidPoolNotNumber(t *testing.T) {
	_, err := Parse("hs:quic?endpoint=1.2.3.4:9000|pool=bad")
	if err == nil {
		t.Error("expected error for pool=bad")
	}
}

func TestParse_InvalidParamNoEquals(t *testing.T) {
	_, err := Parse("hs:quic?endpoint=1.2.3.4:9000|badparam")
	if err == nil {
		t.Error("expected error for parameter without '='")
	}
}

func TestParse_EmptyKey(t *testing.T) {
	_, err := Parse("hs:quic?=value")
	if err == nil {
		t.Error("expected error for empty parameter key")
	}
}

func TestParse_WrongSchemePrefix(t *testing.T) {
	_, err := Parse("aeron:quic?endpoint=1.2.3.4:9000")
	if err == nil {
		t.Error("expected error for wrong scheme")
	}
}

// ─── String (round-trip) ──────────────────────────────────────────────────────

func TestString_IPC(t *testing.T) {
	ch, _ := Parse("hs:ipc")
	if got := ch.String(); got != "hs:ipc" {
		t.Errorf("String: got %q want \"hs:ipc\"", got)
	}
}

func TestString_QUIC_ContainsFields(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=10.0.0.5:7777|pool=4|cc=bbrv3")
	s := ch.String()
	if !strings.HasPrefix(s, "hs:quic?") {
		t.Errorf("String: %q does not start with hs:quic?", s)
	}
	if !strings.Contains(s, "endpoint=10.0.0.5:7777") {
		t.Errorf("String: %q missing endpoint", s)
	}
	if !strings.Contains(s, "pool=4") {
		t.Errorf("String: %q missing pool", s)
	}
	if !strings.Contains(s, "cc=bbrv3") {
		t.Errorf("String: %q missing cc", s)
	}
}

func TestString_RoundTrip(t *testing.T) {
	uris := []string{
		"hs:ipc",
		"hs:quic?endpoint=10.0.0.5:7777",
		"hs:mdc?control=10.0.0.5:7778|destinations=dynamic",
	}
	for _, uri := range uris {
		ch, err := Parse(uri)
		if err != nil {
			t.Errorf("Parse(%q): %v", uri, err)
			continue
		}
		s := ch.String()
		ch2, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(String(%q)) = %q: %v", uri, s, err)
			continue
		}
		if ch2.Transport != ch.Transport {
			t.Errorf("round-trip transport mismatch for %q", uri)
		}
		if ch2.Endpoint != ch.Endpoint {
			t.Errorf("round-trip endpoint mismatch for %q: got %q", uri, ch2.Endpoint)
		}
		for k, v := range ch.Params {
			if ch2.Params[k] != v {
				t.Errorf("round-trip param[%q] mismatch for %q: got %q want %q", k, uri, ch2.Params[k], v)
			}
		}
	}
}

// ─── PoolSize ─────────────────────────────────────────────────────────────────

func TestPoolSize_Explicit(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|pool=8")
	size, isAuto := ch.PoolSize()
	if size != 8 || isAuto {
		t.Errorf("PoolSize: got (%d, %v) want (8, false)", size, isAuto)
	}
}

func TestPoolSize_Auto(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|pool=auto")
	size, isAuto := ch.PoolSize()
	if size != 0 || !isAuto {
		t.Errorf("PoolSize: got (%d, %v) want (0, true)", size, isAuto)
	}
}

func TestPoolSize_Default(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000")
	size, isAuto := ch.PoolSize()
	if size != 4 || isAuto {
		t.Errorf("PoolSize: got (%d, %v) want (4, false)", size, isAuto)
	}
}

func TestPoolSize_Boundary1(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|pool=1")
	size, isAuto := ch.PoolSize()
	if size != 1 || isAuto {
		t.Errorf("PoolSize: got (%d, %v) want (1, false)", size, isAuto)
	}
}

func TestPoolSize_Boundary16(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|pool=16")
	size, isAuto := ch.PoolSize()
	if size != 16 || isAuto {
		t.Errorf("PoolSize: got (%d, %v) want (16, false)", size, isAuto)
	}
}

// ─── CCName ───────────────────────────────────────────────────────────────────

func TestCCName_Default(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000")
	if got := ch.CCName(); got != "bbrv3" {
		t.Errorf("CCName default: got %q want \"bbrv3\"", got)
	}
}

func TestCCName_Explicit(t *testing.T) {
	tests := []struct{ cc, want string }{
		{"cubic", "cubic"},
		{"bbr", "bbr"},
		{"bbrv3", "bbrv3"},
		{"drl", "drl"},
	}
	for _, tc := range tests {
		ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|cc=" + tc.cc)
		if got := ch.CCName(); got != tc.want {
			t.Errorf("CCName(%q): got %q want %q", tc.cc, got, tc.want)
		}
	}
}

// ─── IsReliable ───────────────────────────────────────────────────────────────

func TestIsReliable_Default(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000")
	if !ch.IsReliable() {
		t.Error("IsReliable default: expected true")
	}
}

func TestIsReliable_True(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|reliable=true")
	if !ch.IsReliable() {
		t.Error("IsReliable(true): expected true")
	}
}

func TestIsReliable_TrueUpperCase(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|reliable=TRUE")
	if !ch.IsReliable() {
		t.Error("IsReliable(TRUE): expected true")
	}
}

func TestIsReliable_False(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|reliable=false")
	if ch.IsReliable() {
		t.Error("IsReliable(false): expected false")
	}
}

// ─── MTU ─────────────────────────────────────────────────────────────────────

func TestMTU_NotSet(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000")
	if ch.MTU() != 0 {
		t.Errorf("MTU (not set): got %d want 0", ch.MTU())
	}
}

func TestMTU_Set(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|mtu=1400")
	if ch.MTU() != 1400 {
		t.Errorf("MTU: got %d want 1400", ch.MTU())
	}
}

func TestMTU_Invalid(t *testing.T) {
	// Invalid MTU values degrade gracefully to 0.
	ch := &Channel{Params: map[string]string{"mtu": "bad"}}
	if ch.MTU() != 0 {
		t.Errorf("MTU(bad): got %d want 0", ch.MTU())
	}
}

func TestMTU_Zero(t *testing.T) {
	ch := &Channel{Params: map[string]string{"mtu": "0"}}
	if ch.MTU() != 0 {
		t.Errorf("MTU(0): got %d want 0", ch.MTU())
	}
}

// ─── ProbeInterval ────────────────────────────────────────────────────────────

func TestProbeInterval_NotSet(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000")
	if ch.ProbeInterval() != 0 {
		t.Errorf("ProbeInterval (not set): got %v want 0", ch.ProbeInterval())
	}
}

func TestProbeInterval_Milliseconds(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|probe-interval=500ms")
	if ch.ProbeInterval() != 500*time.Millisecond {
		t.Errorf("ProbeInterval: got %v want 500ms", ch.ProbeInterval())
	}
}

func TestProbeInterval_Seconds(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|probe-interval=2s")
	if ch.ProbeInterval() != 2*time.Second {
		t.Errorf("ProbeInterval: got %v want 2s", ch.ProbeInterval())
	}
}

func TestProbeInterval_Invalid(t *testing.T) {
	ch := &Channel{Params: map[string]string{"probe-interval": "not-a-duration"}}
	if ch.ProbeInterval() != 0 {
		t.Errorf("ProbeInterval(invalid): got %v want 0", ch.ProbeInterval())
	}
}

// ─── Identity ─────────────────────────────────────────────────────────────────

func TestIdentity_NotSet(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000")
	if ch.Identity() != "" {
		t.Errorf("Identity (not set): got %q want empty", ch.Identity())
	}
}

func TestIdentity_Set(t *testing.T) {
	ch, _ := Parse("hs:quic?endpoint=1.2.3.4:5000|identity=node-42")
	if ch.Identity() != "node-42" {
		t.Errorf("Identity: got %q want \"node-42\"", ch.Identity())
	}
}

// ─── Transport.String ─────────────────────────────────────────────────────────

func TestTransportString(t *testing.T) {
	tests := []struct {
		t    Transport
		want string
	}{
		{TransportQUIC, "quic"},
		{TransportIPC, "ipc"},
		{TransportMDC, "mdc"},
		{Transport(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.t.String(); got != tc.want {
			t.Errorf("Transport(%d).String() = %q, want %q", tc.t, got, tc.want)
		}
	}
}

// ─── Edge cases ───────────────────────────────────────────────────────────────

func TestParse_EmptyParamValue(t *testing.T) {
	// A param like "identity=" should be accepted with an empty value.
	ch, err := Parse("hs:quic?endpoint=1.2.3.4:5000|identity=")
	if err != nil {
		t.Fatalf("Parse with empty value: %v", err)
	}
	if ch.Identity() != "" {
		t.Errorf("Identity: got %q want empty", ch.Identity())
	}
}

func TestParse_MultipleParams(t *testing.T) {
	uri := "hs:quic?endpoint=1.2.3.4:5000|pool=2|cc=cubic|mtu=9000|reliable=false|identity=probe"
	ch, err := Parse(uri)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ch.PoolSize_n() != 2 {
		t.Errorf("pool: got %d want 2", ch.PoolSize_n())
	}
	if ch.CCName() != "cubic" {
		t.Errorf("cc: got %q want \"cubic\"", ch.CCName())
	}
	if ch.MTU() != 9000 {
		t.Errorf("mtu: got %d want 9000", ch.MTU())
	}
	if ch.IsReliable() {
		t.Error("reliable: expected false")
	}
	if ch.Identity() != "probe" {
		t.Errorf("identity: got %q", ch.Identity())
	}
}

// PoolSize_n is a test helper to get just the size (ignoring isAuto).
func (c *Channel) PoolSize_n() int {
	n, _ := c.PoolSize()
	return n
}

func TestParse_NoQuery_QUIC(t *testing.T) {
	// QUIC without any query is valid syntactically.
	ch, err := Parse("hs:quic")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ch.Transport != TransportQUIC {
		t.Errorf("Transport: got %v", ch.Transport)
	}
	if ch.Endpoint != "" {
		t.Errorf("Endpoint: got %q", ch.Endpoint)
	}
}
