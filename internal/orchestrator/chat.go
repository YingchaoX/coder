package orchestrator

import (
	"context"

	"coder/internal/chat"
	"coder/internal/provider"
)

func (o *Orchestrator) chatWithRetry(
	ctx context.Context,
	messages []chat.Message,
	definitions []chat.ToolDef,
	onTextChunk TextChunkFunc,
	onReasoningChunk TextChunkFunc,
) (provider.ChatResponse, error) {
	model := ""
	if o.provider != nil {
		model = o.provider.CurrentModel()
	}
	req := provider.ChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    definitions,
	}
	var cb *provider.StreamCallbacks
	if onTextChunk != nil || onReasoningChunk != nil {
		cb = &provider.StreamCallbacks{
			OnTextChunk: onTextChunk,
			OnReasoningChunk: func(chunk string) {
				if onReasoningChunk != nil {
					onReasoningChunk(chunk)
				}
			},
		}
	}
	resp, err := o.provider.Chat(ctx, req, cb)
	if err != nil {
		return provider.ChatResponse{}, err
	}
	if len(resp.ToolCalls) == 0 {
		if recovered, cleaned := recoverToolCallsFromContent(resp.Content, definitions); len(recovered) > 0 {
			resp.ToolCalls = recovered
			resp.Content = cleaned
		}
	}
	return resp, nil
}
