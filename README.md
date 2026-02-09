# coder

Offline coding agent for terminal usage (single binary).

## Implemented

- OpenAI-compatible provider (`/v1/chat/completions`, SSE streaming)
- Tool loop orchestration with `PLAN` / `TOOL` / `ANSWER` segmented output
- Built-in tools: `read`, `write`, `list`, `glob`, `grep`, `patch`, `bash`, `todoread`, `todowrite`, `skill`, `task`
- Workspace path sandbox for file tools
- Mandatory approval for dangerous `bash` commands
- Policy-based permission checks (`allow` / `ask` / `deny`)
- Agent profiles (`build`, `plan`, `general`, `explore`) and `task` subagent execution
- Complex-task todo tracking (`todoread` / `todowrite`)
- Auto verification loop after code edits (`write`/`patch`)
- Context length estimation via `/context`
- Runtime model switching via `/models`
- Rules/instructions context loading (`AGENTS.md`, configured instruction files)
- Context auto-compaction (summary + tool output pruning)
- Session persistence (`new`, `continue/use`, `fork`, `revert`, `summarize`)
- `!<command>` direct shell mode persisted in session history
- Optional `@path` mention expansion inside prompt (workspace sandboxed)

## Build & Run

```bash
go build -o bin/agent ./cmd/agent
./bin/agent -config agent.config.json.example
```

## REPL Commands

- `/help`
- `/new`
- `/sessions`
- `/use <id>`
- `/fork <id>`
- `/revert <message_count>`
- `/agent <name>`
- `/models [model_id]`
- `/context`
- `/tools`
- `/skills`
- `/todo`
- `/summarize`
- `/compact`
- `/config`
- `/mcp`
- `/exit`

Direct shell command mode:

- `!<shell command>`

## Config

Config supports JSON and JSONC. See `agent.config.json.example`.

Environment overrides:

- `AGENT_CONFIG_PATH`
- `AGENT_BASE_URL`
- `AGENT_MODEL`
- `AGENT_API_KEY`
- `DASHSCOPE_API_KEY` (fallback)
- `AGENT_WORKSPACE_ROOT`
- `AGENT_MAX_STEPS`
- `AGENT_CACHE_PATH`

## Test

```bash
go test ./...
go build ./cmd/agent
```

## GitHub

- CI workflow: `.github/workflows/ci.yml` (`gofmt` + `go vet` + `go test ./...` + build check)
- Release workflow: `.github/workflows/release.yml` (cross-build Linux/macOS/Windows binaries, upload artifacts, and attach assets when pushing `v*` tags)
