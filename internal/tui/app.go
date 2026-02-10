package tui

import (
	"context"
	"fmt"
	"strings"

	"coder/internal/i18n"
	"coder/internal/orchestrator"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PanelID 面板标识
// PanelID identifies a panel
type PanelID int

const (
	PanelChat PanelID = iota
	PanelFiles
	PanelLogs
)

// --- Tea Messages ---

// TextChunkMsg 流式文本块
// TextChunkMsg is a streaming text chunk
type TextChunkMsg struct{ Text string }

// ReasoningChunkMsg 推理文本块
// ReasoningChunkMsg is a reasoning text chunk
type ReasoningChunkMsg struct{ Text string }

// ToolStartMsg 工具开始执行
// ToolStartMsg indicates tool execution started
type ToolStartMsg struct{ Name, Summary string }

// ToolDoneMsg 工具执行完成
// ToolDoneMsg indicates tool execution done
type ToolDoneMsg struct{ Name, Summary string }

// TurnDoneMsg 回合完成
// TurnDoneMsg indicates a turn is done
type TurnDoneMsg struct {
	Content string
	Err     error
}

// StreamingStartMsg 开始流式输出
// StreamingStartMsg indicates streaming has started
type StreamingStartMsg struct{}

// ContextUpdateMsg 上下文信息更新
// ContextUpdateMsg carries updated context info
type ContextUpdateMsg struct {
	Tokens  int
	Limit   int
	Percent float64
}

// SessionInfoMsg 会话信息更新
// SessionInfoMsg carries session info
type SessionInfoMsg struct {
	ID    string
	Agent string
	Model string
}

// SendInputMsg 表示有新的用户输入需要发送给 orchestrator
// SendInputMsg carries a new user input to be processed by the orchestrator
type SendInputMsg struct {
	Text string
}

// TurnErrorMsg 表示一次对话回合出错
// TurnErrorMsg indicates an error from a turn
type TurnErrorMsg struct {
	Err error
}

// App Bubble Tea 主 Model
// App is the main Bubble Tea model
type App struct {
	// 布局 / Layout
	width  int
	height int

	// 面板 / Panels
	activePanel PanelID
	chatView    viewport.Model
	filesView   viewport.Model
	logsView    viewport.Model

	// 输入 / Input
	input        textarea.Model
	inputFocused bool

	// 侧边栏数据 / Sidebar data
	agentName  string
	modelName  string
	sessionID  string
	tokens     int
	tokenLimit int
	tokenPct   float64
	todoItems  []string

	// 内容缓冲（使用指针避免 strings.Builder 被复制） / Content buffers (use pointers to avoid copying strings.Builder)
	chatContent *strings.Builder
	logContent  *strings.Builder
	fileContent *strings.Builder

	// 状态 / State
	streaming       bool
	streamBuffer    *strings.Builder
	lastError       string
	workspace       string
	hadStreamChunks bool

	// 配置 / Config
	theme  Theme
	keys   KeyMap
	locale *i18n.I18n
	// 编排器 / Orchestrator
	orch *orchestrator.Orchestrator
}

// NewApp 创建 TUI 应用
// NewApp creates a new TUI application
func NewApp(workspace, agent, model, sessionID string, orch *orchestrator.Orchestrator) App {
	ta := textarea.New()
	ta.Placeholder = i18n.T("input.placeholder")
	ta.CharLimit = 8192
	ta.SetHeight(3)
	ta.Focus()

	theme := DarkTheme()

	return App{
		activePanel:  PanelChat,
		input:        ta,
		inputFocused: true,
		agentName:    agent,
		modelName:    model,
		sessionID:    sessionID,
		workspace:    workspace,
		tokenLimit:   24000,
		theme:        theme,
		keys:         DefaultKeyMap(),
		locale:       i18n.Global(),
		orch:         orch,

		chatContent:  &strings.Builder{},
		logContent:   &strings.Builder{},
		fileContent:  &strings.Builder{},
		streamBuffer: &strings.Builder{},
	}
}

func (a App) Init() tea.Cmd {
	return textarea.Blink
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "tab":
			a.activePanel = (a.activePanel + 1) % 3
			return a, nil
		case "esc":
			if a.streaming {
				a.streaming = false
				a.appendLog("⚠ Generation interrupted")
			}
			return a, nil
		case "enter":
			// Enter 发送消息，Shift+Enter 由 textarea 处理换行
			if a.inputFocused {
				text := strings.TrimSpace(a.input.Value())
				if text != "" {
					cmds = append(cmds, a.handleSendInput(text))
				}
				return a, tea.Batch(cmds...)
			}
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.relayout()
		return a, nil

	case TextChunkMsg:
		a.streaming = true
		a.hadStreamChunks = true
		if a.streamBuffer == nil {
			a.streamBuffer = &strings.Builder{}
		}
		a.streamBuffer.WriteString(msg.Text)
		a.updateChatFromStream()
		return a, nil

	case ReasoningChunkMsg:
		// 推理内容追加到日志面板 / Reasoning appended to logs
		a.appendLog("💭 " + msg.Text)
		return a, nil

	case ToolStartMsg:
		a.appendChat(fmt.Sprintf("\n🔧 %s %s", msg.Name, msg.Summary))
		a.appendLog(fmt.Sprintf("[TOOL] %s: %s", msg.Name, msg.Summary))
		return a, nil

	case ToolDoneMsg:
		a.appendToolDone(msg.Name, msg.Summary)
		return a, nil

	case TurnDoneMsg:
		a.streaming = false
		if msg.Err != nil {
			a.lastError = msg.Err.Error()
			a.appendChat("\n❌ " + msg.Err.Error())
		} else {
			// 如果这一轮没有收到任何流式 chunk，则把最终内容一次性追加到聊天面板。
			// For non-streaming responses (no chunks received), append the final content once.
			if !a.hadStreamChunks && strings.TrimSpace(msg.Content) != "" {
				a.appendAssistantMarkdown(msg.Content)
			} else if a.hadStreamChunks {
				// 流式内容已经在 chat 中，这里把缓冲刷入正式内容。
				// Streaming content already displayed; merge buffer into persistent chat.
				a.flushStreamToChat()
			}
		}
		a.hadStreamChunks = false
		if a.streamBuffer != nil {
			a.streamBuffer.Reset()
		}
		return a, nil

	case StreamingStartMsg:
		a.streaming = true
		if a.streamBuffer == nil {
			a.streamBuffer = &strings.Builder{}
		}
		a.streamBuffer.Reset()
		return a, nil

	case ContextUpdateMsg:
		a.tokens = msg.Tokens
		a.tokenLimit = msg.Limit
		a.tokenPct = msg.Percent
		return a, nil

	case SessionInfoMsg:
		a.sessionID = msg.ID
		a.agentName = msg.Agent
		a.modelName = msg.Model
		return a, nil
	case TurnErrorMsg:
		if msg.Err != nil {
			a.lastError = msg.Err.Error()
			a.appendChat("\n❌ " + msg.Err.Error())
		}
		return a, nil
	}

	// 更新输入区 / Update input area
	if a.inputFocused {
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Initializing..."
	}

	// 计算布局尺寸 / Calculate layout dimensions
	sidebarWidth := a.width * 25 / 100
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 40 {
		sidebarWidth = 40
	}
	if a.width < 80 {
		sidebarWidth = 0
	}

	mainWidth := a.width - sidebarWidth
	if sidebarWidth > 0 {
		mainWidth-- // border
	}

	inputHeight := 5
	statusHeight := 1
	tabHeight := 1
	panelHeight := a.height - inputHeight - statusHeight - tabHeight

	if panelHeight < 3 {
		panelHeight = 3
	}

	// 构建各部分 / Build components
	tabs := a.renderTabs(mainWidth)
	panel := a.renderActivePanel(mainWidth, panelHeight)
	inputBox := a.renderInput(mainWidth, inputHeight)
	statusBar := a.renderStatusBar(a.width)

	// 左侧主区域 / Left main area
	main := lipgloss.JoinVertical(lipgloss.Left, tabs, panel, inputBox)

	// 右侧侧边栏 / Right sidebar
	if sidebarWidth > 0 {
		sidebar := a.renderSidebar(sidebarWidth, a.height-statusHeight)
		main = lipgloss.JoinHorizontal(lipgloss.Top, main, sidebar)
	}

	// 底部状态栏 / Bottom status bar
	return lipgloss.JoinVertical(lipgloss.Left, main, statusBar)
}

// --- 内部方法 / Internal methods ---

func (a *App) relayout() {
	mainWidth := a.width
	panelHeight := a.height - 8

	if panelHeight < 3 {
		panelHeight = 3
	}

	a.chatView = viewport.New(mainWidth, panelHeight)
	if a.chatContent != nil {
		a.chatView.SetContent(a.chatContent.String())
	}

	a.filesView = viewport.New(mainWidth, panelHeight)
	if a.fileContent != nil {
		a.filesView.SetContent(a.fileContent.String())
	}

	a.logsView = viewport.New(mainWidth, panelHeight)
	if a.logContent != nil {
		a.logsView.SetContent(a.logContent.String())
	}

	a.input.SetWidth(mainWidth - 4)
}

func (a *App) appendChat(text string) {
	if a.chatContent == nil {
		a.chatContent = &strings.Builder{}
	}
	a.chatContent.WriteString(text + "\n")
	a.chatView.SetContent(a.chatContent.String())
	a.chatView.GotoBottom()
}

func (a *App) appendLog(text string) {
	if a.logContent == nil {
		a.logContent = &strings.Builder{}
	}
	a.logContent.WriteString(text + "\n")
	a.logsView.SetContent(a.logContent.String())
}

func (a *App) updateChatFromStream() {
	// 在流式输出时，显示已有内容 + 流式缓冲
	base := ""
	if a.chatContent != nil {
		base = a.chatContent.String()
	}
	content := base
	if a.streamBuffer != nil && a.streamBuffer.Len() > 0 {
		content += a.streamBuffer.String()
	}
	a.chatView.SetContent(content)
	a.chatView.GotoBottom()
}

func (a *App) flushStreamToChat() {
	if a.streamBuffer != nil && a.streamBuffer.Len() > 0 {
		a.appendAssistantMarkdown(a.streamBuffer.String())
	}
}

// appendToolDone 以结构化方式展示工具结果（尤其是 write 的 diff）
// appendToolDone renders tool completion in a structured way (with pretty diffs for write).
func (a *App) appendToolDone(name, summary string) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}

	// 将首行视作总览，其余视作详细信息（通常是 diff）
	head, detail := splitHeadAndDetail(summary)

	a.appendChat(fmt.Sprintf("  ✓ %s", head))
	a.appendLog(fmt.Sprintf("[DONE] %s: %s", name, head))

	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}

	// 如果看起来像 unified diff，则用专门的 diff 渲染；否则当作普通多行文本缩进展示。
	if looksLikeDiff(detail) {
		rendered := RenderDiff(detail, a.theme)
		a.appendChat(indentBlock(rendered, "    "))
		a.appendLog(indentBlock(rendered, "    "))
	} else {
		a.appendChat(indentBlock(detail, "    "))
		a.appendLog(indentBlock(detail, "    "))
	}
}

// splitHeadAndDetail 将多行文本拆成首行和剩余部分。
// splitHeadAndDetail splits multi-line summary into head (first line) and detail (rest).
func splitHeadAndDetail(s string) (string, string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	parts := strings.SplitN(normalized, "\n", 2)
	head := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return head, ""
	}
	return head, strings.TrimRight(parts[1], "\n")
}

// looksLikeDiff 粗略判断文本是否是 unified diff。
// looksLikeDiff makes a cheap guess whether the text is a unified diff.
func looksLikeDiff(s string) bool {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return false
	}
	nonEmpty := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		if strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "@@ ") ||
			strings.HasPrefix(line, "diff --") {
			return true
		}
		if nonEmpty >= 20 {
			// 太长了，不再精细判断
			break
		}
	}
	return false
}

// indentBlock 为多行文本统一添加缩进前缀。
// indentBlock adds a prefix to each line in a multi-line block.
func indentBlock(s, prefix string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = prefix
		} else {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// appendAssistantMarkdown 以 markdown 方式渲染助手回复（支持 `code` / ```code``` 块）
// appendAssistantMarkdown renders assistant replies as markdown (inline `code` and ```blocks```).
func (a *App) appendAssistantMarkdown(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}

	width := a.chatView.Width
	if width <= 0 {
		width = a.width
	}

	rendered := RenderMarkdown(trimmed, width)

	if a.chatContent == nil {
		a.chatContent = &strings.Builder{}
	}
	// 与其他消息之间留一空行，提升可读性。
	if a.chatContent.Len() > 0 && !strings.HasSuffix(a.chatContent.String(), "\n\n") {
		a.chatContent.WriteString("\n")
	}

	a.chatContent.WriteString(rendered)
	a.chatContent.WriteString("\n")
	a.chatView.SetContent(a.chatContent.String())
	a.chatView.GotoBottom()
}

// --- 渲染方法 / Render methods ---

func (a App) renderTabs(width int) string {
	tabs := []struct {
		id   PanelID
		name string
	}{
		{PanelChat, a.locale.T("panel.chat")},
		{PanelFiles, a.locale.T("panel.files")},
		{PanelLogs, a.locale.T("panel.logs")},
	}

	var parts []string
	for _, tab := range tabs {
		style := a.theme.InactiveTabStyle
		if tab.id == a.activePanel {
			style = a.theme.ActiveTabStyle
		}
		parts = append(parts, style.Render(tab.name))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (a App) renderActivePanel(width, height int) string {
	style := lipgloss.NewStyle().
		Width(width).
		Height(height)

	var content string
	switch a.activePanel {
	case PanelChat:
		content = a.chatView.View()
	case PanelFiles:
		if a.fileContent == nil || a.fileContent.Len() == 0 {
			content = a.theme.MutedStyle.Render("  No files accessed yet")
		} else {
			content = a.filesView.View()
		}
	case PanelLogs:
		if a.logContent == nil || a.logContent.Len() == 0 {
			content = a.theme.MutedStyle.Render("  No logs yet")
		} else {
			content = a.logsView.View()
		}
	}

	return style.Render(content)
}

func (a App) renderInput(width, height int) string {
	style := a.theme.InputStyle.Width(width)
	return style.Render(a.input.View())
}

func (a App) renderSidebar(width, height int) string {
	var parts []string

	// 标题 / Title
	parts = append(parts, a.theme.TitleStyle.Render(" Coder"))
	parts = append(parts, "")

	// 上下文 / Context
	parts = append(parts, a.theme.TitleStyle.Render(" "+a.locale.T("sidebar.context")))
	bar := renderProgressBar(a.tokenPct, width-4)
	parts = append(parts, "  "+bar)
	parts = append(parts, fmt.Sprintf("  %d / %d", a.tokens, a.tokenLimit))
	parts = append(parts, fmt.Sprintf("  %.1f%% spent", a.tokenPct))
	parts = append(parts, "")

	// Agent / Model
	parts = append(parts, a.theme.TitleStyle.Render(" "+a.locale.T("sidebar.agent")))
	parts = append(parts, "  "+a.agentName)
	parts = append(parts, "")

	parts = append(parts, a.theme.TitleStyle.Render(" "+a.locale.T("sidebar.model")))
	parts = append(parts, "  "+a.modelName)
	parts = append(parts, "")

	// Todo
	if len(a.todoItems) > 0 {
		parts = append(parts, a.theme.TitleStyle.Render(" "+a.locale.T("sidebar.todo")))
		for _, item := range a.todoItems {
			parts = append(parts, "  "+item)
		}
		parts = append(parts, "")
	}

	content := strings.Join(parts, "\n")

	style := a.theme.SidebarStyle.
		Width(width).
		Height(height)

	return style.Render(content)
}

func (a App) renderStatusBar(width int) string {
	status := a.locale.T("status.ready")
	if a.streaming {
		status = a.locale.T("status.streaming")
	}

	left := fmt.Sprintf(" %s · %s · %s", a.agentName, a.modelName, status)
	right := fmt.Sprintf("%s  ", a.workspace)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := left + strings.Repeat(" ", gap) + right
	return a.theme.StatusBarStyle.Width(width).Render(bar)
}

func renderProgressBar(percent float64, width int) string {
	if width < 4 {
		width = 4
	}
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	return "█" + strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// AppendUserMessage 添加用户消息到聊天面板
// AppendUserMessage adds a user message to the chat panel
func (a *App) AppendUserMessage(text string) {
	a.appendChat("\n👤 " + text)
}

// AppendFile 添加文件到文件面板
// AppendFile adds a file entry to the files panel
func (a *App) AppendFile(path string) {
	a.fileContent.WriteString("  📄 " + path + "\n")
	a.filesView.SetContent(a.fileContent.String())
}

// SetTodoItems 更新侧边栏 todo 列表
// SetTodoItems updates the sidebar todo list
func (a *App) SetTodoItems(items []string) {
	a.todoItems = items
}

// Run 启动 Bubble Tea TUI
// Run starts the Bubble Tea TUI application
func Run(app App) error {
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// 将 orchestrator 的回调绑定到 TUI（文本流 + 工具事件）
	if app.orch != nil {
		app.orch.SetTextStreamCallback(func(chunk string) {
			if strings.TrimSpace(chunk) == "" {
				return
			}
			p.Send(TextChunkMsg{Text: chunk})
		})
		app.orch.SetToolEventCallback(func(name, summary string, done bool) {
			if done {
				p.Send(ToolDoneMsg{Name: name, Summary: summary})
			} else {
				p.Send(ToolStartMsg{Name: name, Summary: summary})
			}
		})
	}

	_, err := p.Run()
	return err
}

// handleSendInput 处理发送消息：追加用户消息并启动一次对话回合
// handleSendInput appends the user message and starts a new turn with the orchestrator
func (a *App) handleSendInput(text string) tea.Cmd {
	if a.orch == nil {
		return nil
	}
	a.AppendUserMessage(text)
	a.input.SetValue("")
	return a.runTurnCmd(text)
}

// runTurnCmd 在后台调用 orchestrator.RunInput，并以消息形式把结果回传给 TUI
// runTurnCmd runs orchestrator.RunInput in background and returns final result as messages
func (a App) runTurnCmd(text string) tea.Cmd {
	if a.orch == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		// 在 TUI 中我们依赖 orchestrator 的文本回调做真正的流式渲染，
		// 这里将 out 设为 nil，只拿最终文本结果用于非流式场景。
		content, err := a.orch.RunInput(ctx, text, nil)
		if err != nil {
			return TurnDoneMsg{Content: content, Err: err}
		}
		return TurnDoneMsg{Content: content, Err: nil}
	}
}
