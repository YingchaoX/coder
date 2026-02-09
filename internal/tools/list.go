package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"os"

	"coder/internal/chat"
	"coder/internal/security"
)

type ListTool struct {
	ws *security.Workspace
}

func NewListTool(ws *security.Workspace) *ListTool {
	return &ListTool{ws: ws}
}

func (t *ListTool) Name() string {
	return "list"
}

func (t *ListTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "List directory entries in workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		},
	}
}

func (t *ListTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("list args: %w", err)
		}
	}
	if in.Path == "" {
		in.Path = "."
	}

	resolved, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", fmt.Errorf("list directory: %w", err)
	}

	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(t.ws.Root(), filepath.Join(resolved, e.Name()))
		items = append(items, map[string]any{
			"name":       e.Name(),
			"path":       rel,
			"is_dir":     e.IsDir(),
			"size_bytes": info.Size(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["name"]) < fmt.Sprint(items[j]["name"])
	})

	return mustJSON(map[string]any{
		"ok":    true,
		"path":  resolved,
		"items": items,
	}), nil
}
