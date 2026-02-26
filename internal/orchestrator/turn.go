package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"coder/internal/chat"
	"coder/internal/contextmgr"
	"coder/internal/permission"
	"coder/internal/tools"
)

func (o *Orchestrator) RunTurn(ctx context.Context, userInput string, out io.Writer) (string, error) {
	undoRecorder := newTurnUndoRecorder(o.workspaceRoot)
	defer o.commitTurnUndo(undoRecorder)

	o.appendMessage(chat.Message{Role: "user", Content: userInput})
	o.emitContextUpdate()
	o.refreshTodos(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var finalText string
	turnEditedCode := false
	editedPaths := make([]string, 0, 4)
	verifyAttempts := 0

	for step := 0; step < o.resolveMaxSteps(); step++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		o.maybeCompact()
		o.emitContextUpdate()

		if o.provider == nil {
			return "", fmt.Errorf("provider unavailable")
		}
		streamRenderer := newAnswerStreamRenderer(out)
		thinkingRenderer := newThinkingStreamRenderer(out)
		streamed := false
		streamedThinking := false
		var onTextChunk TextChunkFunc
		var onReasoningChunk TextChunkFunc
		if out != nil {
			onTextChunk = func(chunk string) {
				if chunk == "" {
					return
				}
				streamed = true
				streamRenderer.Append(chunk)
				if o.onTextChunk != nil {
					o.onTextChunk(chunk)
				}
			}
			onReasoningChunk = func(chunk string) {
				if chunk == "" {
					return
				}
				streamedThinking = true
				thinkingRenderer.Append(chunk)
			}
		} else if o.onTextChunk != nil {
			onTextChunk = o.onTextChunk
		}

		// 对于闲聊/简单问候，不提供工具定义，避免模型过度探索
		toolDefs := o.filterToolDefsByPolicy(o.registry.DefinitionsFiltered(o.activeAgent.ToolEnabled))
		if isChattyGreeting(userInput) && step == 0 {
			toolDefs = nil
		}
		resp, err := o.chatWithRetry(ctx, o.buildProviderMessages(), toolDefs, onTextChunk, onReasoningChunk)
		if err != nil {
			if streamed {
				streamRenderer.Finish()
			}
			if streamedThinking {
				thinkingRenderer.Finish()
			}
			if isContextCancellationErr(ctx, err) {
				return "", contextErrOr(ctx, err)
			}
			return "", fmt.Errorf("provider chat: %w", err)
		}
		if streamed {
			streamRenderer.Finish()
		}
		if streamedThinking {
			thinkingRenderer.Finish()
		}

		assistantMsg := chat.Message{Role: "assistant", Content: resp.Content, Reasoning: resp.Reasoning, ToolCalls: resp.ToolCalls}
		o.appendMessage(assistantMsg)
		_ = o.flushSessionToFile(ctx)

		if resp.Reasoning != "" && out != nil && !streamedThinking {
			renderThinkingBlock(out, resp.Reasoning)
		}
		if resp.Content != "" {
			finalText = resp.Content
			if out != nil && !streamed {
				renderAssistantBlock(out, resp.Content, len(resp.ToolCalls) == 0)
			}
		}

		if len(resp.ToolCalls) == 0 {
			if turnEditedCode && shouldAutoVerifyEditedPaths(editedPaths) && o.workflow.AutoVerifyAfterEdit && verifyAttempts < o.workflow.MaxVerifyAttempts && o.isToolAllowed("bash") && o.registry.Has("bash") {
				command := o.pickVerifyCommand()
				if command != "" {
					verifyAttempts++
					passed, retryable, err := o.runAutoVerify(ctx, command, verifyAttempts, out)
					if err == nil && !passed {
						if retryable && verifyAttempts < o.workflow.MaxVerifyAttempts {
							repairHint := fmt.Sprintf("Auto verification command `%s` failed. Please fix the issues, then continue and make verification pass.", command)
							o.appendMessage(chat.Message{Role: "user", Content: repairHint})
							continue
						}
						if !retryable {
							verifyWarn := fmt.Sprintf("Auto verification command `%s` failed due to environment/runtime issues. Continue with best-effort manual validation.", command)
							o.appendMessage(chat.Message{Role: "assistant", Content: verifyWarn})
							_ = o.flushSessionToFile(ctx)
						}
					}
					if err != nil {
						if isContextCancellationErr(ctx, err) {
							return "", contextErrOr(ctx, err)
						}
						verifyWarn := fmt.Sprintf("Auto verification could not complete (%v). Continue with best-effort manual validation.", err)
						o.appendMessage(chat.Message{Role: "assistant", Content: verifyWarn})
						_ = o.flushSessionToFile(ctx)
					}
				}
			}
			o.refreshTodos(ctx)
			return finalText, nil
		}

		for _, call := range resp.ToolCalls {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			startSummary := formatToolStart(call.Function.Name, call.Function.Arguments)
			if out != nil {
				renderToolStart(out, startSummary)
			}
			if o.onToolEvent != nil {
				o.onToolEvent(call.Function.Name, startSummary, false)
			}
			if !o.isToolAllowed(call.Function.Name) {
				reason := fmt.Sprintf("tool %s disabled by active agent %s", call.Function.Name, o.activeAgent.Name)
				if out != nil {
					renderToolBlocked(out, reason)
				}
				o.appendToolDenied(call, reason)
				continue
			}
			args := json.RawMessage(call.Function.Arguments)
			decision := permission.Result{Decision: permission.DecisionAllow}
			if o.policy != nil {
				decision = o.policy.Decide(call.Function.Name, args)
			}
			if decision.Decision == permission.DecisionDeny {
				reason := strings.TrimSpace(decision.Reason)
				if reason == "" {
					reason = "blocked by policy"
				}
				if out != nil {
					renderToolBlocked(out, summarizeForLog(reason))
				}
				o.appendToolDenied(call, reason)
				continue
			}

			approvalReq, err := o.registry.ApprovalRequest(call.Function.Name, args)
			if err != nil {
				if out != nil {
					renderToolError(out, summarizeForLog(err.Error()))
				}
				o.appendToolError(call, fmt.Errorf("approval check: %w", err))
				continue
			}
			needsApproval := decision.Decision == permission.DecisionAsk || approvalReq != nil
			if needsApproval {
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
					if out != nil {
						renderToolBlocked(out, "approval callback unavailable")
					}
					o.appendToolDenied(call, "approval callback unavailable")
					continue
				}
				allowed, err := o.onApproval(ctx, tools.ApprovalRequest{
					Tool:    call.Function.Name,
					Reason:  approvalReason,
					RawArgs: string(args),
				})
				if err != nil {
					if isContextCancellationErr(ctx, err) {
						return "", contextErrOr(ctx, err)
					}
					return "", fmt.Errorf("approval callback: %w", err)
				}
				if !allowed {
					if err := ctx.Err(); err != nil {
						return "", err
					}
					if out != nil {
						renderToolBlocked(out, summarizeForLog(approvalReason))
					}
					o.appendToolDenied(call, approvalReason)
					continue
				}
			}
			if call.Function.Name == "write" || call.Function.Name == "edit" || call.Function.Name == "patch" {
				undoRecorder.CaptureFromToolCall(call.Function.Name, args)
			}

			result, err := o.registry.Execute(ctx, call.Function.Name, args)
			if err != nil {
				if isContextCancellationErr(ctx, err) {
					return "", contextErrOr(ctx, err)
				}
				if out != nil {
					renderToolError(out, summarizeForLog(err.Error()))
				}
				o.appendToolError(call, err)
				continue
			}
			resultSummary := summarizeToolResult(call.Function.Name, result)
			if out != nil {
				renderToolResult(out, resultSummary)
			}
			if o.onToolEvent != nil {
				o.onToolEvent(call.Function.Name, resultSummary, true)
			}
			o.appendMessage(chat.Message{
				Role:       "tool",
				Name:       call.Function.Name,
				ToolCallID: call.ID,
				Content:    result,
			})
			if call.Function.Name == "todoread" || call.Function.Name == "todowrite" {
				if o.onTodoUpdate != nil {
					items := todoItemsFromResult(result)
					if items != nil {
						o.onTodoUpdate(items)
					}
				}
			}
			if call.Function.Name == "write" || call.Function.Name == "edit" || call.Function.Name == "patch" {
				turnEditedCode = true
				if editedPath := editedPathFromToolCall(call.Function.Name, args); editedPath != "" {
					editedPaths = append(editedPaths, editedPath)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return finalText, fmt.Errorf("step limit reached (%d)", o.resolveMaxSteps())
}

func joinApprovalReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "approval required"
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(reasons))
	for _, raw := range reasons {
		reason := strings.TrimSpace(raw)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	if len(out) == 0 {
		return "approval required"
	}
	return strings.Join(out, "; ")
}

func (o *Orchestrator) maybeCompact() {
	if !o.compaction.Auto {
		return
	}
	messages := o.buildProviderMessages()
	estimated := contextmgr.EstimateTokens(messages)
	threshold := int(float64(o.contextTokenLimit) * o.compaction.Threshold)
	if estimated <= threshold {
		return
	}
	compacted, summary, changed := contextmgr.CompactWithStrategy(
		context.Background(), o.messages, o.compaction.RecentMessages, o.compaction.Prune, o.compStrategy)
	if !changed {
		return
	}
	o.messages = compacted
	o.messageTimestamps = make([]string, len(o.messages))
	o.lastCompaction = summary
}

func (o *Orchestrator) buildProviderMessages() []chat.Message {
	out := []chat.Message{}
	if o.assembler != nil {
		out = append(out, o.assembler.StaticMessages()...)
	}
	if modeMsg := o.runtimeModeSystemMessage(); strings.TrimSpace(modeMsg.Content) != "" {
		out = append(out, modeMsg)
	}
	out = append(out, o.messages...)
	return out
}

func (o *Orchestrator) runtimeModeSystemMessage() chat.Message {
	switch o.CurrentMode() {
	case "plan":
		return chat.Message{
			Role: "system",
			Content: "[RUNTIME_MODE]\n" +
				"Current mode is PLAN.\n" +
				"- Prioritize read-only analysis and planning.\n" +
				"- You may read/analyze code, use fetch for web access, manage todos when helpful, and run read-only diagnostic bash commands when needed.\n" +
				"- Plans can be provided directly in natural-language responses; todos are optional planning aids.\n" +
				"- For environment/setup requests (install/uninstall/configure software), ask clarifying questions first and use minimal diagnostics only when necessary.\n" +
				"- Do NOT perform repository mutations yourself (no edit/write/patch/delete, no commit/stage operations, no subagent task delegation).\n" +
				"- If user asks for implementation, provide an actionable plan and required changes.",
		}
	default:
		return chat.Message{
			Role: "system",
			Content: "[RUNTIME_MODE]\n" +
				"Current mode is BUILD.\n" +
				"- Focus on implementing and validating changes.\n" +
				"- Do NOT create or update todos in this mode (todowrite is plan-only).",
		}
	}
}

func (o *Orchestrator) filterToolDefsByPolicy(defs []chat.ToolDef) []chat.ToolDef {
	if o.policy == nil || len(defs) == 0 {
		return defs
	}
	filtered := make([]chat.ToolDef, 0, len(defs))
	for _, def := range defs {
		name := strings.ToLower(strings.TrimSpace(def.Function.Name))
		if name == "" {
			continue
		}
		// bash permission depends on concrete command args; keep runtime checks.
		if name == "bash" {
			filtered = append(filtered, def)
			continue
		}
		if o.policy.Decide(name, nil).Decision == permission.DecisionDeny {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

func (o *Orchestrator) emitContextUpdate() {
	if o.onContextUpdate == nil {
		return
	}
	messages := o.buildProviderMessages()
	estimated := contextmgr.EstimateTokens(messages)
	limit := o.contextTokenLimit
	if limit <= 0 {
		limit = 24000
	}
	percent := 0.0
	if limit > 0 {
		percent = float64(estimated) / float64(limit) * 100
	}
	o.onContextUpdate(estimated, limit, percent)
}

// refreshTodos 从存储读取当前待办并推送给前端（TUI 侧栏 / REPL 可 no-op；回合开始/结束时调用）
// refreshTodos reads current todos from store and pushes to frontend (TUI sidebar or REPL no-op; called at turn start/end)
func (o *Orchestrator) refreshTodos(ctx context.Context) {
	if o.onTodoUpdate == nil || !o.registry.Has("todoread") {
		return
	}
	result, err := o.registry.Execute(ctx, "todoread", json.RawMessage(`{}`))
	if err != nil {
		return
	}
	items := todoItemsFromResult(result)
	o.onTodoUpdate(items)
}
