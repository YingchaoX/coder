package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAppUpdate_StateTransitions(t *testing.T) {
	app := NewApp("/tmp", "build", "gpt", "s1", nil)
	app.width, app.height = 100, 30
	app.relayout()

	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := m.(App)
	if updated.activePanel != PanelFiles {
		t.Fatalf("expected files panel, got %v", updated.activePanel)
	}

	updated.streaming = true
	m, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = m.(App)
	if updated.streaming {
		t.Fatalf("expected streaming false after esc")
	}
	if !strings.Contains(updated.logContent.String(), "Generation interrupted") {
		t.Fatalf("missing interruption log: %q", updated.logContent.String())
	}
}

func TestAppUpdate_StreamAndTurnDone(t *testing.T) {
	app := NewApp("/tmp", "build", "gpt", "s1", nil)
	app.width, app.height = 100, 30
	app.relayout()

	m, _ := app.Update(TextChunkMsg{Text: "hello"})
	updated := m.(App)
	if !updated.streaming || updated.streamBuffer.String() != "hello" {
		t.Fatalf("unexpected stream state")
	}

	m, _ = updated.Update(TurnDoneMsg{Content: "", Err: nil})
	updated = m.(App)
	if updated.streaming {
		t.Fatalf("expected streaming false")
	}
	if !strings.Contains(updated.chatContent.String(), "hello") {
		t.Fatalf("missing streamed content in chat: %q", updated.chatContent.String())
	}
}

func TestAppUpdate_ToolAndErrors(t *testing.T) {
	app := NewApp("/tmp", "build", "gpt", "s1", nil)
	app.width, app.height = 100, 30
	app.relayout()

	m, _ := app.Update(ToolStartMsg{Name: "read", Summary: "start"})
	updated := m.(App)
	if !strings.Contains(updated.chatContent.String(), "🔧 read") {
		t.Fatalf("missing tool start in chat")
	}

	m, _ = updated.Update(ToolDoneMsg{Name: "read", Summary: "done"})
	updated = m.(App)
	if !strings.Contains(updated.chatContent.String(), "✓ done") {
		t.Fatalf("missing tool done in chat")
	}

	err := errors.New("boom")
	m, _ = updated.Update(TurnErrorMsg{Err: err})
	updated = m.(App)
	if updated.lastError != "boom" {
		t.Fatalf("unexpected last error: %q", updated.lastError)
	}
}

func TestAppHelpers(t *testing.T) {
	head, detail := splitHeadAndDetail("a\nb\n")
	if head != "a" || detail != "b" {
		t.Fatalf("unexpected split: %q / %q", head, detail)
	}
	if !looksLikeDiff("@@ -1 +1 @@\n-old\n+new") {
		t.Fatalf("expected diff detection")
	}
	if got := indentBlock("x\n\n", "--"); !strings.Contains(got, "--x") {
		t.Fatalf("unexpected indent: %q", got)
	}
}
