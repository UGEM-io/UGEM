// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrSchedulerNotRunning = errors.New("scheduler not running")
	ErrItemNotFound        = errors.New("scheduled item not found")
)

type SchedulerImpl struct {
	mu            sync.RWMutex
	items         map[string]*ScheduledItem
	priorityQueue []*ScheduledItem
	clock         LogicalClock
	goalEngine    GoalEngine
	eventLog      EventLog
	stopCh        chan struct{}
	running       bool
	wg            sync.WaitGroup
	mode          SchedulerMode
}

func NewScheduler(goalEngine GoalEngine, eventLog EventLog, mode SchedulerMode) *SchedulerImpl {
	return &SchedulerImpl{
		items:         make(map[string]*ScheduledItem),
		priorityQueue: make([]*ScheduledItem, 0),
		goalEngine:    goalEngine,
		eventLog:      eventLog,
		running:       false,
		mode:          mode,
	}
}

func (s *SchedulerImpl) Schedule(goals []Goal, events []Event, timers []TimerTrigger) []ScheduledItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]ScheduledItem, 0)

	for _, goal := range goals {
		var executeAt time.Time
		if s.mode == SchedulerModeStrict {
			// In strict mode, time is derived from the logical clock
			executeAt = time.Unix(int64(s.clock), 0)
		} else {
			executeAt = time.Now()
			if goal.Deadline.IsZero() == false {
				executeAt = goal.Deadline.Add(-goal.Timeout)
				if executeAt.Before(time.Now()) {
					executeAt = time.Now()
				}
			}
		}

		item := ScheduledItem{
			ID:        goal.ID,
			GoalID:    goal.ID,
			Priority:  goal.Priority,
			Clock:     s.clock,
			ExecuteAt: executeAt,
			Deadline:  goal.Deadline,
			Type:      "goal",
		}
		items = append(items, item)
		s.items[goal.ID] = &item
		s.insertIntoQueue(&item)
	}

	for _, event := range events {
		var executeAt time.Time
		if s.mode == SchedulerModeStrict {
			executeAt = time.Unix(int64(event.Clock), 0)
		} else {
			executeAt = time.Now()
		}

		item := ScheduledItem{
			ID:        fmt.Sprintf("event_%d", event.ID),
			GoalID:    "",
			Priority:  -1,
			Clock:     event.Clock,
			ExecuteAt: executeAt,
			Type:      "event",
		}
		items = append(items, item)
		s.items[item.ID] = &item
		s.insertIntoQueue(&item)
	}

	for _, timer := range timers {
		item := ScheduledItem{
			ID:        timer.ID,
			GoalID:    timer.GoalID,
			Priority:  100,
			Clock:     s.clock,
			ExecuteAt: timer.ExecuteAt,
			Type:      "timer",
		}
		items = append(items, item)
		s.items[timer.ID] = &item
		s.insertIntoQueue(&item)
	}

	s.clock++

	sort.Slice(s.priorityQueue, func(i, j int) bool {
		return s.compareItems(s.priorityQueue[i], s.priorityQueue[j]) < 0
	})

	return items
}

func (s *SchedulerImpl) compareItems(a, b *ScheduledItem) int {
	if !a.Deadline.IsZero() && !b.Deadline.IsZero() {
		if a.Deadline.Before(b.Deadline) {
			return -1
		}
		if a.Deadline.After(b.Deadline) {
			return 1
		}
	} else if !a.Deadline.IsZero() {
		return -1
	} else if !b.Deadline.IsZero() {
		return 1
	}

	if a.ExecuteAt.Before(b.ExecuteAt) {
		return -1
	}
	if a.ExecuteAt.After(b.ExecuteAt) {
		return 1
	}

	if a.Priority != b.Priority {
		if a.Priority > b.Priority {
			return -1
		}
		return 1
	}

	if a.Clock != b.Clock {
		if a.Clock < b.Clock {
			return -1
		}
		return 1
	}

	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}

	return 0
}

func (s *SchedulerImpl) insertIntoQueue(item *ScheduledItem) {
	s.priorityQueue = append(s.priorityQueue, item)
}

func (s *SchedulerImpl) Enqueue(goal Goal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var executeAt time.Time
	if s.mode == SchedulerModeStrict {
		executeAt = time.Unix(int64(s.clock), 0)
	} else {
		executeAt = time.Now()
	}

	item := ScheduledItem{
		ID:        goal.ID,
		GoalID:    goal.ID,
		Priority:  goal.Priority,
		Clock:     s.clock,
		ExecuteAt: executeAt,
		Type:      "goal",
	}

	s.items[goal.ID] = &item
	s.priorityQueue = append(s.priorityQueue, &item)

	s.sortQueue()
}

func (s *SchedulerImpl) Dequeue(id string) *ScheduledItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists := s.items[id]
	if !exists {
		return nil
	}

	for i, qitem := range s.priorityQueue {
		if qitem.ID == id {
			s.priorityQueue = append(s.priorityQueue[:i], s.priorityQueue[i+1:]...)
			break
		}
	}

	delete(s.items, id)

	return item
}

func (s *SchedulerImpl) Next() *ScheduledItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.priorityQueue) == 0 {
		return nil
	}

	now := time.Now()

	for _, item := range s.priorityQueue {
		if item.ExecuteAt.After(now) || item.ExecuteAt.Equal(now) {
			return item
		}
	}

	return nil
}

func (s *SchedulerImpl) Peek() *ScheduledItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.priorityQueue) == 0 {
		return nil
	}

	return s.priorityQueue[0]
}

func (s *SchedulerImpl) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

func (s *SchedulerImpl) sortQueue() {
	sort.Slice(s.priorityQueue, func(i, j int) bool {
		return s.compareItems(s.priorityQueue[i], s.priorityQueue[j]) < 0
	})
}

func (s *SchedulerImpl) Run() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.runLoop()

	return nil
}

func (s *SchedulerImpl) runLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.processScheduledItems()
		}
	}
}

func (s *SchedulerImpl) processScheduledItems() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var now time.Time
	if s.mode == SchedulerModeStrict {
		now = time.Unix(int64(s.clock), 0)
	} else {
		now = time.Now()
	}
	readyItems := make([]*ScheduledItem, 0)

	for _, item := range s.priorityQueue {
		if item.ExecuteAt.After(now) {
			continue
		}
		readyItems = append(readyItems, item)
	}

	for _, item := range readyItems {
		for i, qitem := range s.priorityQueue {
			if qitem.ID == item.ID {
				s.priorityQueue = append(s.priorityQueue[:i], s.priorityQueue[i+1:]...)
				break
			}
		}
	}
}

func (s *SchedulerImpl) DequeueReady() []*ScheduledItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.mode == SchedulerModeStrict {
		now = time.Unix(int64(s.clock), 0)
	}

	readyItems := make([]*ScheduledItem, 0)
	remaining := make([]*ScheduledItem, 0)

	for _, item := range s.priorityQueue {
		if !item.ExecuteAt.After(now) {
			readyItems = append(readyItems, item)
			delete(s.items, item.ID)
		} else {
			remaining = append(remaining, item)
		}
	}

	s.priorityQueue = remaining
	return readyItems
}

func (s *SchedulerImpl) Stop() error {
	s.mu.Lock()

	s.running = false
	if s.stopCh != nil {
		close(s.stopCh)
	}
	s.mu.Unlock()

	s.wg.Wait()

	s.mu.Lock()
	s.running = false
	s.stopCh = nil
	s.mu.Unlock()

	return nil
}

func (s *SchedulerImpl) GetScheduledItem(id string) (*ScheduledItem, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, exists := s.items[id]
	return item, exists
}

func (s *SchedulerImpl) ListItems() []ScheduledItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]ScheduledItem, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, *item)
	}

	sort.Slice(items, func(i, j int) bool {
		return s.compareItems(&items[i], &items[j]) < 0
	})

	return items
}

func (s *SchedulerImpl) GetClock() LogicalClock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clock
}

func (s *SchedulerImpl) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
