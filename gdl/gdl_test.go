// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"testing"
	"time"

	"github.com/ugem-io/ugem/runtime"
)

var testGDL = `
type User
	id: uuid
	email: string

event user.created

action send_email

goal create_user
	priority: 10
	trigger: user.created
	actions: send_email
`

func TestGDLParser(t *testing.T) {
	prog, err := ParseGDL(testGDL)
	if err != nil {
		t.Fatalf("Failed to parse GDL: %v", err)
	}

	if len(prog.Types) != 1 {
		t.Errorf("Expected 1 type, got %d", len(prog.Types))
	}

	if len(prog.Events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(prog.Events))
	}

	if len(prog.Goals) != 1 {
		t.Errorf("Expected 1 goal, got %d", len(prog.Goals))
	}

	t.Logf("Parsed: %d types, %d events, %d goals", len(prog.Types), len(prog.Events), len(prog.Goals))
}

func TestGDLCompiler(t *testing.T) {
	compiled, err := CompileGDL(testGDL)
	if err != nil {
		t.Fatalf("Failed to compile GDL: %v", err)
	}

	if len(compiled.Events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(compiled.Events))
	}

	if len(compiled.Goals) != 1 {
		t.Errorf("Expected 1 goal, got %d", len(compiled.Goals))
	}

	if len(compiled.Actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(compiled.Actions))
	}

	t.Logf("Compiled: %d events, %d goals, %d actions", len(compiled.Events), len(compiled.Goals), len(compiled.Actions))
}

func TestGDLRuntime(t *testing.T) {
	compiled, err := CompileGDL(testGDL)
	if err != nil {
		t.Fatalf("Failed to compile GDL: %v", err)
	}

	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)

	RegisterAllActions(rt.GetPlanner())

	if err := rt.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer rt.Stop()

	if err := compiled.CreateRuntime(rt); err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	event := runtime.Event{
		Type:       "user.created",
		WritePaths: []runtime.Path{"user.id", "user.email"},
		StateMutator: func(s runtime.State) error {
			s.Set("user.id", runtime.TypedValue{Type: "uuid", Value: "u1"})
			s.Set("user.email", runtime.TypedValue{Type: "string", Value: "test@example.com"})
			return nil
		},
	}

	if err := rt.SubmitEvent(event); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	goals := rt.GetGoalEngine().ListGoals()
	t.Logf("Goals after event: %d", len(goals))

	for _, g := range goals {
		t.Logf("  - %s: %s", g.ID, g.State)
	}

	t.Log("GDL Runtime test PASSED")
}

func TestAdvancedGDL(t *testing.T) {
	advancedGDL := `
goal payment_system
	priority: 20
	condition: state.balance >= 100
	condition: state.user.verified == true
	spawn: notify_user when state.balance < 50
	spawn: alert_admin
	actions: [process.payment, audit.log]
`
	prog, err := ParseGDL(advancedGDL)
	if err != nil {
		t.Fatalf("Failed to parse advanced GDL: %v", err)
	}

	if len(prog.Goals) != 1 {
		t.Fatalf("Expected 1 goal, got %d", len(prog.Goals))
	}

	goal := prog.Goals[0]
	if len(goal.Condition) != 2 {
		t.Errorf("Expected 2 conditions, got %d", len(goal.Condition))
	}

	if len(goal.Spawns) != 2 {
		t.Errorf("Expected 2 spawns, got %d", len(goal.Spawns))
	}

	if len(goal.Actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(goal.Actions))
	}

	compiled, err := NewCompiler(prog).Compile()
	if err != nil {
		t.Fatalf("Failed to compile advanced GDL: %v", err)
	}

	// Mock state for verification
	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)
	state := rt.GetState()
	state.Set("balance", runtime.TypedValue{Type: "float", Value: 120.0})
	state.Set("user.verified", runtime.TypedValue{Type: "bool", Value: true})

	g := compiled.Goals[0]
	if !g.Condition(state) {
		t.Error("Goal condition should be true for balance=120 and verified=true")
	}

	state.Set("balance", runtime.TypedValue{Type: "float", Value: 80.0})
	if g.Condition(state) {
		t.Error("Goal condition should be false for balance=80")
	}

	// Verify spawns
	spawns := g.Spawn(state)
	if len(spawns) != 2 {
		t.Errorf("Expected 2 spawned goals, got %d", len(spawns))
	}

	t.Log("Advanced GDL verification PASSED")
}
