package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"coder/internal/chat"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIProvider 使用 go-openai SDK 的 Provider 实现
// OpenAIProvider implements Provider using the go-openai SDK
type OpenAIProvider struct {
	client     *openai.Client
	model      string
	cfg        OpenAIConfig
	mu         sync.RWMutex
}

// OpenAIConfig SDK provider 配置
// OpenAIConfig is the SDK provider configuration
type OpenAIConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	TimeoutMS   int
	MaxRetries  int
	ReasoningOn bool
}

// NewOpenAIProvider 创建基于 SDK 的 provider
// NewOpenAIProvider creates an SDK-based provider
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	config := openai.DefaultConfig(cfg.APIKey)
	config.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.TimeoutMS > 0 {
		// 注意: SDK 内部处理超时，这里不设置 HTTP 超时
		// Note: timeout for the HTTP client is handled differently for streaming
	}

	client := openai.NewClientWithConfig(config)
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	return &OpenAIProvider{
		client: client,
		model:  cfg.Model,
		cfg:    cfg,
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

func (p *OpenAIProvider) CurrentModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

func (p *OpenAIProvider) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model is empty")
	}
	p.mu.Lock()
	p.model = model
	p.mu.Unlock()
	return nil
}

func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	resp, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	models := make([]ModelInfo, 0, len(resp.Models))
	for _, m := range resp.Models {
		models = append(models, ModelInfo{
			ID:      m.ID,
			OwnedBy: m.OwnedBy,
		})
	}
	return models, nil
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest, cb *StreamCallbacks) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.CurrentModel()
	}

	messages := convertMessages(req.Messages)
	sdkReq := openai.ChatCompletionRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}

	if len(req.Tools) > 0 {
		sdkReq.Tools = convertTools(req.Tools)
		sdkReq.ToolChoice = "auto"
	}
	if req.Temperature != nil {
		sdkReq.Temperature = float32(*req.Temperature)
	}
	if req.TopP != nil {
		sdkReq.TopP = float32(*req.TopP)
	}
	if req.MaxTokens > 0 {
		sdkReq.MaxTokens = req.MaxTokens
	}

	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(150*(1<<(attempt-1))) * time.Millisecond
			select {
			case <-ctx.Done():
				return ChatResponse{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err := p.chatStream(ctx, sdkReq, cb)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// 不可重试的错误 / Non-retryable errors
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ChatResponse{}, err
		}
		if attempt >= p.cfg.MaxRetries {
			break
		}
	}
	return ChatResponse{}, fmt.Errorf("provider chat failed after %d retries: %w", p.cfg.MaxRetries, lastErr)
}

func (p *OpenAIProvider) chatStream(ctx context.Context, req openai.ChatCompletionRequest, cb *StreamCallbacks) (ChatResponse, error) {
	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create stream: %w", err)
	}
	defer stream.Close()

	var (
		contentBuilder   strings.Builder
		reasoningBuilder strings.Builder
		toolCallsByIdx   = map[int]*toolCallAccumulator{}
		finishReason     string
		usage            Usage
	)

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// 如果已经收到部分内容，返回已有的而不是报错
			// If we already have partial content, return what we have
			if contentBuilder.Len() > 0 || len(toolCallsByIdx) > 0 {
				break
			}
			return ChatResponse{}, fmt.Errorf("recv stream: %w", err)
		}

		for _, choice := range resp.Choices {
			if choice.FinishReason != "" {
				finishReason = string(choice.FinishReason)
			}

			// 文本内容 / Text content
			if choice.Delta.Content != "" {
				contentBuilder.WriteString(choice.Delta.Content)
				if cb != nil && cb.OnTextChunk != nil {
					cb.OnTextChunk(choice.Delta.Content)
				}
			}

			// Reasoning 内容 (o1/o3 模型)
			// Reasoning content (o1/o3 models)
			if choice.Delta.ReasoningContent != "" {
				reasoningBuilder.WriteString(choice.Delta.ReasoningContent)
				if cb != nil && cb.OnReasoningChunk != nil {
					cb.OnReasoningChunk(choice.Delta.ReasoningContent)
				}
			}

			// Tool calls
			for _, tc := range choice.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				acc, ok := toolCallsByIdx[idx]
				if !ok {
					acc = &toolCallAccumulator{}
					toolCallsByIdx[idx] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Type != "" {
					acc.typ = string(tc.Type)
				}
				if tc.Function.Name != "" {
					acc.name += tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.args.WriteString(tc.Function.Arguments)
				}
			}
		}

		// Usage (部分 provider 在最后一个 chunk 中返回)
		// Usage (some providers return it in the last chunk)
		if resp.Usage != nil {
			usage = Usage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
			}
			if resp.Usage.CompletionTokensDetails != nil {
				usage.ReasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}
	}

	// 组装 tool calls / Assemble tool calls
	toolCalls := assembleToolCalls(toolCallsByIdx)
	if cb != nil && cb.OnToolCall != nil {
		for _, tc := range toolCalls {
			cb.OnToolCall(tc)
		}
	}
	if cb != nil && cb.OnUsage != nil {
		cb.OnUsage(usage)
	}

	return ChatResponse{
		Content:      contentBuilder.String(),
		Reasoning:    reasoningBuilder.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

type toolCallAccumulator struct {
	id   string
	typ  string
	name string
	args strings.Builder
}

func assembleToolCalls(byIdx map[int]*toolCallAccumulator) []chat.ToolCall {
	if len(byIdx) == 0 {
		return nil
	}
	// 按 index 排序 / Sort by index
	maxIdx := 0
	for idx := range byIdx {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	calls := make([]chat.ToolCall, 0, len(byIdx))
	for i := 0; i <= maxIdx; i++ {
		acc, ok := byIdx[i]
		if !ok {
			continue
		}
		id := strings.TrimSpace(acc.id)
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		typ := strings.TrimSpace(acc.typ)
		if typ == "" {
			typ = "function"
		}
		calls = append(calls, chat.ToolCall{
			ID:   id,
			Type: typ,
			Function: chat.ToolCallFunction{
				Name:      strings.TrimSpace(acc.name),
				Arguments: acc.args.String(),
			},
		})
	}
	return calls
}

// --- Message / Tool Conversion ---

func convertMessages(messages []chat.Message) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, m := range messages {
		msg := openai.ChatCompletionMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]openai.ToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolType(tc.Type),
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		out = append(out, msg)
	}
	return out
}

func convertTools(tools []chat.ToolDef) []openai.Tool {
	out := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	return out
}
