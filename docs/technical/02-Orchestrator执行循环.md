# 02. Orchestrator 执行循环（目标态）

## 1. 入口函数
- `RunInput(ctx, input, out)`：统一入口。
- `RunTurn(ctx, userInput, out)`：普通回合。
- `runBangCommand(ctx, rawInput, command, out)`：`!` 命令。
- `runSlashCommand(ctx, rawInput, command, args, out)`：`/` 命令。

## 2. 输入分发
1. trim 用户输入。
2. 前缀 `!`：命令模式。
3. 前缀 `/`：内建命令模式。
4. 其它：普通对话模式。

## 3. 普通回合主循环
1. 追加 user 消息。
2. 复杂任务判定 + todo 自动初始化（满足配置与状态条件时）。
3. 每步模型调用前执行 `maybeCompact`。
4. 调 Provider 获取响应（流式文本/推理/工具调用）。
5. 写入 assistant 消息。
6. 若无工具调用：根据模式与配置决定是否自动验证，然后结束回合。
7. 若有工具调用：逐个执行，写入 tool 消息，再进入下一步。
8. 达到步数上限返回 `step limit reached`。

### 3.1 推荐实现分层（重构目标）

`RunTurn` 应只保留流程编排职责，具体逻辑下沉为 pipeline step：

1. `prepareTurnState`：输入入栈、上下文刷新、回合状态初始化。
2. `chatStep`：调用 provider（含流式输出控制）。
3. `toolLoopStep`：工具调用循环（策略、审批、执行、回写）。
4. `autoVerifyStep`：自动验证与重试注入。
5. `persistStep`：session 落盘与收尾同步。

要求：

- 每个 step 尽量可单测。
- step 之间通过显式状态结构传递，不直接读写过多 orchestrator 字段。

## 4. 工具调用执行顺序
1. Agent 工具开关检查。
2. Policy 决策（`allow/ask/deny`）。
3. 工具级审批检查（`ApprovalRequest`）。
4. 聚合审批原因（策略层 + 工具层），**最多触发一次审批交互**。
5. 执行工具。
6. 结果写入 `tool` 消息并更新运行状态。

补充约束：

- 审批链路应支持“策略层 ask + 工具层 approval request”聚合为一次交互。
- tool 执行失败要标准化写回（`{"ok":false,"error":"..."}`）并继续后续流程判定。

## 5. 模式行为矩阵
- `build`
  - 目标：交付改动。
  - 允许读写与自动验证。
  - 禁用 `todowrite`（不能设置 todos）。
- `plan`
  - 目标：规划与分析。
  - 工具层阻断写改删：禁用 `edit/write/patch/task` 与变更型 git 工具。
  - 允许联网（`fetch`）与 todo 规划（`todoread/todowrite`）。
  - 启用 `question` 工具：模型可向用户提问选择题以澄清意图。
  - `bash` 使用白名单直通 + 非白名单审批：
    - 白名单（如 `ls/cat/grep/git status|diff|log/uname/pwd/id`）直接执行。
    - 其他命令默认 `ask`，走审批后执行。

## 6. `!` 命令分支
- 直接调用 `bash` 工具，不发模型请求。
- 经过与普通 `bash` 一致的策略与审批链：
  - Policy 决策（`allow/ask/deny`）
  - 工具层风险审批（如危险命令与重定向覆盖检查）
- 返回结构化执行结果（命令、exit code、stdout/stderr）。

## 7. `/` 命令分支
首发全量支持（与需求 02 子命令契约一致）：
- `/help`
- `/model <name>`
- `/permissions [preset]`
- `/mode <build|plan>`（或 `/build`、`/plan` 等价形式）
- `/tools`
- `/skills`
- `/todos`
- `/new`
- `/resume <session-id>`
- `/compact`
- `/diff`
- `/undo`

子命令契约摘要：
- `/help`：展示命令、Enter/Ctrl+D 输入规则、流式中断等说明。
- `/model <name>`：立即切换当前会话模型，并尝试持久化到 `./.coder/config.json`。
- `/permissions [preset]`：无参数时展示当前权限矩阵；有参数时切换权限预设（`build`、`plan`），并联动当前模式。
- `/mode <build|plan>`：切换当前模式并联动切换同名 Agent 与权限预设（或使用 `/build`、`/plan`）。
- `/tools`：展示当前可用工具列表/摘要。
- `/skills`：展示当前可用技能列表/摘要。
- `/todos`：仅查看当前会话 todo 列表（只读）。
- `/new`：创建新会话并切到空上下文输入态。
- `/resume <session-id>`：按会话 ID 恢复历史会话；若目标不存在，返回可读错误。
- `/compact`：强制执行一次上下文压缩并回显摘要。
- `/diff`：展示当前工作区改动差异摘要；可展开查看详细 diff。
- `/undo`：撤销“上一次用户输入对应整回合”产生的文件改动（基于回合级文件快照），不依赖 git。

行为约束：
- 未知命令返回可读错误（REPL 输出到 stdout）。
- `/model` 当前会话立即生效，并尝试写入 `./.coder/config.json`。
- `/resume` 仅接受 `<session-id>`，不支持索引或别名。
- `/undo` 仅回滚最近一回合中由文件写工具（`write/edit/patch`）影响的文件；无可回滚快照时返回可读提示。
- 当前版本仅支持线性会话，不支持 `/fork`。

## 8. 自动验证循环（严格白名单）
触发条件：
- 本回合执行过 `write` 或 `patch`。
- 编辑目标不全是文档类路径。
- `workflow.auto_verify_after_edit=true`。
- `bash` 工具可用。
- 尝试次数未超过 `max_verify_attempts`。
- 当前模式为 `build`，且用户明确要求或流程判定需要自动验证。

执行规则：
- 仅允许白名单命令：
  - `go test ./...`
  - `pytest -q`
  - `npm test -- --watch=false`
  - `pnpm test -- --watch=false`
  - `yarn test --watch=false`
  - `cargo test`
  - `mvn -q test`
  - `gradle test`
  - `./gradlew test`
- 若未命中白名单：拒绝执行并提示白名单限制。
- 若无可执行白名单命令：显式提示“未执行自动验证”。
- 可重试失败时注入修复提示继续回合；最多 `max_verify_attempts` 次。

工程约束：

- 自动验证开关与重试默认值应来源于配置层，orchestrator 不维护重复硬编码默认值。
- 是否触发验证的判定逻辑与命令选择逻辑应解耦，分别可测试。

## 9. 复杂任务判定
按需求固定规则执行：
1. 显式多目标枚举命中即复杂。
2. 文本长度阈值判断。
3. 关键词与分段阈值判断。

## 10. 回合持久化要求
- 每回合完成后持久化消息与关键状态。
- `/new`、`/resume` 会切换当前 session 上下文。
- 维护回合级 undo 快照栈（仅保留最近若干回合），供 `/undo` 精确回滚使用。

## 10.1 错误与取消语义

- `context canceled/deadline exceeded`：优先返回上下文错误，不包装为业务错误。
- provider/tool/approval 等非上下文错误：按链路分别包装，保留根因信息。
- Esc 取消后不自动回滚副作用，保持“已执行即生效”。

## 11. 运行态取消（Esc）
- REPL 在执行 `RunInput` 时提供可取消的 `context`；Esc 触发 `context cancel`。
- `RunTurn` 在模型调用、审批、tool 执行、自动验证链路中检测 `context canceled` 并立即终止，不继续后续自动化步骤。
- 审批等待场景中 Esc 语义为全局 Cancel（不是 `N`）。
- 取消不做回滚；已完成副作用保持，todo 状态维持最后一次持久化结果。

## 12. 兼容策略（重构期间）

- 对外行为保持稳定：`RunInput`、`RunTurn`、`/` 与 `!` 命令契约不变。
- 允许的小行为修复（例如错误文案更准确、边界值处理修复）需在变更说明中列出。
- 任何行为变化必须配套回归测试（优先 orchestrator 层单测）。
