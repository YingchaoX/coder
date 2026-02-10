package provider

import (
	"context"

	"coder/internal/chat"
)

// ChatRequest 封装一次模型请求
// ChatRequest wraps a single model call
type ChatRequest struct {
	Model       string
	Messages    []chat.Message
	Tools       []chat.ToolDef
	Temperature *float64
	TopP        *float64
	MaxTokens   int
}

// StreamCallbacks 流式响应的回调集
// StreamCallbacks is the callback set for streaming responses
type StreamCallbacks struct {
	OnTextChunk      func(chunk string)
	OnReasoningChunk func(chunk string)
	OnToolCall       func(call chat.ToolCall)
	OnUsage          func(usage Usage)
}

// Usage token 用量统计
// Usage reports token consumption
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	TotalTokens      int
}

// ChatResponse 完整响应
// ChatResponse is the complete response
type ChatResponse struct {
	Content      string
	Reasoning    string
	ToolCalls    []chat.ToolCall
	FinishReason string
	Usage        Usage
}

// ModelInfo 模型基本信息
// ModelInfo describes a model
type ModelInfo struct {
	ID      string
	OwnedBy string
}

// Provider 模型提供方接口，面向未来多 provider 扩展
// Provider is the model backend interface, designed for future multi-provider extensibility
type Provider interface {
	// Chat 发送聊天请求并返回响应（支持流式回调）
	// Chat sends a request and returns a response (supports streaming callbacks)
	Chat(ctx context.Context, req ChatRequest, cb *StreamCallbacks) (ChatResponse, error)

	// ListModels 列出可用模型
	// ListModels lists available models
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// Name 返回 provider 名称
	// Name returns the provider name
	Name() string

	// CurrentModel 返回当前活跃模型
	// CurrentModel returns the current active model
	CurrentModel() string

	// SetModel 切换活跃模型
	// SetModel switches the active model
	SetModel(model string) error
}
