package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BuildUnifiedDiff builds a compact single-hunk unified diff preview.
func BuildUnifiedDiff(path, oldContent, newContent string) (string, int, int) {
	oldNorm := normalizeLineEndings(oldContent)
	newNorm := normalizeLineEndings(newContent)
	if oldNorm == newNorm {
		return "", 0, 0
	}

	oldLines := splitDiffLines(oldNorm)
	newLines := splitDiffLines(newNorm)

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}

	suffix := 0
	for len(oldLines)-1-suffix >= prefix &&
		len(newLines)-1-suffix >= prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	oldChangedStart := prefix
	oldChangedEnd := len(oldLines) - suffix
	newChangedStart := prefix
	newChangedEnd := len(newLines) - suffix

	const contextLines = 1
	preStart := maxInt(0, prefix-contextLines)
	postOldStart := oldChangedEnd
	postOldEnd := minInt(len(oldLines), oldChangedEnd+contextLines)
	postNewStart := newChangedEnd
	postNewEnd := minInt(len(newLines), newChangedEnd+contextLines)

	oldCount := (prefix - preStart) + (oldChangedEnd - oldChangedStart) + (postOldEnd - postOldStart)
	newCount := (prefix - preStart) + (newChangedEnd - newChangedStart) + (postNewEnd - postNewStart)

	oldStart := preStart + 1
	newStart := preStart + 1
	if oldCount == 0 {
		oldStart = preStart
	}
	if newCount == 0 {
		newStart = preStart
	}

	displayPath := normalizeDiffPath(path)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "--- a/%s\n", displayPath)
	_, _ = fmt.Fprintf(&b, "+++ b/%s\n", displayPath)
	_, _ = fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)

	for _, line := range oldLines[preStart:prefix] {
		b.WriteString(" ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	deletions := 0
	for _, line := range oldLines[oldChangedStart:oldChangedEnd] {
		deletions++
		b.WriteString("-")
		b.WriteString(line)
		b.WriteString("\n")
	}

	additions := 0
	for _, line := range newLines[newChangedStart:newChangedEnd] {
		additions++
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}

	for i := 0; i < postOldEnd-postOldStart && i < postNewEnd-postNewStart; i++ {
		b.WriteString(" ")
		b.WriteString(oldLines[postOldStart+i])
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n"), additions, deletions
}

// TruncateUnifiedDiff bounds diff output for terminal/context readability.
func TruncateUnifiedDiff(diff string, maxLines, maxBytes int) (string, bool) {
	diff = strings.TrimSpace(strings.ReplaceAll(diff, "\r\n", "\n"))
	if diff == "" {
		return "", false
	}

	lines := strings.Split(diff, "\n")
	truncated := false
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if maxBytes > 0 && len(out) > maxBytes {
		out = strings.TrimRight(out[:maxBytes], "\n")
		truncated = true
	}
	if truncated {
		out += "\n... (diff truncated)"
	}
	return out, truncated
}

func normalizeDiffPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return "file"
	}
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimPrefix(p, "./")
	if p == "" || p == "." {
		return "file"
	}
	return p
}

func normalizeLineEndings(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(content, "\r", "\n")
}

func splitDiffLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a <= b {
		return a
	}
	return b
}
