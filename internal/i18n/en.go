package i18n

// EnMessages English message catalog
var EnMessages = map[string]string{
	// UI (TUI/REPL) - Panel titles
	"panel.chat":  "Chat",
	"panel.files": "Files",
	"panel.logs":  "Logs",

	// UI (TUI sidebar)
	"sidebar.context": "Context",
	"sidebar.agent":   "Agent",
	"sidebar.model":   "Model",
	"sidebar.lsp":     "LSP",
	"sidebar.todo":    "Todo",

	// UI - Status bar
	"status.workspace":   "Workspace",
	"status.ready":       "Ready",
	"status.streaming":   "Streaming...",
	"status.thinking":    "Thinking...",
	"status.interrupted": "Generation interrupted",
	"status.expand_hint": "expand",

	// UI - Mode (REPL /mode or TUI Tab)
	"mode.build": "build",
	"mode.plan":  "plan",

	// UI (TUI sidebar 5 modules)
	"sidebar.tools":  "Tools",
	"sidebar.skills": "Skills",

	// UI - Input
	"input.placeholder": "Type a message... (Shift+Enter for newline)",
	"input.submit_hint": "Enter to send",

	// UI - Keybindings (TUI)
	"keys.tab":    "tab switch",
	"keys.esc":    "esc interrupt",
	"keys.ctrl_p": "ctrl+p commands",

	// Approval
	"approval.title":            "Approval Required",
	"approval.tool":             "Tool: %s",
	"approval.reason":           "Reason: %s",
	"approval.allow":            "Allow",
	"approval.deny":             "Deny",
	"approval.danger":           "⚠ DANGEROUS COMMAND",
	"approval.prompt":           "Allow this action? [y/N]",
	"approval.allow_all":        "Allow all (non-dangerous)",
	"approval.denied":           "Denied by user",
	"approval.callback_missing": "Approval callback unavailable",

	// Commands
	"cmd.help":     "Show available commands",
	"cmd.new":      "Create new session",
	"cmd.sessions": "List sessions",
	"cmd.exit":     "Exit application",

	// Errors
	"error.provider":   "Provider error: %s",
	"error.tool":       "Tool error: %s",
	"error.permission": "Permission denied: %s",
	"error.session":    "Session error: %s",

	// Context
	"context.tokens":    "Tokens: %d / %d (%.1f%%)",
	"context.messages":  "Messages: %d",
	"context.precise":   "precise",
	"context.estimated": "estimated",

	// Compaction
	"compact.done":       "Context compacted",
	"compact.not_needed": "Compaction not needed",
	"compact.llm":        "Using LLM for summarization",
	"compact.fallback":   "Using heuristic summarization (LLM unavailable)",

	// Session
	"session.new":      "New session: %s",
	"session.loaded":   "Loaded session: %s",
	"session.forked":   "Forked session: %s",
	"session.reverted": "Reverted to %d messages",
	"session.saved":    "Session saved",
	"session.none":     "No sessions found",

	// Model
	"model.current":  "Current model: %s",
	"model.switched": "Model switched to: %s",

	// Agent
	"agent.active": "Active agent: %s (%s)",

	// Tool results
	"tool.start":   "Running %s...",
	"tool.done":    "Done",
	"tool.blocked": "Blocked: %s",
	"tool.error":   "Error: %s",

	// Startup
	"startup.welcome":   "Coder started in workspace: %s",
	"startup.session":   "Session: %s agent=%s",
	"startup.repl_mode": "Running in REPL mode",
}
