package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"coder/internal/chat"
	"coder/internal/security"
)

// GitManager manages git availability detection and caching
type GitManager struct {
	ws        *security.Workspace
	once      sync.Once
	available bool
	isRepo    bool
	version   string
}

// NewGitManager creates a new GitManager instance
func NewGitManager(ws *security.Workspace) *GitManager {
	return &GitManager{ws: ws}
}

// Check returns git availability, repo status, and version
// Uses sync.Once to ensure detection runs only once
func (m *GitManager) Check() (available bool, isRepo bool, version string) {
	m.once.Do(func() {
		m.available, m.version = m.checkGit()
		if m.available {
			m.isRepo = m.checkRepo()
		}
	})
	return m.available, m.isRepo, m.version
}

// checkGit detects if git is installed and returns version
func (m *GitManager) checkGit() (bool, string) {
	cmd := exec.Command("git", "--version")
	out, err := cmd.Output()
	if err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(string(out))
}

// checkRepo detects if current directory is a git repository
func (m *GitManager) checkRepo() bool {
	cmd := exec.Command("git", "-C", m.ws.Root(), "rev-parse", "--git-dir")
	err := cmd.Run()
	return err == nil
}

// checkGitAvailable is a helper that checks git availability and returns error response if not available
func checkGitAvailable(manager *GitManager) (map[string]any, bool) {
	available, isRepo, _ := manager.Check()
	if !available {
		return map[string]any{
			"ok":    false,
			"error": "git not installed",
			"hint":  "Install git to use git tools, or use 'bash' tool with git commands",
		}, false
	}
	if !isRepo {
		return map[string]any{
			"ok":    false,
			"error": "not a git repository",
			"hint":  "Initialize a git repository with 'git init' or use 'bash' tool",
		}, false
	}
	return nil, true
}

// GitStatusTool shows git working tree status
type GitStatusTool struct {
	ws      *security.Workspace
	manager *GitManager
}

// NewGitStatusTool creates a new GitStatusTool instance
func NewGitStatusTool(ws *security.Workspace, manager *GitManager) *GitStatusTool {
	return &GitStatusTool{ws: ws, manager: manager}
}

// Name returns the tool name
func (t *GitStatusTool) Name() string {
	return "git_status"
}

// Definition returns the tool definition
func (t *GitStatusTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Show git working tree status",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"short": map[string]any{
						"type":        "boolean",
						"description": "Show in short format",
					},
				},
			},
		},
	}
}

// Execute runs the git status command
func (t *GitStatusTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Short bool `json:"short"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("git_status args: %w", err)
	}

	if resp, ok := checkGitAvailable(t.manager); !ok {
		return mustJSON(resp), nil
	}

	cmdArgs := []string{"-C", t.ws.Root(), "status"}
	if in.Short {
		cmdArgs = append(cmdArgs, "--short")
	}

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return mustJSON(map[string]any{
			"ok":    false,
			"error": string(out),
		}), nil
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"content": string(out),
	}), nil
}

// GitDiffTool shows changes between working tree and index/staged
type GitDiffTool struct {
	ws      *security.Workspace
	manager *GitManager
}

// NewGitDiffTool creates a new GitDiffTool instance
func NewGitDiffTool(ws *security.Workspace, manager *GitManager) *GitDiffTool {
	return &GitDiffTool{ws: ws, manager: manager}
}

// Name returns the tool name
func (t *GitDiffTool) Name() string {
	return "git_diff"
}

// Definition returns the tool definition
func (t *GitDiffTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Show changes between working tree and index/staged",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"staged": map[string]any{
						"type":        "boolean",
						"description": "Show staged changes (git diff --staged)",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Specific file or directory to diff",
					},
				},
			},
		},
	}
}

// Execute runs the git diff command
func (t *GitDiffTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Staged bool   `json:"staged"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("git_diff args: %w", err)
	}

	if resp, ok := checkGitAvailable(t.manager); !ok {
		return mustJSON(resp), nil
	}

	cmdArgs := []string{"-C", t.ws.Root(), "diff"}
	if in.Staged {
		cmdArgs = append(cmdArgs, "--staged")
	}
	if in.Path != "" {
		resolved, err := t.ws.Resolve(in.Path)
		if err != nil {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		cmdArgs = append(cmdArgs, resolved)
	}

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return mustJSON(map[string]any{
			"ok":    false,
			"error": string(out),
		}), nil
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"content": string(out),
	}), nil
}

// GitLogTool shows commit history
type GitLogTool struct {
	ws      *security.Workspace
	manager *GitManager
}

// NewGitLogTool creates a new GitLogTool instance
func NewGitLogTool(ws *security.Workspace, manager *GitManager) *GitLogTool {
	return &GitLogTool{ws: ws, manager: manager}
}

// Name returns the tool name
func (t *GitLogTool) Name() string {
	return "git_log"
}

// Definition returns the tool definition
func (t *GitLogTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Show commit history",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of commits to show (default 20)",
					},
					"oneline": map[string]any{
						"type":        "boolean",
						"description": "Show one line per commit",
					},
				},
			},
		},
	}
}

// Execute runs the git log command
func (t *GitLogTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Limit   int  `json:"limit"`
		Oneline bool `json:"oneline"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("git_log args: %w", err)
	}

	if resp, ok := checkGitAvailable(t.manager); !ok {
		return mustJSON(resp), nil
	}

	if in.Limit <= 0 {
		in.Limit = 20
	}
	if in.Limit > 100 {
		in.Limit = 100 // Cap at 100 for performance
	}

	cmdArgs := []string{"-C", t.ws.Root(), "log"}
	if in.Oneline {
		cmdArgs = append(cmdArgs, "--oneline")
	}
	cmdArgs = append(cmdArgs, fmt.Sprintf("-%d", in.Limit))

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return mustJSON(map[string]any{
			"ok":    false,
			"error": string(out),
		}), nil
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"content": string(out),
	}), nil
}

// GitAddTool adds file contents to the staging area
type GitAddTool struct {
	ws      *security.Workspace
	manager *GitManager
}

// NewGitAddTool creates a new GitAddTool instance
func NewGitAddTool(ws *security.Workspace, manager *GitManager) *GitAddTool {
	return &GitAddTool{ws: ws, manager: manager}
}

// Name returns the tool name
func (t *GitAddTool) Name() string {
	return "git_add"
}

// Definition returns the tool definition
func (t *GitAddTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Add file contents to the staging area",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Files to add (use '.' for all)",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// Execute runs the git add command
func (t *GitAddTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("git_add args: %w", err)
	}

	if in.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	if resp, ok := checkGitAvailable(t.manager); !ok {
		return mustJSON(resp), nil
	}

	// Resolve path for security
	resolved, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", t.ws.Root(), "add", resolved)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return mustJSON(map[string]any{
			"ok":    false,
			"error": string(out),
		}), nil
	}

	return mustJSON(map[string]any{
		"ok":   true,
		"path": in.Path,
	}), nil
}

// ApprovalRequest returns approval request for git_add
func (t *GitAddTool) ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error) {
	return &ApprovalRequest{
		Tool:    t.Name(),
		Reason:  "git add modifies staging area",
		RawArgs: string(args),
	}, nil
}

// Dangerous commit arguments that should be blocked or require special approval
var dangerousCommitArgs = regexp.MustCompile(`(?i)--amend|--force|--no-verify|-n(\s|$)|--allow-empty`)

// GitCommitTool creates a new commit
type GitCommitTool struct {
	ws      *security.Workspace
	manager *GitManager
}

// NewGitCommitTool creates a new GitCommitTool instance
func NewGitCommitTool(ws *security.Workspace, manager *GitManager) *GitCommitTool {
	return &GitCommitTool{ws: ws, manager: manager}
}

// Name returns the tool name
func (t *GitCommitTool) Name() string {
	return "git_commit"
}

// Definition returns the tool definition
func (t *GitCommitTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Commit staged changes",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{
						"type":        "string",
						"description": "Commit message",
					},
				},
				"required": []string{"message"},
			},
		},
	}
}

// Execute runs the git commit command
func (t *GitCommitTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("git_commit args: %w", err)
	}

	if in.Message == "" {
		return "", fmt.Errorf("message is required")
	}

	if resp, ok := checkGitAvailable(t.manager); !ok {
		return mustJSON(resp), nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", t.ws.Root(), "commit", "-m", in.Message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return mustJSON(map[string]any{
			"ok":    false,
			"error": string(out),
		}), nil
	}

	// Extract commit hash from output
	output := string(out)
	commitHash := ""
	if idx := strings.Index(output, "]"); idx > 0 {
		// Output format: [branch hash] message
		start := strings.LastIndex(output[:idx], " ")
		if start > 0 {
			commitHash = output[start+1 : idx]
		}
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"commit":  commitHash,
		"message": in.Message,
	}), nil
}

// ApprovalRequest returns approval request for git_commit
// Checks for dangerous arguments in the commit message
func (t *GitCommitTool) ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}

	// Check for dangerous flags in commit message
	if dangerousCommitArgs.MatchString(in.Message) {
		return &ApprovalRequest{
			Tool:    t.Name(),
			Reason:  "commit message may contain dangerous flags",
			RawArgs: string(args),
		}, nil
	}

	return &ApprovalRequest{
		Tool:    t.Name(),
		Reason:  "git commit creates a new commit",
		RawArgs: string(args),
	}, nil
}
