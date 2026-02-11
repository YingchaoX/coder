package skills

import (
	_ "embed"
)

//go:embed builtin/create-skill/SKILL.md
var builtinCreateSkill string

// MergeBuiltin merges embedded builtin skills into the manager.
// Only adds a builtin skill if the name is not already present (user path overrides builtin).
func MergeBuiltin(m *Manager) {
	if m == nil || m.items == nil {
		return
	}
	if m.builtinContent == nil {
		m.builtinContent = make(map[string]string)
	}
	content := builtinCreateSkill
	if content == "" {
		return
	}
	info, err := parseSkillContent(content, "builtin:create-skill")
	if err != nil {
		return
	}
	if _, exists := m.items[info.Name]; exists {
		return
	}
	m.items[info.Name] = info
	m.builtinContent[info.Name] = content
}
