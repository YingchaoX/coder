package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

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
	requireTodoFirst := o.workflow.RequireTodoForComplex &&
		o.registry.Has("todoread") &&
		o.registry.Has("todowrite") &&
		o.isToolAllowed("todoread") &&
		o.isToolAllowed("todowrite") &&
		isComplexTask(userInput)

	if requireTodoFirst {
		o.ensureSessionTodos(ctx, userInput, out)
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}

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
		toolDefs := o.registry.DefinitionsFiltered(o.activeAgent.ToolEnabled)
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
				"- You may read/analyze code, use fetch for web access, manage todos, and run read-only diagnostic bash commands (for example: uname, pwd, id).\n" +
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

func (o *Orchestrator) hasSessionTodos(ctx context.Context) (bool, error) {
	result, err := o.registry.Execute(ctx, "todoread", json.RawMessage(`{}`))
	if err != nil {
		return false, err
	}
	payload := parseJSONObject(result)
	if payload == nil {
		return false, nil
	}
	if getInt(payload, "count", 0) <= 0 {
		return false, nil
	}
	items := getArray(payload, "items")
	if len(items) == 0 {
		return false, nil
	}
	hasNonCompleted := false
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(getString(item, "status", "")))
		if status != "completed" {
			hasNonCompleted = true
			break
		}
	}
	// 返回值语义：
	// - true  表示“当前会话仍有未完成的 todo”，后续输入应倾向继续沿用这组 todo。
	// - false 表示“todo 为空或全部 completed”，新复杂任务可以触发自动初始化新的 todo 列表。
	return hasNonCompleted, nil
}

func (o *Orchestrator) ensureSessionTodos(ctx context.Context, userInput string, out io.Writer) {
	hasTodos, err := o.hasSessionTodos(ctx)
	if err != nil || hasTodos {
		return
	}
	args := mustJSON(map[string]any{
		"todos": defaultTodoItems(userInput),
	})
	if out != nil {
		renderToolStart(out, "* Auto init todo list")
	}
	result, err := o.registry.Execute(ctx, "todowrite", json.RawMessage(args))
	callID := fmt.Sprintf("auto_todo_init_%d", time.Now().UnixNano())
	if err != nil {
		if out != nil {
			renderToolError(out, summarizeForLog(err.Error()))
		}
		return
	}
	if out != nil {
		renderToolResult(out, summarizeToolResult("todowrite", result))
	}
	o.appendSyntheticToolExchange("todowrite", args, result, callID)
	if o.onTodoUpdate != nil {
		items := todoItemsFromResult(result)
		if items != nil {
			o.onTodoUpdate(items)
		}
	}
}

func defaultTodoItems(userInput string) []map[string]any {
	objective := short(strings.TrimSpace(userInput), 80)
	if objective == "" {
		objective = "Handle request"
	}
	// If the user already provided explicit steps (e.g. "1. ... 2. ..."),
	// generate step-based todos instead of a generic "clarify" item.
	// 如果用户已经给出明确步骤（例如 "1. ... 2. ..."），则直接按步骤生成待办，避免泛化的“澄清需求”。
	if steps := parseNumberedSteps(userInput); len(steps) >= 2 {
		items := make([]map[string]any, 0, len(steps)+1)
		for i, s := range steps {
			priority := "high"
			if i >= 2 {
				priority = "medium"
			}
			status := "pending"
			if i == 0 {
				status = "in_progress"
			}
			items = append(items, map[string]any{
				"content":  s,
				"status":   status,
				"priority": priority,
			})
		}
		// Add a lightweight wrap-up step if user didn't mention it.
		// 如果用户没提到“总结/验证”，补一条轻量收尾步骤。
		if !containsAnyFold(userInput, []string{"验证", "测试", "总结", "review", "verify", "test", "summary"}) {
			items = append(items, map[string]any{
				"content":  "执行验证并总结变更",
				"status":   "pending",
				"priority": "low",
			})
		}
		return items
	}
	if containsHan(userInput) {
		return []map[string]any{
			{"content": "阅读代码并确认目标/验收标准", "status": "in_progress", "priority": "high"},
			{"content": "实施修改: " + objective, "status": "pending", "priority": "high"},
			{"content": "执行验证并总结变更", "status": "pending", "priority": "medium"},
		}
	}
	return []map[string]any{
		{"content": "Clarify scope and acceptance criteria", "status": "in_progress", "priority": "high"},
		{"content": "Implement changes: " + objective, "status": "pending", "priority": "high"},
		{"content": "Run validation and summarize results", "status": "pending", "priority": "medium"},
	}
}

func parseNumberedSteps(input string) []string {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil
	}
	// Match common numbered step prefixes: "1.", "1、", "1)", "(1)" (also works mid-string after punctuation/space/newline).
	// 匹配常见编号前缀："1." "1、" "1)" "(1)"（也支持出现在句中、逗号后、换行后）。
	re := regexp.MustCompile(`(?m)(?:^|[，,;；\n]\s*)\(?\s*\d{1,2}\s*[\.、\)]\s*`)
	indices := re.FindAllStringIndex(s, -1)
	if len(indices) < 2 {
		return nil
	}
	steps := make([]string, 0, len(indices))
	for i, idx := range indices {
		start := idx[1]
		end := len(s)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		part := strings.TrimSpace(s[start:end])
		part = strings.Trim(part, "，,;；")
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		steps = append(steps, part)
	}
	if len(steps) < 2 {
		return nil
	}
	return steps
}

func containsAnyFold(s string, needles []string) bool {
	if strings.TrimSpace(s) == "" || len(needles) == 0 {
		return false
	}
	lower := strings.ToLower(s)
	for _, n := range needles {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
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
