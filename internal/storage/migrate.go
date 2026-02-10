package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coder/internal/chat"
)

// MigrateFromJSON 将旧版 JSON session 文件迁移到 SQLite
// MigrateFromJSON migrates legacy JSON session files into SQLite
func MigrateFromJSON(jsonDir string, store *SQLiteStore) (int, error) {
	jsonDir = strings.TrimSpace(jsonDir)
	if jsonDir == "" {
		return 0, nil
	}

	sessionsDir := filepath.Join(jsonDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read sessions dir: %w", err)
	}

	migrated := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}

		metaPath := filepath.Join(sessionsDir, e.Name())
		sessionID := strings.TrimSuffix(e.Name(), ".meta.json")

		// 检查是否已存在 / Check if already migrated
		if _, loadErr := store.LoadSession(sessionID); loadErr == nil {
			continue
		}

		meta, messages, todos, err := loadJSONSession(sessionsDir, sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip migrate %s: %v\n", metaPath, err)
			continue
		}

		if err := store.CreateSession(meta); err != nil {
			fmt.Fprintf(os.Stderr, "migrate session %s failed: %v\n", sessionID, err)
			continue
		}
		if len(messages) > 0 {
			if err := store.SaveMessages(sessionID, messages); err != nil {
				fmt.Fprintf(os.Stderr, "migrate messages %s failed: %v\n", sessionID, err)
				continue
			}
		}
		if len(todos) > 0 {
			if err := store.ReplaceTodos(sessionID, todos); err != nil {
				fmt.Fprintf(os.Stderr, "migrate todos %s failed: %v\n", sessionID, err)
			}
		}
		migrated++
	}
	return migrated, nil
}

func loadJSONSession(dir, sessionID string) (SessionMeta, []chat.Message, []TodoItem, error) {
	metaPath := filepath.Join(dir, sessionID+".meta.json")
	msgPath := filepath.Join(dir, sessionID+".messages.json")
	todoPath := filepath.Join(dir, sessionID+".todo.json")

	var meta SessionMeta
	if err := readJSON(metaPath, &meta); err != nil {
		return SessionMeta{}, nil, nil, err
	}

	var messages []chat.Message
	if _, err := os.Stat(msgPath); err == nil {
		if err := readJSON(msgPath, &messages); err != nil {
			return SessionMeta{}, nil, nil, err
		}
	}

	var todos []TodoItem
	if _, err := os.Stat(todoPath); err == nil {
		_ = readJSON(todoPath, &todos) // 非关键 / Non-critical
	}

	return meta, messages, todos, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
