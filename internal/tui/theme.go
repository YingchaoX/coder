package tui

import "github.com/charmbracelet/lipgloss"

// Theme 定义 TUI 主题色彩和样式
// Theme defines TUI colors and styles
type Theme struct {
	// 基础色 / Base colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Danger    lipgloss.Color
	Warning   lipgloss.Color
	Success   lipgloss.Color
	Muted     lipgloss.Color
	Text      lipgloss.Color
	TextDim   lipgloss.Color
	BgPanel   lipgloss.Color
	BgSidebar lipgloss.Color
	Border    lipgloss.Color

	// 预构建样式 / Pre-built styles
	TitleStyle       lipgloss.Style
	ActiveTabStyle   lipgloss.Style
	InactiveTabStyle lipgloss.Style
	StatusBarStyle   lipgloss.Style
	SidebarStyle     lipgloss.Style
	PanelStyle       lipgloss.Style
	InputStyle       lipgloss.Style
	ErrorStyle       lipgloss.Style
	SuccessStyle     lipgloss.Style
	MutedStyle       lipgloss.Style
	DangerStyle      lipgloss.Style
	DiffAddStyle     lipgloss.Style
	DiffDelStyle     lipgloss.Style
	DiffHunkStyle    lipgloss.Style
}

// DarkTheme 暗色主题（默认）
// DarkTheme is the default dark theme
func DarkTheme() Theme {
	t := Theme{
		Primary:   lipgloss.Color("#7C3AED"),
		Secondary: lipgloss.Color("#06B6D4"),
		Accent:    lipgloss.Color("#F59E0B"),
		Danger:    lipgloss.Color("#EF4444"),
		Warning:   lipgloss.Color("#F59E0B"),
		Success:   lipgloss.Color("#10B981"),
		Muted:     lipgloss.Color("#6B7280"),
		Text:      lipgloss.Color("#E5E7EB"),
		TextDim:   lipgloss.Color("#9CA3AF"),
		BgPanel:   lipgloss.Color("#1F2937"),
		BgSidebar: lipgloss.Color("#111827"),
		Border:    lipgloss.Color("#374151"),
	}

	t.TitleStyle = lipgloss.NewStyle().
		Foreground(t.Primary).
		Bold(true)

	t.ActiveTabStyle = lipgloss.NewStyle().
		Foreground(t.Text).
		Background(t.Primary).
		Padding(0, 2).
		Bold(true)

	t.InactiveTabStyle = lipgloss.NewStyle().
		Foreground(t.TextDim).
		Padding(0, 2)

	t.StatusBarStyle = lipgloss.NewStyle().
		Foreground(t.TextDim).
		Background(lipgloss.Color("#111827"))

	t.SidebarStyle = lipgloss.NewStyle().
		Foreground(t.Text).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Border)

	t.PanelStyle = lipgloss.NewStyle().
		Foreground(t.Text)

	t.InputStyle = lipgloss.NewStyle().
		Foreground(t.Text).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Border)

	t.ErrorStyle = lipgloss.NewStyle().
		Foreground(t.Danger).
		Bold(true)

	t.SuccessStyle = lipgloss.NewStyle().
		Foreground(t.Success)

	t.MutedStyle = lipgloss.NewStyle().
		Foreground(t.Muted)

	t.DangerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(t.Danger).
		Bold(true).
		Padding(0, 1)

	t.DiffAddStyle = lipgloss.NewStyle().
		Foreground(t.Success)

	t.DiffDelStyle = lipgloss.NewStyle().
		Foreground(t.Danger)

	t.DiffHunkStyle = lipgloss.NewStyle().
		Foreground(t.Secondary)

	return t
}
