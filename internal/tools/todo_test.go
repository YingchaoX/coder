package tools

import (
	"context"
	"encoding/json"
	"testing"

	"coder/internal/storage"
)

func TestTodoReadWrite(t *testing.T) {
	mgr, err := storage.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := mgr.CreateSession("build", "model", "/tmp", true, true)
	if err != nil {
		t.Fatal(err)
	}
	getID := func() string { return meta.ID }

	readTool := NewTodoReadTool(mgr, getID)
	writeTool := NewTodoWriteTool(mgr, getID)

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
