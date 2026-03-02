// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ugem-io/ugem/runtime"
)

type Compiler struct {
	program *GDLProgram
	types   map[string]runtime.TypedValue
	errors  []string
}

func NewCompiler(program *GDLProgram) *Compiler {
	return &Compiler{
		program: program,
		types:   make(map[string]runtime.TypedValue),
		errors:  make([]string, 0),
	}
}

func (c *Compiler) Compile() (*CompiledProgram, error) {
	compiled := &CompiledProgram{
		Goals:   make([]runtime.Goal, 0),
		Events:  make([]runtime.Event, 0),
		Actions: make(map[string]func(ctx runtime.ActionContext) (map[string]interface{}, error)),
	}

	for _, typ := range c.program.Types {
		c.types[typ.Name] = runtime.TypedValue{Type: string(typ.Kind), Value: nil}
	}

	for _, event := range c.program.Events {
		compiled.Events = append(compiled.Events, c.compileEvent(event))
	}

	for _, action := range c.program.Actions {
		compiled.Actions[action.Name] = c.compileAction(action)
	}

	for _, goal := range c.program.Goals {
		compiled.Goals = append(compiled.Goals, c.compileGoal(goal))
	}

	if len(c.errors) > 0 {
		return compiled, fmt.Errorf("compilation errors:\n%s", strings.Join(c.errors, "\n"))
	}

	return compiled, nil
}

func (c *Compiler) compileEvent(event EventDef) runtime.Event {
	return runtime.Event{
		Type:       event.Name,
		WritePaths: toPaths(event.WritePaths),
		ReadPaths:  toPaths(event.ReadPaths),
		Payload:    make(map[string]interface{}),
	}
}

func toPaths(paths []string) []runtime.Path {
	result := make([]runtime.Path, len(paths))
	for i, p := range paths {
		result[i] = runtime.Path(p)
	}
	return result
}

func (c *Compiler) compileAction(action ActionDef) func(ctx runtime.ActionContext) (map[string]interface{}, error) {
	return func(ctx runtime.ActionContext) (map[string]interface{}, error) {
		return map[string]interface{}{
			"success": true,
		}, nil
	}
}

func (c *Compiler) compileGoal(goal GoalDef) runtime.Goal {
	g := runtime.Goal{
		ID:       goal.Name,
		Priority: goal.Priority,
		State:    runtime.GoalStateActive,
	}

	if len(goal.Condition) > 0 {
		conds := goal.Condition
		g.Condition = func(state runtime.State) bool {
			return evaluateConditions(conds, state)
		}
	}

	if len(goal.Spawns) > 0 {
		spawns := goal.Spawns
		g.Spawn = func(state runtime.State) []runtime.Goal {
			result := make([]runtime.Goal, 0)
			for _, s := range spawns {
				spawnGoal := runtime.Goal{
					ID:       s.Goal,
					Priority: 5,
					State:    runtime.GoalStatePending,
				}
				if s.When.Path != "" {
					path := s.When.Path
					val := s.When.Value
					op := s.When.Op
					spawnGoal.Condition = func(state runtime.State) bool {
						v, ok := state.Get(runtime.Path(path))
						if !ok {
							return false
						}
						return compareValues(v.Value, op, val)
					}
				}
				result = append(result, spawnGoal)
			}
			return result
		}
	}

	if goal.Trigger != nil {
		path := runtime.Path(goal.Trigger.Path)
		g.Trigger = runtime.GoalTrigger{
			Type: goal.Trigger.Type,
			Path: &path,
		}
	}

	return g
}

func evaluateConditions(conditions []Condition, state runtime.State) bool {
	for _, cond := range conditions {
		val, exists := state.Get(runtime.Path(cond.Path))
		if !exists {
			return false
		}

		if !compareValues(val.Value, cond.Op, cond.Value) {
			return false
		}
	}
	return true
}

func compareValues(actual interface{}, op string, target interface{}) bool {
	actualStr := fmt.Sprintf("%v", actual)
	targetStr := fmt.Sprintf("%v", target)

	switch op {
	case "==":
		return actualStr == targetStr
	case "!=":
		return actualStr != targetStr
	case ">":
		iv1, err1 := strconv.ParseFloat(actualStr, 64)
		iv2, err2 := strconv.ParseFloat(targetStr, 64)
		return err1 == nil && err2 == nil && iv1 > iv2
	case "<":
		iv1, err1 := strconv.ParseFloat(actualStr, 64)
		iv2, err2 := strconv.ParseFloat(targetStr, 64)
		return err1 == nil && err2 == nil && iv1 < iv2
	case ">=":
		iv1, err1 := strconv.ParseFloat(actualStr, 64)
		iv2, err2 := strconv.ParseFloat(targetStr, 64)
		return err1 == nil && err2 == nil && iv1 >= iv2
	case "<=":
		iv1, err1 := strconv.ParseFloat(actualStr, 64)
		iv2, err2 := strconv.ParseFloat(targetStr, 64)
		return err1 == nil && err2 == nil && iv1 <= iv2
	default:
		return false
	}
}

type CompiledProgram struct {
	Goals   []runtime.Goal
	Events  []runtime.Event
	Actions map[string]func(ctx runtime.ActionContext) (map[string]interface{}, error)
}

func CompileGDL(input string) (*CompiledProgram, error) {
	program, err := ParseGDL(input)
	if err != nil {
		return nil, err
	}

	compiler := NewCompiler(program)
	return compiler.Compile()
}

func (c *CompiledProgram) CreateRuntime(rt *runtime.GoalRuntime) error {
	for name, action := range c.Actions {
		rt.GetPlanner().RegisterAction(name, func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
			return action(ctx)
		})
	}

	for _, goal := range c.Goals {
		if err := rt.SubmitGoal(goal); err != nil {
			return err
		}
	}

	return nil
}

func ParseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func ParseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
