package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrPathOutsideWorkspace = errors.New("path outside workspace")

type Workspace struct {
	root string
}

func NewWorkspace(root string) (*Workspace, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("workspace root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If cwd does not have symlinks or cannot be resolved, keep abs path.
		resolved = abs
	}
	return &Workspace{root: resolved}, nil
}

func (w *Workspace) Root() string {
	return w.root
}

func (w *Workspace) Resolve(path string) (string, error) {
	target := path
	if strings.TrimSpace(target) == "" {
		target = w.root
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(w.root, target)
	}

	clean := filepath.Clean(target)
	resolved, err := resolveWithParentSymlink(clean)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(w.root, resolved)
	if err != nil {
		return "", fmt.Errorf("relative path check: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrPathOutsideWorkspace
	}
	return resolved, nil
}

func resolveWithParentSymlink(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve symlink: %w", err)
	}

	parent := filepath.Dir(path)
	base := filepath.Base(path)
	parentResolved, perr := filepath.EvalSymlinks(parent)
	if perr != nil {
		if errors.Is(perr, os.ErrNotExist) {
			parentResolved = parent
		} else {
			return "", fmt.Errorf("resolve parent symlink: %w", perr)
		}
	}
	return filepath.Join(parentResolved, base), nil
}
