## Coder

一个面向本地代码仓库的终端智能助手，提供「REPL 交互 + LLM 编排 + 安全工具链」，用于在无 IDE 插件、弱网络环境下完成代码阅读、修改与测试。

- **离线友好**：默认仅依赖操作系统基础能力（shell、文件系统）和私有 OpenAI 兼容模型服务。
- **极简交互**：内置终端 REPL，两行提示符，支持多种运行模式与内建命令。
- **安全工具链**：`read/write/patch/bash/todo/skill/task` 等工具均受权限策略与工作区边界约束。
- **可观测与可回滚**：自动 diff、`/undo` 回滚（基于 git）、SQLite 持久化会话与 todo。
- **可扩展 Skills**：通过本地 `SKILL.md` 与内置 skills 扩展特定工作流。

更多背景与细节可参考：
- 需求文档：`docs/requirements.md` 与 `docs/requirements/*.md`
- 技术设计：`docs/technical/*.md`

---

## 功能总览

- **交互层（REPL）**
  - 终端内双行提示符：第一行显示 `context: N tokens · model: xxx`，第二行 `[mode] /path/to/cwd> `。
  - 支持普通对话、`!` 命令模式、`/` 内建命令。
  - 流式输出：thinking、工具事件、diff、错误信息统一输出到 stdout。

- **编排层（Orchestrator）**
  - 统一入口 `RunInput`，根据前缀分发到普通回合 / `!` 命令 / `/` 命令。
  - 回合内执行模型-工具循环，支持自动压缩上下文、复杂任务自动 todo、自动验证。

- **工具层（Tools）**
  - 文件与代码：`read/write/list/glob/grep/patch`。
  - 命令执行：`bash`（带超时与输出截断），支持 `!` 直通模式与策略审批模式。
  - 任务与待办：`todo`（读写会话 todo），`task`（触发子 agent，返回 summary）。
  - 扩展：`skill` 工具用于列出/加载 Skills。

- **权限与安全**
  - `internal/security` + `internal/permission` 负责工作区边界、危险命令识别与策略决策。
  - 策略值 `allow/ask/deny`，支持按工具与 bash pattern 配置，`yolo + bash` 特殊全放行路径。

- **存储与会话**
  - 使用 SQLite 持久化消息、会话、todo 及权限日志，默认路径 `~/.coder`（可通过配置覆盖）。
  - `/new` 创建新会话、`/resume <session-id>` 恢复历史会话。

- **Skills 机制**
  - 内置 Skills 通过 `//go:embed` 打入二进制（例如 `create-skill`）。
  - 支持从本地目录（默认 `./.skills`、`~/.codex/skills`）加载用户自定义 `SKILL.md`。

---

## 架构概览

参考 `docs/technical/00-总体架构.md`，当前实现大致分为：

- **启动层**：`cmd/agent/main.go`
  - 解析命令行参数 `-config`、`-cwd`、`-lang`。
  - 调用 `config.Load` 加载配置。
  - 通过 `bootstrap.Build` 初始化 provider、工具注册表、存储、Orchestrator 等组件。
  - 创建 REPL loop 并运行。

- **交互层**：`internal/repl`
  - 实现提示符渲染、输入读取（TTY 与非 TTY）、历史记录、多行粘贴、Ctrl+D / Ctrl+C 行为。
  - 按行把用户输入转发给 Orchestrator，并将其返回的块（answer、command、tool 事件等）写回 stdout。

- **编排层**：`internal/orchestrator`
  - 维护对话消息与时间戳，估算上下文 token 用量，负责上下文压缩。
  - 根据用户输入分发到：
    - 普通回合：`RunTurn` → 模型调用 → 工具链执行 → 自动验证。
    - `!` 命令：`runBangCommand` 直接执行 `bash` 工具并渲染 `[COMMAND]` 块。
    - `/` 命令：`runSlashCommand` 执行内建命令，如 `/mode`、`/tools`、`/undo` 等。

- **模型层**：`internal/provider`
  - 对接 OpenAI 兼容接口（默认阿里云 DashScope），封装请求组装、流式聚合、错误与重试策略。

- **安全层**：`internal/security` + `internal/permission`
  - 路径约束（工作区内访问）、危险命令分析与审批策略、命令 allowlist。

- **上下文与压缩**：`internal/contextmgr`
  - 构造发送给模型的消息列表、估算 token、按阈值和策略进行上下文裁剪。

- **存储层**：`internal/storage`
  - SQLite schema 初始化与迁移，会话与消息 CRUD、todo 存储。

- **Skills 扩展层**：`internal/skills`
  - 发现本地与内置 skills，解析 frontmatter，提供给 `skill` 工具与 Orchestrator 使用。

---

## 安装与构建

- **环境要求**
  - Go `1.24` 及以上（`go.mod` 中为 `go 1.24.2`）。
  - 可访问一个 OpenAI 兼容的模型服务（默认值指向 DashScope）。
  - 本地文件系统与 shell（例如 `/bin/sh`）。

- **获取代码**

```bash
git clone <your-repo-url> coder
cd coder
```

- **构建二进制**
2026-02-11

```bash
go build -o coder ./cmd/agent
```

---

## 运行示例

### 1. 准备配置

配置由 `internal/config` 加载，默认查找顺序为：

1. 全局配置（按顺序合并）：
   - `~/.coder/config.json`
2. 项目级配置（先由 `-config` 或环境变量覆盖路径，再由仓库根目录自动发现，默认 `.coder/config.json`）：
   - CLI：`-config /path/to/config.json`
   - 环境变量：`AGENT_CONFIG_PATH`
   - 自动发现文件（按顺序）：`agent.config.json`、`.coder/config.json`
3. 环境变量覆盖：
   - `AGENT_BASE_URL`、`AGENT_MODEL`、`AGENT_API_KEY` / `DASHSCOPE_API_KEY`
   - `AGENT_WORKSPACE_ROOT`、`AGENT_MAX_STEPS`、`AGENT_CACHE_PATH`

一个最小可用的项目级配置示例（`./.coder/config.json`）：

```json
{
  "provider": {
    "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "model": "qwen3-coder-30b-a3b-instruct",
    "api_key": "YOUR_API_KEY"
  },
  "runtime": {
    "workspace_root": "/path/to/your/workspace"
  }
}
```

> 完整字段含义与默认值请参考 `internal/config/config.go` 与 `docs/technical/10-配置加载与覆盖.md`。

### 2. 启动 REPL

在项目根目录运行：

```bash
./coder -cwd /path/to/your/workspace -lang zh-CN
```

- `-cwd`：覆盖工作区根路径；未指定时，优先使用配置中的 `runtime.workspace_root`，否则默认为当前工作目录。
- `-lang`：界面语言，支持 `en` 与 `zh-CN`。
- `-config`：可显式指定配置文件路径（JSON）。

看到提示符类似：

```text
context: 0 tokens · model: qwen2.5-coder-32b-instruct
[default] /path/to/your/workspace>
```

即表示 REPL 已启动成功。

---

## 基本交互与命令

### 普通输入

- 直接输入问题或指令（不以 `!` 或 `/` 开头），按 Enter 发送。
- Orchestrator 会进入“模型-工具循环”，根据需要自动调用 `read/write/patch/bash/todo/skill/task` 等工具。

### `!` 命令模式（直通 bash）

- 行首以 `!` 开头时，走命令模式，例如：

```text
! ls
! go test ./...
```

- 特点：
  - 不调用模型，视为“用户自己在 shell 中执行”。
  - 结果以 `[COMMAND]` 区块返回，包含命令回显、exit code、持续时间、stdout/stderr 等。
  - 仍受工作区边界与 `bash` 工具内部超时/输出截断约束。

### `/` 内建命令

`/` 命令由 `internal/orchestrator` 的 slash 分支解析，当前实现包括（详见 `docs/technical/02-Orchestrator执行循环.md`）：

- `/help`：展示基本使用说明与命令列表。
- `/model <name>`：切换当前会话模型，并尝试写入 `./.coder/config.json`。
- `/permissions [preset]`：展示或切换权限预设（如 `strict`、`balanced`、`auto-edit`、`yolo`）。
- `/mode <name>`：切换模式，也可使用 `/plan`、`/default`、`/auto-edit`、`/yolo` 快捷命令。
- `/tools`：展示当前注册与可用的工具列表。
- `/skills`：展示当前可用的 Skills 列表。
- `/todos`：查看当前会话 todo 列表（只读）。
- `/new`：创建新会话。
- `/resume <session-id>`：按会话 ID 恢复历史会话。
- `/compact`：立刻执行一次上下文压缩，并回显摘要。
- `/diff`：展示当前工作区改动摘要与 diff。
- `/undo`：撤销上一次用户输入对应整回合产生的文件改动（仅在存在 git 仓库且 git 可用时启用）。

---

## 模式与权限策略

### 模式（REPL /mode）

当前支持四种模式（`internal/orchestrator` 中的 `mode` 字段）：

- `plan`：仅规划与拆解，默认禁写入（`write/patch`）与高风险命令，不启用自动验证。
- `default`：均衡模式，写入与高风险操作按策略 `allow/ask/deny` 执行，自动验证仅在用户明确要求时触发。
- `auto-edit`：偏向交付改动，允许主动读写与自动验证，支持失败后注入修复提示并重试。
- `yolo`：高自治模式，对 `bash` 走全放行路径，但仍有工作区与基础安全约束。

模式会直接体现在提示符第二行的 `[mode]` 部分。

### 权限策略与审批

- 权限配置结构见 `PermissionConfig`（`internal/config/config.go`）：
  - 支持为 `read/write/list/glob/grep/patch/todoread/todowrite/skill/task` 与 `bash` pattern 分别设置策略。
  - 策略值为 `allow/ask/deny`。
  - `bash` 支持按 pattern 配置，例如默认允许 `ls *`、`go test *` 等安全命令。
- 策略为 `ask` 时：
  - 在 stdout 展示待执行命令与风险说明。
  - 交互式读取 `y/n/always`（策略层 ask）或 `y/n`（工具层危险命令审批）。
- `permission.command_allowlist`：
  - 记录已被标记为 “always allow” 的命令名。
  - 可通过交互选择 `always`，也可由 `WriteCommandAllowlist` 写入。

---

## Tools 与 Skills

### 主要工具一览

> 工具接口统一通过 `internal/tools.Registry` 暴露给 Orchestrator 与 provider。

- **文件/代码相关**
  - `read`：读取文件片段，用于代码浏览与上下文收集。
  - `write`：写入文件（带 diff 预览），遵循权限策略与自动验证规则。
  - `patch`：应用 patch（统一 diff），支持 hunk 校验与错误回退。
  - `list` / `glob` / `grep`：列目录、按模式匹配与搜索文本。
- **命令执行**
  - `bash`：执行 shell 命令，内置超时与输出截断；通过 JSON 结果返回 stdout/stderr/exit_code 等。
- **任务与待办**
  - `todo`：读写当前会话 todo 列表，支持 `pending/in_progress/completed` 等状态。
  - `task`：运行子 agent 任务：
    - 参数：`agent`、`objective`（必填）与可选 `prompt`。
    - 返回：包含 `ok/agent/summary` 字段的 JSON。
- **技能加载**
  - `skill`：列出或加载 Skills 内容，供模型在规划阶段自动调用。

### Skills 加载规则

- 用户 skills 路径由 `skills.paths` 配置（默认为若干本地目录），按路径扫描 `SKILL.md`。
- 内置 skills 存放于 `internal/skills/builtin`，通过 `//go:embed` 嵌入。
- 合并顺序：
  - 先加载用户路径，再加载内置；同名时用户版本优先。
  - 多路径下同名冲突会在启动期报错。

---

## 存储与会话管理

- 存储配置见 `StorageConfig`：
  - 默认 `base_dir = "~/.coder"`，在 `normalize` 阶段展开为绝对路径。
  - 还包括日志滚动上限 `log_max_mb` 与缓存 TTL（小时）。
- SQLite schema 与操作在 `internal/storage` 中实现：
  - 支持会话与消息 CRUD、todo 存储与迁移。
  - `migrate.go` 负责 schema 迁移逻辑。

命令 `/new` 与 `/resume` 通过 Orchestrator 与 Storage 协作完成新会话创建和历史会话恢复。

---

## 开发与测试

- **运行测试**

```bash
go test ./...
```

- **覆盖率目标与策略**
  - 见 `docs/testing/00-测试设计与TDD计划.md`。
  - 目标：整体覆盖率 ≥ 60%，核心包（`internal/orchestrator`、`internal/tools`、`internal/security`、`internal/permission`、`internal/config` 等）有更高包级阈值。
  - 推荐命令：

```bash
go test ./... -count=1
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

- **主要目录结构**

- `cmd/agent`：入口二进制。
- `internal/orchestrator`：编排器与模式、工具循环实现。
- `internal/repl`：终端 REPL 交互与渲染。
- `internal/tools`：工具实现与注册表。
- `internal/security` / `internal/permission`：安全与权限策略。
- `internal/config`：配置加载、合并与归一化。
- `internal/storage`：SQLite 存储层。
- `internal/skills`：Skills 发现与加载。
- `docs/requirements`：需求与产品行为说明。
- `docs/technical`：技术设计文档。

---

## 后续工作

本仓库仍在持续演进中，若发现 README 与实现或文档不一致，可按以下方式对齐：

- 以 `docs/requirements/*.md` 与 `docs/technical/*.md` 为准，更新 README 中对应说明。
- 按 `docs/testing/00-测试设计与TDD计划.md` 补充或调整测试用例。
- 在提交前运行 `go test ./...` 与覆盖率检查，确保行为与文档保持一致。
