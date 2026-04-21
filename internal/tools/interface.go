package tools

import (
	"context"
	"encoding/json"

	"coder/internal/chat"
)

type ApprovalRequest struct {
	Tool    string
	Reason  string
	RawArgs string
}

type CommandStreamer interface {
	OnCommandStart(tool, command string)
	OnCommandChunk(tool, stream, chunk string)
	OnCommandFinish(tool string, exitCode int, durationMS int64)
}

type commandStreamerContextKey struct{}

type Tool interface {
	Name() string
	Definition() chat.ToolDef
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

type ApprovalAware interface {
	ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error)
}

func WithCommandStreamer(ctx context.Context, s CommandStreamer) context.Context {
	if ctx == nil || s == nil {
		return ctx
	}
	return context.WithValue(ctx, commandStreamerContextKey{}, s)
}

func CommandStreamerFromContext(ctx context.Context) (CommandStreamer, bool) {
	if ctx == nil {
		return nil, false
	}
	v := ctx.Value(commandStreamerContextKey{})
	if v == nil {
		return nil, false
	}
	s, ok := v.(CommandStreamer)
	return s, ok
}
