// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ugem-io/ugem/runtime"
)

// WorkspaceConfig represents the project configuration from goalruntime.yaml.
type WorkspaceConfig struct {
	Name    string
	Version string
	Apps    []string
	Runtime RuntimeConfig
}

// RuntimeConfig holds runtime-level settings.
type RuntimeConfig struct {
	Mode string // "normal" or "strict"
	HTTP string
	GRPC string
	Log  string
}

// App represents a single GDL application within the workspace.
type App struct {
	Name    string
	Dir     string
	Program *GDLProgram
}

// Workspace represents a loaded project with all its apps.
type Workspace struct {
	ProjectDir string
	Config     WorkspaceConfig
	Apps       []App
	Shared     *GDLProgram // shared/ types loaded first
	Merged     *GDLProgram // all programs merged
}

// FindProjectRoot walks up from startDir looking for goalruntime.yaml,
// similar to how git finds .git.
func FindProjectRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		configPath := filepath.Join(dir, "goalruntime.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("no goalruntime.yaml found (searched upward from %s)", startDir)
}

// ResolveProjectDir determines the project directory using the priority:
// 1. Explicit projectDir argument (from -project flag)
// 2. GOALRUNTIME_PROJECT environment variable
// 3. Walk up from current working directory
func ResolveProjectDir(projectDir string) (string, error) {
	if projectDir != "" {
		abs, err := filepath.Abs(projectDir)
		if err != nil {
			return "", err
		}
		configPath := filepath.Join(abs, "goalruntime.yaml")
		if _, err := os.Stat(configPath); err != nil {
			return "", fmt.Errorf("no goalruntime.yaml found in %s", abs)
		}
		return abs, nil
	}

	if envDir := os.Getenv("GOALRUNTIME_PROJECT"); envDir != "" {
		return ResolveProjectDir(envDir)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return FindProjectRoot(cwd)
}

// LoadWorkspace loads a complete workspace from the given project directory.
func LoadWorkspace(projectDir string) (*Workspace, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	// Parse config
	config, err := parseConfig(filepath.Join(absDir, "goalruntime.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	ws := &Workspace{
		ProjectDir: absDir,
		Config:     *config,
		Apps:       make([]App, 0),
		Shared:     &GDLProgram{},
		Merged: &GDLProgram{
			Types:     make([]TypeDef, 0),
			Events:    make([]EventDef, 0),
			Actions:   make([]ActionDef, 0),
			Goals:     make([]GoalDef, 0),
			Contracts: make([]ContractDef, 0),
			Policies:  make([]PolicyDef, 0),
			Tests:     make([]TestCaseDef, 0),
		},
	}

	// Load shared/ first (types available to all apps)
	sharedDir := filepath.Join(absDir, "shared")
	if info, err := os.Stat(sharedDir); err == nil && info.IsDir() {
		sharedProgram, err := loadGDLDir(sharedDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load shared/: %w", err)
		}
		ws.Shared = sharedProgram
		ws.Merged = MergePrograms(ws.Merged, sharedProgram)
	}

	// Load each app
	appsDir := filepath.Join(absDir, "apps")
	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		return ws, nil // No apps dir is fine for an empty project
	}

	for _, appName := range config.Apps {
		appDir := filepath.Join(appsDir, appName)
		if _, err := os.Stat(appDir); os.IsNotExist(err) {
			return nil, fmt.Errorf("app %q declared in config but directory %s not found", appName, appDir)
		}

		appProgram, err := loadGDLDir(appDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load app %q: %w", appName, err)
		}

		ws.Apps = append(ws.Apps, App{
			Name:    appName,
			Dir:     appDir,
			Program: appProgram,
		})

		ws.Merged = MergePrograms(ws.Merged, appProgram)
	}

	// Load tests/ directory at project root
	testsDir := filepath.Join(absDir, "tests")
	if info, err := os.Stat(testsDir); err == nil && info.IsDir() {
		testsProgram, err := loadGDLDir(testsDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load tests/: %w", err)
		}
		ws.Merged = MergePrograms(ws.Merged, testsProgram)
	}

	return ws, nil
}

// Compile compiles the merged workspace program.
func (ws *Workspace) Compile() (*CompiledProgram, error) {
	compiler := NewCompiler(ws.Merged)
	return compiler.Compile()
}

// Apply compiles and registers all workspace definitions onto the runtime.
func (ws *Workspace) Apply(rt *runtime.GoalRuntime) error {
	compiled, err := ws.Compile()
	if err != nil {
		return err
	}
	return compiled.CreateRuntime(rt)
}

// Summary returns a human-readable summary of the loaded workspace.
func (ws *Workspace) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Project: %s (v%s)\n", ws.Config.Name, ws.Config.Version))
	sb.WriteString(fmt.Sprintf("Apps: %d\n", len(ws.Apps)))
	for _, app := range ws.Apps {
		sb.WriteString(fmt.Sprintf("  - %s: %d types, %d events, %d goals, %d contracts, %d policies\n",
			app.Name,
			len(app.Program.Types),
			len(app.Program.Events),
			len(app.Program.Goals),
			len(app.Program.Contracts),
			len(app.Program.Policies),
		))
	}
	if ws.Shared != nil && (len(ws.Shared.Types) > 0 || len(ws.Shared.Contracts) > 0 || len(ws.Shared.Policies) > 0) {
		sb.WriteString(fmt.Sprintf("Shared: %d types, %d contracts, %d policies\n",
			len(ws.Shared.Types), len(ws.Shared.Contracts), len(ws.Shared.Policies)))
	}
	sb.WriteString(fmt.Sprintf("Total: %d types, %d events, %d goals, %d contracts, %d policies, %d tests\n",
		len(ws.Merged.Types),
		len(ws.Merged.Events),
		len(ws.Merged.Goals),
		len(ws.Merged.Contracts),
		len(ws.Merged.Policies),
		len(ws.Merged.Tests),
	))
	return sb.String()
}

// loadGDLDir discovers and parses all .gdl files in a directory.
func loadGDLDir(dir string) (*GDLProgram, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	merged := &GDLProgram{
		Types:     make([]TypeDef, 0),
		Events:    make([]EventDef, 0),
		Actions:   make([]ActionDef, 0),
		Goals:     make([]GoalDef, 0),
		Contracts: make([]ContractDef, 0),
		Policies:  make([]PolicyDef, 0),
		Tests:     make([]TestCaseDef, 0),
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gdl") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		program, err := ParseGDLFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", filePath, err)
		}

		merged = MergePrograms(merged, program)
	}

	return merged, nil
}

// parseConfig reads and parses goalruntime.yaml.
// Uses simple line-based parsing to avoid external YAML dependency.
func parseConfig(path string) (*WorkspaceConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := &WorkspaceConfig{
		Runtime: RuntimeConfig{
			Mode: "normal",
			HTTP: ":8080",
			GRPC: ":50051",
			Log:  "info",
		},
	}

	scanner := bufio.NewScanner(file)
	var section string // "", "apps", "runtime"

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Detect section by indentation and keywords
		if strings.HasPrefix(trimmed, "apps:") {
			section = "apps"
			continue
		}
		if strings.HasPrefix(trimmed, "runtime:") {
			section = "runtime"
			continue
		}

		// Top-level key: value
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.Contains(trimmed, ":") {
			section = ""
			parts := strings.SplitN(trimmed, ":", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			switch key {
			case "name":
				config.Name = val
			case "version":
				config.Version = val
			}
			continue
		}

		// Indented items
		switch section {
		case "apps":
			if strings.HasPrefix(trimmed, "- ") {
				appName := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				config.Apps = append(config.Apps, appName)
			}
		case "runtime":
			if strings.Contains(trimmed, ":") {
				parts := strings.SplitN(trimmed, ":", 2)
				key := strings.TrimSpace(parts[0])
				val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				switch key {
				case "mode":
					config.Runtime.Mode = val
				case "http":
					config.Runtime.HTTP = val
				case "grpc":
					config.Runtime.GRPC = val
				case "log":
					config.Runtime.Log = val
				}
			}
		}
	}

	if config.Name == "" {
		config.Name = filepath.Base(filepath.Dir(path))
	}

	return config, scanner.Err()
}

// InitProject scaffolds a new GDL project in the given directory.
func InitProject(dir string, name string) error {
	if name == "" {
		name = filepath.Base(dir)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	// Create project structure
	dirs := []string{
		absDir,
		filepath.Join(absDir, "apps"),
		filepath.Join(absDir, "apps", "main"),
		filepath.Join(absDir, "shared"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", d, err)
		}
	}

	// Write goalruntime.yaml
	configContent := fmt.Sprintf(`name: %s
version: "1.0"
apps:
  - main
runtime:
  mode: normal
  http: ":8080"
  grpc: ":50051"
  log: info
`, name)
	if err := os.WriteFile(filepath.Join(absDir, "goalruntime.yaml"), []byte(configContent), 0644); err != nil {
		return err
	}

	// Write example types.gdl in shared/
	sharedTypes := `# Shared types available to all apps
# type User struct {
#     ID: string
#     Email: string
# }
`
	if err := os.WriteFile(filepath.Join(absDir, "shared", "types.gdl"), []byte(sharedTypes), 0644); err != nil {
		return err
	}

	// Write example app files
	mainTypes := `type Task struct {
    ID: string
    Title: string
    Done: bool
}
`
	if err := os.WriteFile(filepath.Join(absDir, "apps", "main", "types.gdl"), []byte(mainTypes), 0644); err != nil {
		return err
	}

	mainEvents := `event task_created {
    path: state.task
}

event task_completed {
    path: state.task.done
}
`
	if err := os.WriteFile(filepath.Join(absDir, "apps", "main", "events.gdl"), []byte(mainEvents), 0644); err != nil {
		return err
	}

	mainGoals := `goal complete_task
  priority: 10
  trigger: state.task_created
  condition: state.task.done == true
  actions: [log]
`
	if err := os.WriteFile(filepath.Join(absDir, "apps", "main", "goals.gdl"), []byte(mainGoals), 0644); err != nil {
		return err
	}

	// Write contracts.gdl
	mainContracts := `contract TaskService {
  event task_created
  event task_completed
  action log
}
`
	if err := os.WriteFile(filepath.Join(absDir, "apps", "main", "contracts.gdl"), []byte(mainContracts), 0644); err != nil {
		return err
	}

	// Write policies.gdl
	mainPolicies := `policy task_validation {
  require task.title != ""
}
`
	if err := os.WriteFile(filepath.Join(absDir, "apps", "main", "policies.gdl"), []byte(mainPolicies), 0644); err != nil {
		return err
	}

	// Write shared contracts and policies
	sharedContracts := `# Shared contracts across all apps
# contract UserService {
#   event user.created
#   action user.validate
# }
`
	if err := os.WriteFile(filepath.Join(absDir, "shared", "contracts.gdl"), []byte(sharedContracts), 0644); err != nil {
		return err
	}

	sharedPolicies := `# Global policies applied to all apps
# policy global_auth {
#   require user.authenticated == true
# }
`
	if err := os.WriteFile(filepath.Join(absDir, "shared", "policies.gdl"), []byte(sharedPolicies), 0644); err != nil {
		return err
	}

	// Create tests/ directory with example test
	testsDir := filepath.Join(absDir, "tests")
	if err := os.MkdirAll(testsDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", testsDir, err)
	}

	exampleTest := `test task_completion {
  given task.title = "Buy groceries"
  given task.done = false
  when task.create(title="Buy groceries")
  expect event task_created
  expect goal complete_task.active
}
`
	if err := os.WriteFile(filepath.Join(testsDir, "task_test.gdl"), []byte(exampleTest), 0644); err != nil {
		return err
	}

	return nil
}
