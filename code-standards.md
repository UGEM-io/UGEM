# UGEM Code Standards

> This document outlines the coding standards, conventions, and best practices for contributing to UGEM.

---

## Philosophy

UGEM is built on three core principles:

| Principle | Description |
|-----------|-------------|
| 🎯 **Determinism** | Code must produce predictable, reproducible results |
| 🔒 **Safety** | Prefer explicit error handling over runtime panics |
| 📖 **Clarity** | Code is read more than written — optimize for readability |

---

## General Guidelines

### Code Style

- ✅ Run `go fmt` before every commit
- ✅ Run `go vet` to catch common issues
- ✅ Use `golint` for style recommendations
- ✅ Keep lines under **100 characters** when reasonable
- ✅ Use **2 spaces** for indentation (Go standard)

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Packages | lowercase, short | `runtime`, `gdl`, `planning` |
| Functions | PascalCase, descriptive | `ResolveGoal()`, `ExecuteAction()` |
| Variables | camelCase, meaningful | `goalState`, `eventLog` |
| Constants | PascalCase or SCREAMING_SNAKE | `MaxRetries`, `DEFAULT_TIMEOUT` |
| Interfaces | PascalCase, ends with -er | `Planner`, `Dispatcher` |
| Files | lowercase, snake_case | `event_bus.go`, `types.go` |

### Function Design

```go
// ✅ Good: Small, focused, well-named
func (e *Engine) ResolveGoal(goal *Goal) error {
    if goal == nil {
        return ErrNilGoal
    }
    return e.planner.CreatePlan(goal)
}

// ❌ Bad: Does too much, unclear purpose
func process(x string) {
    // ... 200 lines of code
}
```

---

## Documentation

### Package Documentation

Every package must have a doc comment:

```go
// Package runtime provides the core execution engine for UGEM.
// It handles goal resolution, event processing, and action dispatching.
package runtime
```

### Function Documentation

Public functions require doc comments:

```go
// ExecuteAction runs the specified action and records the result.
// Returns an error if the action fails or validation fails.
func (d *Dispatcher) ExecuteAction(action *Action) error
```

### Comment Style

- Use **full sentences** for documentation
- Use **// comments** for implementation notes
- Prefix with **TODO:** for tracking future work

```go
// CalculateNextState derives the new state from the current state
// and the given event. This is deterministic by design.
func CalculateNextState(state *State, event *Event) (*State, error)
```

---

## Error Handling

### Rules

| Rule | Example |
|------|---------|
| ✅ Return errors explicitly | `return nil, ErrNotFound` |
| ✅ Wrap errors with context | `return nil, fmt.Errorf("parse goal: %w", err)` |
| ✅ Use sentinel errors | `var ErrGoalNotFound = errors.New("goal not found")` |
| ❌ Don't ignore errors | `_ = doSomething()` |
| ❌ Avoid panic in production | `panic("should not happen")` |

### Error Definition Pattern

```go
package gdl

import "errors"

// Sentinel errors for the GDL package
var (
    ErrParseFailed   = errors.New("parse failed")
    ErrTypeNotFound  = errors.New("type not found")
    ErrInvalidGoal   = errors.New("invalid goal")
)
```

---

## Testing

### Requirements

- ✅ All public functions must have tests
- ✅ Use table-driven tests for multiple scenarios
- ✅ Name tests descriptively: `Test<Function>_<Scenario>_<Expected>`
- ✅ Use `t.Helper()` for test utilities

### Test Structure

```go
func TestGoalEngine_ResolveGoal_Success(t *testing.T) {
    // Arrange
    engine := NewGoalEngine()
    goal := &Goal{ID: "test-1", Condition: "status == PAID"}

    // Act
    err := engine.ResolveGoal(goal)

    // Assert
    assert.NoError(t, err)
    assert.Equal(t, StateResolved, goal.State)
}

func TestGoalEngine_ResolveGoal_InvalidGoal(t *testing.T) {
    tests := []struct {
        name    string
        goal    *Goal
        wantErr error
    }{
        {"nil goal", nil, ErrNilGoal},
        {"empty id", &Goal{ID: ""}, ErrInvalidGoal},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            engine := NewGoalEngine()
            err := engine.ResolveGoal(tt.goal)
            assert.ErrorIs(t, err, tt.wantErr)
        })
    }
}
```

### Test File Naming

| File | Test File |
|------|-----------|
| `runtime/kernel.go` | `runtime/kernel_test.go` |
| `gdl/parser.go` | `gdl/gdl_test.go` |

---

## Git Workflow

### Branch Naming

```
<type>/<description>

Examples:
├── feat/add-goal-partitioning
├── fix/event-log-corruption
├── docs/readme-updates
├── refactor/scheduler-optimization
├── test/add-e2e-scenarios
```

### Commit Messages

Follow **Conventional Commits**:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

| Type | Description |
|------|-------------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation |
| `style` | Formatting |
| `refactor` | Code refactoring |
| `test` | Tests |
| `chore` | Maintenance |

### Examples

```bash
feat(gdl): add support for nested type definitions

Fixes #42
Co-authored-by: Jane Doe <jane@example.com>
```

```bash
fix(runtime): prevent race condition in event bus

The event bus was not thread-safe when processing concurrent
events. Added mutex protection to the publish method.
```

---

## Code Review Checklist

### Before Submitting

- [ ] Code compiles without errors
- [ ] All tests pass (`go test ./...`)
- [ ] `go fmt` has been run
- [ ] `go vet` reports no issues
- [ ] New functions have doc comments
- [ ] Edge cases are handled
- [ ] No hardcoded secrets or credentials

### Review Criteria

| Area | Check |
|------|-------|
| 🔍 **Correctness** | Does the code do what it claims? |
| 🧪 **Tests** | Are tests comprehensive? |
| 📖 **Readability** | Is the code easy to understand? |
| ⚡ **Performance** | Any obvious inefficiencies? |
| 🔒 **Security** | Any security vulnerabilities? |
| 📦 **Coupling** | Is the change properly scoped? |

---

## Project Structure

```
ugem/
├── cmd/              # Entry points
├── gdl/              # Goal Definition Language
├── runtime/          # Core execution engine
├── planning/         # Goal planning subsystem
├── distributed/      # Distributed coordination
├── storage/          # Persistence layer
├── http/             # HTTP server
├── grpc/             # gRPC service
├── logging/          # Logging utilities
├── observability/    # Metrics & tracing
├── plugins/          # Plugin system
├── security/         # Security utilities
└── demo/             # Demo applications
```

---

## Dependencies

### Adding Dependencies

1. Only add dependencies when necessary
2. Prefer standard library over external packages
3. Keep dependency list updated: `go mod tidy`
4. Document why each dependency is needed

### Prohibited

- ❌ Unlicensed dependencies
- ❌ Dependencies with known security vulnerabilities
- ❌ Heavy frameworks when simple solutions suffice

---

## Security

### Never Hardcode

```go
// ❌ Bad
apiKey := "sk-1234567890abcdef"

// ✅ Good
apiKey := os.Getenv("API_KEY")
```

### Input Validation

```go
func NewGoal(id, condition string) (*Goal, error) {
    if id == "" {
        return nil, ErrEmptyGoalID
    }
    if condition == "" {
        return nil, ErrEmptyCondition
    }
    return &Goal{ID: id, Condition: condition}, nil
}
```

---

## Performance

### Guidelines

- ✅ Profile before optimizing
- ✅ Use `sync.Pool` for frequent allocations
- ✅ Batch database operations when possible
- ✅ Use buffered channels for high-throughput scenarios

### Benchmarking

```go
func BenchmarkResolveGoal(b *testing.B) {
    engine := NewGoalEngine()
    goal := &Goal{ID: "bench-goal", Condition: "status == ACTIVE"}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = engine.ResolveGoal(goal)
    }
}
```

---

<div align="center">

**Remember: Code is read far more often than it is written. Write for the reader.**

</div>
