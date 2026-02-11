package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxUndoTurns = 20

type undoFileSnapshot struct {
	Path    string
	Existed bool
	Content []byte
	Mode    os.FileMode
}

type turnUndoEntry struct {
	Files []undoFileSnapshot
}

type turnUndoRecorder struct {
	workspaceRoot string
	order         []string
	snapshots     map[string]undoFileSnapshot
}

func newTurnUndoRecorder(workspaceRoot string) *turnUndoRecorder {
	return &turnUndoRecorder{
		workspaceRoot: strings.TrimSpace(workspaceRoot),
		order:         make([]string, 0, 8),
		snapshots:     make(map[string]undoFileSnapshot),
	}
}

func (r *turnUndoRecorder) CaptureFromToolCall(tool string, args json.RawMessage) {
	if r == nil {
		return
	}
	for _, rawPath := range affectedPathsFromToolCallForUndo(tool, args) {
		r.capturePath(rawPath)
	}
}

func (r *turnUndoRecorder) HasSnapshots() bool {
	return r != nil && len(r.order) > 0
}

func (r *turnUndoRecorder) Entry() turnUndoEntry {
	if r == nil || len(r.order) == 0 {
		return turnUndoEntry{}
	}
	out := turnUndoEntry{Files: make([]undoFileSnapshot, 0, len(r.order))}
	for _, path := range r.order {
		s, ok := r.snapshots[path]
		if !ok {
			continue
		}
		out.Files = append(out.Files, s)
	}
	return out
}

func (r *turnUndoRecorder) capturePath(rawPath string) {
	resolved, ok := resolveUndoPath(r.workspaceRoot, rawPath)
	if !ok {
		return
	}
	if _, exists := r.snapshots[resolved]; exists {
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			r.order = append(r.order, resolved)
			r.snapshots[resolved] = undoFileSnapshot{
				Path:    resolved,
				Existed: false,
			}
		}
		return
	}
	if info.IsDir() {
		return
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return
	}
	r.order = append(r.order, resolved)
	r.snapshots[resolved] = undoFileSnapshot{
		Path:    resolved,
		Existed: true,
		Content: data,
		Mode:    info.Mode().Perm(),
	}
}

func resolveUndoPath(workspaceRoot, target string) (string, bool) {
	root := strings.TrimSpace(workspaceRoot)
	path := strings.TrimSpace(target)
	if root == "" || path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}

func affectedPathsFromToolCallForUndo(tool string, args json.RawMessage) []string {
	switch strings.TrimSpace(strings.ToLower(tool)) {
	case "write":
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil
		}
		return nonEmptyPaths(in.Path)
	case "edit":
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil
		}
		return nonEmptyPaths(in.Path)
	case "patch":
		var in struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil
		}
		return parsePatchAffectedPaths(in.Patch)
	default:
		return nil
	}
}

func parsePatchAffectedPaths(patch string) []string {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	for i := 0; i+1 < len(lines); i++ {
		left := strings.TrimSpace(lines[i])
		right := strings.TrimSpace(lines[i+1])
		if !strings.HasPrefix(left, "--- ") || !strings.HasPrefix(right, "+++ ") {
			continue
		}
		oldPath := parsePatchHeaderPath(left)
		newPath := parsePatchHeaderPath(right)
		for _, p := range []string{oldPath, newPath} {
			if p == "" || p == "/dev/null" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
		i++
	}
	return out
}

func parsePatchHeaderPath(header string) string {
	if len(header) < 4 {
		return ""
	}
	rest := strings.TrimSpace(header[4:])
	if idx := strings.IndexAny(rest, "\t "); idx >= 0 {
		rest = rest[:idx]
	}
	rest = strings.TrimPrefix(rest, "a/")
	rest = strings.TrimPrefix(rest, "b/")
	return strings.TrimSpace(rest)
}

func nonEmptyPaths(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func (o *Orchestrator) pushUndoEntry(entry turnUndoEntry) {
	if len(entry.Files) == 0 {
		return
	}
	o.undoStack = append(o.undoStack, entry)
	if len(o.undoStack) <= maxUndoTurns {
		return
	}
	o.undoStack = append([]turnUndoEntry(nil), o.undoStack[len(o.undoStack)-maxUndoTurns:]...)
}

func (o *Orchestrator) commitTurnUndo(recorder *turnUndoRecorder) {
	if recorder == nil || !recorder.HasSnapshots() {
		return
	}
	o.pushUndoEntry(recorder.Entry())
}

func (o *Orchestrator) undoLastTurn() (string, error) {
	if len(o.undoStack) == 0 {
		return "No undoable file changes from the last turn.", nil
	}
	entry := o.undoStack[len(o.undoStack)-1]
	o.undoStack = o.undoStack[:len(o.undoStack)-1]
	restored := 0
	removed := 0
	for i := len(entry.Files) - 1; i >= 0; i-- {
		snap := entry.Files[i]
		if snap.Existed {
			mode := snap.Mode
			if mode == 0 {
				mode = 0o644
			}
			if err := os.MkdirAll(filepath.Dir(snap.Path), 0o755); err != nil {
				o.undoStack = append(o.undoStack, entry)
				return "", fmt.Errorf("undo mkdir %s: %w", filepath.Dir(snap.Path), err)
			}
			if err := os.WriteFile(snap.Path, snap.Content, mode); err != nil {
				o.undoStack = append(o.undoStack, entry)
				return "", fmt.Errorf("undo restore %s: %w", snap.Path, err)
			}
			restored++
			continue
		}
		info, err := os.Stat(snap.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			o.undoStack = append(o.undoStack, entry)
			return "", fmt.Errorf("undo stat %s: %w", snap.Path, err)
		}
		if info.IsDir() {
			continue
		}
		if err := os.Remove(snap.Path); err != nil {
			o.undoStack = append(o.undoStack, entry)
			return "", fmt.Errorf("undo remove %s: %w", snap.Path, err)
		}
		removed++
	}
	return fmt.Sprintf("Undo applied: restored %d file(s), removed %d newly created file(s).", restored, removed), nil
}
