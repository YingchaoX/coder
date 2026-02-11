package storage

import (
	"path/filepath"
	"testing"

	"coder/internal/chat"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSQLiteStore_SessionCRUD(t *testing.T) {
	store := newTestStore(t)

	meta := SessionMeta{
		ID:    "sess_test_001",
		Title: "test session",
		Agent: "build",
		Model: "qwen-plus",
		CWD:   "/tmp",
	}
	meta.Compaction.Auto = true
	meta.Compaction.Prune = true

	// Create
	if err := store.CreateSession(meta); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Load
	loaded, err := store.LoadSession("sess_test_001")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Title != "test session" {
		t.Fatalf("Title=%q, want %q", loaded.Title, "test session")
	}
	if !loaded.Compaction.Auto {
		t.Fatalf("Compaction.Auto should be true")
	}

	// Update
	meta.Title = "updated title"
	if err := store.SaveSession(meta); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded2, _ := store.LoadSession("sess_test_001")
	if loaded2.Title != "updated title" {
		t.Fatalf("Title=%q after update, want %q", loaded2.Title, "updated title")
	}

	// List
	metas, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("ListSessions count=%d, want 1", len(metas))
	}
}

func TestSQLiteStore_Messages(t *testing.T) {
	store := newTestStore(t)

	meta := SessionMeta{ID: "sess_msg_001", Agent: "build"}
	if err := store.CreateSession(meta); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	messages := []chat.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there", Reasoning: "thinking..."},
		{Role: "assistant", Content: "", ToolCalls: []chat.ToolCall{
			{ID: "call_1", Type: "function", Function: chat.ToolCallFunction{Name: "read", Arguments: `{"path":"main.go"}`}},
		}},
		{Role: "tool", Name: "read", ToolCallID: "call_1", Content: `{"ok":true}`},
	}

	if err := store.SaveMessages("sess_msg_001", messages); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	loaded, err := store.LoadMessages("sess_msg_001")
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(loaded) != 4 {
		t.Fatalf("LoadMessages count=%d, want 4", len(loaded))
	}
	if loaded[0].Role != "user" || loaded[0].Content != "hello" {
		t.Fatalf("msg[0] unexpected: %+v", loaded[0])
	}
	if loaded[1].Reasoning != "thinking..." {
		t.Fatalf("msg[1].Reasoning=%q, want %q", loaded[1].Reasoning, "thinking...")
	}
	if len(loaded[2].ToolCalls) != 1 || loaded[2].ToolCalls[0].Function.Name != "read" {
		t.Fatalf("msg[2] tool_calls unexpected: %+v", loaded[2])
	}
	if loaded[3].ToolCallID != "call_1" {
		t.Fatalf("msg[3].ToolCallID=%q, want %q", loaded[3].ToolCallID, "call_1")
	}

	// 覆盖保存 / Overwrite save
	messages2 := []chat.Message{{Role: "user", Content: "only one"}}
	if err := store.SaveMessages("sess_msg_001", messages2); err != nil {
		t.Fatalf("SaveMessages overwrite: %v", err)
	}
	loaded2, _ := store.LoadMessages("sess_msg_001")
	if len(loaded2) != 1 {
		t.Fatalf("overwrite count=%d, want 1", len(loaded2))
	}
}

func TestSQLiteStore_AppendMessages(t *testing.T) {
	store := newTestStore(t)

	meta := SessionMeta{ID: "sess_msg_append_001", Agent: "build"}
	if err := store.CreateSession(meta); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	part1 := []chat.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	if err := store.AppendMessages("sess_msg_append_001", 0, part1); err != nil {
		t.Fatalf("AppendMessages part1: %v", err)
	}

	part2 := []chat.Message{
		{Role: "user", Content: "next"},
	}
	if err := store.AppendMessages("sess_msg_append_001", 2, part2); err != nil {
		t.Fatalf("AppendMessages part2: %v", err)
	}

	loaded, err := store.LoadMessages("sess_msg_append_001")
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("LoadMessages count=%d, want 3", len(loaded))
	}
	if loaded[2].Content != "next" {
		t.Fatalf("msg[2].Content=%q, want %q", loaded[2].Content, "next")
	}
}

func TestSQLiteStore_Todos(t *testing.T) {
	store := newTestStore(t)

	meta := SessionMeta{ID: "sess_todo_001", Agent: "build"}
	_ = store.CreateSession(meta)

	items := []TodoItem{
		{ID: "t1", Content: "step 1", Status: "completed", Priority: "high"},
		{ID: "t2", Content: "step 2", Status: "in_progress", Priority: "medium"},
		{ID: "t3", Content: "step 3", Status: "pending", Priority: "low"},
	}
	if err := store.ReplaceTodos("sess_todo_001", items); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}

	loaded, err := store.ListTodos("sess_todo_001")
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("ListTodos count=%d, want 3", len(loaded))
	}
	if loaded[0].Content != "step 1" || loaded[0].Status != "completed" {
		t.Fatalf("todo[0] unexpected: %+v", loaded[0])
	}

	// 替换 / Replace
	items2 := []TodoItem{{ID: "t1", Content: "only one", Status: "pending"}}
	if err := store.ReplaceTodos("sess_todo_001", items2); err != nil {
		t.Fatalf("ReplaceTodos replace: %v", err)
	}
	loaded2, _ := store.ListTodos("sess_todo_001")
	if len(loaded2) != 1 {
		t.Fatalf("replace count=%d, want 1", len(loaded2))
	}
}

func TestSQLiteStore_PermissionLog(t *testing.T) {
	store := newTestStore(t)

	meta := SessionMeta{ID: "sess_perm_001", Agent: "build"}
	_ = store.CreateSession(meta)

	err := store.LogPermission(PermissionEntry{
		SessionID: "sess_perm_001",
		Tool:      "bash",
		Decision:  "allow",
		Reason:    "user approved",
	})
	if err != nil {
		t.Fatalf("LogPermission: %v", err)
	}
}

func TestSQLiteStore_LoadNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.LoadSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}
