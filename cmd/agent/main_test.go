package main

import (
	"testing"

	"coder/internal/config"
)

func TestResolveWorkspaceRoot(t *testing.T) {
	cfg := config.Default()
	cfg.Runtime.WorkspaceRoot = ""

	// override wins
	root, err := resolveWorkspaceRoot("/tmp/foo", cfg)
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if root != "/tmp/foo" {
		t.Fatalf("got %q", root)
	}

	// cfg.Runtime.WorkspaceRoot when override empty
	cfg.Runtime.WorkspaceRoot = "/from/config"
	root, err = resolveWorkspaceRoot("", cfg)
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if root != "/from/config" {
		t.Fatalf("got %q", root)
	}

	// empty both -> Getwd
	cfg.Runtime.WorkspaceRoot = ""
	root, err = resolveWorkspaceRoot("", cfg)
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if root == "" {
		t.Fatal("expected non-empty cwd")
	}
}
