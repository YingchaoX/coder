package i18n

// ZhCNMessages 简体中文消息目录
// ZhCNMessages Simplified Chinese message catalog
var ZhCNMessages = map[string]string{
	// TUI - 面板标题
	"panel.chat":  "对话",
	"panel.files": "文件",
	"panel.logs":  "日志",

	// TUI - 侧边栏
	"sidebar.context": "上下文",
	"sidebar.agent":   "智能体",
	"sidebar.model":   "模型",
	"sidebar.mcp":     "MCP",
	"sidebar.lsp":     "LSP",
	"sidebar.todo":    "待办",

	// TUI - 状态栏
	"status.workspace": "工作区",
	"status.ready":     "就绪",
	"status.streaming": "生成中...",
	"status.thinking":  "思考中...",

	// TUI - 输入
	"input.placeholder": "输入消息... (Shift+Enter 换行)",
	"input.submit_hint": "回车发送",

	// TUI - 快捷键提示
	"keys.tab":    "tab 切换面板",
	"keys.esc":    "esc 中断",
	"keys.ctrl_p": "ctrl+p 命令",

	// 审批
	"approval.title":           "需要审批",
	"approval.tool":            "工具: %s",
	"approval.reason":          "原因: %s",
	"approval.allow":           "允许",
	"approval.deny":            "拒绝",
	"approval.danger":          "⚠ 危险命令",
	"approval.prompt":          "允许此操作? [y/N]",
	"approval.allow_all":       "允许所有 (非危险)",
	"approval.denied":          "已被用户拒绝",
	"approval.callback_missing": "审批回调不可用",

	// 命令
	"cmd.help":     "显示可用命令",
	"cmd.new":      "创建新会话",
	"cmd.sessions": "列出会话",
	"cmd.exit":     "退出应用",

	// 错误
	"error.provider":   "模型服务错误: %s",
	"error.tool":       "工具错误: %s",
	"error.permission": "权限拒绝: %s",
	"error.session":    "会话错误: %s",

	// 上下文
	"context.tokens":    "Token: %d / %d (%.1f%%)",
	"context.messages":  "消息数: %d",
	"context.precise":   "精确",
	"context.estimated": "估算",

	// 压缩
	"compact.done":       "上下文已压缩",
	"compact.not_needed": "无需压缩",
	"compact.llm":        "使用 LLM 生成摘要",
	"compact.fallback":   "使用启发式摘要 (LLM 不可用)",

	// 会话
	"session.new":     "新会话: %s",
	"session.loaded":  "已加载会话: %s",
	"session.forked":  "已分叉会话: %s",
	"session.reverted": "已回退到 %d 条消息",
	"session.saved":   "会话已保存",
	"session.none":    "未找到会话",

	// 模型
	"model.current":  "当前模型: %s",
	"model.switched": "已切换模型: %s",

	// 智能体
	"agent.active": "活跃智能体: %s (%s)",

	// 工具结果
	"tool.start":   "正在执行 %s...",
	"tool.done":    "完成",
	"tool.blocked": "已阻止: %s",
	"tool.error":   "错误: %s",

	// 启动
	"startup.welcome":   "Coder 已启动，工作区: %s",
	"startup.session":   "会话: %s 智能体=%s",
	"startup.repl_mode": "REPL 模式运行中 (使用 --tui 启用完整 TUI)",
}
