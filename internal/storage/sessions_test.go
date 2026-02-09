package storage

import (
	"testing"

	"coder/internal/chat"
)

func TestSessionCRUD(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	meta, _, err := m.CreateSession("build", "model", "/tmp", true, true)
	if err != nil {
		t.Fatal(err)
	}
	messages := []chat.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if err := m.Save(meta, messages); err != nil {
		t.Fatal(err)
	}

	loadedMeta, loadedMessages, err := m.Load(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedMeta.ID != meta.ID {
		t.Fatalf("id mismatch")
	}
	if len(loadedMessages) != 2 {
		t.Fatalf("message length mismatch")
	}

	if _, _, err := m.RevertTo(meta.ID, 1); err != nil {
		t.Fatal(err)
	}
	_, reverted, err := m.Load(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reverted) != 1 {
		t.Fatalf("revert failed")
	}

	todos := []TodoItem{
		{ID: "t1", Content: "step1", Status: "pending", Priority: "high"},
		{ID: "t2", Content: "step2", Status: "in_progress", Priority: "medium"},
	}
	if err := m.ReplaceTodos(meta.ID, todos); err != nil {
		t.Fatal(err)
	}
	gotTodos, err := m.ListTodos(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotTodos) != 2 {
		t.Fatalf("unexpected todo count: %d", len(gotTodos))
	}
}
