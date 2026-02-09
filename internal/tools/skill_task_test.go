package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coder/internal/permission"
	"coder/internal/skills"
)

func TestSkillToolListAndLoad(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Demo\n\ncontent"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := skills.Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	tool := NewSkillTool(m, func(name string, action string) permission.Decision {
		if name == "blocked" {
			return permission.DecisionDeny
		}
		return permission.DecisionAllow
	})

	listArgs, _ := json.Marshal(map[string]any{"action": "list"})
	listResult, err := tool.Execute(context.Background(), listArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listResult, "demo") {
		t.Fatalf("unexpected list result: %s", listResult)
	}

	loadArgs, _ := json.Marshal(map[string]any{"action": "load", "name": "demo"})
	loadResult, err := tool.Execute(context.Background(), loadArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loadResult, "content") {
		t.Fatalf("unexpected load result: %s", loadResult)
	}
}

func TestTaskTool(t *testing.T) {
	tool := NewTaskTool(func(ctx context.Context, agentName string, prompt string) (string, error) {
		return agentName + ":" + prompt, nil
	})
	args, _ := json.Marshal(map[string]any{"agent": "explore", "objective": "scan"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "explore:scan") {
		t.Fatalf("unexpected result: %s", result)
	}
}
