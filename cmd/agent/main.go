package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"coder/internal/bootstrap"
	"coder/internal/config"
	"coder/internal/i18n"
	"coder/internal/repl"
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

	i18n.Init(locale)

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config failed: %v\n", err)
		os.Exit(1)
	}

	root, err := resolveWorkspaceRoot(workspace, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve cwd failed: %v\n", err)
		os.Exit(1)
	}

	res, err := bootstrap.Build(cfg, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	defer res.Store.Close()

	loop := repl.NewLoop(res)
	if err := repl.Run(loop); err != nil {
		fmt.Fprintf(os.Stderr, "REPL error: %v\n", err)
		os.Exit(1)
	}
}

// resolveWorkspaceRoot 解析工作区根路径（供 main 与测试使用）
// resolveWorkspaceRoot resolves workspace root (for main and tests)
func resolveWorkspaceRoot(override string, cfg config.Config) (string, error) {
	root := strings.TrimSpace(override)
	if root == "" {
		root = strings.TrimSpace(cfg.Runtime.WorkspaceRoot)
	}
	if root == "" {
		return os.Getwd()
	}
	return root, nil
}
