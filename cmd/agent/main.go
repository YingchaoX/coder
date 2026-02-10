package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coder/internal/agent"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/i18n"
	"coder/internal/mcp"
	"coder/internal/orchestrator"
	"coder/internal/permission"
	"coder/internal/provider"
	"coder/internal/security"
	"coder/internal/storage"
	"coder/internal/tools"
	"coder/internal/tui"
)

func main() {
	var (
		configPath string
		workspace  string
		locale     string
	)
	flag.StringVar(&configPath, "config", "", "Path to config JSON/JSONC")
	flag.StringVar(&workspace, "cwd", "", "Workspace root override")
	flag.StringVar(&locale, "lang", "", "UI language (en, zh-CN)")
	flag.Parse()

	// i18n 初始化 / Initialize i18n
	i18n.Init(locale)

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config failed: %v\n", err)
		os.Exit(1)
	}

	root := strings.TrimSpace(workspace)
	if root == "" {
		root = strings.TrimSpace(cfg.Runtime.WorkspaceRoot)
	}
	if root == "" {
		root, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolve cwd failed: %v\n", err)
			os.Exit(1)
		}
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init workspace failed: %v\n", err)
		os.Exit(1)
	}

	// SQLite 存储 / SQLite storage
	dbPath := filepath.Join(cfg.Storage.BaseDir, "coder.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init storage failed: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// 自动迁移旧 JSON 数据 / Auto-migrate legacy JSON data
	if migrated, migErr := storage.MigrateFromJSON(cfg.Storage.BaseDir, store); migErr == nil && migrated > 0 {
		fmt.Fprintf(os.Stderr, "migrated %d legacy sessions to SQLite\n", migrated)
	}

	skillManager, err := discoverSkills(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover skills failed: %v\n", err)
		os.Exit(1)
	}

	policy := permission.New(cfg.Permission)
	agentsCfg := mergeAgentConfig(cfg.Agent, cfg.Agents)
	activeProfile := agent.Resolve("", agentsCfg)
	mcpManager := mcp.NewManager(cfg.MCP)
	mcpManager.StartEnabled(context.Background())

	instructionFiles := append([]string(nil), cfg.Instructions...)
	instructionFiles = append(instructionFiles, cfg.Permission.InstructionFiles...)
	assembler := contextmgr.New(defaultSystemPrompt, ws.Root(), filepath.Join(cfg.Storage.BaseDir, "AGENTS.md"), instructionFiles)

	// v2: 使用新 Provider 接口 / Use new Provider interface
	providerClient := provider.NewOpenAIProvider(provider.OpenAIConfig{
		BaseURL:    cfg.Provider.BaseURL,
		APIKey:     cfg.Provider.APIKey,
		Model:      cfg.Provider.Model,
		TimeoutMS:  cfg.Provider.TimeoutMS,
		MaxRetries: 3,
	})

	// 创建 session / Create session
	sessionMeta := storage.SessionMeta{
		ID:    newSessionID(),
		Agent: activeProfile.Name,
		Model: cfg.Provider.Model,
		CWD:   ws.Root(),
	}
	sessionMeta.Compaction.Auto = cfg.Compaction.Auto
	sessionMeta.Compaction.Prune = cfg.Compaction.Prune
	if err := store.CreateSession(sessionMeta); err != nil {
		fmt.Fprintf(os.Stderr, "create session failed: %v\n", err)
		os.Exit(1)
	}

	// 构建工具注册表 / Build tool registry
	taskTool := tools.NewTaskTool(nil)
	skillTool := tools.NewSkillTool(skillManager, func(name string, action string) permission.Decision {
		return policy.SkillVisibilityDecision(name)
	})
	todoReadTool := tools.NewTodoReadTool(store, func() string { return sessionMeta.ID })
	todoWriteTool := tools.NewTodoWriteTool(store, func() string { return sessionMeta.ID })
	toolList := []tools.Tool{
		tools.NewReadTool(ws),
		tools.NewWriteTool(ws),
		tools.NewListTool(ws),
		tools.NewGlobTool(ws),
		tools.NewGrepTool(ws),
		tools.NewPatchTool(ws),
		tools.NewBashTool(ws.Root(), cfg.Safety.CommandTimeoutMS, cfg.Safety.OutputLimitBytes),
		todoReadTool,
		todoWriteTool,
		skillTool,
		taskTool,
	}
	for _, server := range mcpManager.Servers() {
		if server.Enabled() {
			toolList = append(toolList, tools.NewMCPProxyTool(server))
		}
	}
	registry := tools.NewRegistry(toolList...)

	// 简单审批回调：当前版本下统一自动允许策略为 ask 的操作（危险命令仍由 Policy 拒绝）
	// Simple approval callback: auto-allow operations marked as "ask" by policy; dangerous ones are still denied by Policy.
	approveFn := func(ctx context.Context, req tools.ApprovalRequest) (bool, error) {
		_ = ctx
		_ = req
		return true, nil
	}

	// 构建 orchestrator / Build orchestrator
	orch := orchestrator.New(providerClient, registry, orchestrator.Options{
		MaxSteps:          cfg.Runtime.MaxSteps,
		SystemPrompt:      defaultSystemPrompt,
		OnApproval:        approveFn,
		Policy:            policy,
		Assembler:         assembler,
		Compaction:        cfg.Compaction,
		ContextTokenLimit: cfg.Runtime.ContextTokenLimit,
		ActiveAgent:       activeProfile,
		Agents:            agentsCfg,
		Workflow:          cfg.Workflow,
		WorkspaceRoot:     ws.Root(),
	})
	taskTool.SetRunner(func(ctx context.Context, agentName string, prompt string) (string, error) {
		return orch.RunSubtask(ctx, agentName, prompt)
	})

	// 启动 Bubble Tea TUI，并将 orchestrator 注入 / Launch Bubble Tea TUI with orchestrator
	app := tui.NewApp(ws.Root(), activeProfile.Name, cfg.Provider.Model, sessionMeta.ID, orch)
	if err := tui.Run(app); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
