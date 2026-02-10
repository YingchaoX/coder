package contextmgr

import (
	"testing"

	"coder/internal/chat"
)

func TestTokenizer_Heuristic(t *testing.T) {
	// 即使 tiktoken 不可用，启发式也应该可用
	// Heuristic should always work even without tiktoken
	tok := &Tokenizer{fallback: true, encodingName: "cl100k_base"}

	count := tok.CountText("Hello world")
	if count <= 0 {
		t.Fatalf("heuristic CountText should return > 0, got %d", count)
	}

	// CJK 文本
	cjkCount := tok.CountText("你好世界")
	if cjkCount <= 0 {
		t.Fatalf("heuristic CountText for CJK should return > 0, got %d", cjkCount)
	}
}

func TestTokenizer_CountMessages(t *testing.T) {
	tok := &Tokenizer{fallback: true, encodingName: "cl100k_base"}

	messages := []chat.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	count := tok.Count(messages)
	if count <= 0 {
		t.Fatalf("Count should return > 0, got %d", count)
	}
}

func TestTokenizer_EmptyText(t *testing.T) {
	tok := &Tokenizer{fallback: true}
	if tok.CountText("") != 0 {
		t.Fatal("empty text should return 0")
	}
}

func TestModelToEncoding(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"gpt-4", "cl100k_base"},
		{"gpt-3.5-turbo", "cl100k_base"},
		{"gpt-4o-mini", "o200k_base"},
		{"o1-preview", "o200k_base"},
		{"o3-mini", "o200k_base"},
		{"qwen-plus", "cl100k_base"},
		{"qwen2.5-coder-32b-instruct", "cl100k_base"},
		{"claude-3-opus", "cl100k_base"},
		{"", "cl100k_base"},
		{"unknown-model", "cl100k_base"},
	}
	for _, tt := range tests {
		got := modelToEncoding(tt.model)
		if got != tt.expected {
			t.Errorf("modelToEncoding(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}

func TestTokenizer_IsPrecise(t *testing.T) {
	fallbackTok := &Tokenizer{fallback: true}
	if fallbackTok.IsPrecise() {
		t.Fatal("fallback tokenizer should not be precise")
	}
}

func TestEstimateTokens_UsesTokenizer(t *testing.T) {
	// 确保 EstimateTokens 函数仍然可用
	// Ensure EstimateTokens function still works
	messages := []chat.Message{
		{Role: "user", Content: "hello world"},
	}
	count := EstimateTokens(messages)
	if count <= 0 {
		t.Fatalf("EstimateTokens should return > 0, got %d", count)
	}
}

func TestHeuristicTokenCount(t *testing.T) {
	tests := []struct {
		input string
		minOK bool
	}{
		{"Hello world, this is a test.", true},
		{"你好世界，这是一个测试。", true},
		{"Mixed 混合 text 文本", true},
		{"", false},
	}
	for _, tt := range tests {
		got := heuristicTokenCount(tt.input)
		if tt.minOK && got <= 0 {
			t.Errorf("heuristicTokenCount(%q) = %d, want > 0", tt.input, got)
		}
		if !tt.minOK && got != 0 {
			t.Errorf("heuristicTokenCount(%q) = %d, want 0", tt.input, got)
		}
	}
}
