package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coder/internal/agent"
	"coder/internal/chat"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/permission"
	"coder/internal/provider"
	"coder/internal/storage"
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

type scriptedProvider struct {
	model     string
	responses []provider.ChatResponse
	callCount int
	requests  []provider.ChatRequest
}

func (p *scriptedProvider) Chat(_ context.Context, req provider.ChatRequest, _ *provider.StreamCallbacks) (provider.ChatResponse, error) {
	p.requests = append(p.requests, req)
	if p.callCount >= len(p.responses) {
		return provider.ChatResponse{}, errors.New("no scripted response")
	}
	resp := p.responses[p.callCount]
	p.callCount++
	return resp, nil
}

func (p *scriptedProvider) ListModels(context.Context) ([]provider.ModelInfo, error) { return nil, nil }
func (p *scriptedProvider) Name() string                                             { return "scripted" }
func (p *scriptedProvider) CurrentModel() string                                     { return p.model }
func (p *scriptedProvider) SetModel(model string) error {
	p.model = model
	return nil
}

type cancelAwareTool struct {
	name   string
	cancel context.CancelFunc
}

func (t *cancelAwareTool) Name() string { return t.name }

func (t *cancelAwareTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:       t.name,
			Parameters: map[string]any{"type": "object"},
		},
	}
}

func (t *cancelAwareTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	<-ctx.Done()
	return "", ctx.Err()
}

func TestFormatToolStart(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args string
		want string
	}{
		{name: "grep", tool: "grep", args: `{"pattern":"Home","path":"."}`, want: `* Grep "Home" in "."`},
		{name: "read path only", tool: "read", args: `{"path":"README.md"}`, want: `* Read "README.md"`},
		{name: "read with range", tool: "read", args: `{"path":"README.md","offset":1,"limit":100}`, want: `* Read "README.md"[1-100]`},
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
		{name: "read bytes with range", tool: "read", result: `{"ok":true,"path":"README.md","content":"abc","start_line":1,"end_line":3,"has_more":false}`, matches: []string{"3 bytes", `"README.md"`, "[1-3]"}},
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
	for _, needle := range []string{"[ANSWER]", "第一行", "第二行"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("missing %q in rendered output: %q", needle, rendered)
		}
	}
	if strings.Contains(rendered, "│") {
		t.Fatalf("unexpected vertical bar in rendered output (ANSWER block has no left border): %q", rendered)
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

func TestFormatBangCommandResult(t *testing.T) {
	got := formatBangCommandResult("echo hello", `{"ok":true,"exit_code":0,"duration_ms":7,"stdout":"hello\n","stderr":"","truncated":false}`)
	for _, needle := range []string{"$ echo hello", "exit=0", "stdout:", "hello"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in %q", needle, got)
		}
	}
	if strings.Contains(got, "[command mode]") {
		t.Fatalf("unexpected legacy header [command mode] in %q", got)
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

func TestRunTurnStopsImmediatelyOnToolContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tool := &cancelAwareTool{name: "bash", cancel: cancel}
	registry := tools.NewRegistry(tool)
	prov := &scriptedProvider{
		model: "demo-model",
		responses: []provider.ChatResponse{
			{
				ToolCalls: []chat.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: chat.ToolCallFunction{
							Name:      "bash",
							Arguments: `{"command":"echo test"}`,
						},
					},
				},
			},
		},
	}
	orch := New(prov, registry, Options{
		MaxSteps: 4,
		ActiveAgent: agent.Profile{
			Name: "build",
			ToolEnabled: map[string]bool{
				"bash": true,
			},
		},
	})

	_, err := orch.RunTurn(ctx, "run command", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got: %v", err)
	}
	if prov.callCount != 1 {
		t.Fatalf("expected single provider call, got %d", prov.callCount)
	}
	for _, msg := range orch.messages {
		if msg.Role == "tool" {
			t.Fatalf("expected no tool message appended after cancellation, found: %+v", msg)
		}
	}
}

func TestRunInputBangDeniedPersistsResult(t *testing.T) {
	registry := tools.NewRegistry(tools.NewBashTool(t.TempDir(), 2000, 1<<20))
	orch := New(nil, registry, Options{
		ActiveAgent: agent.Profile{
			Name: "test-agent",
			ToolEnabled: map[string]bool{
				"bash": false,
			},
		},
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

func TestRunInputBangRespectsPolicyPreset(t *testing.T) {
	registry := tools.NewRegistry(tools.NewBashTool(t.TempDir(), 2000, 1<<20))
	pol := permission.New(config.PermissionConfig{Default: "ask", Bash: map[string]string{"*": "ask"}})
	approvalCalls := 0
	orch := New(nil, registry, Options{
		Policy: pol,
		OnApproval: func(_ context.Context, req tools.ApprovalRequest) (bool, error) {
			approvalCalls++
			if req.Tool != "bash" {
				t.Fatalf("unexpected approval tool: %s", req.Tool)
			}
			return true, nil
		},
	})
	orch.SetMode("plan")

	got, err := orch.RunInput(context.Background(), "! echo hi", nil)
	if err != nil {
		t.Fatalf("RunInput failed: %v", err)
	}
	if strings.Contains(strings.ToLower(got), "command mode denied") {
		t.Fatalf("echo should be approval-based (not hard denied), got: %q", got)
	}
	if approvalCalls == 0 {
		t.Fatal("expected approval callback for ask command")
	}

	got, err = orch.RunInput(context.Background(), "! ls", nil)
	if err != nil {
		t.Fatalf("RunInput failed: %v", err)
	}
	if strings.Contains(strings.ToLower(got), "command mode denied") {
		t.Fatalf("ls should be allowed in plan whitelist, got: %q", got)
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
	if stats.MessageCount != 3 {
		t.Fatalf("message count=%d", stats.MessageCount)
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

func TestEditedPathFromToolCallWrite(t *testing.T) {
	args := mustJSON(map[string]any{
		"path":    "internal/tools/read.go",
		"content": "test",
	})
	got := editedPathFromToolCall("write", json.RawMessage(args))
	if got != "internal/tools/read.go" {
		t.Fatalf("editedPathFromToolCall(write) = %q, want %q", got, "internal/tools/read.go")
	}
}

func TestEditedPathFromToolCallEdit(t *testing.T) {
	args := mustJSON(map[string]any{
		"path":       "internal/tools/read.go",
		"old_string": "a",
		"new_string": "b",
	})
	got := editedPathFromToolCall("edit", json.RawMessage(args))
	if got != "internal/tools/read.go" {
		t.Fatalf("editedPathFromToolCall(edit) = %q, want %q", got, "internal/tools/read.go")
	}
}

func TestEditedPathFromToolCallPatchMarkdown(t *testing.T) {
	patch := `--- a/README.md
+++ b/README.md
@@ -1,3 +1,4 @@
 line1
 line2
 line3
`
	args := mustJSON(map[string]any{
		"patch": patch,
	})
	got := editedPathFromToolCall("patch", json.RawMessage(args))
	if got != "README.md" {
		t.Fatalf("editedPathFromToolCall(patch) = %q, want %q", got, "README.md")
	}
	if shouldAutoVerifyEditedPaths([]string{got}) {
		t.Fatalf("expected docs-only patch edits to skip auto-verify")
	}
}

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantCmd  string
		wantArgs string
		wantOk   bool
	}{
		{input: "/help", wantCmd: "help", wantArgs: "", wantOk: true},
		{input: "  /model qwen  ", wantCmd: "model", wantArgs: "qwen", wantOk: true},
		{input: "normal", wantCmd: "", wantArgs: "", wantOk: false},
		{input: "/", wantCmd: "", wantArgs: "", wantOk: true},
	}
	for _, tc := range tests {
		cmd, args, ok := parseSlashCommand(tc.input)
		if cmd != tc.wantCmd || args != tc.wantArgs || ok != tc.wantOk {
			t.Fatalf("parseSlashCommand(%q) = (%q, %q, %v), want (%q, %q, %v)", tc.input, cmd, args, ok, tc.wantCmd, tc.wantArgs, tc.wantOk)
		}
	}
}

func TestRunInputSlashCommand(t *testing.T) {
	registry := tools.NewRegistry()
	orch := New(nil, registry, Options{})

	got, err := orch.RunInput(context.Background(), "/help", nil)
	if err != nil {
		t.Fatalf("RunInput /help failed: %v", err)
	}
	if !strings.Contains(got, "Commands:") || !strings.Contains(got, "\n  /help\n") || !strings.Contains(got, "\n  /resume [session-id]\n") {
		t.Fatalf("unexpected /help output: %q", got)
	}

	got2, err := orch.RunInput(context.Background(), "/unknown", nil)
	if err != nil {
		t.Fatalf("RunInput /unknown failed: %v", err)
	}
	if !strings.Contains(got2, "Unknown") && !strings.Contains(got2, "unknown") {
		t.Fatalf("unexpected /unknown output: %q", got2)
	}
}

func TestRunInputResumeWithoutArgsListsSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	s1 := storage.SessionMeta{ID: "sess_a", Agent: "build", Model: "m1", CWD: "/tmp/a"}
	s2 := storage.SessionMeta{ID: "sess_b", Agent: "explore", Model: "m2", CWD: "/tmp/b"}
	if err := store.CreateSession(s1); err != nil {
		t.Fatalf("create session s1: %v", err)
	}
	if err := store.CreateSession(s2); err != nil {
		t.Fatalf("create session s2: %v", err)
	}

	current := "sess_b"
	orch := New(nil, tools.NewRegistry(), Options{
		Store:        store,
		SessionIDRef: &current,
	})

	got, err := orch.RunInput(context.Background(), "/resume", nil)
	if err != nil {
		t.Fatalf("RunInput /resume failed: %v", err)
	}
	for _, needle := range []string{"Recent sessions (timezone: Asia/Shanghai, UTC+08:00):", "sess_a", "sess_b", "Use /resume <session-id> to restore."} {
		if !strings.Contains(got, needle) {
			t.Fatalf("expected %q in output: %q", needle, got)
		}
	}
	if !strings.Contains(got, "UTC+08:00") {
		t.Fatalf("expected beijing timezone marker in output: %q", got)
	}
	if !strings.Contains(got, "* sess_b") {
		t.Fatalf("expected current session marker for sess_b: %q", got)
	}
}

func TestIsComplexTask(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"hi", false},
		{strings.Repeat("a", 80), true},
		{"step by step refactor", true},
		{"然后优化；修复", true},
		{"one two three four five six seven eight nine ten eleven twelve thirteen fourteen", true},
	}
	for _, tc := range tests {
		got := isComplexTask(tc.input)
		if got != tc.want {
			t.Fatalf("isComplexTask(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsChattyGreeting(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// 闲聊/问候
		{"你好", true},
		{"您好", true},
		{"hello", true},
		{"hi", true},
		{"hey", true},
		{"在吗", true},
		{"早上好", true},
		{"good morning", true},
		{"thanks", true},
		{"thank you", true},
		{"bye", true},
		{"再见", true},
		{"怎么样", true},
		{"ok", true},
		{"how are you", true},

		// 不是闲聊 - 有任务指令
		{"你好，帮我修改代码", false},
		{"hello, add a feature", false},
		{"查看一下文件", false},
		{"run test", false},
		{"fix the bug", false},
		{"实现功能", false},
		{"create file.txt", false},

		// 不是闲聊 - 太长
		{"你好，这是一个很长的问候语句，超过了长度限制所以不是闲聊", false},
		{"hello this is a very long greeting message that exceeds the limit", false},

		// 不是闲聊 - 包含冒号（通常是任务描述）
		{"你好: 请帮我做这件事", false},
		{"task: hello", false},

		// 不是闲聊 - 空字符串
		{"", false},

		// 边界情况 - 短但无明确问候词
		{"what", false},
		{"why", false},
	}
	for _, tc := range tests {
		got := isChattyGreeting(tc.input)
		if got != tc.want {
			t.Fatalf("isChattyGreeting(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsDocLikePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"docs/foo", true},
		{"x/docs/y", true},
		{"README.md", true},
		{"file.txt", true},
		{"internal/foo.go", false},
		{"", false},
	}
	for _, tc := range tests {
		got := isDocLikePath(tc.path)
		if got != tc.want {
			t.Fatalf("isDocLikePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestSummarizeForLog(t *testing.T) {
	if summarizeForLog("") != "-" {
		t.Fatalf("empty: %q", summarizeForLog(""))
	}
	if summarizeForLog("  short  ") != "short" {
		t.Fatalf("short: %q", summarizeForLog("  short  "))
	}
	long := strings.Repeat("x", 300)
	out := summarizeForLog(long)
	if !strings.HasSuffix(out, "...(truncated)") {
		t.Fatalf("long truncate: %q", out)
	}
}

func TestParseJSONObjectAndGetters(t *testing.T) {
	m := parseJSONObject(`{"a":"v","b":2,"c":[1,2]}`)
	if m == nil {
		t.Fatal("parseJSONObject returned nil")
	}
	if getString(m, "a", "") != "v" {
		t.Fatalf("getString: %q", getString(m, "a", ""))
	}
	if getString(m, "missing", "def") != "def" {
		t.Fatalf("getString missing: %q", getString(m, "missing", "def"))
	}
	if getInt(m, "b", 0) != 2 {
		t.Fatalf("getInt: %d", getInt(m, "b", 0))
	}
	if getInt(m, "missing", 42) != 42 {
		t.Fatalf("getInt missing: %d", getInt(m, "missing", 42))
	}
	if len(getArray(m, "c")) != 2 {
		t.Fatalf("getArray: %v", getArray(m, "c"))
	}
	if parseJSONObject("") != nil || parseJSONObject("invalid") != nil {
		t.Fatal("parseJSONObject invalid")
	}
}

func TestShortQuoteOrDashFirstLine(t *testing.T) {
	if short("hello", 3) != "hel..." {
		t.Fatalf("short: %q", short("hello", 3))
	}
	if short("hi", 10) != "hi" {
		t.Fatalf("short no trunc: %q", short("hi", 10))
	}
	if quoteOrDash("") != "-" {
		t.Fatalf("quoteOrDash empty: %q", quoteOrDash(""))
	}
	if firstLine("\n\n  first  \nsecond") != "first" {
		t.Fatalf("firstLine: %q", firstLine("\n\n  first  \nsecond"))
	}
}

func TestRunTurnPlanDoesNotAutoInitializeTodos(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "todoread", result: `{"ok":true,"count":0,"items":[]}`},
		mockTool{name: "todowrite", result: `{"ok":true,"count":1,"items":[{"content":"a","status":"in_progress"}]}`},
	)
	prov := &scriptedProvider{
		model: "demo-model",
		responses: []provider.ChatResponse{
			{Content: "这里是执行计划。"},
		},
	}
	orch := New(prov, registry, Options{MaxSteps: 3})
	orch.SetMode("plan")

	got, err := orch.RunTurn(context.Background(), "安装 python", nil)
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if !strings.Contains(got, "计划") {
		t.Fatalf("unexpected final output: %q", got)
	}
	for _, msg := range orch.messages {
		if msg.Role == "tool" && msg.Name == "todowrite" {
			t.Fatalf("unexpected auto todowrite in plan mode: %+v", msg)
		}
	}
}

func TestRunTurnPlanAllowsDirectTodoWriteWhenModelCallsIt(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "todowrite", result: `{"ok":true,"count":1,"items":[{"content":"explicit todo","status":"in_progress"}]}`},
	)
	prov := &scriptedProvider{
		model: "demo-model",
		responses: []provider.ChatResponse{
			{
				ToolCalls: []chat.ToolCall{
					{
						ID:   "call_todo_first",
						Type: "function",
						Function: chat.ToolCallFunction{
							Name:      "todowrite",
							Arguments: `{"todos":[{"content":"explicit todo","status":"in_progress","priority":"high"}]}`,
						},
					},
				},
			},
			{Content: "todo 已更新。"},
		},
	}
	orch := New(prov, registry, Options{MaxSteps: 4})
	orch.SetMode("plan")

	got, err := orch.RunTurn(context.Background(), "请先记一个todo", nil)
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if !strings.Contains(got, "todo") {
		t.Fatalf("unexpected final output: %q", got)
	}

	seenToolResult := false
	for _, msg := range orch.messages {
		if msg.Role != "tool" || msg.Name != "todowrite" {
			continue
		}
		if strings.Contains(msg.Content, `"denied":true`) {
			t.Fatalf("todowrite should not be blocked by orchestrator in plan mode: %q", msg.Content)
		}
		if strings.Contains(msg.Content, `"ok":true`) {
			seenToolResult = true
		}
	}
	if !seenToolResult {
		t.Fatal("expected todowrite tool result")
	}
}

func TestRunTurnFiltersPolicyDeniedToolsFromDefinitions(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "read", result: `{"ok":true}`},
		mockTool{name: "write", result: `{"ok":true}`},
		mockTool{name: "edit", result: `{"ok":true}`},
		mockTool{name: "patch", result: `{"ok":true}`},
		mockTool{name: "task", result: `{"ok":true}`},
		mockTool{name: "bash", result: `{"ok":true}`},
	)
	prov := &scriptedProvider{
		model: "demo-model",
		responses: []provider.ChatResponse{
			{Content: "analysis only"},
		},
	}
	orch := New(prov, registry, Options{MaxSteps: 2})
	orch.policy = permission.New(config.PermissionConfig{
		Default: "ask", Read: "allow", Edit: "deny", Write: "deny", Patch: "deny", Task: "deny",
		Bash: map[string]string{"*": "ask"},
	})

	if _, err := orch.RunTurn(context.Background(), "analyze code structure", nil); err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if len(prov.requests) == 0 {
		t.Fatal("expected provider to receive at least one request")
	}
	seen := map[string]bool{}
	for _, def := range prov.requests[0].Tools {
		seen[def.Function.Name] = true
	}
	if !seen["read"] {
		t.Fatalf("expected read tool definition, got %+v", seen)
	}
	if !seen["bash"] {
		t.Fatalf("expected bash tool definition, got %+v", seen)
	}
	for _, denied := range []string{"write", "edit", "patch", "task"} {
		if seen[denied] {
			t.Fatalf("expected %s to be filtered out by policy deny", denied)
		}
	}
}

func TestRecoverToolCallsFromContent_TaggedFunction(t *testing.T) {
	content := "I will run a command.\n<tool_call>\n<function=bash>\n<parameter=command>\nuname\n</parameter>\n</function>\n</tool_call>"
	defs := []chat.ToolDef{
		{Type: "function", Function: chat.ToolFunction{Name: "bash", Parameters: map[string]any{"type": "object"}}},
	}
	calls, cleaned := recoverToolCallsFromContent(content, defs)
	if len(calls) != 1 {
		t.Fatalf("recovered calls=%d, want 1", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("unexpected tool name: %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"command":"uname"}` {
		t.Fatalf("unexpected args: %s", calls[0].Function.Arguments)
	}
	if strings.Contains(cleaned, "<tool_call>") {
		t.Fatalf("expected cleaned content without tool_call block, got %q", cleaned)
	}
}

func TestRecoverToolCallsFromContent_BareTaggedFunction(t *testing.T) {
	content := "I'll help you install Python.\n<function=bash>\n<parameter=command>\nuname -s\n</parameter>\n</function>\n</tool_call>"
	defs := []chat.ToolDef{
		{Type: "function", Function: chat.ToolFunction{Name: "bash", Parameters: map[string]any{"type": "object"}}},
	}
	calls, cleaned := recoverToolCallsFromContent(content, defs)
	if len(calls) != 1 {
		t.Fatalf("recovered calls=%d, want 1", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("unexpected tool name: %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"command":"uname -s"}` {
		t.Fatalf("unexpected args: %s", calls[0].Function.Arguments)
	}
	if strings.Contains(cleaned, "<function=bash>") {
		t.Fatalf("expected cleaned content without function block, got %q", cleaned)
	}
}

func TestRecoverToolCallsFromContent_JSONStyle(t *testing.T) {
	content := "<tool_call>{\"name\":\"bash\",\"arguments\":{\"command\":\"uname -a\"}}</tool_call>"
	defs := []chat.ToolDef{
		{Type: "function", Function: chat.ToolFunction{Name: "bash", Parameters: map[string]any{"type": "object"}}},
	}
	calls, cleaned := recoverToolCallsFromContent(content, defs)
	if len(calls) != 1 {
		t.Fatalf("recovered calls=%d, want 1", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("unexpected tool name: %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"command":"uname -a"}` {
		t.Fatalf("unexpected args: %s", calls[0].Function.Arguments)
	}
	if cleaned != "" {
		t.Fatalf("expected empty cleaned content, got %q", cleaned)
	}
}

func TestChatWithRetryRecoversTaggedToolCalls(t *testing.T) {
	prov := &scriptedProvider{
		model: "demo-model",
		responses: []provider.ChatResponse{
			{
				Content: "<tool_call><function=bash><parameter=command>uname</parameter></function></tool_call>",
			},
		},
	}
	orch := New(prov, tools.NewRegistry(), Options{})
	defs := []chat.ToolDef{
		{Type: "function", Function: chat.ToolFunction{Name: "bash", Parameters: map[string]any{"type": "object"}}},
	}
	resp, err := orch.chatWithRetry(context.Background(), nil, defs, nil, nil)
	if err != nil {
		t.Fatalf("chatWithRetry failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected recovered tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls[0])
	}
}

func TestTodoStatusMarker(t *testing.T) {
	if todoStatusMarker("completed") != "[x]" {
		t.Fatalf("completed: %q", todoStatusMarker("completed"))
	}
	if todoStatusMarker("in_progress") != "[~]" {
		t.Fatalf("in_progress: %q", todoStatusMarker("in_progress"))
	}
	if todoStatusMarker("pending") != "[ ]" {
		t.Fatalf("pending: %q", todoStatusMarker("pending"))
	}
}

func TestContainsHan(t *testing.T) {
	if !containsHan("中文") {
		t.Fatal("expected true for 中文")
	}
	if containsHan("ascii only") {
		t.Fatal("expected false for ascii only")
	}
}

func TestSessionIDAccessors(t *testing.T) {
	var sid string
	orch := New(nil, tools.NewRegistry(), Options{
		SessionIDRef: &sid,
	})

	if got := orch.GetCurrentSessionID(); got != "" {
		t.Fatalf("expected empty session id, got %q", got)
	}

	orch.SetCurrentSessionID("sess_1")
	if sid != "sess_1" {
		t.Fatalf("sessionIDRef not updated, got %q", sid)
	}
	if got := orch.GetCurrentSessionID(); got != "sess_1" {
		t.Fatalf("GetCurrentSessionID=%q", got)
	}

	// nil ref should be safe no-op
	orch2 := New(nil, tools.NewRegistry(), Options{})
	orch2.SetCurrentSessionID("ignored")
	if got := orch2.GetCurrentSessionID(); got != "" {
		t.Fatalf("expected empty session id when ref nil, got %q", got)
	}
}

func TestModeAccessors(t *testing.T) {
	orch := New(nil, tools.NewRegistry(), Options{})

	if got := orch.CurrentMode(); got != "build" {
		t.Fatalf("default mode=%q", got)
	}

	orch.SetMode("PLAN")
	if got := orch.CurrentMode(); got != "plan" {
		t.Fatalf("after SetMode(plan) got %q", got)
	}

	orch.SetMode("  build ")
	if got := orch.CurrentMode(); got != "build" {
		t.Fatalf("after SetMode(build) got %q", got)
	}

	// invalid mode should be ignored and keep previous value
	orch.SetMode("invalid-mode")
	if got := orch.CurrentMode(); got != "build" {
		t.Fatalf("invalid mode should not change value, got %q", got)
	}
}

func TestSlashModeAndPermissionsSync(t *testing.T) {
	pol := permission.New(config.PermissionConfig{
		Default: "ask",
		Bash:    map[string]string{"*": "ask"},
	})
	orch := New(nil, tools.NewRegistry(), Options{
		Policy: pol,
	})

	got, err := orch.RunInput(context.Background(), "/mode plan", nil)
	if err != nil {
		t.Fatalf("mode plan failed: %v", err)
	}
	if !strings.Contains(got, "Mode set to plan") {
		t.Fatalf("unexpected /mode output: %q", got)
	}
	if orch.ActiveAgent().Name != "plan" {
		t.Fatalf("active agent=%q, want plan", orch.ActiveAgent().Name)
	}
	if decision := orch.policy.Decide("bash", json.RawMessage(`{"command":"git add ."}`)).Decision; decision != permission.DecisionAsk {
		t.Fatalf("plan preset should require approval for non-whitelisted bash command, got %s", decision)
	}

	got, err = orch.RunInput(context.Background(), "/permissions build", nil)
	if err != nil {
		t.Fatalf("permissions build failed: %v", err)
	}
	if !strings.Contains(got, "Permissions set to preset: build") {
		t.Fatalf("unexpected /permissions output: %q", got)
	}
	if orch.CurrentMode() != "build" {
		t.Fatalf("mode=%q, want build", orch.CurrentMode())
	}
	if orch.ActiveAgent().Name != "build" {
		t.Fatalf("active agent=%q, want build", orch.ActiveAgent().Name)
	}
}

func TestModeControlsTodoWriteAvailability(t *testing.T) {
	orch := New(nil, tools.NewRegistry(), Options{})
	if orch.CurrentMode() != "build" {
		t.Fatalf("mode=%q", orch.CurrentMode())
	}
	if orch.isToolAllowed("todowrite") {
		t.Fatal("build mode should disable todowrite")
	}
	orch.SetMode("plan")
	if !orch.isToolAllowed("todowrite") {
		t.Fatal("plan mode should allow todowrite")
	}
}

func TestPickVerifyCommandRespectsWorkflowOverride(t *testing.T) {
	orch := New(nil, tools.NewRegistry(), Options{
		Workflow: config.WorkflowConfig{
			VerifyCommands: []string{"  npm test  ", ""},
		},
	})
	got := orch.pickVerifyCommand()
	if got != "npm test" {
		t.Fatalf("expected workflow override \"npm test\", got %q", got)
	}
}

func TestPickVerifyCommandAutoDetectsByFiles(t *testing.T) {
	// go.mod -> go test ./...
	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	orchGo := New(nil, tools.NewRegistry(), Options{
		WorkspaceRoot: goDir,
	})
	if got := orchGo.pickVerifyCommand(); got != "go test ./..." {
		t.Fatalf("expected go test ./..., got %q", got)
	}

	// pyproject.toml -> pytest
	pyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pyDir, "pyproject.toml"), []byte("[tool.poetry]\n"), 0o644); err != nil {
		t.Fatalf("write pyproject.toml: %v", err)
	}
	orchPy := New(nil, tools.NewRegistry(), Options{
		WorkspaceRoot: pyDir,
	})
	if got := orchPy.pickVerifyCommand(); got != "pytest" {
		t.Fatalf("expected pytest, got %q", got)
	}

	// package.json -> npm test -- --watch=false
	jsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(jsDir, "package.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	orchJS := New(nil, tools.NewRegistry(), Options{
		WorkspaceRoot: jsDir,
	})
	if got := orchJS.pickVerifyCommand(); got != "npm test -- --watch=false" {
		t.Fatalf("expected npm test -- --watch=false, got %q", got)
	}

	// no known files -> empty
	emptyDir := t.TempDir()
	orchEmpty := New(nil, tools.NewRegistry(), Options{
		WorkspaceRoot: emptyDir,
	})
	if got := orchEmpty.pickVerifyCommand(); got != "" {
		t.Fatalf("expected empty command, got %q", got)
	}
}

func TestIsCoderConfigPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: ".coder/config.json", want: true},
		{path: "sub/.coder/config.json", want: true},
		{path: "CODER/config.json", want: false},
		{path: "", want: false},
	}
	for _, tc := range tests {
		got := isCoderConfigPath(tc.path)
		if got != tc.want {
			t.Fatalf("isCoderConfigPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestEmitContextUpdateUsesLimitAndTokens(t *testing.T) {
	orch := New(nil, tools.NewRegistry(), Options{
		ContextTokenLimit: 1000,
	})
	orch.LoadMessages([]chat.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	})

	calls := 0
	var lastTokens, lastLimit int
	var lastPercent float64
	orch.SetContextUpdateCallback(func(tokens, limit int, percent float64) {
		calls++
		lastTokens, lastLimit, lastPercent = tokens, limit, percent
	})

	orch.emitContextUpdate()

	if calls != 1 {
		t.Fatalf("expected 1 context update call, got %d", calls)
	}
	if lastLimit != 1000 {
		t.Fatalf("unexpected limit: %d", lastLimit)
	}
	if lastTokens <= 0 {
		t.Fatalf("expected positive token estimate, got %d", lastTokens)
	}
	if lastPercent <= 0 {
		t.Fatalf("expected positive usage percent, got %f", lastPercent)
	}
}

func TestRefreshTodosUsesTodoToolAndCallback(t *testing.T) {
	registry := tools.NewRegistry(
		mockTool{name: "todoread", result: `{"ok":true,"count":1,"items":[{"content":"demo","status":"pending"}]}`},
	)
	orch := New(nil, registry, Options{})

	var seen []string
	orch.SetTodoUpdateCallback(func(items []string) {
		seen = append(seen, items...)
	})

	orch.refreshTodos(context.Background())

	if len(seen) != 1 {
		t.Fatalf("expected 1 todo item, got %d", len(seen))
	}
	if !strings.Contains(seen[0], "demo") {
		t.Fatalf("unexpected todo content: %q", seen[0])
	}
}

func TestFlushSessionToFileWritesPerSessionJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sid := "sess_test"
	registry := tools.NewRegistry(
		mockTool{name: "read", result: `{"ok":true}`},
	)
	orch := New(nil, registry, Options{
		WorkspaceRoot: tmpDir,
		SessionIDRef:  &sid,
	})
	orch.assembler = contextmgr.New("SYSTEM_PROMPT", tmpDir, "", nil)

	orch.appendMessage(chat.Message{Role: "user", Content: "hello"})
	orch.appendMessage(chat.Message{Role: "assistant", Content: "world"})
	orch.appendMessage(chat.Message{
		Role: "assistant",
		ToolCalls: []chat.ToolCall{
			{
				ID:   "call_read_1",
				Type: "function",
				Function: chat.ToolCallFunction{
					Name:      "read",
					Arguments: `{"path":"README.md"}`,
				},
			},
		},
	})
	orch.appendMessage(chat.Message{
		Role:       "tool",
		Content:    `{"ok":true}`,
		Name:       "read",
		ToolCallID: "call_read_1",
	})

	if err := orch.flushSessionToFile(context.Background()); err != nil {
		t.Fatalf("flushSessionToFile failed: %v", err)
	}

	path := filepath.Join(tmpDir, ".coder", "sessions", sid+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}

	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("unmarshal session file: %v", err)
	}

	if sf.SessionID != sid {
		t.Fatalf("SessionID=%q, want %q", sf.SessionID, sid)
	}
	if len(sf.Tools) == 0 {
		t.Fatalf("expected non-empty tools slice in session file")
	}
	foundRead := false
	for _, td := range sf.Tools {
		if td.Function.Name == "read" {
			if td.Type != "function" {
				t.Fatalf("tool type=%q, want function", td.Type)
			}
			foundRead = true
		}
	}
	if !foundRead {
		t.Fatalf("expected tool definition for \"read\" in tools list")
	}
	if sf.CreatedAt == "" || sf.UpdatedAt == "" {
		t.Fatalf("expected non-empty timestamps, got created_at=%q updated_at=%q", sf.CreatedAt, sf.UpdatedAt)
	}
	if _, err := time.Parse(time.RFC3339, sf.CreatedAt); err != nil {
		t.Fatalf("created_at not RFC3339: %v", err)
	}
	if len(sf.Messages) != 5 {
		t.Fatalf("expected 5 messages (1 system + 4 runtime), got %d", len(sf.Messages))
	}
	if sf.Messages[0].Role != "system" {
		t.Fatalf("first message role=%q, want system", sf.Messages[0].Role)
	}
	for i, m := range sf.Messages {
		if strings.TrimSpace(m.Timestamp) == "" {
			t.Fatalf("message %d has empty timestamp", i)
		}
	}

	firstCreated := sf.CreatedAt

	// 第二次刷新应保留 created_at，并追加新消息。
	orch.appendMessage(chat.Message{Role: "user", Content: "follow-up"})
	time.Sleep(10 * time.Millisecond)

	if err := orch.flushSessionToFile(context.Background()); err != nil {
		t.Fatalf("second flushSessionToFile failed: %v", err)
	}

	data2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session file after second flush: %v", err)
	}
	var sf2 sessionFile
	if err := json.Unmarshal(data2, &sf2); err != nil {
		t.Fatalf("unmarshal session file after second flush: %v", err)
	}
	if sf2.CreatedAt != firstCreated {
		t.Fatalf("CreatedAt changed across flushes: first=%q second=%q", firstCreated, sf2.CreatedAt)
	}
	if len(sf2.Messages) != 6 {
		t.Fatalf("expected 6 messages after second flush, got %d", len(sf2.Messages))
	}
}
