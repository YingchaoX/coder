# v2 设计文档 / v2 Design Document

> Date: 2026-02-10
> Status: Draft
> Scope: 基础层升级 + TUI 重构 + 功能增强 + 质量强化

---

## 1. 架构总览 / Architecture Overview

### 1.1 v1 → v2 模块演进 / Module Evolution

```
v1 (current)                          v2 (target)
─────────────────                     ─────────────────────────
cmd/agent/main.go (readline REPL)  →  cmd/agent/main.go (Bubble Tea app)
                                      internal/tui/          ← NEW
                                        app.go               ← Tea Model 根
                                        chat_panel.go        ← 聊天面板
                                        files_panel.go       ← 文件面板
                                        logs_panel.go        ← 日志面板
                                        sidebar.go           ← 侧边栏
                                        statusbar.go         ← 状态栏
                                        input.go             ← 输入区
                                        approval.go          ← 审批对话框
                                        diff_view.go         ← diff 视图
                                        render.go            ← markdown/代码渲染
                                        theme.go             ← 主题定义
                                        keys.go              ← 快捷键绑定
internal/provider/openai.go        →  internal/provider/
                                        provider.go          ← 接口定义
                                        openai.go            ← SDK-based 实现
                                        types.go             ← 公共类型
internal/storage/sessions.go       →  internal/storage/
                                        storage.go           ← 接口定义
                                        sqlite.go            ← SQLite 实现
                                        migrate.go           ← JSON → SQLite 迁移
internal/contextmgr/assembler.go   →  internal/contextmgr/
                                        assembler.go         ← 保持
                                        tokenizer.go         ← NEW tiktoken
                                        compaction.go        ← 重构: LLM strategy
                                      internal/i18n/         ← NEW
                                        i18n.go
                                        en.go
                                        zh_cn.go
internal/chat/types.go             →  internal/chat/types.go ← 扩展 reasoning
[其余模块保持不变]
```

### 1.2 依赖方向 / Dependency Flow

```
cmd/agent
    ↓
internal/tui  ←───── internal/i18n
    ↓
internal/orchestrator
    ↓                 ↓
internal/provider  internal/tools
    ↓                 ↓
internal/chat     internal/security
    ↓
internal/contextmgr (tokenizer)
    ↓
internal/storage (sqlite)
```

---

## 2. Provider 层重构 / Provider Layer Redesign

### 2.1 接口定义 / Interface

```go
// internal/provider/provider.go

package provider

import (
    "context"
    "coder/internal/chat"
)

// ChatRequest 封装一次模型请求
// ChatRequest wraps a single model call
type ChatRequest struct {
    Model       string
    Messages    []chat.Message
    Tools       []chat.ToolDef
    Temperature *float64
    TopP        *float64
    MaxTokens   int
}

// ChatStream 流式响应的回调集
// ChatStream is the callback set for streaming responses
type StreamCallbacks struct {
    OnTextChunk      func(chunk string)
    OnReasoningChunk func(chunk string)
    OnToolCall       func(call chat.ToolCall)
    OnUsage          func(usage Usage)
}

// Usage token 用量统计
// Usage reports token consumption
type Usage struct {
    PromptTokens     int
    CompletionTokens int
    ReasoningTokens  int
    TotalTokens      int
}

// ChatResponse 非流式完整响应
// ChatResponse is the complete non-streaming response
type ChatResponse struct {
    Content      string
    Reasoning    string
    ToolCalls    []chat.ToolCall
    FinishReason string
    Usage        Usage
}

// ModelInfo 模型基本信息
// ModelInfo describes a model
type ModelInfo struct {
    ID      string
    OwnedBy string
}

// Provider 模型提供方接口
// Provider is the model backend interface
type Provider interface {
    Chat(ctx context.Context, req ChatRequest, cb *StreamCallbacks) (ChatResponse, error)
    ListModels(ctx context.Context) ([]ModelInfo, error)
    Name() string
    CurrentModel() string
    SetModel(model string) error
}
```

### 2.2 OpenAI SDK 实现 / OpenAI SDK Implementation

```go
// internal/provider/openai.go

// OpenAIProvider 使用 sashabaranov/go-openai SDK
// OpenAIProvider uses the go-openai SDK
type OpenAIProvider struct {
    client *openai.Client
    model  string
    cfg    OpenAIConfig
    mu     sync.RWMutex
}

type OpenAIConfig struct {
    BaseURL      string
    APIKey       string
    Model        string
    TimeoutMS    int
    MaxRetries   int
    ReasoningOn  bool
}
```

关键变更:
- `Chat()` 内部使用 `client.CreateChatCompletionStream()` 替代手写 SSE
- Reasoning token 通过 SDK response delta 中的 `reasoning_content` 字段提取
- Token usage 直接从 SDK response 的 `Usage` 字段获取
- 错误重试逻辑内置: 429/500/502/503 → 指数退避

### 2.3 Reasoning Token 处理 / Reasoning Token Handling

```go
// chat/types.go 扩展
type Message struct {
    Role       string     `json:"role"`
    Content    string     `json:"content,omitempty"`
    Reasoning  string     `json:"reasoning,omitempty"`  // NEW: 推理内容
    Name       string     `json:"name,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}
```

流式处理时:
1. `delta.content` → 常规文本，通过 `OnTextChunk` 回调
2. `delta.reasoning_content` → 推理文本，通过 `OnReasoningChunk` 回调
3. 两种内容分别累积，最终组装到 `ChatResponse`
4. TUI 中 reasoning 内容独立渲染区域

---

## 3. Storage 层重构 / Storage Layer Redesign

### 3.1 接口定义 / Interface

```go
// internal/storage/storage.go

package storage

import "coder/internal/chat"

// Store 持久化接口，支持多后端
// Store is the persistence interface supporting multiple backends
type Store interface {
    // Session 操作 / Session operations
    CreateSession(meta SessionMeta) error
    SaveSession(meta SessionMeta) error
    LoadSession(id string) (SessionMeta, error)
    ListSessions() ([]SessionMeta, error)
    DeleteSession(id string) error

    // Message 操作 / Message operations
    SaveMessages(sessionID string, messages []chat.Message) error
    LoadMessages(sessionID string) ([]chat.Message, error)
    AppendMessage(sessionID string, msg chat.Message) error

    // Todo 操作 / Todo operations
    ListTodos(sessionID string) ([]TodoItem, error)
    ReplaceTodos(sessionID string, items []TodoItem) error

    // 权限日志 / Permission log
    LogPermission(sessionID, tool, decision, reason string) error

    // 生命周期 / Lifecycle
    Close() error
}
```

### 3.2 SQLite 实现要点 / SQLite Implementation Details

```go
// internal/storage/sqlite.go

type SQLiteStore struct {
    db   *sql.DB
    path string
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
    // 1. 打开数据库连接 / Open database
    db, err := sql.Open("sqlite", dbPath)

    // 2. 启用 WAL 模式 / Enable WAL
    _, _ = db.Exec("PRAGMA journal_mode=WAL")
    _, _ = db.Exec("PRAGMA busy_timeout=5000")
    _, _ = db.Exec("PRAGMA foreign_keys=ON")
    _, _ = db.Exec("PRAGMA synchronous=NORMAL")

    // 3. 创建表 / Create tables
    store.ensureSchema()

    // 4. 执行迁移 / Run migrations
    store.migrate()

    return store, nil
}
```

### 3.3 JSON 迁移 / JSON Migration

```go
// internal/storage/migrate.go

func MigrateFromJSON(jsonDir string, store *SQLiteStore) error {
    // 1. 扫描 *.meta.json 文件
    // 2. 逐个读取 meta + messages + todo
    // 3. 写入 SQLite
    // 4. 记录迁移完成标记
}
```

### 3.4 Message 存储格式 / Message Storage Format

SQLite 中 messages 表每行存储一条消息:
- `tool_calls` 字段存储 JSON 序列化的 `[]ToolCall`
- `reasoning` 字段存储推理内容文本
- `seq` 字段维护消息顺序
- 使用 `INSERT OR REPLACE` 保证幂等性

Session save 时:
1. BEGIN TRANSACTION
2. 清除该 session 旧消息（或 diff merge）
3. 批量 INSERT 新消息
4. 更新 session meta 的 `updated_at`
5. COMMIT

---

## 4. Token 计数 / Tokenizer Design

### 4.1 实现 / Implementation

```go
// internal/contextmgr/tokenizer.go

package contextmgr

// Tokenizer 精确 token 计数器
// Tokenizer provides precise token counting
type Tokenizer struct {
    encoder *tiktoken.Encoding
    name    string
}

// NewTokenizer 根据模型名创建 tokenizer
// NewTokenizer creates a tokenizer based on model name
func NewTokenizer(model string) *Tokenizer {
    // model → encoding 映射:
    // gpt-4*, gpt-3.5* → cl100k_base
    // o1*, o3* → o200k_base
    // qwen* → cl100k_base (兼容)
    // 默认 → cl100k_base
}

// Count 计算消息列表的总 token 数
// Count returns total token count for messages
func (t *Tokenizer) Count(messages []chat.Message) int {
    total := 0
    for _, msg := range messages {
        total += t.countMessage(msg)
    }
    return total
}

// CountText 计算单个文本的 token 数
// CountText counts tokens for a single text string
func (t *Tokenizer) CountText(text string) int {
    tokens := t.encoder.Encode(text, nil, nil)
    return len(tokens)
}
```

### 4.2 BPE 数据嵌入策略 / BPE Data Embedding

```go
//go:embed data/cl100k_base.tiktoken.gz
var cl100kData []byte

// 启动时解压并初始化
// Decompress and initialize at startup
func init() {
    // gzip decompress → tiktoken.NewEncoding()
}
```

- 使用 gzip 压缩嵌入，减少二进制体积 (~1.6MB → ~1MB)
- 全局单例，线程安全
- lazy init: 首次调用时初始化

### 4.3 EstimateTokens 替换 / Replace EstimateTokens

```go
// 旧接口保持兼容 / Old interface kept for compatibility
func EstimateTokens(messages []chat.Message) int {
    return DefaultTokenizer().Count(messages)
}
```

---

## 5. TUI 设计 / TUI Design

### 5.1 Bubble Tea Model 结构 / Model Structure

```go
// internal/tui/app.go

type App struct {
    // 面板 / Panels
    chatPanel  ChatPanel
    filesPanel FilesPanel
    logsPanel  LogsPanel
    sidebar    Sidebar
    statusBar  StatusBar
    inputArea  InputArea
    approval   *ApprovalDialog  // nil when not showing

    // 焦点管理 / Focus
    activePanel PanelID
    width       int
    height      int

    // 业务依赖 / Business deps
    orchestrator *orchestrator.Orchestrator
    store        storage.Store
    config       config.Config
    i18n         *i18n.I18n

    // 状态 / State
    streaming    bool
    currentMeta  storage.SessionMeta
    pendingInput string
}

type PanelID int
const (
    PanelChat PanelID = iota
    PanelFiles
    PanelLogs
)
```

### 5.2 布局算法 / Layout

```
┌────────────────────────────────────────┬────────────────┐
│                                        │   Sidebar      │
│  ┌──────┬──────┬──────┐               │   ─────────    │
│  │ Chat │Files │ Logs │  (tabs)       │   Context:     │
│  └──────┴──────┴──────┘               │   ███░░ 67%    │
│                                        │                │
│  [Active Panel Content]               │   Agent: build │
│  ┃ assistant:                         │   Model: qwen  │
│  ┃ Here is the implementation...      │                │
│  ┃ ```go                              │   MCP:         │
│  ┃ func main() { ... }               │   • srv1 ✓     │
│  ┃ ```                                │                │
│  ┃                                    │   Todo:        │
│  ┃ [TOOL] * Write "main.go"          │   [x] step 1   │
│  ┃   -> created (32 bytes)            │   [~] step 2   │
│  ┃                                    │   [ ] step 3   │
│                                        │                │
├────────────────────────────────────────┤                │
│  > user input area                     │                │
│    (multi-line textarea)               │                │
├────────────────────────────────────────┴────────────────┤
│  Build · claude-opus-4-5  ····  ~/project   tab switch  │
└─────────────────────────────────────────────────────────┘
```

布局计算:
```go
func (a *App) layout() {
    sidebarWidth := a.width * 25 / 100   // 25%
    if sidebarWidth < 20 { sidebarWidth = 20 }
    if sidebarWidth > 40 { sidebarWidth = 40 }

    mainWidth := a.width - sidebarWidth - 1  // -1 for border

    inputHeight := 3   // 最小 3 行
    statusHeight := 1
    tabHeight := 1

    panelHeight := a.height - inputHeight - statusHeight - tabHeight

    // 窗口太小时隐藏 sidebar
    if a.width < 80 {
        sidebarWidth = 0
        mainWidth = a.width
    }
}
```

### 5.3 Markdown 渲染 / Markdown Rendering

```go
// internal/tui/render.go

func renderMarkdown(content string, width int) string {
    r, _ := glamour.NewTermRenderer(
        glamour.WithAutoStyle(),
        glamour.WithWordWrap(width),
    )
    out, err := r.Render(content)
    if err != nil {
        return content  // fallback to raw
    }
    return out
}
```

代码块语法高亮集成:
- Glamour 内部使用 Chroma 做代码块高亮
- 通过自定义 Glamour style 配置 Chroma 主题
- 支持 256 色和 true color 自动检测

### 5.4 Diff 视图组件 / Diff View Component

```go
// internal/tui/diff_view.go

type DiffView struct {
    lines      []DiffLine
    viewport   viewport.Model
    showLineNo bool
}

type DiffLine struct {
    Kind     byte   // ' ', '+', '-', '@'
    LineNoOld int
    LineNoNew int
    Content  string
    Rendered string  // 带语法高亮的渲染结果
}

func NewDiffView(oldContent, newContent, path string, width int) DiffView {
    // 1. 生成 unified diff
    // 2. 对每行使用 Chroma 语法高亮
    // 3. 添加行号
    // 4. 包装为 viewport 可滚动
}
```

### 5.5 Approval 对话框 / Approval Dialog

```go
// internal/tui/approval.go

type ApprovalDialog struct {
    tool     string
    reason   string
    args     string
    diffView *DiffView  // 可选的 diff 预览
    danger   bool       // 是否为危险命令

    width  int
    height int
    result chan bool
}

func (d *ApprovalDialog) View() string {
    // ┌─ Approval Required ─────────────────┐
    // │ Tool: bash                           │
    // │ Reason: dangerous command (rm)       │
    // │                                      │
    // │ ⚠ DANGEROUS COMMAND                  │  ← 红色，仅危险命令
    // │                                      │
    // │ Command: rm -rf /tmp/test            │
    // │                                      │
    // │        [y] Allow    [n] Deny         │
    // └──────────────────────────────────────┘
}
```

### 5.6 Streaming 渲染流程 / Streaming Render Flow

```
User Input
    ↓
App.Update(SendInputMsg)
    ↓
goroutine: orchestrator.RunTurn()
    ↓                    ↓                      ↓
OnTextChunk(chunk)   OnReasoningChunk(chunk)  OnToolCall(call)
    ↓                    ↓                      ↓
tea.Send(TextChunkMsg)  tea.Send(ReasonMsg)   tea.Send(ToolCallMsg)
    ↓
App.Update → chatPanel.appendChunk()
    ↓
App.View() → 增量渲染
```

关键: orchestrator 的 `RunTurn` 在 goroutine 中执行，通过 Bubble Tea 的 `tea.Cmd` / `tea.Send` 机制将事件投递回 TUI 主循环。

---

## 6. Compaction 重构 / Compaction Redesign

### 6.1 LLM 策略 / LLM Strategy

```go
// internal/contextmgr/compaction.go

type CompactionStrategy interface {
    Summarize(ctx context.Context, messages []chat.Message) (string, error)
}

// LLMCompaction 使用 LLM 生成摘要
type LLMCompaction struct {
    provider provider.Provider
    model    string  // 空=使用当前模型
    maxTokens int
}

func (c *LLMCompaction) Summarize(ctx context.Context, messages []chat.Message) (string, error) {
    prompt := buildSummaryPrompt(messages)
    resp, err := c.provider.Chat(ctx, provider.ChatRequest{
        Model:    c.model,
        Messages: []chat.Message{
            {Role: "system", Content: summarySystemPrompt},
            {Role: "user", Content: prompt},
        },
        MaxTokens: c.maxTokens,
    }, nil)
    if err != nil {
        return "", err  // caller 处理 fallback
    }
    return resp.Content, nil
}

// RegexCompaction v1 兼容的正则提取
type RegexCompaction struct{}

func (c *RegexCompaction) Summarize(_ context.Context, messages []chat.Message) (string, error) {
    return summarizeMessages(messages), nil  // 复用 v1 逻辑
}
```

### 6.2 Compact 流程 / Compact Flow

```
maybeCompact()
    ↓
tokenizer.Count(messages) > threshold?
    ↓ yes
split messages: [old...] [recent...]
    ↓
strategy.Summarize(ctx, old)
    ↓ success           ↓ error
use LLM summary    fallback to regex
    ↓
replace old messages with [COMPACTION_SUMMARY]
    ↓
persist to storage
```

---

## 7. i18n 设计 / i18n Design

### 7.1 目录结构 / Structure

```go
// internal/i18n/i18n.go

type I18n struct {
    locale   string
    messages map[string]string
}

// T 翻译函数 / Translation function
func (i *I18n) T(key string, args ...any) string {
    tmpl, ok := i.messages[key]
    if !ok {
        return key  // fallback: 返回 key 本身
    }
    if len(args) == 0 {
        return tmpl
    }
    return fmt.Sprintf(tmpl, args...)
}

// DetectLocale 自动检测 / Auto-detect locale
func DetectLocale() string {
    for _, env := range []string{"AGENT_LANG", "LANG", "LC_ALL"} {
        if v := os.Getenv(env); v != "" {
            if strings.HasPrefix(v, "zh") {
                return "zh-CN"
            }
            return "en"
        }
    }
    return "en"
}
```

### 7.2 Message Keys

```go
// internal/i18n/en.go
var EnMessages = map[string]string{
    "app.title":           "Coder",
    "panel.chat":          "Chat",
    "panel.files":         "Files",
    "panel.logs":          "Logs",
    "sidebar.context":     "Context",
    "sidebar.agent":       "Agent",
    "sidebar.model":       "Model",
    "sidebar.mcp":         "MCP",
    "sidebar.todo":        "Todo",
    "status.workspace":    "Workspace",
    "approval.title":      "Approval Required",
    "approval.allow":      "Allow",
    "approval.deny":       "Deny",
    "approval.danger":     "⚠ DANGEROUS COMMAND",
    "error.provider":      "Provider error: %s",
    "error.tool":          "Tool error: %s",
    "compact.done":        "Context compacted",
    "compact.not_needed":  "Compaction not needed",
    // ...
}
```

---

## 8. 测试策略 / Test Strategy

### 8.1 Mock Provider

```go
// internal/provider/mock_test.go

type MockProvider struct {
    responses []ChatResponse
    callCount int
}

func (m *MockProvider) Chat(ctx context.Context, req ChatRequest, cb *StreamCallbacks) (ChatResponse, error) {
    if m.callCount >= len(m.responses) {
        return ChatResponse{}, fmt.Errorf("no more mock responses")
    }
    resp := m.responses[m.callCount]
    m.callCount++
    // 模拟流式回调 / Simulate streaming callbacks
    if cb != nil && cb.OnTextChunk != nil {
        cb.OnTextChunk(resp.Content)
    }
    return resp, nil
}
```

### 8.2 集成测试模式 / Integration Test Pattern

```go
func TestOrchestrator_FullTurnWithTools(t *testing.T) {
    mock := &MockProvider{
        responses: []ChatResponse{
            {ToolCalls: []chat.ToolCall{readFileCall("main.go")}},
            {Content: "Here is the file content analysis..."},
        },
    }
    store := storage.NewMemoryStore()
    orch := orchestrator.New(mock, registry, opts)

    result, err := orch.RunTurn(ctx, "read main.go", &bytes.Buffer{})
    assert.NoError(t, err)
    assert.Contains(t, result, "analysis")
    assert.Equal(t, 2, mock.callCount)
}
```

### 8.3 TUI Snapshot 测试 / TUI Snapshot Tests

```go
func TestChatPanel_Render(t *testing.T) {
    panel := NewChatPanel(80, 24)
    panel.AddMessage(chat.Message{Role: "user", Content: "hello"})
    panel.AddMessage(chat.Message{Role: "assistant", Content: "Hi!"})

    got := panel.View()
    golden := readGoldenFile(t, "chat_panel_basic.golden")
    if got != golden {
        updateGoldenFile(t, "chat_panel_basic.golden", got)
        t.Fatalf("snapshot mismatch")
    }
}
```

---

## 9. 实施顺序 / Implementation Order

### Phase 1: 基础层 (不影响现有 TUI)

1. **添加新依赖** → `go get` 所有 v2 依赖
2. **Provider 接口 + OpenAI SDK 实现** → `internal/provider/`
   - 定义 `Provider` interface
   - 实现 `OpenAIProvider` (SDK-based)
   - 保留旧 `Client` 临时兼容
   - 添加 reasoning token 支持
3. **Tokenizer** → `internal/contextmgr/tokenizer.go`
   - 嵌入 BPE 数据
   - 实现 `Tokenizer` + `Count()`
   - 替换 `EstimateTokens()`
4. **SQLite Storage** → `internal/storage/sqlite.go`
   - 实现 `Store` interface
   - JSON → SQLite 迁移
   - WAL 模式
5. **LLM Compaction** → `internal/contextmgr/compaction.go`
6. **i18n** → `internal/i18n/`

### Phase 2: TUI 重构

7. **TUI 骨架** → `internal/tui/app.go`
   - Bubble Tea Model 框架
   - 布局管理
   - 面板切换
8. **聊天面板** → `internal/tui/chat_panel.go`
   - 消息渲染 + markdown
   - 流式文本追加
   - Reasoning 折叠区域
9. **输入区域** → `internal/tui/input.go`
   - Textarea bubble
   - CJK 支持
   - 历史记录
10. **其余面板** → files, logs, sidebar, statusbar
11. **Approval 对话框** → modal
12. **Diff 视图** → diff_view.go

### Phase 3: 集成与质量

13. **集成 TUI + orchestrator**
    - 替换 `cmd/agent/main.go`
    - 保留 `--repl` fallback
14. **测试覆盖率提升**
15. **Makefile + CI 更新**

---

## 10. 向后兼容 / Backward Compatibility

### 10.1 `--repl` 模式 / REPL Mode
- v2 默认启动 Bubble Tea TUI
- `--repl` flag 启动 v1 的 readline REPL
- REPL 模式复用相同的 provider/storage/orchestrator

### 10.2 配置兼容 / Config Compatibility
- v1 配置文件完全兼容，新字段有默认值
- `storage.type` 默认 `"sqlite"`，可设为 `"json"` 回退
- `compaction.strategy` 默认 `"llm"`，可设为 `"regex"` 回退

### 10.3 数据兼容 / Data Compatibility
- 首次 v2 启动自动迁移 JSON sessions → SQLite
- 旧 JSON 文件保留在原位（不删除）
- 迁移日志写入 `~/.coder/migration.log`

---

## 11. 文件变更清单 / Change List

### 新增文件 / New Files
```
internal/tui/app.go
internal/tui/chat_panel.go
internal/tui/files_panel.go
internal/tui/logs_panel.go
internal/tui/sidebar.go
internal/tui/statusbar.go
internal/tui/input.go
internal/tui/approval.go
internal/tui/diff_view.go
internal/tui/render.go
internal/tui/theme.go
internal/tui/keys.go
internal/provider/provider.go     (接口)
internal/provider/types.go        (公共类型)
internal/storage/storage.go       (接口)
internal/storage/sqlite.go
internal/storage/migrate.go
internal/contextmgr/tokenizer.go
internal/contextmgr/compaction.go
internal/i18n/i18n.go
internal/i18n/en.go
internal/i18n/zh_cn.go
internal/contextmgr/data/cl100k_base.tiktoken.gz  (embedded)
Makefile
```

### 修改文件 / Modified Files
```
go.mod                            (新依赖)
go.sum
internal/chat/types.go            (+ Reasoning 字段)
internal/provider/openai.go       (重写为 SDK-based)
internal/contextmgr/assembler.go  (使用 Tokenizer)
internal/orchestrator/orchestrator.go  (适配新 Provider 接口)
internal/config/config.go         (新配置字段)
cmd/agent/main.go                 (TUI 入口 + --repl flag)
.github/workflows/ci.yml          (增强 pipeline)
```

### 保持不变 / Unchanged
```
internal/tools/*                  (全部保持)
internal/security/*               (全部保持)
internal/permission/*             (全部保持)
internal/skills/*                 (全部保持)
internal/mcp/*                    (全部保持)
internal/agent/*                  (全部保持)
```
