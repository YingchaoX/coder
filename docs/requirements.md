# Coder 需求说明（按功能拆分）

## 1. 文档定位
本目录用于描述 **当前代码实现对应的产品需求基线**，并把需求拆成可独立细化的功能点。

适用场景：
- 你要按功能点继续细化需求（例如只改权限策略、只改交互体验）。
- 你要和技术实现逐条对齐，避免“文档写了但代码没做”或“代码做了文档没写”。

说明：
- 本文档基于仓库当前实现（TUI + Orchestrator + Tools + SQLite + Skills）。

## 2. 阅读顺序（建议）
1. `docs/requirements/00-文档范围与定位.md`
2. `docs/requirements/01-产品形态与界面.md`
3. `docs/requirements/02-交互逻辑与状态流.md`
4. `docs/requirements/03-工具能力清单.md`
5. `docs/requirements/04-安全与权限规则.md`
6. `docs/requirements/05-Agent与任务编排.md`
7. `docs/requirements/06-配置与运行规则.md`
8. `docs/requirements/07-异常场景与预期表现.md`

## 3. 与技术文档的关系
- 需求说明回答“是什么、怎么交互、什么情况下会发生什么”。
- 技术说明回答“代码如何实现、逻辑条件在哪里、边界如何处理”。

对应技术目录：`docs/technical/`
