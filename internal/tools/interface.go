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

type Tool interface {
	Name() string
	Definition() chat.ToolDef
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

type ApprovalAware interface {
	ApprovalRequest(args json.RawMessage) (*ApprovalRequest, error)
}
