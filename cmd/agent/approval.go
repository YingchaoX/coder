package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"coder/internal/orchestrator"
	"coder/internal/security"
	"coder/internal/tools"

	"github.com/chzyer/readline"
)

func approvalPrompt(reader lineInput, ws *security.Workspace) orchestrator.ApprovalFunc {
	return func(_ context.Context, req tools.ApprovalRequest) (bool, error) {
		fmt.Println()
		fmt.Printf("Approval required for tool=%s\n", req.Tool)
		fmt.Printf("Reason: %s\n", req.Reason)
		formatted := formatApprovalArgs(req, ws)
		if strings.TrimSpace(formatted) == "" {
			fmt.Println("Args: -")
		} else {
			fmt.Println("Args:")
			fmt.Println(indentBlock(formatted, "  "))
		}
		line, err := reader.ReadLine("Allow once? [y/N]: ")
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) || errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes", nil
	}
}

func formatApprovalArgs(req tools.ApprovalRequest, ws *security.Workspace) string {
	switch strings.TrimSpace(req.Tool) {
	case "write":
		return formatWriteApprovalArgs(req.RawArgs, ws)
	case "patch":
		return formatPatchApprovalArgs(req.RawArgs)
	default:
		return prettyJSONOrRaw(req.RawArgs, 2400)
	}
}

func formatWriteApprovalArgs(rawArgs string, ws *security.Workspace) string {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &in); err != nil {
		return prettyJSONOrRaw(rawArgs, 2400)
	}

	path := strings.TrimSpace(in.Path)
	if path == "" {
		path = "-"
	}
	resolved := path
	original := ""
	exists := false
	if ws != nil {
		if rp, err := ws.Resolve(path); err == nil {
			resolved = rp
			if data, readErr := os.ReadFile(rp); readErr == nil {
				exists = true
				original = string(data)
			}
		}
	}

	operation := "create"
	if exists {
		operation = "update"
		if normalizeLineEndings(original) == normalizeLineEndings(in.Content) {
			operation = "unchanged"
		}
	}
	lines := []string{
		fmt.Sprintf("path: %s", path),
		fmt.Sprintf("resolved_path: %s", resolved),
		fmt.Sprintf("bytes: %d", len(in.Content)),
		fmt.Sprintf("operation: %s", operation),
	}
	if operation == "update" {
		diff, additions, deletions := tools.BuildUnifiedDiff(path, original, in.Content)
		diff, truncated := tools.TruncateUnifiedDiff(diff, 60, 6000)
		lines = append(lines, fmt.Sprintf("changes: +%d -%d", additions, deletions))
		if strings.TrimSpace(diff) != "" {
			if truncated {
				lines = append(lines, "diff_preview: (truncated)")
			} else {
				lines = append(lines, "diff_preview:")
			}
			lines = append(lines, indentBlock(colorizeUnifiedDiff(diff), "  "))
		}
	}
	return strings.Join(lines, "\n")
}

func formatPatchApprovalArgs(rawArgs string) string {
	var in struct {
		Patch  string `json:"patch"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &in); err != nil {
		return prettyJSONOrRaw(rawArgs, 2400)
	}
	lines := []string{
		fmt.Sprintf("dry_run: %v", in.DryRun),
	}
	preview := strings.TrimSpace(in.Patch)
	if preview != "" {
		preview = truncateLinesAndBytes(preview, 60, 6000)
		preview = colorizeUnifiedDiff(preview)
		lines = append(lines, "patch_preview:")
		lines = append(lines, indentBlock(preview, "  "))
	}
	return strings.Join(lines, "\n")
}

func prettyJSONOrRaw(raw string, maxBytes int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		if data, marshalErr := json.MarshalIndent(parsed, "", "  "); marshalErr == nil {
			return truncateLinesAndBytes(string(data), 120, maxBytes)
		}
	}
	return truncateLinesAndBytes(trimmed, 120, maxBytes)
}

func truncateLinesAndBytes(text string, maxLines, maxBytes int) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	truncated := false
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[:maxBytes]
		truncated = true
	}
	out = strings.TrimRight(out, "\n")
	if truncated {
		out += "\n... (truncated)"
	}
	return out
}

func indentBlock(text, prefix string) string {
	if strings.TrimSpace(text) == "" {
		return prefix + "-"
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func colorizeUnifiedDiff(diff string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(diff, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lines[i] = colorizeDiffLine(line)
	}
	return strings.Join(lines, "\n")
}

func colorizeDiffLine(line string) string {
	if line == "" || !cliEnableColor() {
		return line
	}
	leading := len(line) - len(strings.TrimLeft(line, " \t"))
	prefix := line[:leading]
	body := line[leading:]
	if body == "" {
		return line
	}
	switch {
	case strings.HasPrefix(body, "diff --"), strings.HasPrefix(body, "index "), strings.HasPrefix(body, "---"), strings.HasPrefix(body, "+++"):
		return prefix + cliStyle(body, cliAnsiYellow)
	case strings.HasPrefix(body, "@@"):
		return prefix + cliStyle(body, cliAnsiCyan)
	case strings.HasPrefix(body, "+"):
		return prefix + cliStyle(body, cliAnsiGreen)
	case strings.HasPrefix(body, "-"):
		return prefix + cliStyle(body, cliAnsiRed)
	default:
		return line
	}
}

func cliStyle(text, code string) string {
	if text == "" || code == "" || !cliEnableColor() {
		return text
	}
	return code + text + cliAnsiReset
}

func cliEnableColor() bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("AGENT_NO_COLOR")) != "" {
		return false
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))) != "dumb"
}

func normalizeLineEndings(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(content, "\r", "\n")
}
