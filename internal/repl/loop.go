package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"coder/internal/bootstrap"
	"coder/internal/orchestrator"
	"coder/internal/tools"
)

// ANSI colors for prompt (per doc 09)
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[90m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiBold   = "\x1b[1m"
)

// Loop holds REPL state: orchestrator, prompt info, and input history.
// Loop 持有 REPL 状态：编排器、提示符信息与输入历史。
type Loop struct {
	*bootstrap.BuildResult
	// prompt state (updated by SetContextUpdateCallback before each prompt)
	tokens int
	limit  int
	// history of submitted inputs (for future ↑/↓; stored but not yet used without raw terminal)
	history []string
}

// NewLoop builds a REPL loop from a BuildResult.
func NewLoop(res *bootstrap.BuildResult) *Loop {
	return &Loop{BuildResult: res, history: make([]string, 0, 64)}
}

// Run runs the REPL: two-line prompt, read input, RunInput(ctx, text, os.Stdout).
func Run(loop *Loop) error {
	orch := loop.Orch
	if orch == nil {
		return fmt.Errorf("orchestrator is nil")
	}

	// REPL only needs context update for the first line of the prompt (tokens · model).
	orch.SetContextUpdateCallback(func(tokens, limit int, percent float64) {
		loop.tokens = tokens
		loop.limit = limit
	})
	// No-op for REPL: todos are shown in conversation or via /todos.
	orch.SetTodoUpdateCallback(func([]string) {})
	// Optional: could set TextStream/ToolEvent to no-op; orchestrator already writes to out when out != nil.

	ctx := context.Background()
	stdin := bufio.NewReader(os.Stdin)
	stdout := os.Stdout
	stdinFd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(stdinFd)

	for {
		loop.updatePromptState(orch)
		loop.printPromptTo(stdout)

		var text string
		var err error
		if isTTY {
			text, err = readInputRaw(stdinFd, os.Stdin, stdout, loop.history)
		} else {
			var lines []string
			lines, err = readInput(stdin)
			if err == nil && lines != nil {
				text = strings.TrimSpace(strings.Join(lines, "\n"))
			}
		}
		if err != nil {
			return err
		}
		if text == tabModeToggleToken {
			next := "build"
			if strings.EqualFold(orch.CurrentMode(), "build") {
				next = "plan"
			}
			orch.SetMode(next)
			_, _ = fmt.Fprintf(stdout, "\nMode set to %s\n", next)
			continue
		}
		if text == "" {
			continue
		}
		// Non-TTY input path doesn't echo keystrokes; ensure outputs start on a new line.
		// 非 TTY 输入路径不会回显按键；确保后续输出从新行开始。
		if !isTTY {
			_, _ = fmt.Fprintln(stdout)
		}

		// Store in history for future ↑/↓ when raw terminal is used
		loop.history = append(loop.history, text)
		if len(loop.history) > 1000 {
			loop.history = loop.history[1:]
		}

		runCtx := ctx
		runOut := io.Writer(stdout)
		var (
			turnCancel context.CancelFunc
			rtCtrl     *runtimeController
		)
		if isTTY {
			runOut = newTerminalOutputWriter(stdout)
			runCtx, turnCancel = context.WithCancel(context.Background())
			rtCtrl, err = newRuntimeController(stdinFd, os.Stdin, runOut, turnCancel)
			if err != nil {
				if turnCancel != nil {
					turnCancel()
				}
				return err
			}
			runCtx = bootstrap.WithApprovalPrompter(runCtx, rtCtrl)
			runCtx = tools.WithQuestionPrompter(runCtx, rtCtrl)
		}

		_, err = orch.RunInput(runCtx, text, runOut)
		if turnCancel != nil {
			turnCancel()
		}
		if rtCtrl != nil {
			closeErr := rtCtrl.Close()
			if closeErr != nil && err == nil {
				err = closeErr
			}
			if rtCtrl.Interrupted() {
				return errInterrupt
			}
			if rtCtrl.CancelledByESC() {
				printEscCancelled(stdout)
				continue
			}
		}
		if err != nil {
			fmt.Fprintf(stdout, "\n%serror: %v%s\n", ansiRed, err, ansiReset)
		}
	}
}

const ansiRed = "\x1b[31m"

func (loop *Loop) updatePromptState(orch *orchestrator.Orchestrator) {
	// Ensure we have tokens/limit for prompt; orchestrator callback may have set them
	if loop.limit == 0 {
		stats := orch.CurrentContextStats()
		loop.tokens = stats.EstimatedTokens
		loop.limit = stats.ContextLimit
		if loop.limit <= 0 {
			loop.limit = 24000
		}
	}
}

// printPromptTo writes the two-line prompt to w (per doc 09).
func (loop *Loop) printPromptTo(w io.Writer) {
	model := loop.Model
	mode := "build"
	if loop.Orch != nil {
		if m := loop.Orch.CurrentModel(); m != "" {
			model = m
		}
		if m := loop.Orch.CurrentMode(); m != "" {
			mode = m
		}
	}
	cwd := loop.WorkspaceRoot

	// Line 1: context: N tokens · model: xxx (dim)
	line1 := fmt.Sprintf("context: %d tokens · model: %s", loop.tokens, model)
	if useColor() {
		_, _ = fmt.Fprintf(w, "%s%s%s\n", ansiDim, line1, ansiReset)
	} else {
		_, _ = fmt.Fprintln(w, line1)
	}
	// Line 2: [mode] /path>
	line2 := fmt.Sprintf("[%s] %s> ", mode, cwd)
	if useColor() {
		_, _ = fmt.Fprintf(w, "%s%s%s", ansiGreen, line2, ansiReset)
	} else {
		_, _ = fmt.Fprint(w, line2)
	}
}

// errInterrupt is returned when user presses Ctrl+C in raw mode so Run can exit.
var errInterrupt = fmt.Errorf("interrupt")

// Bracketed paste mode: enable \e[?2004h, disable \e[?2004l; paste wraps in \e[200~...\e[201~.
const (
	bpmEnable  = "\x1b[?2004h"
	bpmDisable = "\x1b[?2004l"
	// Note: we already consumed the leading '[' after ESC, so CSI payload here is "200~"/"201~".
	bpmStart = "200~"
	bpmEnd   = "201~"
)

const tabModeToggleToken = "__CODER_REPL_TOGGLE_MODE__"

// readInputRaw reads from stdin in raw mode: Enter = send; paste multi-line
// shows [copy N lines], then Enter sends. Caller must pass
// stdinFd = int(os.Stdin.Fd()). Echoes input to out. When history is non-nil,
// Up/Down arrows navigate previously submitted input lines.
// readInputRaw 在 raw 模式下读取输入：Enter 发送，粘贴多行显示
// “[copy N lines]” 后 Enter 发送整段；当传入 history 时，↑/↓ 用于在历史输入间切换。
func readInputRaw(stdinFd int, stdin *os.File, out io.Writer, history []string) (string, error) {
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return "", err
	}
	defer term.Restore(stdinFd, oldState)
	_, _ = out.Write([]byte(bpmEnable))
	if f, ok := out.(*os.File); ok {
		_ = f.Sync()
	}
	defer func() { _, _ = out.Write([]byte(bpmDisable)) }()

	var buf strings.Builder
	var nav *historyNavigator
	if len(history) > 0 {
		nav = newHistoryNavigator(history)
	}
	var pendingPaste string
	pastePending := false
	rd := bufio.NewReader(stdin)

	for {
		b, err := rd.ReadByte()
		if err != nil {
			return buf.String(), err
		}
		switch b {
		case 0x03: // Ctrl+C
			return "", errInterrupt
		case 0x04: // Ctrl+D no longer submits; ignore
			continue
		case 0x7f, 0x08: // Backspace (DEL or BS)
			if pastePending {
				pastePending = false
				pendingPaste = ""
				continue
			}
			s := buf.String()
			if len(s) == 0 {
				continue
			}
			next, width := deleteLastRuneAndWidth(s)
			// Truncate by rune (not by byte) so UTF-8 stays valid.
			// 按 rune 截断（不是按 byte），避免破坏 UTF-8。
			buf.Reset()
			buf.WriteString(next)
			for i := 0; i < width; i++ {
				_, _ = out.Write([]byte{'\b', ' ', '\b'})
			}
		case '\r', '\n':
			if pastePending {
				pastePending = false
				// Ensure the next output starts at column 0 on a new line.
				// 使用 \r\n 保证光标回到行首并换到下一行，避免 [TOOL] 等输出缩进错位。
				_, _ = out.Write([]byte("\r\n"))
				return pendingPaste, nil
			}
			// 简化行为：不再做无 BPM fallback，直接按当前缓冲区内容发送单行。
			lineSoFar := buf.String()
			// Ensure the next output starts at column 0 on a new line.
			// 使用 \r\n 保证光标回到行首并换到下一行，避免 [TOOL] 等输出缩进错位。
			_, _ = out.Write([]byte("\r\n"))
			return lineSoFar, nil
		case '\t':
			if !pastePending && buf.Len() == 0 {
				return tabModeToggleToken, nil
			}
			buf.WriteByte(b)
			_, _ = out.Write([]byte{b})
		case 0x1b:
			if pastePending {
				pastePending = false
				pendingPaste = ""
				continue
			}
			// Bare ESC in input mode clears current line.
			if rd.Buffered() == 0 {
				current := buf.String()
				if current != "" {
					buf.Reset()
					clearEchoedInput(out, current)
				}
				continue
			}
			next, err := rd.ReadByte()
			if err != nil {
				buf.WriteByte(b)
				_, _ = out.Write([]byte{b})
				return buf.String(), err
			}
			if next != '[' {
				current := buf.String()
				if current != "" {
					buf.Reset()
					clearEchoedInput(out, current)
				}
				// Keep the follow-up keypress (if printable) as the first char after clearing.
				if next >= 0x20 && next <= 0x7e {
					buf.WriteByte(next)
					_, _ = out.Write([]byte{next})
				}
				continue
			}
			// Read CSI until final byte (letter or ~)
			var csi []byte
			for {
				c, err := rd.ReadByte()
				if err != nil {
					return buf.String(), err
				}
				csi = append(csi, c)
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' {
					break
				}
			}
			if string(csi) == bpmStart {
				// Bracketed paste: read until \e[201~
				var pasteBuf strings.Builder
			pasteLoop:
				for {
					c, err := rd.ReadByte()
					if err != nil {
						return buf.String(), err
					}
					if c != 0x1b {
						pasteBuf.WriteByte(c)
						continue
					}
					end := make([]byte, 5)
					for i := 0; i < 5; i++ {
						end[i], err = rd.ReadByte()
						if err != nil {
							pasteBuf.WriteByte(0x1b)
							pasteBuf.Write(end[:i])
							return buf.String(), err
						}
					}
					if end[0] == '[' && end[1] == '2' && end[2] == '0' && end[3] == '1' && end[4] == '~' {
						break pasteLoop
					}
					pasteBuf.WriteByte(0x1b)
					pasteBuf.Write(end)
				}
				body := pasteBuf.String()
				// Normalize CRLF/CR to LF so multi-line content and line counting are consistent.
				body = strings.ReplaceAll(body, "\r\n", "\n")
				body = strings.ReplaceAll(body, "\r", "\n")
				// If the pasted content is a single line (no '\n'), treat it like normal input:
				// echo it and append into the current buffer. Only multi-line paste shows "[copy N lines]".
				// 如果粘贴内容只有一行（不含 '\n'），就像普通输入一样：直接回显并追加到缓冲区；
				// 只有多行粘贴才显示 "[copy N lines]"。
				if !strings.Contains(body, "\n") {
					buf.WriteString(body)
					_, _ = out.Write([]byte(body))
					continue
				}

				trimmed := strings.TrimRight(body, "\n")
				n := 1 + strings.Count(trimmed, "\n")
				if n < 2 {
					n = 2
				}
				msg := fmt.Sprintf("[copy %d lines]", n)
				if useColor() {
					_, _ = fmt.Fprintf(out, "%s%s%s ", ansiDim, msg, ansiReset)
				} else {
					_, _ = fmt.Fprintf(out, "%s ", msg)
				}
				pendingPaste = body
				pastePending = true
				buf.Reset()
				continue
			}
			// Arrow keys for history navigation: ESC [ A/B
			if nav != nil {
				last := csi[len(csi)-1]
				switch last {
				case 'A': // Up: older history
					current := buf.String()
					next, ok := nav.Prev()
					if !ok {
						break
					}
					display := historyDisplayString(next)
					if current == display {
						break
					}
					if current != "" {
						clearEchoedInput(out, current)
					}
					buf.Reset()
					buf.WriteString(display)
					if display != "" {
						_, _ = out.Write([]byte(display))
					}
					continue
				case 'B': // Down: newer history / fresh input
					current := buf.String()
					next, ok := nav.Next()
					if !ok {
						break
					}
					display := historyDisplayString(next)
					if current == display {
						break
					}
					if current != "" {
						clearEchoedInput(out, current)
					}
					buf.Reset()
					buf.WriteString(display)
					if display != "" {
						_, _ = out.Write([]byte(display))
					}
					continue
				}
			}
			// Other CSI: discard, do not echo
		default:
			if pastePending {
				// Treat printable characters typed after a multi-line paste
				// as part of the same input instead of cancelling the paste.
				// 将多行粘贴后的可打印字符视为粘贴内容的追加，而不是丢弃粘贴结果。
				if nb, ok := appendPrintableToPaste(pendingPaste, b); ok {
					pendingPaste = nb
					_, _ = out.Write([]byte{b})
					continue
				}
				// 非可打印字符仍按“取消粘贴”处理，退回普通输入路径。
				pastePending = false
				pendingPaste = ""
			}
			buf.WriteByte(b)
			_, _ = out.Write([]byte{b})
		}
	}
}

// readInput is used when stdin is not a TTY (pipe/redirect): read until EOF as one message.
// See doc 09: non-TTY reads by line or EOF; this implementation uses "read until EOF".
func readInput(rd *bufio.Reader) ([]string, error) {
	readLine := func() (string, error) {
		var line []byte
		for {
			b, err := rd.ReadByte()
			if err != nil {
				if len(line) > 0 {
					return string(line), nil
				}
				return "", err
			}
			if b == '\n' {
				return string(line), nil
			}
			line = append(line, b)
		}
	}

	var lines []string
	for {
		line, err := readLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("stdin closed")
	}
	return lines, nil
}

func useColor() bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("AGENT_NO_COLOR")) != "" {
		return false
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))) != "dumb"
}

func clearEchoedInput(out io.Writer, line string) {
	if out == nil || line == "" {
		return
	}
	width := runewidth.StringWidth(line)
	if width <= 0 {
		width = len([]rune(line))
	}
	for i := 0; i < width; i++ {
		_, _ = out.Write([]byte{'\b', ' ', '\b'})
	}
}

func printEscCancelled(out io.Writer) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintln(out)
	msg := "Cancelled by ESC"
	if useColor() {
		_, _ = fmt.Fprintf(out, "%s%s%s\n", ansiYellow, msg, ansiReset)
	} else {
		_, _ = fmt.Fprintln(out, msg)
	}
	_, _ = fmt.Fprintln(out, "Stopped model stream and tool execution; todo state remains unchanged unless a tool had already completed.")
}
