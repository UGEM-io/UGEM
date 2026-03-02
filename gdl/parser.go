// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type TypeKind string

const (
	TypeString TypeKind = "string"
	TypeInt    TypeKind = "int"
	TypeFloat  TypeKind = "float"
	TypeBool   TypeKind = "bool"
	TypeUUID   TypeKind = "uuid"
	TypeEnum   TypeKind = "enum"
	TypeList   TypeKind = "list"
	TypeTime   TypeKind = "time"
)

type Field struct {
	Name     string
	Type     TypeKind
	Optional bool
	Default  interface{}
}

type EnumValue struct {
	Name  string
	Value string
}

type TypeDef struct {
	Name   string
	Kind   TypeKind
	Fields []Field
	Values []EnumValue
}

type EventDef struct {
	Name       string
	ReadPaths  []string
	WritePaths []string
}

type ActionDef struct {
	Name   string
	Input  []Field
	Output []Field
}

type Condition struct {
	Path  string
	Op    string
	Value interface{}
}

type Trigger struct {
	Type string
	Path string
}

type Spawn struct {
	Goal string
	When Condition
}

type GoalDef struct {
	Name      string
	Priority  int
	Condition []Condition
	Trigger   *Trigger
	Spawns    []Spawn
	Actions   []string
}

// ContractDef defines the interface a service app exposes.
type ContractDef struct {
	Name    string
	Events  []string // events this contract exposes
	Actions []string // actions this contract provides
}

// PolicyRule is a single require clause in a policy.
type PolicyRule struct {
	Path  string
	Op    string
	Value interface{}
}

// PolicyDef defines business invariants or compliance rules.
type PolicyDef struct {
	Name  string
	Rules []PolicyRule
}

// TestStep is a single step in a GDL test case.
type TestStep struct {
	Kind  string // "given", "when", "expect"
	Expr  string // raw expression
}

// TestCaseDef defines a GDL-native test case.
type TestCaseDef struct {
	Name  string
	Steps []TestStep
}

type GDLProgram struct {
	Types     []TypeDef
	Events    []EventDef
	Actions   []ActionDef
	Goals     []GoalDef
	Contracts []ContractDef
	Policies  []PolicyDef
	Tests     []TestCaseDef
}

func ParseGDL(input string) (*GDLProgram, error) {
	lines := strings.Split(input, "\n")
	program := &GDLProgram{
		Types:     make([]TypeDef, 0),
		Events:    make([]EventDef, 0),
		Actions:   make([]ActionDef, 0),
		Goals:     make([]GoalDef, 0),
		Contracts: make([]ContractDef, 0),
		Policies:  make([]PolicyDef, 0),
		Tests:     make([]TestCaseDef, 0),
	}

	var currentType TypeDef
	var currentEvent EventDef
	var currentAction ActionDef
	var currentGoal GoalDef
	var currentContract ContractDef
	var currentPolicy PolicyDef
	var currentTest TestCaseDef

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "type ") {
			if currentType.Name != "" {
				program.Types = append(program.Types, currentType)
			}
			currentType = TypeDef{Kind: TypeString}
			name := strings.TrimSpace(strings.TrimPrefix(line, "type "))
			currentType.Name = name
			continue
		}

		if strings.HasPrefix(line, "event ") {
			if currentEvent.Name != "" {
				program.Events = append(program.Events, currentEvent)
			}
			currentEvent = EventDef{}
			parts := strings.Fields(strings.TrimPrefix(line, "event "))
			if len(parts) > 0 {
				currentEvent.Name = parts[0]
			}
			continue
		}

		if strings.HasPrefix(line, "action ") {
			if currentAction.Name != "" {
				program.Actions = append(program.Actions, currentAction)
			}
			currentAction = ActionDef{}
			parts := strings.Fields(strings.TrimPrefix(line, "action "))
			if len(parts) > 0 {
				currentAction.Name = parts[0]
			}
			continue
		}

		if strings.HasPrefix(line, "goal ") {
			if currentGoal.Name != "" {
				program.Goals = append(program.Goals, currentGoal)
			}
			currentGoal = GoalDef{Priority: 5}
			parts := strings.Fields(strings.TrimPrefix(line, "goal "))
			if len(parts) > 0 {
				currentGoal.Name = parts[0]
			}
			continue
		}

		if strings.HasPrefix(line, "contract ") {
			if currentContract.Name != "" {
				program.Contracts = append(program.Contracts, currentContract)
			}
			currentContract = ContractDef{}
			parts := strings.Fields(strings.TrimPrefix(line, "contract "))
			if len(parts) > 0 {
				currentContract.Name = parts[0]
			}
			continue
		}

		if strings.HasPrefix(line, "policy ") {
			if currentPolicy.Name != "" {
				program.Policies = append(program.Policies, currentPolicy)
			}
			currentPolicy = PolicyDef{}
			parts := strings.Fields(strings.TrimPrefix(line, "policy "))
			if len(parts) > 0 {
				currentPolicy.Name = parts[0]
			}
			continue
		}

		if strings.HasPrefix(line, "test ") {
			if currentTest.Name != "" {
				program.Tests = append(program.Tests, currentTest)
			}
			currentTest = TestCaseDef{}
			parts := strings.Fields(strings.TrimPrefix(line, "test "))
			if len(parts) > 0 {
				currentTest.Name = parts[0]
			}
			continue
		}

		if currentType.Name != "" {
			if strings.Contains(line, ":") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					fieldName := strings.TrimSpace(parts[0])
					fieldTypeStr := strings.TrimSpace(parts[1])
					fieldType := TypeKind(fieldTypeStr)
					if strings.HasSuffix(fieldTypeStr, "?") {
						fieldType = TypeKind(strings.TrimSuffix(fieldTypeStr, "?"))
						currentType.Fields = append(currentType.Fields, Field{Name: fieldName, Type: fieldType, Optional: true})
					} else {
						currentType.Fields = append(currentType.Fields, Field{Name: fieldName, Type: fieldType})
					}
				}
			}
		}

		if strings.HasPrefix(line, "priority:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				p, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				currentGoal.Priority = p
			}
		}

		if strings.HasPrefix(line, "trigger:") {
			triggerStr := strings.TrimSpace(strings.TrimPrefix(line, "trigger:"))
			triggerStr = strings.TrimPrefix(triggerStr, "state.")
			triggerStr = strings.TrimPrefix(triggerStr, "event.")
			currentGoal.Trigger = &Trigger{Type: "event", Path: triggerStr}
		}

		if strings.HasPrefix(line, "condition:") {
			expr := strings.TrimSpace(strings.TrimPrefix(line, "condition:"))
			// Simple parser for "path op value"
			for _, op := range []string{"==", ">=", "<=", "!=", ">", "<"} {
				if strings.Contains(expr, op) {
					parts := strings.SplitN(expr, op, 2)
					if len(parts) == 2 {
						path := strings.TrimSpace(parts[0])
						path = strings.TrimPrefix(path, "state.")
						path = strings.TrimPrefix(path, "event.")
						currentGoal.Condition = append(currentGoal.Condition, Condition{
							Path:  path,
							Op:    op,
							Value: strings.TrimSpace(parts[1]),
						})
						break
					}
				}
			}
		}

		if strings.HasPrefix(line, "spawn:") {
			spawnStr := strings.TrimSpace(strings.TrimPrefix(line, "spawn:"))
			// Format: "goal_name when path == value"
			parts := strings.SplitN(spawnStr, " when ", 2)
			if len(parts) == 2 {
				goalName := strings.TrimSpace(parts[0])
				expr := strings.TrimSpace(parts[1])
				for _, op := range []string{"==", ">=", "<=", "!=", ">", "<"} {
					if strings.Contains(expr, op) {
						opParts := strings.SplitN(expr, op, 2)
						if len(opParts) == 2 {
							path := strings.TrimSpace(opParts[0])
							path = strings.TrimPrefix(path, "state.")
							path = strings.TrimPrefix(path, "event.")
							currentGoal.Spawns = append(currentGoal.Spawns, Spawn{
								Goal: goalName,
								When: Condition{
									Path:  path,
									Op:    op,
									Value: strings.TrimSpace(opParts[1]),
								},
							})
							break
						}
					}
				}
			} else {
				// Simple spawn without condition
				currentGoal.Spawns = append(currentGoal.Spawns, Spawn{Goal: spawnStr})
			}
		}

		if strings.HasPrefix(line, "actions:") {
			actionsStr := strings.TrimSpace(strings.TrimPrefix(line, "actions:"))
			actionsStr = strings.Trim(actionsStr, "[]")
			actionsList := strings.Split(actionsStr, ",")
			for i := range actionsList {
				actionsList[i] = strings.TrimSpace(actionsList[i])
			}
			if currentGoal.Name != "" {
				currentGoal.Actions = actionsList
			}
		}

		// Contract body: event and action declarations
		if currentContract.Name != "" {
			if strings.HasPrefix(line, "event ") {
				eventName := strings.TrimSpace(strings.TrimPrefix(line, "event "))
				currentContract.Events = append(currentContract.Events, eventName)
			} else if strings.HasPrefix(line, "action ") {
				actionName := strings.TrimSpace(strings.TrimPrefix(line, "action "))
				currentContract.Actions = append(currentContract.Actions, actionName)
			}
		}

		// Policy body: require clauses
		if currentPolicy.Name != "" && strings.HasPrefix(line, "require ") {
			expr := strings.TrimSpace(strings.TrimPrefix(line, "require "))
			// Parse "path op value" expressions
			for _, op := range []string{"==", ">=", "<=", "!=", ">", "<"} {
				if strings.Contains(expr, op) {
					parts := strings.SplitN(expr, op, 2)
					if len(parts) == 2 {
						currentPolicy.Rules = append(currentPolicy.Rules, PolicyRule{
							Path:  strings.TrimSpace(parts[0]),
							Op:    op,
							Value: strings.TrimSpace(parts[1]),
						})
						break
					}
				}
			}
		}

		// Test body: given/when/expect steps
		if currentTest.Name != "" {
			for _, kw := range []string{"given", "when", "expect"} {
				if strings.HasPrefix(line, kw+" ") {
					expr := strings.TrimSpace(strings.TrimPrefix(line, kw+" "))
					currentTest.Steps = append(currentTest.Steps, TestStep{Kind: kw, Expr: expr})
					break
				}
			}
		}
	}

	if currentType.Name != "" {
		program.Types = append(program.Types, currentType)
	}
	if currentEvent.Name != "" {
		program.Events = append(program.Events, currentEvent)
	}
	if currentAction.Name != "" {
		program.Actions = append(program.Actions, currentAction)
	}
	if currentGoal.Name != "" {
		program.Goals = append(program.Goals, currentGoal)
	}
	if currentContract.Name != "" {
		program.Contracts = append(program.Contracts, currentContract)
	}
	if currentPolicy.Name != "" {
		program.Policies = append(program.Policies, currentPolicy)
	}
	if currentTest.Name != "" {
		program.Tests = append(program.Tests, currentTest)
	}

	return program, nil
}

func init() {
	_ = regexp.MustCompile("")
}

// ParseGDLFile reads a .gdl file from disk and parses it.
func ParseGDLFile(path string) (*GDLProgram, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return ParseGDL(string(data))
}

// MergePrograms combines multiple GDLPrograms into one by appending
// all types, events, actions, and goals.
func MergePrograms(programs ...*GDLProgram) *GDLProgram {
	merged := &GDLProgram{
		Types:     make([]TypeDef, 0),
		Events:    make([]EventDef, 0),
		Actions:   make([]ActionDef, 0),
		Goals:     make([]GoalDef, 0),
		Contracts: make([]ContractDef, 0),
		Policies:  make([]PolicyDef, 0),
		Tests:     make([]TestCaseDef, 0),
	}

	for _, p := range programs {
		if p == nil {
			continue
		}
		merged.Types = append(merged.Types, p.Types...)
		merged.Events = append(merged.Events, p.Events...)
		merged.Actions = append(merged.Actions, p.Actions...)
		merged.Goals = append(merged.Goals, p.Goals...)
		merged.Contracts = append(merged.Contracts, p.Contracts...)
		merged.Policies = append(merged.Policies, p.Policies...)
		merged.Tests = append(merged.Tests, p.Tests...)
	}

	return merged
}
