package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coder/internal/security"
)

func TestFormatWriteApprovalArgsUpdateIncludesDiff(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{
		"path":    "a.txt",
		"content": "new\n",
	})
	got := formatWriteApprovalArgs(string(raw), ws)
	for _, needle := range []string{"operation: update", "changes: +1 -1", "diff_preview:", "-old", "+new"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in approval args:\n%s", needle, got)
		}
	}
}

func TestTodoStatusMarker(t *testing.T) {
	if marker := todoStatusMarker("pending"); marker != "[ ]" {
		t.Fatalf("pending marker=%q", marker)
	}
	if marker := todoStatusMarker("in_progress"); marker != "[~]" {
		t.Fatalf("in_progress marker=%q", marker)
	}
	if marker := todoStatusMarker("completed"); marker != "[x]" {
		t.Fatalf("completed marker=%q", marker)
	}
}

func TestResolveModelTarget(t *testing.T) {
	models := []string{"qwen-plus", "qwen-max", "qwen-turbo"}
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "by name", input: "/models qwen-max", want: "qwen-max"},
		{name: "by index", input: "/models 2", want: "qwen-max"},
		{name: "by set alias", input: "/models set qwen-turbo", want: "qwen-turbo"},
		{name: "quoted", input: `/models "qwen-plus"`, want: "qwen-plus"},
		{name: "out of range", input: "/models 9", wantErr: true},
		{name: "empty", input: "/models", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveModelTarget(tc.input, models)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got model=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveModelTarget(%q)=%q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestColorizeUnifiedDiffAddsAnsiCodes(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("AGENT_NO_COLOR", "")

	diff := "@@ -1,1 +1,1 @@\n-old\n+new"
	got := colorizeUnifiedDiff(diff)
	for _, needle := range []string{cliAnsiCyan + "@@", cliAnsiRed + "-old", cliAnsiGreen + "+new"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in colored diff: %q", needle, got)
		}
	}
}

func TestColorizeUnifiedDiffRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	diff := "@@ -1,1 +1,1 @@\n-old\n+new"
	got := colorizeUnifiedDiff(diff)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("expected no ANSI sequences when NO_COLOR is set: %q", got)
	}
}
