package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"coder/internal/storage"
)

func TestTodoReadWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	meta := storage.SessionMeta{
		ID:    "sess_test_todo",
		Agent: "build",
		Model: "model",
		CWD:   "/tmp",
	}
	if err := store.CreateSession(meta); err != nil {
		t.Fatal(err)
	}
	getID := func() string { return meta.ID }

	readTool := NewTodoReadTool(store, getID)
	writeTool := NewTodoWriteTool(store, getID)

	args, _ := json.Marshal(map[string]any{
		"todos": []map[string]any{
			{"id": "1", "content": "step1", "status": "in_progress", "priority": "high"},
			{"id": "2", "content": "step2", "status": "pending", "priority": "medium"},
		},
	})
	if _, err := writeTool.Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	result, err := readTool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatalf("empty result")
	}
}
