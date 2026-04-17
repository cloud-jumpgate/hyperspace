// hyperspace-stat attaches to a running hsd and prints live counter values.
//
// Usage:
//
//	hyperspace-stat [--cnc /dev/shm/hyperspace/cnc.dat] [--interval 1s]
//
// The tool memory-maps the CnC file, reads counters every --interval, computes
// per-second rates from successive samples, and prints a refreshed table to stdout.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/counters"
	"github.com/cloud-jumpgate/hyperspace/pkg/ipc/memmap"
)

// counterMeta holds display metadata for each counter.
type counterMeta struct {
	id      int
	label   string
	showRate bool // false for absolute-only counters (e.g. connections_active)
}

// displayCounters defines the ordered display list.
var displayCounters = []counterMeta{
	{counters.CtrBytesSent, "bytes_sent", true},
	{counters.CtrBytesReceived, "bytes_received", true},
	{counters.CtrMsgSent, "messages_sent", true},
	{counters.CtrMsgReceived, "messages_received", true},
	{counters.CtrConnectionsActive, "connections_active", false},
	{counters.CtrConnectionOpens, "connection_opens", false},
	{counters.CtrConnectionCloses, "connection_closes", false},
	{counters.CtrPingsSent, "pings_sent", true},
	{counters.CtrPongsReceived, "pongs_received", true},
	{counters.CtrLostFrames, "lost_frames", true},
	{counters.CtrBackPressureEvents, "backpressure_events", true},
	{counters.CtrRotationEvents, "rotation_events", false},
}

func main() {
	cncPath := flag.String("cnc", "/dev/shm/hyperspace/cnc.dat", "path to the CnC file")
	intervalStr := flag.String("interval", "1s", "refresh interval (e.g. 500ms, 1s, 2s)")
	flag.Parse()

	interval, err := time.ParseDuration(*intervalStr)
	if err != nil {
		slog.Error("invalid --interval", "value", *intervalStr, "err", err)
		os.Exit(1)
	}
	if interval <= 0 {
		slog.Error("--interval must be positive")
		os.Exit(1)
	}

	mf, err := memmap.Open(*cncPath)
	if err != nil {
		slog.Error("failed to open CnC file", "path", *cncPath, "err", err)
		os.Exit(1)
	}
	defer func() {
		if cerr := mf.Close(); cerr != nil {
			slog.Error("failed to close CnC file", "err", cerr)
		}
	}()

	buf := mf.Bytes()
	if len(buf) < counters.NumCounters*8 {
		slog.Error("CnC file too small for counter region",
			"size", len(buf), "required", counters.NumCounters*8)
		os.Exit(1)
	}

	reader := counters.NewCountersReader(buf)

	// Capture initial sample for rate computation.
	prev := snapshot(reader)
	prevTime := time.Now()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for t := range ticker.C {
		curr := snapshot(reader)
		elapsed := t.Sub(prevTime).Seconds()
		printTable(curr, prev, elapsed)
		prev = curr
		prevTime = t
	}
}

// snapshot reads all counter values atomically into a slice indexed by counter ID.
func snapshot(r *counters.CountersReader) []int64 {
	s := make([]int64, counters.NumCounters)
	for i := 0; i < counters.NumCounters; i++ {
		s[i] = r.Get(i)
	}
	return s
}

// printTable writes the counter table to stdout using text/tabwriter.
func printTable(curr, prev []int64, elapsedSecs float64) {
	fmt.Printf("\nhyperspace-stat — %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "COUNTER\tVALUE\tRATE/s")

	for _, m := range displayCounters {
		val := curr[m.id]
		rateStr := "—"
		if m.showRate && elapsedSecs > 0 {
			delta := curr[m.id] - prev[m.id]
			rate := float64(delta) / elapsedSecs
			rateStr = formatInt(int64(rate))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", m.label, formatInt(val), rateStr)
	}

	w.Flush()
}

// formatInt formats n with thousands separators.
func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	// Insert commas every 3 digits from the right.
	result := make([]byte, 0, len(s)+len(s)/3)
	offset := len(s) % 3
	if offset == 0 {
		offset = 3
	}
	result = append(result, s[:offset]...)
	for i := offset; i < len(s); i += 3 {
		result = append(result, ',')
		result = append(result, s[i:i+3]...)
	}
	return string(result)
}
