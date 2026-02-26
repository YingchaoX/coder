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

// isAllowedByCommandAllowlist 判断给定命令是否命中项目级 command_allowlist。
// isAllowedByCommandAllowlist checks whether the given command is allowed by project-level command_allowlist.
func (p *Policy) isAllowedByCommandAllowlist(command string) bool {
	if len(p.cfg.CommandAllowlist) == 0 {
		return false
	}
	name := config.NormalizeCommandName(command)
	if name == "" {
		return false
	}
	for _, raw := range p.cfg.CommandAllowlist {
		if strings.ToLower(strings.TrimSpace(raw)) == name {
			return true
		}
	}
	return false
}

// AddToCommandAllowlist 追加命令名到 allowlist，返回是否实际新增。
// AddToCommandAllowlist appends a command name to the allowlist and returns true if it was newly added.
func (p *Policy) AddToCommandAllowlist(commandName string) bool {
	name := strings.ToLower(strings.TrimSpace(commandName))
	if name == "" {
		return false
	}
	for _, raw := range p.cfg.CommandAllowlist {
		if strings.ToLower(strings.TrimSpace(raw)) == name {
			return false
		}
	}
	p.cfg.CommandAllowlist = append(p.cfg.CommandAllowlist, name)
	return true
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
	case "edit":
		return p.cfg.Edit
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
	case "fetch":
		return p.cfg.Fetch
	case "question":
		return p.cfg.Question
	case "lsp_diagnostics":
		return p.cfg.LSPDiagnostics
	case "lsp_definition":
		return p.cfg.LSPDefinition
	case "lsp_hover":
		return p.cfg.LSPHover
	case "git_status", "git_diff", "git_log", "pdf_parser":
		return p.cfg.Read
	case "git_add", "git_commit":
		return p.cfg.Write
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

	// allowlist：当策略决策为 ask 且命中项目级 command_allowlist 时，直接 allow。
	if decision == DecisionAsk && p.isAllowedByCommandAllowlist(command) {
		return Result{Decision: DecisionAllow}
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

// Summary 返回当前权限矩阵的简短描述（供 /permissions 展示）
func (p *Policy) Summary() string {
	def := string(p.defaultDecision())
	parts := []string{
		"default: " + def,
		"read: " + p.cfg.Read,
		"edit: " + p.cfg.Edit,
		"write: " + p.cfg.Write,
		"list: " + p.cfg.List,
		"glob: " + p.cfg.Glob,
		"grep: " + p.cfg.Grep,
		"patch: " + p.cfg.Patch,
		"todoread: " + p.cfg.TodoRead,
		"todowrite: " + p.cfg.TodoWrite,
		"skill: " + p.cfg.Skill,
		"task: " + p.cfg.Task,
		"fetch: " + p.cfg.Fetch,
		"question: " + p.cfg.Question,
		"lsp_diagnostics: " + p.cfg.LSPDiagnostics,
		"lsp_definition: " + p.cfg.LSPDefinition,
		"lsp_hover: " + p.cfg.LSPHover,
	}
	bashDef := def
	if p.cfg.Bash != nil {
		if v, ok := p.cfg.Bash["*"]; ok && strings.TrimSpace(v) != "" {
			bashDef = strings.TrimSpace(strings.ToLower(v))
		}
	}
	parts = append(parts, "bash: "+bashDef)
	return strings.Join(parts, ", ")
}

// PresetConfig 返回命名预设的权限配置；name 为 build | plan
func PresetConfig(name string) (config.PermissionConfig, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "build":
		return config.PermissionConfig{
			Default: "ask", Read: "allow", Edit: "ask", Write: "ask", List: "allow", Glob: "allow", Grep: "allow", Patch: "ask",
			LSPDiagnostics: "allow", LSPDefinition: "allow", LSPHover: "allow",
			TodoRead: "allow", TodoWrite: "allow", Skill: "ask", Task: "ask", Fetch: "ask",
			ExternalDir: "ask",
			Bash: map[string]string{"*": "ask", "ls *": "allow", "cat *": "allow", "grep *": "allow", "go test *": "allow", "pytest*": "allow", "npm test*": "allow", "pnpm test*": "allow", "yarn test*": "allow"},
		}, true
	case "plan":
		return config.PermissionConfig{
			Default: "ask", Read: "allow", Edit: "deny", Write: "deny", List: "allow", Glob: "allow", Grep: "allow", Patch: "deny",
			LSPDiagnostics: "allow", LSPDefinition: "allow", LSPHover: "allow",
			TodoRead: "allow", TodoWrite: "allow", Skill: "allow", Task: "deny", Fetch: "allow", Question: "allow",
			ExternalDir: "ask",
			Bash: map[string]string{
				"*":            "ask",
				"ls":           "allow",
				"ls *":         "allow",
				"cat *":        "allow",
				"grep *":       "allow",
				"uname":        "allow",
				"uname *":      "allow",
				"pwd":          "allow",
				"whoami":       "allow",
				"id":           "allow",
				"which *":      "allow",
				"env":          "allow",
				"git status":   "allow",
				"git status *": "allow",
				"git diff":     "allow",
				"git diff *":   "allow",
				"git log":      "allow",
				"git log *":    "allow",
			},
		}, true
	default:
		return config.PermissionConfig{}, false
	}
}

// ApplyPreset 应用命名预设并返回是否成功
func (p *Policy) ApplyPreset(name string) bool {
	cfg, ok := PresetConfig(name)
	if !ok {
		return false
	}
	p.cfg = cfg
	return true
}

// ExternalDirDecision 返回外部目录访问权限决策
func (p *Policy) ExternalDirDecision() Decision {
	return normalizeDecision(p.cfg.ExternalDir, DecisionAsk)
}
