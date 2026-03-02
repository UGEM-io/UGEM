// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func BenchmarkSequentialPlanning(b *testing.B) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.Start()
	defer rt.Stop()

	for i := 0; i < b.N; i++ {
		goal := Goal{
			ID:       fmt.Sprintf("goal_%d", i),
			Priority: 10,
			Condition: func(s State) bool {
				return true
			},
		}
		rt.SubmitGoal(goal)
		time.Sleep(10 * time.Millisecond)
	}
}

func BenchmarkEventTriggeredGoals(b *testing.B) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.Start()
	defer rt.Stop()

	for i := 0; i < b.N; i++ {
		event := Event{
			Type:       fmt.Sprintf("event_%d", i),
			WritePaths: []Path{Path(fmt.Sprintf("path_%d", i))},
			StateMutator: func(s State) error {
				return nil
			},
		}
		rt.SubmitEvent(event)
		time.Sleep(1 * time.Millisecond)
	}
}

func BenchmarkHighConcurrency(b *testing.B) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.Start()
	defer rt.Stop()

	var wg sync.WaitGroup
	numGoroutines := 100

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < b.N/numGoroutines; i++ {
				goal := Goal{
					ID:       fmt.Sprintf("goal_%d_%d", id, i),
					Priority: id % 10,
					Condition: func(s State) bool {
						return true
					},
				}
				rt.SubmitGoal(goal)
			}
		}(g)
	}

	wg.Wait()
}

func BenchmarkDynamicReplanning(b *testing.B) {
	rt := NewRuntime(SchedulerModeNormal)
	rt.Start()
	defer rt.Stop()

	planner := rt.GetPlanner()

	planner.RegisterAction("test_action", func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error) {
		return map[string]interface{}{"success": true}, nil
	})

	for i := 0; i < b.N; i++ {
		goal := Goal{
			ID:       fmt.Sprintf("replan_goal_%d", i),
			Priority: 10,
			Condition: func(s State) bool {
				val, ok := s.Get("result_path")
				return ok && val.Value == "success"
			},
		}
		rt.SubmitGoal(goal)

		time.Sleep(1 * time.Millisecond)

		event := Event{
			Type:       "replan_event",
			WritePaths: []Path{"result_path"},
			StateMutator: func(s State) error {
				s.Set("result_path", TypedValue{Type: "string", Value: "success"})
				return nil
			},
		}
		rt.SubmitEvent(event)
	}
}

func BenchmarkMetricsCollection(b *testing.B) {
	metrics := NewMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordGoalComplete()
		metrics.RecordActionSuccess(time.Millisecond * 10)
		metrics.RecordEventReactionTime(time.Millisecond * 5)
		metrics.RecordScheduling(fmt.Sprintf("goal_%d", i))
	}
}

func BenchmarkShortTermMemory(b *testing.B) {
	mem := NewShortTermMemory(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem.AddRecord(ExecutionRecord{
			ID:        fmt.Sprintf("record_%d", i),
			GoalID:    fmt.Sprintf("goal_%d", i),
			ActionID:  "test_action",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Status:    "success",
		})

		if i%10 == 0 {
			mem.RecordFailure(fmt.Sprintf("goal_%d", i), "test_action", "error")
		}
	}
}

func TestMetricsReport(t *testing.T) {
	metrics := NewMetrics()

	metrics.TotalGoals = 100
	metrics.CompletedGoals = 80
	metrics.FailedGoals = 15
	metrics.CancelledGoals = 5
	metrics.TotalActions = 200
	metrics.SuccessfulActions = 180
	metrics.FailedActions = 20

	report := metrics.GetReport()

	if report["total_goals"].(int) != 100 {
		t.Errorf("Expected 100 total goals, got %d", report["total_goals"].(int))
	}

	if report["completed_goals"].(int) != 80 {
		t.Errorf("Expected 80 completed goals, got %d", report["completed_goals"].(int))
	}

	if report["throughput_goals_sec"].(float64) <= 0 {
		t.Error("Throughput should be positive")
	}

	t.Logf("Metrics Report: %+v", report)
}

func TestShortTermMemory(t *testing.T) {
	mem := NewShortTermMemory(10)

	for i := 0; i < 15; i++ {
		mem.AddRecord(ExecutionRecord{
			ID:     fmt.Sprintf("record_%d", i),
			GoalID: "test_goal",
			Status: "success",
		})
	}

	records := mem.GetRecentRecords()
	if len(records) != 10 {
		t.Errorf("Expected 10 records, got %d", len(records))
	}

	mem.RecordFailure("test_goal", "action_1", "error 1")
	mem.RecordFailure("test_goal", "action_1", "error 2")

	count := mem.GetFailureCount("test_goal")
	if count != 2 {
		t.Errorf("Expected 2 failures, got %d", count)
	}

	failures := mem.GetRecentFailures("test_goal")
	if len(failures) != 2 {
		t.Errorf("Expected 2 failure messages, got %d", len(failures))
	}

	mem.Clear()
	count = mem.GetFailureCount("test_goal")
	if count != 0 {
		t.Errorf("Expected 0 failures after clear, got %d", count)
	}
}

func TestBackoffStrategies(t *testing.T) {
	ad := NewActionDispatcher(1)

	tests := []struct {
		name     string
		backoff  *RetryStrategy
		attempts int
		expected time.Duration
	}{
		{
			name:     "linear",
			backoff:  &RetryStrategy{InitialDelay: 10 * time.Millisecond, Strategy: BackoffLinear, Multiplier: 1},
			attempts: 3,
			expected: 30 * time.Millisecond,
		},
		{
			name:     "exponential",
			backoff:  &RetryStrategy{InitialDelay: 10 * time.Millisecond, Strategy: BackoffExponential},
			attempts: 3,
			expected: 40 * time.Millisecond,
		},
		{
			name:     "fibonacci",
			backoff:  &RetryStrategy{InitialDelay: 10 * time.Millisecond, Strategy: BackoffFibonacci},
			attempts: 5,
			expected: 50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Action{
				ID:      "test",
				Retries: tt.attempts,
				Backoff: tt.backoff,
			}

			ctx := ActionContext{}
			_ = ctx

			result := ad.calculateBackoff(action.Backoff, tt.attempts-1)

			if result < tt.expected-5*time.Millisecond || result > tt.expected+5*time.Millisecond {
				t.Errorf("Expected ~%v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDeadlineAwareScheduling(t *testing.T) {
	sched := NewScheduler(nil, nil, SchedulerModeNormal)

	goal1 := Goal{
		ID:       "goal1",
		Priority: 5,
		Deadline: time.Now().Add(10 * time.Second),
		Timeout:  5 * time.Second,
	}

	goal2 := Goal{
		ID:       "goal2",
		Priority: 10,
		Deadline: time.Now().Add(1 * time.Second),
		Timeout:  500 * time.Millisecond,
	}

	sched.Schedule([]Goal{goal1, goal2}, []Event{}, []TimerTrigger{})

	item := sched.Peek()
	if item.GoalID != "goal2" {
		t.Errorf("Expected goal2 to be scheduled first (earlier deadline), got %s", item.GoalID)
	}

	t.Logf("Scheduled first: %s (deadline: %v)", item.GoalID, item.Deadline)
}
