package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"coder/internal/chat"
	"coder/internal/security"
)

type GlobTool struct {
	ws *security.Workspace
}

func NewGlobTool(ws *security.Workspace) *GlobTool {
	return &GlobTool{ws: ws}
}

func (t *GlobTool) Name() string {
	return "glob"
}

func (t *GlobTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Find files using glob pattern inside workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string"},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func (t *GlobTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("glob args: %w", err)
	}
	pattern := strings.TrimSpace(in.Pattern)
	if pattern == "" {
		return "", fmt.Errorf("glob pattern is empty")
	}

	if filepath.IsAbs(pattern) {
		return "", fmt.Errorf("absolute glob pattern is not allowed")
	}

	patternAbs := filepath.Join(t.ws.Root(), pattern)
	matches, err := filepath.Glob(patternAbs)
	if err != nil {
		return "", fmt.Errorf("run glob: %w", err)
	}

	relMatches := make([]string, 0, len(matches))
	for _, m := range matches {
		resolved, err := t.ws.Resolve(m)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(t.ws.Root(), resolved)
		relMatches = append(relMatches, rel)
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"pattern": pattern,
		"matches": relMatches,
	}), nil
}
