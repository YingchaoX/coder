package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// RenderMarkdown 使用 Glamour 渲染 markdown 文本
// RenderMarkdown renders markdown text using Glamour
func RenderMarkdown(content string, width int) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	if width <= 0 {
		width = 80
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}

	rendered, err := r.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimRight(rendered, "\n")
}

// RenderDiffLine 为 diff 行添加颜色
// RenderDiffLine colorizes a diff line
func RenderDiffLine(line string, theme Theme) string {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return line
	}

	switch {
	case strings.HasPrefix(trimmed, "+++"), strings.HasPrefix(trimmed, "---"),
		strings.HasPrefix(trimmed, "diff --"), strings.HasPrefix(trimmed, "index "):
		return theme.MutedStyle.Render(line)
	case strings.HasPrefix(trimmed, "@@"):
		return theme.DiffHunkStyle.Render(line)
	case strings.HasPrefix(trimmed, "+"):
		return theme.DiffAddStyle.Render(line)
	case strings.HasPrefix(trimmed, "-"):
		return theme.DiffDelStyle.Render(line)
	default:
		return line
	}
}

// RenderDiff 渲染完整 diff
// RenderDiff renders a complete diff with colors
func RenderDiff(diff string, theme Theme) string {
	if strings.TrimSpace(diff) == "" {
		return ""
	}

	lines := strings.Split(diff, "\n")
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, RenderDiffLine(line, theme))
	}
	return strings.Join(rendered, "\n")
}
