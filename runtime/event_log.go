// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"errors"
	"sync"
	"time"

	"github.com/ugem-io/ugem/storage"
)

var (
	ErrEventNotFound  = errors.New("event not found")
	ErrInvalidEventID = errors.New("invalid event ID")
	ErrInvalidRange   = errors.New("invalid event range")
)

type EventLogImpl struct {
	mu     sync.RWMutex
	events []Event
	clock  LogicalClock
	nextID EventID
	pss    *storage.PersistentStore
}

func NewEventLog() *EventLogImpl {
	return &EventLogImpl{
		events: make([]Event, 0),
		clock:  0,
		nextID: 1,
	}
}

func (e *EventLogImpl) SetPersistence(pss *storage.PersistentStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pss = pss

	if pss != nil {
		storageEvents, err := pss.WAL.Replay()
		if err == nil {
			e.events = make([]Event, 0, len(storageEvents))
			for _, se := range storageEvents {
				event := Event{
					ID:        EventID(se.ID),
					Clock:     LogicalClock(se.Clock),
					Timestamp: se.Timestamp,
					Type:      se.Type,
					Payload:   se.Payload,
					Trace: TraceContext{
						TraceID:       se.Trace.TraceID,
						ParentEventID: se.Trace.ParentEventID,
						GoalID:        se.Trace.GoalID,
						ActionID:      se.Trace.ActionID,
					},
				}
				for _, p := range se.WritePaths {
					event.WritePaths = append(event.WritePaths, Path(p))
				}
				for _, p := range se.ReadPaths {
					event.ReadPaths = append(event.ReadPaths, Path(p))
				}
				for _, m := range se.Mutations {
					event.StateMutations = append(event.StateMutations, StateMutation{
						Path: Path(m.Path),
						Value: TypedValue{
							Type:  m.Value.Type,
							Value: m.Value.Value,
						},
					})
				}
				e.events = append(e.events, event)
			}
			if len(e.events) > 0 {
				lastEvent := e.events[len(e.events)-1]
				e.nextID = lastEvent.ID + 1
				e.clock = lastEvent.Clock
			}
		}
	}
}

func (e *EventLogImpl) Append(event Event) (EventID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	event.ID = e.nextID
	event.Clock = e.clock + 1
	event.Timestamp = time.Now()

	if e.pss != nil {
		storageEvent := storage.Event{
			Type:       event.Type,
			Payload:    event.Payload,
			WritePaths: make([]storage.Path, len(event.WritePaths)),
			ReadPaths:  make([]storage.Path, len(event.ReadPaths)),
			Trace: storage.TraceContext{
				TraceID:       event.Trace.TraceID,
				ParentEventID: event.Trace.ParentEventID,
				GoalID:        event.Trace.GoalID,
				ActionID:      event.Trace.ActionID,
			},
		}
		for i, p := range event.WritePaths {
			storageEvent.WritePaths[i] = storage.Path(p)
		}
		for i, p := range event.ReadPaths {
			storageEvent.ReadPaths[i] = storage.Path(p)
		}
		for _, m := range event.StateMutations {
			storageEvent.Mutations = append(storageEvent.Mutations, storage.Mutation{
				Path: storage.Path(m.Path),
				Value: storage.TypedValue{
					Type:  m.Value.Type,
					Value: m.Value.Value,
				},
			})
		}

		id, err := e.pss.WAL.Append(storageEvent)
		if err != nil {
			return 0, err
		}
		event.ID = EventID(id)
	}

	e.events = append(e.events, event)
	e.nextID = event.ID + 1
	e.clock = event.Clock

	return event.ID, nil
}

func (e *EventLogImpl) Get(id EventID) (Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if id == 0 || id >= e.nextID {
		return Event{}, ErrEventNotFound
	}

	return e.events[id-1], nil
}

func (e *EventLogImpl) Range(start, end EventID) ([]Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if start > end {
		return nil, ErrInvalidRange
	}

	if start == 0 {
		start = 1
	}

	if end >= e.nextID {
		end = e.nextID - 1
	}

	if start > end {
		return []Event{}, nil
	}

	result := make([]Event, end-start+1)
	copy(result, e.events[start-1:end])

	return result, nil
}

func (e *EventLogImpl) Length() EventID {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.nextID - 1
}

func (e *EventLogImpl) GetClock() LogicalClock {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.clock
}

func (e *EventLogImpl) Replay() (<-chan Event, error) {
	return e.ReplayUntil(time.Time{})
}

func (e *EventLogImpl) ReplayUntil(timestamp time.Time) (<-chan Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	events := make([]Event, 0)
	for _, event := range e.events {
		if !timestamp.IsZero() && event.Timestamp.After(timestamp) {
			break
		}
		events = append(events, event)
	}

	ch := make(chan Event, len(events))
	go func() {
		for _, event := range events {
			ch <- event
		}
		close(ch)
	}()

	return ch, nil
}

func (e *EventLogImpl) GetEvents() []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]Event, len(e.events))
	copy(result, e.events)

	return result
}
