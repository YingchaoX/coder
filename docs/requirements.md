# Offline Coding Agent Requirements

## 1. Document Info
- Project: Offline single-binary coding agent (OpenCode-inspired)
- Date: 2026-02-08 (v1) / 2026-02-10 (v2)
- Target platform: Linux (`x86_64`, `arm64`; Hygon treated as `x86_64`)
- Product form: TUI client only

---

# Part I — v1 Requirements (Implemented)

## 2. Background And Scope
- The environment is offline.
- A model service is already deployed and provides OpenAI-compatible API via vLLM.
- The runtime environment does not have npm, VSCode, or other IDE dependencies.
- The deliverable must be a single binary with no runtime dependency installation.

## 3. Product Goal
- Provide OpenCode-like developer workflow in terminal:
- Multi-turn coding conversation
- Tool calling (`read/write/search/patch/bash`)
- Local MCP tool integration
- Skill loading via `SKILL.md`
- Strong safety guardrails (mandatory confirmation for dangerous commands)

## 4. Non-Goals (v1)
- No VSCode/Cursor plugin integration.
- No remote MCP servers.
- No internet web tools (`webfetch`/`websearch`).
- No hard dependency on LSP servers.
- No built-in git commit/push workflow automation.

## 5. Constraints
- Single Linux binary only.
- All built-in file tool access must be restricted to startup `cwd`.
- `bash` is command execution capability and does not imply full file-level sandboxing.
- Logs and cache are allowed to persist on disk.
- Dangerous shell commands must always require interactive approval.

## 6. Reference Baseline (OpenCode)
- Client/server style interaction is used as architectural reference.
- Built-in agent layering (`build`, `plan`, `general/explore` style) is used as behavior reference.
- Permission model (`allow`/`ask`/`deny` + pattern rules) is used as policy reference.
- `AGENTS.md` and `SKILL.md` are first-class context inputs.
- MCP tools are injected into model toolset and consume context budget.

## 7. System Overview
- Single process, modular architecture (for single-binary delivery).
- Suggested modules:
- `tui`: input/output, session view, approval prompts
- `orchestrator`: turn loop, tool dispatch, retry/abort
- `context`: rules/skills/session history assembly and compaction
- `agent`: agent profile, permissions merge, task delegation
- `tooling`: file/search/patch/shell tools
- `mcp`: local MCP process lifecycle and tool bridge
- `provider`: OpenAI-compatible vLLM adapter
- `storage`: sessions, approvals, logs, cache
- `security`: path sandboxing + dangerous command policy

## 8. Functional Requirements

### 8.1 Context Management
1. Session data model
- `Session` contains metadata, active agent, model config, compaction state.
- `Message` contains role and parts (`text`, `tool_call`, `tool_result`, `reasoning_meta`).
- Support `new`, `continue`, `fork`, `summarize`, `revert`.

2. Context assembly pipeline (per turn)
- Base system prompt
- Global rules (`~/.config/.../AGENTS.md` equivalent)
- Project rules (`./AGENTS.md`)
- Additional instruction files (config-driven list)
- Session recent history
- Loaded skill content
- Latest tool results (with truncation)

3. Compaction policy
- `compaction.auto=true` by default.
- Auto summarize when context budget threshold is reached.
- `compaction.prune=true` by default to trim old tool outputs.
- Preserve critical state in summary:
- current objective
- files touched
- pending risks
- next actionable steps

4. Token budget policy
- Hard cap per request to avoid model rejection.
- Truncate large file/tool outputs with explicit marker.
- MCP tool descriptions/results participate in budget accounting.

5. Rules loading
- Load from project `AGENTS.md` first, then fallback hierarchy if configured.
- Rules are treated as high-priority instructions in context.

6. Skills loading
- Discover `SKILL.md` definitions from configured folders.
- Lazy load by name only when required by the agent/tool.
- Skill permissions are enforced before loading content.

7. Task progress tracking (mandatory for complex tasks)
- For complex implementation tasks, agent must maintain a structured todo list in-session.
- Todo list must support read and write operations.
- Todo item statuses must support at least `pending`, `in_progress`, `completed`.
- During execution, agent should keep todo list synchronized with real progress.

8. Context length visibility
- Session must provide a way to inspect current context length estimate.
- Display should include at least: estimated token usage, configured limit, and utilization percentage.

### 8.2 Agent Management
1. Agent types
- Primary agents: user-facing interaction agents.
- Subagents: task-focused workers invoked by task tool.

2. Built-in minimal agent set
- `build`: full workflow agent.
- `plan`: read-focused agent (no file writes; bash stricter).
- `general`: subagent for broad exploration.
- `explore`: read-only subagent for search-heavy tasks.
- Hidden system agents for title/summary/compaction can be internal-only.

3. Agent profile fields
- `name`, `mode`, `description`, `model_override`
- `tools enabled/disabled`
- `permission` override
- `max_steps`, `temperature`, `top_p` (optional)

4. Permission merge model
- Global permission baseline.
- Agent-level override.
- Runtime mandatory safety guards (cannot be bypassed by config).
- Effective policy resolved before every tool invocation.

5. Task delegation
- Primary agent can invoke subagent via `task`.
- `permission.task` controls which subagents may be launched.
- Subagent runs in child session context and returns summary/result to parent.

### 8.3 Interaction Logic
1. Turn state machine
- `idle -> planning -> awaiting_permission -> tool_running -> model_generating -> done/error`
- Supports abort and timeout transitions.

2. Input protocol
- Plain language prompt
- Optional command mode (`/command`)
- Prefix command mode (`!<shell command>`): execute command directly without model round-trip
- Optional file mentions (`@path`) resolved within sandbox

2.1 Prefix command mode behavior (`!`)
- Input starts with `!` (e.g. `! ls -la`): run shell command immediately in workspace root.
- This path bypasses LLM planning/tool-call loop, but must still apply dangerous-command mandatory approval policy.
- Command transcript must be persisted into session context for later turns:
- one `user` message containing original `!` input
- one `assistant` message containing command, exit code, stdout/stderr (with truncation marker when applicable)

3. Tool loop
- Model emits tool call.
- Permission check runs.
- If `ask`, prompt user in TUI.
- Execute tool and capture structured output.
- Return tool result back to model until completion or step limit.

4. Approval UX (mandatory)
- For dangerous shell command: always prompt, no bypass.
- Prompt includes:
- exact command
- reason/request context
- impact warning
- decision options: `allow once` / `deny`
- No permanent allow for dangerous-command class.

5. Error handling
- Tool execution errors are surfaced with concise actionable hints.
- Model/provider errors support retry with backoff.
- MCP unavailable tools degrade gracefully without crashing session.

6. Assistant answer streaming (mandatory)
- TUI must render assistant text incrementally during generation instead of waiting for full completion.
- `ANSWER` block should appear as soon as first text chunk arrives and append subsequent chunks in place.
- Streamed text must still be persisted as one complete assistant message in session history after the turn finishes.
- If stream is interrupted, show partial output and return explicit provider stream error.

7. Execution quality loop (mandatory for code-edit turns)
- If a turn performs code edits (`write`/`patch`), agent must run validation commands automatically before final completion whenever possible.
- Validation command can be configured and should support language-aware defaults (e.g., Go uses `go test ./...`).
- On validation failure, failure output must be fed back into the model loop to trigger iterative fixes.
- Loop ends only when validation passes, step limit is reached, or explicit stop/error is returned.

8. Runtime model switching
- User can list available models and switch active model at runtime via command mode.
- Model switching must apply to subsequent provider calls in the same session.
- Active model should be reflected in persisted session metadata.

9. P1 input editor upgrade (low-risk first rollout)
- REPL input must use a Unicode-aware line editor so CJK input/delete/backspace/cursor movement do not break layout.
- Input box must support `Up/Down` history navigation for previous prompts, consistent with common Linux terminal behavior.
- Prompt history should persist across restarts via local history file in app storage.
- If advanced line editor is unavailable, client must gracefully fall back to basic line input without crashing.

## 9. Tooling Requirements

### 9.1 Built-in Tools (v1 required)
- `read`: read file content
- `write`: write/replace file content
- `list`: list directory entries
- `grep`: content search (internal implementation; external `rg` optional fast path)
- `glob`: file pattern search
- `patch`: unified diff apply/reject
- `bash`: shell command execution with guardrails
- `todoread`: read current todo list
- `todowrite`: update current todo list
- `skill`: list/load skills
- `task`: invoke subagent

### 9.2 Path Security
- Normalize and resolve path before access.
- Deny path escaping outside `cwd`.
- Deny symlink escape to external directory.
- Return explicit permission error code and reason.

### 9.3 Dangerous Command Policy
- Mandatory confirm class (non-exhaustive):
- `rm`, `mv`, `chmod`, `chown`, `dd`, `mkfs`, `shutdown`, `reboot`
- redirection overwrite patterns (`>`, `1>`, `2>`) when target file exists
- policy is regex/pattern based and configurable, but cannot be disabled globally in v1.

## 10. MCP Requirements (Local Only)
1. Server type
- Support only local MCP process (`stdio`).
- No remote MCP in v1.

2. Configuration
- Multiple MCP servers supported.
- Per-server fields:
- `enabled`
- `command` (array)
- `environment` (map)
- `timeout_ms`

3. Lifecycle
- Start enabled MCP servers at app startup or first use.
- Health check and tools discovery with timeout.
- Restart policy for crash (bounded retries).

4. Context control
- Limit exposed MCP tool count and tool schema size.
- Truncate oversized MCP results with marker.

## 11. Skills Requirements (`SKILL.md`)
1. Discovery
- Project and global skill directories are scanned.
- Skill name uniqueness required at load time.

2. Metadata
- Parse frontmatter/basic metadata (`name`, `description`) when present.
- If missing, fallback to folder name + first paragraph summary.

3. Permission
- `permission.skill` supports `allow/ask/deny` with pattern matching.
- `deny` hides skill from available list.

4. Usage model
- Agent sees a lightweight index of available skills.
- Full `SKILL.md` content loaded only when explicitly requested.

## 12. Configuration Requirements
1. File format
- JSON/JSONC support.
- Project-level file + global-level file with deterministic precedence.

2. Core keys (v1)
- `provider` / `model` / `provider.models`
- `permission`
- `agent`
- `mcp`
- `compaction`
- `instructions`
- `logs` / `cache`

3. Env override (v1)
- Base URL, model ID, config path, cache path.

## 13. Observability And Storage
- Persist:
- sessions metadata
- message timeline
- permission decisions (excluding dangerous-command permanent allow)
- mcp startup/errors
- tool execution summary
- Provide bounded-size log rotation and cache TTL.

## 14. Performance Targets
- First token latency target: <= 2.5s in LAN setting (excluding model latency spikes).
- Interactive tool approval roundtrip: <= 150ms UI response.
- Search on medium repository should remain interactive (target < 1s for common query).

## 15. Compatibility And Build
- Build artifacts:
- `linux-x86_64` (glibc)
- `linux-x86_64` (musl static preferred)
- `linux-arm64` (glibc)
- `linux-arm64` (musl static preferred)
- Runtime dependencies: none required by user install flow.

## 16. Acceptance Criteria
1. Single binary launches on target Linux without extra package installation.
2. Connects to vLLM OpenAI-compatible endpoint and streams responses.
3. Executes built-in tools in closed loop until completion.
4. Any dangerous shell command always triggers mandatory confirmation.
5. Path access outside `cwd` is blocked for all file tools.
6. Local MCP servers can be configured, started, and called by model.
7. Skills discovered from `SKILL.md` and loaded on demand with permissions.
8. Context auto-compaction works and preserves task continuity.
9. Rules loading precedence is consistent: project `AGENTS.md` overrides global rules.
10. Permission key semantics are unambiguous (`write`/`patch`, or documented `edit` alias).
11. Complex coding tasks can persist and update todo list in-session via built-in todo tools.
12. When code is edited in a turn, automatic validation loop runs and feeds failures back for repair.
13. User can view current context length estimate (`used/limit/%`) in command mode.
14. User can switch active model via `/models <model_id>` and next turn uses the new model.
15. Chinese prompt editing and deletion in input line does not corrupt cursor/layout.
16. Input supports `Up/Down` recall of previous prompts and remains available after restart.

## 17. Milestones
1. M1: Core runtime
- Provider adapter, session model, TUI shell, basic tool loop.

2. M2: Safety + file/tooling
- Path sandboxing, dangerous command confirmation, read/write/list/grep/glob/patch/bash.

3. M3: Context + rules + skills
- AGENTS loading, instructions pipeline, compaction, `skill` tool.

4. M4: Agent framework
- Built-in agents, permission merge, task/subagent workflow.

5. M5: MCP local integration
- Local MCP lifecycle, timeout/retry, context budget controls.

6. M6: Hardening
- Regression tests, performance tuning, release packaging for four targets.

## 18. Risks And Mitigations
1. Risk: Context overflow due to MCP/schema/tool outputs.
- Mitigation: strict truncation and compaction pipeline.

2. Risk: Unsafe shell execution.
- Mitigation: non-bypassable dangerous-command confirmation.

3. Risk: Single-binary portability issues across glibc variants.
- Mitigation: provide musl static builds in addition to glibc builds.

4. Risk: No LSP may reduce semantic accuracy.
- Mitigation: optimize `grep/glob/read` pipeline and keep optional LSP extension point for v2.

## 19. Open Questions
1. Whether to support multi-project workspace switching within a single app session.
2. Whether dangerous command policy should include network-related commands even in offline environments.
3. Whether approval decisions should be persisted per session only or per project.

## 20. Appendix: Suggested v1 Default Policy
```json
{
  "permission": {
    "*": "ask",
    "read": "allow",
    "list": "allow",
    "glob": "allow",
    "grep": "allow",
    "write": "ask",
    "patch": "ask",
    "skill": "ask",
    "task": "ask",
    "bash": {
      "*": "ask",
      "ls *": "allow",
      "cat *": "allow",
      "grep *": "allow"
    },
    "external_directory": "deny"
  },
  "compaction": {
    "auto": true,
    "prune": true
  }
}
```

## 21. Reference Links
- https://github.com/anomalyco/opencode
- https://opencode.ai/docs/agents
- https://opencode.ai/docs/permissions
- https://opencode.ai/docs/tools
- https://opencode.ai/docs/mcp-servers
- https://opencode.ai/docs/skills
- https://opencode.ai/docs/rules
- https://opencode.ai/docs/config
- https://opencode.ai/docs/server

## 22. Additive Update (2026-02-08, Non-Breaking Merge)
- This section is additive only and does not replace any existing v1 scope, milestones, or future design items.

### 22.1 Interaction Visibility (Implemented)
1. Tool execution must be explicitly visible in terminal output.
- Show tool start line.
- Show tool result summary line.
- Show blocked/error line when applicable.

2. Assistant output must be visually segmented.
- `PLAN` block: intermediate planning/process text before tool completion.
- `ANSWER` block: final answer for current turn.
- `TOOL` block/line: tool execution lifecycle.

3. Output readability should support ANSI color by default.
- Color off switches: `NO_COLOR` or `AGENT_NO_COLOR`.

### 22.2 Chinese Compatibility (Implemented)
1. Default system behavior should reply in user's language unless explicitly requested otherwise.
2. Provider content parsing must support:
- string content
- typed content arrays/objects (extracting text parts)
- compact JSON fallback when text extraction is unavailable

### 22.3 Bailian Compatibility (Implemented)
1. API key env fallback:
- If `AGENT_API_KEY` is unset, use `DASHSCOPE_API_KEY`.

2. Endpoint guidance (config/docs level):
- China (Beijing): `https://dashscope.aliyuncs.com/compatible-mode/v1`
- Singapore: `https://dashscope-intl.aliyuncs.com/compatible-mode/v1`
- US (Virginia): `https://dashscope-us.aliyuncs.com/compatible-mode/v1`

### 22.4 Scope Clarification
- Items such as MCP, `task` subagent, `patch`, and richer session persistence remain planned scope (not removed by this update).

### 22.5 UX And Edit Visibility (Added 2026-02-08)
1. Todo-first behavior for complex tasks (mandatory)
- For complex implementation turns, if current session has no todo list, agent must prioritize todo creation before executing non-todo tools.
- Runtime should auto-initialize a starter todo list when missing, so user does not need to run extra todo commands manually.

2. Todo checklist rendering (mandatory)
- Todo output should be human-readable checklist style, for example:
- `[ ] task_a`
- `[~] task_b`
- `[x] task_c`
- `/todo` command and todo tool summaries must both use checklist-friendly rendering instead of only count-based output.

3. Structured approval args for file edits (mandatory)
- Approval prompt for edit tools must not dump raw JSON args directly when payload is large.
- For `write` to an existing file, approval view must parse args and show structured fields plus a readable diff preview.
- Display should prioritize path, operation type, line-level change summary, and diff snippet.

4. Write result readability (mandatory)
- `write` tool result should expose operation metadata (`created`/`updated`/`unchanged`) and change summary (`additions`/`deletions`).
- Terminal tool result rendering should support multi-line summaries (including diff/checklist snippets) for readability.

---

# Part II — v2 Requirements (2026-02-10)

> v2 是增量升级，不替换任何 v1 已实现功能。所有新增依赖必须为纯 Go 实现（CGo-free），确保继续支持离线单二进制交付。
>
> v2 is an incremental upgrade that does not replace any implemented v1 functionality. All new dependencies must be pure Go (CGo-free) to maintain offline single-binary delivery.

## 23. v2 背景与目标 / v2 Background And Goals

### 23.1 驱动因素 / Drivers
- v1 的 REPL 模式在多文件编辑、长对话场景下用户体验不佳
- token 计数使用 `len(runes)/4` 粗估，误差 30-50%，导致 compaction 时机不准
- 手写 SSE 解析脆弱，需 `repairJSON` 打补丁，不如标准 SDK 可靠
- JSON 文件持久化缺乏原子写入保障，并发场景下有丢数据风险
- compaction 摘要基于正则提取，质量有限
- 无 reasoning token 支持，无法利用 o1/o3 系列模型能力
- 缺乏国际化支持

### 23.2 v2 产品目标 / v2 Product Goals
1. 提供类 OpenCode 的全屏终端 TUI 交互体验
2. 精确的 token 管理和上下文控制
3. 可靠的 provider 通信层（SDK-based）
4. 可靠的数据持久化（SQLite WAL）
5. 高质量的上下文压缩（LLM-generated summaries）
6. 支持 reasoning token（o1/o3 系列模型）
7. 国际化/本地化支持
8. 测试覆盖率 ≥ 60%

### 23.3 v2 约束 / v2 Constraints
- 继续保持单二进制交付，所有依赖纯 Go（CGo-free）
- 继续支持离线环境，无互联网依赖
- 继续支持 OpenAI-compatible API（vLLM/DashScope 等）
- 向后兼容 v1 配置文件格式（新字段可选，旧字段保留）
- 向后兼容 v1 session 数据（提供迁移路径）

## 24. TUI 全面升级 / TUI Overhaul

### 24.1 框架选型 / Framework
- 使用 Bubble Tea (`charmbracelet/bubbletea`) 实现全屏交互式 TUI
- 使用 Lip Gloss (`charmbracelet/lipgloss`) 处理样式和布局
- 使用 Bubbles (`charmbracelet/bubbles`) 提供标准 UI 组件

### 24.2 多面板布局 / Multi-Panel Layout
1. **主布局**: 三栏/可切换面板
   - **聊天面板 (Chat Panel)**: 对话历史 + 流式输出区域
   - **文件面板 (Files Panel)**: 当前会话涉及的文件列表 + 预览
   - **日志面板 (Logs Panel)**: 工具执行日志 + MCP 状态 + 系统事件
2. **侧边栏**: 上下文信息
   - 当前会话标题和描述
   - context token 使用量 (bar / percentage)
   - 当前 Agent 名称和模型
   - MCP 服务器连接状态
   - LSP 状态（预留）
   - Todo 列表概览
3. **底部状态栏**:
   - 当前 agent + model
   - workspace 路径
   - 快捷键提示 (`tab` switch panel, `esc` interrupt, `ctrl+p` commands)
4. **输入区域**:
   - 多行输入编辑器 (textarea bubble)
   - CJK/宽字符正确处理
   - 输入历史 (Up/Down)
   - `@path` 补全

### 24.3 Markdown 渲染 / Markdown Rendering
- 使用 Glamour (`charmbracelet/glamour`) 渲染 assistant 回复中的 markdown
- 支持: 标题、列表、代码块、表格、粗体/斜体、链接
- 代码块使用 Chroma 语法高亮
- 自动检测 terminal 宽度和颜色能力
- 暗色主题为默认，可通过 `GLAMOUR_STYLE` 切换

### 24.4 语法高亮 / Syntax Highlighting
- 使用 Chroma (`alecthomas/chroma`) 对代码块和 diff 进行语法高亮
- 支持的场景:
  - assistant 回复中的 fenced code blocks (```go, ```python, etc.)
  - `read` 工具结果中的文件内容
  - `write`/`patch` 工具结果中的 diff
  - 文件面板中的文件预览
- 终端 256 色 / true color 自动检测

### 24.5 结构化 Diff 视图 / Structured Diff View
- `write` 和 `patch` 工具执行后展示结构化 diff
- 格式: 统一 diff 格式 + 行号 + 语法高亮
- 长 diff 支持折叠/展开
- 添加/删除行使用 green/red 背景色
- Approval 视图中直接展示 diff 预览

### 24.6 宽字符处理 / Wide Character Handling
- 使用 `mattn/go-runewidth` 处理 CJK 宽字符宽度计算
- 所有面板布局计算使用 runewidth 而非 `len()`
- 输入编辑器正确处理 CJK 光标移动
- 表格和对齐输出使用 runewidth 计算列宽

### 24.7 面板切换与快捷键 / Keybindings
| 快捷键 | 功能 |
|---|---|
| `Tab` | 切换面板焦点 |
| `Esc` | 中断当前生成 / 返回输入 |
| `Ctrl+P` | 打开命令面板 |
| `Ctrl+L` | 清屏 |
| `Ctrl+C` | 退出 (double press) |
| `Enter` | 提交输入 |
| `Shift+Enter` | 输入换行 |
| `Up/Down` | 输入历史 (在输入区) / 滚动 (在面板区) |
| `PgUp/PgDn` | 面板翻页 |
| `Ctrl+F` | 面板内搜索 |

### 24.8 Approval 对话框 / Approval Dialog
- 全屏模态对话框，不再使用 readline 行输入
- 显示: 工具名称、原因、参数摘要、diff 预览
- 按键: `y` allow once / `n` deny / `a` allow all (非危险) / `Esc` deny
- 危险命令: 红色警告横幅，无 `allow all` 选项

### 24.9 Spinner 与进度指示 / Spinners
- 模型生成时显示 spinner + elapsed time
- 工具执行时显示工具名 + spinner
- 长时间 bash 命令显示实时 stdout 流

## 25. Token 精确计数 / Precise Token Counting

### 25.1 实现方式 / Implementation
- 使用 `tiktoken-go` (纯 Go 实现) 进行精确 token 计数
- 支持的编码: `cl100k_base` (GPT-4 / ChatGPT), `o200k_base` (o1/o3)
- 编码选择基于模型名称自动映射

### 25.2 BPE 数据嵌入 / BPE Data Embedding
- 离线环境要求: BPE 编码数据必须嵌入二进制 (`go:embed`)
- 需要嵌入的文件:
  - `cl100k_base.tiktoken` (~1.6 MB)
  - `o200k_base.tiktoken` (~4.1 MB, 如支持 o-series)
- 或者使用预编译的 rank map 减小二进制体积

### 25.3 应用场景 / Usage
- `contextmgr.EstimateTokens()` 替换为精确计数
- compaction threshold 检查精确化
- `/context` 命令显示精确 token 使用量
- 侧边栏实时显示 token 用量百分比
- 工具结果截断阈值基于精确 token 计数

### 25.4 性能要求 / Performance
- 单次 token 计数 < 5ms (典型 4K token 消息)
- 编码器实例全局单例，避免重复初始化
- 支持增量计数（对流式输出）

## 26. Provider SDK 化 / Provider SDK Migration

### 26.1 SDK 选型 / SDK Choice
- 使用 `sashabaranov/go-openai` 作为 OpenAI-compatible SDK
- 该 SDK 纯 Go，支持 SSE streaming、tool calling、vision
- 替换当前手写的 SSE 解析逻辑 (`internal/provider/openai.go`)

### 26.2 Provider 接口抽象 / Provider Interface
```go
// Provider 接口 - 面向未来多 provider 扩展
// Provider interface - designed for future multi-provider extensibility
type Provider interface {
    // Chat 发送聊天请求并返回流式响应
    // Chat sends a chat request and returns a streaming response
    Chat(ctx context.Context, req ChatRequest) (*ChatStream, error)

    // ListModels 列出可用模型
    // ListModels lists available models
    ListModels(ctx context.Context) ([]ModelInfo, error)

    // Name 返回 provider 名称
    // Name returns the provider name
    Name() string
}
```

### 26.3 可扩展性设计 / Extensibility
- 当前仅实现 `OpenAIProvider`，但接口设计支持未来扩展:
  - `AnthropicProvider` (Claude)
  - `GoogleProvider` (Gemini)
  - `OllamaProvider` (本地模型)
- Provider 通过 config 中的 `provider.type` 字段选择
- 默认 `type: "openai"` 保持向后兼容

### 26.4 Reasoning Token 支持 / Reasoning Token Support
- 支持 o1/o3 系列模型的 reasoning 输出
- Message 类型扩展:
  - 新增 `ReasoningContent` 字段存储推理过程
  - 新增 `reasoning` 类型的 content part
- TUI 渲染:
  - reasoning 内容使用可折叠区域显示
  - 默认折叠，用户可展开查看完整推理过程
  - 使用 dimmed/gray 样式与正常回复区分
- Token 计数:
  - reasoning token 独立计数
  - 在 `/context` 和侧边栏中分别显示 `prompt/completion/reasoning` 用量

### 26.5 流式处理 / Streaming
- 使用 SDK 原生 SSE stream 接口
- 回调签名:
  - `OnTextChunk(chunk string)` — 文本增量
  - `OnReasoningChunk(chunk string)` — 推理增量
  - `OnToolCall(call ToolCall)` — 工具调用
  - `OnFinish(usage UsageInfo)` — 完成 + token 用量
- 错误处理:
  - 网络断开: 自动重连 + 指数退避 (最多 4 次)
  - 429 限速: 解析 `Retry-After` header，等待后重试
  - 500 系错误: 指数退避重试

## 27. SQLite 持久化 / SQLite Persistence

### 27.1 选型 / Choice
- 使用 `modernc.org/sqlite` — 纯 Go (CGo-free) SQLite 实现
- WAL 模式 (Write-Ahead Logging) 提供并发安全
- 单文件数据库: `~/.coder/coder.db`

### 27.2 数据库 Schema / Database Schema
```sql
-- 会话元数据 / Session metadata
CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL DEFAULT '',
    agent         TEXT NOT NULL DEFAULT 'build',
    model         TEXT NOT NULL DEFAULT '',
    cwd           TEXT NOT NULL DEFAULT '',
    summary       TEXT NOT NULL DEFAULT '',
    compact_auto  INTEGER NOT NULL DEFAULT 1,
    compact_prune INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- 会话消息 / Session messages
CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    seq         INTEGER NOT NULL,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_calls  TEXT NOT NULL DEFAULT '[]',  -- JSON array
    reasoning   TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    UNIQUE(session_id, seq)
);

-- Todo 条目 / Todo items
CREATE TABLE todos (
    id         TEXT NOT NULL,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    content    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',
    priority   TEXT NOT NULL DEFAULT 'medium',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(session_id, id)
);

-- 权限决策日志 / Permission decision log
CREATE TABLE permission_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    tool       TEXT NOT NULL,
    decision   TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    args_hash  TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- 索引 / Indexes
CREATE INDEX idx_messages_session ON messages(session_id, seq);
CREATE INDEX idx_todos_session ON todos(session_id);
CREATE INDEX idx_permission_log_session ON permission_log(session_id);
```

### 27.3 迁移策略 / Migration Strategy
- 首次启动 v2 时:
  1. 创建 SQLite 数据库 + schema
  2. 扫描旧版 JSON session 文件
  3. 自动迁移到 SQLite
  4. 保留旧文件作为备份 (不删除)
- 后续版本的 schema 变更通过 migration table 管理

### 27.4 并发安全 / Concurrency
- 连接池: 最多 1 writer + N readers
- WAL 模式: 读写不互斥
- 事务: session save 使用 BEGIN/COMMIT 包裹
- busy timeout: 5 秒

## 28. LLM Compaction / LLM-Based Compaction

### 28.1 策略 / Strategy
- 当 context token 超过阈值时，使用 LLM 生成高质量摘要
- 替换 v1 中基于正则提取的 `summarizeMessages()` 函数
- 使用内部 `summarizer` agent profile 执行摘要

### 28.2 摘要 Prompt / Summary Prompt
```
Summarize this conversation for an AI coding assistant that will continue the task.
Preserve: current objective, files modified/created, key decisions, pending issues, next steps.
Be concise but complete. Output plain text, no markdown formatting.
```

### 28.3 回退策略 / Fallback
- 如果 LLM 调用失败（网络/超时），回退到 v1 的正则提取方式
- 摘要结果缓存，避免重复调用
- 摘要 token 用量不计入用户 context budget

### 28.4 触发条件 / Triggers
- 自动: `compaction.auto=true` 且 context 超过 `compaction.threshold`
- 手动: 用户执行 `/compact` 命令
- compaction 后保留最近 `compaction.recent_messages` 条消息

## 29. 国际化 / Internationalization (i18n)

### 29.1 设计原则 / Design Principles
- 所有用户可见的 UI 字符串通过 message catalog 管理
- 运行时根据 locale 自动选择语言
- locale 检测优先级: `AGENT_LANG` > `LANG` > `LC_ALL` > 默认 `en`

### 29.2 支持语言 / Supported Languages
- v2 首批: `en` (English), `zh-CN` (简体中文)
- message catalog 使用 Go embed 嵌入

### 29.3 覆盖范围 / Scope
- TUI 面板标题和标签
- 状态栏文本
- 命令帮助文本
- 错误消息
- approval 对话框文本
- 默认 system prompt (可配置覆盖)
- **不覆盖**: LLM 输出内容 (由模型和用户语言决定)

### 29.4 实现方式 / Implementation
```go
// i18n 包结构 / i18n package structure
// internal/i18n/
//   catalog.go     — message ID -> template 映射
//   en.go          — English messages
//   zh_cn.go       — 简体中文 messages
//   i18n.go        — T() 函数 + locale 检测
```

## 30. 测试与质量 / Testing And Quality

### 30.1 覆盖率目标 / Coverage Target
- 总体代码覆盖率 ≥ 60%
- 核心模块 (orchestrator, provider, storage, security) ≥ 75%
- 工具模块 (tools/*) ≥ 70%
- TUI 模块 ≥ 40% (UI 测试天然较难)

### 30.2 测试类型 / Test Types
1. **单元测试**: 每个包的纯逻辑测试
2. **集成测试**: mock provider + orchestrator 端到端测试
3. **Snapshot 测试**: TUI 渲染输出的 golden file 比对
4. **Benchmark 测试**: tiktoken 编码性能、grep 搜索性能

### 30.3 可配置 Lint + Test Pipeline
- CI 中执行:
  1. `gofmt -l .` — 格式化检查
  2. `go vet ./...` — 静态分析
  3. `staticcheck ./...` — 额外静态检查 (可选)
  4. `go test -race -coverprofile=coverage.out ./...` — 带竞态检测的测试
  5. `go tool cover -func=coverage.out` — 覆盖率报告
  6. `go build ./cmd/agent` — 编译检查
- 本地开发:
  - `make test` — 快速测试
  - `make lint` — lint 检查
  - `make coverage` — 覆盖率报告
  - `make all` — lint + test + build

### 30.4 Agent 自动验证升级 / Auto-Verify Upgrade
- 支持配置多个验证命令（按顺序执行）
- 支持 sandbox 内测试执行:
  - 验证命令在隔离环境中执行
  - 捕获 exit code + stdout + stderr
  - 超时保护
- 验证命令可按文件类型配置:
  ```json
  {
    "workflow": {
      "verify_commands": {
        "*.go": ["go test ./...", "go vet ./..."],
        "*.py": ["pytest", "ruff check ."],
        "*.ts": ["npm test -- --watchAll=false"],
        "*": []
      }
    }
  }
  ```

## 31. v2 新增配置项 / v2 New Config Keys

```jsonc
{
  // provider 扩展 / Provider extensions
  "provider": {
    "type": "openai",              // "openai" (default), future: "anthropic", "google"
    "reasoning": true,             // 启用 reasoning token 解析 / Enable reasoning token parsing
    "max_retries": 4,              // 最大重试次数 / Max retry count
    "retry_backoff_ms": [1000, 2000, 4000, 8000]  // 退避间隔 / Backoff intervals
  },

  // TUI 配置 / TUI config
  "tui": {
    "theme": "dark",               // "dark" | "light"
    "glamour_style": "dark",       // Glamour 主题 / Glamour theme
    "show_reasoning": false,       // 默认是否显示 reasoning / Show reasoning by default
    "sidebar_width": 30,           // 侧边栏宽度 / Sidebar width percentage
    "max_diff_lines": 80           // diff 预览最大行数 / Max diff preview lines
  },

  // 存储配置扩展 / Storage extensions
  "storage": {
    "type": "sqlite",              // "sqlite" (v2 default), "json" (v1 compat)
    "sqlite_path": "~/.coder/coder.db",
    "wal_mode": true
  },

  // compaction 扩展 / Compaction extensions
  "compaction": {
    "strategy": "llm",             // "llm" (v2 default), "regex" (v1 compat)
    "summary_model": "",           // 空=使用当前模型 / Empty=use current model
    "max_summary_tokens": 500
  },

  // i18n 配置 / i18n config
  "i18n": {
    "locale": "",                  // 空=自动检测 / Empty=auto-detect
    "fallback": "en"
  },

  // 测试 pipeline / Test pipeline
  "workflow": {
    "verify_commands": {},          // 按文件类型 / By file type (v2 format)
    "lint_commands": [],            // lint 命令列表 / Lint command list
    "test_timeout_ms": 60000
  }
}
```

## 32. v2 验收标准 / v2 Acceptance Criteria

1. 启动后展示全屏 Bubble Tea TUI，包含聊天、文件、日志三面板
2. 侧边栏实时显示 context token 用量、agent、model、MCP 状态
3. assistant 回复中的 markdown 正确渲染（标题、列表、代码块带语法高亮）
4. `write`/`patch` 执行后展示结构化 diff 视图，带语法高亮和行号
5. CJK 输入/删除/光标移动在全屏 TUI 中不出现排版异常
6. token 计数精确度误差 < 5% (对比 OpenAI tokenizer 结果)
7. 流式输出使用 SDK stream，无手写 SSE 解析
8. 支持 reasoning token 的模型输出（如 o1/o3），reasoning 内容可折叠显示
9. Session 数据持久化到 SQLite，WAL 模式启用
10. 旧版 JSON session 文件自动迁移到 SQLite
11. compaction 使用 LLM 生成摘要，fallback 到正则提取
12. UI 文本支持 en/zh-CN 两种语言，自动检测 locale
13. 测试覆盖率 ≥ 60%，核心模块 ≥ 75%
14. CI pipeline 包含 lint + vet + test + race + coverage + build
15. Makefile 提供 `test`/`lint`/`coverage`/`build`/`all` targets

## 33. v2 里程碑 / v2 Milestones

1. **M7: 基础层升级** (Week 1-2)
   - SQLite 持久化 + JSON 迁移
   - tiktoken 精确 token 计数
   - OpenAI SDK 替换 + reasoning token
   - Provider 接口抽象

2. **M8: TUI 框架** (Week 3-5)
   - Bubble Tea 全屏应用骨架
   - 多面板布局 (聊天/文件/日志)
   - 输入编辑器 (多行 + CJK)
   - 侧边栏信息面板
   - 底部状态栏

3. **M9: TUI 渲染** (Week 5-7)
   - Glamour markdown 渲染
   - Chroma 语法高亮
   - 结构化 diff 视图
   - Approval 模态对话框
   - Spinner + 进度指示

4. **M10: 功能升级** (Week 7-8)
   - LLM compaction
   - i18n (en + zh-CN)
   - 可配置 lint/test pipeline

5. **M11: 质量强化** (Week 8-10)
   - 测试覆盖率提升
   - Benchmark 测试
   - 性能优化
   - 文档更新

## 34. v2 风险与缓解 / v2 Risks And Mitigations

1. **Risk**: Bubble Tea TUI 大量重写可能引入回归
   - Mitigation: 保留 `--repl` flag 可切换回 v1 REPL 模式

2. **Risk**: `modernc.org/sqlite` 纯 Go 性能低于 CGo SQLite
   - Mitigation: WAL + 合理缓存; session 数据量不大，性能足够

3. **Risk**: tiktoken BPE 数据嵌入增大二进制体积 (~5 MB)
   - Mitigation: 使用压缩嵌入 + lazy init; 或提供 slim build 选项

4. **Risk**: LLM compaction 调用失败影响用户体验
   - Mitigation: 双策略回退 (LLM -> regex); compaction 失败不阻塞正常使用

5. **Risk**: 多面板 TUI 在小终端窗口下布局异常
   - Mitigation: 响应式布局; 窗口过小时自动切换为单面板模式

## 35. v2 依赖清单 / v2 Dependency List

| 包 | 用途 | 纯 Go | 大小估算 |
|---|---|---|---|
| `charmbracelet/bubbletea` | TUI 框架 | Yes | ~50 KB |
| `charmbracelet/lipgloss` | 样式/布局 | Yes | ~30 KB |
| `charmbracelet/bubbles` | UI 组件 | Yes | ~40 KB |
| `charmbracelet/glamour` | Markdown 渲染 | Yes | ~100 KB |
| `alecthomas/chroma` | 语法高亮 | Yes | ~2 MB (含语法定义) |
| `mattn/go-runewidth` | 宽字符宽度 | Yes | ~10 KB |
| `sashabaranov/go-openai` | OpenAI SDK | Yes | ~50 KB |
| `modernc.org/sqlite` | SQLite (CGo-free) | Yes | ~8 MB (含 SQLite C 翻译) |
| `tiktoken-go/tokenizer` | Token 计数 | Yes | ~2 MB (含 BPE 数据) |

**预计二进制体积增长**: ~12-15 MB (从 ~8 MB 到 ~20-23 MB)
