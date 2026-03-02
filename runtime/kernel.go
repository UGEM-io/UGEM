// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/ugem-io/ugem/storage"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrRuntimeNotRunning     = errors.New("runtime not running")
	ErrRuntimeAlreadyRunning = errors.New("runtime already running")
)

type GoalRuntime struct {
	mu               sync.RWMutex
	stateManager     StateManager
	eventLog         EventLog
	goalEngine       GoalEngine
	sched            Scheduler
	planner          Planner
	eventBus         EventBus
	actionDispatcher ActionDispatcher
	clock            atomic.Uint64
	activePlans      map[string]bool
	planSem          chan struct{}
	stopCh           chan struct{}
	wg               sync.WaitGroup
	running          bool
	mode             SchedulerMode
	pss              *storage.PersistentStore
	plugins          []Plugin
	pluginConfigs    map[string]map[string]string
}

func NewRuntime(mode SchedulerMode) *GoalRuntime {
	stateManager := NewStateManager()
	eventLog := NewEventLog()
	goalEngine := NewGoalEngine(stateManager)
	sched := NewScheduler(goalEngine, eventLog, mode)
	planner := NewPlanner()
	eventBus := NewEventBus()
	actionDispatcher := NewActionDispatcher(10)
	actionDispatcher.SetPlanner(planner)

	return &GoalRuntime{
		stateManager:     stateManager,
		eventLog:         eventLog,
		goalEngine:       goalEngine,
		sched:            sched,
		planner:          planner,
		eventBus:         eventBus,
		actionDispatcher: actionDispatcher,
		activePlans:      make(map[string]bool),
		planSem:          make(chan struct{}, 100), // Max 100 concurrent plan executions
		stopCh:           make(chan struct{}),
		running:          false,
		mode:             mode,
		plugins:          make([]Plugin, 0),
		pluginConfigs:    make(map[string]map[string]string),
	}
}

func (r *GoalRuntime) RegisterPlugin(p Plugin, config map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = append(r.plugins, p)
	r.pluginConfigs[p.Name()] = config
}

func (r *GoalRuntime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startUnlocked()
}

func (r *GoalRuntime) startUnlocked() error {
	if r.running {
		return ErrRuntimeAlreadyRunning
	}

	r.running = true
	r.stopCh = make(chan struct{})

	if err := r.eventBus.Start(); err != nil {
		r.running = false
		return err
	}

	if ge, ok := r.goalEngine.(*GoalEngineImpl); ok {
		if err := ge.Start(); err != nil {
			r.running = false
			return err
		}
	}

	if err := r.actionDispatcher.Start(); err != nil {
		r.running = false
		return err
	}

	if err := r.sched.Run(); err != nil {
		r.running = false
		return err
	}

	if err := r.goalEngine.Run(); err != nil {
		r.running = false
		return err
	}

	// Initialize and register plugins
	for _, p := range r.plugins {
		if err := p.Init(context.Background(), r.pluginConfigs[p.Name()]); err != nil {
			r.running = false
			return fmt.Errorf("plugin %s init failed: %w", p.Name(), err)
		}
		for actionType, handler := range p.Actions() {
			r.planner.RegisterAction(actionType, handler)
		}
	}

	r.wg.Add(1)
	go r.executionLoop()

	return nil
}

func (r *GoalRuntime) Stop() error {
	return r.stopUnlocked()
}

func (r *GoalRuntime) stopUnlocked() error {
	r.mu.Lock()

	if !r.running {
		r.mu.Unlock()
		return ErrRuntimeNotRunning
	}

	if r.stopCh != nil {
		close(r.stopCh)
	}
	r.mu.Unlock()

	r.eventBus.Stop()
	r.actionDispatcher.Stop()
	r.sched.Stop()
	r.goalEngine.Stop()

	r.wg.Wait()

	r.mu.Lock()
	r.running = false
	r.stopCh = nil
	r.mu.Unlock()

	return nil
}

func (r *GoalRuntime) executionLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.processGoals()
		}
	}
}

func (r *GoalRuntime) processGoals() {
	activeGoals := r.goalEngine.ListActiveGoals()

	now := time.Now()

	for _, g := range activeGoals {
		// Deadline enforcement (monotonic-safe: also check elapsed since start)
		if !g.Deadline.IsZero() {
			if now.After(g.Deadline) {
				r.goalEngine.FailGoal(g.ID)
				continue
			}
			// Guard against wall-clock backward jump: if started + allowed duration exceeded
			if g.StartedAt != nil {
				allowedDuration := g.Deadline.Sub(g.CreatedAt)
				if allowedDuration > 0 && now.Sub(*g.StartedAt) > allowedDuration {
					r.goalEngine.FailGoal(g.ID)
					continue
				}
			}
		}

		// Timeout enforcement (already monotonic-safe via time.Since)
		if g.Timeout > 0 && g.StartedAt != nil && now.Sub(*g.StartedAt) > g.Timeout {
			r.goalEngine.FailGoal(g.ID)
			continue
		}

		completed := r.goalEngine.Evaluate(g, r.stateManager)

		if completed {
			r.goalEngine.CompleteGoal(g.ID)

			children := r.goalEngine.SpawnChildren(g, r.stateManager)
			for _, child := range children {
				r.sched.Enqueue(child)
			}
		} else {
			// Skip planning if this goal already has an active plan
			r.mu.RLock()
			_, hasActivePlan := r.activePlans[g.ID]
			r.mu.RUnlock()
			if hasActivePlan {
				continue
			}

			missingPaths := r.computeMissingConditions(g)

			if len(missingPaths) > 0 {
				var plan *Plan
				var err error

				// Goal-level retry with fallback planning
				if g.Retry != nil && g.Attempts > 0 {
					plan, err = r.planner.ResolveWithFallback(g.ID, missingPaths, r.stateManager, g.Attempts)
				} else {
					plan, err = r.planner.Resolve(missingPaths, r.stateManager)
				}
				if err != nil {
					continue
				}

				if len(plan.Actions) > 0 {
					// Concurrency limit: try to acquire semaphore
					select {
					case r.planSem <- struct{}{}:
					default:
						// At capacity, skip this cycle
						continue
					}

					// Mark plan as active and execute asynchronously
					r.mu.Lock()
					r.activePlans[g.ID] = true
					r.mu.Unlock()

					r.wg.Add(1)
					go func(p *Plan, goal Goal) {
						defer func() {
							r.wg.Done()
							<-r.planSem // Release semaphore
							r.mu.Lock()
							delete(r.activePlans, goal.ID)
							r.mu.Unlock()
							if rec := recover(); rec != nil {
								// Plan panicked — don't crash the runtime
							}
						}()
						r.executePlan(p, goal)
					}(plan, g)
				}
			}
		}
	}
}

func (r *GoalRuntime) computeMissingConditions(g Goal) []Path {
	missing := make([]Path, 0)

	// Check trigger path
	if g.Trigger.Path != nil {
		if _, exists := r.stateManager.Get(*g.Trigger.Path); !exists {
			missing = append(missing, *g.Trigger.Path)
		}
	}

	// If goal has a condition and the condition is not met, check for missing paths
	if g.Condition != nil && !g.Condition(r.stateManager) {
		// If there's a trigger path, it's the known dependency
		if g.Trigger.Path != nil && len(missing) == 0 {
			missing = append(missing, *g.Trigger.Path)
		}
	}

	return missing
}

func (r *GoalRuntime) executePlan(plan *Plan, g Goal) {
	currentClock := LogicalClock(r.clock.Load())

	for _, action := range plan.Actions {
		ctx := ActionContext{
			EventID: 0,
			Clock:   currentClock,
			Trace:   g.Trace,
		}
		ctx.Trace.ActionID = action.ID
		ctx.Trace.GoalID = g.ID

		result, err := r.actionDispatcher.Execute(action, ctx)
		if err != nil {
			continue
		}

		if result != nil {
			r.submitEventFromAction(result, action, ctx.Trace)
		}
	}

	r.clock.Add(1)
}

func (r *GoalRuntime) submitEventFromAction(actionOutput map[string]interface{}, action Action, trace TraceContext) {
	event := Event{
		Type:       action.Type + "_result",
		Payload:    actionOutput,
		WritePaths: []Path{Path(action.ID)},
		Trace:      trace,
	}

	r.SubmitEvent(event)
}

func (r *GoalRuntime) SubmitGoal(goal Goal) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return ErrRuntimeNotRunning
	}

	if goal.Trace.TraceID == "" {
		goal.Trace.TraceID = fmt.Sprintf("trace-%d", time.Now().UnixNano())
		goal.Trace.GoalID = goal.ID
	}

	if err := r.goalEngine.AddGoal(goal); err != nil {
		return err
	}

	r.sched.Enqueue(goal)

	return nil
}

func (r *GoalRuntime) SubmitEvent(event Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return ErrRuntimeNotRunning
	}

	if event.Trace.TraceID == "" {
		event.Trace.TraceID = fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}

	eventID, err := r.eventLog.Append(event)
	if err != nil {
		return err
	}

	event.ID = eventID
	event.Trace.ParentEventID = strconv.FormatUint(uint64(eventID), 10)

	if err := r.stateManager.ApplyEvent(event); err != nil {
		return err
	}

	r.goalEngine.HandleEvent(event)

	r.eventBus.Publish(event)

	r.clock.Add(1)

	return nil
}

func (r *GoalRuntime) SetPersistence(pss *storage.PersistentStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pss = pss

	if sm, ok := r.stateManager.(*StateManagerImpl); ok {
		sm.SetPersistence(pss)
	}
	if el, ok := r.eventLog.(*EventLogImpl); ok {
		el.SetPersistence(pss)
	}

	// Reconstruct state from persistent events
	events := r.eventLog.GetEvents()
	if len(events) > 0 {
		r.stateManager.Reconstruct(events)
		r.clock.Store(uint64(events[len(events)-1].Clock))
	}
}

func (r *GoalRuntime) GetState() StateManager {
	return r.stateManager
}

func (r *GoalRuntime) GetEventLog() EventLog {
	return r.eventLog
}

func (r *GoalRuntime) GetStateSnapshot() (StateSnapshot, error) {
	return r.stateManager.GetSnapshot()
}

func (r *GoalRuntime) Rewind(timestamp time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. Stop if running
	wasRunning := r.running
	if wasRunning {
		r.mu.Unlock()
		r.stopUnlocked()
		r.mu.Lock()
	}

	// 2. Replay events until timestamp
	ch, err := r.eventLog.ReplayUntil(timestamp)
	if err != nil {
		return err
	}

	events := make([]Event, 0)
	for e := range ch {
		events = append(events, e)
	}

	// 3. Reconstruct state
	if err := r.stateManager.Reconstruct(events); err != nil {
		return err
	}

	// 4. Update internal clock to match last event
	if len(events) > 0 {
		r.clock.Store(uint64(events[len(events)-1].Clock))
	} else {
		r.clock.Store(0)
	}

	// 5. Restart if it was running
	if wasRunning {
		return r.startUnlocked()
	}

	return nil
}

func (r *GoalRuntime) GetPSS() *storage.PersistentStore {
	return r.pss
}

func (r *GoalRuntime) Simulate(goal Goal) ([]ActionResult, error) {
	// 1. Create a snapshot of current state
	state, _ := r.stateManager.GetSnapshot()
	shadowState := NewStateManager()
	shadowState.Apply(state)

	// 2. Create a shadow planner and dispatcher
	shadowPlanner := NewPlanner()
	// Copy actions from original planner if possible, or just use no-ops for simulation
	
	shadowDispatcher := NewActionDispatcher(1)
	shadowDispatcher.SetPlanner(shadowPlanner)

	// 3. Resolve and execute plan in isolation
	missing := r.computeMissingConditions(goal)
	plan, err := shadowPlanner.Resolve(missing, shadowState)
	if err != nil {
		return nil, err
	}

	results := make([]ActionResult, 0)
	for _, action := range plan.Actions {
		// In simulation, we don't actually execute, just record the intent
		results = append(results, ActionResult{
			ActionID: action.ID,
			Success:  true,
			Output:   map[string]interface{}{"status": "simulated"},
		})
	}

	return results, nil
}

func (r *GoalRuntime) Fork(name string) (*GoalRuntime, error) {
	if r.pss == nil {
		return nil, fmt.Errorf("persistence must be enabled to fork")
	}

	forkDir := filepath.Join(r.pss.GetDataDir(), "forks", name)
	if err := os.MkdirAll(forkDir, 0755); err != nil {
		return nil, err
	}

	// Simple fork: just create a new PSS in the fork directory
	// In a real system, we'd copy the snapshot and WAL
	newPSS, err := storage.NewPersistentStore(forkDir)
	if err != nil {
		return nil, err
	}

	newRuntime := NewRuntime(r.mode)
	newRuntime.SetPersistence(newPSS)
	
	// Pre-load state from original
	snap, _ := r.stateManager.GetSnapshot()
	newRuntime.stateManager.Apply(snap)

	return newRuntime, nil
}

func (r *GoalRuntime) GetGoalEngine() GoalEngine {
	return r.goalEngine
}

func (r *GoalRuntime) GetScheduler() Scheduler {
	return r.sched
}

func (r *GoalRuntime) GetPlanner() Planner {
	return r.planner
}

func (r *GoalRuntime) GetEventBus() EventBus {
	return r.eventBus
}

func (r *GoalRuntime) GetActionDispatcher() ActionDispatcher {
	return r.actionDispatcher
}

func (r *GoalRuntime) GetClock() LogicalClock {
	return LogicalClock(r.clock.Load())
}

func (r *GoalRuntime) RegisterExternalWorker(actionType string, addr string) {
	r.actionDispatcher.RegisterExternalWorker(actionType, addr)
}

func (r *GoalRuntime) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

func (r *GoalRuntime) Replay() error {
	ch, err := r.eventLog.Replay()
	if err != nil {
		return err
	}

	events := make([]Event, 0)
	for e := range ch {
		events = append(events, e)
	}

	r.stateManager.Reconstruct(events)

	return nil
}
