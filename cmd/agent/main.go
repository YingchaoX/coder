package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"coder/internal/agent"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/mcp"
	"coder/internal/orchestrator"
	"coder/internal/permission"
	"coder/internal/provider"
	"coder/internal/security"
	"coder/internal/skills"
	"coder/internal/storage"
	"coder/internal/tools"

	"github.com/chzyer/readline"
)

func main() {
	var (
		configPath string
		workspace  string
	)
	flag.StringVar(&configPath, "config", "", "Path to config JSON/JSONC")
	flag.StringVar(&workspace, "cwd", "", "Workspace root override")
	flag.Parse()

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

	store, err := storage.NewManager(cfg.Storage.BaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init storage failed: %v\n", err)
		os.Exit(1)
	}

	skillManager, err := skills.Discover(cfg.Skills.Paths)
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

	var currentSessionID string
	taskTool := tools.NewTaskTool(nil)
	skillTool := tools.NewSkillTool(skillManager, func(name string, action string) permission.Decision {
		return policy.SkillVisibilityDecision(name)
	})
	todoReadTool := tools.NewTodoReadTool(store, func() string { return currentSessionID })
	todoWriteTool := tools.NewTodoWriteTool(store, func() string { return currentSessionID })
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
		if !serverCfgEnabled(server) {
			continue
		}
		toolList = append(toolList, tools.NewMCPProxyTool(server))
	}
	registry := tools.NewRegistry(toolList...)

	providerClient := provider.NewClient(cfg.Provider)
	inputReader, inputErr := newLineInput(filepath.Join(cfg.Storage.BaseDir, "repl.history"))
	if inputErr != nil {
		fmt.Fprintf(os.Stderr, "line editor unavailable, fallback to basic input: %v\n", inputErr)
	}
	defer inputReader.Close()

	orch := orchestrator.New(providerClient, registry, orchestrator.Options{
		MaxSteps:          cfg.Runtime.MaxSteps,
		SystemPrompt:      defaultSystemPrompt,
		OnApproval:        approvalPrompt(inputReader, ws),
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

	currentMeta, _, err := store.CreateSession(activeProfile.Name, cfg.Provider.Model, ws.Root(), cfg.Compaction.Auto, cfg.Compaction.Prune)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create session failed: %v\n", err)
		os.Exit(1)
	}
	currentSessionID = currentMeta.ID

	fmt.Printf("offline-agent started in workspace: %s\n", ws.Root())
	fmt.Printf("session: %s agent=%s\n", currentMeta.ID, activeProfile.Name)
	availableModels := normalizedModels(cfg.Provider.Models, orch.CurrentModel())
	printREPLCommands(os.Stdout)

	saveCurrent := func() {
		currentMeta.Agent = orch.ActiveAgent().Name
		currentMeta.Model = orch.CurrentModel()
		if err := store.Save(currentMeta, orch.Messages()); err != nil {
			fmt.Fprintf(os.Stderr, "save session failed: %v\n", err)
		}
	}

	for {
		line, err := inputReader.ReadLine("> ")
		if err != nil {
			switch {
			case errors.Is(err, readline.ErrInterrupt):
				fmt.Fprintln(os.Stdout)
				continue
			case errors.Is(err, io.EOF):
				fmt.Fprintln(os.Stderr, "\nexit")
				return
			default:
				fmt.Fprintf(os.Stderr, "read input failed: %v\n", err)
				return
			}
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			if handled, shouldExit := handleCommand(input, cfg, ws.Root(), store, skillManager, mcpManager, orch, registry, &currentMeta, &availableModels); handled {
				if shouldExit {
					saveCurrent()
					return
				}
				currentSessionID = currentMeta.ID
				saveCurrent()
				continue
			}
		}

		resolvedInput := expandFileMentions(input, ws)
		if _, err := orch.RunInput(context.Background(), resolvedInput, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "turn failed: %v\n", err)
		}
		saveCurrent()
	}
}
