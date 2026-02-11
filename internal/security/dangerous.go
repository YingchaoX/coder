package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var dangerousCmdPattern = regexp.MustCompile(`(^|[\s;&|()])(rm|mv|chmod|chown|dd|mkfs|shutdown|reboot)([\s;&|()]|$)`)
var shellSegmentPattern = regexp.MustCompile(`&&|\|\||[|;\n]`)

var dangerousCommands = map[string]struct{}{
	"rm":       {},
	"mv":       {},
	"chmod":    {},
	"chown":    {},
	"dd":       {},
	"mkfs":     {},
	"shutdown": {},
	"reboot":   {},
}

type CommandRisk struct {
	RequireApproval bool
	Reason          string
}

func AnalyzeCommand(command string) CommandRisk {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return CommandRisk{RequireApproval: false}
	}

	if strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`") {
		return CommandRisk{
			RequireApproval: true,
			Reason:          "contains command substitution/backticks",
		}
	}

	if _, err := parseShellWords(trimmed); err != nil {
		return CommandRisk{
			RequireApproval: true,
			Reason:          "command parse failed (fail closed)",
		}
	}

	if hasDangerousCommand(trimmed) {
		return CommandRisk{
			RequireApproval: true,
			Reason:          "matches dangerous command policy",
		}
	}

	return CommandRisk{RequireApproval: false}
}

func parseShellWords(input string) ([]string, error) {
	var (
		out         []string
		cur         strings.Builder
		inSingle    bool
		inDouble    bool
		escaped     bool
		justFlushed bool
	)

	flush := func() {
		if cur.Len() > 0 || justFlushed {
			out = append(out, cur.String())
			cur.Reset()
			justFlushed = false
		}
	}

	for _, r := range input {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			justFlushed = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			justFlushed = true
		case isSpace(r) && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteRune(r)
			justFlushed = false
		}
	}

	if escaped {
		return nil, errors.New("dangling escape")
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unmatched quote")
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

func hasDangerousCommand(command string) bool {
	segments := shellSegmentPattern.Split(command, -1)
	for _, raw := range segments {
		words, err := parseShellWords(raw)
		if err != nil {
			continue
		}
		name := firstCommandName(words)
		if _, ok := dangerousCommands[name]; ok {
			return true
		}
	}
	// Keep regex fallback for edge cases (for example malformed but still executable shell fragments).
	return dangerousCmdPattern.MatchString(command)
}

func firstCommandName(words []string) string {
	if len(words) == 0 {
		return ""
	}
	i := 0
	for i < len(words) {
		w := strings.TrimSpace(words[i])
		if w == "" {
			i++
			continue
		}
		// Skip leading env assignments like KEY=VALUE.
		if strings.Contains(w, "=") && !strings.Contains(w, "/") {
			i++
			continue
		}
		// Skip common wrappers so "sudo rm -rf" still resolves to rm.
		switch strings.ToLower(w) {
		case "sudo", "env", "command", "builtin", "time", "nohup":
			i++
			continue
		}
		if strings.ContainsRune(w, '/') {
			w = strings.TrimSpace(filepath.Base(w))
		}
		return strings.ToLower(w)
	}
	return ""
}
