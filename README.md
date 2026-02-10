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

# 启动（示例配置）
./bin/agent -config agent.config.json.example

# 指定工作区与语言
./bin/agent -config agent.config.json.example -cwd /path/to/project -lang zh-CN
```

**命令行参数**

| 参数 | 说明 |
|------|------|
| `-config` | 配置文件路径（JSON/JSONC） |
| `-cwd` | 工作区根目录，覆盖配置中的 `workspace_root` |
| `-lang` | UI 语言：`en`、`zh-CN`，空则自动检测 |

---

## TUI 与操作

- **布局**：聊天面板、文件面板、日志面板；侧边栏（会话、context 用量、agent、model）；底部状态栏。
- **输入**：多行输入；`@path` 提及（工作区内路径）；`!<shell 命令>` 直接执行并记入会话。
- **快捷键**（以实际代码为准）：`Tab` 切换面板、`Esc` 中断/返回、`Ctrl+P` 命令面板等。

**命令（通过命令面板或输入）**

| 命令 | 说明 |
|------|------|
| `/help` | 帮助 |
| `/new` | 新会话 |
| `/sessions` | 列出会话 |
| `/use <id>` | 切换会话 |
| `/fork <id>` | 从会话分叉 |
| `/revert <n>` | 回滚最近 n 条消息 |
| `/agent <name>` | 切换代理 |
| `/models [model_id]` | 查看/切换模型 |
| `/context` | 上下文长度（token 估算/限制/利用率） |
| `/tools` | 可用工具 |
| `/skills` | 技能列表 |
| `/todo` | 待办（checklist 展示） |
| `/summarize` | 生成会话摘要 |
| `/compact` | 手动压缩上下文 |
| `/config` | 当前配置 |
| `/mcp` | MCP 状态 |
| `/exit` | 退出 |

直接 Shell：输入以 `!` 开头，如 `! ls -la`，仍受危险命令审批策略约束。

---

## 配置

参考 `agent.config.json.example`。支持 JSON/JSONC。

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
| `AGENT_CONFIG_PATH` | 配置文件路径 |
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

- `docs/requirements.md` — 产品需求（v1/v2）
- `docs/USAGE.md` — 使用说明与示例
