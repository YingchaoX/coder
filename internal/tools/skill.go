package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coder/internal/chat"
	"coder/internal/permission"
	"coder/internal/skills"
)

type SkillDecisionFunc func(name string, action string) permission.Decision

type SkillTool struct {
	manager  *skills.Manager
	decideFn SkillDecisionFunc
}

func NewSkillTool(manager *skills.Manager, decideFn SkillDecisionFunc) *SkillTool {
	return &SkillTool{manager: manager, decideFn: decideFn}
}

func (t *SkillTool) Name() string {
	return "skill"
}

func (t *SkillTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "List or load skill content from SKILL.md",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []string{"list", "load"}},
					"name":   map[string]any{"type": "string"},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (t *SkillTool) ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error) {
	var in struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("skill args: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action != "load" || t.decideFn == nil {
		return nil, nil
	}
	if t.decideFn(strings.TrimSpace(in.Name), action) != permission.DecisionAsk {
		return nil, nil
	}
	return &ApprovalRequest{
		Tool:    t.Name(),
		Reason:  fmt.Sprintf("skill load requires approval: %s", strings.TrimSpace(in.Name)),
		RawArgs: string(args),
	}, nil
}

func (t *SkillTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	if t.manager == nil {
		return "", fmt.Errorf("skill manager unavailable")
	}
	var in struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("skill args: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "list":
		all := t.manager.List()
		items := make([]map[string]any, 0, len(all))
		for _, s := range all {
			if t.decideFn != nil && t.decideFn(s.Name, action) == permission.DecisionDeny {
				continue
			}
			items = append(items, map[string]any{
				"name":        s.Name,
				"description": s.Description,
			})
		}
		return mustJSON(map[string]any{"ok": true, "items": items, "count": len(items)}), nil
	case "load":
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return "", fmt.Errorf("skill name is empty")
		}
		if t.decideFn != nil && t.decideFn(name, action) == permission.DecisionDeny {
			return "", fmt.Errorf("skill denied by permission")
		}
		content, err := t.manager.Load(name)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"ok":      true,
			"name":    name,
			"content": content,
		}), nil
	default:
		return "", fmt.Errorf("invalid action: %s", in.Action)
	}
}
