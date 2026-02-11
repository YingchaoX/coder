package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	httpClient *http.Client
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

	httpClient := &http.Client{}
	if cfg.TimeoutMS > 0 {
		httpClient.Timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	config.HTTPClient = httpClient

	client := openai.NewClientWithConfig(config)
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	return &OpenAIProvider{
		client:     client,
		httpClient: httpClient,
		model:      cfg.Model,
		cfg:        cfg,
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

		resp, err := p.chatStreamCompat(ctx, compatChatRequest{
			Model:       model,
			Messages:    req.Messages,
			Stream:      true,
			Tools:       req.Tools,
			Temperature: req.Temperature,
			TopP:        req.TopP,
			MaxTokens:   req.MaxTokens,
		}, cb)
		// 兼容实现失败时，回退到 SDK 实现（主要用于非 Ollama / 特殊服务端）。
		// Fallback to SDK stream if compat stream fails.
		if err != nil {
			sdkResp, sdkErr := p.chatStream(ctx, buildSDKRequest(model, req), cb)
			if sdkErr == nil {
				return sdkResp, nil
			}
		}
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

// --- OpenAI-compatible streaming (compat) ---

type compatChatRequest struct {
	Model       string         `json:"model"`
	Messages    []chat.Message `json:"messages"`
	Stream      bool           `json:"stream"`
	Tools       []chat.ToolDef `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
}

type compatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Role             string `json:"role,omitempty"`
			Content          string `json:"content,omitempty"`
			Reasoning        string `json:"reasoning,omitempty"`
			ReasoningContent string `json:"reasoning_content,omitempty"`
			ToolCalls        []struct {
				Index    *int   `json:"index,omitempty"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens            int `json:"prompt_tokens"`
		CompletionTokens        int `json:"completion_tokens"`
		TotalTokens             int `json:"total_tokens"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
}

func (p *OpenAIProvider) chatStreamCompat(ctx context.Context, req compatChatRequest, cb *StreamCallbacks) (ChatResponse, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.BaseURL), "/")
	if baseURL == "" {
		return ChatResponse{}, fmt.Errorf("base_url is empty")
	}
	if req.Model == "" {
		req.Model = p.CurrentModel()
	}
	if len(req.Tools) > 0 && req.ToolChoice == nil {
		req.ToolChoice = "auto"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(p.cfg.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.cfg.APIKey))
	}

	client := p.httpClient
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return ChatResponse{}, fmt.Errorf("http status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var (
		contentBuilder   strings.Builder
		reasoningBuilder strings.Builder
		toolCallsByIdx   = map[int]*toolCallAccumulator{}
		finishReason     string
		usage            Usage
	)

	// SSE: each line begins with "data: {json}" or "data: [DONE]"
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for long JSON lines.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}

		var chunk compatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Some servers may interleave non-JSON lines; ignore parse errors cautiously.
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				finishReason = strings.TrimSpace(*choice.FinishReason)
			}

			if choice.Delta.Content != "" {
				contentBuilder.WriteString(choice.Delta.Content)
				if cb != nil && cb.OnTextChunk != nil {
					cb.OnTextChunk(choice.Delta.Content)
				}
			}

			reasoningChunk := choice.Delta.ReasoningContent
			if reasoningChunk == "" {
				reasoningChunk = choice.Delta.Reasoning
			}
			if reasoningChunk != "" {
				reasoningBuilder.WriteString(reasoningChunk)
				if cb != nil && cb.OnReasoningChunk != nil {
					cb.OnReasoningChunk(reasoningChunk)
				}
			}

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
					acc.typ = tc.Type
				}
				if tc.Function.Name != "" {
					acc.name += tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.args.WriteString(tc.Function.Arguments)
				}
			}
		}

		if chunk.Usage != nil {
			usage = Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if chunk.Usage.CompletionTokensDetails != nil {
				usage.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// If we already have partial content or tool calls, return what we have.
		if contentBuilder.Len() == 0 && len(toolCallsByIdx) == 0 && reasoningBuilder.Len() == 0 {
			return ChatResponse{}, fmt.Errorf("stream scan: %w", err)
		}
	}

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

func buildSDKRequest(model string, req ChatRequest) openai.ChatCompletionRequest {
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
	return sdkReq
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
