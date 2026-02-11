package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (o *Orchestrator) pickVerifyCommand() string {
	for _, cmd := range o.workflow.VerifyCommands {
		trimmed := strings.TrimSpace(cmd)
		if trimmed != "" {
			return trimmed
		}
	}
	root := strings.TrimSpace(o.workspaceRoot)
	if root == "" {
		root = "."
	}
	if exists(filepath.Join(root, "go.mod")) {
		return "go test ./..."
	}
	if exists(filepath.Join(root, "pyproject.toml")) || exists(filepath.Join(root, "pytest.ini")) || exists(filepath.Join(root, "requirements.txt")) {
		return "pytest"
	}
	if exists(filepath.Join(root, "package.json")) {
		return "npm test -- --watch=false"
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (o *Orchestrator) runAutoVerify(ctx context.Context, command string, attempt int, out io.Writer) (bool, bool, error) {
	args := mustJSON(map[string]string{"command": command})
	rawArgs := json.RawMessage(args)
	if out != nil {
		renderToolStart(out, fmt.Sprintf("* Auto verify (attempt %d) %s", attempt, quoteOrDash(command)))
	}
	result, err := o.registry.Execute(ctx, "bash", rawArgs)
	callID := fmt.Sprintf("auto_verify_%d", attempt)
	if err != nil {
		if out != nil {
			renderToolError(out, summarizeForLog(err.Error()))
		}
		return false, false, err
	}
	if out != nil {
		renderToolResult(out, summarizeToolResult("bash", result))
	}
	o.appendSyntheticToolExchange("bash", args, result, callID)
	parsed := parseJSONObject(result)
	if getInt(parsed, "exit_code", 1) == 0 {
		return true, false, nil
	}
	return false, shouldRetryAutoVerifyFailure(parsed), nil
}

func editedPathFromToolCall(tool string, args json.RawMessage) string {
	switch strings.TrimSpace(tool) {
	case "write":
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return ""
		}
		return strings.TrimSpace(payload.Path)
	case "patch":
		// Best-effort extraction of the first patched file path from unified diff.
		// Format we expect (same as internal/tools/patch.go):
		//   --- a/old/path
		//   +++ b/new/path
		var payload struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return ""
		}
		patch := strings.TrimSpace(payload.Patch)
		if patch == "" {
			return ""
		}
		lines := strings.Split(patch, "\n")
		for _, raw := range lines {
			line := strings.TrimSpace(raw)
			if !strings.HasPrefix(line, "+++") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(line, "+++"))
			if rest == "" || rest == "/dev/null" {
				continue
			}
			if idx := strings.IndexAny(rest, "\t "); idx >= 0 {
				rest = rest[:idx]
			}
			rest = strings.TrimSpace(rest)
			rest = strings.TrimPrefix(rest, "a/")
			rest = strings.TrimPrefix(rest, "b/")
			rest = filepath.ToSlash(strings.TrimSpace(rest))
			if rest == "" || rest == "/dev/null" {
				continue
			}
			return rest
		}
	}
	return ""
}

func shouldAutoVerifyEditedPaths(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, path := range paths {
		// edits under .coder/ are treated as configuration changes and do not require auto-verify
		if isCoderConfigPath(path) {
			continue
		}
		if !isDocLikePath(path) {
			return true
		}
	}
	return false
}

func isCoderConfigPath(path string) bool {
	cleaned := strings.TrimSpace(strings.ToLower(filepath.ToSlash(path)))
	if cleaned == "" {
		return false
	}
	if strings.HasPrefix(cleaned, ".coder/") || strings.Contains(cleaned, "/.coder/") {
		return true
	}
	return false
}

func isDocLikePath(path string) bool {
	cleaned := strings.TrimSpace(strings.ToLower(filepath.ToSlash(path)))
	if cleaned == "" {
		return false
	}
	if strings.HasPrefix(cleaned, "docs/") || strings.Contains(cleaned, "/docs/") {
		return true
	}
	switch filepath.Ext(cleaned) {
	case ".md", ".mdx", ".txt", ".rst", ".adoc":
		return true
	default:
		return false
	}
}

func shouldRetryAutoVerifyFailure(result map[string]any) bool {
	stderr := strings.ToLower(strings.TrimSpace(getString(result, "stderr", "")))
	stdout := strings.ToLower(strings.TrimSpace(getString(result, "stdout", "")))
	combined := strings.TrimSpace(stderr + "\n" + stdout)
	if combined == "" {
		return true
	}
	if strings.Contains(combined, "missing lc_uuid") || strings.Contains(combined, "dyld") {
		return false
	}
	if strings.Contains(combined, "command not found") || strings.Contains(combined, "not recognized as an internal or external command") {
		return false
	}
	if strings.Contains(combined, "no such file or directory") {
		for _, startupFile := range []string{".profile", ".zprofile", ".zshrc", ".bash_profile", ".bashrc"} {
			if strings.Contains(combined, startupFile) {
				return false
			}
		}
	}
	return true
}
