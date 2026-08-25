package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTasksToolLifecycle(t *testing.T) {
	workspace := t.TempDir()
	tool := NewTasksTool(workspace)

	// Add a plan with two tasks.
	result, err := tool.Execute(`{"action":"plan","description":"Crear index.html\nCrear styles.css\nCrear script.js"}`)
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if !strings.Contains(result, "3 tareas") {
		t.Errorf("expected 3 tasks, got: %s", result)
	}
	if !strings.Contains(result, "[ ]") {
		t.Errorf("expected pending markers, got: %s", result)
	}

	// Mark task #1 done.
	result, err = tool.Execute(`{"action":"done","id":1}`)
	if err != nil {
		t.Fatalf("done failed: %v", err)
	}
	if !strings.Contains(result, "[x] #1") {
		t.Errorf("expected task 1 marked done, got: %s", result)
	}

	// Progress should be 1/3.
	result, err = tool.Execute(`{"action":"list"}`)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(result, "1/3") {
		t.Errorf("expected progress 1/3, got: %s", result)
	}

	// Persistence: reload from disk.
	store, err := LoadTasks(workspace)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(store.Tasks) != 3 {
		t.Errorf("expected 3 persisted tasks, got %d", len(store.Tasks))
	}
	if !store.Tasks[0].Done {
		t.Error("expected task 1 persisted as done")
	}

	// Clear.
	if _, err := tool.Execute(`{"action":"clear"}`); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, tasksRel)); err != nil {
		t.Errorf("tasks file should still exist after clear: %v", err)
	}
}
