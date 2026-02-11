package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coder/internal/chat"
	"coder/internal/security"
)

type WriteTool struct {
	ws *security.Workspace
}

func NewWriteTool(ws *security.Workspace) *WriteTool {
	return &WriteTool{ws: ws}
}

func (t *WriteTool) Name() string {
	return "write"
}

func (t *WriteTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Write full content to a file in workspace. This is expensive and should be used mainly for creating new files or completely replacing existing files, not for small localized edits or appending a single line.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (t *WriteTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("write args: %w", err)
	}

	resolved, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	original := ""
	existed := false
	if data, readErr := os.ReadFile(resolved); readErr == nil {
		existed = true
		original = string(data)
	} else if !os.IsNotExist(readErr) {
		return "", fmt.Errorf("read original file: %w", readErr)
	}
	parent, err := t.ws.Resolve(filepath.Dir(in.Path))
	if err != nil {
		return "", fmt.Errorf("resolve parent path: %w", err)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create parent directories: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(in.Content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	operation := "created"
	if existed {
		operation = "updated"
		if normalizeLineEndings(original) == normalizeLineEndings(in.Content) {
			operation = "unchanged"
		}
	}
	diff, additions, deletions := "", 0, 0
	diffTruncated := false
	if operation == "created" || operation == "updated" {
		diff, additions, deletions = BuildUnifiedDiff(strings.TrimSpace(in.Path), original, in.Content)
		diff, diffTruncated = TruncateUnifiedDiff(diff, 80, 8000)
	}

	return mustJSON(map[string]any{
		"ok":             true,
		"path":           resolved,
		"size":           len(in.Content),
		"operation":      operation,
		"additions":      additions,
		"deletions":      deletions,
		"diff":           diff,
		"diff_truncated": diffTruncated,
	}), nil
}
