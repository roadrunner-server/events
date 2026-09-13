package events

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newQueuedTestBus(t *testing.T) QueuedEventBus {
	t.Helper()
	bus := newEventsBus()
	done := make(chan struct{})
	go func() {
		defer close(done)
		bus.handleEvents()
	}()
	t.Cleanup(func() {
		close(bus.internalEvCh)
		<-done
	})
	return bus
}

func nextQueuedEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second * 5):
		t.Fatal("the event was not delivered")
		return nil
	}
}

func TestQueuedSubscriptionRetainsEventsInOrder(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
	}{
		{name: "unbuffered receiver"},
		{name: "one buffered event", capacity: 1},
		{name: "small receiver buffer", capacity: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const count = 512
			bus := newQueuedTestBus(t)
			ch := make(chan Event, tt.capacity)
			observer := make(chan Event, count)
			require.NoError(t, bus.SubscribePQueued("queued", "*.EventJOBSDriverCommand", ch))
			t.Cleanup(func() { bus.Unsubscribe("queued") })
			require.NoError(t, bus.SubscribeP("observer", "*.EventJOBSDriverCommand", observer))
			t.Cleanup(func() { bus.Unsubscribe("observer") })

			for i := range count {
				bus.Send(NewEvent(EventJOBSDriverCommand, "queue", strconv.Itoa(i)))
			}
			// A slow queued receiver must let other subscribers receive events.
			for i := range count {
				require.Equal(t, strconv.Itoa(i), nextQueuedEvent(t, observer).Message())
			}
			for i := range count {
				ev := nextQueuedEvent(t, ch)
				require.Equal(t, strconv.Itoa(i), ev.Message())
				require.Equal(t, "queue", ev.Plugin())
			}
			bus.Send(NewEvent(EventJOBSDriverCommand, "Queue", "after drain"))
			ev := nextQueuedEvent(t, ch)
			require.Equal(t, "after drain", ev.Message())
			require.Equal(t, "Queue", ev.Plugin())
		})
	}
}

func TestQueuedSubscriptionValidation(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		pattern string
		nilCh   bool
	}{
		{name: "nil channel", id: "queued", pattern: "*", nilCh: true},
		{name: "empty id", pattern: "*"},
		{name: "blank id", id: "  ", pattern: "*"},
		{name: "empty pattern", id: "queued"},
		{name: "blank pattern", id: "queued", pattern: "  "},
		{name: "invalid pattern", id: "queued", pattern: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := newQueuedTestBus(t)
			var ch chan Event
			if !tt.nilCh {
				ch = make(chan Event)
			}
			require.Error(t, bus.SubscribePQueued(tt.id, tt.pattern, ch))
			require.Zero(t, bus.Len())
		})
	}
}

func TestQueuedSubscriptionUnsubscribe(t *testing.T) {
	tests := []struct {
		name       string
		pattern    bool
		pending    bool
		duplicates int
	}{
		{name: "idle subscriber", duplicates: 1},
		{name: "blocked subscriber", pending: true, duplicates: 1},
		{name: "all subscriber patterns", pending: true, duplicates: 3},
		{name: "idle pattern", pattern: true, duplicates: 1},
		{name: "blocked pattern", pattern: true, pending: true, duplicates: 1},
		{name: "duplicate patterns", pattern: true, pending: true, duplicates: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const pattern = "*.EventJOBSDriverCommand"
			bus := newQueuedTestBus(t)
			ch := make(chan Event)
			for range tt.duplicates {
				require.NoError(t, bus.SubscribePQueued("queued", pattern, ch))
			}
			t.Cleanup(func() { bus.Unsubscribe("queued") })
			other := make(chan Event, 1)
			require.NoError(t, bus.SubscribeP("queued", "*.EventWorkerError", other))
			observer := make(chan Event, 2)
			require.NoError(t, bus.SubscribeAll("observer", observer))
			t.Cleanup(func() { bus.Unsubscribe("observer") })
			if tt.pending {
				bus.Send(NewEvent(EventJOBSDriverCommand, "queue", "first"))
				bus.Send(NewEvent(EventJOBSDriverCommand, "queue", "second"))
				require.Equal(t, "first", nextQueuedEvent(t, observer).Message())
				require.Equal(t, "second", nextQueuedEvent(t, observer).Message())
			}

			done := make(chan struct{})
			go func() {
				if tt.pattern {
					bus.UnsubscribeP("queued", pattern)
				} else {
					bus.Unsubscribe("queued")
				}
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second * 5):
				t.Fatal("unsubscribe waited for the blocked receiver")
			}
			close(ch)
			bus.Send(NewEvent(EventJOBSDriverCommand, "queue", "after unsubscribe"))
			bus.Send(NewEvent(EventWorkerError, "worker", "still active"))
			require.Equal(t, "after unsubscribe", nextQueuedEvent(t, observer).Message())
			require.Equal(t, "still active", nextQueuedEvent(t, observer).Message())
			if tt.pattern {
				require.Equal(t, "still active", nextQueuedEvent(t, other).Message())
			} else {
				require.Empty(t, other)
			}
		})
	}
}

func TestQueuedUnsubscribeDuringPublish(t *testing.T) {
	bus := newQueuedTestBus(t)
	ch := make(chan Event)
	require.NoError(t, bus.SubscribePQueued("queued", "*", ch))
	t.Cleanup(func() { bus.Unsubscribe("queued") })
	observer := make(chan Event, 1000)
	require.NoError(t, bus.SubscribeAll("observer", observer))
	t.Cleanup(func() { bus.Unsubscribe("observer") })
	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := range 1000 {
			bus.Send(NewEvent(EventJOBSDriverCommand, "queue", strconv.Itoa(i)))
		}
	}()
	nextQueuedEvent(t, observer)
	bus.Unsubscribe("queued")
	close(ch)
	<-published
	for range 999 {
		nextQueuedEvent(t, observer)
	}
}
