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
	if p.ToolEnabled["write"] {
		t.Fatalf("plan should disable write")
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
