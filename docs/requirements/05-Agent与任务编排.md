# 05. Agent 与任务编排

## 1. 内置 Agent
- `build`（primary）：默认主代理，负责交付改动；禁用 `todowrite`（不能设置 todos）和 `question`（不能向用户提问）。
- `plan`（primary）：规划代理，可联网与规划 todo，可向用户提问确认；禁用写改删相关工具（`write/edit/patch`）与 `task`。
- `general`（subagent）：通用子代理。
- `explore`（subagent）：只读探索，禁用 `edit/write/patch/bash/task/todowrite`。

## 2. Agent 生效方式
- Agent 决定“模型可见工具集合”和“执行前工具开关检查”。
- 即使模型返回禁用工具调用，也会在执行前被拦截为 blocked tool 结果。
- `/mode <build|plan>` 与 Agent 联动：切换模式会同步切换同名 Agent 与同名权限预设。

## 3. 子任务（task 工具）
- 输入：`agent + objective`。
- 仅允许 `mode=subagent` 的 agent。
- 子任务内部强制禁用：`task`、`todoread`、`todowrite`（防递归/串扰）。
- 输出：`summary`（字符串，父回合直接消费）。

## 4. Todo 机制
- 数据域：会话级（按 session ID 存取）。
- 状态：`pending` / `in_progress` / `completed`。
- 约束：最多 1 条 `in_progress`。
- 写入方式：`todowrite` 为整表替换。

## 5. 自动初始化 Todo（复杂任务）
触发条件：
- `workflow.require_todo_for_complex=true`
- 工具可用且未被当前 agent 禁用
- 输入被 `isComplexTask` 判定为复杂
- 当前会话无“未完成 todo”

约束：
- `build` 因禁用 `todowrite`，不会自动初始化 todos。
- todo 创建/更新仅允许在 `plan` 模式完成。

默认生成策略：
- 中文输入：`阅读代码并确认目标/验收标准 -> 实施修改 -> 验证总结`
- 英文输入：`Clarify scope -> Implement changes -> Validate and summarize`
- 若用户输入显式编号步骤（1./2./3.），优先按步骤生成。
