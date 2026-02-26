package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"coder/internal/chat"
	"coder/internal/permission"
	"coder/internal/tools"
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

	decision := permission.Result{Decision: permission.DecisionAllow}
	if o.policy != nil {
		decision = o.policy.Decide("bash", rawArgs)
	}
	if decision.Decision == permission.DecisionDeny {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "blocked by policy"
		}
		msg := "command mode denied: " + reason
		o.appendMessage(chat.Message{Role: "assistant", Content: msg})
		_ = o.flushSessionToFile(ctx)
		if out != nil {
			renderToolBlocked(out, summarizeForLog(msg))
		}
		return msg, nil
	}

	approvalReq, approvalErr := o.registry.ApprovalRequest("bash", rawArgs)
	if approvalErr != nil {
		msg := "command mode denied: approval check failed: " + approvalErr.Error()
		o.appendMessage(chat.Message{Role: "assistant", Content: msg})
		_ = o.flushSessionToFile(ctx)
		if out != nil {
			renderToolError(out, summarizeForLog(msg))
		}
		return msg, nil
	}
	if decision.Decision == permission.DecisionAsk || approvalReq != nil {
		reasons := make([]string, 0, 2)
		if decision.Decision == permission.DecisionAsk {
			if r := strings.TrimSpace(decision.Reason); r != "" {
				reasons = append(reasons, r)
			}
		}
		if approvalReq != nil {
			if r := strings.TrimSpace(approvalReq.Reason); r != "" {
				reasons = append(reasons, r)
			}
		}
		approvalReason := joinApprovalReasons(reasons)
		if o.onApproval == nil {
			msg := "command mode denied: approval callback unavailable"
			o.appendMessage(chat.Message{Role: "assistant", Content: msg})
			_ = o.flushSessionToFile(ctx)
			if out != nil {
				renderToolBlocked(out, summarizeForLog(msg))
			}
			return msg, nil
		}
		allowed, err := o.onApproval(ctx, tools.ApprovalRequest{
			Tool:    "bash",
			Reason:  approvalReason,
			RawArgs: string(rawArgs),
		})
		if err != nil {
			return "", fmt.Errorf("command mode approval callback: %w", err)
		}
		if !allowed {
			msg := "command mode denied: " + approvalReason
			o.appendMessage(chat.Message{Role: "assistant", Content: msg})
			_ = o.flushSessionToFile(ctx)
			if out != nil {
				renderToolBlocked(out, summarizeForLog(msg))
			}
			return msg, nil
		}
	}

	if out != nil {
		renderToolStart(out, formatToolStart("bash", args))
	}

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
