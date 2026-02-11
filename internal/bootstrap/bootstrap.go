package bootstrap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

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
	"coder/internal/tools"
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
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = strings.TrimSpace(cfg.Runtime.WorkspaceRoot)
	}
	if root == "" {
		return nil, fmt.Errorf("workspace root is empty")
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

	taskTool := tools.NewTaskTool(nil)
	skillTool := tools.NewSkillTool(skillManager, func(name string, action string) permission.Decision {
		return policy.SkillVisibilityDecision(name)
	})
	todoReadTool := tools.NewTodoReadTool(store, func() string { return *sessionIDRef })
	todoWriteTool := tools.NewTodoWriteTool(store, func() string { return *sessionIDRef })
	toolList := []tools.Tool{
		tools.NewReadTool(ws),
		tools.NewWriteTool(ws),
		tools.NewEditTool(ws),
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
	registry := tools.NewRegistry(toolList...)

	approveFn := func(ctx context.Context, req tools.ApprovalRequest) (bool, error) {
		_ = ctx

		isTTY := term.IsTerminal(int(os.Stdin.Fd()))
		isBash := strings.EqualFold(strings.TrimSpace(req.Tool), "bash")
		reason := strings.TrimSpace(req.Reason)

		// 非交互环境：为安全起见，继续拒绝执行，避免静默放行破坏性操作。
		if !isTTY {
			return false, nil
		}

		// 解析 bash 命令文本（仅在需要展示或加入 allowlist 时使用）。
		reader := bufio.NewReader(os.Stdin)
		bashCommand := ""
		if isBash && strings.TrimSpace(req.RawArgs) != "" {
			var in struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal([]byte(req.RawArgs), &in); err == nil {
				bashCommand = strings.TrimSpace(in.Command)
			}
		}

		// 区分策略层 ask 与工具层危险命令风险审批。
		isPolicyAsk := strings.Contains(reason, "policy requires approval")
		isDangerous := strings.Contains(reason, "dangerous") ||
			strings.Contains(reason, "overwrite") ||
			strings.Contains(reason, "substitution") ||
			strings.Contains(reason, "parse failed") ||
			strings.Contains(reason, "matches dangerous command policy")

		// 非交互模式配置：策略层 ask 可自动放行；危险命令仍需显式 y/n。
		if !cfg.Approval.Interactive && !isDangerous {
			// 仅策略层 ask 走 auto_approve_ask；危险命令一律不在此路径放行。
			if cfg.Approval.AutoApproveAsk || isPolicyAsk {
				return true, nil
			}
		}

		// 交互式审批：策略层 ask 支持 y/n/always；危险命令仅 y/n。
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintf(os.Stdout, "[approval required] tool=%s reason=%s\n", req.Tool, req.Reason)
		if isBash && bashCommand != "" {
			_, _ = fmt.Fprintf(os.Stdout, "[command] %s\n", bashCommand)
		}

		if isDangerous {
			// 危险命令风险审批：始终仅 y/n。
			_, _ = fmt.Fprint(os.Stdout, "允许执行？(y/N): ")
			line, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(line))
			if ans != "y" && ans != "yes" {
				return false, nil
			}
			return true, nil
		}

		// 策略层 ask：支持 y/n/always。
		_, _ = fmt.Fprint(os.Stdout, "允许执行？(y/N/always): ")
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		switch ans {
		case "y", "yes":
			return true, nil
		case "always", "a":
			// 仅针对 bash 记录 allowlist；按命令名归一化。
			if isBash && bashCommand != "" {
				name := config.NormalizeCommandName(bashCommand)
				if name != "" {
					if policy.AddToCommandAllowlist(name) {
						// best-effort 持久化到项目配置；失败不影响本次放行。
						_ = config.WriteCommandAllowlist(ws.Root(), name)
					}
				}
			}
			return true, nil
		default:
			return false, nil
		}
	}

	toolNames := registry.Names()
	skillNames := make([]string, 0, len(skillManager.List()))
	for _, info := range skillManager.List() {
		skillNames = append(skillNames, info.Name)
	}
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
