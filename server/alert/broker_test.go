package alert

import (
	"testing"
	"time"
)

func testEvent() Event {
	return Event{
		AgentID:  "agent1",
		Hostname: "myhost",
		Metric:   MetricCPU,
	}
}

func TestSubscribe_Publish_DeliversEvent(t *testing.T) {
	b := NewBroker()
	ch, unsub := b.Subscribe(1)
	defer unsub()

	b.Publish(testEvent())
	select {
	case e := <-ch:
		if e.AgentID != "agent1" {
			t.Errorf("expected agent1, got %s", e.AgentID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received within timeout")
	}
}

func TestPublish_NoSubscribers_DoesNotBlock(t *testing.T) {
	b := NewBroker()
	done := make(chan struct{})
	go func() {
		b.Publish(testEvent())
		close(done)
	}()
	select {
	case <-done:
		// pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Publish blocked with no subscribers")
	}
}

func TestUnsubscribe_StopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, unsub := b.Subscribe(1)
	unsub() // unsubscribe before publishing

	b.Publish(testEvent())
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received event after unsubscribe")
		}
		// channel was closed — that's expected
	default:
		// nothing in channel — that's also expected
	}
}

func TestMultipleSubscribers_EachReceivesEvent(t *testing.T) {
	b := NewBroker()
	const n = 3
	channels := make([]<-chan Event, n)
	unsubs := make([]func(), n)
	for i := 0; i < n; i++ {
		ch, unsub := b.Subscribe(1)
		channels[i] = ch
		unsubs[i] = unsub
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	b.Publish(testEvent())

	for i, ch := range channels {
		select {
		case e := <-ch:
			if e.AgentID != "agent1" {
				t.Errorf("subscriber %d: expected agent1, got %s", i, e.AgentID)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d did not receive event", i)
		}
	}
}
