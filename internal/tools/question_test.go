package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type mockQuestionPrompter struct {
	answers   []string
	cancelled bool
	err       error
}

func (m *mockQuestionPrompter) PromptQuestion(_ context.Context, _ QuestionRequest) (*QuestionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &QuestionResponse{Answers: m.answers, Cancelled: m.cancelled}, nil
}

func TestQuestionToolDefinition(t *testing.T) {
	tool := NewQuestionTool()
	if tool.Name() != "question" {
		t.Fatalf("expected name=question, got %q", tool.Name())
	}
	def := tool.Definition()
	if def.Function.Name != "question" {
		t.Fatalf("expected function name=question, got %q", def.Function.Name)
	}
	params, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters.properties missing")
	}
	if _, ok := params["questions"]; !ok {
		t.Fatal("parameters.properties.questions missing")
	}
}

func TestQuestionToolExecuteWithOptionSelection(t *testing.T) {
	tool := NewQuestionTool()
	prompter := &mockQuestionPrompter{answers: []string{"重构整个模块"}}
	ctx := WithQuestionPrompter(context.Background(), prompter)

	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{
			{
				"question": "如何处理这个模块？",
				"options": []map[string]any{
					{"label": "重构整个模块", "description": "完全重写"},
					{"label": "只修改核心函数", "description": "最小改动"},
				},
			},
		},
	})
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"重构整个模块"`) {
		t.Fatalf("expected answer in result, got: %s", result)
	}
	if !strings.Contains(result, "User has answered") {
		t.Fatalf("expected formatted output, got: %s", result)
	}
}

func TestQuestionToolExecuteWithCustomText(t *testing.T) {
	tool := NewQuestionTool()
	prompter := &mockQuestionPrompter{answers: []string{"我想先看看依赖关系再决定"}}
	ctx := WithQuestionPrompter(context.Background(), prompter)

	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{
			{
				"question": "如何处理？",
				"options": []map[string]any{
					{"label": "A", "description": "选项A"},
					{"label": "B", "description": "选项B"},
				},
			},
		},
	})
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"我想先看看依赖关系再决定"`) {
		t.Fatalf("expected custom answer in result, got: %s", result)
	}
}

func TestQuestionToolExecuteCancelled(t *testing.T) {
	tool := NewQuestionTool()
	prompter := &mockQuestionPrompter{cancelled: true}
	ctx := WithQuestionPrompter(context.Background(), prompter)

	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{
			{
				"question": "test?",
				"options": []map[string]any{
					{"label": "A", "description": "a"},
					{"label": "B", "description": "b"},
				},
			},
		},
	})
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "dismissed") {
		t.Fatalf("expected dismissed message, got: %s", result)
	}
}

func TestQuestionToolExecuteNoPrompter(t *testing.T) {
	tool := NewQuestionTool()
	ctx := context.Background()

	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{
			{
				"question": "test?",
				"options": []map[string]any{
					{"label": "A", "description": "a"},
					{"label": "B", "description": "b"},
				},
			},
		},
	})
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "unavailable") {
		t.Fatalf("expected unavailable message, got: %s", result)
	}
}

func TestQuestionToolValidation(t *testing.T) {
	tool := NewQuestionTool()
	ctx := context.Background()

	t.Run("empty questions", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{"questions": []any{}})
		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Fatal("expected error for empty questions")
		}
	})

	t.Run("empty question text", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"questions": []map[string]any{
				{
					"question": "",
					"options": []map[string]any{
						{"label": "A", "description": "a"},
						{"label": "B", "description": "b"},
					},
				},
			},
		})
		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Fatal("expected error for empty question text")
		}
	})

	t.Run("too few options", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"questions": []map[string]any{
				{
					"question": "test?",
					"options":  []map[string]any{{"label": "A", "description": "a"}},
				},
			},
		})
		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Fatal("expected error for too few options")
		}
	})
}

func TestQuestionToolMultipleQuestions(t *testing.T) {
	tool := NewQuestionTool()
	prompter := &mockQuestionPrompter{answers: []string{"选项A", "自定义回答"}}
	ctx := WithQuestionPrompter(context.Background(), prompter)

	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{
			{
				"question": "第一个问题？",
				"options": []map[string]any{
					{"label": "选项A", "description": "a"},
					{"label": "选项B", "description": "b"},
				},
			},
			{
				"question": "第二个问题？",
				"options": []map[string]any{
					{"label": "X", "description": "x"},
					{"label": "Y", "description": "y"},
				},
			},
		},
	})
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"选项A"`) {
		t.Fatalf("expected first answer, got: %s", result)
	}
	if !strings.Contains(result, `"自定义回答"`) {
		t.Fatalf("expected second answer, got: %s", result)
	}
}

func TestResolveQuestionAnswer(t *testing.T) {
	options := []QuestionOption{
		{Label: "重构整个模块", Description: "完全重写"},
		{Label: "只修改核心函数", Description: "最小改动"},
		{Label: "保持不变", Description: "不动"},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"1", "重构整个模块"},
		{"2", "只修改核心函数"},
		{"3", "保持不变"},
		{"0", "0"},
		{"4", "4"},
		{"我想先看看", "我想先看看"},
		{"abc", "abc"},
		{"", ""},
		{"12", "12"},
	}
	for _, tt := range tests {
		got := ResolveQuestionAnswer(tt.input, options)
		if got != tt.expected {
			t.Errorf("ResolveQuestionAnswer(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestQuestionPrompterContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := QuestionPrompterFromContext(ctx); ok {
		t.Fatal("expected no prompter in empty context")
	}

	prompter := &mockQuestionPrompter{}
	ctx = WithQuestionPrompter(ctx, prompter)
	got, ok := QuestionPrompterFromContext(ctx)
	if !ok {
		t.Fatal("expected prompter in context")
	}
	if got != prompter {
		t.Fatal("expected same prompter instance")
	}
}
