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

	return eb.subscribe(subID, "*", ch)
}

// SubscribeP pattern like "pluginName.EventType"
func (eb *Bus) SubscribeP(subID string, pattern string, ch chan<- Event) error {
	if ch == nil {
		return errors.Str("nil channel provided")
	}

	subIDTr := strings.Trim(subID, " ")
	patternTr := strings.Trim(pattern, " ")

	if subIDTr == "" || patternTr == "" {
		return errors.Str("subscriberID or pattern can't be empty")
	}

	return eb.subscribe(subID, pattern, ch)
}

func (eb *Bus) Unsubscribe(subID string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	delete(eb.subscribers, subID)
}

func (eb *Bus) UnsubscribeP(subID, pattern string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if _, ok := eb.subscribers[subID]; !ok {
		return
	}

	sbArr := eb.subscribers[subID]

	for i := range sbArr {
		if sbArr[i].pattern == pattern {
			sbArr[i] = sbArr[len(sbArr)-1]
			sbArr = sbArr[:len(sbArr)-1]
			// replace with new array
			eb.subscribers[subID] = sbArr
		}
	}
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

func (eb *Bus) subscribe(subID string, pattern string, ch chan<- Event) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	w, err := newWildcard(pattern)
	if err != nil {
		return err
	}

	if subArr, ok := eb.subscribers[subID]; ok {
		// at this point we are confident that sb is a []*sub type
		subArr = append(subArr, &sub{
			pattern: pattern,
			w:       w,
			events:  ch,
		})

		eb.subscribers[subID] = subArr

		return nil
	}

	subArr := make([]*sub, 0, 1)
	subArr = append(subArr, &sub{
		pattern: pattern,
		w:       w,
		events:  ch,
	})

	eb.subscribers[subID] = subArr

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
