# 09. TUI 交互与渲染实现（目标态）

## 1. 固定布局
- 主区：单时间线面板（用户消息、助手输出、工具摘要、日志事件统一按时间展示）。
- 侧栏：固定 5 个模块，仅展示：
  - `Context`
  - `Model`
  - `Todos`
  - `Tools`
  - `Skills`
- 顶部：无独立状态条。
- 输入区下方：仅 1 行合并状态信息，必须包含 cwd。

## 2. 交互键位
- `Enter`：发送输入
- `Shift+Enter`：换行
- `Tab`：按固定顺序切换模式：`plan -> default -> auto-edit -> yolo`
- `Esc`：中断流式显示（UI 级）
- `Ctrl+C`：退出

## 3. 输入分支
- `!` 前缀：命令模式，直走 `bash`。
- `/` 前缀：内建命令。
- 普通文本：进入模型-工具循环。

## 4. 流式渲染
- 助手文本按 chunk 增量渲染。
- `thinking`（reasoning）支持折叠/展开，默认折叠。
- 工具开始/结束事件展示摘要与耗时。

## 5. 工具结果渲染
- 时间线默认展示工具摘要。
- 支持展开查看完整参数与完整输出。
- `write/patch` diff 默认使用左右并排视图展示。
- 保留基础 diff 高亮（`+`/`-`/`@@`）。

## 6. 状态同步
TUI 与 Orchestrator 通过回调消息同步：
- 文本 chunk
- reasoning chunk
- tool start / tool done
- context token 使用率
- session/model 更新事件

## 7. 错误可见性
- 命令错误、工具错误、权限拒绝均需可读展示。
- 流式中断后保留已接收内容，且明确标注中断状态。
