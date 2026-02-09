package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coder/internal/chat"
)

type TaskRunner func(ctx context.Context, agentName string, prompt string) (string, error)

type TaskTool struct {
	runner TaskRunner
}

func NewTaskTool(runner TaskRunner) *TaskTool {
	return &TaskTool{runner: runner}
}

func (t *TaskTool) SetRunner(runner TaskRunner) {
	t.runner = runner
}

func (t *TaskTool) Name() string {
	return "task"
}

func (t *TaskTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Run a subagent task and return its summary",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent":     map[string]any{"type": "string"},
					"objective": map[string]any{"type": "string"},
					"prompt":    map[string]any{"type": "string"},
				},
				"required": []string{"agent", "objective"},
			},
		},
	}
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.runner == nil {
		return "", fmt.Errorf("task runner unavailable")
	}
	var in struct {
		Agent     string `json:"agent"`
		Objective string `json:"objective"`
		Prompt    string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("task args: %w", err)
	}
	agentName := strings.TrimSpace(in.Agent)
	if agentName == "" {
		return "", fmt.Errorf("task agent is empty")
	}
	objective := strings.TrimSpace(in.Objective)
	if objective == "" {
		objective = strings.TrimSpace(in.Prompt)
	}
	if objective == "" {
		return "", fmt.Errorf("task objective is empty")
	}

	summary, err := t.runner(ctx, agentName, objective)
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{
		"ok":      true,
		"agent":   agentName,
		"summary": summary,
	}), nil
}
