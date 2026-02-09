package security

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var dangerousCmdPattern = regexp.MustCompile(`(^|[\s;&|()])(rm|mv|chmod|chown|dd|mkfs|shutdown|reboot)([\s;&|()]|$)`)

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

	if dangerousCmdPattern.MatchString(trimmed) {
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
