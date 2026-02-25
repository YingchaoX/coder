package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"coder/internal/chat"
)

func (o *Orchestrator) resolveMaxSteps() int {
	if o.activeAgent.MaxSteps > 0 {
		return o.activeAgent.MaxSteps
	}
	if o.maxSteps <= 0 {
		return 128
	}
	return o.maxSteps
}

func isContextCancellationErr(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx != nil && ctx.Err() != nil
}

func contextErrOr(ctx context.Context, fallback error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return fallback
}

func (o *Orchestrator) appendSyntheticToolExchange(toolName, args, result, callID string) {
	if strings.TrimSpace(toolName) == "" || strings.TrimSpace(callID) == "" {
		return
	}
	o.appendMessage(chat.Message{
		Role: "assistant",
		ToolCalls: []chat.ToolCall{
			{
				ID:   callID,
				Type: "function",
				Function: chat.ToolCallFunction{
					Name:      toolName,
					Arguments: args,
				},
			},
		},
	})
	o.appendMessage(chat.Message{
		Role:       "tool",
		Name:       toolName,
		ToolCallID: callID,
		Content:    result,
	})
}

func (o *Orchestrator) appendToolDenied(call chat.ToolCall, reason string) {
	o.appendMessage(chat.Message{
		Role:       "tool",
		Name:       call.Function.Name,
		ToolCallID: call.ID,
		Content: mustJSON(map[string]any{
			"ok":     false,
			"denied": true,
			"reason": reason,
		}),
	})
}

func (o *Orchestrator) appendToolError(call chat.ToolCall, err error) {
	o.appendMessage(chat.Message{
		Role:       "tool",
		Name:       call.Function.Name,
		ToolCallID: call.ID,
		Content: mustJSON(map[string]any{
			"ok":    false,
			"error": err.Error(),
		}),
	})
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"marshal tool result failed"}`
	}
	return string(data)
}

func summarizeForLog(s string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if normalized == "" {
		return "-"
	}
	const maxRunes = 220
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return string(runes[:maxRunes]) + "...(truncated)"
}

func formatToolStart(name string, rawArgs string) string {
	args := parseJSONObject(rawArgs)
	switch name {
	case "read":
		path := getString(args, "path", "")
		offset := getInt(args, "offset", 0)
		limit := getInt(args, "limit", 0)
		// 仅当调用方显式传入 offset/limit 时，在起始行中展示行区间。
		// Only show line range when caller explicitly provides offset/limit.
		if offset > 0 && limit > 0 {
			end := offset + limit - 1
			return fmt.Sprintf("* Read %s[%d-%d]", quoteOrDash(path), offset, end)
		}
		return fmt.Sprintf("* Read %s", quoteOrDash(path))
	case "list":
		path := getString(args, "path", ".")
		return fmt.Sprintf("* List %s", quoteOrDash(path))
	case "glob":
		pattern := getString(args, "pattern", "")
		return fmt.Sprintf("* Glob %s", quoteOrDash(pattern))
	case "grep":
		pattern := getString(args, "pattern", "")
		path := getString(args, "path", ".")
		return fmt.Sprintf("* Grep %s in %s", quoteOrDash(pattern), quoteOrDash(path))
	case "write":
		path := getString(args, "path", "")
		content := getString(args, "content", "")
		return fmt.Sprintf("* Write %s (%d bytes)", quoteOrDash(path), len(content))
	case "edit":
		path := getString(args, "path", "")
		return fmt.Sprintf("* Edit %s", quoteOrDash(path))
	case "patch":
		return "* Apply patch"
	case "todoread":
		return "* Read todo list"
	case "todowrite":
		return "* Update todo list"
	case "skill":
		action := getString(args, "action", "")
		nameArg := getString(args, "name", "")
		if nameArg == "" {
			return fmt.Sprintf("* Skill %s", quoteOrDash(action))
		}
		return fmt.Sprintf("* Skill %s %s", quoteOrDash(action), quoteOrDash(nameArg))
	case "task":
		agentName := getString(args, "agent", "")
		objective := getString(args, "objective", "")
		if objective == "" {
			objective = getString(args, "prompt", "")
		}
		return fmt.Sprintf("* Task %s: %s", quoteOrDash(agentName), quoteOrDash(short(objective, 80)))
	case "bash":
		cmd := getString(args, "command", "")
		return fmt.Sprintf("* Bash %s", quoteOrDash(cmd))
	default:
		return fmt.Sprintf("* %s args=%s", title(name), summarizeForLog(rawArgs))
	}
}

func summarizeToolResult(name string, rawResult string) string {
	result := parseJSONObject(rawResult)
	if len(result) == 0 {
		return summarizeForLog(rawResult)
	}
	switch name {
	case "read":
		path := getString(result, "path", "")
		content := getString(result, "content", "")
		start := getInt(result, "start_line", 0)
		end := getInt(result, "end_line", 0)
		hasMore := false
		if v, ok := result["has_more"].(bool); ok {
			hasMore = v
		}
		base := fmt.Sprintf("read %d bytes from %s", len(content), quoteOrDash(path))
		if start > 0 && end >= start {
			if hasMore {
				return fmt.Sprintf("%s [%d-%d] (more lines)", base, start, end)
			}
			return fmt.Sprintf("%s [%d-%d]", base, start, end)
		}
		return base
	case "list":
		path := getString(result, "path", "")
		return fmt.Sprintf("%d entries in %s", len(getArray(result, "items")), quoteOrDash(path))
	case "glob":
		return fmt.Sprintf("%d matches", len(getArray(result, "matches")))
	case "grep":
		count := getInt(result, "count", len(getArray(result, "matches")))
		return fmt.Sprintf("%d matches", count)
	case "write":
		path := getString(result, "path", "")
		size := getInt(result, "size", 0)
		operation := strings.ToLower(strings.TrimSpace(getString(result, "operation", "")))
		additions := getInt(result, "additions", 0)
		deletions := getInt(result, "deletions", 0)
		diff := strings.TrimSpace(getString(result, "diff", ""))
		line := fmt.Sprintf("wrote %d bytes to %s", size, quoteOrDash(path))
		switch operation {
		case "created":
			line = fmt.Sprintf("created %s (+%d lines, %d bytes)", quoteOrDash(path), additions, size)
		case "updated":
			line = fmt.Sprintf("updated %s (+%d -%d lines, %d bytes)", quoteOrDash(path), additions, deletions, size)
		case "unchanged":
			line = fmt.Sprintf("no-op write to %s (%d bytes)", quoteOrDash(path), size)
		}
		if diff != "" {
			return line + "\n" + diff
		}
		return line
	case "edit":
		path := getString(result, "path", "")
		size := getInt(result, "size", 0)
		operation := strings.ToLower(strings.TrimSpace(getString(result, "operation", "")))
		additions := getInt(result, "additions", 0)
		deletions := getInt(result, "deletions", 0)
		diff := strings.TrimSpace(getString(result, "diff", ""))
		replacements := getInt(result, "replacements", 0)
		line := fmt.Sprintf("edited %s (%d bytes, %d replacement(s))", quoteOrDash(path), size, replacements)
		switch operation {
		case "updated":
			line = fmt.Sprintf("updated %s (+%d -%d lines, %d bytes, %d replacement(s))", quoteOrDash(path), additions, deletions, size, replacements)
		case "unchanged":
			line = fmt.Sprintf("no-op edit to %s (%d bytes, %d replacement(s))", quoteOrDash(path), size, replacements)
		}
		if diff != "" {
			return line + "\n" + diff
		}
		return line
	case "patch":
		return fmt.Sprintf("patched %d file(s)", getInt(result, "applied", 0))
	case "todoread":
		return formatTodoSummary(result, "todo")
	case "todowrite":
		return formatTodoSummary(result, "todo updated")
	case "skill":
		if content := getString(result, "content", ""); content != "" {
			return fmt.Sprintf("loaded skill (%d bytes)", len(content))
		}
		return fmt.Sprintf("%d skills", getInt(result, "count", len(getArray(result, "items"))))
	case "task":
		return summarizeForLog(getString(result, "summary", "task finished"))
	case "bash":
		exitCode := getInt(result, "exit_code", -1)
		duration := getInt(result, "duration_ms", 0)
		stdout := strings.TrimSpace(getString(result, "stdout", ""))
		stderr := strings.TrimSpace(getString(result, "stderr", ""))
		if exitCode == 0 {
			if stdout != "" {
				return fmt.Sprintf("exit=0 in %dms, stdout=%s", duration, summarizeForLog(firstLine(stdout)))
			}
			return fmt.Sprintf("exit=0 in %dms", duration)
		}
		if stderr != "" {
			return fmt.Sprintf("exit=%d in %dms, stderr=%s", exitCode, duration, summarizeForLog(firstLine(stderr)))
		}
		return fmt.Sprintf("exit=%d in %dms", exitCode, duration)
	default:
		if errText := getString(result, "error", ""); errText != "" {
			return summarizeForLog(errText)
		}
		return summarizeForLog(rawResult)
	}
}

// todoItemsFromResult 从 todoread/todowrite 的 JSON result 解析出展示用 []string（TUI 侧栏或 REPL /todos）
// todoItemsFromResult parses todoread/todowrite JSON result into display lines (TUI sidebar or REPL /todos)
func todoItemsFromResult(rawResult string) []string {
	result := parseJSONObject(rawResult)
	if result == nil {
		return nil
	}
	items := getArray(result, "items")
	if len(items) == 0 {
		return nil
	}
	var out []string
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content := strings.TrimSpace(getString(item, "content", ""))
		if content == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s", todoStatusMarker(getString(item, "status", "")), content))
	}
	return out
}

func formatTodoSummary(result map[string]any, label string) string {
	items := getArray(result, "items")
	headline := fmt.Sprintf("%s items=%d", label, getInt(result, "count", len(items)))
	if len(items) == 0 {
		return headline
	}
	lines := []string{headline}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content := strings.TrimSpace(getString(item, "content", ""))
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", todoStatusMarker(getString(item, "status", "")), content))
	}
	if len(lines) == 1 {
		return headline
	}
	return strings.Join(lines, "\n")
}

func parseJSONObject(s string) map[string]any {
	var out map[string]any
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func getString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return fallback
		}
		return val
	default:
		return fallback
	}
}

func getArray(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	out, ok := v.([]any)
	if !ok {
		return nil
	}
	return out
}

func getInt(m map[string]any, key string, fallback int) int {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case string:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

func firstLine(s string) string {
	parts := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return ""
}

func quoteOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strconv.Quote(summarizeForLog(s))
}

func title(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Tool"
	}
	runes := []rune(s)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func short(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}

func todoStatusMarker(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	default:
		return "[ ]"
	}
}

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func isComplexTask(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}
	if len([]rune(trimmed)) >= 80 {
		return true
	}
	keywords := []string{
		"并", "然后", "同时", "步骤", "重构", "实现", "修复", "优化",
		"and then", "step by step", "refactor", "implement", "fix",
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	delimiters := strings.Count(trimmed, "，") + strings.Count(trimmed, ",") + strings.Count(trimmed, ";") + strings.Count(trimmed, "；")
	if delimiters >= 2 {
		return true
	}
	return len(strings.Fields(trimmed)) >= 14
}

func shouldRequireTodoInPlan(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}
	if isChattyGreeting(trimmed) {
		return false
	}
	if isComplexTask(trimmed) {
		return true
	}
	if isEnvironmentSetupTask(trimmed) {
		return true
	}
	return containsAnyFold(trimmed, []string{
		"todo", "todos", "plan", "roadmap", "steps",
		"计划", "规划", "步骤", "方案", "拆解",
	})
}

func isEnvironmentSetupTask(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}
	return containsAnyFold(trimmed, []string{
		"install", "uninstall", "setup", "set up", "configure", "configuration",
		"brew", "apt", "yum", "dnf", "pacman", "pip", "conda", "pyenv", "nvm", "asdf",
		"安装", "卸载", "配置", "环境", "依赖",
	})
}

func isInfoGatheringTool(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch name {
	case "read", "list", "glob", "grep", "bash", "fetch",
		"lsp_diagnostics", "lsp_definition", "lsp_hover",
		"git_status", "git_diff", "git_log", "pdf_parser":
		return true
	default:
		return false
	}
}

// isChattyGreeting 判断输入是否是闲聊/简单问候，不需要使用工具
// 泛化性判断：短文本（<30字符）、仅包含问候/寒暄/简单问好的模式、没有具体任务指令
func isChattyGreeting(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return false
	}

	// 长度检查：超过50字符的通常不是闲聊
	runes := []rune(trimmed)
	if len(runes) > 50 {
		return false
	}

	// 包含指令性关键词的，不是闲聊
	instructionKeywords := []string{
		"修改", "添加", "删除", "修复", "优化", "重构", "实现", "创建",
		"modify", "add", "delete", "fix", "optimize", "refactor", "implement", "create",
		"改", "写", "查", "看", "读", "run", "execute", "build", "test",
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range instructionKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}

	// 包含任务标点（冒号、问号+请求语气）的不是闲聊
	if strings.Contains(trimmed, ":") || strings.Contains(trimmed, "：") {
		return false
	}

	// 问候模式匹配（多语言支持）
	greetingPatterns := []string{
		"你好", "您好", "hello", "hi", "hey", "yo",
		"在吗", "在？", "在?", "在么", "在不",
		"早上好", "下午好", "晚上好", "good morning", "good afternoon", "good evening",
		"g'day", "hola", "bonjour", "ciao", "olá", "hej", "hallo", "szia", "привет",
		"谢谢", "多谢", "thanks", "thank you", "thx", "谢了",
		"再见", "拜拜", "bye", "goodbye", "see you", "cya",
		"睡", "吃", "天气", "time", "时间", "几点", "date", "日期",
		"怎么样", "好吗", "ok", "okay", "好", "行", "可以",
	}

	for _, pattern := range greetingPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			// 匹配到问候词，且问候词占总内容的大部分，判定为闲聊
			// 如果问候后还有大量非问候内容（>20字符），则不是闲聊
			patternIdx := strings.Index(lower, strings.ToLower(pattern))
			afterGreeting := strings.TrimSpace(lower[patternIdx+len(pattern):])
			if len([]rune(afterGreeting)) < 20 {
				return true
			}
		}
	}

	// 纯感叹词/简单短句（1-3个词）
	words := strings.Fields(trimmed)
	if len(words) <= 3 && len(runes) < 20 {
		// 检查是否主要是问候语气
		for _, w := range words {
			wLower := strings.ToLower(w)
			for _, p := range greetingPatterns {
				if strings.Contains(p, wLower) || strings.Contains(wLower, p) {
					return true
				}
			}
		}
	}

	return false
}
