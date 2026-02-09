package main

import (
	"fmt"
	"strconv"
	"strings"

	"coder/internal/config"
	"coder/internal/mcp"
)

func resolveModelTarget(input string, availableModels []string) (string, error) {
	raw := strings.TrimSpace(input)
	if !strings.HasPrefix(raw, "/models") {
		return "", fmt.Errorf("invalid models command")
	}
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "/models"))
	if raw == "" {
		return "", fmt.Errorf("missing model")
	}
	if len(raw) >= 4 && strings.EqualFold(raw[:4], "set ") {
		raw = strings.TrimSpace(raw[4:])
	}
	if raw == "" {
		return "", fmt.Errorf("missing model")
	}
	if unquoted, err := strconv.Unquote(raw); err == nil {
		raw = strings.TrimSpace(unquoted)
	} else if len(raw) >= 2 {
		last := raw[len(raw)-1]
		if (raw[0] == '\'' && last == '\'') || (raw[0] == '"' && last == '"') {
			raw = strings.TrimSpace(raw[1 : len(raw)-1])
		}
	}
	if raw == "" {
		return "", fmt.Errorf("empty model")
	}
	for _, model := range availableModels {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		if strings.EqualFold(trimmed, raw) {
			return trimmed, nil
		}
	}
	if index, err := strconv.Atoi(raw); err == nil {
		if index < 1 || index > len(availableModels) {
			return "", fmt.Errorf("index out of range")
		}
		return strings.TrimSpace(availableModels[index-1]), nil
	}
	return raw, nil
}

func mergeAgentConfig(a config.AgentConfig, b config.AgentConfig) config.AgentConfig {
	out := a
	if strings.TrimSpace(b.Default) != "" {
		out.Default = b.Default
	}
	if len(b.Definitions) > 0 {
		out.Definitions = append(out.Definitions, b.Definitions...)
	}
	return out
}

func serverCfgEnabled(server *mcp.Server) bool {
	if server == nil {
		return false
	}
	return server.Enabled()
}

func normalizedModels(existing []string, current string) []string {
	out := make([]string, 0, len(existing)+1)
	seen := map[string]struct{}{}
	for _, model := range existing {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	current = strings.TrimSpace(current)
	if current != "" {
		if _, ok := seen[current]; !ok {
			out = append([]string{current}, out...)
		}
	}
	return out
}
