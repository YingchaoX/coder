package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"coder/internal/chat"
)

func parseBangCommand(input string) (command string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "!") {
		return "", false
	}
	command = strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	return command, true
}

const maxBangOutputLines = 20

func (o *Orchestrator) runBangCommand(ctx context.Context, rawInput, command string, out io.Writer) (string, error) {
	o.appendMessage(chat.Message{Role: "user", Content: rawInput})
	defer func() {
		_ = o.flushSessionToFile(ctx)
	}()

	if strings.TrimSpace(command) == "" {
		msg := "command mode error: empty command after '!'."
		o.appendMessage(chat.Message{Role: "assistant", Content: msg})
		_ = o.flushSessionToFile(ctx)
		if out != nil {
			renderToolError(out, msg)
		}
		return msg, nil
	}
	if !o.isToolAllowed("bash") {
		msg := fmt.Sprintf("command mode denied: bash disabled by active agent %s", o.activeAgent.Name)
		o.appendMessage(chat.Message{Role: "assistant", Content: msg})
		_ = o.flushSessionToFile(ctx)
		if out != nil {
			renderToolBlocked(out, msg)
		}
		return msg, nil
	}

	args := mustJSON(map[string]string{"command": command})
	rawArgs := json.RawMessage(args)

	if out != nil {
		renderToolStart(out, formatToolStart("bash", args))
	}

	// 命令模式下跳过 Policy 与风险审批链，等价用户直接执行 shell。
	result, err := o.registry.Execute(ctx, "bash", rawArgs)
	if err != nil {
		return "", fmt.Errorf("execute command mode: %w", err)
	}
	if out != nil {
		renderToolResult(out, summarizeToolResult("bash", result))
	}

	msg := formatBangCommandResult(command, result)
	o.appendMessage(chat.Message{Role: "assistant", Content: msg})
	_ = o.flushSessionToFile(ctx)
	if out != nil {
		renderCommandBlock(out, msg)
	}
	o.emitContextUpdate()
	return msg, nil
}

func formatBangCommandResult(command, rawResult string) string {
	result := parseJSONObject(rawResult)
	exitCode := getInt(result, "exit_code", -1)
	duration := getInt(result, "duration_ms", 0)
	stdout := getString(result, "stdout", "")
	stderr := getString(result, "stderr", "")
	truncated := false
	if result != nil {
		v, ok := result["truncated"].(bool)
		if ok {
			truncated = v
		}
	}
	var b strings.Builder
	b.WriteString("$ ")
	b.WriteString(command)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("exit=%d duration=%dms", exitCode, duration))
	if truncated {
		b.WriteString(" (truncated)")
	}
	if strings.TrimSpace(stdout) != "" {
		stdoutLimited, stdoutTruncated := limitOutputLines(stdout, maxBangOutputLines)
		b.WriteString("\nstdout:\n")
		b.WriteString(stdoutLimited)
		if stdoutTruncated {
			b.WriteString("\n...[output truncated for display]")
		}
	}
	if strings.TrimSpace(stderr) != "" {
		stderrLimited, stderrTruncated := limitOutputLines(stderr, maxBangOutputLines)
		b.WriteString("\nstderr:\n")
		b.WriteString(stderrLimited)
		if stderrTruncated {
			b.WriteString("\n...[error output truncated for display]")
		}
	}
	if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
		b.WriteString("\n(no output)")
	}
	return strings.TrimRight(b.String(), "\n")
}

func limitOutputLines(s string, max int) (string, bool) {
	if max <= 0 {
		return s, false
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) <= max {
		return s, false
	}
	limited := strings.Join(lines[:max], "\n")
	return limited, true
}
