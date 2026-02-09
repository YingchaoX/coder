package contextmgr

import (
	"testing"

	"coder/internal/chat"
)

func TestCompact(t *testing.T) {
	msgs := []chat.Message{
		{Role: "user", Content: "task 1"},
		{Role: "assistant", Content: "doing"},
		{Role: "tool", Content: `{"ok":true,"path":"a.go","content":"long"}`},
		{Role: "user", Content: "task 2"},
		{Role: "assistant", Content: "next step"},
		{Role: "user", Content: "task 3"},
		{Role: "assistant", Content: "processing"},
	}
	compacted, summary, changed := Compact(msgs, 3, true)
	if !changed {
		t.Fatalf("expected changed")
	}
	if summary == "" {
		t.Fatalf("expected summary")
	}
	if len(compacted) >= len(msgs) {
		t.Fatalf("expected compacted messages to be smaller")
	}
}
