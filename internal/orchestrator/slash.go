package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"coder/internal/config"
	"coder/internal/storage"
)

// parseSlashCommand 解析 "/" 命令：返回 command 与 args（剩余部分）
// parseSlashCommand parses a "/" command: returns command and args (rest of line)
func parseSlashCommand(input string) (command string, args string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "/"))
	if rest == "" {
		return "", "", true
	}
	parts := strings.SplitN(rest, " ", 2)
	command = strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return command, args, true
}

// runSlashCommand 处理 "/" 内建命令；未知命令返回提示
// runSlashCommand handles "/" built-in commands; unknown command returns a hint
func (o *Orchestrator) runSlashCommand(ctx context.Context, rawInput, command, args string, out io.Writer) (string, error) {
	_ = rawInput
	switch command {
	case "help":
		return strings.Join([]string{
			"Commands:",
			"  /help",
			"  /model <name>",
			"  /permissions [preset]",
			"  /mode <plan|default|auto-edit|yolo>",
			"  /plan",
			"  /default",
			"  /auto-edit",
			"  /yolo",
			"  /tools",
			"  /skills",
			"  /todos",
			"  /new",
			"  /resume [session-id]",
			"  /sessions",
			"  /compact",
			"  /diff",
			"  /undo",
			"",
			"Input (TTY):",
			"  Enter = send",
			"  multi-line via paste ([copy N lines] then Enter)",
			"  Ctrl+D = ignored",
			"  Esc = clear current input line",
			"",
			"Runtime cancel:",
			"  Esc = stop current model/tool automation and return control to prompt (prints \"Cancelled by ESC\")",
			"",
			"Input (non-TTY): read all lines until EOF as one message.",
		}, "\n"), nil
	case "mode":
		mode := strings.TrimSpace(strings.ToLower(args))
		if mode == "" {
			return "Current mode: " + o.CurrentMode() + ". Usage: /mode plan|default|auto-edit|yolo", nil
		}
		o.SetMode(mode)
		return "Mode set to " + o.CurrentMode(), nil
	case "plan", "default", "auto-edit", "yolo":
		o.SetMode(command)
		return "Mode set to " + command, nil
	case "tools":
		names := o.registry.Names()
		if len(names) == 0 {
			return "No tools registered.", nil
		}
		return "Tools: " + strings.Join(names, ", "), nil
	case "skills":
		if len(o.skillNames) == 0 {
			return "No skills loaded.", nil
		}
		return "Skills: " + strings.Join(o.skillNames, ", "), nil
	case "todos":
		if !o.registry.Has("todoread") {
			return "Todo tool not available.", nil
		}
		result, err := o.registry.Execute(ctx, "todoread", json.RawMessage(`{}`))
		if err != nil {
			return "Failed to read todos: " + err.Error(), nil
		}
		items := todoItemsFromResult(result)
		if len(items) == 0 {
			return "No todos.", nil
		}
		return "Todos:\n  " + strings.Join(items, "\n  "), nil
	case "model":
		model := strings.TrimSpace(args)
		if model == "" {
			return "Current model: " + o.provider.CurrentModel() + ". Usage: /model <name>", nil
		}
		if err := o.provider.SetModel(model); err != nil {
			return "Failed to set model: " + err.Error(), nil
		}
		sid := o.GetCurrentSessionID()
		if o.store != nil && sid != "" {
			meta, err := o.store.LoadSession(sid)
			if err == nil {
				meta.Model = model
				_ = o.store.SaveSession(meta)
			}
		}
		if o.configBasePath != "" {
			if err := config.WriteProviderModel(o.configBasePath, model); err != nil {
				return "Model set to " + model + " (config persist failed: " + err.Error() + ")", nil
			}
		}
		return "Model set to " + model, nil
	case "permissions":
		preset := strings.TrimSpace(strings.ToLower(args))
		if preset == "" {
			return "Current permissions: " + o.policy.Summary() + ". Presets: strict, balanced, auto-edit, yolo. Usage: /permissions [preset]", nil
		}
		if o.policy.ApplyPreset(preset) {
			return "Permissions set to preset: " + preset, nil
		}
		return "Unknown preset: " + preset + ". Use: strict, balanced, auto-edit, yolo", nil
	case "new":
		if o.store == nil {
			return "Store not available.", nil
		}
		model := o.provider.CurrentModel()
		if model == "" {
			model = "default"
		}
		newMeta := storage.SessionMeta{
			ID:    storage.NewSessionID(),
			Agent: o.activeAgent.Name,
			Model: model,
			CWD:   o.workspaceRoot,
		}
		if err := o.store.CreateSession(newMeta); err != nil {
			return "Failed to create session: " + err.Error(), nil
		}
		o.Reset()
		o.SetCurrentSessionID(newMeta.ID)
		// After creating a new session and clearing messages, recompute context tokens
		// so REPL/TUI can immediately show an accurate "context: N tokens" line.
		o.emitContextUpdate()
		return "New session: " + newMeta.ID, nil
	case "sessions":
		return o.renderSessionListForResume(), nil
	case "resume":
		if o.store == nil {
			return "Store not available.", nil
		}
		sid := strings.TrimSpace(args)
		if sid == "" {
			return o.renderSessionListForResume(), nil
		}
		_, err := o.store.LoadSession(sid)
		if err != nil {
			return "Session not found: " + sid, nil
		}
		msgs, err := o.store.LoadMessages(sid)
		if err != nil {
			return "Failed to load messages: " + err.Error(), nil
		}
		o.LoadMessages(msgs)
		o.SetCurrentSessionID(sid)
		_ = o.flushSessionToFile(ctx)
		// After loading a historical session, recompute context tokens so the prompt
		// reflects the restored conversation length.
		o.emitContextUpdate()
		return fmt.Sprintf("Resumed session %s (%d messages)", sid, len(msgs)), nil
	case "compact":
		if !o.CompactNow() {
			last := strings.TrimSpace(o.LastCompactionSummary())
			if last == "" {
				return "No compaction performed (context below threshold or no messages).", nil
			}
			return "Compaction summary (no structural changes applied):\n" + last, nil
		}
		summary := strings.TrimSpace(o.LastCompactionSummary())
		_ = o.flushSessionToFile(ctx)
		// Compaction changes the effective context length; recompute tokens so the
		// next prompt shows the new (usually shorter) context usage.
		o.emitContextUpdate()
		if summary == "" {
			return "Context compacted.", nil
		}
		return "Context compacted. Summary:\n" + summary, nil
	case "diff":
		if !o.registry.Has("bash") {
			return "Diff unavailable: bash tool not registered.", nil
		}
		result, err := o.registry.Execute(ctx, "bash", json.RawMessage(`{"command":"git diff --stat && git diff"}`))
		if err != nil {
			return "Failed to run git diff: " + err.Error(), nil
		}
		// 直接返回 bash JSON 原文，由调用方按需渲染；避免在此依赖命令模式专用渲染逻辑。
		return result, nil
	case "undo":
		undoResult, err := o.undoLastTurn()
		if err != nil {
			return "Failed to undo last turn: " + err.Error(), nil
		}
		return undoResult, nil
	default:
		return "Unknown command: /" + command + ". Type /help for available commands.", nil
	}
}

func (o *Orchestrator) renderSessionListForResume() string {
	if o.store == nil {
		return "Store not available."
	}
	metas, err := o.store.ListSessions()
	if err != nil {
		return "Failed to list sessions: " + err.Error()
	}
	if len(metas) == 0 {
		return "No saved sessions. Use /new to create one."
	}
	const maxItems = 12
	limit := len(metas)
	if limit > maxItems {
		limit = maxItems
	}
	current := strings.TrimSpace(o.GetCurrentSessionID())
	lines := make([]string, 0, limit+3)
	lines = append(lines, "Recent sessions (timezone: Asia/Shanghai, UTC+08:00):")
	for i := 0; i < limit; i++ {
		meta := metas[i]
		model := strings.TrimSpace(meta.Model)
		if model == "" {
			model = "-"
		}
		agent := strings.TrimSpace(meta.Agent)
		if agent == "" {
			agent = "-"
		}
		updatedRaw := strings.TrimSpace(meta.UpdatedAt)
		if updatedRaw == "" {
			updatedRaw = strings.TrimSpace(meta.CreatedAt)
		}
		updated := "-"
		if updatedRaw != "" {
			updated = formatSessionTimeForDisplay(updatedRaw)
		}
		marker := " "
		if current != "" && current == strings.TrimSpace(meta.ID) {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("  %s %s  model=%s  agent=%s  updated=%s", marker, meta.ID, model, agent, updated))
	}
	if len(metas) > limit {
		lines = append(lines, fmt.Sprintf("  ... and %d more", len(metas)-limit))
	}
	lines = append(lines, "Use /resume <session-id> to restore.")
	return strings.Join(lines, "\n")
}

func formatSessionTimeForDisplay(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "-"
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return ts.UTC().Format(time.RFC3339)
	}
	return ts.In(loc).Format("2006-01-02 15:04:05 UTC+08:00")
}
