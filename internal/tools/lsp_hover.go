package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coder/internal/chat"
	"coder/internal/lsp"
)

// LSPHoverTool provides hover information via LSP
// LSPHoverTool 通过 LSP 提供悬停信息功能
type LSPHoverTool struct {
	manager *lsp.Manager
}

// NewLSPHoverTool creates a new LSP hover tool
// NewLSPHoverTool 创建一个新的 LSP 悬停工具
func NewLSPHoverTool(manager *lsp.Manager) *LSPHoverTool {
	return &LSPHoverTool{manager: manager}
}

// Name returns the tool name
func (t *LSPHoverTool) Name() string {
	return "lsp_hover"
}

// Definition returns the tool definition
func (t *LSPHoverTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Get hover information (type, documentation) for a symbol using Language Server Protocol. Supports Python (.py) and Shell (.sh) files. Line and character are 0-based indices.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file containing the symbol",
					},
					"line": map[string]any{
						"type":        "integer",
						"description": "Line number (0-based) where the symbol is located",
					},
					"character": map[string]any{
						"type":        "integer",
						"description": "Character position (0-based) in the line",
					},
				},
				"required": []string{"path", "line", "character"},
			},
		},
	}
}

// Execute runs the hover tool
func (t *LSPHoverTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("lsp_hover args: %w", err)
	}

	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}

	// Check if file exists
	absPath, err := filepath.Abs(in.Path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	if _, err := os.Stat(absPath); err != nil {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("file not found: %s", in.Path),
			"server_status": "unavailable",
		}), nil
	}

	// Check if LSP is available for this file
	lang := t.manager.GetLanguageFromPath(in.Path)
	if lang == "" {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("file type not supported by LSP: %s", in.Path),
			"fallback":      "Use 'read' tool to examine the code",
			"server_status": "unsupported",
		}), nil
	}

	if !t.manager.IsAvailable(lang) {
		missing := t.manager.GetMissingServers()
		var hint string
		for _, m := range missing {
			if m.Lang == lang {
				hint = m.InstallHint
				break
			}
		}

		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("LSP server for %s is not installed", lang),
			"install_hint":  hint,
			"fallback":      "Use 'read' tool to examine the code",
			"server_status": "not_installed",
		}), nil
	}

	// Get client
	client, err := t.manager.GetClient(lang)
	if err != nil {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("failed to start LSP client: %v", err),
			"fallback":      "Use 'read' tool to examine the code",
			"server_status": "error",
		}), nil
	}

	// Read file content and open it
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	uri := "file://" + absPath
	if err := client.DidOpen(uri, lang, string(content), 1); err != nil {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("failed to open file in LSP: %v", err),
			"fallback":      "Use 'read' tool to examine the code",
			"server_status": "error",
		}), nil
	}

	// Request hover
	hover, err := client.Hover(ctx, uri, in.Line, in.Character)
	if err != nil {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("LSP hover request failed: %v", err),
			"fallback":      "Use 'read' tool to examine the code",
			"server_status": "error",
		}), nil
	}

	if hover == nil || hover.Contents.Value == "" {
		return mustJSON(map[string]any{
			"ok":            true,
			"path":          in.Path,
			"line":          in.Line,
			"character":     in.Character,
			"contents":      "",
			"server_status": "active",
			"message":       "No hover information available for the symbol at the given position",
		}), nil
	}

	return mustJSON(map[string]any{
		"ok":            true,
		"path":          in.Path,
		"line":          in.Line,
		"character":     in.Character,
		"contents":      hover.Contents.Value,
		"content_kind":  hover.Contents.Kind,
		"server_status": "active",
	}), nil
}
