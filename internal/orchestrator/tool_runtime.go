package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"coder/internal/tools"
)

type liveCommandStream struct {
	out             io.Writer
	file            *os.File
	logPath         string
	displayPath     string
	stdoutLineStart bool
	stderrLineStart bool
	mu              sync.Mutex
}

func newLiveCommandStream(workspaceRoot, sessionID, label string, out io.Writer) *liveCommandStream {
	stream := &liveCommandStream{
		out:             out,
		stdoutLineStart: true,
		stderrLineStart: true,
	}
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return stream
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "adhoc"
	}
	name := sanitizeRunLabel(label)
	if name == "" {
		name = "bash"
	}
	dir := filepath.Join(root, ".coder", "runs", sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stream
	}
	filename := fmt.Sprintf("%s-%s.log", time.Now().UTC().Format("20060102T150405Z"), name)
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	if err != nil {
		return stream
	}
	stream.file = f
	stream.logPath = path
	if rel, relErr := filepath.Rel(root, path); relErr == nil {
		stream.displayPath = filepath.ToSlash(rel)
	} else {
		stream.displayPath = path
	}
	return stream
}

func (s *liveCommandStream) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}

func (s *liveCommandStream) LogPath() string {
	if s == nil {
		return ""
	}
	if s.displayPath != "" {
		return s.displayPath
	}
	return s.logPath
}

func (s *liveCommandStream) OnCommandStart(_ string, _ string) {
	if s == nil || s.out == nil || s.LogPath() == "" {
		return
	}
	renderToolResult(s.out, "streaming logs to "+s.LogPath())
}

func (s *liveCommandStream) OnCommandChunk(_ string, stream, chunk string) {
	if s == nil || chunk == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		_, _ = s.file.WriteString(chunk)
	}
	if s.out == nil {
		return
	}
	s.writeChunk(stream, chunk)
}

func (s *liveCommandStream) OnCommandFinish(_ string, _ int, _ int64) {
	if s == nil || s.out == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stdoutLineStart {
		_, _ = fmt.Fprintln(s.out)
		s.stdoutLineStart = true
	}
	if !s.stderrLineStart {
		_, _ = fmt.Fprintln(s.out)
		s.stderrLineStart = true
	}
}

func (s *liveCommandStream) writeChunk(stream, chunk string) {
	lineStart := &s.stdoutLineStart
	prefix := "     | "
	color := ""
	if stream == "stderr" {
		lineStart = &s.stderrLineStart
		prefix = "     ! "
		color = ansiRed
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(chunk, "\r\n", "\n"), "\r", "\n")
	for _, ch := range normalized {
		if *lineStart {
			if color == "" {
				_, _ = fmt.Fprint(s.out, prefix)
			} else {
				_, _ = fmt.Fprint(s.out, style(prefix, color))
			}
			*lineStart = false
		}
		if ch == '\n' {
			_, _ = fmt.Fprint(s.out, "\n")
			*lineStart = true
			continue
		}
		text := string(ch)
		if color != "" {
			text = style(text, color)
		}
		_, _ = fmt.Fprint(s.out, text)
	}
}

func sanitizeRunLabel(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func (o *Orchestrator) executeToolWithRuntime(ctx context.Context, name string, args json.RawMessage, out io.Writer, runLabel string) (string, error) {
	if o == nil || o.registry == nil {
		return "", fmt.Errorf("tool registry unavailable")
	}
	var stream *liveCommandStream
	if strings.EqualFold(strings.TrimSpace(name), "bash") {
		stream = newLiveCommandStream(o.workspaceRoot, o.GetCurrentSessionID(), runLabel, out)
		ctx = tools.WithCommandStreamer(ctx, stream)
		defer stream.Close()
	}
	result, err := o.registry.Execute(ctx, name, args)
	if err != nil {
		return "", err
	}
	if stream != nil && stream.LogPath() != "" {
		result = attachCommandLogPath(result, stream.LogPath())
	}
	return result, nil
}

func attachCommandLogPath(rawResult, logPath string) string {
	if strings.TrimSpace(rawResult) == "" || strings.TrimSpace(logPath) == "" {
		return rawResult
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(rawResult), &obj); err != nil {
		return rawResult
	}
	if _, exists := obj["log_path"]; !exists {
		obj["log_path"] = logPath
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return rawResult
	}
	return string(data)
}

func (o *Orchestrator) checkpointSession(ctx context.Context) {
	if o == nil {
		return
	}
	_ = o.flushSessionToFile(ctx)
}
