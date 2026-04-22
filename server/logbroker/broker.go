// Package logbroker fans live log lines out to in-process subscribers.
// Each (agentID, containerName) key has its own channel list; slow
// subscribers are dropped rather than blocking ingestion.
package logbroker

import (
	"sync"

	"github.com/technonext/chowkidar/server/logstore"
)

type subscriber struct {
	ch chan logstore.Line
}

type Broker struct {
	mu   sync.RWMutex
	subs map[string]map[*subscriber]struct{}
}

func New() *Broker {
	return &Broker{subs: map[string]map[*subscriber]struct{}{}}
}

func key(agentID, name string) string { return agentID + "/" + name }

// Publish is non-blocking: if a subscriber's channel is full, its line is
// dropped so ingestion is never stalled by a slow viewer.
func (b *Broker) Publish(agentID string, l logstore.Line) {
	k := key(agentID, l.ContainerName)
	b.mu.RLock()
	subs := b.subs[k]
	for s := range subs {
		select {
		case s.ch <- l:
		default:
		}
	}
	b.mu.RUnlock()
}

// Subscribe returns a channel that receives live lines for the given
// (agent, container). Caller must invoke the returned unsubscribe fn when done.
func (b *Broker) Subscribe(agentID, name string, buf int) (<-chan logstore.Line, func()) {
	k := key(agentID, name)
	s := &subscriber{ch: make(chan logstore.Line, buf)}

	b.mu.Lock()
	if b.subs[k] == nil {
		b.subs[k] = map[*subscriber]struct{}{}
	}
	b.subs[k][s] = struct{}{}
	b.mu.Unlock()

	return s.ch, func() {
		b.mu.Lock()
		if set, ok := b.subs[k]; ok {
			delete(set, s)
			if len(set) == 0 {
				delete(b.subs, k)
			}
		}
		b.mu.Unlock()
		close(s.ch)
	}
}
