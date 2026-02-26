package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"

	"coder/internal/agent"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/defaults"
	"coder/internal/orchestrator"
	"coder/internal/permission"
	"coder/internal/provider"
	"coder/internal/security"
	"coder/internal/skills"
	"coder/internal/storage"
)

// BuildResult 与 UI 无关的构建结果，供 main 构造 REPL
// BuildResult is UI-agnostic; main uses it to construct REPL
type BuildResult struct {
	Orch          *orchestrator.Orchestrator
	Store         storage.Store
	WorkspaceRoot string
	AgentName     string
	Model         string
	SessionID     string
	ToolNames     []string
	SkillNames    []string
}

// Build 按文档顺序初始化并返回 BuildResult；调用方负责 defer result.Store.Close()
// Build initializes in doc order and returns BuildResult; caller must defer result.Store.Close()
func Build(cfg config.Config, workspaceRoot string) (*BuildResult, error) {
	root, err := resolveWorkspaceRoot(cfg, workspaceRoot)
	if err != nil {
		return nil, err
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		return nil, fmt.Errorf("init workspace: %w", err)
	}

	dbPath := filepath.Join(cfg.Storage.BaseDir, "coder.db")
	sqliteStore, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}
	store := sqliteStore

	if migrated, migErr := storage.MigrateFromJSON(cfg.Storage.BaseDir, sqliteStore); migErr == nil && migrated > 0 {
		_ = migrated // optional: log "migrated N legacy sessions"
	}

	skillManager, err := skills.Discover(cfg.Skills.Paths)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}
	skills.MergeBuiltin(skillManager)

	lspManager := initLSPManager(cfg, ws)
	gitManager := initGitManager(ws)

	policy := permission.New(cfg.Permission)
	agentsCfg := config.MergeAgentConfig(cfg.Agent, cfg.Agents)
	activeProfile := agent.Resolve("", agentsCfg)

	instructionFiles := append([]string(nil), cfg.Instructions...)
	instructionFiles = append(instructionFiles, cfg.Permission.InstructionFiles...)
	assembler := contextmgr.New(defaults.DefaultSystemPrompt, ws.Root(), filepath.Join(cfg.Storage.BaseDir, "AGENTS.md"), instructionFiles)

	providerClient := provider.NewOpenAIProvider(provider.OpenAIConfig{
		BaseURL:    cfg.Provider.BaseURL,
		APIKey:     cfg.Provider.APIKey,
		Model:      cfg.Provider.Model,
		TimeoutMS:  cfg.Provider.TimeoutMS,
		MaxRetries: 3,
	})

	sessionMeta := storage.SessionMeta{
		ID:    storage.NewSessionID(),
		Agent: activeProfile.Name,
		Model: cfg.Provider.Model,
		CWD:   ws.Root(),
	}
	sessionMeta.Compaction.Auto = cfg.Compaction.Auto
	sessionMeta.Compaction.Prune = cfg.Compaction.Prune
	if err := store.CreateSession(sessionMeta); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sessionIDRef := &sessionMeta.ID

	registry, taskTool := buildToolRegistry(cfg, ws, store, sessionIDRef, skillManager, policy, lspManager, gitManager)
	approveFn := buildApprovalFunc(cfg, policy, ws.Root())

	toolNames := registry.Names()
	skillNames := collectSkillNames(skillManager)
	orch := orchestrator.New(providerClient, registry, orchestrator.Options{
		MaxSteps:          cfg.Runtime.MaxSteps,
		SystemPrompt:      defaults.DefaultSystemPrompt,
		OnApproval:        approveFn,
		Policy:            policy,
		Assembler:         assembler,
		Compaction:        cfg.Compaction,
		ContextTokenLimit: cfg.Runtime.ContextTokenLimit,
		ActiveAgent:       activeProfile,
		Agents:            agentsCfg,
		Workflow:          cfg.Workflow,
		WorkspaceRoot:     ws.Root(),
		SkillNames:        skillNames,
		Store:             store,
		SessionIDRef:      sessionIDRef,
		ConfigBasePath:    ws.Root(),
	})
	taskTool.SetRunner(func(ctx context.Context, agentName string, prompt string) (string, error) {
		return orch.RunSubtask(ctx, agentName, prompt)
	})

	return &BuildResult{
		Orch:          orch,
		Store:         store,
		WorkspaceRoot: ws.Root(),
		AgentName:     activeProfile.Name,
		Model:         cfg.Provider.Model,
		SessionID:     sessionMeta.ID,
		ToolNames:     toolNames,
		SkillNames:    skillNames,
	}, nil
}
