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

func TestPatchToolUpdateFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewPatchTool(ws)

	patch := strings.Join([]string{
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1,2 +1,2 @@",
		" line1",
		"-line2",
		"+line3",
		"",
	}, "\n")
	args, _ := json.Marshal(map[string]any{"patch": patch})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("execute patch: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "line1\nline3\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}
