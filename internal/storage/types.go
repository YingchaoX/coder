package storage

// SessionMeta 会话元数据
// SessionMeta holds session metadata
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

// TodoItem 待办条目
// TodoItem is a single todo entry
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}
