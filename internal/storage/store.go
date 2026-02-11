package storage

import "coder/internal/chat"

// Store 持久化接口，支持多后端 (SQLite / JSON legacy)
// Store is the persistence interface supporting multiple backends
type Store interface {
	// Session 操作 / Session operations
	CreateSession(meta SessionMeta) error
	SaveSession(meta SessionMeta) error
	LoadSession(id string) (SessionMeta, error)
	ListSessions() ([]SessionMeta, error)

	// Message 操作 / Message operations
	SaveMessages(sessionID string, messages []chat.Message) error
	AppendMessages(sessionID string, startSeq int, messages []chat.Message) error
	LoadMessages(sessionID string) ([]chat.Message, error)

	// Todo 操作 / Todo operations
	ListTodos(sessionID string) ([]TodoItem, error)
	ReplaceTodos(sessionID string, items []TodoItem) error

	// 权限日志 / Permission log
	LogPermission(entry PermissionEntry) error

	// 生命周期 / Lifecycle
	Close() error
}

// PermissionEntry 权限决策日志条目
// PermissionEntry records a single permission decision
type PermissionEntry struct {
	SessionID string
	Tool      string
	Decision  string
	Reason    string
}
