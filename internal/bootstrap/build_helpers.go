package bootstrap

import (
	"fmt"
	"os"
	"strings"

	"coder/internal/config"
	"coder/internal/lsp"
	"coder/internal/permission"
	"coder/internal/security"
	"coder/internal/skills"
	"coder/internal/storage"
	"coder/internal/tools"
)

func resolveWorkspaceRoot(cfg config.Config, workspaceRoot string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = strings.TrimSpace(cfg.Runtime.WorkspaceRoot)
	}
	if root == "" {
		return "", fmt.Errorf("workspace root is empty")
	}
	return root, nil
}

func initLSPManager(cfg config.Config, ws *security.Workspace) *lsp.Manager {
	lspManager := lsp.NewManager(cfg.LSP, ws.Root())
	if len(lspManager.DetectServers()) == 0 {
		return lspManager
	}
	fmt.Fprintln(os.Stderr, "[LSP] Some language servers are not installed:")
	for _, info := range lspManager.GetMissingServers() {
		fmt.Fprintf(os.Stderr, "[LSP]   %s (%s): %s\n", info.Lang, info.Command, info.InstallHint)
	}
	fmt.Fprintln(os.Stderr, "[LSP] LSP tools will be disabled for these languages. Install the servers to enable LSP features.")
	return lspManager
}

func initGitManager(ws *security.Workspace) *tools.GitManager {
	gitManager := tools.NewGitManager(ws)
	if available, isRepo, version := gitManager.Check(); !available {
		fmt.Fprintln(os.Stderr, "[Git] Git is not installed.")
		fmt.Fprintln(os.Stderr, "[Git] Git tools will be disabled. Install git to enable git features.")
	} else if !isRepo {
		fmt.Fprintln(os.Stderr, "[Git] Current directory is not a git repository.")
		fmt.Fprintln(os.Stderr, "[Git] Git tools will work in degraded mode. Initialize git to enable full features.")
	} else {
		fmt.Fprintf(os.Stderr, "[Git] Git detected: %s\n", version)
	}
	return gitManager
}

func buildToolRegistry(
	cfg config.Config,
	ws *security.Workspace,
	store storage.Store,
	sessionIDRef *string,
	skillManager *skills.Manager,
	policy *permission.Policy,
	lspManager *lsp.Manager,
	gitManager *tools.GitManager,
) (*tools.Registry, *tools.TaskTool) {
	taskTool := tools.NewTaskTool(nil)
	skillTool := tools.NewSkillTool(skillManager, func(name string, _ string) permission.Decision {
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
		tools.NewLSPDiagnosticsTool(lspManager),
		tools.NewLSPDefinitionTool(lspManager),
		tools.NewLSPHoverTool(lspManager),
		tools.NewGitStatusTool(ws, gitManager),
		tools.NewGitDiffTool(ws, gitManager),
		tools.NewGitLogTool(ws, gitManager),
		tools.NewGitAddTool(ws, gitManager),
		tools.NewGitCommitTool(ws, gitManager),
		tools.NewFetchTool(ws, tools.FetchConfig{
			TimeoutSec:     cfg.Fetch.TimeoutMS / 1000,
			MaxTextSizeKB:  cfg.Fetch.MaxTextSizeKB,
			MaxImageSizeMB: cfg.Fetch.MaxImageSizeMB,
			SkipTLSVerify:  cfg.Fetch.SkipTLSVerify,
			DefaultHeaders: cfg.Fetch.DefaultHeaders,
		}),
		tools.NewPDFParserTool(ws),
		tools.NewQuestionTool(),
	}

	return tools.NewRegistry(toolList...), taskTool
}

func collectSkillNames(skillManager *skills.Manager) []string {
	skillInfos := skillManager.List()
	skillNames := make([]string, 0, len(skillInfos))
	for _, info := range skillInfos {
		skillNames = append(skillNames, info.Name)
	}
	return skillNames
}
