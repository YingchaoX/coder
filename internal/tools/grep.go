package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"coder/internal/chat"
	"coder/internal/security"
)

type GrepTool struct {
	ws *security.Workspace
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func NewGrepTool(ws *security.Workspace) *GrepTool {
	return &GrepTool{ws: ws}
}

func (t *GrepTool) Name() string {
	return "grep"
}

func (t *GrepTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Search text content recursively in workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string"},
					"path":        map[string]any{"type": "string"},
					"max_matches": map[string]any{"type": "integer"},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func (t *GrepTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		MaxMatches int    `json:"max_matches"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("grep args: %w", err)
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return "", fmt.Errorf("grep pattern is empty")
	}
	if in.Path == "" {
		in.Path = "."
	}
	if in.MaxMatches <= 0 {
		in.MaxMatches = 200
	}

	root, err := t.ws.Resolve(in.Path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return "", fmt.Errorf("compile pattern: %w", err)
	}

	matches := make([]grepMatch, 0, in.MaxMatches)

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if len(matches) >= in.MaxMatches {
			return io.EOF
		}
		ok, err := isTextFile(path)
		if err != nil || !ok {
			return nil
		}
		if err := grepFile(path, re, t.ws.Root(), &matches, in.MaxMatches); err != nil {
			return nil
		}
		return nil
	})
	if walkErr != nil && walkErr != io.EOF {
		return "", fmt.Errorf("walk files: %w", walkErr)
	}

	return mustJSON(map[string]any{
		"ok":      true,
		"pattern": in.Pattern,
		"matches": matches,
		"count":   len(matches),
	}), nil
}

func grepFile(path string, re *regexp.Regexp, root string, matches *[]grepMatch, max int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	lineNo := 0
	rel, _ := filepath.Rel(root, path)
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if re.MatchString(line) {
			*matches = append(*matches, grepMatch{
				Path: rel,
				Line: lineNo,
				Text: line,
			})
			if len(*matches) >= max {
				return io.EOF
			}
		}
	}
	if err := scanner.Err(); err != nil && err != bufio.ErrTooLong {
		return err
	}
	return nil
}

func isTextFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 2048)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	sample := buf[:n]
	return !bytes.Contains(sample, []byte{0}), nil
}
