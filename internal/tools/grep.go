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
	"sort"
	"strings"

	"coder/internal/chat"
	"coder/internal/security"
)

type GrepTool struct {
	ws *security.Workspace
}

const (
	defaultGrepMaxMatches       = 200
	defaultGrepMaxScannedFiles  = 5000
	defaultGrepMaxFileSizeBytes = 2 << 20
)

var defaultIgnoredDirNames = map[string]struct{}{
	".git":         {},
	".svn":         {},
	".hg":          {},
	"node_modules": {},
	"dist":         {},
	"build":        {},
	"vendor":       {},
	".venv":        {},
	"venv":         {},
	".next":        {},
	"target":       {},
	"coverage":     {},
	"tmp":          {},
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
		in.MaxMatches = defaultGrepMaxMatches
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
	filesScanned := 0
	truncated := false

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(t.ws.Root(), path)
		if relErr != nil {
			rel = d.Name()
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if shouldSkipGrepDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipGrepFile(rel, d.Name()) {
			return nil
		}
		if len(matches) >= in.MaxMatches || filesScanned >= defaultGrepMaxScannedFiles {
			truncated = true
			return io.EOF
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.Size() > defaultGrepMaxFileSizeBytes {
			return nil
		}
		ok, err := isTextFile(path)
		if err != nil || !ok {
			return nil
		}
		filesScanned++
		if err := grepFile(path, rel, re, &matches, in.MaxMatches); err != nil {
			if err == io.EOF {
				truncated = true
				return io.EOF
			}
			return nil
		}
		return nil
	})
	if walkErr != nil && walkErr != io.EOF {
		return "", fmt.Errorf("walk files: %w", walkErr)
	}

	return mustJSON(map[string]any{
		"ok":               true,
		"pattern":          in.Pattern,
		"matches":          matches,
		"count":            len(matches),
		"files_scanned":    filesScanned,
		"truncated":        truncated,
		"ignored_patterns": defaultGrepIgnoredPatterns(),
	}), nil
}

func shouldSkipGrepDir(rel, name string) bool {
	if name == "" || rel == "." {
		return false
	}
	if _, ok := defaultIgnoredDirNames[name]; ok {
		return true
	}
	return strings.HasPrefix(name, ".cache")
}

func shouldSkipGrepFile(rel, name string) bool {
	if strings.HasSuffix(rel, ".min.js") || strings.HasSuffix(rel, ".min.css") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".pdf", ".zip", ".tar", ".gz", ".tgz", ".jar", ".class", ".so", ".dylib":
		return true
	default:
		return false
	}
}

func defaultGrepIgnoredPatterns() []string {
	out := make([]string, 0, len(defaultIgnoredDirNames))
	for name := range defaultIgnoredDirNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func grepFile(path, rel string, re *regexp.Regexp, matches *[]grepMatch, max int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	lineNo := 0
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
