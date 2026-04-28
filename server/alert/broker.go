package alert

import "sync"

// Broker fans alert events out to in-process subscribers (notably the SSE
// handler that surfaces toasts on the frontend). Slow subscribers have their
// events dropped rather than blocking publishing.
type Broker struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: map[chan Event]struct{}{}}
}

// Publish is non-blocking: a full subscriber channel drops the event.
func (b *Broker) Publish(e Event) {
	b.mu.RLock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
	b.mu.RUnlock()
}

// Subscribe returns a receive channel. The unsubscribe fn removes the
// channel from the map under the write lock before closing it, so Publish
// (holding RLock) cannot send on a closed channel.
func (b *Broker) Subscribe(buf int) (<-chan Event, func()) {
	ch := make(chan Event, buf)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}
