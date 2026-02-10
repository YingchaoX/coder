package contextmgr

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"coder/internal/chat"
)

func TestRegexCompaction_Summarize(t *testing.T) {
	c := &RegexCompaction{}
	messages := []chat.Message{
		{Role: "user", Content: "Implement a function to sort files"},
		{Role: "assistant", Content: "I'll read the file first"},
		{Role: "tool", Name: "read", Content: `{"ok":true,"path":"main.go","content":"package main"}`},
	}

	summary, err := c.Summarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !strings.Contains(summary, "objective") {
		t.Fatalf("summary should contain 'objective': %q", summary)
	}
}

func TestLLMCompaction_Summarize(t *testing.T) {
	mockSummarizer := func(_ context.Context, sys, user string) (string, error) {
		if !strings.Contains(sys, "summarizer") {
			return "", fmt.Errorf("expected system prompt")
		}
		return "LLM summary: implemented file sorting", nil
	}

	c := NewLLMCompaction(mockSummarizer, 500)
	messages := []chat.Message{
		{Role: "user", Content: "Sort files"},
		{Role: "assistant", Content: "Done"},
	}

	summary, err := c.Summarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !strings.Contains(summary, "LLM summary") {
		t.Fatalf("summary should contain 'LLM summary': %q", summary)
	}
}

func TestLLMCompaction_NoSummarizer(t *testing.T) {
	c := NewLLMCompaction(nil, 500)
	_, err := c.Summarize(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error with nil summarizer")
	}
}

func TestFallbackCompaction(t *testing.T) {
	failingLLM := &LLMCompaction{
		summarize: func(_ context.Context, _, _ string) (string, error) {
			return "", fmt.Errorf("network error")
		},
	}
	regex := &RegexCompaction{}
	fallback := NewFallbackCompaction(failingLLM, regex)

	messages := []chat.Message{
		{Role: "user", Content: "Test fallback behavior"},
		{Role: "assistant", Content: "OK"},
	}

	summary, err := fallback.Summarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("Fallback should not error: %v", err)
	}
	if strings.TrimSpace(summary) == "" {
		t.Fatal("Fallback should produce non-empty summary")
	}
}

func TestCompactWithStrategy_TooFewMessages(t *testing.T) {
	messages := []chat.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	result, _, changed := CompactWithStrategy(context.Background(), messages, 4, false, nil)
	if changed {
		t.Fatal("should not compact with too few messages")
	}
	if len(result) != 2 {
		t.Fatalf("result len=%d, want 2", len(result))
	}
}

func TestCompactWithStrategy_WithLLM(t *testing.T) {
	messages := make([]chat.Message, 20)
	for i := range messages {
		if i%2 == 0 {
			messages[i] = chat.Message{Role: "user", Content: fmt.Sprintf("message %d", i)}
		} else {
			messages[i] = chat.Message{Role: "assistant", Content: fmt.Sprintf("response %d", i)}
		}
	}

	mockStrategy := &RegexCompaction{}
	result, summary, changed := CompactWithStrategy(context.Background(), messages, 4, false, mockStrategy)
	if !changed {
		t.Fatal("should have compacted")
	}
	if strings.TrimSpace(summary) == "" {
		t.Fatal("summary should not be empty")
	}
	if len(result) > len(messages) {
		t.Fatalf("compacted len=%d should be <= original len=%d", len(result), len(messages))
	}
	// 第一条应该是 compaction summary / First should be compaction summary
	if !strings.Contains(result[0].Content, "COMPACTION_SUMMARY") {
		t.Fatalf("first message should be summary: %q", result[0].Content)
	}
}

func TestBuildSummaryInput(t *testing.T) {
	messages := []chat.Message{
		{Role: "user", Content: "implement sorting"},
		{Role: "assistant", Content: "I'll implement it", ToolCalls: []chat.ToolCall{
			{Function: chat.ToolCallFunction{Name: "write", Arguments: `{"path":"sort.go","content":"..."}`}},
		}},
		{Role: "tool", Name: "write", Content: `{"ok":true}`},
	}

	input := buildSummaryInput(messages)
	if !strings.Contains(input, "User: implement sorting") {
		t.Fatalf("should contain user message: %q", input)
	}
	if !strings.Contains(input, "Tool call: write") {
		t.Fatalf("should contain tool call: %q", input)
	}
}
