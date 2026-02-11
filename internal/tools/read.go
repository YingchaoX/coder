package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
					"path": map[string]any{
						"type": "string",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Line offset (1-based). Defaults to 1 when not provided.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max number of lines to read. Defaults to 100 and is capped at 200.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *ReadTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("read args: %w", err)
	}
	const (
		defaultLimit = 50
		maxLimit     = 200
	)
	// isTail: any negative offset means "tail mode", read the last N lines (N = limit).
	isTail := in.Offset < 0
	if !isTail && in.Offset <= 0 {
		in.Offset = 1
	}
	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}
	if in.Limit > maxLimit {
		in.Limit = maxLimit
	}
	resolved, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	collected := 0
	startLine := 0
	endLine := 0
	var lines []string

	for scanner.Scan() {
		lineNo++
		text := scanner.Text()

		if isTail {
			// Tail mode: keep only the last in.Limit lines in a sliding window.
			if len(lines) == in.Limit {
				lines = lines[1:]
			}
			lines = append(lines, text)
			continue
		}

		if lineNo < in.Offset {
			continue
		}
		if collected < in.Limit {
			if startLine == 0 {
				startLine = lineNo
			}
			lines = append(lines, text)
			collected++
			endLine = lineNo
			continue
		}
		// 已经达到本次 limit，继续扫描剩余行但不再收集，用于判断 has_more。
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	if isTail {
		// Compute start and end line for the last page.
		endLine = lineNo
		if len(lines) > 0 {
			startLine = endLine - len(lines) + 1
		}
	}

	hasMore := false
	if isTail {
		// In tail mode, has_more indicates there are earlier lines before this page.
		if startLine > 1 {
			hasMore = true
		}
	} else {
		if lineNo > endLine && endLine != 0 {
			// 文件在当前分块之后还有更多内容。
			hasMore = true
		}
	}

	return mustJSON(map[string]any{
		"ok":         true,
		"path":       resolved,
		"content":    strings.Join(lines, "\n"),
		"start_line": startLine,
		"end_line":   endLine,
		"has_more":   hasMore,
	}), nil
}
