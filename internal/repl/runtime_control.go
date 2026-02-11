package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"coder/internal/bootstrap"
	"coder/internal/tools"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

var errApprovalControllerClosed = errors.New("approval controller closed")

type approvalPrompt struct {
	ctx    context.Context
	req    tools.ApprovalRequest
	opts   bootstrap.ApprovalPromptOptions
	respCh chan approvalResponse
}

type approvalResponse struct {
	decision bootstrap.ApprovalDecision
	err      error
}

type runtimeController struct {
	stdinFd int
	out     io.Writer
	cancel  context.CancelFunc
	oldTerm *term.State

	stopCh    chan struct{}
	doneCh    chan struct{}
	promptReq chan approvalPrompt

	cancelledByESC atomic.Bool
	interrupted    atomic.Bool

	closeOnce sync.Once
	closeErr  error
}

func newRuntimeController(stdinFd int, stdin *os.File, out io.Writer, cancel context.CancelFunc) (*runtimeController, error) {
	if stdin == nil {
		return nil, fmt.Errorf("stdin is nil")
	}
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		return nil, fmt.Errorf("enable runtime raw mode: %w", err)
	}
	c := &runtimeController{
		stdinFd:   stdinFd,
		out:       out,
		cancel:    cancel,
		oldTerm:   oldState,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		promptReq: make(chan approvalPrompt),
	}
	go c.loop()
	return c, nil
}

func (c *runtimeController) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		close(c.stopCh)
		<-c.doneCh
		if c.oldTerm != nil {
			c.closeErr = term.Restore(c.stdinFd, c.oldTerm)
		}
	})
	return c.closeErr
}

func (c *runtimeController) CancelledByESC() bool {
	if c == nil {
		return false
	}
	return c.cancelledByESC.Load()
}

func (c *runtimeController) Interrupted() bool {
	if c == nil {
		return false
	}
	return c.interrupted.Load()
}

func (c *runtimeController) PromptApproval(ctx context.Context, req tools.ApprovalRequest, opts bootstrap.ApprovalPromptOptions) (bootstrap.ApprovalDecision, error) {
	if c == nil {
		return bootstrap.ApprovalDecisionDeny, errApprovalControllerClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	prompt := approvalPrompt{
		ctx:    ctx,
		req:    req,
		opts:   opts,
		respCh: make(chan approvalResponse, 1),
	}
	select {
	case <-c.stopCh:
		return bootstrap.ApprovalDecisionDeny, errApprovalControllerClosed
	case <-ctx.Done():
		return bootstrap.ApprovalDecisionDeny, ctx.Err()
	case c.promptReq <- prompt:
	}
	select {
	case <-c.stopCh:
		return bootstrap.ApprovalDecisionDeny, errApprovalControllerClosed
	case <-ctx.Done():
		return bootstrap.ApprovalDecisionDeny, ctx.Err()
	case resp := <-prompt.respCh:
		return resp.decision, resp.err
	}
}

func (c *runtimeController) loop() {
	defer close(c.doneCh)

	var pending *approvalPrompt
	var lineInput strings.Builder

	for {
		if pending != nil {
			select {
			case <-pending.ctx.Done():
				c.respondApproval(pending, bootstrap.ApprovalDecisionDeny, pending.ctx.Err())
				pending = nil
				lineInput.Reset()
				continue
			default:
			}
		}

		select {
		case <-c.stopCh:
			if pending != nil {
				c.respondApproval(pending, bootstrap.ApprovalDecisionDeny, errApprovalControllerClosed)
			}
			return
		case req := <-c.promptReq:
			if pending != nil {
				c.respondApproval(&req, bootstrap.ApprovalDecisionDeny, errors.New("another approval is in progress"))
				continue
			}
			pending = &req
			lineInput.Reset()
			c.printApprovalPrompt(req.req, req.opts)
			continue
		default:
		}

		b, ok := c.readByteWithTimeout(80 * time.Millisecond)
		if !ok {
			continue
		}

		if pending == nil {
			c.handleRuntimeKey(b)
			continue
		}
		done := c.handleApprovalKey(pending, &lineInput, b)
		if done {
			pending = nil
			lineInput.Reset()
		}
	}
}

func (c *runtimeController) readByteWithTimeout(timeout time.Duration) (byte, bool) {
	ms := int(timeout / time.Millisecond)
	if ms <= 0 {
		ms = 1
	}
	fds := []unix.PollFd{{Fd: int32(c.stdinFd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, ms)
	if err != nil {
		if errors.Is(err, unix.EINTR) {
			return 0, false
		}
		return 0, false
	}
	if n <= 0 {
		return 0, false
	}
	if fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) == 0 {
		return 0, false
	}
	var one [1]byte
	nr, err := unix.Read(c.stdinFd, one[:])
	if err != nil {
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			return 0, false
		}
		return 0, false
	}
	if nr != 1 {
		return 0, false
	}
	return one[0], true
}

func (c *runtimeController) handleRuntimeKey(b byte) {
	switch b {
	case 0x03: // Ctrl+C
		c.interrupted.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
	case 0x1b: // Esc
		c.cancelledByESC.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
	}
}

func (c *runtimeController) handleApprovalKey(p *approvalPrompt, lineInput *strings.Builder, b byte) bool {
	switch b {
	case 0x03: // Ctrl+C
		c.interrupted.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
		c.respondApproval(p, bootstrap.ApprovalDecisionDeny, context.Canceled)
		return true
	case 0x1b: // Esc -> cancel whole run
		c.cancelledByESC.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
		_, _ = fmt.Fprint(c.out, "\r\n")
		c.respondApproval(p, bootstrap.ApprovalDecisionDeny, context.Canceled)
		return true
	case '\r', '\n':
		input := strings.TrimSpace(strings.ToLower(lineInput.String()))
		decision, ok := parseApprovalDecision(input, p.opts.AllowAlways)
		if !ok {
			_, _ = fmt.Fprint(c.out, "\r\n输入无效，请输入 y / n")
			if p.opts.AllowAlways {
				_, _ = fmt.Fprint(c.out, " / always")
			}
			_, _ = fmt.Fprint(c.out, "（或 Esc 取消）：")
			lineInput.Reset()
			return false
		}
		_, _ = fmt.Fprint(c.out, "\r\n")
		c.respondApproval(p, decision, nil)
		return true
	case 0x7f, 0x08: // Backspace
		s := lineInput.String()
		if s == "" {
			return false
		}
		next, width := deleteLastRuneAndWidth(s)
		lineInput.Reset()
		lineInput.WriteString(next)
		for i := 0; i < width; i++ {
			_, _ = c.out.Write([]byte{'\b', ' ', '\b'})
		}
		return false
	default:
		// Accept printable ASCII for approval line editing.
		if b < 0x20 || b > 0x7e {
			return false
		}
		lineInput.WriteByte(b)
		_, _ = c.out.Write([]byte{b})
		return false
	}
}

func parseApprovalDecision(input string, allowAlways bool) (bootstrap.ApprovalDecision, bool) {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "", "n", "no":
		return bootstrap.ApprovalDecisionDeny, true
	case "y", "yes":
		return bootstrap.ApprovalDecisionAllowOnce, true
	case "a", "always":
		if allowAlways {
			return bootstrap.ApprovalDecisionAllowAlways, true
		}
		return bootstrap.ApprovalDecisionDeny, false
	default:
		return bootstrap.ApprovalDecisionDeny, false
	}
}

func (c *runtimeController) respondApproval(p *approvalPrompt, decision bootstrap.ApprovalDecision, err error) {
	if p == nil {
		return
	}
	select {
	case p.respCh <- approvalResponse{decision: decision, err: err}:
	default:
	}
}

func (c *runtimeController) printApprovalPrompt(req tools.ApprovalRequest, opts bootstrap.ApprovalPromptOptions) {
	if c == nil || c.out == nil {
		return
	}
	_, _ = fmt.Fprintln(c.out)
	_, _ = fmt.Fprintf(c.out, "[approval required] tool=%s reason=%s\r\n", req.Tool, req.Reason)
	if strings.EqualFold(strings.TrimSpace(req.Tool), "bash") && strings.TrimSpace(opts.BashCommand) != "" {
		_, _ = fmt.Fprintf(c.out, "[command] %s\r\n", strings.TrimSpace(opts.BashCommand))
	}
	if opts.AllowAlways {
		_, _ = fmt.Fprint(c.out, "允许执行？(y/N/always, Esc=cancel): ")
		return
	}
	_, _ = fmt.Fprint(c.out, "允许执行？(y/N, Esc=cancel): ")
}
