package contextmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"coder/internal/chat"
)

type Assembler struct {
	SystemPrompt      string
	WorkspaceRoot     string
	GlobalRulesPath   string
	InstructionFiles  []string
	ToolOutputMaxRune int
}

func New(systemPrompt, workspaceRoot, globalRulesPath string, instructionFiles []string) *Assembler {
	return &Assembler{
		SystemPrompt:      strings.TrimSpace(systemPrompt),
		WorkspaceRoot:     strings.TrimSpace(workspaceRoot),
		GlobalRulesPath:   strings.TrimSpace(globalRulesPath),
		InstructionFiles:  append([]string(nil), instructionFiles...),
		ToolOutputMaxRune: 4000,
	}
}

func (a *Assembler) StaticMessages() []chat.Message {
	out := []chat.Message{}
	if a.SystemPrompt != "" {
		out = append(out, chat.Message{Role: "system", Content: a.SystemPrompt})
	}

	projectRules := filepath.Join(a.WorkspaceRoot, "AGENTS.md")
	if content, ok := readFile(projectRules, 32768); ok {
		out = append(out, chat.Message{Role: "system", Content: "[PROJECT_RULES]\n" + content})
	}
	if content, ok := readFile(a.GlobalRulesPath, 32768); ok {
		out = append(out, chat.Message{Role: "system", Content: "[GLOBAL_RULES]\n" + content})
	}
	for _, path := range a.InstructionFiles {
		if content, ok := readFile(path, 32768); ok {
			out = append(out, chat.Message{Role: "system", Content: fmt.Sprintf("[INSTRUCTION:%s]\n%s", filepath.Base(path), content)})
		}
	}
	return out
}

func readFile(path string, maxBytes int) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	content := string(data)
	runes := []rune(content)
	if len(runes) > maxBytes {
		content = string(runes[:maxBytes]) + "\n...[truncated]"
	}
	return content, true
}

func EstimateTokens(messages []chat.Message) int {
	total := 0
	for _, m := range messages {
		total += len([]rune(m.Content))/4 + 4
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				total += len([]rune(tc.Function.Name))/4 + len([]rune(tc.Function.Arguments))/4 + 8
			}
		}
	}
	if total < len(messages)*4 {
		total = len(messages) * 4
	}
	return total
}

func Compact(messages []chat.Message, keepRecent int, pruneToolOutputs bool) ([]chat.Message, string, bool) {
	if keepRecent < 4 {
		keepRecent = 4
	}
	if len(messages) <= keepRecent+2 {
		return messages, "", false
	}

	msgs := append([]chat.Message(nil), messages...)
	if pruneToolOutputs {
		for i := range msgs {
			if msgs[i].Role != "tool" {
				continue
			}
			msgs[i].Content = pruneToolOutput(msgs[i].Content)
		}
	}

	split := len(msgs) - keepRecent
	if split < 1 {
		split = 1
	}
	head := msgs[:split]
	tail := msgs[split:]
	summary := summarizeMessages(head)
	if strings.TrimSpace(summary) == "" {
		return msgs, "", false
	}

	compacted := make([]chat.Message, 0, len(tail)+1)
	compacted = append(compacted, chat.Message{
		Role:    "assistant",
		Content: "[COMPACTION_SUMMARY]\n" + summary,
	})
	compacted = append(compacted, tail...)
	return compacted, summary, true
}

func pruneToolOutput(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		r := []rune(raw)
		if len(r) <= 2000 {
			return raw
		}
		return string(r[:2000]) + "...(truncated)"
	}
	for _, key := range []string{"content", "stdout", "stderr"} {
		v, ok := obj[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		r := []rune(s)
		if len(r) > 1200 {
			obj[key] = string(r[:1200]) + "...(truncated)"
		}
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return string(data)
}

func summarizeMessages(msgs []chat.Message) string {
	objective := ""
	files := map[string]struct{}{}
	risks := map[string]struct{}{}
	steps := []string{}

	pathPattern := regexp.MustCompile(`([A-Za-z0-9_./-]+\.[A-Za-z0-9_]+)`)
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if objective == "" {
				objective = strings.TrimSpace(m.Content)
			}
			steps = append(steps, short(m.Content, 140))
		case "assistant":
			if strings.Contains(strings.ToLower(m.Content), "next") {
				steps = append(steps, short(m.Content, 140))
			}
		case "tool":
			if strings.Contains(strings.ToLower(m.Content), "denied") || strings.Contains(strings.ToLower(m.Content), "error") {
				risks[short(m.Content, 120)] = struct{}{}
			}
			for _, hit := range pathPattern.FindAllString(m.Content, -1) {
				if strings.Contains(hit, " ") {
					continue
				}
				files[hit] = struct{}{}
			}
		}
	}
	if objective == "" {
		objective = "continue current task"
	}

	fileList := mapKeys(files, 8)
	riskList := mapKeys(risks, 5)
	stepList := uniqueStrings(steps, 4)

	var b strings.Builder
	b.WriteString("- current objective: ")
	b.WriteString(objective)
	b.WriteString("\n")
	b.WriteString("- files touched: ")
	if len(fileList) == 0 {
		b.WriteString("(none captured)")
	} else {
		b.WriteString(strings.Join(fileList, ", "))
	}
	b.WriteString("\n")
	b.WriteString("- pending risks: ")
	if len(riskList) == 0 {
		b.WriteString("(none captured)")
	} else {
		b.WriteString(strings.Join(riskList, " | "))
	}
	b.WriteString("\n")
	b.WriteString("- next actionable steps: ")
	if len(stepList) == 0 {
		b.WriteString("continue from latest user request")
	} else {
		b.WriteString(strings.Join(stepList, " -> "))
	}
	return b.String()
}

func mapKeys(m map[string]struct{}, limit int) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func uniqueStrings(items []string, limit int) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func short(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}
