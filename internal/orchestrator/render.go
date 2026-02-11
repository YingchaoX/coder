package orchestrator

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type answerStreamRenderer struct {
	out             io.Writer
	started         bool
	lineStart       bool
	pendingNewlines int
	hasVisibleText  bool
}

func newAnswerStreamRenderer(out io.Writer) *answerStreamRenderer {
	return &answerStreamRenderer{out: out, lineStart: true}
}

type thinkingStreamRenderer struct {
	out             io.Writer
	started         bool
	lineStart       bool
	pendingNewlines int
	hasVisibleText  bool
}

func newThinkingStreamRenderer(out io.Writer) *thinkingStreamRenderer {
	return &thinkingStreamRenderer{out: out, lineStart: true}
}

func (r *thinkingStreamRenderer) start() {
	if r == nil || r.out == nil || r.started {
		return
	}
	r.started = true
	_, _ = fmt.Fprintln(r.out)
	_, _ = fmt.Fprintf(r.out, "%s %s\n", style("[THINK]", ansiGray+";"+ansiBold), style(strings.Repeat("─", 40), ansiGray))
}

func (r *thinkingStreamRenderer) Append(chunk string) {
	if r == nil || r.out == nil || chunk == "" {
		return
	}
	r.start()
	normalized := strings.ReplaceAll(strings.ReplaceAll(chunk, "\r\n", "\n"), "\r", "\n")
	for _, ch := range normalized {
		if ch == '\n' {
			r.pendingNewlines++
			continue
		}
		r.flushPendingNewlines()
		if r.lineStart {
			r.lineStart = false
		}
		// thinking 用 dim 灰色 / show thinking in dim gray
		_, _ = fmt.Fprint(r.out, style(string(ch), ansiGray))
		r.hasVisibleText = true
	}
}

func (r *thinkingStreamRenderer) Finish() {
	if r == nil || r.out == nil || !r.started {
		return
	}
	r.pendingNewlines = 0
	if !r.lineStart {
		_, _ = fmt.Fprintln(r.out)
		r.lineStart = true
	}
	_, _ = fmt.Fprintln(r.out)
}

func (r *thinkingStreamRenderer) flushPendingNewlines() {
	if r.pendingNewlines == 0 {
		return
	}
	if !r.hasVisibleText {
		r.pendingNewlines = 0
		return
	}
	newlineCount := r.pendingNewlines
	if newlineCount > 2 {
		newlineCount = 2
	}
	for i := 0; i < newlineCount; i++ {
		_, _ = fmt.Fprint(r.out, "\n")
	}
	r.pendingNewlines = 0
	r.lineStart = true
}

func (r *answerStreamRenderer) start() {
	if r == nil || r.out == nil || r.started {
		return
	}
	r.started = true
	_, _ = fmt.Fprintln(r.out)
	_, _ = fmt.Fprintf(r.out, "%s %s\n", style("[ANSWER]", ansiCyan+";"+ansiBold), style(strings.Repeat("─", 40), ansiCyan))
}

func (r *answerStreamRenderer) Append(chunk string) {
	if r == nil || r.out == nil || chunk == "" {
		return
	}
	r.start()
	normalized := strings.ReplaceAll(strings.ReplaceAll(chunk, "\r\n", "\n"), "\r", "\n")
	for _, ch := range normalized {
		if ch == '\n' {
			r.pendingNewlines++
			continue
		}
		r.flushPendingNewlines()
		if r.lineStart {
			r.lineStart = false
		}
		_, _ = fmt.Fprint(r.out, string(ch))
		r.hasVisibleText = true
	}
}

func (r *answerStreamRenderer) Finish() {
	if r == nil || r.out == nil || !r.started {
		return
	}
	r.pendingNewlines = 0
	if !r.lineStart {
		_, _ = fmt.Fprintln(r.out)
		r.lineStart = true
	}
	_, _ = fmt.Fprintln(r.out)
}

func (r *answerStreamRenderer) flushPendingNewlines() {
	if r.pendingNewlines == 0 {
		return
	}
	if !r.hasVisibleText {
		r.pendingNewlines = 0
		return
	}
	newlineCount := r.pendingNewlines
	if newlineCount > 2 {
		newlineCount = 2
	}
	for i := 0; i < newlineCount; i++ {
		_, _ = fmt.Fprint(r.out, "\n")
	}
	r.pendingNewlines = 0
	r.lineStart = true
}

func renderAssistantBlock(out io.Writer, content string, isFinal bool) {
	kind := "PLAN"
	color := ansiGray
	if isFinal {
		kind = "ANSWER"
		color = ansiCyan
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "%s %s\n", style("["+kind+"]", color+";"+ansiBold), style(strings.Repeat("─", 40), color))
	lines := compactAssistantLines(content)
	for _, line := range lines {
		if line == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintf(out, "%s\n", line)
	}
	_, _ = fmt.Fprintln(out)
}

// renderCommandBlock 渲染命令模式输出块，使用 [COMMAND] 头部。
// renderCommandBlock renders command-mode output with a [COMMAND] header.
func renderCommandBlock(out io.Writer, content string) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "%s %s\n", style("[COMMAND]", ansiCyan+";"+ansiBold), style(strings.Repeat("─", 40), ansiCyan))
	lines := compactAssistantLines(content)
	for _, line := range lines {
		if line == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintf(out, "%s\n", line)
	}
	_, _ = fmt.Fprintln(out)
}

func renderThinkingBlock(out io.Writer, content string) {
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "%s %s\n", style("[THINK]", ansiGray+";"+ansiBold), style(strings.Repeat("─", 40), ansiGray))
	lines := compactAssistantLines(content)
	for _, line := range lines {
		if line == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintln(out, style(line, ansiGray))
	}
	_, _ = fmt.Fprintln(out)
}

func renderToolStart(out io.Writer, message string) {
	_, _ = fmt.Fprintf(out, "%s %s\n", style("[TOOL]", ansiYellow+";"+ansiBold), style(message, ansiYellow))
}

func renderToolResult(out io.Writer, message string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(message, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "  %s %s\n", style("->", ansiGreen+";"+ansiBold), style(lines[0], ansiGray))
	for _, line := range lines[1:] {
		if line == "" {
			_, _ = fmt.Fprintln(out)
			continue
		}
		_, _ = fmt.Fprintf(out, "     %s\n", styleToolDetailLine(line))
	}
}

func renderToolError(out io.Writer, message string) {
	_, _ = fmt.Fprintf(out, "  %s %s\n", style("x", ansiRed+";"+ansiBold), style(message, ansiRed))
}

func renderToolBlocked(out io.Writer, message string) {
	_, _ = fmt.Fprintf(out, "  %s %s\n", style("!", ansiYellow+";"+ansiBold), style("blocked: "+message, ansiYellow))
}

func style(text, codes string) string {
	if text == "" || !enableColor() {
		return text
	}
	segments := strings.Split(codes, ";")
	var builder strings.Builder
	for _, segment := range segments {
		code := strings.TrimSpace(segment)
		if code == "" {
			continue
		}
		builder.WriteString(code)
	}
	if builder.Len() == 0 {
		return text
	}
	return builder.String() + text + ansiReset
}

func styleToolDetailLine(line string) string {
	// Todo list lines (before diff prefixes)
	switch {
	case strings.HasPrefix(line, "[x] "):
		return style(line, ansiCyan)
	case strings.HasPrefix(line, "[~] "):
		return style(line, ansiYellow)
	case strings.HasPrefix(line, "[ ] "):
		return style(line, ansiGray)
	}
	switch {
	case strings.HasPrefix(line, "diff --"), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
		return style(line, ansiYellow)
	case strings.HasPrefix(line, "@@"):
		return style(line, ansiCyan)
	case strings.HasPrefix(line, "+"):
		return style(line, ansiGreen)
	case strings.HasPrefix(line, "-"):
		return style(line, ansiRed)
	default:
		return style(line, ansiGray)
	}
}

func compactAssistantLines(content string) []string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	normalized = strings.Trim(normalized, "\n")
	if normalized == "" {
		return []string{""}
	}
	rawLines := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(rawLines))
	blankSeen := false
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			if blankSeen {
				continue
			}
			lines = append(lines, "")
			blankSeen = true
			continue
		}
		lines = append(lines, line)
		blankSeen = false
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func enableColor() bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("AGENT_NO_COLOR")) != "" {
		return false
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))) != "dumb"
}
