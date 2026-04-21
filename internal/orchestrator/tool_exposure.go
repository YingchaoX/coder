package orchestrator

import (
	"strings"

	"coder/internal/chat"
)

var minimalCoreTools = map[string]bool{
	"read":  true,
	"list":  true,
	"glob":  true,
	"grep":  true,
	"edit":  true,
	"write": true,
	"bash":  true,
}

func (o *Orchestrator) resolveToolDefsForInput(userInput string) []chat.ToolDef {
	if o == nil || o.registry == nil {
		return nil
	}

	enabled := make(map[string]bool, len(o.activeAgent.ToolEnabled))
	for name, allowed := range o.activeAgent.ToolEnabled {
		enabled[name] = allowed
	}

	for name := range enabled {
		if minimalCoreTools[name] {
			continue
		}
		enabled[name] = false
	}

	lower := strings.ToLower(strings.TrimSpace(userInput))

	if enabled["question"] {
		enabled["question"] = true
	}

	if wantsFetch(lower) && o.activeAgent.ToolEnabled["fetch"] {
		enabled["fetch"] = true
	}
	if wantsPatch(lower) && o.activeAgent.ToolEnabled["patch"] {
		enabled["patch"] = true
	}
	if wantsGit(lower) {
		for _, name := range []string{"git_status", "git_diff", "git_log", "git_add", "git_commit"} {
			if o.activeAgent.ToolEnabled[name] {
				enabled[name] = true
			}
		}
	}
	if wantsPDF(lower) && o.activeAgent.ToolEnabled["pdf_parser"] {
		enabled["pdf_parser"] = true
	}
	if wantsLSP(lower) {
		for _, name := range []string{"lsp_diagnostics", "lsp_definition", "lsp_hover"} {
			if o.activeAgent.ToolEnabled[name] {
				enabled[name] = true
			}
		}
	}
	if wantsTodo(lower) {
		if o.activeAgent.ToolEnabled["todoread"] {
			enabled["todoread"] = true
		}
		if o.activeAgent.ToolEnabled["todowrite"] {
			enabled["todowrite"] = true
		}
	}
	if wantsTask(lower) && o.activeAgent.ToolEnabled["task"] {
		enabled["task"] = true
	}
	if wantsSkill(lower) && o.activeAgent.ToolEnabled["skill"] {
		enabled["skill"] = true
	}

	defs := o.registry.DefinitionsFiltered(enabled)
	return o.filterToolDefsByPolicy(defs)
}

func wantsFetch(lower string) bool {
	if lower == "" {
		return false
	}
	markers := []string{
		"http://", "https://", "www.", "api", "endpoint", "swagger", "openapi",
		"docs", "documentation", "service", "intranet", "内网", "接口", "文档", "抓取",
	}
	return containsAny(lower, markers)
}

func wantsPatch(lower string) bool {
	return containsAny(lower, []string{"patch", "unified diff", "hunk", "补丁", "diff"})
}

func wantsGit(lower string) bool {
	return containsAny(lower, []string{"git", "commit", "stage", "stash", "branch", "rebase", "diff", "status"})
}

func wantsPDF(lower string) bool {
	return containsAny(lower, []string{".pdf", " pdf", "pdf ", "文档 pdf", "解析 pdf"})
}

func wantsLSP(lower string) bool {
	return containsAny(lower, []string{"definition", "hover", "diagnostic", "lsp", "定义", "诊断", "跳转"})
}

func wantsTodo(lower string) bool {
	return containsAny(lower, []string{"todo", "todos", "plan", "checklist", "步骤", "计划", "待办"})
}

func wantsTask(lower string) bool {
	return containsAny(lower, []string{"subtask", "sub-agent", "subagent", "delegate", "子任务", "子 agent", "子agent"})
}

func wantsSkill(lower string) bool {
	return containsAny(lower, []string{"skill", "skills", "workflow", "技能", "工作流"})
}

func containsAny(s string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
