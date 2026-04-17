package cc_test

import (
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/cc"
	// Import implementations to trigger their init() registrations.
	_ "github.com/cloud-jumpgate/hyperspace/pkg/cc/bbr"
	_ "github.com/cloud-jumpgate/hyperspace/pkg/cc/bbrv3"
	_ "github.com/cloud-jumpgate/hyperspace/pkg/cc/cubic"
	_ "github.com/cloud-jumpgate/hyperspace/pkg/cc/drl"
)

func TestRegisterAndNew(t *testing.T) {
	// All implementations should be registered via init().
	names := cc.Names()
	want := map[string]bool{"bbr": true, "bbrv3": true, "cubic": true, "drl": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected registered name: %q", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("expected registered name not found: %q", n)
	}
}

func TestNamesIsSorted(t *testing.T) {
	names := cc.Names()
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("Names() not sorted: %v", names)
			break
		}
	}
}

func TestNewUnknownReturnsError(t *testing.T) {
	_, err := cc.New("nonexistent", 12000, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for unknown CC name, got nil")
	}
}

func TestNewKnownReturnsCongestionControl(t *testing.T) {
	for _, name := range []string{"cubic", "bbr", "bbrv3", "drl"} {
		ctrl, err := cc.New(name, 12000, 50*time.Millisecond)
		if err != nil {
			t.Fatalf("New(%q): unexpected error: %v", name, err)
		}
		if ctrl == nil {
			t.Fatalf("New(%q): got nil controller", name)
		}
		if ctrl.Name() != name {
			t.Errorf("New(%q): Name() = %q, want %q", name, ctrl.Name(), name)
		}
	}
}

func TestRegisterPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate Register, got none")
		}
	}()
	// Register a name twice — second call must panic.
	cc.Register("cubic-dup-test", func(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
		return nil
	})
	cc.Register("cubic-dup-test", func(initialCwnd int, minRTT time.Duration) cc.CongestionControl {
		return nil
	})
}
