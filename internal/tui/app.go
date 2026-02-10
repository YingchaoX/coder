package tui

import (
	"fmt"
	"strings"

	"coder/internal/i18n"

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

	// 内容缓冲 / Content buffers
	chatContent strings.Builder
	logContent  strings.Builder
	fileContent strings.Builder

	// 状态 / State
	streaming    bool
	streamBuffer strings.Builder
	lastError    string
	workspace    string

	// 配置 / Config
	theme  Theme
	keys   KeyMap
	locale *i18n.I18n
}

// NewApp 创建 TUI 应用
// NewApp creates a new TUI application
func NewApp(workspace, agent, model, sessionID string) App {
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
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.relayout()
		return a, nil

	case TextChunkMsg:
		a.streaming = true
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
		a.appendChat(fmt.Sprintf("  ✓ %s", msg.Summary))
		a.appendLog(fmt.Sprintf("[DONE] %s: %s", msg.Name, msg.Summary))
		return a, nil

	case TurnDoneMsg:
		a.streaming = false
		if msg.Err != nil {
			a.lastError = msg.Err.Error()
			a.appendChat("\n❌ " + msg.Err.Error())
		} else if a.streamBuffer.Len() > 0 {
			// 流式内容已经在 chat 中，这里刷新最终版本
			a.flushStreamToChat()
		}
		a.streamBuffer.Reset()
		return a, nil

	case StreamingStartMsg:
		a.streaming = true
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
	a.chatView.SetContent(a.chatContent.String())

	a.filesView = viewport.New(mainWidth, panelHeight)
	a.filesView.SetContent(a.fileContent.String())

	a.logsView = viewport.New(mainWidth, panelHeight)
	a.logsView.SetContent(a.logContent.String())

	a.input.SetWidth(mainWidth - 4)
}

func (a *App) appendChat(text string) {
	a.chatContent.WriteString(text + "\n")
	a.chatView.SetContent(a.chatContent.String())
	a.chatView.GotoBottom()
}

func (a *App) appendLog(text string) {
	a.logContent.WriteString(text + "\n")
	a.logsView.SetContent(a.logContent.String())
}

func (a *App) updateChatFromStream() {
	// 在流式输出时，显示已有内容 + 流式缓冲
	content := a.chatContent.String()
	if a.streamBuffer.Len() > 0 {
		content += "\n" + a.streamBuffer.String()
	}
	a.chatView.SetContent(content)
	a.chatView.GotoBottom()
}

func (a *App) flushStreamToChat() {
	if a.streamBuffer.Len() > 0 {
		a.chatContent.WriteString("\n" + a.streamBuffer.String() + "\n")
		a.chatView.SetContent(a.chatContent.String())
		a.chatView.GotoBottom()
	}
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
		if a.fileContent.Len() == 0 {
			content = a.theme.MutedStyle.Render("  No files accessed yet")
		} else {
			content = a.filesView.View()
		}
	case PanelLogs:
		if a.logContent.Len() == 0 {
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
func Run(workspace, agent, model, sessionID string) error {
	app := NewApp(workspace, agent, model, sessionID)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
