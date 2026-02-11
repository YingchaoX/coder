# 05. Agent 与任务编排

## 1. 内置 Agent
- `build`（primary）：默认主代理，工具全开。
- `plan`（primary）：规划代理，禁用 `write/edit/patch`。
- `general`（subagent）：通用子代理。
- `explore`（subagent）：只读探索，禁用 `edit/write/patch/bash/task/todowrite`。

## 2. Agent 生效方式
- Agent 决定“模型可见工具集合”和“执行前工具开关检查”。
- 即使模型返回禁用工具调用，也会在执行前被拦截为 blocked tool 结果。
- `/mode` 与 Agent 解耦：`/mode` 不会自动切换 Agent。

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

默认生成策略：
- 中文输入：`阅读代码并确认目标/验收标准 -> 实施修改 -> 验证总结`
- 英文输入：`Clarify scope -> Implement changes -> Validate and summarize`
- 若用户输入显式编号步骤（1./2./3.），优先按步骤生成。
