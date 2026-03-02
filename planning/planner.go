// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package planning

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ugem-io/ugem/logging"
	"github.com/ugem-io/ugem/runtime"
)

type DependencyNode struct {
	GoalID     string
	Dependents []string
	Dependees  []string
	Weight     int
	Metadata   map[string]interface{}
}

type DependencyGraph struct {
	nodes map[string]*DependencyNode
	mu    sync.RWMutex
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*DependencyNode),
	}
}

func (g *DependencyGraph) AddNode(goalID string, dependees []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.nodes[goalID]; exists {
		return fmt.Errorf("node %s already exists", goalID)
	}

	node := &DependencyNode{
		GoalID:    goalID,
		Dependees: dependees,
	}

	for _, dep := range dependees {
		if existing, ok := g.nodes[dep]; ok {
			existing.Dependents = append(existing.Dependents, goalID)
		}
	}

	g.nodes[goalID] = node
	return nil
}

func (g *DependencyGraph) HasNode(goalID string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, exists := g.nodes[goalID]
	return exists
}

func (g *DependencyGraph) AddDependency(from, to string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	fromNode, exists := g.nodes[from]
	if !exists {
		fromNode = &DependencyNode{GoalID: from, Dependees: []string{}}
		g.nodes[from] = fromNode
	}

	toNode, exists := g.nodes[to]
	if !exists {
		toNode = &DependencyNode{GoalID: to, Dependents: []string{}}
		g.nodes[to] = toNode
	}

	fromNode.Dependees = append(fromNode.Dependees, to)
	toNode.Dependents = append(toNode.Dependents, from)

	return nil
}

func (g *DependencyGraph) RemoveNode(goalID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	node, exists := g.nodes[goalID]
	if !exists {
		return fmt.Errorf("node %s not found", goalID)
	}

	for _, dep := range node.Dependents {
		if n, ok := g.nodes[dep]; ok {
			for i, d := range n.Dependees {
				if d == goalID {
					n.Dependees = append(n.Dependees[:i], n.Dependees[i+1:]...)
					break
				}
			}
		}
	}

	for _, dep := range node.Dependees {
		if n, ok := g.nodes[dep]; ok {
			for i, d := range n.Dependents {
				if d == goalID {
					n.Dependents = append(n.Dependents[:i], n.Dependents[i+1:]...)
					break
				}
			}
		}
	}

	delete(g.nodes, goalID)
	return nil
}

func (g *DependencyGraph) HasCycle() (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(node string) (bool, string)
	dfs = func(node string) (bool, string) {
		visited[node] = true
		recStack[node] = true

		currentNode, ok := g.nodes[node]
		if !ok {
			return false, ""
		}

		for _, neighbor := range currentNode.Dependees {
			if !visited[neighbor] {
				if cycle, path := dfs(neighbor); cycle {
					return true, path + " -> " + node
				}
			} else if recStack[neighbor] {
				return true, neighbor + " -> " + node
			}
		}

		recStack[node] = false
		return false, ""
	}

	for nodeID := range g.nodes {
		if !visited[nodeID] {
			if cycle, path := dfs(nodeID); cycle {
				return true, path
			}
		}
	}

	return false, ""
}

func (g *DependencyGraph) TopologicalSort() ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	inDegree := make(map[string]int)
	for id := range g.nodes {
		inDegree[id] = 0
	}

	for _, node := range g.nodes {
		for _, dep := range node.Dependees {
			inDegree[dep]++
		}
	}

	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	result := make([]string, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		node := g.nodes[current]
		for _, dep := range node.Dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("graph has cycles")
	}

	return result, nil
}

func (g *DependencyGraph) GetExecutionOrder() ([]string, error) {
	order, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}

	return order, nil
}

func (g *DependencyGraph) GetCriticalPath() ([]string, int, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.nodes) == 0 {
		return nil, 0, nil
	}

	longestPath := make(map[string]int)
	for id := range g.nodes {
		longestPath[id] = 1
	}

	var dfs func(string) int
	dfs = func(node string) int {
		currentNode := g.nodes[node]
		if len(currentNode.Dependees) == 0 {
			return 1
		}

		maxPath := 0
		for _, dep := range currentNode.Dependees {
			pathLen := dfs(dep)
			if pathLen > maxPath {
				maxPath = pathLen
			}
		}

		longestPath[node] = maxPath + 1
		return maxPath + 1
	}

	for id := range g.nodes {
		dfs(id)
	}

	var maxLength int
	var endNode string
	for id, length := range longestPath {
		if length > maxLength {
			maxLength = length
			endNode = id
		}
	}

	path := make([]string, 0, maxLength)
	current := endNode
	visited := make(map[string]bool)

	for current != "" && !visited[current] {
		path = append(path, current)
		visited[current] = true

		currentNode := g.nodes[current]
		var next string
		maxLen := 0

		for _, dep := range currentNode.Dependees {
			if longestPath[dep] > maxLen {
				maxLen = longestPath[dep]
				next = dep
			}
		}
		current = next
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path, maxLength - 1, nil
}

type WhatIfScenario struct {
	Name           string
	Changes        map[string]map[string]interface{}
	ProjectedGoals []string
	RiskScore      float64
}

type WhatIfAnalyzer struct {
	graph        *DependencyGraph
	currentGoals map[string]*runtime.Goal
	mu           sync.RWMutex
}

func NewWhatIfAnalyzer(goals []*runtime.Goal) *WhatIfAnalyzer {
	a := &WhatIfAnalyzer{
		currentGoals: make(map[string]*runtime.Goal),
		graph:        NewDependencyGraph(),
	}

	for _, goal := range goals {
		a.currentGoals[goal.ID] = goal
		a.graph.AddNode(goal.ID, []string{})
	}

	return a
}

func (a *WhatIfAnalyzer) SimulateGoalCompletion(goalID string, cascade bool) *WhatIfScenario {
	a.mu.Lock()
	defer a.mu.Unlock()

	scenario := &WhatIfScenario{
		Name:           fmt.Sprintf("complete_%s", goalID),
		Changes:        make(map[string]map[string]interface{}),
		ProjectedGoals: []string{},
	}

	scenario.Changes[goalID] = map[string]interface{}{
		"state": runtime.GoalStateComplete,
	}

	if cascade {
		affected := a.getAffectedGoals(goalID)
		scenario.ProjectedGoals = affected

		riskScore := float64(len(affected)) * 0.1
		if riskScore > 1.0 {
			riskScore = 1.0
		}
		scenario.RiskScore = riskScore
	}

	return scenario
}

func (a *WhatIfAnalyzer) SimulateGoalFailure(goalID string, cascade bool) *WhatIfScenario {
	a.mu.Lock()
	defer a.mu.Unlock()

	scenario := &WhatIfScenario{
		Name:           fmt.Sprintf("fail_%s", goalID),
		Changes:        make(map[string]map[string]interface{}),
		ProjectedGoals: []string{},
	}

	scenario.Changes[goalID] = map[string]interface{}{
		"state": runtime.GoalStateFailed,
	}

	if cascade {
		affected := a.getAffectedGoals(goalID)
		scenario.ProjectedGoals = affected

		riskScore := float64(len(affected)) * 0.3
		if riskScore > 1.0 {
			riskScore = 1.0
		}
		scenario.RiskScore = riskScore
	}

	return scenario
}

func (a *WhatIfAnalyzer) SimulateResourceConstraint(resourceID string, capacity float64) *WhatIfScenario {
	a.mu.Lock()
	defer a.mu.Unlock()

	scenario := &WhatIfScenario{
		Name:           fmt.Sprintf("resource_constraint_%s", resourceID),
		Changes:        make(map[string]map[string]interface{}),
		ProjectedGoals: []string{},
		RiskScore:      0.5,
	}

	for goalID, goal := range a.currentGoals {
		if goal.State == runtime.GoalStateActive {
			scenario.Changes[goalID] = map[string]interface{}{
				"state":    runtime.GoalStatePending,
				"resource": resourceID,
			}
			scenario.ProjectedGoals = append(scenario.ProjectedGoals, goalID)
		}
	}

	return scenario
}

func (a *WhatIfAnalyzer) getAffectedGoals(goalID string) []string {
	affected := make([]string, 0)
	visited := make(map[string]bool)

	var dfs func(string)
	dfs = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		node, ok := a.graph.nodes[id]
		if !ok {
			return
		}

		for _, dependent := range node.Dependents {
			affected = append(affected, dependent)
			dfs(dependent)
		}
	}

	dfs(goalID)
	return affected
}

func (a *WhatIfAnalyzer) CompareScenarios(scenarios ...*WhatIfScenario) *WhatIfScenario {
	var best *WhatIfScenario
	bestScore := -1.0

	for _, s := range scenarios {
		if s.RiskScore > bestScore {
			bestScore = s.RiskScore
			best = s
		}
	}

	return best
}

type Planner struct {
	graph        *DependencyGraph
	goals        map[string]*runtime.Goal
	alternatives map[string][]*WhatIfScenario
	mu           sync.RWMutex
}

func NewPlanner() *Planner {
	return &Planner{
		graph:        NewDependencyGraph(),
		goals:        make(map[string]*runtime.Goal),
		alternatives: make(map[string][]*WhatIfScenario),
	}
}

func (p *Planner) AddGoal(goal *runtime.Goal) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.graph.AddNode(goal.ID, []string{}); err != nil {
		return err
	}

	p.goals[goal.ID] = goal
	logging.Info("goal added to planner", logging.Field{"goal_id": goal.ID})

	return nil
}

func (p *Planner) GetExecutionPlan() ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	order, err := p.graph.GetExecutionOrder()
	if err != nil {
		return nil, fmt.Errorf("failed to get execution order: %w", err)
	}

	plan := make([]string, 0)
	for _, goalID := range order {
		if _, exists := p.goals[goalID]; exists {
			plan = append(plan, goalID)
		}
	}

	return plan, nil
}

func (p *Planner) GetCriticalPath() ([]string, int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	path, length, err := p.graph.GetCriticalPath()
	if err != nil {
		return nil, 0, err
	}

	filteredPath := make([]string, 0)
	for _, goalID := range path {
		if _, exists := p.goals[goalID]; exists {
			filteredPath = append(filteredPath, goalID)
		}
	}

	return filteredPath, length, nil
}

func (p *Planner) AnalyzeDependencies() (map[string][]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string][]string)
	for goalID := range p.goals {
		node, exists := p.graph.nodes[goalID]
		if exists {
			result[goalID] = node.Dependees
		} else {
			result[goalID] = []string{}
		}
	}

	return result, nil
}

func (p *Planner) FindParallelizable() ([][]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	executionOrder, err := p.graph.TopologicalSort()
	if err != nil {
		return nil, err
	}

	levels := make(map[string]int)
	for _, goalID := range executionOrder {
		node := p.graph.nodes[goalID]
		if len(node.Dependees) == 0 {
			levels[goalID] = 0
		} else {
			maxLevel := 0
			for _, dep := range node.Dependees {
				if l, ok := levels[dep]; ok && l > maxLevel {
					maxLevel = l
				}
			}
			levels[goalID] = maxLevel + 1
		}
	}

	parallelGroups := make(map[int][]string)
	for goalID, level := range levels {
		parallelGroups[level] = append(parallelGroups[level], goalID)
	}

	result := make([][]string, 0)
	keys := make([]int, 0, len(parallelGroups))
	for k := range parallelGroups {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	for _, k := range keys {
		result = append(result, parallelGroups[k])
	}

	return result, nil
}
