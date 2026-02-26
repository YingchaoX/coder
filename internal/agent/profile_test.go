package agent

import (
	"testing"

	"coder/internal/config"
)

func TestResolveBuiltins(t *testing.T) {
	p := Resolve("plan", config.AgentConfig{Default: "build"})
	if p.Name != "plan" {
		t.Fatalf("name=%q", p.Name)
	}
	if p.ToolEnabled["edit"] {
		t.Fatalf("plan should disable edit")
	}
	if p.ToolEnabled["write"] {
		t.Fatalf("plan should disable write")
	}
	if p.ToolEnabled["task"] {
		t.Fatalf("plan should disable task")
	}
	if p.ToolEnabled["git_add"] {
		t.Fatalf("plan should disable git_add")
	}
	if p.ToolEnabled["git_commit"] {
		t.Fatalf("plan should disable git_commit")
	}
	if !p.ToolEnabled["question"] {
		t.Fatalf("plan should enable question")
	}
}

func TestResolveBuildDisablesTodoWrite(t *testing.T) {
	p := Resolve("build", config.AgentConfig{Default: "build"})
	if p.Name != "build" {
		t.Fatalf("name=%q", p.Name)
	}
	if p.ToolEnabled["todowrite"] {
		t.Fatalf("build should disable todowrite")
	}
	if !p.ToolEnabled["todoread"] {
		t.Fatalf("build should keep todoread enabled")
	}
	if p.ToolEnabled["question"] {
		t.Fatalf("build should disable question")
	}
}

func TestResolveCustomOverride(t *testing.T) {
	p := Resolve("custom", config.AgentConfig{
		Definitions: []config.AgentDefinition{{
			Name:  "custom",
			Mode:  "subagent",
			Tools: map[string]string{"bash": "deny", "read": "allow"},
		}},
	})
	if p.Name != "custom" {
		t.Fatalf("name=%q", p.Name)
	}
	if p.ToolEnabled["bash"] {
		t.Fatalf("custom bash should be disabled")
	}
	if !p.ToolEnabled["read"] {
		t.Fatalf("custom read should be enabled")
	}
}
