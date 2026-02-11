package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ProviderConfig struct {
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	Models    []string `json:"models"`
	APIKey    string   `json:"api_key"`
	TimeoutMS int      `json:"timeout_ms"`
}

type RuntimeConfig struct {
	WorkspaceRoot     string `json:"workspace_root"`
	MaxSteps          int    `json:"max_steps"`
	ContextTokenLimit int    `json:"context_token_limit"`
}

type SafetyConfig struct {
	CommandTimeoutMS int `json:"command_timeout_ms"`
	OutputLimitBytes int `json:"output_limit_bytes"`
}

type CompactionConfig struct {
	Auto           bool    `json:"auto"`
	Prune          bool    `json:"prune"`
	Threshold      float64 `json:"threshold"`
	RecentMessages int     `json:"recent_messages"`
}

type ApprovalConfig struct {
	// AutoApproveAsk 控制策略层 ask 的默认行为；true 时可在非交互模式下自动放行。
	// AutoApproveAsk controls default behavior for policy-level ask; when true, ask may be auto-approved in non-interactive mode.
	AutoApproveAsk bool `json:"auto_approve_ask"`
	// Interactive 决定是否启用交互式审批（在 stdout 打印命令并读取 y/n/always）。
	// Interactive decides whether to run interactive approval (print command to stdout and read y/n/always).
	Interactive bool `json:"interactive"`
}

type PermissionConfig struct {
	DefaultWildcard string            `json:"*"`
	Default         string            `json:"default"`
	Read            string            `json:"read"`
	Write           string            `json:"write"`
	List            string            `json:"list"`
	Glob            string            `json:"glob"`
	Grep            string            `json:"grep"`
	Patch           string            `json:"patch"`
	TodoRead        string            `json:"todoread"`
	TodoWrite       string            `json:"todowrite"`
	Bash            map[string]string `json:"bash"`
	Skill           string            `json:"skill"`
	Task            string            `json:"task"`
	ExternalDir     string            `json:"external_directory"`
	// CommandAllowlist 记录“始终同意的命令”（按命令名归一化）。
	// CommandAllowlist stores commands that have been marked as \"always allow\" (normalized by command name).
	CommandAllowlist []string `json:"command_allowlist"`
	InstructionFiles []string `json:"instruction_files"`
}

type WorkflowConfig struct {
	RequireTodoForComplex bool     `json:"require_todo_for_complex"`
	AutoVerifyAfterEdit   bool     `json:"auto_verify_after_edit"`
	MaxVerifyAttempts     int      `json:"max_verify_attempts"`
	VerifyCommands        []string `json:"verify_commands"`
}

type AgentDefinition struct {
	Name          string            `json:"name"`
	Mode          string            `json:"mode"`
	Description   string            `json:"description"`
	ModelOverride string            `json:"model_override"`
	Tools         map[string]string `json:"tools"`
	MaxSteps      int               `json:"max_steps"`
	Temperature   float64           `json:"temperature"`
	TopP          float64           `json:"top_p"`
}

type AgentConfig struct {
	Default     string            `json:"default"`
	Definitions []AgentDefinition `json:"definitions"`
}

type SkillsConfig struct {
	Paths []string `json:"paths"`
}

type StorageConfig struct {
	BaseDir       string `json:"base_dir"`
	LogMaxMB      int    `json:"log_max_mb"`
	CacheTTLHours int    `json:"cache_ttl_hours"`
}

type Config struct {
	Provider     ProviderConfig   `json:"provider"`
	Runtime      RuntimeConfig    `json:"runtime"`
	Safety       SafetyConfig     `json:"safety"`
	Compaction   CompactionConfig `json:"compaction"`
	Workflow     WorkflowConfig   `json:"workflow"`
	Approval     ApprovalConfig   `json:"approval"`
	Permission   PermissionConfig `json:"permission"`
	Agent        AgentConfig      `json:"agent"`
	Agents       AgentConfig      `json:"agents"`
	Skills       SkillsConfig     `json:"skills"`
	Instructions []string         `json:"instructions"`
	Storage      StorageConfig    `json:"storage"`
}

type fileCompactionConfig struct {
	Auto           *bool    `json:"auto"`
	Prune          *bool    `json:"prune"`
	Threshold      *float64 `json:"threshold"`
	RecentMessages *int     `json:"recent_messages"`
}

type fileWorkflowConfig struct {
	RequireTodoForComplex *bool     `json:"require_todo_for_complex"`
	AutoVerifyAfterEdit   *bool     `json:"auto_verify_after_edit"`
	MaxVerifyAttempts     *int      `json:"max_verify_attempts"`
	VerifyCommands        *[]string `json:"verify_commands"`
}

type fileApprovalConfig struct {
	AutoApproveAsk *bool `json:"auto_approve_ask"`
	Interactive    *bool `json:"interactive"`
}

type fileConfig struct {
	Provider     *ProviderConfig       `json:"provider"`
	Runtime      *RuntimeConfig        `json:"runtime"`
	Safety       *SafetyConfig         `json:"safety"`
	Compaction   *fileCompactionConfig `json:"compaction"`
	Workflow     *fileWorkflowConfig   `json:"workflow"`
	Approval     *fileApprovalConfig   `json:"approval"`
	Permission   *PermissionConfig     `json:"permission"`
	Agent        *AgentConfig          `json:"agent"`
	Agents       *AgentConfig          `json:"agents"`
	Skills       *SkillsConfig         `json:"skills"`
	Instructions *[]string             `json:"instructions"`
	Storage      *StorageConfig        `json:"storage"`
}

func Default() Config {
	return Config{
		Provider: ProviderConfig{
			BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:     "qwen3-coder-30b-a3b-instruct",
			Models:    []string{"qwen3-coder-30b-a3b-instruct"},
			TimeoutMS: 120000,
		},
		Runtime: RuntimeConfig{
			MaxSteps:          128,
			ContextTokenLimit: 24000,
		},
		Safety: SafetyConfig{
			CommandTimeoutMS: 120000,
			OutputLimitBytes: 1 << 20,
		},
		Compaction: CompactionConfig{
			Auto:           true,
			Prune:          true,
			Threshold:      0.8,
			RecentMessages: 12,
		},
		Approval: ApprovalConfig{
			AutoApproveAsk: false,
			Interactive:    true,
		},
		Permission: PermissionConfig{
			DefaultWildcard: "ask",
			Read:            "allow",
			List:            "allow",
			Glob:            "allow",
			Grep:            "allow",
			Write:           "ask",
			Patch:           "ask",
			TodoRead:        "allow",
			TodoWrite:       "allow",
			Skill:           "ask",
			Task:            "ask",
			Bash: map[string]string{
				"*":          "ask",
				"ls *":       "allow",
				"cat *":      "allow",
				"grep *":     "allow",
				"go test *":  "allow",
				"pytest*":    "allow",
				"npm test*":  "allow",
				"pnpm test*": "allow",
				"yarn test*": "allow",
			},
			ExternalDir: "deny",
		},
		Workflow: WorkflowConfig{
			RequireTodoForComplex: true,
			AutoVerifyAfterEdit:   true,
			MaxVerifyAttempts:     2,
			VerifyCommands:        nil,
		},
		Agent:  AgentConfig{Default: "build"},
		Skills: SkillsConfig{Paths: []string{"./.coder/skills", "~/.coder/skills"}},
		Storage: StorageConfig{
			BaseDir:       "~/.coder",
			LogMaxMB:      20,
			CacheTTLHours: 168,
		},
	}
}

// MergeAgentConfig 合并 Agent 配置：b 的 Default 覆盖 a，b 的 Definitions 追加到 a
// MergeAgentConfig merges agent config: b.Default overrides a; b.Definitions are appended to a
func MergeAgentConfig(a, b AgentConfig) AgentConfig {
	out := a
	if strings.TrimSpace(b.Default) != "" {
		out.Default = b.Default
	}
	if len(b.Definitions) > 0 {
		out.Definitions = append(out.Definitions, b.Definitions...)
	}
	return out
}

func Load(path string) (Config, error) {
	cfg := Default()

	for _, globalPath := range globalConfigPaths() {
		if err := mergeFromFile(&cfg, globalPath); err != nil {
			return Config{}, err
		}
	}

	resolvedPath := strings.TrimSpace(path)
	if envPath := strings.TrimSpace(os.Getenv("AGENT_CONFIG_PATH")); envPath != "" {
		resolvedPath = envPath
	}
	if resolvedPath == "" {
		resolvedPath = findProjectConfigPath()
	}
	if err := mergeFromFile(&cfg, resolvedPath); err != nil {
		return Config{}, err
	}

	if err := normalize(&cfg); err != nil {
		return Config{}, err
	}
	return applyEnv(cfg)
}

func globalConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	current := filepath.Join(home, ".coder", "config.json")
	return []string{current}
}

func findProjectConfigPath() string {
	candidates := []string{
		"agent.config.json",
		".coder/config.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func mergeFromFile(cfg *Config, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	resolved, err := expandPath(path)
	if err != nil {
		return fmt.Errorf("expand config path %q: %w", path, err)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %q: %w", resolved, err)
	}

	cleaned := stripJSONComments(data)
	var fileCfg fileConfig
	if err := json.Unmarshal(cleaned, &fileCfg); err != nil {
		return fmt.Errorf("parse config %q: %w", resolved, err)
	}
	applyFileConfig(cfg, fileCfg)
	return nil
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Provider != nil {
		cfg.Provider = mergeProvider(cfg.Provider, *fc.Provider)
	}
	if fc.Runtime != nil {
		cfg.Runtime = mergeRuntime(cfg.Runtime, *fc.Runtime)
	}
	if fc.Safety != nil {
		cfg.Safety = mergeSafety(cfg.Safety, *fc.Safety)
	}
	if fc.Compaction != nil {
		if fc.Compaction.Auto != nil {
			cfg.Compaction.Auto = *fc.Compaction.Auto
		}
		if fc.Compaction.Prune != nil {
			cfg.Compaction.Prune = *fc.Compaction.Prune
		}
		if fc.Compaction.Threshold != nil {
			cfg.Compaction.Threshold = *fc.Compaction.Threshold
		}
		if fc.Compaction.RecentMessages != nil {
			cfg.Compaction.RecentMessages = *fc.Compaction.RecentMessages
		}
	}
	if fc.Workflow != nil {
		if fc.Workflow.RequireTodoForComplex != nil {
			cfg.Workflow.RequireTodoForComplex = *fc.Workflow.RequireTodoForComplex
		}
		if fc.Workflow.AutoVerifyAfterEdit != nil {
			cfg.Workflow.AutoVerifyAfterEdit = *fc.Workflow.AutoVerifyAfterEdit
		}
		if fc.Workflow.MaxVerifyAttempts != nil {
			cfg.Workflow.MaxVerifyAttempts = *fc.Workflow.MaxVerifyAttempts
		}
		if fc.Workflow.VerifyCommands != nil {
			cfg.Workflow.VerifyCommands = append([]string(nil), (*fc.Workflow.VerifyCommands)...)
		}
	}
	if fc.Approval != nil {
		if fc.Approval.AutoApproveAsk != nil {
			cfg.Approval.AutoApproveAsk = *fc.Approval.AutoApproveAsk
		}
		if fc.Approval.Interactive != nil {
			cfg.Approval.Interactive = *fc.Approval.Interactive
		}
	}
	if fc.Permission != nil {
		cfg.Permission = mergePermission(cfg.Permission, *fc.Permission)
	}
	if fc.Agent != nil {
		cfg.Agent = mergeAgents(cfg.Agent, *fc.Agent)
	}
	if fc.Agents != nil {
		cfg.Agents = mergeAgents(cfg.Agents, *fc.Agents)
	}
	if fc.Skills != nil {
		cfg.Skills = *fc.Skills
	}
	if fc.Instructions != nil {
		cfg.Instructions = append([]string(nil), (*fc.Instructions)...)
	}
	if fc.Storage != nil {
		cfg.Storage = mergeStorage(cfg.Storage, *fc.Storage)
	}
}

func mergeProvider(base ProviderConfig, override ProviderConfig) ProviderConfig {
	if strings.TrimSpace(override.BaseURL) != "" {
		base.BaseURL = override.BaseURL
	}
	if strings.TrimSpace(override.Model) != "" {
		base.Model = override.Model
	}
	if strings.TrimSpace(override.APIKey) != "" {
		base.APIKey = override.APIKey
	}
	if len(override.Models) > 0 {
		base.Models = append([]string(nil), override.Models...)
	}
	if override.TimeoutMS > 0 {
		base.TimeoutMS = override.TimeoutMS
	}
	return base
}

func mergeRuntime(base RuntimeConfig, override RuntimeConfig) RuntimeConfig {
	if strings.TrimSpace(override.WorkspaceRoot) != "" {
		base.WorkspaceRoot = override.WorkspaceRoot
	}
	if override.MaxSteps > 0 {
		base.MaxSteps = override.MaxSteps
	}
	if override.ContextTokenLimit > 0 {
		base.ContextTokenLimit = override.ContextTokenLimit
	}
	return base
}

func mergeSafety(base SafetyConfig, override SafetyConfig) SafetyConfig {
	if override.CommandTimeoutMS > 0 {
		base.CommandTimeoutMS = override.CommandTimeoutMS
	}
	if override.OutputLimitBytes > 0 {
		base.OutputLimitBytes = override.OutputLimitBytes
	}
	return base
}

func mergePermission(base PermissionConfig, override PermissionConfig) PermissionConfig {
	if strings.TrimSpace(override.DefaultWildcard) != "" {
		base.DefaultWildcard = override.DefaultWildcard
	}
	if strings.TrimSpace(override.Default) != "" {
		base.Default = override.Default
	}
	if strings.TrimSpace(override.Read) != "" {
		base.Read = override.Read
	}
	if strings.TrimSpace(override.Write) != "" {
		base.Write = override.Write
	}
	if strings.TrimSpace(override.List) != "" {
		base.List = override.List
	}
	if strings.TrimSpace(override.Glob) != "" {
		base.Glob = override.Glob
	}
	if strings.TrimSpace(override.Grep) != "" {
		base.Grep = override.Grep
	}
	if strings.TrimSpace(override.Patch) != "" {
		base.Patch = override.Patch
	}
	if strings.TrimSpace(override.TodoRead) != "" {
		base.TodoRead = override.TodoRead
	}
	if strings.TrimSpace(override.TodoWrite) != "" {
		base.TodoWrite = override.TodoWrite
	}
	if strings.TrimSpace(override.Skill) != "" {
		base.Skill = override.Skill
	}
	if strings.TrimSpace(override.Task) != "" {
		base.Task = override.Task
	}
	if strings.TrimSpace(override.ExternalDir) != "" {
		base.ExternalDir = override.ExternalDir
	}
	if len(override.InstructionFiles) > 0 {
		base.InstructionFiles = append([]string(nil), override.InstructionFiles...)
	}
	if len(override.Bash) > 0 {
		base.Bash = map[string]string{}
		for k, v := range override.Bash {
			base.Bash[k] = v
		}
	}
	if len(override.CommandAllowlist) > 0 {
		// 覆盖式赋值，按当前文件配置为准；归一化在 normalize 中处理。
		base.CommandAllowlist = append([]string(nil), override.CommandAllowlist...)
	}
	return base
}

func mergeAgents(base AgentConfig, override AgentConfig) AgentConfig {
	if strings.TrimSpace(override.Default) != "" {
		base.Default = override.Default
	}
	if len(override.Definitions) > 0 {
		base.Definitions = append([]AgentDefinition(nil), override.Definitions...)
	}
	return base
}

func mergeStorage(base StorageConfig, override StorageConfig) StorageConfig {
	if strings.TrimSpace(override.BaseDir) != "" {
		base.BaseDir = override.BaseDir
	}
	if override.LogMaxMB > 0 {
		base.LogMaxMB = override.LogMaxMB
	}
	if override.CacheTTLHours > 0 {
		base.CacheTTLHours = override.CacheTTLHours
	}
	return base
}

func normalize(cfg *Config) error {
	if cfg.Provider.BaseURL == "" {
		cfg.Provider.BaseURL = Default().Provider.BaseURL
	}
	if cfg.Provider.Model == "" {
		cfg.Provider.Model = Default().Provider.Model
	}
	if cfg.Provider.TimeoutMS <= 0 {
		cfg.Provider.TimeoutMS = Default().Provider.TimeoutMS
	}
	cfg.Provider.Models = normalizeModelList(cfg.Provider.Models)
	if len(cfg.Provider.Models) == 0 {
		cfg.Provider.Models = append(cfg.Provider.Models, cfg.Provider.Model)
	}
	if !containsString(cfg.Provider.Models, cfg.Provider.Model) {
		cfg.Provider.Models = append([]string{cfg.Provider.Model}, cfg.Provider.Models...)
		cfg.Provider.Models = normalizeModelList(cfg.Provider.Models)
	}

	if cfg.Runtime.MaxSteps <= 0 {
		cfg.Runtime.MaxSteps = Default().Runtime.MaxSteps
	}
	if cfg.Runtime.ContextTokenLimit <= 0 {
		cfg.Runtime.ContextTokenLimit = Default().Runtime.ContextTokenLimit
	}

	if cfg.Safety.CommandTimeoutMS <= 0 {
		cfg.Safety.CommandTimeoutMS = Default().Safety.CommandTimeoutMS
	}
	if cfg.Safety.OutputLimitBytes <= 0 {
		cfg.Safety.OutputLimitBytes = Default().Safety.OutputLimitBytes
	}

	if cfg.Compaction.Threshold <= 0 || cfg.Compaction.Threshold >= 1 {
		cfg.Compaction.Threshold = Default().Compaction.Threshold
	}
	if cfg.Compaction.RecentMessages <= 0 {
		cfg.Compaction.RecentMessages = Default().Compaction.RecentMessages
	}
	// Approval defaults
	if !cfg.Approval.Interactive && !cfg.Approval.AutoApproveAsk {
		// 若未显式配置，保持默认：交互式审批开启，auto_approve_ask 关闭。
		def := Default().Approval
		cfg.Approval = def
	}
	if cfg.Workflow.MaxVerifyAttempts <= 0 {
		cfg.Workflow.MaxVerifyAttempts = Default().Workflow.MaxVerifyAttempts
	}
	cfg.Workflow.VerifyCommands = normalizeCommandList(cfg.Workflow.VerifyCommands)

	if strings.TrimSpace(cfg.Permission.Default) == "" {
		cfg.Permission.Default = strings.TrimSpace(cfg.Permission.DefaultWildcard)
	}
	if strings.TrimSpace(cfg.Permission.Default) == "" {
		cfg.Permission.Default = "ask"
	}
	if len(cfg.Permission.Bash) == 0 {
		cfg.Permission.Bash = Default().Permission.Bash
	}
	if cfg.Agent.Default == "" {
		cfg.Agent.Default = "build"
	}
	if cfg.Agents.Default == "" {
		cfg.Agents.Default = cfg.Agent.Default
	}
	if len(cfg.Skills.Paths) == 0 {
		cfg.Skills.Paths = Default().Skills.Paths
	}

	storageDir, err := expandPath(cfg.Storage.BaseDir)
	if err != nil {
		return err
	}
	cfg.Storage.BaseDir = storageDir
	if cfg.Storage.LogMaxMB <= 0 {
		cfg.Storage.LogMaxMB = Default().Storage.LogMaxMB
	}
	if cfg.Storage.CacheTTLHours <= 0 {
		cfg.Storage.CacheTTLHours = Default().Storage.CacheTTLHours
	}

	cfg.Instructions = normalizePaths(cfg.Instructions)
	cfg.Permission.InstructionFiles = normalizePaths(cfg.Permission.InstructionFiles)
	cfg.Skills.Paths = normalizePaths(cfg.Skills.Paths)
	cfg.Runtime.WorkspaceRoot = strings.TrimSpace(cfg.Runtime.WorkspaceRoot)

	// 归一化 command_allowlist：按命令名小写存储，去重。
	if len(cfg.Permission.CommandAllowlist) > 0 {
		seen := map[string]struct{}{}
		norm := make([]string, 0, len(cfg.Permission.CommandAllowlist))
		for _, raw := range cfg.Permission.CommandAllowlist {
			name := NormalizeCommandName(raw)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			norm = append(norm, name)
		}
		cfg.Permission.CommandAllowlist = norm
	}

	return nil
}

func applyEnv(cfg Config) (Config, error) {
	if v := strings.TrimSpace(os.Getenv("AGENT_BASE_URL")); v != "" {
		cfg.Provider.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("AGENT_MODEL")); v != "" {
		cfg.Provider.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("AGENT_API_KEY")); v != "" {
		cfg.Provider.APIKey = v
	} else if v := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("AGENT_WORKSPACE_ROOT")); v != "" {
		cfg.Runtime.WorkspaceRoot = v
	}
	if v := strings.TrimSpace(os.Getenv("AGENT_MAX_STEPS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("invalid AGENT_MAX_STEPS: %q", v)
		}
		cfg.Runtime.MaxSteps = n
	}
	if v := strings.TrimSpace(os.Getenv("AGENT_CACHE_PATH")); v != "" {
		cfg.Storage.BaseDir = v
	}

	return cfg, normalize(&cfg)
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		expanded, err := expandPath(trimmed)
		if err != nil {
			continue
		}
		if _, ok := seen[expanded]; ok {
			continue
		}
		seen[expanded] = struct{}{}
		out = append(out, expanded)
	}
	return out
}

func normalizeCommandList(commands []string) []string {
	out := make([]string, 0, len(commands))
	for _, c := range commands {
		trimmed := strings.TrimSpace(c)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// NormalizeCommandName 归一化命令名：去掉前置环境变量，取命令基名并转为小写。
// NormalizeCommandName normalizes a shell command to its base name (lowercased), ignoring leading env assignments.
func NormalizeCommandName(command string) string {
	s := strings.TrimSpace(command)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		// 跳过形如 KEY=VAL 的前置环境变量（不含路径分隔符）。
		if strings.Contains(p, "=") && !strings.Contains(p, "/") {
			continue
		}
		name := p
		if strings.ContainsRune(name, '/') {
			name = filepath.Base(name)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		return name
	}
	return ""
}

func normalizeModelList(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, m := range models {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Abs(path)
}

func stripJSONComments(data []byte) []byte {
	const (
		stateNormal = iota
		stateString
		stateLineComment
		stateBlockComment
	)

	state := stateNormal
	escaped := false
	out := bytes.Buffer{}

	for i := 0; i < len(data); i++ {
		c := data[i]
		next := byte(0)
		if i+1 < len(data) {
			next = data[i+1]
		}

		switch state {
		case stateNormal:
			if c == '"' {
				state = stateString
				out.WriteByte(c)
				continue
			}
			if c == '/' && next == '/' {
				state = stateLineComment
				i++
				continue
			}
			if c == '/' && next == '*' {
				state = stateBlockComment
				i++
				continue
			}
			out.WriteByte(c)
		case stateString:
			out.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				state = stateNormal
			}
		case stateLineComment:
			if c == '\n' {
				state = stateNormal
				out.WriteByte(c)
			}
		case stateBlockComment:
			if c == '*' && next == '/' {
				state = stateNormal
				i++
			}
		}
	}

	return out.Bytes()
}

// WriteProviderModel 将 provider.model 写入项目配置（./.coder/config.json）；目录不存在则创建
// WriteProviderModel writes provider.model to project config (./.coder/config.json); creates dir if needed
func WriteProviderModel(projectDir, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("model is empty")
	}
	dir := filepath.Join(strings.TrimSpace(projectDir), ".coder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir .coder: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	var out map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &out); err != nil {
			out = nil
		}
	}
	if out == nil {
		out = make(map[string]any)
	}
	providerMap, _ := out["provider"].(map[string]any)
	if providerMap == nil {
		providerMap = make(map[string]any)
	}
	providerMap["model"] = model
	out["provider"] = providerMap
	data, err = json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteCommandAllowlist 追加命令名到项目级 allowlist（permission.command_allowlist），目录不存在则创建。
// WriteCommandAllowlist appends a command name to project-level permission.command_allowlist; creates .coder if needed.
func WriteCommandAllowlist(projectDir, commandName string) error {
	name := NormalizeCommandName(commandName)
	if name == "" {
		return errors.New("command name is empty")
	}
	dir := filepath.Join(strings.TrimSpace(projectDir), ".coder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir .coder: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	var root map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			root = nil
		}
	}
	if root == nil {
		root = make(map[string]any)
	}
	permAny, ok := root["permission"]
	var perm map[string]any
	if ok {
		if m, ok2 := permAny.(map[string]any); ok2 {
			perm = m
		}
	}
	if perm == nil {
		perm = make(map[string]any)
	}
	existingAny, _ := perm["command_allowlist"].([]any)
	seen := map[string]struct{}{}
	names := make([]string, 0, len(existingAny)+1)
	for _, v := range existingAny {
		s, ok := v.(string)
		if !ok {
			continue
		}
		n := strings.ToLower(strings.TrimSpace(s))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	if _, ok := seen[name]; !ok {
		names = append(names, name)
	}
	outArr := make([]any, 0, len(names))
	for _, n := range names {
		outArr = append(outArr, n)
	}
	perm["command_allowlist"] = outArr
	root["permission"] = perm
	data, err = json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
