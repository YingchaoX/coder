package repl

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"coder/internal/bootstrap"
)

func TestReadInput_SingleLine(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("hello\n"))
	lines, err := readInput(rd)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("want [hello], got %q", lines)
	}
}

func TestReadInput_SingleLineNoTrailingNewline(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("hello"))
	lines, err := readInput(rd)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("want [hello], got %q", lines)
	}
}

func TestReadInput_MultiLineUntilEOF(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("line1\nline2\nline3\n\n"))
	lines, err := readInput(rd)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	// non-TTY: read until EOF as one message; trailing empty line included
	if len(lines) != 4 || lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" || lines[3] != "" {
		t.Fatalf("want [line1 line2 line3 \"\"], got %q", lines)
	}
}

func TestReadInput_LeadingBlankIncluded(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("\n\nhi\n"))
	lines, err := readInput(rd)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	// non-TTY: read until EOF, all lines included
	if len(lines) != 3 || lines[0] != "" || lines[1] != "" || lines[2] != "hi" {
		t.Fatalf("want [\"\" \"\" hi], got %q", lines)
	}
}

func TestReadInput_SingleNewlineReturnsOneEmptyLine(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("\n"))
	lines, err := readInput(rd)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	// non-TTY: one line then EOF yields one empty line; caller TrimSpace skips as empty
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("want one empty line [\"\"], got %q", lines)
	}
}

func TestReadInput_EmptyStdinReturnsError(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader(""))
	_, err := readInput(rd)
	if err == nil {
		t.Fatal("want error on empty stdin")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("want 'closed' in error: %v", err)
	}
}

func TestNewLoop(t *testing.T) {
	res := &bootstrap.BuildResult{
		WorkspaceRoot: "/tmp",
		AgentName:     "build",
		Model:         "gpt-4o",
		SessionID:     "s1",
	}
	loop := NewLoop(res)
	if loop == nil {
		t.Fatal("NewLoop returned nil")
	}
	if loop.WorkspaceRoot != "/tmp" || loop.history == nil {
		t.Fatalf("loop state wrong: %+v", loop)
	}
}

func TestRun_NilOrchReturnsError(t *testing.T) {
	res := &bootstrap.BuildResult{Orch: nil, WorkspaceRoot: "/tmp"}
	loop := NewLoop(res)
	err := Run(loop)
	if err == nil {
		t.Fatal("Run with nil orch should return error")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("want nil in error: %v", err)
	}
}

func TestPrintPromptTo_Format(t *testing.T) {
	res := &bootstrap.BuildResult{
		Orch:          nil,
		WorkspaceRoot: "/path/to/cwd",
		Model:         "gpt-4o",
	}
	loop := NewLoop(res)
	loop.tokens = 1200
	loop.limit = 24000

	var buf bytes.Buffer
	loop.printPromptTo(&buf)
	out := buf.String()
	if !strings.Contains(out, "context: 1200 tokens") {
		t.Errorf("prompt should contain context line: %q", out)
	}
	if !strings.Contains(out, "model: gpt-4o") {
		t.Errorf("prompt should contain model: %q", out)
	}
	if !strings.Contains(out, "[build]") {
		t.Errorf("prompt should contain [build]: %q", out)
	}
	if !strings.Contains(out, "/path/to/cwd>") {
		t.Errorf("prompt should contain cwd: %q", out)
	}
}
