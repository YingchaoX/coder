package provider

import (
	"strings"
	"testing"

	"coder/internal/chat"
)

func TestConvertMessages(t *testing.T) {
	messages := []chat.Message{
		{Role: "system", Content: "You are a helper"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", ToolCalls: []chat.ToolCall{
			{ID: "call_1", Type: "function", Function: chat.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`}},
		}},
		{Role: "tool", Name: "read", ToolCallID: "call_1", Content: `{"ok":true}`},
	}

	converted := convertMessages(messages)
	if len(converted) != 4 {
		t.Fatalf("convertMessages len=%d, want 4", len(converted))
	}
	if converted[0].Role != "system" || converted[0].Content != "You are a helper" {
		t.Fatalf("msg[0] unexpected: %+v", converted[0])
	}
	if len(converted[2].ToolCalls) != 1 || converted[2].ToolCalls[0].Function.Name != "read" {
		t.Fatalf("msg[2] tool calls unexpected: %+v", converted[2])
	}
	if converted[3].ToolCallID != "call_1" {
		t.Fatalf("msg[3] ToolCallID=%q, want call_1", converted[3].ToolCallID)
	}
}

func TestConvertTools(t *testing.T) {
	tools := []chat.ToolDef{
		{
			Type: "function",
			Function: chat.ToolFunction{
				Name:        "read",
				Description: "Read a file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	converted := convertTools(tools)
	if len(converted) != 1 {
		t.Fatalf("convertTools len=%d, want 1", len(converted))
	}
	if converted[0].Function.Name != "read" {
		t.Fatalf("tool[0].Name=%q, want read", converted[0].Function.Name)
	}
}

func TestAssembleToolCalls(t *testing.T) {
	byIdx := map[int]*toolCallAccumulator{
		0: {id: "call_abc", typ: "function", name: "bash"},
		1: {id: "call_def", typ: "function", name: "read"},
	}
	byIdx[0].args.WriteString(`{"command":"ls"}`)
	byIdx[1].args.WriteString(`{"path":"main.go"}`)

	calls := assembleToolCalls(byIdx)
	if len(calls) != 2 {
		t.Fatalf("assembleToolCalls len=%d, want 2", len(calls))
	}
	if calls[0].Function.Name != "bash" || calls[0].ID != "call_abc" {
		t.Fatalf("call[0] unexpected: %+v", calls[0])
	}
	if calls[1].Function.Name != "read" {
		t.Fatalf("call[1] unexpected: %+v", calls[1])
	}
}

func TestAssembleToolCalls_Empty(t *testing.T) {
	calls := assembleToolCalls(map[int]*toolCallAccumulator{})
	if calls != nil {
		t.Fatalf("empty should return nil, got %v", calls)
	}
}

func TestAssembleToolCalls_MissingID(t *testing.T) {
	byIdx := map[int]*toolCallAccumulator{
		0: {typ: "function", name: "test"},
	}
	calls := assembleToolCalls(byIdx)
	if len(calls) != 1 {
		t.Fatalf("len=%d, want 1", len(calls))
	}
	if !strings.HasPrefix(calls[0].ID, "call_") {
		t.Fatalf("ID=%q, should have call_ prefix", calls[0].ID)
	}
}

func TestOpenAIProviderSetModel(t *testing.T) {
	p := &OpenAIProvider{model: "gpt-4"}
	if p.CurrentModel() != "gpt-4" {
		t.Fatalf("CurrentModel()=%q, want gpt-4", p.CurrentModel())
	}
	if err := p.SetModel("gpt-3.5-turbo"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if p.CurrentModel() != "gpt-3.5-turbo" {
		t.Fatalf("CurrentModel()=%q after set, want gpt-3.5-turbo", p.CurrentModel())
	}
	if err := p.SetModel(""); err == nil {
		t.Fatal("SetModel empty should error")
	}
}

func TestOpenAIProviderName(t *testing.T) {
	p := &OpenAIProvider{}
	if p.Name() != "openai" {
		t.Fatalf("Name()=%q, want openai", p.Name())
	}
}
