package repl

import (
	"unicode/utf8"

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
