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

// EditTool 提供基于 old_string/new_string 的安全局部替换，而不是让模型手写 unified diff。
// EditTool provides safe, localized edits based on old_string/new_string, instead of asking the model to handcraft unified diffs.
type EditTool struct {
	ws *security.Workspace
}

func NewEditTool(ws *security.Workspace) *EditTool {
	return &EditTool{ws: ws}
}

func (t *EditTool) Name() string {
	return "edit"
}

func (t *EditTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Safely edit an existing file in the workspace by replacing an old string with a new string. Prefer this tool for small, localized edits instead of patch or write.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string"},
					"old_string":  map[string]any{"type": "string"},
					"new_string":  map[string]any{"type": "string"},
					"replace_all": map[string]any{"type": "boolean"},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
	}
}

func (t *EditTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("edit args: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if in.OldString == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}
	if in.OldString == in.NewString {
		return "", fmt.Errorf("old_string and new_string must be different")
	}

	resolved, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	original := string(data)

	updated, replacements, err := applyStringEdit(original, in.OldString, in.NewString, in.ReplaceAll)
	if err != nil {
		return "", err
	}
	if replacements == 0 {
		return "", fmt.Errorf("old_string not found in file content")
	}
	// If nothing changed after normalized comparison, treat as no-op.
	operation := "updated"
	if normalizeLineEndings(original) == normalizeLineEndings(updated) {
		operation = "unchanged"
	}

	if operation != "unchanged" {
		parent, err := t.ws.Resolve(filepath.Dir(in.Path))
		if err != nil {
			return "", fmt.Errorf("resolve parent path: %w", err)
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", fmt.Errorf("create parent directories: %w", err)
		}
		if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
			return "", fmt.Errorf("write file: %w", err)
		}
	}

	diff, additions, deletions := "", 0, 0
	diffTruncated := false
	if operation == "updated" {
		diff, additions, deletions = BuildUnifiedDiff(strings.TrimSpace(in.Path), original, updated)
		diff, diffTruncated = TruncateUnifiedDiff(diff, 80, 8000)
	}

	return mustJSON(map[string]any{
		"ok":             true,
		"path":           resolved,
		"size":           len(updated),
		"operation":      operation,
		"replacements":   replacements,
		"additions":      additions,
		"deletions":      deletions,
		"diff":           diff,
		"diff_truncated": diffTruncated,
	}), nil
}

// applyStringEdit 在文件内容中查找 oldString，并用 newString 进行安全替换。
// 优先尝试精确子串匹配；若失败，再退回到按行 trim 后的块匹配。
// applyStringEdit finds oldString in the file content and safely replaces it with newString.
// It first tries exact substring matching, then falls back to line-trimmed block matching.
func applyStringEdit(content, oldString, newString string, replaceAll bool) (string, int, error) {
	// 1. 精确子串匹配 / exact substring match
	exactCount := strings.Count(content, oldString)
	if exactCount > 0 {
		if replaceAll {
			return strings.ReplaceAll(content, oldString, newString), exactCount, nil
		}
		if exactCount == 1 {
			idx := strings.Index(content, oldString)
			if idx < 0 {
				return "", 0, fmt.Errorf("internal error: exact match count > 0 but index < 0 (exactCount=%d)", exactCount)
			}
			updated := content[:idx] + newString + content[idx+len(oldString):]
			return updated, 1, nil
		}
		return "", 0, fmt.Errorf("old_string matches multiple locations (matches=%d); provide more surrounding context or set replace_all=true", exactCount)
	}

	// 2. 按行 trim 后的块匹配 / line-trimmed block match
	contentLines := strings.Split(content, "\n")
	searchLines := strings.Split(oldString, "\n")
	if len(searchLines) == 0 {
		return "", 0, fmt.Errorf("old_string must not be empty")
	}
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	if len(searchLines) == 0 {
		return "", 0, fmt.Errorf("old_string must not be only whitespace")
	}

	type span struct {
		start int
		end   int
	}
	var matches []span

	for i := 0; i <= len(contentLines)-len(searchLines); i++ {
		ok := true
		for j := 0; j < len(searchLines); j++ {
			if strings.TrimSpace(contentLines[i+j]) != strings.TrimSpace(searchLines[j]) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// 将行号转换为字节区间 / convert line indices to byte offsets
		startOffset := 0
		for k := 0; k < i; k++ {
			startOffset += len(contentLines[k])
			if k < len(contentLines)-1 {
				startOffset++
			}
		}
		endOffset := startOffset
		lastLine := i + len(searchLines) - 1
		for k := i; k <= lastLine; k++ {
			endOffset += len(contentLines[k])
			if k < len(contentLines)-1 {
				endOffset++
			}
		}
		matches = append(matches, span{start: startOffset, end: endOffset})
	}

	if len(matches) == 0 {
		return "", 0, fmt.Errorf("old_string not found in content (even after trimming line whitespace); ensure you copied the exact text (including newlines and indentation) from a recent read or grep result")
	}

	if replaceAll {
		// 从后往前替换，避免偏移 / replace from the end to avoid offset shifts
		updated := content
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			updated = updated[:m.start] + newString + updated[m.end:]
		}
		return updated, len(matches), nil
	}

	if len(matches) > 1 {
		return "", 0, fmt.Errorf("old_string is ambiguous after trimming (matches=%d); provide more surrounding context or set replace_all=true", len(matches))
	}

	m := matches[0]
	updated := content[:m.start] + newString + content[m.end:]
	return updated, 1, nil
}
