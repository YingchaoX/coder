package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"coder/internal/chat"
	"coder/internal/permission"
	"coder/internal/tools"
)

func (o *Orchestrator) handlePolicyCheck(ctx context.Context, call chat.ToolCall, args json.RawMessage, out io.Writer) bool {
	if o.policy == nil {
		return false
	}
	decision := o.policy.Decide(call.Function.Name, args)
	switch decision.Decision {
	case permission.DecisionAllow:
		return false
	case permission.DecisionDeny:
		if out != nil {
			renderToolBlocked(out, summarizeForLog(decision.Reason))
		}
		o.appendToolDenied(call, decision.Reason)
		return true
	case permission.DecisionAsk:
		if o.onApproval == nil {
			reason := "approval callback unavailable"
			if out != nil {
				renderToolBlocked(out, reason)
			}
			o.appendToolDenied(call, reason)
			return true
		}
		allowed, err := o.onApproval(ctx, tools.ApprovalRequest{Tool: call.Function.Name, Reason: decision.Reason, RawArgs: string(args)})
		if err != nil {
			o.appendToolError(call, fmt.Errorf("approval callback: %w", err))
			if out != nil {
				renderToolError(out, summarizeForLog(err.Error()))
			}
			return true
		}
		if !allowed {
			if out != nil {
				renderToolBlocked(out, summarizeForLog(decision.Reason))
			}
			o.appendToolDenied(call, decision.Reason)
			return true
		}
		return false
	default:
		return false
	}
}

func (o *Orchestrator) isToolAllowed(tool string) bool {
	if o.activeAgent.ToolEnabled == nil {
		return true
	}
	enabled, ok := o.activeAgent.ToolEnabled[tool]
	if !ok {
		return true
	}
	return enabled
}
