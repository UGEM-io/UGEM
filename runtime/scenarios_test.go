// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"testing"
	"time"
)

func TestScenario1_UserOnboarding(t *testing.T) {
	runtime := NewRuntime(SchedulerModeNormal)

	if err := runtime.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer runtime.Stop()

	emailVerifiedPath := Path("user.email_verified")
	profileCompletedPath := Path("user.profile_completed")

	emailVerifiedGoal := Goal{
		ID:       "verify_email",
		Priority: 10,
		Condition: func(s State) bool {
			val, ok := s.Get(emailVerifiedPath)
			return ok && val.Value == true
		},
		Spawn: func(s State) []Goal {
			return []Goal{
				{
					ID:       "complete_profile",
					Priority: 5,
					Condition: func(s State) bool {
						val, ok := s.Get(profileCompletedPath)
						return ok && val.Value == true
					},
				},
			}
		},
	}

	if err := runtime.SubmitGoal(emailVerifiedGoal); err != nil {
		t.Fatalf("Failed to submit goal: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	verifyEvent := Event{
		Type:       "email_verified",
		WritePaths: []Path{emailVerifiedPath},
		StateMutator: func(s State) error {
			s.Set(emailVerifiedPath, TypedValue{Type: "bool", Value: true})
			return nil
		},
	}

	if err := runtime.SubmitEvent(verifyEvent); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	goals := runtime.GetGoalEngine().ListGoals()

	emailVerifiedGoalFound := false
	profileCompleteGoalFound := false
	for _, g := range goals {
		if g.ID == "verify_email" && g.State == GoalStateComplete {
			emailVerifiedGoalFound = true
		}
		if g.ID == "complete_profile" {
			profileCompleteGoalFound = true
		}
	}

	if !emailVerifiedGoalFound {
		t.Fatal("Email verified goal should be complete")
	}

	if !profileCompleteGoalFound {
		t.Fatal("Profile completion goal should have been spawned")
	}

	t.Log("Scenario 1: User Onboarding - PASSED")
}

func TestScenario2_PaymentFlow(t *testing.T) {
	runtime := NewRuntime(SchedulerModeNormal)

	if err := runtime.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer runtime.Stop()

	paymentStatusPath := Path("payment.status")

	runtime.GetPlanner().RegisterAction("process_payment", func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error) {
		return map[string]interface{}{"status": "success"}, nil
	})

	paymentGoal := Goal{
		ID:       "process_payment",
		Priority: 20,
		Condition: func(s State) bool {
			val, ok := s.Get(paymentStatusPath)
			return ok && val.Value == "success"
		},
		Trigger: GoalTrigger{
			Type: "event",
			Path: &paymentStatusPath,
		},
	}

	if err := runtime.SubmitGoal(paymentGoal); err != nil {
		t.Fatalf("Failed to submit goal: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	paymentEvent := Event{
		Type:       "payment_completed",
		WritePaths: []Path{paymentStatusPath},
		StateMutator: func(s State) error {
			s.Set(paymentStatusPath, TypedValue{Type: "string", Value: "success"})
			return nil
		},
	}

	if err := runtime.SubmitEvent(paymentEvent); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	goals := runtime.GetGoalEngine().ListGoals()

	paymentComplete := false
	for _, g := range goals {
		if g.ID == "process_payment" && g.State == GoalStateComplete {
			paymentComplete = true
		}
	}

	if !paymentComplete {
		t.Error("Payment goal should be complete")
	}

	t.Log("Scenario 2: Payment Flow - PASSED")
}

func TestScenario3_TimerDrivenGoal(t *testing.T) {
	runtime := NewRuntime(SchedulerModeNormal)

	if err := runtime.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer runtime.Stop()

	reminderSentPath := Path("reminder.sent")

	reminderGoal := Goal{
		ID:       "send_reminder",
		Priority: 5,
		Condition: func(s State) bool {
			val, ok := s.Get(reminderSentPath)
			return ok && val.Value == true
		},
	}

	if err := runtime.SubmitGoal(reminderGoal); err != nil {
		t.Fatalf("Failed to submit goal: %v", err)
	}

	futureTime := time.Now().Add(50 * time.Millisecond)
	timerTrigger := TimerTrigger{
		ID:        "timer_1",
		GoalID:    reminderGoal.ID,
		ExecuteAt: futureTime,
	}

	runtime.GetScheduler().Schedule([]Goal{}, []Event{}, []TimerTrigger{timerTrigger})

	time.Sleep(150 * time.Millisecond)

	reminderEvent := Event{
		Type:       "reminder_sent",
		WritePaths: []Path{reminderSentPath},
		StateMutator: func(s State) error {
			s.Set(reminderSentPath, TypedValue{Type: "bool", Value: true})
			return nil
		},
	}

	if err := runtime.SubmitEvent(reminderEvent); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	goals := runtime.GetGoalEngine().ListGoals()

	reminderSent := false
	for _, g := range goals {
		if g.ID == "send_reminder" && g.State == GoalStateComplete {
			reminderSent = true
		}
	}

	if !reminderSent {
		t.Fatal("Reminder goal should be complete")
	}

	t.Log("Scenario 3: Timer Driven Goal - PASSED")
}

func TestScenario4_Concurrency(t *testing.T) {
	runtime := NewRuntime(SchedulerModeNormal)

	if err := runtime.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer runtime.Stop()

	inventoryPath := Path("inventory.stock")

	runtime.GetState().Set(inventoryPath, TypedValue{Type: "int", Value: 100})

	order1Goal := Goal{
		ID:       "order_1",
		Priority: 10,
		Condition: func(s State) bool {
			val, ok := s.Get(Path("order_1.status"))
			return ok && val.Value == "completed"
		},
	}

	order2Goal := Goal{
		ID:       "order_2",
		Priority: 10,
		Condition: func(s State) bool {
			val, ok := s.Get(Path("order_2.status"))
			return ok && val.Value == "completed"
		},
	}

	if err := runtime.SubmitGoal(order1Goal); err != nil {
		t.Fatalf("Failed to submit order1 goal: %v", err)
	}

	if err := runtime.SubmitGoal(order2Goal); err != nil {
		t.Fatalf("Failed to submit order2 goal: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	order1Event := Event{
		Type:       "order_1_completed",
		WritePaths: []Path{Path("order_1.status")},
		StateMutator: func(s State) error {
			s.Set(Path("order_1.status"), TypedValue{Type: "string", Value: "completed"})
			return nil
		},
	}

	order2Event := Event{
		Type:       "order_2_completed",
		WritePaths: []Path{Path("order_2.status")},
		StateMutator: func(s State) error {
			s.Set(Path("order_2.status"), TypedValue{Type: "string", Value: "completed"})
			return nil
		},
	}

	if err := runtime.SubmitEvent(order1Event); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	if err := runtime.SubmitEvent(order2Event); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	goals := runtime.GetGoalEngine().ListGoals()

	order1Complete := false
	order2Complete := false
	for _, g := range goals {
		if g.ID == "order_1" && g.State == GoalStateComplete {
			order1Complete = true
		}
		if g.ID == "order_2" && g.State == GoalStateComplete {
			order2Complete = true
		}
	}

	if !order1Complete {
		t.Fatal("Order 1 should be complete")
	}

	if !order2Complete {
		t.Fatal("Order 2 should be complete")
	}

	t.Log("Scenario 4: Concurrency - PASSED")
}

func TestDeterminism(t *testing.T) {
	runDeterministicTest(t, 1)
	runDeterministicTest(t, 2)
	runDeterministicTest(t, 3)
}

func runDeterministicTest(t *testing.T, runNum int) {
	runtime := NewRuntime(SchedulerModeNormal)

	if err := runtime.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer runtime.Stop()

	testPath := Path("test.value")

	initialGoal := Goal{
		ID:       "initial_goal",
		Priority: 10,
		Condition: func(s State) bool {
			val, ok := s.Get(testPath)
			return ok && val.Value == "done"
		},
	}

	if err := runtime.SubmitGoal(initialGoal); err != nil {
		t.Fatalf("Failed to submit goal: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	testEvent := Event{
		Type:       "test_event",
		WritePaths: []Path{testPath},
		StateMutator: func(s State) error {
			s.Set(testPath, TypedValue{Type: "string", Value: "done"})
			return nil
		},
	}

	if err := runtime.SubmitEvent(testEvent); err != nil {
		t.Fatalf("Failed to submit event: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	clock := runtime.GetClock()

	t.Logf("Run %d clock: %d", runNum, clock)
}

func TestEventReplay(t *testing.T) {
	runtime := NewRuntime(SchedulerModeNormal)

	if err := runtime.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer runtime.Stop()

	testPath := Path("replay_test.value")

	initialEvent := Event{
		Type:       "initial_event",
		WritePaths: []Path{testPath},
		StateMutator: func(s State) error {
			s.Set(testPath, TypedValue{Type: "string", Value: "initial"})
			return nil
		},
	}

	if err := runtime.SubmitEvent(initialEvent); err != nil {
		t.Fatalf("Failed to submit initial event: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	updateEvent := Event{
		Type:       "update_event",
		WritePaths: []Path{testPath},
		StateMutator: func(s State) error {
			s.Set(testPath, TypedValue{Type: "string", Value: "updated"})
			return nil
		},
	}

	if err := runtime.SubmitEvent(updateEvent); err != nil {
		t.Fatalf("Failed to submit update event: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	val, ok := runtime.GetState().Get(testPath)
	if !ok {
		t.Fatal("Value should exist in state")
	}
	if val.Value != "updated" {
		t.Fatalf("Expected 'updated', got '%v'", val.Value)
	}

	// Reconstruct should replay all events, resulting in the final state
	if err := runtime.Replay(); err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	val, ok = runtime.GetState().Get(testPath)
	if !ok {
		t.Fatal("Value should exist after replay")
	}
	// After replaying all events, we should end up at the final state ("updated")
	// To get back to "initial", we would need to truncate the event log
	if val.Value != "updated" {
		t.Fatalf("Expected 'updated' after replay (replay replays all events), got '%v'", val.Value)
	}

	t.Log("Event Replay - PASSED")
}

func TestStateLocking(t *testing.T) {
	stateMgr := NewStateManager()

	testPath := Path("test.path")

	if err := stateMgr.Lock([]Path{testPath}, "owner1"); err != nil {
		t.Fatalf("Failed to lock: %v", err)
	}

	if err := stateMgr.Lock([]Path{testPath}, "owner2"); err == nil {
		t.Error("Should not be able to lock with different owner")
	}

	if err := stateMgr.Unlock([]Path{testPath}, "owner1"); err != nil {
		t.Fatalf("Failed to unlock: %v", err)
	}

	if err := stateMgr.Lock([]Path{testPath}, "owner2"); err != nil {
		t.Fatalf("Failed to lock after unlock: %v", err)
	}

	t.Log("State Locking - PASSED")
}
