// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime

import (
	"context"
	"sync"
	"time"
)

type File struct {
	ID   string
	URI  string
	Hash string
	Size int64
	Mime string
}

type TypedValue struct {
	Type  string
	Value interface{}
}

type Path string

type EventID uint64

type LogicalClock uint64

type Event struct {
	ID           EventID
	Clock        LogicalClock
	Timestamp    time.Time
	Type         string
	ReadPaths    []Path
	WritePaths   []Path
	Payload      map[string]interface{}
	Trace        TraceContext
	StateMutator   func(state State) error
	StateMutations []StateMutation
	CorrectionOf   EventID
}

type StateMutation struct {
	Path  Path
	Value TypedValue
}

type Action struct {
	ID           string
	Type         string
	Input        map[string]interface{}
	Timeout      time.Duration
	Retries      int
	Idempotency  string
	Execute      func(ctx ActionContext) (map[string]interface{}, error)
	Backoff      *RetryStrategy
	Cancel       func() bool
	FallbackPlan *Plan
}

type ActionContext struct {
	EventID    EventID
	Clock      LogicalClock
	Trace      TraceContext
	CancelCtx  context.Context
	CancelFunc context.CancelFunc
	RetryCount int
}

type State interface {
	Get(path Path) (TypedValue, bool)
	Set(path Path, value TypedValue) error
	Lock(paths []Path, owner string) error
	Unlock(paths []Path, owner string) error
	Snapshot() (StateSnapshot, error)
	Apply(snap StateSnapshot) error
	Diff(before StateSnapshot) (map[Path]TypedValue, error)
}

type StateSnapshot interface {
	ID() uint64
	Clock() LogicalClock
	State() map[Path]TypedValue
}

type GoalState string

const (
	GoalStatePending       GoalState = "pending"
	GoalStateActive        GoalState = "active"
	GoalStateComplete      GoalState = "complete"
	GoalStateFailed        GoalState = "failed"
	GoalStateCancelled     GoalState = "cancelled"
	GoalStateCompensating  GoalState = "compensating"
)

type BackoffStrategy string

const (
	BackoffNone        BackoffStrategy = "none"
	BackoffLinear      BackoffStrategy = "linear"
	BackoffExponential BackoffStrategy = "exponential"
	BackoffFibonacci   BackoffStrategy = "fibonacci"
)

type RetryStrategy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Strategy     BackoffStrategy
	Multiplier   float64
}

type Constraint struct {
	Type     string
	Value    interface{}
	Operator string
}

type SchedulerMode string

const (
	SchedulerModeNormal           SchedulerMode = "normal"
	SchedulerModeStrict           SchedulerMode = "strict_deterministic"
	SchedulerModeReplayValidation SchedulerMode = "replay_validation"
)

type TraceContext struct {
	TraceID       string
	ParentEventID string
	GoalID        string
	ActionID      string
}

type Invariant struct {
	Name      string
	Predicate func(state State) error
}

type Goal struct {
	ID          string
	Priority    int
	State       GoalState
	Clock       LogicalClock
	ParentID    *string
	Condition   func(state State) bool
	Spawn       func(state State) []Goal
	Compensate  func(state State) *Goal
	Trigger     GoalTrigger
	Deadline    time.Time
	Timeout     time.Duration
	Constraints []Constraint
	Metadata    map[string]string
	Trace       TraceContext
	Retry       *RetryStrategy
	Attempts    int
	MaxAttempts int
	CreatedAt   time.Time
	StartedAt   *time.Time
	CancelledAt *time.Time
	FailReason  string
}

type GoalTrigger struct {
	Type string
	Path *Path
}

type Plan struct {
	Actions    []Action
	EventIDs   []EventID
	GoalID     string
	LogicalSeq uint64
}

type AlternativePlan struct {
	Name     string
	Actions  []Action
	Priority int
}

type Planner interface {
	Resolve(missingPaths []Path, state State) (*Plan, error)
	ResolveWithFallback(goalID string, missingPaths []Path, state State, failureCount int) (*Plan, error)
	RegisterAction(actionType string, handler func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error))
	RegisterAlternativePlan(goalID string, alt AlternativePlan)
	GetActionResolver(actionType string) (*ActionResolver, bool)
}

type ActionHandler func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error)

type Plugin interface {
	Name() string
	Init(ctx context.Context, config map[string]string) error
	Actions() map[string]ActionHandler
}

type FileStoragePlugin interface {
	Plugin
	Put(ctx context.Context, stream []byte) (File, error) // Simplified stream for now
	Get(ctx context.Context, uri string) ([]byte, error)
	Delete(ctx context.Context, uri string) error
	Exists(ctx context.Context, uri string) bool
}

type Scheduler interface {
	Schedule(goals []Goal, events []Event, timers []TimerTrigger) []ScheduledItem
	Enqueue(goal Goal)
	Dequeue(id string) *ScheduledItem
	DequeueReady() []*ScheduledItem
	Run() error
	Stop() error
	GetScheduledItem(id string) (*ScheduledItem, bool)
}

type ScheduledItem struct {
	ID        string
	GoalID    string
	Priority  int
	Clock     LogicalClock
	ExecuteAt time.Time
	Deadline  time.Time
	Type      string
}

type TimerTrigger struct {
	ID        string
	GoalID    string
	ExecuteAt time.Time
	Period    time.Duration
}

type ExecutionRecord struct {
	ID           string
	GoalID       string
	ActionID     string
	StartTime    time.Time
	EndTime      time.Time
	Status       string
	Input        map[string]interface{}
	Output       map[string]interface{}
	Error        error
	RetryAttempt int
}

type ShortTermMemory struct {
	mu           sync.RWMutex
	records      []ExecutionRecord
	maxRecords   int
	failures     map[string]int
	failurePaths map[string][]string
}

func NewShortTermMemory(maxRecords int) *ShortTermMemory {
	return &ShortTermMemory{
		records:      make([]ExecutionRecord, 0),
		maxRecords:   maxRecords,
		failures:     make(map[string]int),
		failurePaths: make(map[string][]string),
	}
}

func (m *ShortTermMemory) AddRecord(record ExecutionRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
	if len(m.records) > m.maxRecords {
		m.records = m.records[1:]
	}
}

func (m *ShortTermMemory) RecordFailure(goalID, actionID, errorMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[goalID]++
	if m.failurePaths[goalID] == nil {
		m.failurePaths[goalID] = make([]string, 0)
	}
	m.failurePaths[goalID] = append(m.failurePaths[goalID], errorMsg)
}

func (m *ShortTermMemory) GetFailureCount(goalID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failures[goalID]
}

func (m *ShortTermMemory) GetRecentFailures(goalID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failurePaths[goalID]
}

func (m *ShortTermMemory) GetRecentRecords() []ExecutionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ExecutionRecord, len(m.records))
	copy(result, m.records)
	return result
}

func (m *ShortTermMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = make([]ExecutionRecord, 0)
	m.failures = make(map[string]int)
	m.failurePaths = make(map[string][]string)
}

type Metrics struct {
	mu                 sync.RWMutex
	TotalGoals         int
	CompletedGoals     int
	FailedGoals        int
	CancelledGoals     int
	TotalActions       int
	SuccessfulActions  int
	FailedActions      int
	TotalEvents        int
	AvgExecutionTime   time.Duration
	MaxExecutionTime   time.Duration
	MinExecutionTime   time.Duration
	Throughput         float64
	EventReactionTimes []time.Duration
	SchedulingFairness map[string]int
	StartTime          time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		SchedulingFairness: make(map[string]int),
		StartTime:          time.Now(),
	}
}

func (m *Metrics) RecordGoalComplete() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CompletedGoals++
}

func (m *Metrics) RecordGoalFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedGoals++
}

func (m *Metrics) RecordGoalCancelled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CancelledGoals++
}

func (m *Metrics) RecordActionSuccess(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SuccessfulActions++
	m.TotalActions++
	m.updateExecutionTime(duration)
}

func (m *Metrics) RecordActionFailure(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedActions++
	m.TotalActions++
	m.updateExecutionTime(duration)
}

func (m *Metrics) updateExecutionTime(duration time.Duration) {
	if m.TotalActions == 1 {
		m.AvgExecutionTime = duration
		m.MaxExecutionTime = duration
		m.MinExecutionTime = duration
	} else {
		total := m.AvgExecutionTime * time.Duration(m.TotalActions-1)
		m.AvgExecutionTime = (total + duration) / time.Duration(m.TotalActions)
		if duration > m.MaxExecutionTime {
			m.MaxExecutionTime = duration
		}
		if duration < m.MinExecutionTime {
			m.MinExecutionTime = duration
		}
	}
}

func (m *Metrics) RecordEventReactionTime(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EventReactionTimes = append(m.EventReactionTimes, duration)
	if len(m.EventReactionTimes) > 1000 {
		m.EventReactionTimes = m.EventReactionTimes[1:]
	}
}

func (m *Metrics) RecordScheduling(goalID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SchedulingFairness[goalID]++
}

func (m *Metrics) GetReport() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	elapsed := time.Since(m.StartTime)
	throughput := float64(m.CompletedGoals) / elapsed.Seconds()

	return map[string]interface{}{
		"total_goals":          m.TotalGoals,
		"completed_goals":      m.CompletedGoals,
		"failed_goals":         m.FailedGoals,
		"cancelled_goals":      m.CancelledGoals,
		"total_actions":        m.TotalActions,
		"successful_actions":   m.SuccessfulActions,
		"failed_actions":       m.FailedActions,
		"total_events":         m.TotalEvents,
		"avg_execution_time":   m.AvgExecutionTime.String(),
		"max_execution_time":   m.MaxExecutionTime.String(),
		"min_execution_time":   m.MinExecutionTime.String(),
		"throughput_goals_sec": throughput,
		"elapsed_time":         elapsed.String(),
		"scheduling_fairness":  m.SchedulingFairness,
	}
}

type EventBus interface {
	Start() error
	Stop() error
	Subscribe(subscriber string, eventTypes []string, handler func(Event)) error
	Unsubscribe(subscriber string) error
	Publish(event Event) error
	Replay(fromEventID EventID) (<-chan Event, error)
	GetEvent(id EventID) (Event, error)
}

type ActionDispatcher interface {
	Start() error
	Stop() error
	Execute(action Action, ctx ActionContext) (map[string]interface{}, error)
	ExecuteAsync(action Action, ctx ActionContext) (<-chan ActionResult, error)
	CancelAction(actionID string) bool
	WorkerCount() int
	SetWorkerCount(count int)
	RegisterExternalWorker(actionType string, addr string)
	SetPlanner(p Planner)
}

type ActionResult struct {
	ActionID string
	Success  bool
	Output   map[string]interface{}
	Error    error
	Event    Event
}

type GoalEngine interface {
	AddGoal(goal Goal) error
	RemoveGoal(id string) error
	GetGoal(id string) (Goal, bool)
	ListGoals() []Goal
	ListActiveGoals() []Goal
	Evaluate(goal Goal, state State) bool
	SpawnChildren(goal Goal, state State) []Goal
	CompleteGoal(id string) error
	CancelGoal(id string, reason string) error
	FailGoal(id string) error
	HandleEvent(event Event)
	Run() error
	Stop() error
}

type StateManager interface {
	State
	LockManager
	ApplyEvent(event Event) error
	GetSnapshot() (StateSnapshot, error)
	Reconstruct(events []Event) error
	RegisterInvariant(inv Invariant)
	GetClock() LogicalClock
	AdvanceClock() LogicalClock
}

type LockManager interface {
	Lock(paths []Path, owner string) error
	Unlock(paths []Path, owner string) error
	IsLocked(path Path) bool
	GetLockOwner(path Path) (string, bool)
}

type EventLog interface {
	Append(event Event) (EventID, error)
	Get(id EventID) (Event, error)
	Range(start, end EventID) ([]Event, error)
	Length() EventID
	GetClock() LogicalClock
	Replay() (<-chan Event, error)
	ReplayUntil(timestamp time.Time) (<-chan Event, error)
	GetEvents() []Event
}

type Runtime interface {
	Start() error
	Stop() error
	SubmitGoal(goal Goal) error
	SubmitEvent(event Event) error
	GetState() StateManager
	GetEventLog() EventLog
	GetGoalEngine() GoalEngine
	GetScheduler() Scheduler
	GetPlanner() Planner
	GetEventBus() EventBus
	GetActionDispatcher() ActionDispatcher
}
