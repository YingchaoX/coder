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

// LSPDefinitionTool provides go-to-definition via LSP
// LSPDefinitionTool 通过 LSP 提供跳转到定义功能
type LSPDefinitionTool struct {
	manager *lsp.Manager
}

// NewLSPDefinitionTool creates a new LSP definition tool
// NewLSPDefinitionTool 创建一个新的 LSP 定义工具
func NewLSPDefinitionTool(manager *lsp.Manager) *LSPDefinitionTool {
	return &LSPDefinitionTool{manager: manager}
}

// Name returns the tool name
func (t *LSPDefinitionTool) Name() string {
	return "lsp_definition"
}

// Definition returns the tool definition
func (t *LSPDefinitionTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Find the definition location of a symbol using Language Server Protocol. Supports Python (.py) and Shell (.sh) files. Line and character are 0-based indices.",
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

// Execute runs the definition tool
func (t *LSPDefinitionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("lsp_definition args: %w", err)
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
			"fallback":      "Use 'grep' tool to search for the symbol definition",
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
			"fallback":      "Use 'grep' tool to search for the symbol definition",
			"server_status": "not_installed",
		}), nil
	}

	// Get client
	client, err := t.manager.GetClient(lang)
	if err != nil {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("failed to start LSP client: %v", err),
			"fallback":      "Use 'grep' tool to search for the symbol definition",
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
			"fallback":      "Use 'grep' tool to search for the symbol definition",
			"server_status": "error",
		}), nil
	}

	// Request definition
	locations, err := client.Definition(ctx, uri, in.Line, in.Character)
	if err != nil {
		return mustJSON(map[string]any{
			"ok":            false,
			"error":         fmt.Sprintf("LSP definition request failed: %v", err),
			"fallback":      "Use 'grep' tool to search for the symbol definition",
			"server_status": "error",
		}), nil
	}

	if len(locations) == 0 {
		return mustJSON(map[string]any{
			"ok":            true,
			"path":          in.Path,
			"line":          in.Line,
			"character":     in.Character,
			"locations":     []any{},
			"count":         0,
			"server_status": "active",
			"message":       "No definition found for the symbol at the given position",
		}), nil
	}

	// Convert locations to a more readable format
	resultLocations := make([]map[string]any, len(locations))
	for i, loc := range locations {
		// Convert file:// URI to relative path
		locPath := loc.URI
		if strings.HasPrefix(locPath, "file://") {
			locPath = locPath[7:]
		}

		// Try to make it relative to workspace
		if rel, err := filepath.Rel(".", locPath); err == nil {
			locPath = rel
		}

		resultLocations[i] = map[string]any{
			"uri": locPath,
			"range": map[string]any{
				"start": map[string]any{
					"line":      loc.Range.Start.Line,
					"character": loc.Range.Start.Character,
				},
				"end": map[string]any{
					"line":      loc.Range.End.Line,
					"character": loc.Range.End.Character,
				},
			},
		}
	}

	return mustJSON(map[string]any{
		"ok":            true,
		"path":          in.Path,
		"line":          in.Line,
		"character":     in.Character,
		"locations":     resultLocations,
		"count":         len(locations),
		"server_status": "active",
	}), nil
}
