// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitProject(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myapp")

	if err := InitProject(projectDir, "myapp"); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Verify all expected files exist
	expected := []string{
		"goalruntime.yaml",
		"apps/main/types.gdl",
		"apps/main/events.gdl",
		"apps/main/goals.gdl",
		"shared/types.gdl",
	}

	for _, f := range expected {
		path := filepath.Join(projectDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s not found", f)
		}
	}
}

func TestLoadWorkspace(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "testapp")

	if err := InitProject(projectDir, "testapp"); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	ws, err := LoadWorkspace(projectDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	if ws.Config.Name != "testapp" {
		t.Errorf("expected project name 'testapp', got %q", ws.Config.Name)
	}

	if len(ws.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(ws.Apps))
	}

	if ws.Apps[0].Name != "main" {
		t.Errorf("expected app name 'main', got %q", ws.Apps[0].Name)
	}

	// Merged program should have types, events, and goals
	if len(ws.Merged.Types) == 0 {
		t.Error("expected at least one type in merged program")
	}
	if len(ws.Merged.Events) == 0 {
		t.Error("expected at least one event in merged program")
	}
	if len(ws.Merged.Goals) == 0 {
		t.Error("expected at least one goal in merged program")
	}

	t.Logf("Workspace summary:\n%s", ws.Summary())
}

func TestLoadWorkspaceMultiApp(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "multiapp")

	// Scaffold base project
	if err := InitProject(projectDir, "multiapp"); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Add a second app
	secondApp := filepath.Join(projectDir, "apps", "payments")
	os.MkdirAll(secondApp, 0755)

	os.WriteFile(filepath.Join(secondApp, "types.gdl"), []byte(`type Payment struct {
    ID: string
    Amount: float64
    Status: string
}
`), 0644)

	os.WriteFile(filepath.Join(secondApp, "goals.gdl"), []byte(`goal process_payment
  priority: 20
  trigger: state.payment_created
  condition: state.payment.status == completed
  actions: [payment.charge]
`), 0644)

	// Update config to include new app
	config := `name: multiapp
version: "1.0"
apps:
  - main
  - payments
runtime:
  mode: normal
`
	os.WriteFile(filepath.Join(projectDir, "goalruntime.yaml"), []byte(config), 0644)

	ws, err := LoadWorkspace(projectDir)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}

	if len(ws.Apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(ws.Apps))
	}

	// Should have types from both apps
	if len(ws.Merged.Types) < 2 {
		t.Errorf("expected at least 2 types (Task + Payment), got %d", len(ws.Merged.Types))
	}

	if len(ws.Merged.Goals) < 2 {
		t.Errorf("expected at least 2 goals, got %d", len(ws.Merged.Goals))
	}

	// Compile should succeed
	compiled, err := ws.Compile()
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(compiled.Goals) < 2 {
		t.Errorf("expected at least 2 compiled goals, got %d", len(compiled.Goals))
	}

	t.Logf("Multi-app workspace:\n%s", ws.Summary())
}

func TestMergePrograms(t *testing.T) {
	p1 := &GDLProgram{
		Types:  []TypeDef{{Name: "User"}},
		Events: []EventDef{{Name: "user_created"}},
	}
	p2 := &GDLProgram{
		Types: []TypeDef{{Name: "Order"}},
		Goals: []GoalDef{{Name: "process_order"}},
	}

	merged := MergePrograms(p1, p2)

	if len(merged.Types) != 2 {
		t.Errorf("expected 2 types, got %d", len(merged.Types))
	}
	if len(merged.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(merged.Events))
	}
	if len(merged.Goals) != 1 {
		t.Errorf("expected 1 goal, got %d", len(merged.Goals))
	}
}

func TestFindProjectRoot(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "project")
	subDir := filepath.Join(projectDir, "apps", "main")

	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(projectDir, "goalruntime.yaml"), []byte("name: test\n"), 0644)

	// Should find the root from a subdirectory
	found, err := FindProjectRoot(subDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}

	if found != projectDir {
		t.Errorf("expected %s, got %s", projectDir, found)
	}
}
