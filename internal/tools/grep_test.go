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

func TestGrepToolSkipsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("needle in ignored dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewGrepTool(ws)
	args, _ := json.Marshal(map[string]any{"pattern": "needle"})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("grep execute: %v", err)
	}

	var result struct {
		Count           int      `json:"count"`
		FilesScanned    int      `json:"files_scanned"`
		Truncated       bool     `json:"truncated"`
		IgnoredPatterns []string `json:"ignored_patterns"`
		Matches         []struct {
			Path string `json:"path"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("count=%d, want 1", result.Count)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("files_scanned=%d, want 1", result.FilesScanned)
	}
	if result.Truncated {
		t.Fatal("truncated should be false")
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "main.go" {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	if !containsString(result.IgnoredPatterns, "node_modules") {
		t.Fatalf("ignored_patterns missing node_modules: %+v", result.IgnoredPatterns)
	}
}

func TestGrepToolSkipsLargeFiles(t *testing.T) {
	root := t.TempDir()
	large := strings.Repeat("a", defaultGrepMaxFileSizeBytes+1024)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(large), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewGrepTool(ws)
	args, _ := json.Marshal(map[string]any{"pattern": "needle"})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("grep execute: %v", err)
	}

	var result struct {
		FilesScanned int `json:"files_scanned"`
		Count        int `json:"count"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("files_scanned=%d, want 1", result.FilesScanned)
	}
	if result.Count != 1 {
		t.Fatalf("count=%d, want 1", result.Count)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
