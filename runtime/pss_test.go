// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package runtime_test

import (
	"os"
	"testing"
	"time"

	"github.com/ugem-io/ugem/gdl"
	"github.com/ugem-io/ugem/runtime"
	"github.com/ugem-io/ugem/storage"
)

func TestPSSAndUQL(t *testing.T) {
	dataDir := "test_data_pss"
	os.RemoveAll(dataDir)
	defer os.RemoveAll(dataDir)

	pss, err := storage.NewPersistentStore(dataDir)
	if err != nil {
		t.Fatalf("Failed to create PSS: %v", err)
	}

	t.Log("Starting TestPSSAndUQL")
	rt := runtime.NewRuntime(runtime.SchedulerModeNormal)
	rt.SetPersistence(pss)
	t.Log("Runtime initialized with PSS")
	rt.Start()
	defer rt.Stop()
	t.Log("Runtime started")

	// 1. Submit events to create state
	e1 := runtime.Event{
		Type: "customer_created",
		Payload: map[string]interface{}{"id": "c1", "balance": 500.0},
		WritePaths: []runtime.Path{"customer.c1.balance", "customer.c1.status"},
		StateMutator: func(s runtime.State) error {
			s.Set("customer.c1.balance", runtime.TypedValue{Type: "float", Value: 500.0})
			s.Set("customer.c1.status", runtime.TypedValue{Type: "string", Value: "active"})
			return nil
		},
	}
	t.Log("Submitting event e1")
	rt.SubmitEvent(e1)
	time.Sleep(200 * time.Millisecond) // Give time for persistence
	t.Log("Event e1 submitted")

	// 2. Verify PSS storage
	objPath := dataDir + "/objects/type/customer/c1.bin"
	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		t.Errorf("Object file not created: %s", objPath)
	}

	indexSlice, _ := pss.Search("customer", "balance", 500.0)
	t.Logf("Search result: %v", indexSlice)
	if len(indexSlice) == 0 || indexSlice[0] != "c1" {
		t.Errorf("Index lookup failed for customer balance 500. Got: %v", indexSlice)
	}

	// 3. Test UQL
	t.Log("Executing UQL query")
	uqlEngine := gdl.NewUQLEngine(pss)
	results, err := uqlEngine.Execute("query customer where balance == 500")
	if err != nil {
		t.Errorf("UQL execution failed: %v", err)
	}
	t.Logf("UQL results: %v", results)
	if len(results) == 0 {
		t.Errorf("UQL query returned no results")
	}

	// 4. Test Rewind
	t1 := time.Now()
	t.Logf("T1: %v", t1)
	time.Sleep(200 * time.Millisecond)
	
	e2 := runtime.Event{
		Type: "balance_updated",
		Payload: map[string]interface{}{"id": "c1", "balance": 1000.0},
		WritePaths: []runtime.Path{"customer.c1.balance"},
		StateMutator: func(s runtime.State) error {
			s.Set("customer.c1.balance", runtime.TypedValue{Type: "float", Value: 1000.0})
			return nil
		},
	}
	t.Log("Submitting event e2")
	rt.SubmitEvent(e2)
	time.Sleep(100 * time.Millisecond)

	val, _ := rt.GetState().Get("customer.c1.balance")
	t.Logf("Current balance: %v", val.Value)
	if val.Value != 1000.0 {
		t.Errorf("Balance should be 1000, got %v", val.Value)
	}

	t.Log("Rewinding to T1")
	err = rt.Rewind(t1)
	if err != nil {
		t.Errorf("Rewind failed: %v", err)
	}

	val, _ = rt.GetState().Get("customer.c1.balance")
	t.Logf("Balance after rewind: %v", val.Value)
	if val.Value != 500.0 {
		t.Errorf("Balance after rewind should be 500, got %v", val.Value)
	}

	// 5. Test Fork
	t.Log("Forking timeline")
	fork, err := rt.Fork("test-fork")
	if err != nil {
		t.Errorf("Fork failed: %v", err)
	}
	
	fork.GetState().Set("customer.c1.balance", runtime.TypedValue{Type: "float", Value: 999.0})
	
	// Original should still be 500
	val, _ = rt.GetState().Get("customer.c1.balance")
	t.Logf("Original balance after fork modification: %v", val.Value)
	if val.Value != 500.0 {
		t.Errorf("Original balance changed after fork: %v", val.Value)
	}
	t.Log("Test complete")
}
