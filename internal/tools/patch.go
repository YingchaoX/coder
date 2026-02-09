package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"coder/internal/chat"
	"coder/internal/security"
)

type PatchTool struct {
	ws *security.Workspace
}

func NewPatchTool(ws *security.Workspace) *PatchTool {
	return &PatchTool{ws: ws}
}

func (t *PatchTool) Name() string {
	return "patch"
}

func (t *PatchTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Apply unified diff patch inside workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch":   map[string]any{"type": "string"},
					"dry_run": map[string]any{"type": "boolean"},
				},
				"required": []string{"patch"},
			},
		},
	}
}

func (t *PatchTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Patch  string `json:"patch"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("patch args: %w", err)
	}
	if strings.TrimSpace(in.Patch) == "" {
		return "", fmt.Errorf("patch content is empty")
	}

	files, err := parseUnifiedDiff(in.Patch)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no file patch found")
	}

	summaries := make([]map[string]any, 0, len(files))
	for _, fp := range files {
		s, err := t.applyFilePatch(fp, in.DryRun)
		if err != nil {
			return "", fmt.Errorf("apply %s: %w", fp.displayPath(), err)
		}
		summaries = append(summaries, s)
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"dry_run": in.DryRun,
		"applied": len(summaries),
		"files":   summaries,
	}), nil
}

type diffFile struct {
	OldPath string
	NewPath string
	Hunks   []diffHunk
}

type diffHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []diffLine
}

type diffLine struct {
	Kind    byte
	Content string
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseUnifiedDiff(patch string) ([]diffFile, error) {
	lines := splitKeepNewline(strings.ReplaceAll(patch, "\r\n", "\n"))
	files := []diffFile{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		if !strings.HasPrefix(line, "--- ") {
			i++
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
			return nil, fmt.Errorf("invalid patch header near line %d", i+1)
		}
		fp := diffFile{
			OldPath: parseDiffPath(line),
			NewPath: parseDiffPath(lines[i+1]),
		}
		i += 2

		for i < len(lines) {
			if strings.HasPrefix(lines[i], "--- ") {
				break
			}
			if !strings.HasPrefix(lines[i], "@@ ") {
				i++
				continue
			}
			h, consumed, err := parseHunk(lines[i:])
			if err != nil {
				return nil, err
			}
			fp.Hunks = append(fp.Hunks, h)
			i += consumed
		}
		files = append(files, fp)
	}
	return files, nil
}

func parseHunk(lines []string) (diffHunk, int, error) {
	if len(lines) == 0 {
		return diffHunk{}, 0, fmt.Errorf("empty hunk")
	}
	match := hunkHeader.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if len(match) == 0 {
		return diffHunk{}, 0, fmt.Errorf("invalid hunk header: %s", strings.TrimSpace(lines[0]))
	}
	oldStart, _ := strconv.Atoi(match[1])
	oldCount := 1
	if match[2] != "" {
		oldCount, _ = strconv.Atoi(match[2])
	}
	newStart, _ := strconv.Atoi(match[3])
	newCount := 1
	if match[4] != "" {
		newCount, _ = strconv.Atoi(match[4])
	}

	h := diffHunk{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount}
	consumed := 1
	for consumed < len(lines) {
		line := lines[consumed]
		if strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "--- ") {
			break
		}
		if strings.HasPrefix(line, "\\ No newline") {
			consumed++
			continue
		}
		if line == "" {
			consumed++
			continue
		}
		kind := line[0]
		if kind != ' ' && kind != '+' && kind != '-' {
			return diffHunk{}, 0, fmt.Errorf("invalid hunk line: %q", strings.TrimSpace(line))
		}
		h.Lines = append(h.Lines, diffLine{Kind: kind, Content: line[1:]})
		consumed++
	}
	return h, consumed, nil
}

func parseDiffPath(header string) string {
	rest := strings.TrimSpace(header[4:])
	if idx := strings.IndexAny(rest, "\t "); idx >= 0 {
		rest = rest[:idx]
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, "a/")
	rest = strings.TrimPrefix(rest, "b/")
	return rest
}

func (f diffFile) displayPath() string {
	if f.NewPath != "" && f.NewPath != "/dev/null" {
		return f.NewPath
	}
	return f.OldPath
}

func (t *PatchTool) applyFilePatch(fp diffFile, dryRun bool) (map[string]any, error) {
	addFile := fp.OldPath == "/dev/null"
	deleteFile := fp.NewPath == "/dev/null"
	if addFile && deleteFile {
		return nil, fmt.Errorf("invalid patch for %s", fp.displayPath())
	}
	target := fp.displayPath()
	if strings.TrimSpace(target) == "" || target == "/dev/null" {
		return nil, fmt.Errorf("invalid target path")
	}

	resolved, err := t.ws.Resolve(target)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	original := ""
	if !addFile {
		data, readErr := os.ReadFile(resolved)
		if readErr != nil {
			return nil, fmt.Errorf("read original file: %w", readErr)
		}
		original = string(data)
	}

	updated, err := applyHunks(original, fp.Hunks)
	if err != nil {
		return nil, err
	}

	if dryRun {
		return map[string]any{
			"path":      resolved,
			"operation": operationLabel(addFile, deleteFile),
			"bytes":     len(updated),
		}, nil
	}

	if deleteFile {
		if err := os.Remove(resolved); err != nil {
			return nil, fmt.Errorf("remove file: %w", err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return nil, fmt.Errorf("create parent: %w", err)
		}
		if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
			return nil, fmt.Errorf("write patched file: %w", err)
		}
	}

	return map[string]any{
		"path":      resolved,
		"operation": operationLabel(addFile, deleteFile),
		"bytes":     len(updated),
	}, nil
}

func operationLabel(addFile, deleteFile bool) string {
	if addFile {
		return "create"
	}
	if deleteFile {
		return "delete"
	}
	return "update"
}

func applyHunks(original string, hunks []diffHunk) (string, error) {
	origLines := splitKeepNewline(original)
	if len(hunks) == 0 {
		return original, nil
	}

	out := make([]string, 0, len(origLines))
	idx := 0
	for _, h := range hunks {
		start := h.OldStart - 1
		if h.OldStart == 0 {
			start = 0
		}
		if start < idx || start > len(origLines) {
			return "", fmt.Errorf("hunk start out of range")
		}
		out = append(out, origLines[idx:start]...)
		idx = start

		for _, line := range h.Lines {
			switch line.Kind {
			case ' ':
				if idx >= len(origLines) || origLines[idx] != line.Content {
					return "", fmt.Errorf("context mismatch")
				}
				out = append(out, origLines[idx])
				idx++
			case '-':
				if idx >= len(origLines) || origLines[idx] != line.Content {
					return "", fmt.Errorf("remove mismatch")
				}
				idx++
			case '+':
				out = append(out, line.Content)
			default:
				return "", fmt.Errorf("unsupported diff line kind")
			}
		}
	}
	out = append(out, origLines[idx:]...)
	return strings.Join(out, ""), nil
}

func splitKeepNewline(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
