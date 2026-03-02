package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ugem-io/ugem/distributed"
	"github.com/ugem-io/ugem/gdl"
	"github.com/ugem-io/ugem/grpc"
	ghttp "github.com/ugem-io/ugem/http"
	"github.com/ugem-io/ugem/logging"
	"github.com/ugem-io/ugem/observability"
	"github.com/ugem-io/ugem/runtime"
	"github.com/ugem-io/ugem/security"
	gstorage "github.com/ugem-io/ugem/storage"
	"github.com/ugem-io/ugem/plugins/storage"
	"github.com/ugem-io/ugem/plugins/notification"
	"github.com/ugem-io/ugem/plugins/ai"
)

var (
	httpAddr   = flag.String("http", ":8080", "HTTP server address")
	grpcAddr   = flag.String("grpc", ":50051", "gRPC server address")
	logLevel   = flag.String("log", "info", "Log level (debug, info, warn, error)")
	runMode    = flag.String("mode", "server", "Run mode (server, client, standalone)")
	projectDir = flag.String("project", "", "Path to GDL project directory (auto-detected if not set)")
	dataDir    = flag.String("data", "data", "Directory for persistent state storage")
)

func main() {
	flag.CommandLine.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nCommands (for mode=client or mode=standalone):\n")
		fmt.Fprintf(os.Stderr, "  list, submit, get, cancel, metrics, health, stream, help, exit\n")
		fmt.Fprintf(os.Stderr, "\nSubcommands:\n")
		fmt.Fprintf(os.Stderr, "  init <name>  Scaffold a new GDL project\n")
	}
	flag.Parse()

	// Handle init subcommand before mode switch
	if len(flag.Args()) > 0 && flag.Args()[0] == "init" {
		runInit(flag.Args()[1:])
		return
	}

	logging.Init(logging.Level(*logLevel), "goal-runtime")

	switch *runMode {
	case "server":
		runServer()
	case "client":
		runClient()
	case "standalone":
		runStandalone()
	default:
		fmt.Println("Unknown mode:", *runMode)
		os.Exit(1)
	}
}

func runInit(args []string) {
	name := "myproject"
	if len(args) > 0 {
		name = args[0]
	}

	if err := gdl.InitProject(name, name); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating project: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created project '%s'\n", name)
	fmt.Println("")
	fmt.Println("  Structure:")
	fmt.Printf("    %s/\n", name)
	fmt.Println("    ├── goalruntime.yaml")
	fmt.Println("    ├── apps/")
	fmt.Println("    │   └── main/")
	fmt.Println("    │       ├── types.gdl")
	fmt.Println("    │       ├── events.gdl")
	fmt.Println("    │       └── goals.gdl")
	fmt.Println("    └── shared/")
	fmt.Println("        └── types.gdl")
	fmt.Println("")
	fmt.Println("  Next steps:")
	fmt.Printf("    cd %s\n", name)
	fmt.Println("    goalruntime -mode standalone")
}

func loadWorkspace(rt *runtime.GoalRuntime) {
	dir, err := gdl.ResolveProjectDir(*projectDir)
	if err != nil {
		// No project found is fine — run without GDL
		logging.Info("No GDL project found, running without workspace", logging.Field{"hint": "use 'init <name>' to create one"})
		// Register built-in actions anyway
		gdl.RegisterAllActions(rt.GetPlanner())
		return
	}

	ws, err := gdl.LoadWorkspace(dir)
	if err != nil {
		logging.Error("Failed to load workspace", logging.Field{"error": err.Error(), "dir": dir})
		os.Exit(1)
	}

	logging.Info("Loaded workspace", logging.Field{"project": ws.Config.Name, "apps": len(ws.Apps)})
	fmt.Print(ws.Summary())

	// Register built-in actions
	gdl.RegisterAllActions(rt.GetPlanner())

	// Apply workspace (compile + register goals)
	if err := ws.Apply(rt); err != nil {
		logging.Error("Failed to apply workspace", logging.Field{"error": err.Error()})
		os.Exit(1)
	}
}

func runServer() {
	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)

	if *dataDir != "" {
		pss, err := gstorage.NewPersistentStore(*dataDir)
		if err != nil {
			logging.Error("Failed to initialize PSS", logging.Field{"error": err.Error(), "dir": *dataDir})
			os.Exit(1)
		}
		rt.SetPersistence(pss)
	}
	registerDefaultPlugins(rt)

	if err := rt.Start(); err != nil {
		logging.Error("Failed to start runtime", logging.Field{"error": err.Error()})
		os.Exit(1)
	}

	loadWorkspace(rt)

	observer := observability.NewObserver(rt)
	observer.EnableTracing()

	var grpcPort int
	fmt.Sscanf(strings.TrimPrefix(*grpcAddr, ":"), "%d", &grpcPort)
	if grpcPort == 0 {
		grpcPort = 50051
	}

	grpcServer := grpc.NewServer(rt, grpc.WithPort(grpcPort))
	if err := grpcServer.Start(); err != nil {
		logging.Error("Failed to start gRPC server", logging.Field{"error": err.Error(), "port": grpcPort})
	}

	httpServer := ghttp.NewServer(rt, ghttp.WithHTTPAddr(*httpAddr))
	if err := httpServer.Start(); err != nil {
		logging.Error("Failed to start HTTP server", logging.Field{"error": err.Error()})
	}

	logging.Info("Servers started", logging.Field{
		"http": *httpAddr,
		"grpc": *grpcAddr,
	})

	waitForShutdown(rt, grpcServer, httpServer, observer)
}

func runClient() {
	client, err := ghttp.NewGRPCClient(*grpcAddr)
	if err != nil {
		logging.Error("Failed to connect to server", logging.Field{"error": err.Error()})
		os.Exit(1)
	}
	defer client.Close()

	if len(flag.Args()) > 0 {
		handleClientCommand(client, flag.Args()[0], flag.Args()[1:])
		return
	}

	fmt.Println("Connected to server at", *grpcAddr)
	fmt.Println("Type 'help' for available commands, 'exit' to quit")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		if !handleClientCommand(client, parts[0], parts[1:]) {
			return
		}
	}
}

func handleClientCommand(client *ghttp.GRPCClient, cmd string, args []string) bool {
	switch strings.ToLower(cmd) {
	case "submit":
		if len(args) == 0 {
			fmt.Println("Usage: submit <name> [key=value...]")
			return true
		}
		name := args[0]
		metadata := make(map[string]string)
		for _, arg := range args[1:] {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) == 2 {
				metadata[parts[0]] = parts[1]
			}
		}
		goalID, err := client.SubmitGoal(name, "", 0, metadata)
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Println("Goal created:", goalID)
		}
	case "list", "goals":
		goals, err := client.ListGoals()
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			printGoalsTable(goals)
		}
	case "get":
		var id string
		if len(args) > 0 {
			id = args[0]
		} else {
			fmt.Print("Goal ID: ")
			fmt.Scanln(&id)
		}
		goal, err := client.GetGoal(id)
		if err != nil {
			fmt.Println("Error:", err)
		} else if goal == nil {
			fmt.Println("Goal not found")
		} else {
			printGoal(goal)
		}
	case "cancel":
		var id string
		if len(args) > 0 {
			id = args[0]
		} else {
			fmt.Print("Goal ID: ")
			fmt.Scanln(&id)
		}
		if err := client.CancelGoal(id, "cancelled by user"); err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Println("Goal cancelled")
		}
	case "metrics":
		metrics, err := client.GetMetrics()
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Printf("Metrics:\n")
			fmt.Printf("  Total Goals: %d\n", metrics.TotalGoals)
			fmt.Printf("  Active Goals: %d\n", metrics.ActiveGoals)
			fmt.Printf("  Completed Goals: %d\n", metrics.CompletedGoals)
			fmt.Printf("  Failed Goals: %d\n", metrics.FailedGoals)
			fmt.Printf("  Actions Executed: %d\n", metrics.ActionsExecuted)
		}
	case "health":
		health, err := client.HealthCheck()
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Printf("Health: %v\n", health.Healthy)
			fmt.Printf("Version: %s\n", health.Version)
			fmt.Printf("Uptime: %d seconds\n", health.Uptime)
		}
	case "stream":
		var goalID string
		if len(args) > 0 {
			goalID = args[0]
		}
		stream, err := client.StreamEvents(goalID)
		if err != nil {
			fmt.Println("Error:", err)
			return true
		}
		fmt.Println("Streaming events (Ctrl+C to stop)...")
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Println("Stream error:", err)
				break
			}
			fmt.Printf("[%d] %s: %s\n", event.Timestamp, event.Type, event.Payload)
		}
	case "help":
		printClientHelp()
	case "exit", "quit":
		return false
	default:
		fmt.Println("Unknown command. Type 'help' for available commands.")
	}
	return true
}

func runStandalone() {
	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)

	if *dataDir != "" {
		pss, err := gstorage.NewPersistentStore(*dataDir)
		if err != nil {
			logging.Error("Failed to initialize PSS", logging.Field{"error": err.Error(), "dir": *dataDir})
			os.Exit(1)
		}
		rt.SetPersistence(pss)
	}

	registerDefaultPlugins(rt)

	if err := rt.Start(); err != nil {
		logging.Error("Failed to start runtime", logging.Field{"error": err.Error()})
		os.Exit(1)
	}

	loadWorkspace(rt)

	auth := security.NewAuthenticator()
	auth.AddUser("admin", "password", []string{"admin"})
	auth.AddRole("admin", []string{"read", "write", "delete", "execute", "admin"})

	cluster := distributed.NewCluster("node-1", "localhost:50051", rt)
	_ = cluster

	observer := observability.NewObserver(rt)
	_ = observer

	uqlEngine := gdl.NewUQLEngine(rt.GetPSS())
	_ = uqlEngine

	fmt.Println("Goal Runtime Standalone Mode")
	fmt.Println("================================")
	fmt.Println("Type 'help' for commands, 'exit' to quit")

	if len(flag.Args()) > 0 {
		handleStandaloneCommand(rt, observer, uqlEngine, flag.Args()[0], flag.Args()[1:])
		rt.Stop()
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		if !handleStandaloneCommand(rt, observer, uqlEngine, parts[0], parts[1:]) {
			break
		}
	}
	rt.Stop()
}

func handleStandaloneCommand(rt *runtime.GoalRuntime, observer *observability.Observer, uqlEngine *gdl.UQLEngine, cmd string, args []string) bool {
	switch strings.ToLower(cmd) {
	case "query":
		if len(args) == 0 {
			fmt.Println("Usage: query <UQL statement>")
			return true
		}
		uql := strings.Join(args, " ")
		results, err := uqlEngine.Execute(uql)
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Printf("Results (%d found):\n", len(results))
			for _, r := range results {
				fmt.Printf("  - %v\n", r)
			}
		}
	case "help":
		printStandaloneHelp()
	case "list", "goals":
		goals := rt.GetGoalEngine().ListGoals()
		fmt.Printf("Total goals: %d\n", len(goals))
		for _, g := range goals {
			fmt.Printf("  - %s: %s (priority: %d)\n", g.ID, g.State, g.Priority)
			if len(g.Metadata) > 0 {
				fmt.Printf("    Metadata: %v\n", g.Metadata)
			}
		}
	case "submit":
		if len(args) == 0 {
			fmt.Println("Usage: submit <name> [key=value...]")
			return true
		}
		name := args[0]
		metadata := make(map[string]string)
		for _, arg := range args[1:] {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) == 2 {
				metadata[parts[0]] = parts[1]
			}
		}
		id := name
		if id == "" {
			id = fmt.Sprintf("goal-%d", time.Now().UnixNano())
		}
		goal := runtime.Goal{
			ID:        id,
			Priority:  0,
			Metadata:  metadata,
			State:     runtime.GoalStatePending,
			CreatedAt: time.Now(),
		}
		if err := rt.SubmitGoal(goal); err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Println("Goal submitted:", goal.ID)
		}
	case "rewind":
		if len(args) == 0 {
			fmt.Println("Usage: rewind <timestamp (RFC3339)>")
			return true
		}
		t, err := time.Parse(time.RFC3339, args[0])
		if err != nil {
			fmt.Println("Error: invalid timestamp format. Use RFC3339 (e.g., 2026-03-02T17:00:00Z)")
			return true
		}
		if err := rt.Rewind(t); err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Println("Rewind successful. State reconstructed.")
		}
	case "replay":
		events := rt.GetEventLog().GetEvents()
		fmt.Printf("Replaying %d events:\n", len(events))
		for _, e := range events {
			fmt.Printf("  [%s] %s: goal=%s action=%s trace=%s\n", 
				e.Timestamp.Format("15:04:05"), e.Type, e.Trace.GoalID, e.Trace.ActionID, e.Trace.TraceID)
		}
	case "simulate":
		if len(args) == 0 {
			fmt.Println("Usage: simulate <goal_id>")
			return true
		}
		goal, ok := rt.GetGoalEngine().GetGoal(args[0])
		if !ok {
			fmt.Println("Error: goal not found")
			return true
		}
		results, err := rt.Simulate(goal)
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Printf("Simulation Results for %s:\n", goal.ID)
			for _, res := range results {
				fmt.Printf("  - Action %s: %v (Success: %v)\n", res.ActionID, res.Output, res.Success)
			}
		}
	case "fork":
		if len(args) == 0 {
			fmt.Println("Usage: fork <name>")
			return true
		}
		name := args[0]
		fork, err := rt.Fork(name)
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			fmt.Printf("Forked timeline into '%s'. Data dir: %s\n", name, fork.GetPSS().GetDataDir())
		}
	case "state":
		snap, err := rt.GetStateSnapshot()
		if err != nil {
			fmt.Println("Error:", err)
		} else {
			state := snap.State()
			fmt.Printf("State (clock: %d):\n", snap.Clock())
			for path := range state {
				fmt.Printf("  %s = %v\n", path, state[path].Value)
			}
		}
	case "events":
		length := rt.GetEventLog().Length()
		fmt.Printf("Total events: %d\n", length)
	case "metrics":
		goals := rt.GetGoalEngine().ListGoals()
		var active, completed, failed, pending int
		for _, g := range goals {
			switch g.State {
			case runtime.GoalStateActive:
				active++
			case runtime.GoalStateComplete:
				completed++
			case runtime.GoalStateFailed:
				failed++
			case runtime.GoalStatePending:
				pending++
			}
		}
		fmt.Printf("Metrics:\n")
		fmt.Printf("  Total: %d\n", len(goals))
		fmt.Printf("  Active: %d\n", active)
		fmt.Printf("  Completed: %d\n", completed)
		fmt.Printf("  Failed: %d\n", failed)
		fmt.Printf("  Pending: %d\n", pending)
	case "gdl":
		runGDLReload(rt)
	case "health":
		health, status := observer.GetHealth()
		fmt.Printf("Health Check Status: %s\n", status)
		for name, check := range health {
			fmt.Printf("  %s: %s - %s\n", name, check.Status, check.Message)
		}
	case "stream":
		ch := make(chan runtime.Event, 100)
		handler := func(e runtime.Event) {
			ch <- e
		}
		rt.GetEventBus().Subscribe("cli", nil, handler)
		defer rt.GetEventBus().Unsubscribe("cli")

		fmt.Println("Streaming events (Ctrl+C to stop)...")
		for {
			select {
			case e := <-ch:
				fmt.Printf("[%s] %s: %v\n", e.Timestamp.Format("15:04:05"), e.Type, e.Payload)
			case <-time.After(100 * time.Millisecond):
				// poll for interrupt if needed, but Ctrl+C works fine for standalone mode
			}
		}
	case "exit", "quit":
		return false
	default:
		fmt.Println("Unknown command. Type 'help' for available commands.")
	}
	return true
}

func runGDLReload(rt *runtime.GoalRuntime) {
	loadWorkspace(rt)
	fmt.Println("Workspace reloaded")
}

func printStandaloneHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  list     - List all goals")
	fmt.Println("  submit   - Submit a new goal")
	fmt.Println("  state    - Show current state")
	fmt.Println("  events   - Show event log summary")
	fmt.Println("  metrics  - Show system metrics")
	fmt.Println("  gdl      - Run GDL demo")
	fmt.Println("  health   - Show health status")
	fmt.Println("  stream   - Stream internal events")
	fmt.Println("  exit     - Quit the runtime")
}

func printClientHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  list     - List all goals on server")
	fmt.Println("  submit   - Submit a new goal to server")
	fmt.Println("  get      - Get details of a specific goal")
	fmt.Println("  cancel   - Cancel a goal")
	fmt.Println("  metrics  - Fetch server metrics")
	fmt.Println("  health   - Check server health")
	fmt.Println("  stream   - Stream events from server")
	fmt.Println("  exit     - Quit the client")
}

func printGoals(goals []*grpc.Goal) {
	fmt.Printf("Total goals: %d\n", len(goals))
	for _, g := range goals {
		printGoal(g)
	}
}

func printGoalsTable(goals []*grpc.Goal) {
	fmt.Printf("%-30s | %-10s | %-8s | %-20s\n", "ID", "STATUS", "PRIORITY", "CREATED")
	fmt.Println(strings.Repeat("-", 75))
	for _, g := range goals {
		t := time.Unix(g.CreatedAt, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("%-30s | %-10s | %-8d | %-20s\n", g.Id, g.Status, g.Priority, t)
	}
}

func printGoal(g *grpc.Goal) {
	fmt.Printf("Goal Details:\n")
	fmt.Printf("  ID:       %s\n", g.Id)
	fmt.Printf("  Status:   %s\n", g.Status)
	fmt.Printf("  Priority: %d\n", g.Priority)
	fmt.Printf("  Created:  %s\n", time.Unix(g.CreatedAt, 0).Format(time.RFC3339))
	if len(g.Metadata) > 0 {
		fmt.Printf("  Metadata:\n")
		for k, v := range g.Metadata {
			fmt.Printf("    %-15s: %s\n", k, v)
		}
	}
	if g.Error != "" {
		fmt.Printf("  Error:    %s\n", g.Error)
	}
}

func waitForShutdown(rt *runtime.GoalRuntime, grpcServer *grpc.Server, httpServer *ghttp.Server, observer *observability.Observer) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logging.Info("Shutting down...", logging.Field{})

	grpcServer.Stop()
	httpServer.Stop()
	rt.Stop()

	logging.Info("Shutdown complete", logging.Field{})
}

func registerDefaultPlugins(rt *runtime.GoalRuntime) {
	// File Storage (LocalFS by default)
	lfs := storage.NewLocalFS()
	baseDir := os.Getenv("UGEM_STORAGE_BASE_DIR")
	if baseDir == "" {
		baseDir = "storage/files"
	}
	rt.RegisterPlugin(lfs, map[string]string{
		"base_dir": baseDir,
	})

	// Notification (Console by default)
	notifier := &notification.ConsoleNotifier{}
	rt.RegisterPlugin(notifier, nil)

	// AI (Mock/OpenAI)
	aiPlug := &ai.AIPlugin{}
	rt.RegisterPlugin(aiPlug, map[string]string{
		"api_key": os.Getenv("OPENAI_API_KEY"),
	})
}
