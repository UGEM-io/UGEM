// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"errors"
	"sync"
)

var (
	ErrSubscriberNotFound = errors.New("subscriber not found")
	ErrInvalidEvent       = errors.New("invalid event")

	maxEventHistory = 10000
)

type subscriber struct {
	id         string
	eventTypes []string
	handler    func(Event)
	stopCh     chan struct{}
}

type EventBusImpl struct {
	mu          sync.RWMutex
	subscribers map[string]*subscriber
	eventQueue  chan Event
	events      []Event
	clock       LogicalClock
	stopCh      chan struct{}
	wg          sync.WaitGroup
	running     bool
}

func NewEventBus() *EventBusImpl {
	return &EventBusImpl{
		subscribers: make(map[string]*subscriber),
		eventQueue:  make(chan Event, 10000),
		events:      make([]Event, 0),
		running:     false,
	}
}

func (eb *EventBusImpl) Subscribe(subscriberID string, eventTypes []string, handler func(Event)) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if _, exists := eb.subscribers[subscriberID]; exists {
		return nil
	}

	sub := &subscriber{
		id:         subscriberID,
		eventTypes: eventTypes,
		handler:    handler,
		stopCh:     make(chan struct{}),
	}

	eb.subscribers[subscriberID] = sub

	return nil
}

func (eb *EventBusImpl) Unsubscribe(subscriberID string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	sub, exists := eb.subscribers[subscriberID]
	if !exists {
		return ErrSubscriberNotFound
	}

	close(sub.stopCh)
	delete(eb.subscribers, subscriberID)

	return nil
}

func (eb *EventBusImpl) Publish(event Event) error {
	eb.mu.Lock()
	if !eb.running {
		eb.mu.Unlock()
		return nil
	}
	eb.events = append(eb.events, event)
	// Evict oldest 20% when exceeding cap
	if len(eb.events) > maxEventHistory {
		evictCount := maxEventHistory / 5
		eb.events = eb.events[evictCount:]
	}
	eb.mu.Unlock()

	// Deliver directly to matching subscribers
	eb.deliverEvent(event)

	return nil
}

func (eb *EventBusImpl) shouldDeliver(sub *subscriber, event Event) bool {
	if len(sub.eventTypes) == 0 {
		return true
	}

	for _, eventType := range sub.eventTypes {
		if eventType == event.Type {
			return true
		}
	}

	return false
}

func (eb *EventBusImpl) Start() error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.running {
		return nil
	}

	eb.running = true
	eb.stopCh = make(chan struct{})

	eb.wg.Add(1)
	go eb.eventLoop()

	return nil
}

func (eb *EventBusImpl) Stop() error {
	eb.mu.Lock()

	if !eb.running {
		eb.mu.Unlock()
		return nil
	}
	if eb.stopCh != nil {
		close(eb.stopCh)
	}
	eb.mu.Unlock()

	eb.wg.Wait()

	eb.mu.Lock()
	eb.running = false
	eb.stopCh = nil
	eb.mu.Unlock()

	return nil
}

func (eb *EventBusImpl) eventLoop() {
	defer eb.wg.Done()

	for {
		select {
		case <-eb.stopCh:
			return
		case event := <-eb.eventQueue:
			eb.deliverEvent(event)
		}
	}
}

func (eb *EventBusImpl) deliverEvent(event Event) {
	eb.mu.RLock()
	subs := make([]*subscriber, 0, len(eb.subscribers))
	for _, sub := range eb.subscribers {
		subs = append(subs, sub)
	}
	eb.mu.RUnlock()

	for _, sub := range subs {
		if eb.shouldDeliver(sub, event) {
			select {
			case <-sub.stopCh:
			default:
				go sub.handler(event)
			}
		}
	}
}

func (eb *EventBusImpl) GetEvent(id EventID) (Event, error) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, e := range eb.events {
		if e.ID == id {
			return e, nil
		}
	}
	return Event{}, ErrInvalidEvent
}

func (eb *EventBusImpl) Replay(fromEventID EventID) (<-chan Event, error) {
	eb.mu.RLock()
	events := make([]Event, 0)
	for _, e := range eb.events {
		if e.ID >= fromEventID {
			events = append(events, e)
		}
	}
	eb.mu.RUnlock()

	ch := make(chan Event, len(events))
	go func() {
		for _, e := range events {
			ch <- e
		}
		close(ch)
	}()

	return ch, nil
}

func (eb *EventBusImpl) ListSubscribers() []string {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	result := make([]string, 0, len(eb.subscribers))
	for id := range eb.subscribers {
		result = append(result, id)
	}

	return result
}

func (eb *EventBusImpl) GetSubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return len(eb.subscribers)
}

func (eb *EventBusImpl) IsRunning() bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return eb.running
}

func (eb *EventBusImpl) GetClock() LogicalClock {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return eb.clock
}

func (eb *EventBusImpl) AdvanceClock() LogicalClock {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.clock++
	return eb.clock
}

func (eb *EventBusImpl) PendingEvents() int {
	return len(eb.eventQueue)
}
