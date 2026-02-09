package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"coder/internal/chat"
)

type SessionMeta struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Agent      string `json:"agent"`
	Model      string `json:"model"`
	CWD        string `json:"cwd"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	Summary    string `json:"summary"`
	Compaction struct {
		Auto            bool   `json:"auto"`
		Prune           bool   `json:"prune"`
		LastCompactedAt string `json:"last_compacted_at,omitempty"`
	} `json:"compaction"`
}

type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

type Manager struct {
	baseDir     string
	sessionsDir string
	logsDir     string
	cacheDir    string
	stateDir    string
}

func NewManager(baseDir string) (*Manager, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("storage base dir is empty")
	}
	m := &Manager{
		baseDir:     baseDir,
		sessionsDir: filepath.Join(baseDir, "sessions"),
		logsDir:     filepath.Join(baseDir, "logs"),
		cacheDir:    filepath.Join(baseDir, "cache"),
		stateDir:    filepath.Join(baseDir, "state"),
	}
	for _, dir := range []string{m.baseDir, m.sessionsDir, m.logsDir, m.cacheDir, m.stateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create storage dir %s: %w", dir, err)
		}
	}
	return m, nil
}

func (m *Manager) CreateSession(agent, model, cwd string, compactionAuto, compactionPrune bool) (SessionMeta, []chat.Message, error) {
	id, err := newSessionID()
	if err != nil {
		return SessionMeta{}, nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	meta := SessionMeta{
		ID:        id,
		Title:     "",
		Agent:     strings.TrimSpace(agent),
		Model:     strings.TrimSpace(model),
		CWD:       strings.TrimSpace(cwd),
		CreatedAt: now,
		UpdatedAt: now,
	}
	meta.Compaction.Auto = compactionAuto
	meta.Compaction.Prune = compactionPrune
	if err := m.Save(meta, nil); err != nil {
		return SessionMeta{}, nil, err
	}
	return meta, nil, nil
}

func (m *Manager) Save(meta SessionMeta, messages []chat.Message) error {
	meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(meta.CreatedAt) == "" {
		meta.CreatedAt = meta.UpdatedAt
	}
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = inferTitle(messages)
	}

	metaPath := filepath.Join(m.sessionsDir, meta.ID+".meta.json")
	msgPath := filepath.Join(m.sessionsDir, meta.ID+".messages.json")
	logPath := filepath.Join(m.sessionsDir, meta.ID+".jsonl")

	if err := writeJSONFile(metaPath, meta); err != nil {
		return err
	}
	if err := writeJSONFile(msgPath, messages); err != nil {
		return err
	}
	if err := writeMessageLog(logPath, meta.ID, messages); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Load(sessionID string) (SessionMeta, []chat.Message, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return SessionMeta{}, nil, fmt.Errorf("session id is empty")
	}
	metaPath := filepath.Join(m.sessionsDir, id+".meta.json")
	msgPath := filepath.Join(m.sessionsDir, id+".messages.json")

	var meta SessionMeta
	if err := readJSONFile(metaPath, &meta); err != nil {
		return SessionMeta{}, nil, err
	}
	var messages []chat.Message
	if err := readJSONFile(msgPath, &messages); err != nil {
		return SessionMeta{}, nil, err
	}
	return meta, messages, nil
}

func (m *Manager) List() ([]SessionMeta, error) {
	entries, err := os.ReadDir(m.sessionsDir)
	if err != nil {
		return nil, err
	}
	metas := []SessionMeta{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		var meta SessionMeta
		if err := readJSONFile(filepath.Join(m.sessionsDir, e.Name()), &meta); err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt > metas[j].UpdatedAt
	})
	return metas, nil
}

func (m *Manager) Fork(sourceID, newAgent string) (SessionMeta, []chat.Message, error) {
	sourceMeta, sourceMessages, err := m.Load(sourceID)
	if err != nil {
		return SessionMeta{}, nil, err
	}
	forkMeta, _, err := m.CreateSession(newAgent, sourceMeta.Model, sourceMeta.CWD, sourceMeta.Compaction.Auto, sourceMeta.Compaction.Prune)
	if err != nil {
		return SessionMeta{}, nil, err
	}
	forkMeta.Summary = sourceMeta.Summary
	forkMeta.Title = sourceMeta.Title + " (fork)"
	if err := m.Save(forkMeta, sourceMessages); err != nil {
		return SessionMeta{}, nil, err
	}
	return forkMeta, sourceMessages, nil
}

func (m *Manager) RevertTo(sessionID string, keepMessages int) (SessionMeta, []chat.Message, error) {
	meta, messages, err := m.Load(sessionID)
	if err != nil {
		return SessionMeta{}, nil, err
	}
	if keepMessages < 0 || keepMessages > len(messages) {
		return SessionMeta{}, nil, fmt.Errorf("invalid keep message count %d", keepMessages)
	}
	messages = append([]chat.Message(nil), messages[:keepMessages]...)
	if err := m.Save(meta, messages); err != nil {
		return SessionMeta{}, nil, err
	}
	return meta, messages, nil
}

func (m *Manager) UpdateSummary(sessionID, summary string) (SessionMeta, error) {
	meta, messages, err := m.Load(sessionID)
	if err != nil {
		return SessionMeta{}, err
	}
	meta.Summary = strings.TrimSpace(summary)
	if err := m.Save(meta, messages); err != nil {
		return SessionMeta{}, err
	}
	return meta, nil
}

func (m *Manager) ListTodos(sessionID string) ([]TodoItem, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, fmt.Errorf("session id is empty")
	}
	path := filepath.Join(m.sessionsDir, id+".todo.json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []TodoItem
	if err := readJSONFile(path, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (m *Manager) ReplaceTodos(sessionID string, items []TodoItem) error {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return fmt.Errorf("session id is empty")
	}
	normalized := make([]TodoItem, 0, len(items))
	for _, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if strings.TrimSpace(item.ID) == "" {
			item.ID = fmt.Sprintf("todo_%d", len(normalized)+1)
		}
		status := strings.ToLower(strings.TrimSpace(item.Status))
		switch status {
		case "pending", "in_progress", "completed":
		default:
			status = "pending"
		}
		priority := strings.ToLower(strings.TrimSpace(item.Priority))
		switch priority {
		case "high", "medium", "low":
		default:
			priority = "medium"
		}
		item.Content = content
		item.Status = status
		item.Priority = priority
		normalized = append(normalized, item)
	}
	path := filepath.Join(m.sessionsDir, id+".todo.json")
	return writeJSONFile(path, normalized)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func newSessionID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return fmt.Sprintf("sess_%d_%s", time.Now().UTC().Unix(), hex.EncodeToString(buf)), nil
}

func inferTitle(messages []chat.Message) string {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		t := strings.TrimSpace(msg.Content)
		if t == "" {
			continue
		}
		runes := []rune(t)
		if len(runes) > 48 {
			return string(runes[:48]) + "..."
		}
		return t
	}
	return "new session"
}

func writeMessageLog(path, sessionID string, messages []chat.Message) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i, msg := range messages {
		record := map[string]any{
			"session_id":   sessionID,
			"index":        i,
			"role":         msg.Role,
			"name":         msg.Name,
			"tool_call_id": msg.ToolCallID,
			"content":      msg.Content,
			"tool_calls":   msg.ToolCalls,
			"created_at":   time.Now().UTC().Format(time.RFC3339),
		}
		if err := enc.Encode(record); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}
