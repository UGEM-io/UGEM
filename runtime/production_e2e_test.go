// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime_test

import (
	"context"
	"github.com/ugem-io/ugem/plugins/ai"
	"github.com/ugem-io/ugem/plugins/notification"
	"github.com/ugem-io/ugem/plugins/storage"
	"github.com/ugem-io/ugem/runtime"
	gstorage "github.com/ugem-io/ugem/storage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProductionE2E(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "ugem_prod_data")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dataDir)

	fileDir := filepath.Join(dataDir, "files")

	// 1. First Boot: Setup & Action
	t.Log("--- PHASE 1: INITIAL BOOT ---")
	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)
	
	pss, err := gstorage.NewPersistentStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	rt.SetPersistence(pss)

	// Register Plugins
	rt.RegisterPlugin(storage.NewLocalFS(), map[string]string{"base_dir": fileDir})
	rt.RegisterPlugin(&notification.ConsoleNotifier{}, nil)
	rt.RegisterPlugin(&ai.AIPlugin{}, nil)

	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}

	// Submit a "System Ready" event
	if err := rt.SubmitEvent(runtime.Event{
		Type: "system.ready",
		Payload: map[string]interface{}{"version": "1.0.0"},
	}); err != nil {
		t.Fatalf("Failed to submit system.ready: %v", err)
	}

	// Goal: Process a document
	docGoal := runtime.Goal{
		ID:       "process-doc-1",
		Priority: 10,
		State:    runtime.GoalStatePending,
		Condition: func(s runtime.State) bool {
			v, ok := s.Get("processed/doc-1")
			return ok && v.Value == true
		},
	}
	if err := rt.SubmitGoal(docGoal); err != nil {
		t.Fatalf("Failed to submit docGoal: %v", err)
	}

	// Manual Trigger: Upload the document bytes
	uploadAction := runtime.Action{
		ID:    "upload-doc-1",
		Type:  "file.upload",
		Input: map[string]interface{}{"data": "important business data"},
	}
	
	ctx := runtime.ActionContext{
		CancelCtx: context.Background(),
		Trace:     runtime.TraceContext{TraceID: "t1", GoalID: docGoal.ID},
	}

	res, err := rt.GetActionDispatcher().Execute(uploadAction, ctx)
	if err != nil {
		t.Fatal(err)
	}
	
	fileURI := res["uri"].(string)
	t.Logf("File uploaded to: %s", fileURI)

	// Submit event that file is ready for AI
	if err := rt.SubmitEvent(runtime.Event{
		Type: "file.ready",
		Payload: map[string]interface{}{"uri": fileURI},
		WritePaths: []runtime.Path{"doc/status"},
	}); err != nil {
		t.Fatalf("Failed to submit file.ready: %v", err)
	}

	// Finalize the goal by manually setting the "processed" flag 
	if err := rt.SubmitEvent(runtime.Event{
		Type: "doc.processed",
		Payload: map[string]interface{}{"id": "doc-1"},
		WritePaths: []runtime.Path{"processed/doc-1"},
		StateMutations: []runtime.StateMutation{
			{Path: "processed/doc-1", Value: runtime.TypedValue{Type: "bool", Value: true}},
		},
	}); err != nil {
		t.Fatalf("Failed to submit doc.processed: %v", err)
	}

	// Wait for Goal Engine to mark goal as complete
	time.Sleep(100 * time.Millisecond)

	goal, _ := rt.GetGoalEngine().GetGoal("process-doc-1")
	if goal.State != runtime.GoalStateComplete {
		t.Errorf("Expected goal complete, got %s", goal.State)
	}

	rt.Stop()
	pss.Close()

	// 2. Second Boot: Verify Persistence
	t.Log("--- PHASE 2: RESTART & PERSISTENCE VERIFICATION ---")
	rt2 := runtime.NewRuntime(runtime.SchedulerModeNormal)
	pss2, _ := gstorage.NewPersistentStore(dataDir)
	rt2.SetPersistence(pss2)
	
	if err := rt2.Start(); err != nil {
		t.Fatal(err)
	}
	defer rt2.Stop()
	defer pss2.Close()

	// Verify State Persistence
	val, ok := rt2.GetState().Get("processed/doc-1")
	if !ok || val.Value != true {
		t.Error("State did not persist across restarts")
	}

	// Verify Goal Persistence (should be loaded from PSS if we implemented goal persistence, 
	// otherwise we check if we can reconstruct)
	// Currently, goals are in-memory but events are persistent. 
	// We can verify that we can 'Rewind' or simply check the EventLog.
	if rt2.GetEventLog().Length() == 0 {
		t.Error("Event log is empty after restart")
	}

	t.Log("Production E2E Verified.")
}
