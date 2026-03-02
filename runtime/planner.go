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
	ErrNoPlanFound       = errors.New("no plan found")
	ErrCyclicDependency  = errors.New("cyclic dependency detected")
	ErrUnknownActionType = errors.New("unknown action type")
)

type ActionResolver struct {
	ActionType string
	Handler    func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error)
}

type PlannerImpl struct {
	mu               sync.RWMutex
	resolvers        map[string]*ActionResolver
	dependencyGraph  map[string][]string
	clock            LogicalClock
	actionSeq        uint64
	alternativePlans map[string][]AlternativePlan
}

func NewPlanner() *PlannerImpl {
	return &PlannerImpl{
		resolvers:        make(map[string]*ActionResolver),
		dependencyGraph:  make(map[string][]string),
		clock:            0,
		actionSeq:        0,
		alternativePlans: make(map[string][]AlternativePlan),
	}
}

func (p *PlannerImpl) Resolve(missingPaths []Path, state State) (*Plan, error) {
	if len(missingPaths) == 0 {
		return &Plan{
			Actions:    []Action{},
			EventIDs:   []EventID{},
			LogicalSeq: p.actionSeq,
		}, nil
	}

	p.mu.Lock()
	p.actionSeq++
	p.clock++
	seq := p.actionSeq
	p.mu.Unlock()

	actions := p.buildActionPlan(missingPaths, state)

	return &Plan{
		Actions:    actions,
		EventIDs:   []EventID{},
		GoalID:     "",
		LogicalSeq: seq,
	}, nil
}

func (p *PlannerImpl) ResolveWithFallback(goalID string, missingPaths []Path, state State, failureCount int) (*Plan, error) {
	plan, err := p.Resolve(missingPaths, state)
	if err != nil {
		return nil, err
	}

	p.mu.RLock()
	alts := p.alternativePlans[goalID]
	p.mu.RUnlock()

	if len(alts) > 0 && failureCount > 0 {
		idx := (failureCount - 1) % len(alts)
		alt := alts[idx]
		return &Plan{
			Actions:    alt.Actions,
			EventIDs:   []EventID{},
			GoalID:     goalID,
			LogicalSeq: plan.LogicalSeq,
		}, nil
	}

	return plan, nil
}

func (p *PlannerImpl) RegisterAlternativePlan(goalID string, alt AlternativePlan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.alternativePlans[goalID] = append(p.alternativePlans[goalID], alt)
}

func (p *PlannerImpl) buildActionPlan(missingPaths []Path, state State) []Action {
	actions := make([]Action, 0)

	actionQueue := p.computeActionSequence(missingPaths)

	sort.Slice(actionQueue, func(i, j int) bool {
		return actionQueue[i].ID < actionQueue[j].ID
	})

	for _, action := range actionQueue {
		actions = append(actions, action)
	}

	return actions
}

func (p *PlannerImpl) computeActionSequence(missingPaths []Path) []Action {
	actionQueue := make([]Action, 0)
	visited := make(map[string]bool)
	visitedPaths := make(map[Path]bool)

	var dfs func(path Path) error

	dfs = func(path Path) error {
		if visitedPaths[path] {
			return nil
		}
		visitedPaths[path] = true

		dependencies := p.dependencyGraph[string(path)]
		for _, dep := range dependencies {
			if visited[dep] {
				continue
			}
			visited[dep] = true

			action := p.createActionForPath(Path(dep))
			if action.ID != "" {
				actionQueue = append(actionQueue, action)
			}
		}

		return nil
	}

	for _, path := range missingPaths {
		dfs(path)
	}

	for _, path := range missingPaths {
		action := p.createActionForPath(path)
		if action.ID != "" {
			found := false
			for _, a := range actionQueue {
				if a.ID == action.ID {
					found = true
					break
				}
			}
			if !found {
				actionQueue = append(actionQueue, action)
			}
		}
	}

	return actionQueue
}

func (p *PlannerImpl) createActionForPath(path Path) Action {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Sort resolver keys for deterministic selection
	keys := make([]string, 0, len(p.resolvers))
	for k := range p.resolvers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, actionType := range keys {
		action := Action{
			ID:          string(path) + "_action",
			Type:        actionType,
			Input:       map[string]interface{}{"path": string(path)},
			Timeout:     30 * time.Second,
			Retries:     3,
			Idempotency: string(path),
		}

		return action
	}

	return Action{}
}

func (p *PlannerImpl) RegisterAction(actionType string, handler func(input map[string]interface{}, ctx ActionContext) (map[string]interface{}, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.resolvers[actionType] = &ActionResolver{
		ActionType: actionType,
		Handler:    handler,
	}
}

func (p *PlannerImpl) RegisterDependency(actionType string, dependsOn []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.dependencyGraph[actionType] = dependsOn
}

func (p *PlannerImpl) GetActionResolver(actionType string) (*ActionResolver, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	resolver, exists := p.resolvers[actionType]
	return resolver, exists
}

func (p *PlannerImpl) HasCyclicDependency() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(node string) bool

	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true

		dependencies := p.dependencyGraph[node]
		for _, dep := range dependencies {
			if !visited[dep] {
				if dfs(dep) {
					return true
				}
			} else if recStack[dep] {
				return true
			}
		}

		recStack[node] = false
		return false
	}

	for node := range p.resolvers {
		if !visited[node] {
			if dfs(node) {
				return true
			}
		}
	}

	return false
}

func (p *PlannerImpl) GetClock() LogicalClock {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.clock
}

func (p *PlannerImpl) Validate() error {
	if p.HasCyclicDependency() {
		return ErrCyclicDependency
	}
	return nil
}
