# 测试设计与 TDD 计划（目标覆盖率 60%+）

## 1. 目标与范围
- 目标：在当前约 40% 覆盖率基础上，将项目整体测试覆盖率提升并稳定在 **60% 以上**。
- 方法：采用测试驱动开发（TDD），以“需求文档 + 技术设计”为唯一验收基线。
- 范围：`internal/*` 核心业务模块（不含第三方库与纯 UI 样式细节）。

## 2. 测试策略总览
- 测试金字塔：
  - 单元测试（约 70%）：覆盖核心分支、边界、错误路径。
  - 组件/集成测试（约 20%）：跨模块交互，如 Orchestrator + Registry + Policy。
  - 端到端场景测试（约 10%）：关键用户路径（命令、模式、自动验证、权限）。
- 覆盖重点：优先覆盖“高风险逻辑 + 高变更频率 + 需求强约束”代码。
- 非目标：不追求 UI 像素级快照测试，不追求 100% 覆盖率。

## 3. 覆盖率目标拆解
- 全局目标：`go test ./... -cover` 总覆盖率 >= 60%。
- 包级最低建议阈值（第一阶段）：
  - `internal/orchestrator` >= 70%
  - `internal/tools` >= 70%
  - `internal/security` >= 75%
  - `internal/permission` >= 80%
  - `internal/config` >= 75%
  - `internal/contextmgr` >= 70%
  - `internal/storage` >= 65%
  - `internal/provider` >= 55%
  - `internal/skills` >= 70%
  - `internal/tui` >= 45%（以交互状态机为主，不强求渲染细节）

## 4. 需求追踪测试矩阵
- `docs/requirements/01-产品形态与界面.md`
  - 测试项：Tab 模式轮转顺序、Enter/Shift+Enter、Esc 中断标记、状态栏含 cwd。
- `docs/requirements/02-交互逻辑与状态流.md`
  - 测试项：`!` 与 `/` 分支分发、普通回合工具循环、步数上限、复杂任务 todo 初始化、自动验证触发条件。
- `docs/requirements/03-工具能力清单.md`
  - 测试项：每个工具的成功/失败/边界行为；`task` 返回 `summary` 契约。
- `docs/requirements/04-安全与权限规则.md`
  - 测试项：Policy allow/ask/deny、bash pattern 最长匹配、风险审批、yolo+bash 放行例外。
- `docs/requirements/05-Agent与任务编排.md`
  - 测试项：agent 工具开关拦截、subagent 限制、task 禁递归。
- `docs/requirements/06-配置与运行规则.md`
  - 测试项：配置发现顺序、环境变量覆盖、归一化、`/model` 持久化策略。
- `docs/requirements/07-异常场景与预期表现.md`
  - 测试项：空输入、未知命令、provider 中断、超时、路径越界、step limit reached。

## 5. 按模块 TDD 设计

## 5.1 `internal/orchestrator`（P0）
- Red：先写失败测试
  - `RunInput` 三分支：普通、`!`、`/`。
  - 回合终止条件：无工具调用、步数上限、provider 错误。
  - 自动验证白名单：只允许白名单命令；无命令时必须输出“未执行自动验证”。
  - 模式行为：`plan/default/auto-edit/yolo` 差异，`yolo+bash` 全放行。
  - `/` 命令全集：`/help /model /permissions /new /resume /compact /diff /review /undo`。
- Green：最小实现通过测试。
- Refactor：抽取命令分发、自动验证选择器、模式判定函数，减少重复。

## 5.2 `internal/tools`（P0）
- `read/write/list/glob/grep/patch/bash/todoread/todowrite/skill/task` 全覆盖。
- 关键边界：
  - 路径越界与符号链接逃逸。
  - `patch` hunk mismatch。
  - `bash` 超时、输出截断、空命令。
  - `todowrite` 仅一个 `in_progress`。
  - `task` 必填 `agent/objective`，输出包含 `summary`。

## 5.3 `internal/security` + `internal/permission`（P0）
- `Workspace.Resolve` 越界拒绝与合法路径通过。
- `AnalyzeCommand` 风险识别：命令替换、反引号、危险命令、词法错误。
- Policy 规则优先级：默认规则、工具规则、bash 最长 pattern 命中。

## 5.4 `internal/config`（P0）
- 配置发现顺序：`./.coder/config.json` -> `~/.coder/config.json` -> default。
- JSONC 解析。
- 环境变量覆盖（含非法值报错）。
- normalize：路径展开、去重、默认值补齐。

## 5.5 `internal/contextmgr`（P1）
- StaticMessages 拼装顺序稳定。
- token 估算：启发式回退可用。
- compaction 触发阈值与保留消息数量。
- tool 输出裁剪不丢关键信息。

## 5.6 `internal/storage`（P1）
- SQLite schema 初始化。
- session/messages/todos CRUD。
- SaveMessages 覆盖写。
- 迁移逻辑：可迁移、重复 session 跳过、部分失败不中断。

## 5.7 `internal/provider`（P1）
- request 组装测试（model/messages/tools）。
- stream 聚合测试（content/reasoning/tool_calls）。
- 错误策略：首包失败返回错误、半路失败返回部分内容、可重试错误重试。

## 5.8 `internal/skills`（P2）
- skill 发现、frontmatter 解析、重名冲突。
- load 不存在 skill 错误路径。

## 5.9 `internal/tui`（P2）
- Update 状态机测试（键位、流式消息、工具消息、错误消息）。
- 模式切换与状态栏信息更新。
- 不做复杂渲染快照，仅断言关键状态与关键文案存在。

## 6. 端到端场景用例（高价值）
- 场景 1：普通需求 -> 工具链执行 -> 输出答案 -> 持久化消息。
- 场景 2：`!` 命令在非 yolo 触发审批；在 yolo 直接执行。
- 场景 3：编辑代码后自动验证，失败注入修复提示并重试。
- 场景 4：`/undo` 在无 git 环境返回不可用提示。
- 场景 5：复杂任务自动生成 todo，后续可读写一致。

## 7. TDD 迭代节奏（建议）
- 迭代 1（P0）：`orchestrator + tools + security + permission + config`
  - 目标：覆盖率由 40% 提升到 50%+。
- 迭代 2（P1）：`contextmgr + storage + provider`
  - 目标：覆盖率提升到 58%+。
- 迭代 3（P2）：`skills + tui + 场景回归`
  - 目标：覆盖率稳定在 60%+ 并持续回归。

## 8. 覆盖率门禁与质量门禁
- 本地门禁：
  - `go test ./... -count=1`
  - `go test ./... -coverprofile=coverage.out`
  - `go tool cover -func=coverage.out` 校验总覆盖率 >= 60%
- CI 门禁：
  - PR 必须通过单测。
  - 覆盖率低于基线（60%）直接失败。
  - 对核心包（orchestrator/tools/security/permission/config）设置包级阈值告警。

## 9. 测试编码规范
- 表驱动测试优先（输入/期望/错误分支）。
- 每个缺陷修复必须补回归测试（先 Red 后 Green）。
- 对外部依赖全部 mock/stub（provider、shell、时间、文件系统边界）。
- 错误消息断言使用关键片段，避免脆弱的全字符串匹配。

## 10. 交付物
- 新增/补齐各模块 `_test.go`。
- 覆盖率报告脚本或 Make 目标（可选：`make coverage`）。
- 本文档作为后续 TDD 执行基线，需求或设计变化时同步更新。
