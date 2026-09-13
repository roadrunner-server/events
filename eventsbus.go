package events

import (
	"fmt"
	"strings"
	"sync"

	"github.com/roadrunner-server/errors"
)

type EventBus interface {
	SubscribeAll(subID string, ch chan<- Event) error
	SubscribeP(subID string, pattern string, ch chan<- Event) error
	Unsubscribe(subID string)
	UnsubscribeP(subID, pattern string)
	Len() uint
	Send(ev Event)
}

type Event interface {
	Type() fmt.Stringer
	Plugin() string
	Message() string
}

type sub struct {
	pattern string
	w       *wildcard
	events  chan<- Event
	queue   *eventQueue
}

type Bus struct {
	mu           sync.RWMutex
	subscribers  map[string][]*sub
	internalEvCh chan Event
	stop         chan struct{}
}

func newEventsBus() *Bus {
	return &Bus{
		subscribers:  make(map[string][]*sub, 10),
		internalEvCh: make(chan Event, 100),
		stop:         make(chan struct{}),
	}
}

/*
http.* <-
*/

// SubscribeAll for all RR events
// returns subscriptionID
func (eb *Bus) SubscribeAll(subID string, ch chan<- Event) error {
	if ch == nil {
		return errors.Str("nil channel provided")
	}

	subIDTr := strings.Trim(subID, " ")

	if subIDTr == "" {
		return errors.Str("subscriberID can't be empty")
	}

	return eb.subscribe(subID, "*", ch, false)
}

// SubscribeP pattern like "pluginName.EventType"
func (eb *Bus) SubscribeP(subID string, pattern string, ch chan<- Event) error {
	return eb.subscribePattern(subID, pattern, ch, false)
}

// SubscribePQueued retains matching events in an in-memory queue until the
// receiver accepts them. Unsubscribe or UnsubscribeP discards pending events
// and stops delivery before returning. The receiver owns the channel.
func (eb *Bus) SubscribePQueued(subID string, pattern string, ch chan<- Event) error {
	return eb.subscribePattern(subID, pattern, ch, true)
}

func (eb *Bus) subscribePattern(subID string, pattern string, ch chan<- Event, queued bool) error {
	if ch == nil {
		return errors.Str("nil channel provided")
	}

	subIDTr := strings.Trim(subID, " ")
	patternTr := strings.Trim(pattern, " ")

	if subIDTr == "" || patternTr == "" {
		return errors.Str("subscriberID or pattern can't be empty")
	}

	return eb.subscribe(subID, pattern, ch, queued)
}

func (eb *Bus) Unsubscribe(subID string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for _, s := range eb.subscribers[subID] {
		if s.queue != nil {
			s.queue.stop()
		}
	}
	delete(eb.subscribers, subID)
}

func (eb *Bus) UnsubscribeP(subID, pattern string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subs, ok := eb.subscribers[subID]
	if !ok {
		return
	}

	remaining := subs[:0]
	for _, s := range subs {
		if s.pattern == pattern {
			if s.queue != nil {
				s.queue.stop()
			}
			continue
		}
		remaining = append(remaining, s)
	}
	clear(subs[len(remaining):])
	eb.subscribers[subID] = remaining
}

// Send sends event to the events bus
func (eb *Bus) Send(ev Event) {
	// do not accept nil events
	if ev == nil {
		return
	}

	eb.internalEvCh <- ev
}

func (eb *Bus) Len() uint {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return uint(len(eb.subscribers))
}

func (eb *Bus) subscribe(subID string, pattern string, ch chan<- Event, queued bool) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	w, err := newWildcard(pattern)
	if err != nil {
		return err
	}

	s := &sub{
		pattern: pattern,
		w:       w,
		events:  ch,
	}
	if queued {
		s.queue = newEventQueue(ch)
	}
	eb.subscribers[subID] = append(eb.subscribers[subID], s)

	return nil
}

func (eb *Bus) handleEvents() {
	for ev := range eb.internalEvCh {
		// http.WorkerError for example
		wc := fmt.Sprintf("%s.%s", ev.Plugin(), ev.Type().String())

		eb.mu.RLock()

		for _, vsub := range eb.subscribers {
			for i := range vsub {
				if vsub[i].w.match(wc) {
					if vsub[i].queue != nil {
						vsub[i].queue.enqueue(ev)
						continue
					}
					select {
					case vsub[i].events <- ev:
					default:
					}
				}
			}
		}

		eb.mu.RUnlock()
	}
}
