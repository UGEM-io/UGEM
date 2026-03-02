# Goal Runtime

A deterministic, event-sourced execution kernel for goal-driven software. Instead of writing procedural flows, you declare **what you want** (Goals) and the runtime figures out **how to get there** through events, state mutations, and actions.

## Why Goal Runtime?

Traditional backend architectures scatter logic across controllers, services, queues, and cron jobs. Goal Runtime replaces all of that with a single execution model:

```
Goals → Conditions → Plans → Actions → Events → State
```

**Use Cases:**

- **Workflow Orchestration** — Multi-step business processes (onboarding, payments, approvals) expressed as goals with conditions, not imperative code
- **Autonomous Agents** — AI agents that pursue objectives, re-plan on failure, and coordinate through shared state
- **Event-Driven Systems** — Replace scattered event handlers with declarative goal conditions that react to state changes
- **Deterministic Simulation** — Full replay capability from event logs for debugging, testing, and time-travel analysis
- **Distributed Task Coordination** — Goals partitioned across nodes with consistent scheduling and automatic failover

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    Goal Runtime                      │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌───────────────────┐  │
│  │   Goal   │  │  Event   │  │      State        │  │
│  │  Engine  │  │   Log    │  │     Manager       │  │
│  └────┬─────┘  └────┬─────┘  └────────┬──────────┘  │
│       │              │                 │             │
│  ┌────┴─────┐  ┌────┴─────┐  ┌────────┴──────────┐  │
│  │ Scheduler│  │  Event   │  │     Planner       │  │
│  │          │  │   Bus    │  │                    │  │
│  └──────────┘  └──────────┘  └────────┬──────────┘  │
│                                       │             │
│                              ┌────────┴──────────┐  │
│                              │ Action Dispatcher  │  │
│                              │  (async workers)   │  │
│                              └───────────────────┘  │
└─────────────────────────────────────────────────────┘
         │                │                │
    ┌────┴────┐     ┌────┴────┐     ┌─────┴─────┐
    │  gRPC   │     │  HTTP   │     │    CLI    │
    │ Server  │     │ Server  │     │           │
    └─────────┘     └─────────┘     └───────────┘
```

### Core Components

| Component | Responsibility |
|-----------|---------------|
| **Goal Engine** | Manages goal lifecycle (Pending → Active → Complete/Failed/Cancelled) |
| **State Manager** | Global state graph with path-level locking and MVCC snapshots |
| **Event Log** | Append-only sequential event store with logical clock ordering |
| **Scheduler** | Deterministic priority-based execution ordering |
| **Planner** | Computes minimal action sequences to satisfy goal conditions |
| **Action Dispatcher** | Async execution with retries, backoff, and concurrency control |
| **Event Bus** | Pub/sub event delivery with subscriber filtering |

### Supporting Modules

| Module | Description |
|--------|-------------|
| **GDL** | Goal Definition Language — declarative syntax for defining goals, events, and types |
| **Distributed** | Cluster coordination, consistent hashing, and goal partitioning |
| **Storage** | Write-Ahead Log (WAL) and snapshot store for durability |
| **Security** | Authentication, session management, and role-based access control |
| **Observability** | Health checks, metrics collection, and distributed tracing |

## Getting Started

### Prerequisites

- Go 1.24+

### Installation

```bash
git clone https://github.com/sumeet/UGEM.git
cd goal_method
go build ./...
```

### Running

Goal Runtime supports three modes:

```bash
# Server mode — starts gRPC + HTTP servers (production)
go run cmd/main.go -mode server

# Standalone mode — embedded runtime with interactive CLI (development)
go run cmd/main.go -mode standalone

# Client mode — connects to a running server
go run cmd/main.go -mode client
```

#### Configuration Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-mode` | `server` | Run mode: `server`, `client`, or `standalone` |
| `-http` | `:8080` | HTTP server listen address |
| `-grpc` | `:50051` | gRPC server listen address |
| `-log` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### Running Tests

```bash
# Full test suite
go test ./...

# With race detector
go test ./runtime/... -race

# Benchmarks
go test ./runtime/... -bench=. -benchtime=1s
```

## Writing Software with GDL

GDL (Goal Definition Language) is the primary interface for building on Goal Runtime. It uses a Django-inspired **workspace structure** to manage complexity, split concerns across apps, and enforce business invariants through policies.

### 1. Initialize a Project

```bash
go run cmd/main.go init myproject
cd myproject
```

This scaffolds a robust project structure:
```
myproject/
├── goalruntime.yaml      # Project configuration
├── apps/
│   ├── orders/          # "orders" application
│   │   ├── types.gdl     # Domain types
│   │   ├── events.gdl    # Event declarations
│   │   ├── goals.gdl     # Goal definitions
│   │   ├── contracts.gdl # Public interfaces
│   │   └── policies.gdl  # App-specific invariants
│   └── payments/
└── shared/              # Shared types & global policies
└── tests/               # Native GDL system tests
```

### 2. Define Public Contracts

Contracts define the formal interface between apps, preventing tight coupling in large systems.

```gdl
# apps/payments/contracts.gdl
contract PaymentService {
   event payment.completed
   action payment.charge
}
```

### 3. Enforce Business Policies

Policies define global business invariants, security rules, and regulatory compliance that are enforced across all goals.

```gdl
# apps/orders/policies.gdl
policy order_validation {
   require order.amount > 0
   require customer.verified == true
}
```

### 4. Native GDL Testing

Goal Runtime supports native system testing using `given/when/expect` blocks for deterministic simulation.

```gdl
# tests/order_flow_test.gdl
test successful_checkout {
   given user.balance = 500
   when order.create(amount=200)
   expect event payment.completed
   expect goal fulfillment.active
}
```

### The Mental Model

| Layer | Responsibility | Purpose |
|-------|----------------|---------|
| **Types** | Domain Data | Defines what your data looks like (`struct`) |
| **Events** | State Change | Declares what happened in the system |
| **Goals** | Desired Outcome | Declares **what** the system should achieve |
| **Contracts** | Interfaces | Defines how apps interact (Events + Actions) |
| **Policies** | Invariants | Global rules that goals cannot violate |
| **Tests** | Verification | Deterministic simulation of goal flows |

---

### Built-in Action Types

| Action | Description |
|--------|-------------|
| `http.call` | HTTP requests to external services |
| `db.query` | Database queries |
| `email.send` | Send emails |
| `payment.charge` | Process payments |
| `ai.call` | Invoke AI/LLM APIs |
| `notification.push` | Push notifications |

Custom actions can be registered via the Go API (see below).

---

### Embedding & Extending (Go API)

The Go API is for **embedding** the kernel in a larger Go application or **registering custom action handlers**:

```go
rt := runtime.NewRuntime(runtime.SchedulerModeNormal)
rt.Start()

// Register a custom action that GDL goals can reference
rt.GetPlanner().RegisterAction("payment.charge", func(input map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
    return map[string]interface{}{"charged": true}, nil
})
```

### CLI Commands

When running in `standalone` or `client` mode:

| Command | Description |
|---------|-------------|
| `list` | View all goals and their current states |
| `submit <name> [key=val ...]` | Create a new goal with optional metadata |
| `get <id>` | Inspect a goal's state, trace, and history |
| `cancel <id>` | Cancel a running goal |
| `metrics` | View runtime performance metrics |
| `health` | Check system health status |
| `stream [id]` | Stream real-time events |
| `gdl` | Reload and re-run current GDL workspace |
| `exit` | Shut down the runtime |
| `query <UQL>` | Execute UUGEM Query Language (UQL) search |
| `rewind <T>` | Time-travel state to specific timestamp |
| `fork <name>` | Create isolated simulation branch |

---

## Plugin System

Goal Runtime features a robust plugin architecture that allows extending the kernel with external storage, notification providers, and AI services while maintaining full determinism.

### Default Plugins

| Plugin | Type | Description | Actions |
|--------|------|-------------|---------|
| **LocalFS** | Storage | Deterministic file storage on local disk | `file.upload`, `file.delete` |
| **Console** | Notify | Real-time system notifications to stdout | `notify.send` |
| **AI** | Generic | Orchestration for LLM (OpenAI/Gemini) calls | `ai.process`, `ai.summarize` |

### Deterministic Files

UGEM Core manages file **identity** (metadata + hashes) in the state store, while plugins manage the **bytes**. Replay validation ensures that even if external storage changes, the system can detect state/blob divergence.

### Configuration (Env)

Plugins are configured via environment variables:
- `UGEM_STORAGE_BASE_DIR` — Root directory for `LocalFS`.
- `OPENAI_API_KEY` — API key for the AI plugin.

### Registering Custom Plugins (Go)

```go
type MyPlugin struct{}
func (p *MyPlugin) Name() string { return "custom" }
func (p *MyPlugin) Init(ctx context.Context, cfg map[string]string) error { return nil }
func (p *MyPlugin) Actions() map[string]runtime.ActionHandler {
    return map[string]runtime.ActionHandler{
        "custom.do": func(in map[string]interface{}, ctx runtime.ActionContext) (map[string]interface{}, error) {
            return map[string]interface{}{"ok": true}, nil
        },
    }
}

rt.RegisterPlugin(&MyPlugin{}, nil)
```

---

---

## Project Structure

```
goalruntime/
├── cmd/              # CLI entrypoint (server, client, standalone, init)
├── runtime/          # Core execution kernel
│   ├── kernel.go         # Evaluation loop & event submission
│   ├── state_manager.go  # State graph & snapshots
│   ├── event_log.go      # sequential event store
│   ├── goal_engine.go    # Goal lifecycle
│   └── scheduler.go      # Deterministic scheduling
├── gdl/              # GDL parser, compiler, & workspace loader
├── grpc/             # gRPC server & protobufs
├── http/             # HTTP/REST server
├── distributed/      # Cluster coordination
├── storage/          # WAL & persistence
├── security/         # Auth & RBAC
└── observability/    # Metrics, health, & tracing
```

