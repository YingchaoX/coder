package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coder/internal/chat"

	_ "modernc.org/sqlite"
)

// SQLiteStore 基于 SQLite (WAL 模式) 的持久化实现
// SQLiteStore implements Store using SQLite with WAL mode
type SQLiteStore struct {
	db   *sql.DB
	path string
}

// NewSQLiteStore 创建并初始化 SQLite 数据库
// NewSQLiteStore creates and initializes a SQLite database
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("sqlite db path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// 启用 WAL 模式和优化 PRAGMA / Enable WAL and performance PRAGMAs
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	store := &SQLiteStore{db: db, path: dbPath}
	if err := store.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	return store, nil
}

func (s *SQLiteStore) ensureSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id            TEXT PRIMARY KEY,
		title         TEXT NOT NULL DEFAULT '',
		agent         TEXT NOT NULL DEFAULT 'build',
		model         TEXT NOT NULL DEFAULT '',
		cwd           TEXT NOT NULL DEFAULT '',
		summary       TEXT NOT NULL DEFAULT '',
		compact_auto  INTEGER NOT NULL DEFAULT 1,
		compact_prune INTEGER NOT NULL DEFAULT 1,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS messages (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		seq         INTEGER NOT NULL,
		role        TEXT NOT NULL,
		content     TEXT NOT NULL DEFAULT '',
		name        TEXT NOT NULL DEFAULT '',
		tool_call_id TEXT NOT NULL DEFAULT '',
		tool_calls  TEXT NOT NULL DEFAULT '[]',
		reasoning   TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL,
		UNIQUE(session_id, seq)
	);

	CREATE TABLE IF NOT EXISTS todos (
		id         TEXT NOT NULL,
		session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		content    TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'pending',
		priority   TEXT NOT NULL DEFAULT 'medium',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(session_id, id)
	);

	CREATE TABLE IF NOT EXISTS permission_log (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		tool       TEXT NOT NULL,
		decision   TEXT NOT NULL,
		reason     TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);
	CREATE INDEX IF NOT EXISTS idx_todos_session ON todos(session_id);
	CREATE INDEX IF NOT EXISTS idx_permission_log_session ON permission_log(session_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close 关闭数据库连接 / Close the database connection
func (s *SQLiteStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// --- Session Operations ---

func (s *SQLiteStore) CreateSession(meta SessionMeta) error {
	now := nowUTC()
	if strings.TrimSpace(meta.CreatedAt) == "" {
		meta.CreatedAt = now
	}
	if strings.TrimSpace(meta.UpdatedAt) == "" {
		meta.UpdatedAt = now
	}
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, title, agent, model, cwd, summary, compact_auto, compact_prune, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		meta.ID, meta.Title, meta.Agent, meta.Model, meta.CWD,
		meta.Summary, boolToInt(meta.Compaction.Auto), boolToInt(meta.Compaction.Prune),
		meta.CreatedAt, meta.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SaveSession(meta SessionMeta) error {
	meta.UpdatedAt = nowUTC()
	_, err := s.db.Exec(`
		UPDATE sessions SET title=?, agent=?, model=?, cwd=?, summary=?,
			compact_auto=?, compact_prune=?, updated_at=?
		WHERE id=?`,
		meta.Title, meta.Agent, meta.Model, meta.CWD, meta.Summary,
		boolToInt(meta.Compaction.Auto), boolToInt(meta.Compaction.Prune),
		meta.UpdatedAt, meta.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) LoadSession(id string) (SessionMeta, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SessionMeta{}, fmt.Errorf("session id is empty")
	}
	row := s.db.QueryRow(`
		SELECT id, title, agent, model, cwd, summary, compact_auto, compact_prune, created_at, updated_at
		FROM sessions WHERE id=?`, id)

	var meta SessionMeta
	var compactAuto, compactPrune int
	err := row.Scan(&meta.ID, &meta.Title, &meta.Agent, &meta.Model, &meta.CWD,
		&meta.Summary, &compactAuto, &compactPrune, &meta.CreatedAt, &meta.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return SessionMeta{}, fmt.Errorf("session not found: %s", id)
		}
		return SessionMeta{}, fmt.Errorf("load session: %w", err)
	}
	meta.Compaction.Auto = compactAuto != 0
	meta.Compaction.Prune = compactPrune != 0
	return meta, nil
}

func (s *SQLiteStore) ListSessions() ([]SessionMeta, error) {
	rows, err := s.db.Query(`
		SELECT id, title, agent, model, cwd, summary, compact_auto, compact_prune, created_at, updated_at
		FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var metas []SessionMeta
	for rows.Next() {
		var meta SessionMeta
		var compactAuto, compactPrune int
		if err := rows.Scan(&meta.ID, &meta.Title, &meta.Agent, &meta.Model, &meta.CWD,
			&meta.Summary, &compactAuto, &compactPrune, &meta.CreatedAt, &meta.UpdatedAt); err != nil {
			continue
		}
		meta.Compaction.Auto = compactAuto != 0
		meta.Compaction.Prune = compactPrune != 0
		metas = append(metas, meta)
	}
	return metas, rows.Err()
}

// --- Message Operations ---

func (s *SQLiteStore) SaveMessages(sessionID string, messages []chat.Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 清除旧消息 / Clear old messages
	if _, err := tx.Exec("DELETE FROM messages WHERE session_id=?", sessionID); err != nil {
		return fmt.Errorf("delete old messages: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO messages (session_id, seq, role, content, name, tool_call_id, tool_calls, reasoning, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for i, msg := range messages {
		toolCallsJSON := "[]"
		if len(msg.ToolCalls) > 0 {
			data, marshalErr := json.Marshal(msg.ToolCalls)
			if marshalErr == nil {
				toolCallsJSON = string(data)
			}
		}
		reasoning := ""
		if msg.Reasoning != "" {
			reasoning = msg.Reasoning
		}
		if _, err := stmt.Exec(sessionID, i, msg.Role, msg.Content, msg.Name,
			msg.ToolCallID, toolCallsJSON, reasoning, now); err != nil {
			return fmt.Errorf("insert message %d: %w", i, err)
		}
	}

	// 更新 session 时间戳 / Update session timestamp
	if _, err := tx.Exec("UPDATE sessions SET updated_at=? WHERE id=?", now, sessionID); err != nil {
		return fmt.Errorf("update session timestamp: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) LoadMessages(sessionID string) ([]chat.Message, error) {
	rows, err := s.db.Query(`
		SELECT role, content, name, tool_call_id, tool_calls, reasoning
		FROM messages WHERE session_id=? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []chat.Message
	for rows.Next() {
		var msg chat.Message
		var toolCallsJSON string
		var reasoning string
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.Name,
			&msg.ToolCallID, &toolCallsJSON, &reasoning); err != nil {
			continue
		}
		if toolCallsJSON != "" && toolCallsJSON != "[]" {
			var calls []chat.ToolCall
			if err := json.Unmarshal([]byte(toolCallsJSON), &calls); err == nil {
				msg.ToolCalls = calls
			}
		}
		msg.Reasoning = reasoning
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// --- Todo Operations ---

func (s *SQLiteStore) ListTodos(sessionID string) ([]TodoItem, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is empty")
	}
	rows, err := s.db.Query(`
		SELECT id, content, status, priority FROM todos WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query todos: %w", err)
	}
	defer rows.Close()

	var items []TodoItem
	for rows.Next() {
		var item TodoItem
		if err := rows.Scan(&item.ID, &item.Content, &item.Status, &item.Priority); err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) ReplaceTodos(sessionID string, items []TodoItem) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is empty")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM todos WHERE session_id=?", sessionID); err != nil {
		return fmt.Errorf("delete old todos: %w", err)
	}

	now := nowUTC()
	stmt, err := tx.Prepare(`
		INSERT INTO todos (id, session_id, content, status, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for i, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("todo_%d", i+1)
		}
		status := normalizeStatus(item.Status)
		priority := normalizePriority(item.Priority)
		if _, err := stmt.Exec(id, sessionID, content, status, priority, now, now); err != nil {
			return fmt.Errorf("insert todo %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// --- Permission Log ---

func (s *SQLiteStore) LogPermission(entry PermissionEntry) error {
	_, err := s.db.Exec(`
		INSERT INTO permission_log (session_id, tool, decision, reason, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		entry.SessionID, entry.Tool, entry.Decision, entry.Reason, nowUTC())
	if err != nil {
		return fmt.Errorf("log permission: %w", err)
	}
	return nil
}

// --- Helpers ---

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending", "in_progress", "completed":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "pending"
	}
}

func normalizePriority(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "medium"
	}
}
