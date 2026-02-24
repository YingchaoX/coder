package repl

import (
	"fmt"
	"strings"
	"unicode/utf8"

	// NOTE: runewidth is only used when compiled with the CLI; tests run without
	// terminal width sensitivity are still valid.
	//nolint:depguard
	"github.com/mattn/go-runewidth"
)

// deleteLastRuneAndWidth truncates s by one rune and returns the new string
// plus the number of terminal cells that should be erased for visual consistency.
//
// deleteLastRuneAndWidth 将 s 按 “最后 1 个 rune” 截断，并返回：
// - 截断后的新字符串
// - 为了保持终端显示一致，退格需要擦除的显示列数（cells）
func deleteLastRuneAndWidth(s string) (string, int) {
	if len(s) == 0 {
		return s, 0
	}
	r, size := utf8.DecodeLastRuneInString(s)
	if size <= 0 {
		return s, 0
	}
	next := s[:len(s)-size]
	width := runewidth.RuneWidth(r)
	if width < 1 {
		// Combining / non-printable runes may return 0; ensure we still erase at least one cell.
		// 组合字符/不可见 rune 可能返回 0；兜底至少擦除 1 列。
		width = 1
	}
	return next, width
}

// historyNavigator manages navigation over previously submitted input lines.
// historyNavigator 管理已提交输入行的历史导航，用于 ↑/↓ 回放。
type historyNavigator struct {
	history []string
	// index points to the next history slot:
	//   0..len(history)-1 => concrete history entry
	//   len(history)      => “current empty line” (no history selected)
	index int
}

// newHistoryNavigator constructs a navigator starting at the “new input” slot.
// newHistoryNavigator 创建一个从“新输入行”开始的历史导航器。
func newHistoryNavigator(history []string) *historyNavigator {
	return &historyNavigator{
		history: history,
		index:   len(history),
	}
}

// Prev moves one step backward in history (Up arrow). When already at the
// oldest entry, it stays there and returns that entry again.
//
// Prev 处理向上的历史回放（↑）。到达最早记录后继续向上不会再前移，只会返回最早一条。
func (h *historyNavigator) Prev() (string, bool) {
	if h == nil || len(h.history) == 0 {
		return "", false
	}
	if h.index > len(h.history) {
		h.index = len(h.history)
	}
	if h.index > 0 {
		h.index--
	}
	return h.history[h.index], true
}

// Next moves one step forward in history (Down arrow). When moving past the
// newest entry it returns an empty string to represent “back to fresh input”.
//
// Next 处理向下的历史回放（↓）。越过最新记录后返回空串，表示回到“空输入行”。
func (h *historyNavigator) Next() (string, bool) {
	if h == nil || len(h.history) == 0 {
		return "", false
	}
	if h.index < len(h.history) {
		h.index++
	}
	if h.index == len(h.history) {
		return "", true
	}
	return h.history[h.index], true
}

// appendPrintableToPaste appends a single printable ASCII byte to the pending
// multi-line paste body. It returns the new body and whether the byte was
// accepted. Non-printable bytes are ignored so that控制字符不会污染粘贴内容。
// appendPrintableToPaste 将单个可打印 ASCII 字节追加到多行粘贴内容末尾，
// 返回新的内容以及是否被接受。
func appendPrintableToPaste(body string, b byte) (string, bool) {
	if b < 0x20 || b > 0x7e {
		return body, false
	}
	return body + string(b), true
}

// historyDisplayString converts a raw history entry into a single-line display
// string for the prompt. Multi-line entries are rendered as `[copy N lines]`
// to avoid在终端中直接回显多行内容导致的光标错位与编辑混乱。
// historyDisplayString 将原始历史记录转换为提示符上的单行展示字符串：
// - 单行输入：直接显示原文；
// - 多行输入：显示 `[copy N lines]` 占位，N 为行数。
func historyDisplayString(body string) string {
	if !strings.Contains(body, "\n") {
		return body
	}
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" {
		return "[copy 2 lines]"
	}
	n := 1 + strings.Count(trimmed, "\n")
	if n < 2 {
		n = 2
	}
	return fmt.Sprintf("[copy %d lines]", n)
}
