package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"coder/internal/config"
)

func TestParseContent(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "plain string chinese",
			raw:  `"你好，我可以帮你做什么？"`,
			want: "你好，我可以帮你做什么？",
		},
		{
			name: "typed text array",
			raw:  `[{"type":"text","text":"我肚子有点疼。"}]`,
			want: "我肚子有点疼。",
		},
		{
			name: "mixed reasoning and text",
			raw:  `[{"type":"reasoning","text":"内部推理"},{"type":"text","text":"建议先休息并补水。"}]`,
			want: "建议先休息并补水。",
		},
		{
			name: "nested content object",
			raw:  `{"content":[{"type":"text","text":"请描述更多症状。"}]}`,
			want: "请描述更多症状。",
		},
		{
			name: "fallback to compact json",
			raw:  `{"foo":"bar"}`,
			want: `{"foo":"bar"}`,
		},
		{
			name:    "invalid json",
			raw:     `not-json`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseContent(json.RawMessage(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected content: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestParseStreamResponse(t *testing.T) {
	streamPayload := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"你"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"好"}}]}`,
		``,
		// 第一个 tool_calls 块：模拟流式截断，缺根对象 }，解析器会补全
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"REA"}}]}}]}`,
		``,
		// 第二个 tool_calls 块：模拟流式截断，缺根对象 }，解析器会补全
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"DME.md\u0022}"}}]}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var chunks []string
	resp, err := parseStreamResponse(strings.NewReader(streamPayload), func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if resp.Content != "你好" {
		t.Fatalf("unexpected streamed content: %q", resp.Content)
	}
	if len(chunks) != 2 || chunks[0] != "你" || chunks[1] != "好" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	call := resp.ToolCalls[0]
	if call.Function.Name != "read" {
		t.Fatalf("unexpected tool call name: %q", call.Function.Name)
	}
	if call.Function.Arguments != `{"path":"README.md"}` {
		t.Fatalf("unexpected tool call args: %q", call.Function.Arguments)
	}
}

func TestParseNonStreamResponseEmitsChunk(t *testing.T) {
	raw := `{"choices":[{"message":{"content":"hello","tool_calls":[]},"finish_reason":"stop"}]}`
	var got string
	resp, err := parseNonStreamResponse(strings.NewReader(raw), func(chunk string) {
		got += chunk
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if got != "hello" {
		t.Fatalf("unexpected callback content: %q", got)
	}
}

func TestRepairJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid json unchanged",
			input: `{"a":1,"b":[2,3]}`,
			want:  `{"a":1,"b":[2,3]}`,
		},
		{
			name:  "missing closing brace before array end",
			input: `{"choices":[{"delta":{"k":"v"}}]}`,
			// 已经平衡，不需要修复 / Already balanced, no repair needed
			want: `{"choices":[{"delta":{"k":"v"}}]}`,
		},
		{
			name:  "object missing close before ]",
			input: `[{"a":1]`,
			// 栈顶是 '{' 遇到 ']'，先插入 '}'
			want: `[{"a":1}]`,
		},
		{
			name:  "truncated stream event like test case",
			input: `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"DME.md\u0022}"}}]}]}`,
			// choice 对象缺少闭合 }，修复后在 ] 前插入 }
			want: `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"DME.md\u0022}"}}]}}]}`,
		},
		{
			name:  "truncated at end missing braces",
			input: `{"a":{"b":1`,
			want:  `{"a":{"b":1}}`,
		},
		{
			name:  "truncated at end missing bracket",
			input: `[1,2,3`,
			want:  `[1,2,3]`,
		},
		{
			name:  "string containing braces not affected",
			input: `{"key":"{[}]"}`,
			want:  `{"key":"{[}]"}`,
		},
		{
			name:  "string with escaped quotes",
			input: `{"key":"val\"ue"}`,
			want:  `{"key":"val\"ue"}`,
		},
		{
			name:  "array missing close before }",
			input: `{"a":[1,2}`,
			want:  `{"a":[1,2]}`,
		},
		{
			name:  "empty input",
			input: ``,
			want:  ``,
		},
		{
			name:  "multiple missing closers",
			input: `{"a":[{"b":[{"c":1`,
			want:  `{"a":[{"b":[{"c":1}]}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(repairJSON([]byte(tc.input)))
			if got != tc.want {
				t.Fatalf("repairJSON mismatch:\n  input=%s\n  got  =%s\n  want =%s", tc.input, got, tc.want)
			}
			// 修复后的 JSON 应能被成功解析 / Repaired JSON should be parseable
			var dummy any
			if tc.want != "" {
				if err := json.Unmarshal([]byte(got), &dummy); err != nil {
					t.Fatalf("repaired JSON still invalid: %v\n  json=%s", err, got)
				}
			}
		})
	}
}

func TestClientModelSwitch(t *testing.T) {
	c := NewClient(config.ProviderConfig{
		BaseURL:   "http://127.0.0.1:8000/v1",
		Model:     "m1",
		TimeoutMS: 1000,
	})
	if c.Model() != "m1" {
		t.Fatalf("unexpected model=%q", c.Model())
	}
	if err := c.SetModel("m2"); err != nil {
		t.Fatalf("set model failed: %v", err)
	}
	if c.Model() != "m2" {
		t.Fatalf("unexpected model=%q", c.Model())
	}
	if err := c.SetModel(" "); err == nil {
		t.Fatalf("expected error for empty model")
	}
}
