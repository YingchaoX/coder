package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"coder/internal/agent"
	"coder/internal/chat"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/permission"
	"coder/internal/provider"
	"coder/internal/tools"
)

type ApprovalFunc func(ctx context.Context, req tools.ApprovalRequest) (bool, error)

const (
	ansiReset  = "\x1b[0m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiGray   = "\x1b[90m"
	ansiBold   = "\x1b[1m"
)

type Options struct {
	MaxSteps          int
	SystemPrompt      string
	OnApproval        ApprovalFunc
	Policy            *permission.Policy
	Assembler         *contextmgr.Assembler
	Compaction        config.CompactionConfig
	ContextTokenLimit int
	ActiveAgent       agent.Profile
	Agents            config.AgentConfig
	Workflow          config.WorkflowConfig
	WorkspaceRoot     string
}

type ContextStats struct {
	EstimatedTokens int
	ContextLimit    int
	UsagePercent    float64
	MessageCount    int
}

type Orchestrator struct {
	provider          *provider.Client
	registry          *tools.Registry
	maxSteps          int
	onApproval        ApprovalFunc
	messages          []chat.Message
	policy            *permission.Policy
	assembler         *contextmgr.Assembler
	compaction        config.CompactionConfig
	contextTokenLimit int
	activeAgent       agent.Profile
	agents            config.AgentConfig
	lastCompaction    string
	workflow          config.WorkflowConfig
	workspaceRoot     string
}

func New(providerClient *provider.Client, registry *tools.Registry, opts Options) *Orchestrator {
	maxSteps := opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 128
	}
	contextLimit := opts.ContextTokenLimit
	if contextLimit <= 0 {
		contextLimit = 24000
	}
	if opts.Compaction.Threshold <= 0 || opts.Compaction.Threshold >= 1 {
		opts.Compaction.Threshold = 0.8
	}
	if opts.Compaction.RecentMessages <= 0 {
		opts.Compaction.RecentMessages = 12
	}
	if opts.Workflow.MaxVerifyAttempts <= 0 {
		opts.Workflow.MaxVerifyAttempts = 2
	}

	activeAgent := opts.ActiveAgent
	if activeAgent.Name == "" {
		activeAgent = agent.Resolve("build", opts.Agents)
	}
	o := &Orchestrator{
		provider:          providerClient,
		registry:          registry,
		maxSteps:          maxSteps,
		onApproval:        opts.OnApproval,
		policy:            opts.Policy,
		assembler:         opts.Assembler,
		compaction:        opts.Compaction,
		contextTokenLimit: contextLimit,
		activeAgent:       activeAgent,
		agents:            opts.Agents,
		workflow:          opts.Workflow,
		workspaceRoot:     strings.TrimSpace(opts.WorkspaceRoot),
	}
	o.Reset()
	return o
}

func (o *Orchestrator) Reset() {
	o.messages = o.messages[:0]
	o.lastCompaction = ""
}

func (o *Orchestrator) Messages() []chat.Message {
	return append([]chat.Message(nil), o.messages...)
}

func (o *Orchestrator) LoadMessages(messages []chat.Message) {
	o.messages = append([]chat.Message(nil), messages...)
}

func (o *Orchestrator) SetActiveAgent(profile agent.Profile) {
	if profile.Name == "" {
		return
	}
	o.activeAgent = profile
}

func (o *Orchestrator) ActiveAgent() agent.Profile {
	return o.activeAgent
}

func (o *Orchestrator) LastCompactionSummary() string {
	return o.lastCompaction
}

func (o *Orchestrator) CurrentContextStats() ContextStats {
	messages := o.buildProviderMessages()
	estimated := contextmgr.EstimateTokens(messages)
	limit := o.contextTokenLimit
	percent := 0.0
	if limit > 0 {
		percent = (float64(estimated) / float64(limit)) * 100
	}
	return ContextStats{
		EstimatedTokens: estimated,
		ContextLimit:    limit,
		UsagePercent:    percent,
		MessageCount:    len(messages),
	}
}

func (o *Orchestrator) CurrentModel() string {
	if o.provider == nil {
		return ""
	}
	return o.provider.Model()
}

func (o *Orchestrator) SetModel(model string) error {
	if o.provider == nil {
		return fmt.Errorf("provider unavailable")
	}
	return o.provider.SetModel(model)
}

func (o *Orchestrator) CompactNow() bool {
	compacted, summary, changed := contextmgr.Compact(o.messages, o.compaction.RecentMessages, o.compaction.Prune)
	if !changed {
		return false
	}
	o.messages = compacted
	o.lastCompaction = summary
	return true
}

func (o *Orchestrator) RunInput(ctx context.Context, input string, out io.Writer) (string, error) {
	command, ok := parseBangCommand(input)
	if !ok {
		return o.RunTurn(ctx, input, out)
	}
	return o.runBangCommand(ctx, input, command, out)
}

func (o *Orchestrator) RunTurn(ctx context.Context, userInput string, out io.Writer) (string, error) {
	o.messages = append(o.messages, chat.Message{Role: "user", Content: userInput})

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
	}

	for step := 0; step < o.resolveMaxSteps(); step++ {
		o.maybeCompact()

		if o.provider == nil {
			return "", fmt.Errorf("provider unavailable")
		}
		streamRenderer := newAnswerStreamRenderer(out)
		streamed := false
		onTextChunk := provider.TextChunkFunc(nil)
		if out != nil {
			onTextChunk = func(chunk string) {
				if chunk == "" {
					return
				}
				streamed = true
				streamRenderer.Append(chunk)
			}
		}

		resp, err := o.chatWithRetry(ctx, o.buildProviderMessages(), o.registry.DefinitionsFiltered(o.activeAgent.ToolEnabled), onTextChunk)
		if err != nil {
			return "", fmt.Errorf("provider chat: %w", err)
		}
		if streamed {
			streamRenderer.Finish()
		}

		assistantMsg := chat.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls}
		o.messages = append(o.messages, assistantMsg)

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
							o.messages = append(o.messages, chat.Message{Role: "user", Content: repairHint})
							continue
						}
						if !retryable {
							verifyWarn := fmt.Sprintf("Auto verification command `%s` failed due to environment/runtime issues. Continue with best-effort manual validation.", command)
							o.messages = append(o.messages, chat.Message{Role: "assistant", Content: verifyWarn})
						}
					}
					if err != nil {
						verifyWarn := fmt.Sprintf("Auto verification could not complete (%v). Continue with best-effort manual validation.", err)
						o.messages = append(o.messages, chat.Message{Role: "assistant", Content: verifyWarn})
					}
				}
			}
			return finalText, nil
		}

		for _, call := range resp.ToolCalls {
			if out != nil {
				renderToolStart(out, formatToolStart(call.Function.Name, call.Function.Arguments))
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
			if denied := o.handlePolicyCheck(ctx, call, args, out); denied {
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
			if approvalReq != nil {
				if o.onApproval == nil {
					if out != nil {
						renderToolBlocked(out, "approval callback unavailable")
					}
					o.appendToolDenied(call, "approval callback unavailable")
					continue
				}
				allowed, err := o.onApproval(ctx, *approvalReq)
				if err != nil {
					return "", fmt.Errorf("approval callback: %w", err)
				}
				if !allowed {
					if out != nil {
						renderToolBlocked(out, summarizeForLog(approvalReq.Reason))
					}
					o.appendToolDenied(call, approvalReq.Reason)
					continue
				}
			}

			result, err := o.registry.Execute(ctx, call.Function.Name, args)
			if err != nil {
				if out != nil {
					renderToolError(out, summarizeForLog(err.Error()))
				}
				o.appendToolError(call, err)
				continue
			}
			if out != nil {
				renderToolResult(out, summarizeToolResult(call.Function.Name, result))
			}
			o.messages = append(o.messages, chat.Message{
				Role:       "tool",
				Name:       call.Function.Name,
				ToolCallID: call.ID,
				Content:    result,
			})
			if call.Function.Name == "write" || call.Function.Name == "patch" {
				turnEditedCode = true
				if editedPath := editedPathFromToolCall(call.Function.Name, args); editedPath != "" {
					editedPaths = append(editedPaths, editedPath)
				}
			}
		}
	}
	return finalText, fmt.Errorf("step limit reached (%d)", o.resolveMaxSteps())
}

func (o *Orchestrator) RunSubtask(ctx context.Context, subagentName, objective string) (string, error) {
	profile, ok := agent.ResolveSubagent(subagentName, o.agents)
	if !ok {
		return "", fmt.Errorf("subagent not allowed: %s", subagentName)
	}
	if profile.ToolEnabled == nil {
		profile.ToolEnabled = map[string]bool{}
	}
	profile.ToolEnabled["task"] = false
	profile.ToolEnabled["todoread"] = false
	profile.ToolEnabled["todowrite"] = false

	child := New(o.provider, o.registry, Options{
		MaxSteps:          o.resolveMaxSteps(),
		OnApproval:        o.onApproval,
		Policy:            o.policy,
		Assembler:         o.assembler,
		Compaction:        o.compaction,
		ContextTokenLimit: o.contextTokenLimit,
		ActiveAgent:       profile,
		Agents:            o.agents,
		Workflow:          o.workflow,
		WorkspaceRoot:     o.workspaceRoot,
	})
	summaryPrompt := fmt.Sprintf("Subtask objective: %s\nReturn concise findings and recommended next step.", strings.TrimSpace(objective))
	result, err := child.RunTurn(ctx, summaryPrompt, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result) == "" {
		return "subtask finished with no text output", nil
	}
	return result, nil
}

func (o *Orchestrator) runBangCommand(ctx context.Context, rawInput, command string, out io.Writer) (string, error) {
	o.messages = append(o.messages, chat.Message{Role: "user", Content: rawInput})

	if strings.TrimSpace(command) == "" {
		msg := "command mode error: empty command after '!'."
		o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
		if out != nil {
			renderToolError(out, msg)
		}
		return msg, nil
	}
	if !o.isToolAllowed("bash") {
		msg := fmt.Sprintf("command mode denied: bash disabled by active agent %s", o.activeAgent.Name)
		o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
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
	if o.policy != nil {
		policyDecision := o.policy.Decide("bash", rawArgs)
		if policyDecision.Decision == permission.DecisionDeny {
			msg := "command mode denied: " + policyDecision.Reason
			o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
			if out != nil {
				renderToolBlocked(out, summarizeForLog(policyDecision.Reason))
			}
			return msg, nil
		}
		if policyDecision.Decision == permission.DecisionAsk {
			if o.onApproval == nil {
				msg := "command mode denied: approval callback unavailable"
				o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
				if out != nil {
					renderToolBlocked(out, "approval callback unavailable")
				}
				return msg, nil
			}
			allowed, err := o.onApproval(ctx, tools.ApprovalRequest{Tool: "bash", Reason: policyDecision.Reason, RawArgs: args})
			if err != nil {
				return "", fmt.Errorf("approval callback: %w", err)
			}
			if !allowed {
				msg := "command mode denied: " + policyDecision.Reason
				o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
				if out != nil {
					renderToolBlocked(out, summarizeForLog(policyDecision.Reason))
				}
				return msg, nil
			}
		}
	}

	approvalReq, err := o.registry.ApprovalRequest("bash", rawArgs)
	if err != nil {
		return "", fmt.Errorf("approval check: %w", err)
	}
	if approvalReq != nil {
		if o.onApproval == nil {
			msg := "command mode denied: approval callback unavailable"
			o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
			if out != nil {
				renderToolBlocked(out, "approval callback unavailable")
			}
			return msg, nil
		}
		allowed, err := o.onApproval(ctx, *approvalReq)
		if err != nil {
			return "", fmt.Errorf("approval callback: %w", err)
		}
		if !allowed {
			msg := "command mode denied: " + approvalReq.Reason
			o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
			if out != nil {
				renderToolBlocked(out, summarizeForLog(approvalReq.Reason))
			}
			return msg, nil
		}
	}

	result, err := o.registry.Execute(ctx, "bash", rawArgs)
	if err != nil {
		return "", fmt.Errorf("execute command mode: %w", err)
	}
	if out != nil {
		renderToolResult(out, summarizeToolResult("bash", result))
	}

	msg := formatBangCommandResult(command, result)
	o.messages = append(o.messages, chat.Message{Role: "assistant", Content: msg})
	if out != nil {
		renderAssistantBlock(out, msg, true)
	}
	return msg, nil
}

func (o *Orchestrator) resolveMaxSteps() int {
	if o.activeAgent.MaxSteps > 0 {
		return o.activeAgent.MaxSteps
	}
	if o.maxSteps <= 0 {
		return 128
	}
	return o.maxSteps
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
	compacted, summary, changed := contextmgr.Compact(o.messages, o.compaction.RecentMessages, o.compaction.Prune)
	if !changed {
		return
	}
	o.messages = compacted
	o.lastCompaction = summary
}

func (o *Orchestrator) buildProviderMessages() []chat.Message {
	out := []chat.Message{}
	if o.assembler != nil {
		out = append(out, o.assembler.StaticMessages()...)
	}
	out = append(out, o.messages...)
	return out
}

func (o *Orchestrator) handlePolicyCheck(ctx context.Context, call chat.ToolCall, args json.RawMessage, out io.Writer) bool {
	if o.policy == nil {
		return false
	}
	decision := o.policy.Decide(call.Function.Name, args)
	switch decision.Decision {
	case permission.DecisionAllow:
		return false
	case permission.DecisionDeny:
		if out != nil {
			renderToolBlocked(out, summarizeForLog(decision.Reason))
		}
		o.appendToolDenied(call, decision.Reason)
		return true
	case permission.DecisionAsk:
		if o.onApproval == nil {
			reason := "approval callback unavailable"
			if out != nil {
				renderToolBlocked(out, reason)
			}
			o.appendToolDenied(call, reason)
			return true
		}
		allowed, err := o.onApproval(ctx, tools.ApprovalRequest{Tool: call.Function.Name, Reason: decision.Reason, RawArgs: string(args)})
		if err != nil {
			o.appendToolError(call, fmt.Errorf("approval callback: %w", err))
			if out != nil {
				renderToolError(out, summarizeForLog(err.Error()))
			}
			return true
		}
		if !allowed {
			if out != nil {
				renderToolBlocked(out, summarizeForLog(decision.Reason))
			}
			o.appendToolDenied(call, decision.Reason)
			return true
		}
		return false
	default:
		return false
	}
}

func (o *Orchestrator) isToolAllowed(tool string) bool {
	if o.activeAgent.ToolEnabled == nil {
		return true
	}
	enabled, ok := o.activeAgent.ToolEnabled[tool]
	if !ok {
		return true
	}
	return enabled
}

func (o *Orchestrator) chatWithRetry(
	ctx context.Context,
	messages []chat.Message,
	definitions []chat.ToolDef,
	onTextChunk provider.TextChunkFunc,
) (provider.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := o.provider.Chat(ctx, messages, definitions, onTextChunk)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == 2 {
			break
		}
		backoff := time.Duration(150*(attempt+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return provider.Response{}, lastErr
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
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(getString(item, "status", "")))
		if status != "completed" {
			return true, nil
		}
	}
	return true, nil
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
}

func defaultTodoItems(userInput string) []map[string]any {
	objective := short(strings.TrimSpace(userInput), 80)
	if objective == "" {
		objective = "Handle request"
	}
	if containsHan(userInput) {
		return []map[string]any{
			{"content": "澄清需求与验收标准", "status": "in_progress", "priority": "high"},
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
	if strings.TrimSpace(tool) != "write" {
		return ""
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Path)
}

func shouldAutoVerifyEditedPaths(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, path := range paths {
		if !isDocLikePath(path) {
			return true
		}
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

func (o *Orchestrator) appendSyntheticToolExchange(toolName, args, result, callID string) {
	if strings.TrimSpace(toolName) == "" || strings.TrimSpace(callID) == "" {
		return
	}
	o.messages = append(o.messages, chat.Message{
		Role: "assistant",
		ToolCalls: []chat.ToolCall{
			{
				ID:   callID,
				Type: "function",
				Function: chat.ToolCallFunction{
					Name:      toolName,
					Arguments: args,
				},
			},
		},
	})
	o.messages = append(o.messages, chat.Message{
		Role:       "tool",
		Name:       toolName,
		ToolCallID: callID,
		Content:    result,
	})
}

func (o *Orchestrator) appendToolDenied(call chat.ToolCall, reason string) {
	o.messages = append(o.messages, chat.Message{
		Role:       "tool",
		Name:       call.Function.Name,
		ToolCallID: call.ID,
		Content: mustJSON(map[string]any{
			"ok":     false,
			"denied": true,
			"reason": reason,
		}),
	})
}

func (o *Orchestrator) appendToolError(call chat.ToolCall, err error) {
	o.messages = append(o.messages, chat.Message{
		Role:       "tool",
		Name:       call.Function.Name,
		ToolCallID: call.ID,
		Content: mustJSON(map[string]any{
			"ok":    false,
			"error": err.Error(),
		}),
	})
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"marshal tool result failed"}`
	}
	return string(data)
}

func summarizeForLog(s string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if normalized == "" {
		return "-"
	}

	const maxRunes = 220
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return string(runes[:maxRunes]) + "...(truncated)"
}

func formatToolStart(name string, rawArgs string) string {
	args := parseJSONObject(rawArgs)
	switch name {
	case "read":
		path := getString(args, "path", "")
		return fmt.Sprintf("* Read %s", quoteOrDash(path))
	case "list":
		path := getString(args, "path", ".")
		return fmt.Sprintf("* List %s", quoteOrDash(path))
	case "glob":
		pattern := getString(args, "pattern", "")
		return fmt.Sprintf("* Glob %s", quoteOrDash(pattern))
	case "grep":
		pattern := getString(args, "pattern", "")
		path := getString(args, "path", ".")
		return fmt.Sprintf("* Grep %s in %s", quoteOrDash(pattern), quoteOrDash(path))
	case "write":
		path := getString(args, "path", "")
		content := getString(args, "content", "")
		return fmt.Sprintf("* Write %s (%d bytes)", quoteOrDash(path), len(content))
	case "patch":
		return "* Apply patch"
	case "todoread":
		return "* Read todo list"
	case "todowrite":
		return "* Update todo list"
	case "skill":
		action := getString(args, "action", "")
		name := getString(args, "name", "")
		if name == "" {
			return fmt.Sprintf("* Skill %s", quoteOrDash(action))
		}
		return fmt.Sprintf("* Skill %s %s", quoteOrDash(action), quoteOrDash(name))
	case "task":
		agentName := getString(args, "agent", "")
		objective := getString(args, "objective", "")
		if objective == "" {
			objective = getString(args, "prompt", "")
		}
		return fmt.Sprintf("* Task %s: %s", quoteOrDash(agentName), quoteOrDash(short(objective, 80)))
	case "bash":
		cmd := getString(args, "command", "")
		return fmt.Sprintf("* Bash %s", quoteOrDash(cmd))
	default:
		return fmt.Sprintf("* %s args=%s", title(name), summarizeForLog(rawArgs))
	}
}

func summarizeToolResult(name string, rawResult string) string {
	result := parseJSONObject(rawResult)
	if len(result) == 0 {
		return summarizeForLog(rawResult)
	}

	switch name {
	case "read":
		path := getString(result, "path", "")
		content := getString(result, "content", "")
		return fmt.Sprintf("read %d bytes from %s", len(content), quoteOrDash(path))
	case "list":
		path := getString(result, "path", "")
		return fmt.Sprintf("%d entries in %s", len(getArray(result, "items")), quoteOrDash(path))
	case "glob":
		return fmt.Sprintf("%d matches", len(getArray(result, "matches")))
	case "grep":
		count := getInt(result, "count", len(getArray(result, "matches")))
		return fmt.Sprintf("%d matches", count)
	case "write":
		path := getString(result, "path", "")
		size := getInt(result, "size", 0)
		operation := strings.ToLower(strings.TrimSpace(getString(result, "operation", "")))
		additions := getInt(result, "additions", 0)
		deletions := getInt(result, "deletions", 0)
		diff := strings.TrimSpace(getString(result, "diff", ""))
		line := fmt.Sprintf("wrote %d bytes to %s", size, quoteOrDash(path))
		switch operation {
		case "created":
			line = fmt.Sprintf("created %s (+%d lines, %d bytes)", quoteOrDash(path), additions, size)
		case "updated":
			line = fmt.Sprintf("updated %s (+%d -%d lines, %d bytes)", quoteOrDash(path), additions, deletions, size)
		case "unchanged":
			line = fmt.Sprintf("no-op write to %s (%d bytes)", quoteOrDash(path), size)
		}
		if diff != "" {
			return line + "\n" + diff
		}
		return line
	case "patch":
		return fmt.Sprintf("patched %d file(s)", getInt(result, "applied", 0))
	case "todoread":
		return formatTodoSummary(result, "todo")
	case "todowrite":
		return formatTodoSummary(result, "todo updated")
	case "skill":
		if content := getString(result, "content", ""); content != "" {
			return fmt.Sprintf("loaded skill (%d bytes)", len(content))
		}
		return fmt.Sprintf("%d skills", getInt(result, "count", len(getArray(result, "items"))))
	case "task":
		return summarizeForLog(getString(result, "summary", "task finished"))
	case "bash":
		exitCode := getInt(result, "exit_code", -1)
		duration := getInt(result, "duration_ms", 0)
		stdout := strings.TrimSpace(getString(result, "stdout", ""))
		stderr := strings.TrimSpace(getString(result, "stderr", ""))
		if exitCode == 0 {
			if stdout != "" {
				return fmt.Sprintf("exit=0 in %dms, stdout=%s", duration, summarizeForLog(firstLine(stdout)))
			}
			return fmt.Sprintf("exit=0 in %dms", duration)
		}
		if stderr != "" {
			return fmt.Sprintf("exit=%d in %dms, stderr=%s", exitCode, duration, summarizeForLog(firstLine(stderr)))
		}
		return fmt.Sprintf("exit=%d in %dms", exitCode, duration)
	default:
		if errText := getString(result, "error", ""); errText != "" {
			return summarizeForLog(errText)
		}
		return summarizeForLog(rawResult)
	}
}

func formatTodoSummary(result map[string]any, label string) string {
	items := getArray(result, "items")
	headline := fmt.Sprintf("%s items=%d", label, getInt(result, "count", len(items)))
	if len(items) == 0 {
		return headline
	}
	lines := []string{headline}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content := strings.TrimSpace(getString(item, "content", ""))
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", todoStatusMarker(getString(item, "status", "")), content))
	}
	if len(lines) == 1 {
		return headline
	}
	return strings.Join(lines, "\n")
}

func parseJSONObject(s string) map[string]any {
	var out map[string]any
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func getString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return fallback
		}
		return val
	default:
		return fallback
	}
}

func getArray(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	out, ok := v.([]any)
	if !ok {
		return nil
	}
	return out
}

func getInt(m map[string]any, key string, fallback int) int {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case string:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

func firstLine(s string) string {
	parts := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return ""
}

func quoteOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strconv.Quote(summarizeForLog(s))
}

func title(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Tool"
	}
	runes := []rune(s)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

type answerStreamRenderer struct {
	out             io.Writer
	started         bool
	lineStart       bool
	pendingNewlines int
	hasVisibleText  bool
}

func newAnswerStreamRenderer(out io.Writer) *answerStreamRenderer {
	return &answerStreamRenderer{out: out, lineStart: true}
}

func (r *answerStreamRenderer) start() {
	if r == nil || r.out == nil || r.started {
		return
	}
	r.started = true
	_, _ = fmt.Fprintln(r.out)
	_, _ = fmt.Fprintf(r.out, "%s %s\n", style("[ANSWER]", ansiCyan+";"+ansiBold), style(strings.Repeat("─", 40), ansiCyan))
}

func (r *answerStreamRenderer) Append(chunk string) {
	if r == nil || r.out == nil || chunk == "" {
		return
	}
	r.start()
	normalized := strings.ReplaceAll(strings.ReplaceAll(chunk, "\r\n", "\n"), "\r", "\n")
	for _, ch := range normalized {
		if ch == '\n' {
			r.pendingNewlines++
			continue
		}
		r.flushPendingNewlines()
		if r.lineStart {
			_, _ = fmt.Fprintf(r.out, "%s ", style("│", ansiCyan))
			r.lineStart = false
		}
		_, _ = fmt.Fprint(r.out, string(ch))
		r.hasVisibleText = true
	}
}

func (r *answerStreamRenderer) Finish() {
	if r == nil || r.out == nil || !r.started {
		return
	}
	r.pendingNewlines = 0
	if !r.lineStart {
		_, _ = fmt.Fprintln(r.out)
		r.lineStart = true
	}
	_, _ = fmt.Fprintln(r.out)
}

func (r *answerStreamRenderer) flushPendingNewlines() {
	if r.pendingNewlines == 0 {
		return
	}
	if !r.hasVisibleText {
		r.pendingNewlines = 0
		return
	}
	// Keep at most one blank line between paragraphs.
	newlineCount := r.pendingNewlines
	if newlineCount > 2 {
		newlineCount = 2
	}
	for i := 0; i < newlineCount; i++ {
		_, _ = fmt.Fprint(r.out, "\n")
	}
	r.pendingNewlines = 0
	r.lineStart = true
}

func parseBangCommand(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "!") {
		return "", false
	}
	command := strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	return command, true
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
	b.WriteString("[command mode]\n")
	b.WriteString("$ ")
	b.WriteString(command)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("exit=%d duration=%dms", exitCode, duration))
	if truncated {
		b.WriteString(" (truncated)")
	}

	if strings.TrimSpace(stdout) != "" {
		b.WriteString("\nstdout:\n")
		b.WriteString(stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		b.WriteString("\nstderr:\n")
		b.WriteString(stderr)
	}
	if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
		b.WriteString("\n(no output)")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderAssistantBlock(out io.Writer, content string, isFinal bool) {
	kind := "PLAN"
	color := ansiGray
	if isFinal {
		kind = "ANSWER"
		color = ansiCyan
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "%s %s\n", style("["+kind+"]", color+";"+ansiBold), style(strings.Repeat("─", 40), color))

	lines := compactAssistantLines(content)
	for _, line := range lines {
		if line == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintf(out, "%s %s\n", style("│", color), line)
	}
	_, _ = fmt.Fprintln(out)
}

func renderToolStart(out io.Writer, message string) {
	_, _ = fmt.Fprintf(out, "%s %s\n", style("[TOOL]", ansiYellow+";"+ansiBold), style(message, ansiYellow))
}

func renderToolResult(out io.Writer, message string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(message, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "  %s %s\n", style("->", ansiGreen+";"+ansiBold), style(lines[0], ansiGray))
	for _, line := range lines[1:] {
		if line == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintf(out, "     %s\n", styleToolDetailLine(line))
	}
}

func renderToolError(out io.Writer, message string) {
	_, _ = fmt.Fprintf(out, "  %s %s\n", style("x", ansiRed+";"+ansiBold), style(message, ansiRed))
}

func renderToolBlocked(out io.Writer, message string) {
	_, _ = fmt.Fprintf(out, "  %s %s\n", style("!", ansiYellow+";"+ansiBold), style("blocked: "+message, ansiYellow))
}

func style(text, codes string) string {
	if text == "" || !enableColor() {
		return text
	}
	segments := strings.Split(codes, ";")
	var builder strings.Builder
	for _, segment := range segments {
		code := strings.TrimSpace(segment)
		if code == "" {
			continue
		}
		builder.WriteString(code)
	}
	if builder.Len() == 0 {
		return text
	}
	return builder.String() + text + ansiReset
}

func styleToolDetailLine(line string) string {
	switch {
	case strings.HasPrefix(line, "diff --"), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
		return style(line, ansiYellow)
	case strings.HasPrefix(line, "@@"):
		return style(line, ansiCyan)
	case strings.HasPrefix(line, "+"):
		return style(line, ansiGreen)
	case strings.HasPrefix(line, "-"):
		return style(line, ansiRed)
	default:
		return style(line, ansiGray)
	}
}

func compactAssistantLines(content string) []string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	normalized = strings.Trim(normalized, "\n")
	if normalized == "" {
		return []string{""}
	}
	rawLines := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(rawLines))
	blankSeen := false
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			if blankSeen {
				continue
			}
			lines = append(lines, "")
			blankSeen = true
			continue
		}
		lines = append(lines, line)
		blankSeen = false
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func enableColor() bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("AGENT_NO_COLOR")) != "" {
		return false
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))) != "dumb"
}

func short(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}

func todoStatusMarker(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	default:
		return "[ ]"
	}
}

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func isComplexTask(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}
	if len([]rune(trimmed)) >= 80 {
		return true
	}
	keywords := []string{
		"并", "然后", "同时", "步骤", "重构", "实现", "修复", "优化",
		"and then", "step by step", "refactor", "implement", "fix",
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	delimiters := strings.Count(trimmed, "，") + strings.Count(trimmed, ",") + strings.Count(trimmed, ";") + strings.Count(trimmed, "；")
	if delimiters >= 2 {
		return true
	}
	return len(strings.Fields(trimmed)) >= 14
}
