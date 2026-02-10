package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"coder/internal/config"
	"coder/internal/skills"
)

const defaultSystemPrompt = "You are an offline coding agent. Use tools when needed, keep answers concise, briefly state your next step before calling tools, and reply in the same language as the user unless asked otherwise."

var mentionPattern = regexp.MustCompile(`@([^\s]+)`)

func newSessionID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("sess_%d_%s", time.Now().UTC().Unix(), hex.EncodeToString(buf))
}

func mergeAgentConfig(a config.AgentConfig, b config.AgentConfig) config.AgentConfig {
	out := a
	if b.Default != "" {
		out.Default = b.Default
	}
	if len(b.Definitions) > 0 {
		out.Definitions = append(out.Definitions, b.Definitions...)
	}
	return out
}

func discoverSkills(cfg config.Config) (*skills.Manager, error) {
	return skills.Discover(cfg.Skills.Paths)
}
