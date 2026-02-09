package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coder/internal/security"
)

func TestWriteToolIncludesDiffMetadataOnUpdate(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewWriteTool(ws)

	args, _ := json.Marshal(map[string]any{
		"path":    "a.txt",
		"content": "new\n",
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute write: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["operation"] != "updated" {
		t.Fatalf("operation=%v", result["operation"])
	}
	diff, _ := result["diff"].(string)
	for _, needle := range []string{"-old", "+new"} {
		if !strings.Contains(diff, needle) {
			t.Fatalf("diff missing %q: %q", needle, diff)
		}
	}
}

func TestWriteToolUnchangedOperation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewWriteTool(ws)

	args, _ := json.Marshal(map[string]any{
		"path":    "a.txt",
		"content": "same\n",
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute write: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["operation"] != "unchanged" {
		t.Fatalf("operation=%v", result["operation"])
	}
	diff, _ := result["diff"].(string)
	if strings.TrimSpace(diff) != "" {
		t.Fatalf("expected empty diff, got %q", diff)
	}
}
