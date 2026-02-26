package agent

import (
	"strings"

	"coder/internal/config"
)

type Profile struct {
	Name          string
	Mode          string
	Description   string
	ModelOverride string
	ToolEnabled   map[string]bool
	MaxSteps      int
}

func Builtins() map[string]Profile {
	build := Profile{
		Name:        "build",
		Mode:        "primary",
		Description: "Delivery-focused primary agent",
		ToolEnabled: defaultToolSet(true),
	}
	// Build mode can read todo state but cannot plan/update todos.
	build.ToolEnabled["todowrite"] = false
	// Build mode cannot ask clarifying questions (plan-only).
	build.ToolEnabled["question"] = false

	plan := Profile{
		Name:        "plan",
		Mode:        "primary",
		Description: "Read-only planning primary agent",
		ToolEnabled: defaultToolSet(true),
	}
	plan.ToolEnabled["write"] = false
	plan.ToolEnabled["edit"] = false
	plan.ToolEnabled["patch"] = false
	plan.ToolEnabled["task"] = false
	plan.ToolEnabled["git_add"] = false
	plan.ToolEnabled["git_commit"] = false
	// Plan mode can ask clarifying questions when user intent is ambiguous.
	plan.ToolEnabled["question"] = true

	general := Profile{
		Name:        "general",
		Mode:        "subagent",
		Description: "General exploration subagent",
		ToolEnabled: defaultToolSet(true),
	}

	explore := Profile{
		Name:        "explore",
		Mode:        "subagent",
		Description: "Search-heavy read-only subagent",
		ToolEnabled: map[string]bool{
			"read":      true,
			"list":      true,
			"glob":      true,
			"grep":      true,
			"skill":     true,
			"todoread":  true,
			"todowrite": false,
			"edit":      false,
			"write":     false,
			"patch":     false,
			"bash":      false,
			"task":      false,
		},
	}

	return map[string]Profile{
		build.Name:   build,
		plan.Name:    plan,
		general.Name: general,
		explore.Name: explore,
	}
}

func Resolve(name string, cfg config.AgentConfig) Profile {
	profiles := Builtins()
	for _, d := range cfg.Definitions {
		profiles[d.Name] = applyDefinition(profiles[d.Name], d)
	}

	resolved := strings.TrimSpace(name)
	if resolved == "" {
		resolved = strings.TrimSpace(cfg.Default)
	}
	if resolved == "" {
		resolved = "build"
	}
	if p, ok := profiles[resolved]; ok {
		return p
	}
	return profiles["build"]
}

func ResolveSubagent(name string, cfg config.AgentConfig) (Profile, bool) {
	p := Resolve(name, cfg)
	if strings.ToLower(strings.TrimSpace(p.Mode)) != "subagent" {
		return p, false
	}
	return p, true
}

func applyDefinition(base Profile, d config.AgentDefinition) Profile {
	if base.Name == "" {
		base = Profile{Name: d.Name, ToolEnabled: defaultToolSet(true)}
	}
	if strings.TrimSpace(d.Mode) != "" {
		base.Mode = d.Mode
	}
	if strings.TrimSpace(d.Description) != "" {
		base.Description = d.Description
	}
	if strings.TrimSpace(d.ModelOverride) != "" {
		base.ModelOverride = d.ModelOverride
	}
	if d.MaxSteps > 0 {
		base.MaxSteps = d.MaxSteps
	}
	if len(d.Tools) > 0 {
		for name, decision := range d.Tools {
			base.ToolEnabled[name] = parseToolDecision(decision)
		}
	}
	return base
}

func parseToolDecision(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "off", "deny", "disable", "disabled", "false", "0", "no":
		return false
	default:
		return true
	}
}

func defaultToolSet(v bool) map[string]bool {
	return map[string]bool{
		"read":            v,
		"edit":            v,
		"write":           v,
		"list":            v,
		"glob":            v,
		"grep":            v,
		"patch":           v,
		"bash":            v,
		"skill":           v,
		"task":            v,
		"todoread":        v,
		"todowrite":       v,
		"lsp_diagnostics": v,
		"lsp_definition":  v,
		"lsp_hover":       v,
		"git_status":      v,
		"git_diff":        v,
		"git_log":         v,
		"git_add":         v,
		"git_commit":      v,
		"fetch":           v,
		"pdf_parser":      v,
		"question":        false,
	}
}
