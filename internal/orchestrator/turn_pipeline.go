package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"coder/internal/chat"
	"coder/internal/permission"
	"coder/internal/tools"
)

func (o *Orchestrator) handleNoToolCalls(
	ctx context.Context,
	out io.Writer,
	turnEditedCode bool,
	editedPaths []string,
	verifyAttempts *int,
) (bool, error) {
	if turnEditedCode &&
		shouldAutoVerifyEditedPaths(editedPaths) &&
		o.workflow.AutoVerifyAfterEdit &&
		*verifyAttempts < o.workflow.MaxVerifyAttempts &&
		o.isToolAllowed("bash") &&
		o.registry.Has("bash") {
		command := o.pickVerifyCommand()
		if command != "" {
			*verifyAttempts++
			passed, retryable, err := o.runAutoVerify(ctx, command, *verifyAttempts, out)
			if err == nil && !passed {
				if retryable && *verifyAttempts < o.workflow.MaxVerifyAttempts {
					repairHint := fmt.Sprintf("Auto verification command `%s` failed. Please fix the issues, then continue and make verification pass.", command)
					o.appendMessage(chat.Message{Role: "user", Content: repairHint})
					return true, nil
				}
				if !retryable {
					verifyWarn := fmt.Sprintf("Auto verification command `%s` failed due to environment/runtime issues. Continue with best-effort manual validation.", command)
					o.appendMessage(chat.Message{Role: "assistant", Content: verifyWarn})
					_ = o.flushSessionToFile(ctx)
				}
			}
			if err != nil {
				if isContextCancellationErr(ctx, err) {
					return false, contextErrOr(ctx, err)
				}
				verifyWarn := fmt.Sprintf("Auto verification could not complete (%v). Continue with best-effort manual validation.", err)
				o.appendMessage(chat.Message{Role: "assistant", Content: verifyWarn})
				_ = o.flushSessionToFile(ctx)
			}
		}
	}

	o.refreshTodos(ctx)
	return false, nil
}

func (o *Orchestrator) executeToolCalls(
	ctx context.Context,
	out io.Writer,
	undoRecorder *turnUndoRecorder,
	toolCalls []chat.ToolCall,
	turnEditedCode *bool,
	editedPaths *[]string,
) error {
	for _, call := range toolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}
		startSummary := formatToolStart(call.Function.Name, call.Function.Arguments)
		if out != nil {
			renderToolStart(out, startSummary)
		}
		if o.onToolEvent != nil {
			o.onToolEvent(call.Function.Name, startSummary, false)
		}
		if !o.isToolAllowed(call.Function.Name) {
			reason := fmt.Sprintf("tool %s disabled by active agent %s", call.Function.Name, o.activeAgent.Name)
			if out != nil {
				renderToolBlocked(out, reason)
			}
			o.appendToolDenied(call, reason)
			continue
		}

		args := json.RawMessage(call.Function.Arguments)
		decision := permission.Result{Decision: permission.DecisionAllow}
		if o.policy != nil {
			decision = o.policy.Decide(call.Function.Name, args)
		}
		if decision.Decision == permission.DecisionDeny {
			reason := strings.TrimSpace(decision.Reason)
			if reason == "" {
				reason = "blocked by policy"
			}
			if out != nil {
				renderToolBlocked(out, summarizeForLog(reason))
			}
			o.appendToolDenied(call, reason)
			continue
		}

		approvalReq, err := o.registry.ApprovalRequest(call.Function.Name, args)
		if err != nil {
			if out != nil {
				renderToolError(out, summarizeForLog(err.Error()))
			}
			o.appendToolError(call, fmt.Errorf("approval check: %w", err))
			continue
		}
		needsApproval := decision.Decision == permission.DecisionAsk || approvalReq != nil
		if needsApproval {
			reasons := make([]string, 0, 2)
			if decision.Decision == permission.DecisionAsk {
				if r := strings.TrimSpace(decision.Reason); r != "" {
					reasons = append(reasons, r)
				}
			}
			if approvalReq != nil {
				if r := strings.TrimSpace(approvalReq.Reason); r != "" {
					reasons = append(reasons, r)
				}
			}
			approvalReason := joinApprovalReasons(reasons)
			if o.onApproval == nil {
				if out != nil {
					renderToolBlocked(out, "approval callback unavailable")
				}
				o.appendToolDenied(call, "approval callback unavailable")
				continue
			}
			allowed, err := o.onApproval(ctx, tools.ApprovalRequest{
				Tool:    call.Function.Name,
				Reason:  approvalReason,
				RawArgs: string(args),
			})
			if err != nil {
				if isContextCancellationErr(ctx, err) {
					return contextErrOr(ctx, err)
				}
				return fmt.Errorf("approval callback: %w", err)
			}
			if !allowed {
				if err := ctx.Err(); err != nil {
					return err
				}
				if out != nil {
					renderToolBlocked(out, summarizeForLog(approvalReason))
				}
				o.appendToolDenied(call, approvalReason)
				continue
			}
		}

		if call.Function.Name == "write" || call.Function.Name == "edit" || call.Function.Name == "patch" {
			undoRecorder.CaptureFromToolCall(call.Function.Name, args)
		}

		result, err := o.registry.Execute(ctx, call.Function.Name, args)
		if err != nil {
			if isContextCancellationErr(ctx, err) {
				return contextErrOr(ctx, err)
			}
			if out != nil {
				renderToolError(out, summarizeForLog(err.Error()))
			}
			o.appendToolError(call, err)
			continue
		}
		resultSummary := summarizeToolResult(call.Function.Name, result)
		if out != nil {
			renderToolResult(out, resultSummary)
		}
		if o.onToolEvent != nil {
			o.onToolEvent(call.Function.Name, resultSummary, true)
		}
		o.appendMessage(chat.Message{
			Role:       "tool",
			Name:       call.Function.Name,
			ToolCallID: call.ID,
			Content:    result,
		})
		if call.Function.Name == "todoread" || call.Function.Name == "todowrite" {
			if o.onTodoUpdate != nil {
				items := todoItemsFromResult(result)
				if items != nil {
					o.onTodoUpdate(items)
				}
			}
		}
		if call.Function.Name == "write" || call.Function.Name == "edit" || call.Function.Name == "patch" {
			*turnEditedCode = true
			if editedPath := editedPathFromToolCall(call.Function.Name, args); editedPath != "" {
				*editedPaths = append(*editedPaths, editedPath)
			}
		}
	}
	return nil
}
