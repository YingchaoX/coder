package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitProjectConfigScaffold 在当前工作目录下初始化项目级配置模板（./.coder/config.json）。
// InitProjectConfigScaffold initializes a project-level config scaffold (./.coder/config.json) in the current working directory.
func InitProjectConfigScaffold() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current working directory: %w", err)
	}

	dir := filepath.Join(cwd, ".coder")
	path := filepath.Join(dir, "config.json")

	// 若项目已经有 ./.coder/config.json，则尊重用户现有配置。
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("project config path is a directory: %s", path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat project config: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir .coder: %w", err)
	}

	cfg := Default()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal default config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write project config: %w", err)
	}

	return nil
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
