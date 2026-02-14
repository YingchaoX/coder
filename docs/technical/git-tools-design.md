# Git 工具技术设计文档

## 1. 设计目标

为 Coder 添加原生 Git 工具支持，提升用户体验和安全性，替代现有的间接 `bash` 调用方式。

### 1.1 核心目标
- **更好的用户体验**：原生工具提供结构化输出和友好错误提示
- **安全性**：危险操作需要审批，阻止危险参数
- **降级策略**：非 git 环境提供清晰提示，不中断工作流
- **性能**：首次检测后缓存结果，避免重复执行

## 2. 架构设计

### 2.1 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                     Git 工具层                               │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐        │
│  │git_status│ │git_diff  │ │git_log   │ │git_add   │        │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘        │
│  ┌──────────┐                                               │
│  │git_commit│                                               │
│  └──────────┘                                               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   GitManager (单例)                          │
│  - 检测 git 可用性                                           │
│  - 检测是否为 git 仓库                                        │
│  - 缓存检测结果                                              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Workspace 安全层                          │
│  - 路径解析                                                  │
│  - 权限校验                                                  │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 组件说明

#### GitManager
负责 git 环境的检测和管理，使用 `sync.Once` 确保只检测一次。

```go
type GitManager struct {
    ws        *security.Workspace
    once      sync.Once
    available bool      // git 是否可用
    isRepo    bool      // 是否为 git 仓库
    version   string    // git 版本信息
}
```

#### Git 工具
所有 Git 工具遵循现有 `Tool` 接口规范：
- `Name()` - 返回工具名称
- `Definition()` - 返回工具定义（JSON Schema）
- `Execute()` - 执行工具逻辑
- `ApprovalRequest()` - （可选）审批请求

## 3. 工具详细设计

### 3.1 git_status

**功能**：查看工作区状态

**输入参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| short | boolean | 否 | 简洁格式输出 |

**输出**：
```json
{
  "ok": true,
  "content": "On branch main..."
}
```

**降级处理**：
- git 未安装：`{"ok": false, "error": "git not installed", "hint": "..."}`
- 非 git 仓库：`{"ok": false, "error": "not a git repository", "hint": "..."}`

### 3.2 git_diff

**功能**：查看文件变更

**输入参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| staged | boolean | 否 | 查看暂存区变更（git diff --staged） |
| path | string | 否 | 指定文件或目录 |

**输出**：
```json
{
  "ok": true,
  "content": "diff --git a/file.txt b/file.txt..."
}
```

### 3.3 git_log

**功能**：查看提交历史

**输入参数**：
| 参数 | 类型 | 必需 | 默认值 | 说明 |
|------|------|------|--------|------|
| limit | integer | 否 | 20 | 最大提交数 |
| oneline | boolean | 否 | false | 单行格式 |

**输出**：
```json
{
  "ok": true,
  "content": "commit abc123..."
}
```

### 3.4 git_add

**功能**：添加文件到暂存区

**输入参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| path | string | 是 | 文件路径，`.` 表示所有 |

**安全策略**：
- 需要审批（`ApprovalAware` 接口）
- 审批理由：`"git add modifies staging area"`

**输出**：
```json
{
  "ok": true,
  "files": ["file1.txt", "file2.txt"]
}
```

### 3.5 git_commit

**功能**：提交变更

**输入参数**：
| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| message | string | 是 | 提交信息 |

**安全策略**：
- 需要审批（`ApprovalAware` 接口）
- 检测危险参数：`--amend`, `--force`, `--no-verify`, `-n`, `--allow-empty`
- 如果提交信息包含危险标志，审批理由为 `"commit message may contain dangerous flags"`

**输出**：
```json
{
  "ok": true,
  "commit": "abc123..."
}
```

## 4. 安全设计

### 4.1 危险参数检测

使用正则表达式检测危险参数：

```go
var dangerousCommitArgs = regexp.MustCompile(`--amend|--force|--no-verify|-n\s|--allow-empty`)
```

### 4.2 审批流程

```
用户请求 git_commit
       │
       ▼
检测危险参数？
   │        │
   是      否
   │        │
   ▼        ▼
特殊审批   普通审批
"包含危险标志"  "创建新提交"
   │        │
   └────┬───┘
        ▼
   显示审批提示
        │
        ▼
   用户确认(y/n)
        │
        ▼
   执行 git 命令
```

### 4.3 路径安全

所有路径操作通过 `security.Workspace` 进行校验，确保不越界访问。

## 5. 降级策略

### 5.1 启动时检测

在 `bootstrap.go` 中初始化时检测：

```go
gitManager := tools.NewGitManager(ws)
if available, _, version := gitManager.Check(); !available {
    fmt.Fprintln(os.Stderr, "[Git] Git is not installed.")
} else if _, isRepo, _ := gitManager.Check(); !isRepo {
    fmt.Fprintln(os.Stderr, "[Git] Current directory is not a git repository.")
} else {
    fmt.Fprintf(os.Stderr, "[Git] Git detected: %s\n", version)
}
```

### 5.2 运行时降级

工具执行时返回结构化降级信息：

```go
if !available {
    return mustJSON(map[string]any{
        "ok":    false,
        "error": "git not installed",
        "hint":  "Install git to use git tools, or use 'bash' tool with git commands",
    }), nil
}
```

## 6. 性能考虑

### 6.1 缓存机制

`GitManager` 使用 `sync.Once` 确保 git 检测只执行一次：

```go
func (m *GitManager) Check() (available bool, isRepo bool, version string) {
    m.once.Do(func() {
        // 检测逻辑，只执行一次
    })
    return m.available, m.isRepo, m.version
}
```

### 6.2 超时控制

使用 `context.WithTimeout` 控制 git 命令执行时间，防止卡死。

## 7. 错误处理

### 7.1 错误类型

| 场景 | 错误信息 | 处理方式 |
|------|----------|----------|
| git 未安装 | `git not installed` | 提示安装或改用 bash |
| 非 git 仓库 | `not a git repository` | 提示初始化 git |
| git 命令失败 | 返回 stderr 内容 | 显示具体错误 |
| 危险参数 | 需要特殊审批 | 提示风险 |

### 7.2 错误格式

所有错误返回统一 JSON 格式：

```json
{
  "ok": false,
  "error": "错误描述",
  "hint": "解决建议（可选）"
}
```

## 8. 测试策略

### 8.1 单元测试

- `TestGitManager_NotGitRepo` - 非 git 仓库场景
- `TestGitManager_GitRepo` - 正常 git 仓库场景
- `TestGitStatusTool_NotRepo` - 状态工具降级测试
- `TestGitStatusTool_Repo` - 状态工具正常测试
- `TestGitCommitTool_DangerousArgs` - 危险参数检测

### 8.2 集成测试

- 完整工作流：add → status → diff → commit
- 审批流程测试
- 降级策略测试

## 9. 与现有系统的集成

### 9.1 工具注册

在 `bootstrap.go` 中注册到工具列表：

```go
gitManager := tools.NewGitManager(ws)
toolList := []tools.Tool{
    // ... 现有工具 ...
    tools.NewGitStatusTool(ws, gitManager),
    tools.NewGitDiffTool(ws, gitManager),
    tools.NewGitLogTool(ws, gitManager),
    tools.NewGitAddTool(ws, gitManager),
    tools.NewGitCommitTool(ws, gitManager),
}
```

### 9.2 权限配置

用户可以在配置文件中设置权限：

```json
{
  "permission": {
    "tools": {
      "git_status": "allow",
      "git_diff": "allow",
      "git_log": "allow",
      "git_add": "ask",
      "git_commit": "ask"
    }
  }
}
```

## 10. 未来扩展

### 10.1 可能的扩展

- `git_restore` - 恢复文件
- `git_stash` - 临时存储
- `git_branch` - 分支管理
- `git_checkout` - 切换分支

### 10.2 扩展原则

- 只读操作默认 `allow`
- 写入操作默认 `ask`
- 危险操作需要特殊审批
- 保持与现有工具一致的接口风格

## 11. 参考文档

- [Git 官方文档](https://git-scm.com/doc)
- [Coder 工具接口规范](./tool-interface.md)
- [Coder 安全设计文档](./security-design.md)

---

**版本**: v1.0  
**创建日期**: 2026-02-14  
**作者**: Coder Team
