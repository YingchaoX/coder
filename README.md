# coder

离线终端编码代理，单二进制、全屏 TUI，面向无 IDE 环境的开发工作流。

---

## 特性概览

| 类别 | 说明 |
|------|------|
| **运行形态** | 单二进制，无运行时依赖；Bubble Tea 全屏 TUI |
| **模型接入** | OpenAI 兼容 API（vLLM / DashScope 等），SSE 流式，可选 reasoning token |
| **上下文** | 规则/技能/会话组装，自动压缩（摘要 + 裁剪），SQLite 持久化 |
| **工具** | `read` / `write` / `list` / `glob` / `grep` / `patch` / `bash`，`todoread` / `todowrite`，`skill`，`task`，本地 MCP |
| **安全** | 工作区路径沙箱；危险命令强制审批；策略 `allow` / `ask` / `deny` |
| **代理** | 内置 profile（`build` / `plan` / `general` / `explore`），`task` 子代理 |
| **国际化** | UI 支持 en / zh-CN，locale 自动检测 |

---

## 构建与运行

```bash
# 构建
make build
# 或
go build -o bin/agent ./cmd/agent

# 启动
./bin/agent
```

**配置文件发现顺序**

| 位置 | 说明 |
|------|------|
| `./.coder/config.json` | 当前路径配置（优先） |
| `~/.coder/config.json` | 家目录配置（当前路径不存在时回退） |

---

## TUI 与操作

- **布局**：单主面板（对话/工具/日志时间线）；侧边栏（会话、context 用量、agent、model）；底部状态栏。
- **输入**：多行输入；`@path` 提及（工作区内路径）；`!<shell 命令>` 与 `/<内建命令>` 特殊命令。
- **快捷键**（以实际代码为准）：`Tab` 切换 `ask`/`edit(agent)` 模式，`Esc` 中断/返回。

**命令（通过输入）**

| 命令 | 说明 |
|------|------|
| `/help` | 帮助 |
| `/model [model_id]` | 查看/切换模型 |

直接 Shell：输入以 `!` 开头，如 `! ls -la`，仍受危险命令审批策略约束。

---

## 配置

配置文件使用 `./.coder/config.json`（项目）或 `~/.coder/config.json`（全局回退），仓库中的 `agent.config.json.example` 可作为字段参考。支持 JSON/JSONC。

**主要配置块**

| 键 | 说明 |
|------|------|
| `provider` | `base_url`、`model`、`models`、`api_key`、`timeout_ms` |
| `runtime` | `workspace_root`、`max_steps`、`context_token_limit` |
| `safety` | `command_timeout_ms`、`output_limit_bytes` |
| `compaction` | `auto`、`prune`、`threshold`、`recent_messages` |
| `workflow` | `require_todo_for_complex`、`auto_verify_after_edit`、`verify_commands` 等 |
| `permission` | 全局/工具级 `allow` / `ask` / `deny`，`bash` 子规则 |
| `agents` | `default`、`definitions`（各 agent 的 tools/permission 等） |
| `mcp` | `servers`（本地 stdio MCP） |
| `skills` | `paths`（SKILL.md 扫描目录） |
| `storage` | `base_dir`、`log_max_mb`、`cache_ttl_hours` |
| `instructions` | 额外指令文件列表 |

**环境变量覆盖**

| 变量 | 用途 |
|------|------|
| `AGENT_BASE_URL` | 模型服务 base URL |
| `AGENT_MODEL` | 默认模型 |
| `AGENT_API_KEY` | API 密钥；未设时可退回到 `DASHSCOPE_API_KEY` |
| `AGENT_WORKSPACE_ROOT` | 工作区根目录 |
| `AGENT_MAX_STEPS` | 单轮最大步数 |
| `AGENT_CACHE_PATH` | 缓存目录 |
| `NO_COLOR` / `AGENT_NO_COLOR` | 关闭 ANSI 颜色 |

---

## 测试与质量

```bash
make test          # 测试
make test-race     # 竞态检测
make coverage      # 覆盖率
make lint          # fmt + vet
make all           # lint + test + build
```

---

## CI / 发布

- **CI**：`.github/workflows/ci.yml` — gofmt、go vet、`go test -race -coverprofile`、多平台构建（Linux/macOS amd64、arm64）。
- **发布**：`.github/workflows/release.yml` — 推送 `v*` 标签时构建 Linux/macOS/Windows 二进制并上传 Release 资源。

---

## 文档

- `docs/requirements.md` — 需求说明总索引（按功能点拆分）
- `docs/design-v2.md` — 技术说明总索引（按模块拆分）
- `docs/requirements/` — 需求分册（交互、工具、安全、Agent、配置、异常、限制）
- `docs/technical/` — 技术分册（架构、编排、工具实现、权限、安全、存储、MCP、TUI、配置）
