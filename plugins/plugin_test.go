// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package plugins_test

import (
	"context"
	"github.com/ugem-io/ugem/plugins/storage"
	"github.com/ugem-io/ugem/plugins/notification"
	"github.com/ugem-io/ugem/plugins/ai"
	"github.com/ugem-io/ugem/runtime"
	"testing"
)

func TestPluginIntegration(t *testing.T) {
	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)
	
	// Register Plugins
	lfs := storage.NewLocalFS()
	rt.RegisterPlugin(lfs, map[string]string{"base_dir": t.TempDir()})
	
	notifier := &notification.ConsoleNotifier{}
	rt.RegisterPlugin(notifier, nil)
	
	aiPlug := &ai.AIPlugin{}
	rt.RegisterPlugin(aiPlug, nil)
	
	if err := rt.Start(); err != nil {
		t.Fatalf("Failed to start runtime: %v", err)
	}
	defer rt.Stop()
	
	// Verify Actions are registered in Planner
	planner := rt.GetPlanner()
	
	actionTypes := []string{"file.upload", "file.delete", "notify.send", "ai.process", "ai.summarize"}
	for _, at := range actionTypes {
		if _, ok := planner.GetActionResolver(at); !ok {
			t.Errorf("Action %s not registered", at)
		}
	}
	
	// Test file.upload action directly via ActionDispatcher
	dispatcher := rt.GetActionDispatcher()
	ctx := runtime.ActionContext{
		CancelCtx: context.Background(),
		Trace: runtime.TraceContext{TraceID: "test-trace"},
	}
	
	action := runtime.Action{
		ID:    "test-upload",
		Type:  "file.upload",
		Input: map[string]interface{}{"data": "hello world"},
	}
	
	result, err := dispatcher.Execute(action, ctx)
	if err != nil {
		t.Fatalf("Failed to execute file.upload: %v", err)
	}
	
	if result["hash"] == "" {
		t.Errorf("Expected hash in result, got empty")
	}
	t.Logf("Uploaded file hash: %v", result["hash"])
}
