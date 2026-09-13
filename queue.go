package events

import "sync"

type eventQueue struct {
	mu      sync.Mutex
	ready   *sync.Cond
	pending []Event
	closed  bool
	stopCh  chan struct{}
	done    chan struct{}
}

func newEventQueue(ch chan<- Event) *eventQueue {
	q := &eventQueue{
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	q.ready = sync.NewCond(&q.mu)
	go q.deliver(ch)
	return q
}

func (q *eventQueue) enqueue(ev Event) {
	q.mu.Lock()
	q.pending = append(q.pending, ev)
	q.ready.Signal()
	q.mu.Unlock()
}

func (q *eventQueue) deliver(ch chan<- Event) {
	defer close(q.done)
	for {
		q.mu.Lock()
		for len(q.pending) == 0 && !q.closed {
			q.ready.Wait()
		}
		if q.closed {
			q.mu.Unlock()
			return
		}
		ev := q.pending[0]
		q.pending[0] = nil
		q.pending = q.pending[1:]
		if len(q.pending) == 0 {
			q.pending = nil
		}
		q.mu.Unlock()

		select {
		case ch <- ev:
		case <-q.stopCh:
			return
		}
	}
}

func (q *eventQueue) stop() {
	q.mu.Lock()
	q.closed = true
	close(q.stopCh)
	clear(q.pending)
	q.pending = nil
	q.ready.Signal()
	q.mu.Unlock()
	<-q.done
}
