package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coder/internal/chat"
)

// QuestionOption 选择题的一个选项
// QuestionOption is a single option in a question
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionInfo 一个完整的选择题
// QuestionInfo is a single question with options
type QuestionInfo struct {
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
}

// QuestionRequest 由 question tool 发起的提问请求
// QuestionRequest is a question request initiated by the question tool
type QuestionRequest struct {
	Questions []QuestionInfo
}

// QuestionResponse 用户对问题的回复
// QuestionResponse contains user answers to questions
type QuestionResponse struct {
	Answers   []string
	Cancelled bool
}

// QuestionPrompter 终端层实现此接口来展示选择题并获取用户回复
// QuestionPrompter is implemented by the terminal layer to render questions and collect answers
type QuestionPrompter interface {
	PromptQuestion(ctx context.Context, req QuestionRequest) (*QuestionResponse, error)
}

type questionPrompterContextKey struct{}

// WithQuestionPrompter 将 QuestionPrompter 注入到 context 中
// WithQuestionPrompter injects a QuestionPrompter into the context
func WithQuestionPrompter(ctx context.Context, p QuestionPrompter) context.Context {
	if ctx == nil || p == nil {
		return ctx
	}
	return context.WithValue(ctx, questionPrompterContextKey{}, p)
}

// QuestionPrompterFromContext 从 context 中提取 QuestionPrompter
// QuestionPrompterFromContext extracts a QuestionPrompter from the context
func QuestionPrompterFromContext(ctx context.Context) (QuestionPrompter, bool) {
	if ctx == nil {
		return nil, false
	}
	v := ctx.Value(questionPrompterContextKey{})
	if v == nil {
		return nil, false
	}
	p, ok := v.(QuestionPrompter)
	return p, ok
}

type QuestionTool struct{}

func NewQuestionTool() *QuestionTool {
	return &QuestionTool{}
}

func (t *QuestionTool) Name() string {
	return "question"
}

func (t *QuestionTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name: t.Name(),
			Description: "Ask the user questions to clarify ambiguous instructions, gather preferences, " +
				"or get decisions on implementation choices during planning. " +
				"Each question is presented as a numbered list; the first option is the recommended choice. " +
				"The user can select an option by number or type a custom answer.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"questions": map[string]any{
						"type":        "array",
						"description": "Questions to ask the user",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"question": map[string]any{
									"type":        "string",
									"description": "The question text",
								},
								"options": map[string]any{
									"type":        "array",
									"description": "Available choices; the first option is the recommended one",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"label": map[string]any{
												"type":        "string",
												"description": "Display text for the option (concise, 1-10 words)",
											},
											"description": map[string]any{
												"type":        "string",
												"description": "Explanation of what this choice means",
											},
										},
										"required": []string{"label", "description"},
									},
								},
							},
							"required": []string{"question", "options"},
						},
					},
				},
				"required": []string{"questions"},
			},
		},
	}
}

// ResolveQuestionAnswer 将用户输入解析为选项 label 或自定义文本
// ResolveQuestionAnswer resolves user input to an option label or custom text
func ResolveQuestionAnswer(input string, options []QuestionOption) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	n := 0
	isDigit := true
	for _, r := range input {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		} else {
			isDigit = false
			break
		}
	}
	if isDigit && n >= 1 && n <= len(options) {
		return options[n-1].Label
	}
	return input
}

func (t *QuestionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Questions []QuestionInfo `json:"questions"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if len(input.Questions) == 0 {
		return "", fmt.Errorf("at least one question is required")
	}
	for i, q := range input.Questions {
		if strings.TrimSpace(q.Question) == "" {
			return "", fmt.Errorf("question %d has empty text", i+1)
		}
		if len(q.Options) < 2 {
			return "", fmt.Errorf("question %d must have at least 2 options", i+1)
		}
	}

	prompter, ok := QuestionPrompterFromContext(ctx)
	if !ok {
		return "Question tool is unavailable: no interactive terminal. Proceed with your best judgment.", nil
	}

	resp, err := prompter.PromptQuestion(ctx, QuestionRequest{Questions: input.Questions})
	if err != nil {
		return "", fmt.Errorf("question prompt: %w", err)
	}
	if resp.Cancelled {
		return "User dismissed the questions. Do not repeat these questions. Proceed with your best judgment or take a different approach.", nil
	}

	var parts []string
	for i, q := range input.Questions {
		answer := ""
		if i < len(resp.Answers) {
			answer = resp.Answers[i]
		}
		if answer == "" {
			answer = "Unanswered"
		}
		parts = append(parts, fmt.Sprintf("%q=%q", q.Question, answer))
	}
	return fmt.Sprintf("User has answered your questions: %s. You can now continue with the user's answers in mind.", strings.Join(parts, ", ")), nil
}
