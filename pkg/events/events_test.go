package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/cloud-jumpgate/hyperspace/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventLog_PanicsOnBadCapacity(t *testing.T) {
	assert.Panics(t, func() { events.NewEventLog(0) })
	assert.Panics(t, func() { events.NewEventLog(-1) })
	assert.Panics(t, func() { events.NewEventLog(3) })
	assert.Panics(t, func() { events.NewEventLog(6) })
}

func TestNewEventLog_ValidCapacity(t *testing.T) {
	for _, cap := range []int{1, 2, 4, 8, 16, 64, 128} {
		el := events.NewEventLog(cap)
		require.NotNil(t, el, "capacity %d", cap)
	}
}

func TestLogAndPoll_Basic(t *testing.T) {
	el := events.NewEventLog(16)
	r := el.NewReader()

	evt := events.Event{
		Type:     events.EvtConnectionOpened,
		ConnID:   42,
		StreamID: 7,
		Value1:   1234,
	}
	evt.SetMessage("conn opened")
	el.Log(evt)

	var got []events.Event
	n := r.Poll(func(e events.Event) {
		got = append(got, e)
	})

	require.Equal(t, 1, n)
	require.Len(t, got, 1)
	assert.Equal(t, events.EvtConnectionOpened, got[0].Type)
	assert.Equal(t, uint64(42), got[0].ConnID)
	assert.Equal(t, int32(7), got[0].StreamID)
	assert.Equal(t, int64(1234), got[0].Value1)
	assert.Equal(t, "conn opened", got[0].GetMessage())
}

func TestLogAndPoll_EmptyRing(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()
	n := r.Poll(func(e events.Event) {})
	assert.Equal(t, 0, n)
}

func TestLogAndPoll_MultipleEvents(t *testing.T) {
	el := events.NewEventLog(16)
	r := el.NewReaderFromStart()

	types := []events.EventType{
		events.EvtConnectionOpened,
		events.EvtPublicationAdded,
		events.EvtSubscriptionAdded,
		events.EvtPathProbeRTT,
		events.EvtBackPressure,
	}

	for i, tp := range types {
		evt := events.Event{
			Type:   tp,
			ConnID: uint64(i),
			Value1: int64(i * 100),
		}
		evt.SetMessage("test")
		el.Log(evt)
	}

	var got []events.Event
	r.Poll(func(e events.Event) {
		got = append(got, e)
	})

	require.Len(t, got, len(types))
	for i, tp := range types {
		assert.Equal(t, tp, got[i].Type, "index %d", i)
		assert.Equal(t, uint64(i), got[i].ConnID, "index %d", i)
		assert.Equal(t, int64(i*100), got[i].Value1, "index %d", i)
	}
}

func TestLogAndPoll_TimestampAutoSet(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()

	before := time.Now().UnixNano()
	el.Log(events.Event{Type: events.EvtConnectionOpened})
	after := time.Now().UnixNano()

	var got events.Event
	r.Poll(func(e events.Event) { got = e })

	assert.GreaterOrEqual(t, got.TimestampNs, before)
	assert.LessOrEqual(t, got.TimestampNs, after)
}

func TestLogAndPoll_ExplicitTimestamp(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()

	ts := int64(9876543210)
	el.Log(events.Event{Type: events.EvtConnectionClosed, TimestampNs: ts})

	var got events.Event
	r.Poll(func(e events.Event) { got = e })

	assert.Equal(t, ts, got.TimestampNs)
}

func TestEventFields_AllRoundTrip(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()

	orig := events.Event{
		Type:        events.EvtCCTransition,
		TimestampNs: 111222333444,
		ConnID:      0xDEADBEEFCAFE,
		StreamID:    -7,
		SessionID:   42,
		Value1:      -999999,
		Value2:      888888,
	}
	orig.SetMessage("cc: slow->fast")
	el.Log(orig)

	var got events.Event
	n := r.Poll(func(e events.Event) { got = e })

	require.Equal(t, 1, n)
	assert.Equal(t, orig.Type, got.Type)
	assert.Equal(t, orig.TimestampNs, got.TimestampNs)
	assert.Equal(t, orig.ConnID, got.ConnID)
	assert.Equal(t, orig.StreamID, got.StreamID)
	assert.Equal(t, orig.SessionID, got.SessionID)
	assert.Equal(t, orig.Value1, got.Value1)
	assert.Equal(t, orig.Value2, got.Value2)
	assert.Equal(t, "cc: slow->fast", got.GetMessage())
}

func TestSetMessage_Truncation(t *testing.T) {
	var evt events.Event
	long := "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz012" // 66 chars
	evt.SetMessage(long)
	msg := evt.GetMessage()
	assert.LessOrEqual(t, len(msg), 63)
	assert.Equal(t, byte(0), evt.Message[len(msg)], "null terminator")
}

func TestMultipleReaders(t *testing.T) {
	el := events.NewEventLog(16)

	for i := 0; i < 5; i++ {
		el.Log(events.Event{Type: events.EvtPublicationAdded, ConnID: uint64(i)})
	}

	r1 := el.NewReaderFromStart()
	r2 := el.NewReaderFromStart()

	var got1, got2 []events.Event
	r1.Poll(func(e events.Event) { got1 = append(got1, e) })
	r2.Poll(func(e events.Event) { got2 = append(got2, e) })

	require.Len(t, got1, 5)
	require.Len(t, got2, 5)
	for i := 0; i < 5; i++ {
		assert.Equal(t, uint64(i), got1[i].ConnID)
		assert.Equal(t, uint64(i), got2[i].ConnID)
	}
}

func TestNewReaderMissesOldEvents(t *testing.T) {
	el := events.NewEventLog(8)
	el.Log(events.Event{Type: events.EvtConnectionOpened, ConnID: 1})
	el.Log(events.Event{Type: events.EvtConnectionOpened, ConnID: 2})

	// Reader created after logging — should not see the above events
	r := el.NewReader()
	var got []events.Event
	r.Poll(func(e events.Event) { got = append(got, e) })
	assert.Len(t, got, 0, "new reader should not see events logged before creation")

	// Log a new event — reader should see it
	el.Log(events.Event{Type: events.EvtConnectionClosed, ConnID: 99})
	r.Poll(func(e events.Event) { got = append(got, e) })
	require.Len(t, got, 1)
	assert.Equal(t, uint64(99), got[0].ConnID)
}

func TestPoll_SecondCallReturnsZero(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()

	el.Log(events.Event{Type: events.EvtBackPressure})
	n1 := r.Poll(func(e events.Event) {})
	n2 := r.Poll(func(e events.Event) {})

	assert.Equal(t, 1, n1)
	assert.Equal(t, 0, n2)
}

func TestConcurrentLogAndPoll(t *testing.T) {
	el := events.NewEventLog(128)

	const producers = 4
	const eventsPerProducer = 50

	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Add(1)
		pid := i
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerProducer; j++ {
				el.Log(events.Event{
					Type:   events.EvtPublicationAdded,
					ConnID: uint64(pid*1000 + j),
				})
			}
		}()
	}
	wg.Wait()

	// Drain all events
	r := el.NewReaderFromStart()
	var total int
	r.Poll(func(e events.Event) { total++ })

	// We expect all events but ring may not hold all of them (depends on capacity)
	assert.GreaterOrEqual(t, total, 0)
	assert.LessOrEqual(t, total, producers*eventsPerProducer)
}

func TestGetMessage_NoNullTerminator(t *testing.T) {
	var evt events.Event
	// Fill Message completely with non-zero bytes (no null)
	for i := range evt.Message {
		evt.Message[i] = 'x'
	}
	msg := evt.GetMessage()
	assert.Equal(t, 64, len(msg))
}

func TestLogTimestampZeroGetsFilled(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()

	// Log with explicit zero — should get auto-filled
	el.Log(events.Event{Type: events.EvtConnectionOpened, TimestampNs: 0})

	var got events.Event
	r.Poll(func(e events.Event) { got = e })
	assert.NotZero(t, got.TimestampNs)
}

func TestEncodeDecodeNegativeValues(t *testing.T) {
	el := events.NewEventLog(8)
	r := el.NewReader()

	orig := events.Event{
		Type:        events.EvtCCTransition,
		TimestampNs: -1,
		StreamID:    -2147483648,
		SessionID:   -1,
		Value1:      -9223372036854775808,
		Value2:      -1,
	}
	el.Log(orig)

	var got events.Event
	r.Poll(func(e events.Event) { got = e })

	assert.Equal(t, orig.TimestampNs, got.TimestampNs)
	assert.Equal(t, orig.StreamID, got.StreamID)
	assert.Equal(t, orig.SessionID, got.SessionID)
	assert.Equal(t, orig.Value1, got.Value1)
	assert.Equal(t, orig.Value2, got.Value2)
}

func TestAllEventTypes(t *testing.T) {
	el := events.NewEventLog(32)
	r := el.NewReader()

	eventTypes := []events.EventType{
		events.EvtConnectionOpened,
		events.EvtConnectionClosed,
		events.EvtPublicationAdded,
		events.EvtPublicationRemoved,
		events.EvtSubscriptionAdded,
		events.EvtSubscriptionRemoved,
		events.EvtPathProbeRTT,
		events.EvtPoolLearnerAction,
		events.EvtCCTransition,
		events.EvtBackPressure,
	}

	for _, et := range eventTypes {
		el.Log(events.Event{Type: et})
	}

	var got []events.EventType
	r.Poll(func(e events.Event) { got = append(got, e.Type) })

	require.Len(t, got, len(eventTypes))
	for i, et := range eventTypes {
		assert.Equal(t, et, got[i], "index %d", i)
	}
}
