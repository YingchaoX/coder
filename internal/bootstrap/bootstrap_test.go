package bootstrap

import (
	"path/filepath"
	"strings"
	"testing"

	"coder/internal/config"
)

func TestBuildEmptyWorkspaceRootFails(t *testing.T) {
	cfg := config.Default()
	_, err := Build(cfg, "")
	if err == nil {
		t.Fatal("Build with empty root should fail")
	}
	if !strings.Contains(err.Error(), "workspace") && !strings.Contains(err.Error(), "root") {
		t.Fatalf("expected workspace/root-related error: %v", err)
	}
}

func TestBuildEmptyRootFromConfigFails(t *testing.T) {
	cfg := config.Default()
	cfg.Runtime.WorkspaceRoot = ""
	_, err := Build(cfg, "")
	if err == nil {
		t.Fatal("Build with empty root should fail")
	}
}

func TestBuildSuccessWithTempDir(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Storage.BaseDir = filepath.Join(tmp, "data")
	cfg.Skills.Paths = []string{tmp}
	res, err := Build(cfg, tmp)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	defer res.Store.Close()
	if res.Orch == nil {
		t.Fatal("orch is nil")
	}
	if res.Store == nil {
		t.Fatal("store is nil")
	}
	// WorkspaceRoot may be canonicalized (e.g. /private/var on macOS)
	if res.WorkspaceRoot == "" || !strings.Contains(res.WorkspaceRoot, "TestBuildSuccessWithTempDir") {
		t.Fatalf("WorkspaceRoot should be set and contain temp dir: %q", res.WorkspaceRoot)
	}
	if res.SessionID == "" {
		t.Fatal("SessionID is empty")
	}
}
