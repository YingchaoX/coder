package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Basic(t *testing.T) {
	input := "# Hello\n\nThis is **bold** text."
	result := RenderMarkdown(input, 80)
	if result == "" {
		t.Fatal("RenderMarkdown returned empty")
	}
	// Glamour 应该渲染了标题 / Glamour should have rendered the heading
	if !strings.Contains(result, "Hello") {
		t.Fatalf("result should contain 'Hello': %q", result)
	}
}

func TestRenderMarkdown_Empty(t *testing.T) {
	if RenderMarkdown("", 80) != "" {
		t.Fatal("empty input should return empty")
	}
	if RenderMarkdown("  ", 80) != "" {
		t.Fatal("whitespace input should return empty")
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfunc main() {}\n```"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "func") {
		t.Fatalf("code block should contain 'func': %q", result)
	}
}

func TestRenderDiffLine(t *testing.T) {
	theme := DarkTheme()

	tests := []struct {
		input  string
		expect string
	}{
		{"+added line", "added"},
		{"-removed line", "removed"},
		{"@@ -1,3 +1,4 @@", "@@"},
		{" context line", " context line"},
		{"", ""},
	}
	for _, tt := range tests {
		got := RenderDiffLine(tt.input, theme)
		if tt.expect != "" && !strings.Contains(got, tt.expect) {
			t.Errorf("RenderDiffLine(%q) should contain %q, got %q", tt.input, tt.expect, got)
		}
	}
}

func TestRenderDiff(t *testing.T) {
	theme := DarkTheme()
	diff := "--- a/file.go\n+++ b/file.go\n@@ -1,2 +1,3 @@\n context\n-old\n+new\n+added"
	result := RenderDiff(diff, theme)
	if result == "" {
		t.Fatal("RenderDiff returned empty")
	}
	if !strings.Contains(result, "new") {
		t.Fatalf("should contain 'new': %q", result)
	}
}
