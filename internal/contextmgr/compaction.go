package contextmgr

import (
	"context"
	"fmt"
	"strings"

	"coder/internal/chat"
)

// CompactionStrategy 上下文压缩策略接口
// CompactionStrategy defines the context compaction interface
type CompactionStrategy interface {
	// Summarize 生成消息历史的摘要
	// Summarize generates a summary of message history
	Summarize(ctx context.Context, messages []chat.Message) (string, error)
}

// LLMSummarizer 使用 LLM 进行摘要的函数类型
// LLMSummarizer is a function that calls an LLM for summarization
type LLMSummarizer func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// LLMCompaction 使用 LLM 生成摘要的策略
// LLMCompaction uses LLM to generate summaries
type LLMCompaction struct {
	summarize LLMSummarizer
	maxTokens int
}

// NewLLMCompaction 创建 LLM compaction 策略
// NewLLMCompaction creates an LLM compaction strategy
func NewLLMCompaction(summarize LLMSummarizer, maxTokens int) *LLMCompaction {
	if maxTokens <= 0 {
		maxTokens = 500
	}
	return &LLMCompaction{
		summarize: summarize,
		maxTokens: maxTokens,
	}
}

const summarySystemPrompt = `You are a precise summarizer for an AI coding assistant conversation.
Summarize the conversation preserving:
1. Current objective and task description
2. Files modified, created, or read (with paths)
3. Key decisions and changes made
4. Pending issues or risks
5. Next actionable steps

Be concise but complete. Output plain text, no markdown formatting.
Respond in the same language as the conversation content.`

func (c *LLMCompaction) Summarize(ctx context.Context, messages []chat.Message) (string, error) {
	if c.summarize == nil {
		return "", fmt.Errorf("LLM summarizer not configured")
	}

	userPrompt := buildSummaryInput(messages)
	if strings.TrimSpace(userPrompt) == "" {
		return "", fmt.Errorf("no content to summarize")
	}

	summary, err := c.summarize(ctx, summarySystemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("LLM summarize: %w", err)
	}
	return strings.TrimSpace(summary), nil
}

// RegexCompaction v1 兼容的正则提取策略
// RegexCompaction is the v1-compatible regex extraction strategy
type RegexCompaction struct{}

func (c *RegexCompaction) Summarize(_ context.Context, messages []chat.Message) (string, error) {
	summary := summarizeMessages(messages)
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("regex summarize: empty result")
	}
	return summary, nil
}

// FallbackCompaction 带回退的策略: 先 LLM，失败则 regex
// FallbackCompaction tries LLM first, falls back to regex
type FallbackCompaction struct {
	primary  CompactionStrategy
	fallback CompactionStrategy
}

// NewFallbackCompaction 创建带回退的 compaction 策略
// NewFallbackCompaction creates a compaction strategy with fallback
func NewFallbackCompaction(primary, fallback CompactionStrategy) *FallbackCompaction {
	return &FallbackCompaction{primary: primary, fallback: fallback}
}

func (c *FallbackCompaction) Summarize(ctx context.Context, messages []chat.Message) (string, error) {
	if c.primary != nil {
		summary, err := c.primary.Summarize(ctx, messages)
		if err == nil && strings.TrimSpace(summary) != "" {
			return summary, nil
		}
	}
	if c.fallback != nil {
		return c.fallback.Summarize(ctx, messages)
	}
	return "", fmt.Errorf("all compaction strategies failed")
}

// CompactWithStrategy 使用指定策略执行 compaction
// CompactWithStrategy executes compaction with the specified strategy
func CompactWithStrategy(ctx context.Context, messages []chat.Message, keepRecent int, pruneToolOutputs bool, strategy CompactionStrategy) ([]chat.Message, string, bool) {
	if keepRecent < 4 {
		keepRecent = 4
	}
	if len(messages) <= keepRecent+2 {
		return messages, "", false
	}

	msgs := append([]chat.Message(nil), messages...)
	if pruneToolOutputs {
		for i := range msgs {
			if msgs[i].Role != "tool" {
				continue
			}
			msgs[i].Content = pruneToolOutput(msgs[i].Content)
		}
	}

	split := len(msgs) - keepRecent
	if split < 1 {
		split = 1
	}
	head := msgs[:split]
	tail := msgs[split:]

	var summary string
	if strategy != nil {
		s, err := strategy.Summarize(ctx, head)
		if err == nil && strings.TrimSpace(s) != "" {
			summary = s
		}
	}

	// 如果 strategy 失败，回退到内置的 regex / Fallback to built-in regex
	if strings.TrimSpace(summary) == "" {
		summary = summarizeMessages(head)
	}

	if strings.TrimSpace(summary) == "" {
		return msgs, "", false
	}

	compacted := make([]chat.Message, 0, len(tail)+1)
	compacted = append(compacted, chat.Message{
		Role:    "assistant",
		Content: "[COMPACTION_SUMMARY]\n" + summary,
	})
	compacted = append(compacted, tail...)
	return compacted, summary, true
}

// buildSummaryInput 从消息列表构建摘要输入文本
// buildSummaryInput builds summarization input from messages
func buildSummaryInput(messages []chat.Message) string {
	var b strings.Builder
	b.WriteString("Conversation to summarize:\n\n")

	for _, m := range messages {
		switch m.Role {
		case "user":
			b.WriteString("User: ")
			content := strings.TrimSpace(m.Content)
			if len([]rune(content)) > 500 {
				content = string([]rune(content)[:500]) + "..."
			}
			b.WriteString(content)
			b.WriteString("\n\n")
		case "assistant":
			content := strings.TrimSpace(m.Content)
			if content != "" {
				if len([]rune(content)) > 300 {
					content = string([]rune(content)[:300]) + "..."
				}
				b.WriteString("Assistant: ")
				b.WriteString(content)
				b.WriteString("\n\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString(fmt.Sprintf("Tool call: %s(%s)\n", tc.Function.Name,
					truncateArgs(tc.Function.Arguments, 100)))
			}
		case "tool":
			if m.Name != "" {
				result := strings.TrimSpace(m.Content)
				if len([]rune(result)) > 200 {
					result = string([]rune(result)[:200]) + "..."
				}
				b.WriteString(fmt.Sprintf("Tool result [%s]: %s\n\n", m.Name, result))
			}
		}
	}
	return b.String()
}

func truncateArgs(args string, maxRunes int) string {
	r := []rune(strings.TrimSpace(args))
	if len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes]) + "..."
}
