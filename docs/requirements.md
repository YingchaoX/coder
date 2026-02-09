# Offline Coding Agent Requirements (v1.0)

## 1. Document Info
- Project: Offline single-binary coding agent (OpenCode-inspired)
- Date: 2026-02-08
- Target platform: Linux (`x86_64`, `arm64`; Hygon treated as `x86_64`)
- Product form: TUI client only

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
