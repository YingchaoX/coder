# 08. Skills 实现（目标态）

## 1. 目标与范围
- Skills 默认开启。
- 来源：**内置技能**（随二进制嵌入）+ **本地路径**（`skills.paths` 下的 `SKILL.md`）。
- 不依赖网络下载，不依赖 MCP。

## 2. 发现机制
- **合并顺序**：先按 `skills.paths` 发现用户技能，再合并内置技能；若某 name 已在用户路径中存在，则保留用户版本（用户可覆盖预装）。
- **路径发现**：启动时扫描 `skills.paths` 指定目录；发现规则为目录下 `SKILL.md`。
- **内置发现**：内置技能来自代码库 `internal/skills/builtin/`，经 `//go:embed` 打入二进制；合并时仅添加 name 尚未存在的项。
- 元数据来源：
  - 优先 frontmatter（`name/description`）
  - 回退目录名 + 首段描述
- **同名冲突**：仅发生在路径扫描内部（多路径下同名）时，启动期报错并提示冲突来源；内置与路径同名时路径优先。

## 2.1 预装技能（内置）技术要点
- **存放位置**：`internal/skills/builtin/<skill-name>/SKILL.md`，内容为 coder 约定（路径为 `~/.coder/skills/`、`.coder/skills/`，无 Cursor 表述）。
- **嵌入方式**：在 `internal/skills` 包内用 `//go:embed builtin/create-skill/SKILL.md` 嵌入单文件（或 `builtin/*` 嵌入目录）；不依赖运行时文件系统。
- **加载**：解析嵌入内容得到 name/description 与正文；Manager 持有 `builtinContent map[string]string`（或等价），`Load(name)` 时若为内置则直接返回内存内容，否则 `os.ReadFile(item.Path)`。
- **预装列表**：至少包含 `create-skill`，用于引导用户通过 prompt 创建新技能。

## 3. 工具契约
`skill` 工具支持：
- `action=list`：返回技能列表（`name/description`）
- `action=load`：返回技能全文

## 4. 触发路径
- 显式触发：模型直接调用 `skill` 工具。
- 自动触发：模型在规划阶段基于任务语义决定是否调用 skill。

说明：是否触发由模型决策；系统只提供可见技能清单与稳定调用路径。

## 5. 权限与审批
- `deny`：直接拒绝。
- `ask`：显式 `load` 可走审批。
- 自动触发路径可免交互审批，但不豁免 `deny`。

## 6. 失败回退
- 技能不存在或加载失败时：返回可读错误，并回退到无 skill 路径继续执行。
- skills 扫描失败时：系统降级运行，不阻塞核心对话与工具链。
