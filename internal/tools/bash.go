package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"coder/internal/chat"
	"coder/internal/security"
)

var overwriteRedirectPattern = regexp.MustCompile(`(^|\s)(1>|2>|>)(\s*)([^\s]+)`)

type BashTool struct {
	workspaceRoot    string
	commandTimeoutMS int
	outputLimitBytes int
}

func NewBashTool(workspaceRoot string, commandTimeoutMS, outputLimitBytes int) *BashTool {
	return &BashTool{
		workspaceRoot:    workspaceRoot,
		commandTimeoutMS: commandTimeoutMS,
		outputLimitBytes: outputLimitBytes,
	}
}

func (t *BashTool) Name() string {
	return "bash"
}

func (t *BashTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Run a shell command in workspace root",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
	}
}

func (t *BashTool) ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error) {
	in, err := parseBashArgs(args)
	if err != nil {
		return nil, fmt.Errorf("bash args: %w", err)
	}

	risk := security.AnalyzeCommand(in.Command)
	if risk.RequireApproval {
		return &ApprovalRequest{
			Tool:    t.Name(),
			Reason:  risk.Reason,
			RawArgs: string(args),
		}, nil
	}

	redirectTarget := extractExistingRedirectTarget(in.Command, t.workspaceRoot)
	if redirectTarget != "" {
		return &ApprovalRequest{
			Tool:    t.Name(),
			Reason:  fmt.Sprintf("overwrite redirection target exists: %s", redirectTarget),
			RawArgs: string(args),
		}, nil
	}

	return nil, nil
}

func (t *BashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := parseBashArgs(args)
	if err != nil {
		return "", fmt.Errorf("bash args: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", errors.New("bash command is empty")
	}

	timeout := time.Duration(t.commandTimeoutMS) * time.Millisecond
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "/bin/sh", "-lc", in.Command)
	cmd.Dir = t.workspaceRoot

	stdout := newCappedBuffer(t.outputLimitBytes)
	stderr := newCappedBuffer(t.outputLimitBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := time.Now()
	err = cmd.Run()
	dur := time.Since(start)

	exitCode := 0
	ok := true
	if err != nil {
		ok = false
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			exitCode = 124
		} else {
			return "", fmt.Errorf("run bash command: %w", err)
		}
	}

	return mustJSON(map[string]any{
		"ok":          ok,
		"command":     in.Command,
		"exit_code":   exitCode,
		"stdout":      stdout.String(),
		"stderr":      stderr.String(),
		"truncated":   stdout.truncated || stderr.truncated,
		"duration_ms": dur.Milliseconds(),
	}), nil
}

type bashArgs struct {
	Command string `json:"command"`
}

func parseBashArgs(args json.RawMessage) (bashArgs, error) {
	var in bashArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return bashArgs{}, err
	}
	return in, nil
}

func extractExistingRedirectTarget(command, workspaceRoot string) string {
	matches := overwriteRedirectPattern.FindAllStringSubmatch(command, -1)
	for _, m := range matches {
		if len(m) < 5 {
			continue
		}
		target := strings.Trim(m[4], `"'`)
		if target == "" {
			continue
		}
		resolved := target
		if !filepath.IsAbs(target) {
			resolved = filepath.Join(workspaceRoot, target)
		}
		info, err := os.Stat(resolved)
		if err == nil && !info.IsDir() {
			return resolved
		}
	}
	return ""
}

type cappedBuffer struct {
	max       int
	buf       bytes.Buffer
	truncated bool
}

func newCappedBuffer(max int) *cappedBuffer {
	if max <= 0 {
		max = 1 << 20
	}
	return &cappedBuffer{max: max}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.truncated {
		return len(p), nil
	}
	remain := b.max - b.buf.Len()
	if remain <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		_, _ = b.buf.Write(p[:remain])
		b.truncated = true
		return len(p), nil
	}
	_, err := b.buf.Write(p)
	return len(p), err
}

func (b *cappedBuffer) String() string {
	if !b.truncated {
		return b.buf.String()
	}
	var out bytes.Buffer
	_, _ = io.Copy(&out, bytes.NewReader(b.buf.Bytes()))
	out.WriteString("\n[output truncated]")
	return out.String()
}
