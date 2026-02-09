package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"coder/internal/chat"
	"coder/internal/security"
)

type ReadTool struct {
	ws *security.Workspace
}

func NewReadTool(ws *security.Workspace) *ReadTool {
	return &ReadTool{ws: ws}
}

func (t *ReadTool) Name() string {
	return "read"
}

func (t *ReadTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Read file content from workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *ReadTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("read args: %w", err)
	}
	resolved, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return mustJSON(map[string]any{
		"ok":      true,
		"path":    resolved,
		"content": string(data),
	}), nil
}
