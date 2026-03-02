// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"testing"
)

func TestDependencyTracking(t *testing.T) {
	state := NewStateManager()
	engine := NewGoalEngine(state)

	var evaluated int
	goal := Goal{
		ID:       "goal-1",
		Priority: 1,
		State:    GoalStatePending,
		Condition: func(s State) bool {
			evaluated++
			v, ok := s.Get("order.status")
			if ok && v.Value == "shipped" {
				return true
			}
			return false
		},
	}

	engine.AddGoal(goal)
	engine.Evaluate(engine.goals["goal-1"], state)

	event := Event{
		ID:         1,
		WritePaths: []Path{"order.status"},
	}

	engine.mu.Lock()
	g := engine.goals["goal-1"]
	g.State = GoalStatePending
	engine.goals["goal-1"] = g
	engine.removeFromSlices("goal-1")
	engine.pendingGoals = append(engine.pendingGoals, "goal-1")
	engine.mu.Unlock()

	engine.HandleEvent(event)

	activeGoals := engine.ListActiveGoals()
	if len(activeGoals) == 0 || activeGoals[0].ID != "goal-1" {
		t.Fatalf("goal should become active due to dependency trigger")
	}
}

func TestLateArrivingDataCorrectionAndSaga(t *testing.T) {
	state := NewStateManager()
	engine := NewGoalEngine(state)

	state.Set("order.status", TypedValue{Type: "string", Value: "shipped"})

	var compensated bool

	goal := Goal{
		ID:       "goal-1",
		Priority: 1,
		State:    GoalStatePending,
		Condition: func(s State) bool {
			v, ok := s.Get("order.status")
			if ok && v.Value == "shipped" {
				return true
			}
			return false
		},
		Compensate: func(s State) *Goal {
			compensated = true
			return &Goal{
				ID: "comp-goal-1",
				Condition: func(cs State) bool { return true },
			}
		},
	}

	engine.AddGoal(goal)
	result := engine.Evaluate(engine.goals["goal-1"], state)
	if !result {
		t.Fatalf("goal should be complete initially")
	}

	engine.CompleteGoal("goal-1")

	event := Event{
		ID:         2,
		WritePaths: []Path{"order.status"},
		StateMutator: func(s State) error {
			return s.Set("order.status", TypedValue{Type: "string", Value: "cancelled"})
		},
	}

	state.ApplyEvent(event)

	engine.HandleEvent(event)

	if !compensated {
		t.Fatalf("expected compensation to be triggered")
	}

	g, ok := engine.GetGoal("goal-1")
	if !ok || g.State != GoalStateCompensating {
		t.Fatalf("expected goal state to be compensating, got %v", g.State)
	}

	_, ok = engine.GetGoal("comp-goal-1")
	if !ok {
		t.Fatalf("expected compensation goal to be spawned")
	}
}
