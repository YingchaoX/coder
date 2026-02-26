package orchestrator

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"coder/internal/agent"
	"coder/internal/chat"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/permission"
	"coder/internal/provider"
	"coder/internal/storage"
	"coder/internal/tools"
)

type Orchestrator struct {
	provider          provider.Provider
	registry          *tools.Registry
	maxSteps          int
	onApproval        ApprovalFunc
	onTextChunk       TextChunkFunc
	onToolEvent       ToolEventFunc
	onTodoUpdate      OnTodoUpdate
	onContextUpdate   OnContextUpdate
	messages          []chat.Message
	messageTimestamps []string
	policy            *permission.Policy
	assembler         *contextmgr.Assembler
	compaction        config.CompactionConfig
	contextTokenLimit int
	activeAgent       agent.Profile
	agents            config.AgentConfig
	lastCompaction    string
	workflow          config.WorkflowConfig
	workspaceRoot     string
	compStrategy      contextmgr.CompactionStrategy
	mode              string        // build | plan (REPL /mode)
	skillNames        []string      // for /skills
	store             storage.Store // for /new, /resume, /model
	sessionIDRef      *string       // mutable current session ID
	configBasePath    string        // for /model persist
	lastSyncedMsgN    int
	undoStack         []turnUndoEntry
}

func New(providerClient provider.Provider, registry *tools.Registry, opts Options) *Orchestrator {
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
		skillNames:        append([]string(nil), opts.SkillNames...),
		store:             opts.Store,
		sessionIDRef:      opts.SessionIDRef,
		configBasePath:    strings.TrimSpace(opts.ConfigBasePath),
	}
	initialMode := strings.TrimSpace(strings.ToLower(activeAgent.Name))
	if initialMode == "" {
		initialMode = "build"
	}
	o.SetMode(initialMode)
	o.Reset()
	return o
}

// GetCurrentSessionID 返回当前会话 ID（供 todo 工具等使用）
func (o *Orchestrator) GetCurrentSessionID() string {
	if o.sessionIDRef != nil {
		return *o.sessionIDRef
	}
	return ""
}

// SetCurrentSessionID 设置当前会话 ID（/new、/resume 后调用）
func (o *Orchestrator) SetCurrentSessionID(id string) {
	if o.sessionIDRef != nil {
		*o.sessionIDRef = id
	}
}

func (o *Orchestrator) Reset() {
	o.messages = o.messages[:0]
	o.messageTimestamps = o.messageTimestamps[:0]
	o.lastCompaction = ""
	o.lastSyncedMsgN = 0
	o.undoStack = o.undoStack[:0]
}

func (o *Orchestrator) Messages() []chat.Message {
	return append([]chat.Message(nil), o.messages...)
}

func (o *Orchestrator) LoadMessages(messages []chat.Message) {
	o.messages = append([]chat.Message(nil), messages...)
	o.messageTimestamps = make([]string, len(o.messages))
	o.lastSyncedMsgN = len(o.messages)
	o.undoStack = o.undoStack[:0]
}

// appendMessage 追加一条新的对话消息，并记录时间戳（UTC RFC3339）。
// appendMessage appends a new chat message and records its timestamp (UTC RFC3339).
func (o *Orchestrator) appendMessage(msg chat.Message) {
	o.messages = append(o.messages, msg)
	now := time.Now().UTC().Format(time.RFC3339)
	o.messageTimestamps = append(o.messageTimestamps, now)
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

// SetMode 设置当前用户模式（build/plan），并联动 agent 与 permissions preset。
// SetMode sets current user mode (build/plan) and syncs agent + permissions preset.
func (o *Orchestrator) SetMode(mode string) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return
	}
	switch mode {
	case "build", "plan":
		o.mode = mode
		o.activeAgent = agent.Resolve(mode, o.agents)
		if o.policy != nil {
			_ = o.policy.ApplyPreset(mode)
		}
	}
}

// CurrentMode 返回当前模式
// CurrentMode returns the current user mode
func (o *Orchestrator) CurrentMode() string {
	if o.mode == "" {
		return "build"
	}
	return o.mode
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
	return o.provider.CurrentModel()
}

// currentToolDefs 返回当前会话可用工具的 OpenAI 兼容定义列表。
// currentToolDefs returns OpenAI-compatible tool definitions available in this session.
func (o *Orchestrator) currentToolDefs() []chat.ToolDef {
	if o == nil || o.registry == nil {
		return nil
	}
	return o.registry.Definitions()
}

func (o *Orchestrator) SetTextStreamCallback(fn TextChunkFunc) {
	o.onTextChunk = fn
}

func (o *Orchestrator) SetToolEventCallback(fn ToolEventFunc) {
	o.onToolEvent = fn
}

func (o *Orchestrator) SetTodoUpdateCallback(fn OnTodoUpdate) {
	o.onTodoUpdate = fn
}

func (o *Orchestrator) SetContextUpdateCallback(fn OnContextUpdate) {
	o.onContextUpdate = fn
}

func (o *Orchestrator) SetModel(model string) error {
	if o.provider == nil {
		return fmt.Errorf("provider unavailable")
	}
	return o.provider.SetModel(model)
}

func (o *Orchestrator) CompactNow() bool {
	compacted, summary, changed := contextmgr.CompactWithStrategy(
		context.Background(), o.messages, o.compaction.RecentMessages, o.compaction.Prune, o.compStrategy)
	if !changed {
		return false
	}
	o.messages = compacted
	o.messageTimestamps = make([]string, len(o.messages))
	o.lastCompaction = summary
	return true
}

func (o *Orchestrator) RunInput(ctx context.Context, input string, out io.Writer) (string, error) {
	trimmed := strings.TrimSpace(input)
	if cmd, args, ok := parseSlashCommand(trimmed); ok {
		result, err := o.runSlashCommand(ctx, input, cmd, args, out)
		if err != nil {
			return "", err
		}
		if out != nil && result != "" {
			fmt.Fprintln(out, result)
		}
		return result, nil
	}
	if command, ok := parseBangCommand(input); ok {
		return o.runBangCommand(ctx, input, command, out)
	}
	return o.RunTurn(ctx, input, out)
}
