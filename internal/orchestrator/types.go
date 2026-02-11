package orchestrator

import (
	"context"

	"coder/internal/agent"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/permission"
	"coder/internal/storage"
	"coder/internal/tools"
)

// TextChunkFunc 文本流式回调（v2: 由 Provider StreamCallbacks 驱动）
// TextChunkFunc is the text streaming callback (v2: driven by Provider StreamCallbacks)
type TextChunkFunc = func(chunk string)

// ToolEventFunc 工具执行事件回调（用于前端 REPL/TUI）
// ToolEventFunc is the tool execution event callback (for REPL/TUI frontends)
// done=false 表示工具开始，done=true 表示工具结束。
type ToolEventFunc = func(name, summary string, done bool)

// OnTodoUpdate 待办列表更新回调（todoread/todowrite/ensureSessionTodos 后推送；TUI 侧栏 / REPL 可 no-op）
// OnTodoUpdate is called after todoread/todowrite/ensureSessionTodos; TUI sidebar or REPL may no-op.
type OnTodoUpdate = func(items []string)

// OnContextUpdate 上下文 token 使用更新回调（步进后推送；REPL 用于提示符第一行，TUI 用于侧栏）
// OnContextUpdate is called after steps; REPL uses for prompt line 1, TUI for sidebar.
type OnContextUpdate = func(tokens, limit int, percent float64)

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
	SkillNames        []string      // for /skills (optional)
	Store             storage.Store // for /new, /resume, /model session update
	SessionIDRef      *string       // mutable current session ID (todo tools read this)
	ConfigBasePath    string        // project dir for ./.coder/config.json persist (/model)
}

type ContextStats struct {
	EstimatedTokens int
	ContextLimit    int
	UsagePercent    float64
	MessageCount    int
}
