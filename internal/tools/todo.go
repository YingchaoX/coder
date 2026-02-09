package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coder/internal/chat"
	"coder/internal/storage"
)

type TodoSessionIDFunc func() string

type TodoStore interface {
	ListTodos(sessionID string) ([]storage.TodoItem, error)
	ReplaceTodos(sessionID string, items []storage.TodoItem) error
}

type TodoReadTool struct {
	store     TodoStore
	sessionID TodoSessionIDFunc
}

type TodoWriteTool struct {
	store     TodoStore
	sessionID TodoSessionIDFunc
}

func NewTodoReadTool(store TodoStore, sessionID TodoSessionIDFunc) *TodoReadTool {
	return &TodoReadTool{store: store, sessionID: sessionID}
}

func NewTodoWriteTool(store TodoStore, sessionID TodoSessionIDFunc) *TodoWriteTool {
	return &TodoWriteTool{store: store, sessionID: sessionID}
}

func (t *TodoReadTool) Name() string {
	return "todoread"
}

func (t *TodoWriteTool) Name() string {
	return "todowrite"
}

func (t *TodoReadTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Read current todo list for this session",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func (t *TodoWriteTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Replace current todo list for this session",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"todos": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":       map[string]any{"type": "string"},
								"content":  map[string]any{"type": "string"},
								"status":   map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
								"priority": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
							},
							"required": []string{"content", "status", "priority"},
						},
					},
				},
				"required": []string{"todos"},
			},
		},
	}
}

func (t *TodoReadTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	_ = args
	sessionID := t.currentSessionID()
	if sessionID == "" {
		return "", fmt.Errorf("todo session is unavailable")
	}
	if t.store == nil {
		return "", fmt.Errorf("todo store unavailable")
	}
	items, err := t.store.ListTodos(sessionID)
	if err != nil {
		return "", err
	}
	inProgress := 0
	for _, item := range items {
		if item.Status == "in_progress" {
			inProgress++
		}
	}
	return mustJSON(map[string]any{
		"ok":          true,
		"session_id":  sessionID,
		"items":       items,
		"count":       len(items),
		"in_progress": inProgress,
	}), nil
}

func (t *TodoWriteTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	sessionID := t.currentSessionID()
	if sessionID == "" {
		return "", fmt.Errorf("todo session is unavailable")
	}
	if t.store == nil {
		return "", fmt.Errorf("todo store unavailable")
	}
	var in struct {
		Todos []storage.TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("todowrite args: %w", err)
	}
	if len(in.Todos) == 0 {
		if err := t.store.ReplaceTodos(sessionID, nil); err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"ok":         true,
			"session_id": sessionID,
			"count":      0,
			"items":      []storage.TodoItem{},
		}), nil
	}

	inProgress := 0
	for i := range in.Todos {
		in.Todos[i].Status = strings.ToLower(strings.TrimSpace(in.Todos[i].Status))
		if in.Todos[i].Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress > 1 {
		return "", fmt.Errorf("invalid todos: only one item can be in_progress")
	}

	if err := t.store.ReplaceTodos(sessionID, in.Todos); err != nil {
		return "", err
	}
	items, err := t.store.ListTodos(sessionID)
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{
		"ok":         true,
		"session_id": sessionID,
		"count":      len(items),
		"items":      items,
	}), nil
}

func (t *TodoReadTool) currentSessionID() string {
	if t.sessionID == nil {
		return ""
	}
	return strings.TrimSpace(t.sessionID())
}

func (t *TodoWriteTool) currentSessionID() string {
	if t.sessionID == nil {
		return ""
	}
	return strings.TrimSpace(t.sessionID())
}
