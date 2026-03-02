// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestStrictDeterminism(t *testing.T) {
	rt := NewRuntime(SchedulerModeStrict)
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	// In strict mode, ExecuteAt should be derived from clock
	goal := Goal{ID: "det_goal", Priority: 5}
	rt.SubmitGoal(goal)

	item, ok := rt.sched.GetScheduledItem("det_goal")
	if !ok {
		t.Fatal("goal not scheduled")
	}

	expectedTime := time.Unix(0, 0)
	if !item.ExecuteAt.Equal(expectedTime) {
		t.Errorf("Expected executeAt %v, got %v", expectedTime, item.ExecuteAt)
	}
}

func TestCausalityTracking(t *testing.T) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.Start()
	defer rt.Stop()

	goal := Goal{ID: "parent_goal"}
	rt.SubmitGoal(goal)

	// Check if Trace was initialized
	g, _ := rt.goalEngine.GetGoal("parent_goal")
	traceID := g.Trace.TraceID
	if traceID == "" {
		t.Fatal("TraceID should not be empty")
	}

	// Trigger an action (mock)
	rt.GetPlanner().RegisterAction("test_action", func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error) {
		if ctx.Trace.TraceID != traceID {
			return nil, fmt.Errorf("trace mismatch: expected %s, got %s", traceID, ctx.Trace.TraceID)
		}
		return map[string]interface{}{"ok": true}, nil
	})

	// Manually trigger action for the goal logic
	plan := &Plan{Actions: []Action{{ID: "act1", Type: "test_action"}}}
	rt.executePlan(plan, g)

	time.Sleep(50 * time.Millisecond)

	// Resulting event should have the same TraceID
	found := false
	ch, _ := rt.eventLog.Replay()
	for e := range ch {
		if e.Type == "test_action_result" {
			if e.Trace.TraceID != traceID {
				t.Fatalf("Event trace ID mismatch: %s != %s", e.Trace.TraceID, traceID)
			}
			found = true
		}
	}

	if !found {
		t.Fatal("Action result event not found")
	}
}

func TestInvariantEngine(t *testing.T) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.Start()
	defer rt.Stop()

	balancePath := Path("user.balance")
	rt.stateManager.RegisterInvariant(Invariant{
		Name: "positive_balance",
		Predicate: func(s State) error {
			val, ok := s.Get(balancePath)
			if ok && val.Value.(int) < 0 {
				return errors.New("balance cannot be negative")
			}
			return nil
		},
	})

	// Valid event
	err := rt.SubmitEvent(Event{
		Type: "deposit",
		StateMutator: func(s State) error {
			s.Set(balancePath, TypedValue{Type: "int", Value: 100})
			return nil
		},
	})
	if err != nil {
		t.Errorf("Valid event rejected: %v", err)
	}

	// Invalid event
	err = rt.SubmitEvent(Event{
		Type: "withdraw",
		StateMutator: func(s State) error {
			s.Set(balancePath, TypedValue{Type: "int", Value: -50})
			return nil
		},
	})
	if err == nil {
		t.Fatal("Invalid event should have been rejected by invariant")
	}
	t.Logf("Correctly rejected: %v", err)
}

func TestExternalWorker(t *testing.T) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.RegisterExternalWorker("email.send", "http://localhost:8080")
	rt.Start()
	defer rt.Stop()

	goal := Goal{ID: "email_goal"}
	rt.SubmitGoal(goal)
	g, _ := rt.goalEngine.GetGoal("email_goal")

	plan := &Plan{Actions: []Action{{ID: "msg1", Type: "email.send"}}}
	rt.executePlan(plan, g)

	time.Sleep(100 * time.Millisecond)

	// Check if event was produced by mock worker
	ch, _ := rt.eventLog.Replay()
	found := false
	for e := range ch {
		if e.Type == "email.send_result" {
			if e.Payload["status"] != "completed" {
				t.Errorf("Unexpected status: %v", e.Payload["status"])
			}
			found = true
		}
	}

	if !found {
		t.Fatal("External worker result event not found")
	}
}
