// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrGoalNotFound      = errors.New("goal not found")
	ErrInvalidGoal       = errors.New("invalid goal")
	ErrGoalAlreadyExists = errors.New("goal already exists")
)

type GoalEngineImpl struct {
	mu           sync.RWMutex
	goals        map[string]Goal
	activeGoals  []string
	pendingGoals []string
	clock        LogicalClock
	stateManager StateManager
	eventCh      chan Event
	stopCh       chan struct{}
	wg               sync.WaitGroup
	running          bool
	goalDependencies map[string]map[Path]bool
}

func NewGoalEngine(stateManager StateManager) *GoalEngineImpl {
	return &GoalEngineImpl{
		goals:        make(map[string]Goal),
		activeGoals:  make([]string, 0),
		pendingGoals:     make([]string, 0),
		stateManager:     stateManager,
		eventCh:          make(chan Event, 100),
		goalDependencies: make(map[string]map[Path]bool),
	}
}

func (g *GoalEngineImpl) AddGoal(goal Goal) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if goal.ID == "" {
		return ErrInvalidGoal
	}

	if _, exists := g.goals[goal.ID]; exists {
		return ErrGoalAlreadyExists
	}

	goal.State = GoalStateActive
	goal.Clock = g.clock
	now := time.Now()
	goal.StartedAt = &now

	g.goals[goal.ID] = goal
	g.activeGoals = append(g.activeGoals, goal.ID)

	return nil
}

func (g *GoalEngineImpl) RemoveGoal(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	goal, exists := g.goals[id]
	if !exists {
		return ErrGoalNotFound
	}

	goal.State = GoalStateFailed
	g.goals[id] = goal

	g.removeFromSlices(id)

	delete(g.goals, id)

	return nil
}

func (g *GoalEngineImpl) removeFromSlices(id string) {
	for i, gid := range g.activeGoals {
		if gid == id {
			g.activeGoals = append(g.activeGoals[:i], g.activeGoals[i+1:]...)
			break
		}
	}

	for i, gid := range g.pendingGoals {
		if gid == id {
			g.pendingGoals = append(g.pendingGoals[:i], g.pendingGoals[i+1:]...)
			break
		}
	}
}

func (g *GoalEngineImpl) GetGoal(id string) (Goal, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	goal, exists := g.goals[id]
	return goal, exists
}

func (g *GoalEngineImpl) ListGoals() []Goal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]Goal, 0, len(g.goals))
	for _, goal := range g.goals {
		result = append(result, goal)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].Clock < result[j].Clock
	})

	return result
}

func (g *GoalEngineImpl) ListActiveGoals() []Goal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]Goal, 0, len(g.activeGoals))
	for _, id := range g.activeGoals {
		result = append(result, g.goals[id])
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].Clock < result[j].Clock
	})

	return result
}

func (g *GoalEngineImpl) ListPendingGoals() []Goal {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]Goal, 0, len(g.pendingGoals))
	for _, id := range g.pendingGoals {
		result = append(result, g.goals[id])
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].Clock < result[j].Clock
	})

	return result
}

func (g *GoalEngineImpl) Evaluate(goal Goal, state State) bool {
	if goal.Condition == nil {
		return false
	}
	
	tracker := NewStateTracker(state)
	result := goal.Condition(tracker)
	
	paths := tracker.GetAccessedPaths()
	
	g.mu.Lock()
	if g.goalDependencies == nil {
		g.goalDependencies = make(map[string]map[Path]bool)
	}
	g.goalDependencies[goal.ID] = make(map[Path]bool)
	for _, p := range paths {
		g.goalDependencies[goal.ID][p] = true
	}
	g.mu.Unlock()
	
	return result
}

func (g *GoalEngineImpl) SpawnChildren(goal Goal, state State) []Goal {
	if goal.Spawn == nil {
		return nil
	}

	children := goal.Spawn(state)

	g.mu.Lock()
	defer g.mu.Unlock()

	for i := range children {
		children[i].ParentID = &goal.ID
		children[i].Clock = g.clock
		children[i].State = GoalStatePending
		children[i].Trace = goal.Trace
		children[i].Trace.GoalID = children[i].ID

		g.goals[children[i].ID] = children[i]
		g.pendingGoals = append(g.pendingGoals, children[i].ID)
	}

	g.clock++

	return children
}

func (g *GoalEngineImpl) ActivateGoal(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	goal, exists := g.goals[id]
	if !exists {
		return ErrGoalNotFound
	}

	goal.State = GoalStateActive
	g.goals[id] = goal

	g.removeFromSlices(id)
	g.activeGoals = append(g.activeGoals, id)

	return nil
}

func (g *GoalEngineImpl) CompleteGoal(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	goal, exists := g.goals[id]
	if !exists {
		return ErrGoalNotFound
	}

	goal.State = GoalStateComplete
	g.goals[id] = goal

	g.removeFromSlices(id)

	return nil
}

func (g *GoalEngineImpl) FailGoal(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	goal, exists := g.goals[id]
	if !exists {
		return ErrGoalNotFound
	}

	goal.State = GoalStateFailed
	g.goals[id] = goal

	g.removeFromSlices(id)

	return nil
}

func (g *GoalEngineImpl) CancelGoal(id string, reason string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	goal, exists := g.goals[id]
	if !exists {
		return ErrGoalNotFound
	}

	goal.State = GoalStateCancelled
	goal.FailReason = reason
	now := time.Now()
	goal.CancelledAt = &now
	g.goals[id] = goal

	g.removeFromSlices(id)

	return nil
}

func (g *GoalEngineImpl) HandleEvent(event Event) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if event.WritePaths == nil {
		return
	}

	triggeredGoals := make([]string, 0)
	compensatingGoals := make([]string, 0)

	for id, goal := range g.goals {
		if goal.State == GoalStatePending && goal.Trigger.Path != nil {
			for _, path := range event.WritePaths {
				if *goal.Trigger.Path == path {
					triggeredGoals = append(triggeredGoals, id)
					break
				}
			}
			continue
		}
		
		deps := g.goalDependencies[id]
		if len(deps) > 0 {
			affected := false
			for _, path := range event.WritePaths {
				if deps[path] {
					affected = true
					break
				}
			}
			
			if affected {
				if goal.State == GoalStatePending {
					triggeredGoals = append(triggeredGoals, id)
				} else if goal.State == GoalStateComplete {
					compensatingGoals = append(compensatingGoals, id)
				}
			}
		}
	}

	for _, id := range triggeredGoals {
		g.removeFromSlices(id)
	}

	sort.Strings(triggeredGoals)

	for _, id := range triggeredGoals {
		g.activeGoals = append(g.activeGoals, id)

		goal := g.goals[id]
		goal.State = GoalStateActive
		g.goals[id] = goal
	}

	for _, id := range compensatingGoals {
		goal := g.goals[id]
		tracker := NewStateTracker(g.stateManager)
		stillSatisfied := false
		if goal.Condition != nil {
			stillSatisfied = goal.Condition(tracker)
		}
		
		if !stillSatisfied {
			goal.State = GoalStateCompensating
			g.goals[id] = goal
			
			if goal.Compensate != nil {
				compGoal := goal.Compensate(g.stateManager)
				if compGoal != nil {
					compGoal.ParentID = &goal.ID
					compGoal.Clock = g.clock
					compGoal.State = GoalStatePending
					if compGoal.ID != "" {
						g.goals[compGoal.ID] = *compGoal
						g.pendingGoals = append(g.pendingGoals, compGoal.ID)
					}
				}
			}
		}
	}
}

func (g *GoalEngineImpl) GetClock() LogicalClock {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.clock
}

func (g *GoalEngineImpl) AdvanceClock() LogicalClock {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clock++
	return g.clock
}

func (g *GoalEngineImpl) Run() error {
	return nil
}

func (g *GoalEngineImpl) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	if g.stopCh != nil {
		close(g.stopCh)
	}
	g.mu.Unlock()
	g.wg.Wait()

	g.mu.Lock()
	g.running = false
	g.stopCh = nil
	g.mu.Unlock()
	return nil
}

func (g *GoalEngineImpl) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		return nil
	}
	g.running = true
	g.stopCh = make(chan struct{})
	return nil
}

func (g *GoalEngineImpl) GetEventChannel() chan Event {
	return g.eventCh
}
