package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceResolve_BlocksParentEscape(t *testing.T) {
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}

	_, err = ws.Resolve("../outside.txt")
	if !errors.Is(err, ErrPathOutsideWorkspace) {
		t.Fatalf("Resolve() error = %v, want ErrPathOutsideWorkspace", err)
	}
}

func TestWorkspaceResolve_BlocksSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	linkPath := filepath.Join(root, "escape")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}

	_, err = ws.Resolve("escape/file.txt")
	if !errors.Is(err, ErrPathOutsideWorkspace) {
		t.Fatalf("Resolve() error = %v, want ErrPathOutsideWorkspace", err)
	}
}

func TestWorkspaceResolve_AllowsInsidePath(t *testing.T) {
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}

	got, err := ws.Resolve("a/b/c.txt")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	rel, err := filepath.Rel(ws.Root(), got)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	if rel != filepath.Join("a", "b", "c.txt") {
		t.Fatalf("Resolve() relative path = %q, want %q", rel, filepath.Join("a", "b", "c.txt"))
	}
}
