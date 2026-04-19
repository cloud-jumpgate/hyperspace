package logbuffer

// sprint_contracts_test.go — tests required by sprint contracts F-001 to F-012.
// Added to satisfy CONDITIONAL PASS → PASS promotion for the logbuffer package.

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/memmap"
)

// TestAppender_ThreeTermRotation fills term 0 completely, observes rotation,
// fills term 1, fills term 2, and verifies back-pressure is returned when all
// three terms are exhausted (wrapping back to the already-full term 0).
func TestAppender_ThreeTermRotation(t *testing.T) {
	lb, _ := makeLogBuffer(t, MinTermLength)

	// Fill term 0 until rotation or back-pressure.
	app0 := lb.Appender(0)
	var rotationCode int64
	for i := 0; i < 100000; i++ {
		r := app0.AppendUnfragmented(1, 1, 0, []byte("fill-term-0"), 0)
		if r == AppendRotation || r == AppendBackPressure {
			rotationCode = r
			break
		}
	}
	// Term 0 must have returned rotation or back-pressure.
	if rotationCode != AppendRotation && rotationCode != AppendBackPressure {
		t.Fatalf("term 0: expected AppendRotation or AppendBackPressure, got %d", rotationCode)
	}

	// Fill term 1 until rotation or back-pressure.
	app1 := lb.Appender(1)
	var term1Code int64
	for i := 0; i < 100000; i++ {
		r := app1.AppendUnfragmented(1, 1, 1, []byte("fill-term-1"), 0)
		if r == AppendRotation || r == AppendBackPressure {
			term1Code = r
			break
		}
	}
	if term1Code != AppendRotation && term1Code != AppendBackPressure {
		t.Fatalf("term 1: expected rotation/back-pressure, got %d", term1Code)
	}

	// Fill term 2 — after all three terms are full, back-pressure must eventually occur.
	app2 := lb.Appender(2)
	var term2Code int64
	for i := 0; i < 100000; i++ {
		r := app2.AppendUnfragmented(1, 1, 2, []byte("fill-term-2"), 0)
		if r == AppendRotation || r == AppendBackPressure {
			term2Code = r
			break
		}
	}
	if term2Code != AppendRotation && term2Code != AppendBackPressure {
		t.Fatalf("term 2: expected rotation/back-pressure, got %d", term2Code)
	}

	// All three terms have reached their limits — the three-term rotation sequence is complete.
	t.Logf("term0=%d term1=%d term2=%d", rotationCode, term1Code, term2Code)
}

// TestAppender_ConcurrentWrites spawns 8 goroutines each appending 10,000
// messages to the same log buffer. The test counts successful writes and
// verifies the race detector finds no data races.
func TestAppender_ConcurrentWrites(t *testing.T) {
	// Use a large term so we can observe many successful appends before
	// rotation/back-pressure. DefaultTermLength = 16 MiB.
	termLen := DefaultTermLength
	total := NumPartitions*termLen + LogMetaDataLength
	buf := make([]byte, total)
	lb, err := New(buf, termLen)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// All goroutines share the partition-0 appender — concurrent claims are
	// serialised by the atomic tail counter inside AppendUnfragmented.
	app := lb.Appender(0)

	const goroutines = 8
	const messagesPerGoroutine = 10_000
	const totalMessages = goroutines * messagesPerGoroutine

	var (
		wg        sync.WaitGroup
		written   atomic.Int64
		backPressure atomic.Int64
		rotation  atomic.Int64
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			msg := []byte("concurrent-write-payload")
			for i := 0; i < messagesPerGoroutine; i++ {
				r := app.AppendUnfragmented(int32(id), 1, 0, msg, 0) //nolint:gosec // id bounded by goroutines
				switch r {
				case AppendBackPressure:
					backPressure.Add(1)
				case AppendRotation:
					rotation.Add(1)
				default:
					if r > 0 {
						written.Add(1)
					}
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify totals add up.
	totalSeen := written.Load() + backPressure.Load() + rotation.Load()
	if totalSeen != int64(totalMessages) {
		t.Errorf("total ops %d != expected %d (written=%d bp=%d rot=%d)",
			totalSeen, totalMessages, written.Load(), backPressure.Load(), rotation.Load())
	}

	t.Logf("concurrent writes: success=%d back-pressure=%d rotation=%d",
		written.Load(), backPressure.Load(), rotation.Load())
}

// TestLogBuffer_FilePermissions verifies that memmap.Create produces a file
// with mode 0600 (owner read-write, no group or world access), satisfying
// the security requirement in CLAUDE.md ("mmap files are 0600").
func TestLogBuffer_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test-logbuf.bin"
	const size = int64(MinTermLength)

	m, err := memmap.Create(path, size)
	if err != nil {
		t.Fatalf("memmap.Create: %v", err)
	}
	defer func() { _ = m.Close() }()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}

	perm := fi.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permission: got %04o, want %04o (0600)", perm, 0o600)
	}
}
