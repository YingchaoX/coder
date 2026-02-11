package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"coder/internal/agent"
)

func (o *Orchestrator) RunSubtask(ctx context.Context, subagentName, objective string) (string, error) {
	profile, ok := agent.ResolveSubagent(subagentName, o.agents)
	if !ok {
		return "", fmt.Errorf("subagent not allowed: %s", subagentName)
	}
	if profile.ToolEnabled == nil {
		profile.ToolEnabled = map[string]bool{}
	}
	profile.ToolEnabled["task"] = false
	profile.ToolEnabled["todoread"] = false
	profile.ToolEnabled["todowrite"] = false

	child := New(o.provider, o.registry, Options{
		MaxSteps:          o.resolveMaxSteps(),
		OnApproval:        o.onApproval,
		Policy:            o.policy,
		Assembler:         o.assembler,
		Compaction:        o.compaction,
		ContextTokenLimit: o.contextTokenLimit,
		ActiveAgent:       profile,
		Agents:            o.agents,
		Workflow:          o.workflow,
		WorkspaceRoot:     o.workspaceRoot,
	})
	summaryPrompt := fmt.Sprintf("Subtask objective: %s\nReturn concise findings and recommended next step.", strings.TrimSpace(objective))
	result, err := child.RunTurn(ctx, summaryPrompt, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result) == "" {
		return "subtask finished with no text output", nil
	}
	return result, nil
}
