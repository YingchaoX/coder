package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"coder/internal/chat"
	"coder/internal/provider"
	"coder/internal/tools"
)

type mockTool struct {
	name   string
	result string
}

func (m mockTool) Name() string { return m.name }

func (m mockTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:       m.name,
			Parameters: map[string]any{"type": "object"},
		},
	}
}

func (m mockTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return m.result, nil
}

func TestFormatToolStart(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args string
		want string
	}{
		{name: "grep", tool: "grep", args: `{"pattern":"Home","path":"."}`, want: `* Grep "Home" in "."`},
		{name: "read", tool: "read", args: `{"path":"README.md"}`, want: `* Read "README.md"`},
		{name: "write", tool: "write", args: `{"path":"a.txt","content":"hello"}`, want: `* Write "a.txt" (5 bytes)`},
		{name: "bash", tool: "bash", args: `{"command":"ls -la"}`, want: `* Bash "ls -la"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatToolStart(tc.tool, tc.args)
			if got != tc.want {
				t.Fatalf("unexpected start line: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestSummarizeToolResult(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		result  string
		matches []string
	}{
		{name: "grep count", tool: "grep", result: `{"ok":true,"count":18,"matches":[]}`, matches: []string{"18", "matches"}},
		{name: "read bytes", tool: "read", result: `{"ok":true,"path":"README.md","content":"abc"}`, matches: []string{"3 bytes", `"README.md"`}},
		{name: "bash ok", tool: "bash", result: `{"ok":true,"exit_code":0,"duration_ms":12,"stdout":"done\n","stderr":""}`, matches: []string{"exit=0", "12ms", "done"}},
		{name: "bash fail", tool: "bash", result: `{"ok":false,"exit_code":1,"duration_ms":6,"stdout":"","stderr":"oops"}`, matches: []string{"exit=1", "oops"}},
		{name: "todo checklist", tool: "todoread", result: `{"ok":true,"count":2,"items":[{"content":"step1","status":"in_progress"},{"content":"step2","status":"completed"}]}`, matches: []string{"todo items=2", "[~] step1", "[x] step2"}},
		{name: "write diff", tool: "write", result: `{"ok":true,"path":"a.txt","size":10,"operation":"updated","additions":1,"deletions":1,"diff":"@@ -1,1 +1,1 @@\n-old\n+new"}`, matches: []string{"updated", "+1 -1", "@@", "+new"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeToolResult(tc.tool, tc.result)
			for _, needle := range tc.matches {
				if !strings.Contains(got, needle) {
					t.Fatalf("missing %q in summary %q", needle, got)
				}
			}
		})
	}
}

func TestRenderToolResultMultiline(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	renderToolResult(&out, "line1\nline2\nline3")
	rendered := out.String()
	for _, needle := range []string{"-> line1", "line2", "line3"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("missing %q in rendered output: %q", needle, rendered)
		}
	}
}

func TestRenderToolResultDiffColorized(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("AGENT_NO_COLOR", "")
	var out bytes.Buffer
	renderToolResult(&out, "updated a.txt (+1 -1 lines, 10 bytes)\n@@ -1,1 +1,1 @@\n-old\n+new")
	rendered := out.String()
	for _, needle := range []string{ansiCyan + "@@", ansiRed + "-old", ansiGreen + "+new"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("missing %q in rendered output: %q", needle, rendered)
		}
	}
}

func TestAnswerStreamRenderer(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	renderer := newAnswerStreamRenderer(&out)
	renderer.Append("第一行")
	renderer.Append("\n第二")
	renderer.Append("行")
	renderer.Finish()
	rendered := out.String()
	for _, needle := range []string{"[ANSWER]", "│ 第一行", "│ 第二行"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("missing %q in rendered output: %q", needle, rendered)
		}
	}
}

func TestAnswerStreamRendererCompactsExtraBlankLines(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	renderer := newAnswerStreamRenderer(&out)
	renderer.Append("\n\n第一行\n\n\n第二行\n\n\n")
	renderer.Finish()
	rendered := out.String()
	if strings.Contains(rendered, "第一行\n\n\n第二行") {
		t.Fatalf("unexpected triple-blank gap in rendered output: %q", rendered)
	}
	if strings.Contains(rendered, "\n│\n") {
		t.Fatalf("unexpected standalone border line in rendered output: %q", rendered)
	}
}

func TestParseBangCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "! ls -la", want: "ls -la", ok: true},
		{input: "   !echo hi", want: "echo hi", ok: true},
		{input: "normal prompt", want: "", ok: false},
		{input: "!", want: "", ok: true},
	}
	for _, tc := range tests {
		got, ok := parseBangCommand(tc.input)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("parseBangCommand(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs string
		ok       bool
	}{
		{input: "/help", wantName: "help", wantArgs: "", ok: true},
		{input: " /model gpt-4.1 ", wantName: "model", wantArgs: "gpt-4.1", ok: true},
		{input: "/", wantName: "", wantArgs: "", ok: true},
		{input: "hello", wantName: "", wantArgs: "", ok: false},
	}
	for _, tc := range tests {
		name, args, ok := parseSlashCommand(tc.input)
		if name != tc.wantName || args != tc.wantArgs || ok != tc.ok {
			t.Fatalf("parseSlashCommand(%q)=(%q,%q,%v), want (%q,%q,%v)", tc.input, name, args, ok, tc.wantName, tc.wantArgs, tc.ok)
		}
	}
}

func TestFormatBangCommandResult(t *testing.T) {
	got := formatBangCommandResult("echo hello", `{"ok":true,"exit_code":0,"duration_ms":7,"stdout":"hello\n","stderr":"","truncated":false}`)
	for _, needle := range []string{"[command mode]", "$ echo hello", "exit=0", "hello"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in %q", needle, got)
		}
	}
}

func TestRunInputBangBypassesProviderAndPersistsContext(t *testing.T) {
	registry := tools.NewRegistry(tools.NewBashTool(t.TempDir(), 2000, 1<<20))
	orch := New(nil, registry, Options{})

	got, err := orch.RunInput(context.Background(), "! printf 'hello'", nil)
	if err != nil {
		t.Fatalf("RunInput failed: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("unexpected output: %q", got)
	}
	if len(orch.messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(orch.messages))
	}
	if orch.messages[0].Role != "user" || orch.messages[0].Content != "! printf 'hello'" {
		t.Fatalf("unexpected user message: %+v", orch.messages[0])
	}
	if orch.messages[1].Role != "assistant" || !strings.Contains(orch.messages[1].Content, "hello") {
		t.Fatalf("unexpected assistant message: %+v", orch.messages[1])
	}
}

func TestRunInputBangDeniedPersistsResult(t *testing.T) {
	registry := tools.NewRegistry(tools.NewBashTool(t.TempDir(), 2000, 1<<20))
	orch := New(nil, registry, Options{
		OnApproval: func(_ context.Context, _ tools.ApprovalRequest) (bool, error) { return false, nil },
	})

	got, err := orch.RunInput(context.Background(), "! rm -rf /tmp/demo", nil)
	if err != nil {
		t.Fatalf("RunInput failed: %v", err)
	}
	if !strings.Contains(got, "command mode denied") {
		t.Fatalf("unexpected output: %q", got)
	}
	if len(orch.messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(orch.messages))
	}
	if orch.messages[1].Role != "assistant" || !strings.Contains(orch.messages[1].Content, "command mode denied") {
		t.Fatalf("unexpected assistant message: %+v", orch.messages[1])
	}
}

type stubProvider struct {
	model     string
	responses []provider.ChatResponse
	err       error
	lastReq   provider.ChatRequest
}

func (s *stubProvider) Chat(_ context.Context, req provider.ChatRequest, _ *provider.StreamCallbacks) (provider.ChatResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return provider.ChatResponse{}, s.err
	}
	if len(s.responses) == 0 {
		return provider.ChatResponse{}, nil
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, nil
}

func (s *stubProvider) ListModels(_ context.Context) ([]provider.ModelInfo, error) { return nil, nil }
func (s *stubProvider) Name() string                                               { return "stub" }
func (s *stubProvider) CurrentModel() string                                       { return s.model }
func (s *stubProvider) SetModel(model string) error                                { s.model = model; return nil }

func TestRunInputSlashModelAndHelp(t *testing.T) {
	provider := &stubProvider{model: "gpt-4o-mini"}
	orch := New(provider, tools.NewRegistry(), Options{})

	got, err := orch.RunInput(context.Background(), "/model gpt-4.1", nil)
	if err != nil {
		t.Fatalf("RunInput /model failed: %v", err)
	}
	if !strings.Contains(got, "gpt-4.1") {
		t.Fatalf("unexpected /model output: %q", got)
	}
	if provider.CurrentModel() != "gpt-4.1" {
		t.Fatalf("model not changed: %q", provider.CurrentModel())
	}

	got, err = orch.RunInput(context.Background(), "/help", nil)
	if err != nil {
		t.Fatalf("RunInput /help failed: %v", err)
	}
	if !strings.Contains(got, "/compact") {
		t.Fatalf("unexpected /help output: %q", got)
	}
	if len(orch.messages) != 4 {
		t.Fatalf("unexpected message count: %d", len(orch.messages))
	}
}

func TestRunInputSlashCompactAndUnknown(t *testing.T) {
	orch := New(nil, tools.NewRegistry(), Options{})
	orch.LoadMessages([]chat.Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}})

	got, err := orch.RunInput(context.Background(), "/compact", nil)
	if err != nil {
		t.Fatalf("RunInput /compact failed: %v", err)
	}
	if !strings.Contains(got, "Context") {
		t.Fatalf("unexpected /compact output: %q", got)
	}

	got, err = orch.RunInput(context.Background(), "/what", nil)
	if err != nil {
		t.Fatalf("RunInput /what failed: %v", err)
	}
	if !strings.Contains(got, "Unknown command") {
		t.Fatalf("unexpected unknown output: %q", got)
	}
}

func TestCurrentContextStats(t *testing.T) {
	orch := New(nil, tools.NewRegistry(), Options{
		ContextTokenLimit: 1000,
	})
	orch.LoadMessages([]chat.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	})
	stats := orch.CurrentContextStats()
	if stats.ContextLimit != 1000 {
		t.Fatalf("limit=%d", stats.ContextLimit)
	}
	if stats.EstimatedTokens <= 0 {
		t.Fatalf("estimated=%d", stats.EstimatedTokens)
	}
	if stats.MessageCount != 2 {
		t.Fatalf("message count=%d", stats.MessageCount)
	}
}

func TestDefaultTodoItems(t *testing.T) {
	zh := defaultTodoItems("优化 docs/USAGE.md")
	if len(zh) != 3 {
		t.Fatalf("unexpected zh todo count: %d", len(zh))
	}
	if content, _ := zh[0]["content"].(string); !strings.Contains(content, "澄清需求") {
		t.Fatalf("unexpected zh first todo: %v", zh[0]["content"])
	}

	en := defaultTodoItems("Refactor parser module")
	if len(en) != 3 {
		t.Fatalf("unexpected en todo count: %d", len(en))
	}
	if content, _ := en[0]["content"].(string); !strings.Contains(content, "Clarify scope") {
		t.Fatalf("unexpected en first todo: %v", en[0]["content"])
	}
}

func TestEnsureSessionTodosAppendsValidToolSequence(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "todoread", result: `{"ok":true,"count":0,"items":[]}`},
		mockTool{name: "todowrite", result: `{"ok":true,"count":3,"items":[{"content":"a","status":"in_progress"}]}`},
	)
	orch := New(nil, registry, Options{})

	orch.ensureSessionTodos(context.Background(), "优化文档", nil)

	if len(orch.messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(orch.messages))
	}
	if orch.messages[0].Role != "assistant" || len(orch.messages[0].ToolCalls) != 1 {
		t.Fatalf("unexpected assistant tool call message: %+v", orch.messages[0])
	}
	if orch.messages[0].ToolCalls[0].Function.Name != "todowrite" {
		t.Fatalf("unexpected tool call name: %+v", orch.messages[0].ToolCalls[0])
	}
	if orch.messages[1].Role != "tool" || orch.messages[1].Name != "todowrite" {
		t.Fatalf("unexpected tool message: %+v", orch.messages[1])
	}
	if orch.messages[1].ToolCallID != orch.messages[0].ToolCalls[0].ID {
		t.Fatalf("tool_call_id mismatch: assistant=%q tool=%q", orch.messages[0].ToolCalls[0].ID, orch.messages[1].ToolCallID)
	}
}

func TestRunAutoVerifyAppendsValidToolSequence(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "bash", result: `{"ok":true,"exit_code":0,"duration_ms":1,"stdout":"","stderr":""}`},
	)
	orch := New(nil, registry, Options{})

	passed, retryable, err := orch.runAutoVerify(context.Background(), "go test ./...", 1, nil)
	if err != nil {
		t.Fatalf("runAutoVerify failed: %v", err)
	}
	if !passed {
		t.Fatalf("expected passed=true")
	}
	if retryable {
		t.Fatalf("expected retryable=false when verify passed")
	}
	if len(orch.messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(orch.messages))
	}
	if orch.messages[0].Role != "assistant" || len(orch.messages[0].ToolCalls) != 1 {
		t.Fatalf("unexpected assistant tool call message: %+v", orch.messages[0])
	}
	if orch.messages[0].ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("unexpected tool call name: %+v", orch.messages[0].ToolCalls[0])
	}
	if orch.messages[1].Role != "tool" || orch.messages[1].Name != "bash" {
		t.Fatalf("unexpected tool message: %+v", orch.messages[1])
	}
	if orch.messages[1].ToolCallID != orch.messages[0].ToolCalls[0].ID {
		t.Fatalf("tool_call_id mismatch: assistant=%q tool=%q", orch.messages[0].ToolCalls[0].ID, orch.messages[1].ToolCallID)
	}
}

func TestRunAutoVerifyMarksStartupFailureNonRetryable(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "bash", result: `{"ok":false,"exit_code":1,"duration_ms":2,"stdout":"","stderr":"/Users/demo/.profile: line 4: /Users/demo/.langflow/uv/env: No such file or directory"}`},
	)
	orch := New(nil, registry, Options{})

	passed, retryable, err := orch.runAutoVerify(context.Background(), "go test ./...", 1, nil)
	if err != nil {
		t.Fatalf("runAutoVerify failed: %v", err)
	}
	if passed {
		t.Fatalf("expected passed=false")
	}
	if retryable {
		t.Fatalf("expected retryable=false for shell startup failure")
	}
}

func TestShouldAutoVerifyEditedPaths(t *testing.T) {
	if !shouldAutoVerifyEditedPaths(nil) {
		t.Fatalf("expected true when path list is empty")
	}
	if shouldAutoVerifyEditedPaths([]string{"docs/USAGE.md", "README.md"}) {
		t.Fatalf("expected false for docs-only edits")
	}
	if !shouldAutoVerifyEditedPaths([]string{"docs/USAGE.md", "internal/orchestrator/orchestrator.go"}) {
		t.Fatalf("expected true when at least one non-doc path is edited")
	}
}

func TestRunTurnExecutesToolThenReturnsFinalText(t *testing.T) {
	prov := &stubProvider{
		model: "gpt-test",
		responses: []provider.ChatResponse{
			{
				Content: "",
				ToolCalls: []chat.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: chat.ToolCallFunction{
						Name:      "read",
						Arguments: `{"path":"README.md"}`,
					},
				}},
			},
			{Content: "done"},
		},
	}
	registry := tools.NewRegistry(mockTool{name: "read", result: `{"ok":true,"path":"README.md","content":"x"}`})
	orch := New(prov, registry, Options{})

	got, err := orch.RunTurn(context.Background(), "summarize", nil)
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if got != "done" {
		t.Fatalf("unexpected result: %q", got)
	}
	if len(orch.messages) != 4 {
		t.Fatalf("unexpected messages len: %d", len(orch.messages))
	}
}

func TestRunTurnStepLimitReached(t *testing.T) {
	prov := &stubProvider{
		responses: []provider.ChatResponse{{
			ToolCalls: []chat.ToolCall{{
				ID:       "call_1",
				Type:     "function",
				Function: chat.ToolCallFunction{Name: "read", Arguments: `{}`},
			}},
		}},
	}
	registry := tools.NewRegistry(mockTool{name: "read", result: `{"ok":true}`})
	orch := New(prov, registry, Options{MaxSteps: 1})
	_, err := orch.RunTurn(context.Background(), "loop", nil)
	if err == nil || !strings.Contains(err.Error(), "step limit reached") {
		t.Fatalf("expected step limit error, got: %v", err)
	}
}
