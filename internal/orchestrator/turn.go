package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"coder/internal/chat"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/permission"
)

func (o *Orchestrator) RunTurn(ctx context.Context, userInput string, out io.Writer) (string, error) {
	undoRecorder := newTurnUndoRecorder(o.workspaceRoot)
	defer o.commitTurnUndo(undoRecorder)

	baseToolDefs := o.resolveToolDefsForInput(userInput)
	o.turnToolDefs = append([]chat.ToolDef(nil), baseToolDefs...)

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

		toolDefs := append([]chat.ToolDef(nil), baseToolDefs...)
		// 对于闲聊/简单问候，不提供工具定义，避免模型过度探索
		if isChattyGreeting(userInput) && step == 0 {
			toolDefs = nil
		}
		resp, err := o.chatWithRetry(ctx, o.buildProviderMessages(toolDefs), toolDefs, onTextChunk, onReasoningChunk)
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
			needsNextStep, err := o.handleNoToolCalls(ctx, out, turnEditedCode, editedPaths, &verifyAttempts)
			if err != nil {
				return "", err
			}
			if needsNextStep {
				continue
			}
			return finalText, nil
		}

		if err := o.executeToolCalls(ctx, out, undoRecorder, resp.ToolCalls, &turnEditedCode, &editedPaths); err != nil {
			return "", err
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
	messages := o.buildProviderMessages(o.currentToolDefs())
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

func (o *Orchestrator) buildProviderMessages(toolDefs []chat.ToolDef) []chat.Message {
	out := []chat.Message{}
	if o.assembler != nil {
		out = append(out, o.assembler.StaticMessages()...)
	}
	if modeMsg := o.runtimeModeSystemMessage(); strings.TrimSpace(modeMsg.Content) != "" {
		out = append(out, modeMsg)
	}
	if toolMsg := o.runtimeToolsSystemMessage(toolDefs); strings.TrimSpace(toolMsg.Content) != "" {
		out = append(out, toolMsg)
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
				"- Use only the tools exposed in [RUNTIME_TOOLS].\n" +
				"- For environment/setup requests, ask for missing details first and use minimal diagnostics.\n" +
				"- If the user asks to create, modify, delete, install, or run a mutating command, do NOT execute it in PLAN mode. Respond with a concise plan or next steps instead.\n" +
				"- If bash is exposed in this turn, treat it as read-only diagnostics only (for example: pwd, ls, cat, grep, git status, git diff). Do not use bash for mutations in PLAN mode.\n" +
				"- Do NOT perform repository mutations yourself unless a writable tool is explicitly exposed in this turn.",
		}
	default:
		return chat.Message{
			Role: "system",
			Content: "[RUNTIME_MODE]\n" +
				"Current mode is BUILD.\n" +
				"- Focus on implementing and validating changes.\n" +
				"- Use only the tools exposed in [RUNTIME_TOOLS].\n" +
				"- Prefer small, validated changes over broad refactors.",
		}
	}
}

func (o *Orchestrator) runtimeToolsSystemMessage(toolDefs []chat.ToolDef) chat.Message {
	if len(toolDefs) == 0 {
		return chat.Message{}
	}

	names := make([]string, 0, len(toolDefs))
	has := map[string]bool{}
	for _, def := range toolDefs {
		name := strings.TrimSpace(def.Function.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
		has[name] = true
	}
	if len(names) == 0 {
		return chat.Message{}
	}

	lines := []string{
		"[RUNTIME_TOOLS]",
		"Only the following tools are exposed in this turn: " + strings.Join(names, ", "),
		"- Prefer read/list/grep for focused discovery. Avoid broad repository scans unless necessary.",
	}

	if has["edit"] {
		lines = append(lines, "- Prefer edit for localized changes. Use write mainly for new files or full replacements.")
	}
	if has["patch"] {
		lines = append(lines, "- Use patch only when a localized edit cannot be expressed safely with edit/write. Keep hunks minimal.")
	}
	if has["fetch"] {
		lines = append(lines, "- fetch may be used for HTTP/HTTPS access to internal services and documentation reachable from this environment.")
	}
	if has["todowrite"] {
		lines = append(lines, "- Todos are optional. Use them only for genuinely multi-step work, and keep at most one item in_progress.")
	} else {
		lines = append(lines, "- Do not create or update todos unless todowrite is exposed in this turn.")
	}
	if has["question"] {
		lines = append(lines, "- Use question only when a missing user preference would materially affect the plan.")
	}

	return chat.Message{
		Role:    "system",
		Content: strings.Join(lines, "\n"),
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
	messages := o.buildProviderMessages(o.currentToolDefs())
	estimated := contextmgr.EstimateTokens(messages)
	limit := o.contextTokenLimit
	if limit <= 0 {
		limit = config.DefaultRuntimeContextTokenLimit
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
