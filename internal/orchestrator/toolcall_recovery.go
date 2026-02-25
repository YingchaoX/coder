package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"coder/internal/chat"
)

var (
	toolCallBlockPattern = regexp.MustCompile(`(?is)<tool_call>\s*(.*?)\s*</tool_call>`)
	functionCallPattern  = regexp.MustCompile(`(?is)<function=([a-zA-Z0-9_\-]+)>\s*(.*?)\s*</function>`)
	parameterPattern     = regexp.MustCompile(`(?is)<parameter=([a-zA-Z0-9_\-]+)>\s*(.*?)\s*</parameter>`)
)

// recoverToolCallsFromContent recovers model-emitted pseudo tool-call markup into structured tool calls.
// It supports:
// 1) <tool_call>{"name":"bash","arguments":{"command":"uname"}}</tool_call>
// 2) <tool_call><function=bash><parameter=command>uname</parameter></function></tool_call>
func recoverToolCallsFromContent(content string, defs []chat.ToolDef) ([]chat.ToolCall, string) {
	if strings.TrimSpace(content) == "" || len(defs) == 0 {
		return nil, content
	}
	allowed := map[string]struct{}{}
	for _, d := range defs {
		name := strings.TrimSpace(strings.ToLower(d.Function.Name))
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, content
	}

	matches := toolCallBlockPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, content
	}

	calls := make([]chat.ToolCall, 0, len(matches))
	var cleaned strings.Builder
	last := 0
	for i, m := range matches {
		if len(m) < 4 {
			continue
		}
		start, end := m[0], m[1]
		innerStart, innerEnd := m[2], m[3]
		cleaned.WriteString(content[last:start])
		last = end
		inner := strings.TrimSpace(content[innerStart:innerEnd])
		call, ok := parseRecoveredToolCall(inner, allowed, i+1)
		if !ok {
			// keep unparsed content to avoid data loss
			cleaned.WriteString(content[start:end])
			continue
		}
		calls = append(calls, call)
	}
	cleaned.WriteString(content[last:])
	return calls, strings.TrimSpace(cleaned.String())
}

func parseRecoveredToolCall(inner string, allowed map[string]struct{}, seq int) (chat.ToolCall, bool) {
	if call, ok := parseJSONStyleToolCall(inner, allowed, seq); ok {
		return call, true
	}
	return parseTaggedFunctionToolCall(inner, allowed, seq)
}

func parseJSONStyleToolCall(inner string, allowed map[string]struct{}, seq int) (chat.ToolCall, bool) {
	var payload struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(inner), &payload); err != nil {
		return chat.ToolCall{}, false
	}
	name := strings.ToLower(strings.TrimSpace(payload.Name))
	if name == "" {
		return chat.ToolCall{}, false
	}
	if _, ok := allowed[name]; !ok {
		return chat.ToolCall{}, false
	}
	args := "{}"
	rawArgs := bytes.TrimSpace(payload.Arguments)
	if len(rawArgs) > 0 {
		// Require JSON object arguments for validity.
		if rawArgs[0] != '{' {
			return chat.ToolCall{}, false
		}
		var tmp map[string]any
		if err := json.Unmarshal(rawArgs, &tmp); err != nil {
			return chat.ToolCall{}, false
		}
		argsBytes, _ := json.Marshal(tmp)
		args = string(argsBytes)
	}
	return chat.ToolCall{
		ID:   fmt.Sprintf("recovered_call_%d", seq),
		Type: "function",
		Function: chat.ToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	}, true
}

func parseTaggedFunctionToolCall(inner string, allowed map[string]struct{}, seq int) (chat.ToolCall, bool) {
	m := functionCallPattern.FindStringSubmatch(inner)
	if len(m) != 3 {
		return chat.ToolCall{}, false
	}
	name := strings.ToLower(strings.TrimSpace(m[1]))
	if name == "" {
		return chat.ToolCall{}, false
	}
	if _, ok := allowed[name]; !ok {
		return chat.ToolCall{}, false
	}
	body := m[2]
	params := map[string]any{}
	for _, pm := range parameterPattern.FindAllStringSubmatch(body, -1) {
		if len(pm) != 3 {
			continue
		}
		key := strings.TrimSpace(pm[1])
		val := strings.TrimSpace(pm[2])
		if key == "" {
			continue
		}
		params[key] = val
	}
	if len(params) == 0 {
		return chat.ToolCall{}, false
	}
	args, err := json.Marshal(params)
	if err != nil {
		return chat.ToolCall{}, false
	}
	return chat.ToolCall{
		ID:   fmt.Sprintf("recovered_call_%d", seq),
		Type: "function",
		Function: chat.ToolCallFunction{
			Name:      name,
			Arguments: string(args),
		},
	}, true
}
