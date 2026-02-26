package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coder/internal/chat"
	"coder/internal/permission"
	"coder/internal/security"
)

type ReadTool struct {
	ws     *security.Workspace
	policy *permission.Policy
}

func NewReadTool(ws *security.Workspace, policy *permission.Policy) *ReadTool {
	return &ReadTool{ws: ws, policy: policy}
}

func (t *ReadTool) Name() string {
	return "read"
}

// ApprovalRequest 实现 ApprovalAware 接口，检查是否需要审批外部路径
// ApprovalRequest implements ApprovalAware interface to check if external path needs approval
func (t *ReadTool) ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("parse args: %w", err)
	}

	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, nil
	}

	// 解析路径（展开 ~）
	if strings.HasPrefix(path, "~") {
		expanded, err := t.expandHomePath(path)
		if err != nil {
			return nil, nil // 解析失败，让 Execute 处理错误
		}
		path = expanded
	}

	// 如果不是绝对路径，不需要审批（相对路径限制在 workspace 内）
	if !filepath.IsAbs(path) {
		return nil, nil
	}

	// 检查是否在 workspace 内
	rel, err := filepath.Rel(t.ws.Root(), path)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		// 在 workspace 内，不需要审批
		return nil, nil
	}

	// 在 workspace 外，检查权限策略
	decision := t.policy.ExternalDirDecision()
	switch decision {
	case permission.DecisionAllow:
		// 允许，不需要审批
		return nil, nil
	case permission.DecisionDeny:
		// 拒绝，不需要审批（直接拒绝）
		return nil, nil
	default:
		// ask，需要审批
		return &ApprovalRequest{
			Tool:   t.Name(),
			Reason: fmt.Sprintf("read external path: %s", in.Path),
		}, nil
	}
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
						"description": "Max number of lines to read. Defaults to 50 and is capped at 200.",
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
	resolved, resolveErr := t.resolvePath(in.Path)
	if resolveErr != nil {
		return "", fmt.Errorf("resolve path: %w", resolveErr)
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

// resolvePath 统一处理路径解析，支持相对路径、绝对路径和 ~ 路径
// resolvePath handles path resolution for relative, absolute and ~ paths
func (t *ReadTool) resolvePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	// 1. 处理 ~ 路径：展开为家目录绝对路径
	// Handle ~ path: expand to home directory
	if strings.HasPrefix(path, "~") {
		expanded, err := t.expandHomePath(path)
		if err != nil {
			return "", err
		}
		path = expanded
	}

	// 2. 如果是绝对路径，检查外部权限
	// If absolute path, check external permission
	if filepath.IsAbs(path) {
		return t.checkExternalPath(path)
	}

	// 3. 相对路径：限制在 workspace 内
	// Relative path: restrict to workspace
	return t.ws.Resolve(path)
}

// expandHomePath 将 ~ 展开为家目录绝对路径
// expandHomePath expands ~ to home directory absolute path
func (t *ReadTool) expandHomePath(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	// ~username 格式暂不支持 / ~username format not supported
	return "", fmt.Errorf("unsupported path format: %s", path)
}

// checkExternalPath 检查外部路径权限
// checkExternalPath checks external path permission
// 注意：当策略为 ask 时，如果 Execute 被调用，说明审批已通过
// Note: when policy is ask, if Execute is called, approval has been granted
func (t *ReadTool) checkExternalPath(absPath string) (string, error) {
	// 检查路径是否在 workspace 内 / Check if path is inside workspace
	rel, err := filepath.Rel(t.ws.Root(), absPath)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		// 路径在 workspace 内，直接允许 / Path inside workspace, allow directly
		return absPath, nil
	}

	// 路径在 workspace 外，检查外部路径权限 / Path outside workspace, check permission
	decision := t.policy.ExternalDirDecision()
	switch decision {
	case permission.DecisionAllow:
		// 明确允许 / Explicitly allowed
		return absPath, nil
	case permission.DecisionDeny:
		// 明确拒绝 / Explicitly denied
		return "", fmt.Errorf("external path access denied by policy")
	default:
		// ask 策略：如果 Execute 被调用，说明审批已通过
		// ask policy: if Execute is called, approval has been granted
		return absPath, nil
	}
}
