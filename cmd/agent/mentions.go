package main

import (
	"fmt"
	"os"
	"strings"

	"coder/internal/security"
)

func expandFileMentions(input string, ws *security.Workspace) string {
	if ws == nil || strings.TrimSpace(input) == "" || strings.HasPrefix(strings.TrimSpace(input), "!") {
		return input
	}
	matches := mentionPattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return input
	}

	var snippets []string
	seen := map[string]struct{}{}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		path := strings.TrimSpace(m[1])
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		resolved, err := ws.Resolve(path)
		if err != nil {
			snippets = append(snippets, fmt.Sprintf("@%s -> [error] %v", path, err))
			continue
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			snippets = append(snippets, fmt.Sprintf("@%s -> [error] %v", path, err))
			continue
		}
		content := string(data)
		r := []rune(content)
		if len(r) > 4000 {
			content = string(r[:4000]) + "\n...[truncated]"
		}
		snippets = append(snippets, fmt.Sprintf("@%s:\n%s", path, content))
	}
	if len(snippets) == 0 {
		return input
	}
	return input + "\n\n[FILE_MENTIONS]\n" + strings.Join(snippets, "\n\n")
}
