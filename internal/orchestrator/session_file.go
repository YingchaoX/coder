package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coder/internal/chat"
)

// sessionFileMeta 是写入 .coder/sessions/<session_id>.json 文件的会话级元数据。
// sessionFileMeta holds per-session metadata for .coder/sessions/<session_id>.json.
type sessionFileMeta struct {
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
	CWD   string `json:"cwd,omitempty"`
}

// sessionFileMessage 是带时间戳的对话消息表示。
// sessionFileMessage is a chat message representation with a timestamp.
type sessionFileMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	Reasoning  string          `json:"reasoning,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []chat.ToolCall `json:"tool_calls,omitempty"`
	Timestamp  string          `json:"timestamp,omitempty"`
}

// sessionFile 是单个 session 的完整持久化结构。
// sessionFile is the persisted structure for a single session.
type sessionFile struct {
	SessionID string               `json:"session_id"`
	CreatedAt string               `json:"created_at"`
	UpdatedAt string               `json:"updated_at"`
	Meta      sessionFileMeta      `json:"meta"`
	Messages  []sessionFileMessage `json:"messages"`
	Tools     []chat.ToolDef       `json:"tools,omitempty"`
}

// sessionFilePath 计算当前会话的 JSON 文件路径；若缺少 workspace 或 session ID，返回空字符串。
// sessionFilePath computes the JSON file path for the current session; returns empty string if workspace or session ID is missing.
func (o *Orchestrator) sessionFilePath() string {
	root := strings.TrimSpace(o.workspaceRoot)
	if root == "" {
		return ""
	}
	sid := o.GetCurrentSessionID()
	if strings.TrimSpace(sid) == "" {
		return ""
	}
	return filepath.Join(root, ".coder", "sessions", sid+".json")
}

// flushSessionToFile 将当前会话消息序列写入 .coder/sessions/<session_id>.json。
// flushSessionToFile writes current session messages into .coder/sessions/<session_id>.json.
// 失败时返回错误，但调用方通常应视为 best-effort，不阻断主对话流程。
func (o *Orchestrator) flushSessionToFile(_ context.Context) error {
	o.syncMessagesToStore()

	path := o.sessionFilePath()
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var existing sessionFile
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &existing)
	}

	createdAt := strings.TrimSpace(existing.CreatedAt)
	if createdAt == "" {
		createdAt = now
	}

	meta := sessionFileMeta{
		Title: existing.Meta.Title,
		Agent: o.activeAgent.Name,
		Model: o.CurrentModel(),
		CWD:   o.workspaceRoot,
	}

	// 先写入 assembler 提供的静态 system 消息（例如全局 system prompt），方便离线审阅完整上下文。
	// First, include static system messages from the assembler (e.g. global system prompts) for offline review.
	staticMessages := []chat.Message{}
	if o.assembler != nil {
		staticMessages = append(staticMessages, o.assembler.StaticMessages()...)
	}

	total := len(staticMessages) + len(o.messages)
	messages := make([]sessionFileMessage, 0, total)

	// 静态 system 消息使用 createdAt 作为时间戳。
	// Static system messages use createdAt as their timestamp.
	for _, msg := range staticMessages {
		messages = append(messages, sessionFileMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Reasoning:  msg.Reasoning,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  msg.ToolCalls,
			Timestamp:  createdAt,
		})
	}

	// 运行期消息尽量使用记录的时间戳，缺失时回落到 createdAt。
	// Runtime messages use recorded timestamps when available, falling back to createdAt.
	for i, msg := range o.messages {
		ts := ""
		if i < len(o.messageTimestamps) {
			ts = strings.TrimSpace(o.messageTimestamps[i])
		}
		if ts == "" {
			ts = createdAt
		}
		messages = append(messages, sessionFileMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Reasoning:  msg.Reasoning,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  msg.ToolCalls,
			Timestamp:  ts,
		})
	}

	out := sessionFile{
		SessionID: o.GetCurrentSessionID(),
		CreatedAt: createdAt,
		UpdatedAt: now,
		Meta:      meta,
		Messages:  messages,
		Tools:     o.currentToolDefs(),
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// syncMessagesToStore keeps SQLite session messages in sync for /resume and history recovery.
// Errors are intentionally ignored (best-effort, should not block foreground turns).
func (o *Orchestrator) syncMessagesToStore() {
	if o == nil || o.store == nil {
		return
	}
	sid := strings.TrimSpace(o.GetCurrentSessionID())
	if sid == "" {
		return
	}
	_ = o.store.SaveMessages(sid, o.messages)
}
