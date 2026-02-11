package bootstrap

import (
	"context"

	"coder/internal/tools"
)

type ApprovalDecision int

const (
	ApprovalDecisionDeny ApprovalDecision = iota
	ApprovalDecisionAllowOnce
	ApprovalDecisionAllowAlways
)

type ApprovalPromptOptions struct {
	AllowAlways bool
	BashCommand string
}

type ApprovalPrompter interface {
	PromptApproval(ctx context.Context, req tools.ApprovalRequest, opts ApprovalPromptOptions) (ApprovalDecision, error)
}

type approvalPrompterContextKey struct{}

func WithApprovalPrompter(ctx context.Context, p ApprovalPrompter) context.Context {
	if ctx == nil || p == nil {
		return ctx
	}
	return context.WithValue(ctx, approvalPrompterContextKey{}, p)
}

func approvalPrompterFromContext(ctx context.Context) (ApprovalPrompter, bool) {
	if ctx == nil {
		return nil, false
	}
	v := ctx.Value(approvalPrompterContextKey{})
	if v == nil {
		return nil, false
	}
	p, ok := v.(ApprovalPrompter)
	return p, ok
}
