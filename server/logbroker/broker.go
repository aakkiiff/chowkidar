// Package logbroker fans live log lines out to in-process subscribers.
// Slow subscribers have their lines dropped rather than blocking ingestion.
package logbroker

import (
	"sync"

	"github.com/technonext/chowkidar/server/logstore"
)

type Broker struct {
	mu   sync.RWMutex
	subs map[string]map[chan logstore.Line]struct{}
}

func New() *Broker {
	return &Broker{subs: map[string]map[chan logstore.Line]struct{}{}}
}

func key(agentID, name string) string { return agentID + "/" + name }

// Publish is non-blocking: a full subscriber channel drops the line.
func (b *Broker) Publish(agentID string, l logstore.Line) {
	k := key(agentID, l.ContainerName)
	b.mu.RLock()
	for ch := range b.subs[k] {
		select {
		case ch <- l:
		default:
		}
	}
	b.mu.RUnlock()
}

// Subscribe returns a receive channel for live lines. The unsubscribe fn
// removes the channel from the map under write lock before closing it, so
// Publish (holding RLock) cannot send on a closed channel.
func (b *Broker) Subscribe(agentID, name string, buf int) (<-chan logstore.Line, func()) {
	k := key(agentID, name)
	ch := make(chan logstore.Line, buf)

	b.mu.Lock()
	if b.subs[k] == nil {
		b.subs[k] = map[chan logstore.Line]struct{}{}
	}
	b.subs[k][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if set, ok := b.subs[k]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, k)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
}
