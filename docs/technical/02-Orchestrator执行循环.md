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

## 4. 工具调用执行顺序
1. Agent 工具开关检查。
2. Policy 决策（`allow/ask/deny`）。
3. 工具级审批检查（`ApprovalRequest`）。
4. 执行工具。
5. 结果写入 `tool` 消息并更新运行状态。

例外：`yolo + bash`
- 跳过策略拦截与风险审批，直接执行。

## 5. 模式行为矩阵
- `plan`
  - 目标：方案与拆解。
  - 默认禁写入链路（`write/patch`）与副作用命令。
  - 不触发自动验证。
- `default`
  - 目标：均衡执行。
  - 写入与高风险操作按策略审批。
  - 自动验证仅在用户明确要求时触发。
- `auto-edit`
  - 目标：交付改动。
  - 允许主动读写、自动验证、失败修复重试。
- `yolo`
  - 目标：高自治执行。
  - `bash` 全放行（包含高风险命令）。

## 6. `!` 命令分支
- 直接调用 `bash` 工具，不发模型请求。
- 非 `yolo` 模式仍经过策略与审批。
- 返回结构化执行结果（命令、exit code、stdout/stderr）。

## 7. `/` 命令分支
首发全量支持：
- `/help`
- `/model <name>`
- `/permissions [preset]`
- `/new`
- `/resume <session-id>`
- `/compact`
- `/diff`
- `/review`
- `/undo`

行为约束：
- 未知命令返回可读错误。
- `/model` 当前会话立即生效，并尝试写入 `./.coder/config.json`。
- `/resume` 仅接受 `<session-id>`。
- `/undo` 运行时检测 git；无 git 或非仓库则返回不可用提示。

## 8. 自动验证循环（严格白名单）
触发条件：
- 本回合执行过 `write` 或 `patch`。
- 编辑目标不全是文档类路径。
- `workflow.auto_verify_after_edit=true`。
- `bash` 工具可用。
- 尝试次数未超过 `max_verify_attempts`。
- 当前模式为 `auto-edit` 或 `yolo`，或 `default` 下用户明确要求。

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

## 9. 复杂任务判定
按需求固定规则执行：
1. 显式多目标枚举命中即复杂。
2. 文本长度阈值判断。
3. 关键词与分段阈值判断。

## 10. 回合持久化要求
- 每回合完成后持久化消息与关键状态。
- `/new`、`/resume` 会切换当前 session 上下文。
