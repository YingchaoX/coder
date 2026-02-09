package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coder/internal/chat"
	"coder/internal/mcp"
)

type MCPProxyTool struct {
	server *mcp.Server
}

func NewMCPProxyTool(server *mcp.Server) *MCPProxyTool {
	return &MCPProxyTool{server: server}
}

func (t *MCPProxyTool) Name() string {
	if t.server == nil {
		return "mcp_unknown"
	}
	return t.server.ToolName()
}

func (t *MCPProxyTool) Definition() chat.ToolDef {
	name := t.Name()
	desc := "Call local MCP server"
	if t.server != nil {
		desc = fmt.Sprintf("Call local MCP server %s", name)
	}
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        name,
			Description: desc,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{"type": "object"},
				},
			},
		},
	}
}

func (t *MCPProxyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.server == nil {
		return "", fmt.Errorf("mcp server unavailable")
	}
	var in struct {
		Input map[string]any `json:"input"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("mcp args: %w", err)
		}
	}
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	output, err := t.server.Call(ctx, in.Input)
	if err != nil {
		return "", err
	}
	if len([]rune(output)) > 6000 {
		r := []rune(output)
		output = string(r[:6000]) + "...(truncated)"
	}

	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return mustJSON(map[string]any{"ok": true, "output": ""}), nil
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var generic any
		if err := json.Unmarshal([]byte(trimmed), &generic); err == nil {
			return mustJSON(map[string]any{"ok": true, "output": generic}), nil
		}
	}
	return mustJSON(map[string]any{"ok": true, "output": trimmed}), nil
}
