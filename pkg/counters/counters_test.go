package counters_test

import (
	"sync"
	"testing"

	"github.com/cloud-jumpgate/hyperspace/pkg/counters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBuffer(t *testing.T) {
	buf := counters.NewBuffer()
	require.Len(t, buf, counters.NumCounters*8)
	for _, b := range buf {
		assert.Equal(t, byte(0), b, "buffer should be zero-initialised")
	}
}

func TestCounterNames(t *testing.T) {
	tests := []struct {
		id   int
		name string
	}{
		{counters.CtrBytesSent, "bytes_sent"},
		{counters.CtrBytesReceived, "bytes_received"},
		{counters.CtrMsgSent, "messages_sent"},
		{counters.CtrMsgReceived, "messages_received"},
		{counters.CtrConnectionsActive, "connections_active"},
		{counters.CtrConnectionOpens, "connection_opens"},
		{counters.CtrConnectionCloses, "connection_closes"},
		{counters.CtrPingsSent, "pings_sent"},
		{counters.CtrPongsReceived, "pongs_received"},
		{counters.CtrLostFrames, "lost_frames"},
		{counters.CtrBackPressureEvents, "backpressure_events"},
		{counters.CtrRotationEvents, "rotation_events"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.name, counters.CounterName(tt.id))
	}
	// Unknown IDs
	assert.Equal(t, "", counters.CounterName(-1))
	assert.Equal(t, "", counters.CounterName(counters.NumCounters))
}

func TestWriterGet(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)

	for i := 0; i < counters.NumCounters; i++ {
		assert.Equal(t, int64(0), w.Get(i))
	}
}

func TestWriterSet(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)

	w.Set(counters.CtrBytesSent, 42)
	assert.Equal(t, int64(42), w.Get(counters.CtrBytesSent))

	w.Set(counters.CtrConnectionsActive, 7)
	assert.Equal(t, int64(7), w.Get(counters.CtrConnectionsActive))
}

func TestWriterAdd(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)

	w.Add(counters.CtrMsgSent, 100)
	assert.Equal(t, int64(100), w.Get(counters.CtrMsgSent))

	w.Add(counters.CtrMsgSent, 50)
	assert.Equal(t, int64(150), w.Get(counters.CtrMsgSent))

	// Negative delta
	w.Add(counters.CtrConnectionsActive, 5)
	w.Add(counters.CtrConnectionsActive, -2)
	assert.Equal(t, int64(3), w.Get(counters.CtrConnectionsActive))
}

func TestReaderSeesWriterUpdates(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	r := counters.NewCountersReader(buf)

	w.Set(counters.CtrBytesSent, 999)
	assert.Equal(t, int64(999), r.Get(counters.CtrBytesSent))

	w.Add(counters.CtrLostFrames, 3)
	assert.Equal(t, int64(3), r.Get(counters.CtrLostFrames))
}

func TestWriterReader(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	r := w.Reader()

	w.Set(counters.CtrPingsSent, 1234)
	assert.Equal(t, int64(1234), r.Get(counters.CtrPingsSent))
}

func TestAllCounterIDs(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	r := counters.NewCountersReader(buf)

	for i := 0; i < counters.NumCounters; i++ {
		w.Set(i, int64(i*100))
	}
	for i := 0; i < counters.NumCounters; i++ {
		assert.Equal(t, int64(i*100), r.Get(i), "counter %d mismatch", i)
	}
}

func TestConcurrentAdd(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)

	const goroutines = 100
	const addsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < addsPerGoroutine; j++ {
				w.Add(counters.CtrBytesSent, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(goroutines*addsPerGoroutine), w.Get(counters.CtrBytesSent))
}

func TestConcurrentReadWrite(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	r := counters.NewCountersReader(buf)

	var wg sync.WaitGroup
	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				w.Add(counters.CtrMsgSent, 1)
			}
		}()
	}
	// Readers — just verify no data race
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = r.Get(counters.CtrMsgSent)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(10*500), w.Get(counters.CtrMsgSent))
}

func TestPanicOnTooSmallBuffer(t *testing.T) {
	assert.Panics(t, func() {
		counters.NewCountersWriter(make([]byte, 4))
	})
	assert.Panics(t, func() {
		counters.NewCountersReader(make([]byte, 4))
	})
}

func TestPanicOnInvalidID(t *testing.T) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	r := counters.NewCountersReader(buf)

	assert.Panics(t, func() { w.Get(-1) })
	assert.Panics(t, func() { w.Get(counters.NumCounters) })
	assert.Panics(t, func() { w.Set(-1, 0) })
	assert.Panics(t, func() { w.Add(counters.NumCounters, 1) })
	assert.Panics(t, func() { r.Get(-1) })
	assert.Panics(t, func() { r.Get(counters.NumCounters) })
}

func BenchmarkWriterAdd(b *testing.B) {
	buf := counters.NewBuffer()
	w := counters.NewCountersWriter(buf)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Add(counters.CtrBytesSent, 1)
	}
}

func BenchmarkReaderGet(b *testing.B) {
	buf := counters.NewBuffer()
	r := counters.NewCountersReader(buf)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Get(counters.CtrBytesSent)
	}
}
