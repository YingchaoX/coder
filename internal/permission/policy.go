package permission

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"coder/internal/config"
)

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionAsk   Decision = "ask"
	DecisionDeny  Decision = "deny"
)

type Result struct {
	Decision Decision
	Reason   string
}

type Policy struct {
	cfg config.PermissionConfig
}

func New(cfg config.PermissionConfig) *Policy {
	return &Policy{cfg: cfg}
}

func (p *Policy) Decide(toolName string, rawArgs json.RawMessage) Result {
	tool := strings.ToLower(strings.TrimSpace(toolName))
	if tool == "" {
		return Result{Decision: DecisionAsk, Reason: "tool missing"}
	}

	if tool == "bash" {
		return p.decideBash(rawArgs)
	}

	rule := p.toolRule(tool)
	decision := normalizeDecision(rule, p.defaultDecision())
	switch decision {
	case DecisionAllow:
		return Result{Decision: DecisionAllow}
	case DecisionDeny:
		return Result{Decision: DecisionDeny, Reason: "blocked by policy"}
	default:
		return Result{Decision: DecisionAsk, Reason: "policy requires approval"}
	}
}

func (p *Policy) SkillVisibilityDecision(skillName string) Decision {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return DecisionDeny
	}
	decision := normalizeDecision(p.cfg.Skill, p.defaultDecision())
	return decision
}

func (p *Policy) defaultDecision() Decision {
	if d := normalizeDecision(p.cfg.Default, ""); d != "" {
		return d
	}
	return normalizeDecision(p.cfg.DefaultWildcard, DecisionAsk)
}

func (p *Policy) toolRule(tool string) string {
	switch tool {
	case "read":
		return p.cfg.Read
	case "write":
		return p.cfg.Write
	case "list":
		return p.cfg.List
	case "glob":
		return p.cfg.Glob
	case "grep":
		return p.cfg.Grep
	case "patch":
		return p.cfg.Patch
	case "todoread":
		return p.cfg.TodoRead
	case "todowrite":
		return p.cfg.TodoWrite
	case "skill":
		return p.cfg.Skill
	case "task":
		return p.cfg.Task
	default:
		return p.cfg.Default
	}
}

func (p *Policy) decideBash(rawArgs json.RawMessage) Result {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(rawArgs, &in)
	command := strings.TrimSpace(in.Command)
	if command == "" {
		decision := normalizeDecision(p.cfg.Bash["*"], p.defaultDecision())
		if decision == DecisionDeny {
			return Result{Decision: DecisionDeny, Reason: "bash disabled by policy"}
		}
		if decision == DecisionAllow {
			return Result{Decision: DecisionAllow}
		}
		return Result{Decision: DecisionAsk, Reason: "policy requires approval"}
	}

	decision := normalizeDecision(p.cfg.Bash["*"], p.defaultDecision())
	patterns := make([]string, 0, len(p.cfg.Bash))
	for pattern := range p.cfg.Bash {
		if pattern == "*" {
			continue
		}
		patterns = append(patterns, pattern)
	}
	sort.Slice(patterns, func(i, j int) bool {
		return len(patterns[i]) > len(patterns[j])
	})
	for _, pattern := range patterns {
		ok, err := filepath.Match(pattern, command)
		if err != nil {
			continue
		}
		if ok {
			decision = normalizeDecision(p.cfg.Bash[pattern], decision)
			break
		}
	}

	switch decision {
	case DecisionAllow:
		return Result{Decision: DecisionAllow}
	case DecisionDeny:
		return Result{Decision: DecisionDeny, Reason: "bash blocked by policy"}
	default:
		return Result{Decision: DecisionAsk, Reason: "bash policy requires approval"}
	}
}

func normalizeDecision(raw string, fallback Decision) Decision {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case string(DecisionAllow):
		return DecisionAllow
	case string(DecisionAsk):
		return DecisionAsk
	case string(DecisionDeny):
		return DecisionDeny
	default:
		return fallback
	}
}
