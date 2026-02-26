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
var errQuestionControllerClosed = errors.New("question controller closed")

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

type questionPrompt struct {
	ctx    context.Context
	req    tools.QuestionRequest
	respCh chan questionResponse
}

type questionResponse struct {
	resp *tools.QuestionResponse
	err  error
}

type runtimeController struct {
	stdinFd int
	out     io.Writer
	cancel  context.CancelFunc
	oldTerm *term.State

	stopCh      chan struct{}
	doneCh      chan struct{}
	promptReq   chan approvalPrompt
	questionReq chan questionPrompt

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
		stdinFd:     stdinFd,
		out:         out,
		cancel:      cancel,
		oldTerm:     oldState,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		promptReq:   make(chan approvalPrompt),
		questionReq: make(chan questionPrompt),
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

func (c *runtimeController) PromptQuestion(ctx context.Context, req tools.QuestionRequest) (*tools.QuestionResponse, error) {
	if c == nil {
		return nil, errQuestionControllerClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	prompt := questionPrompt{
		ctx:    ctx,
		req:    req,
		respCh: make(chan questionResponse, 1),
	}
	select {
	case <-c.stopCh:
		return nil, errQuestionControllerClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.questionReq <- prompt:
	}
	select {
	case <-c.stopCh:
		return nil, errQuestionControllerClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-prompt.respCh:
		return resp.resp, resp.err
	}
}

type pendingInteraction struct {
	approval *approvalPrompt
	question *questionPrompt
	qIndex   int
	qAnswers []string
}

func (c *runtimeController) loop() {
	defer close(c.doneCh)

	var pi pendingInteraction
	var lineInput strings.Builder

	for {
		if pi.approval != nil {
			select {
			case <-pi.approval.ctx.Done():
				c.respondApproval(pi.approval, bootstrap.ApprovalDecisionDeny, pi.approval.ctx.Err())
				pi = pendingInteraction{}
				lineInput.Reset()
				continue
			default:
			}
		}
		if pi.question != nil {
			select {
			case <-pi.question.ctx.Done():
				c.respondQuestion(pi.question, nil, pi.question.ctx.Err())
				pi = pendingInteraction{}
				lineInput.Reset()
				continue
			default:
			}
		}

		if pi.approval == nil && pi.question == nil {
			select {
			case <-c.stopCh:
				return
			case req := <-c.promptReq:
				pi.approval = &req
				lineInput.Reset()
				c.printApprovalPrompt(req.req, req.opts)
				continue
			case req := <-c.questionReq:
				pi.question = &req
				pi.qIndex = 0
				pi.qAnswers = make([]string, len(req.req.Questions))
				lineInput.Reset()
				c.printQuestionPrompt(req.req, 0)
				continue
			default:
			}
		}

		b, ok := c.readByteWithTimeout(80 * time.Millisecond)
		if !ok {
			continue
		}

		if pi.approval == nil && pi.question == nil {
			c.handleRuntimeKey(b)
			continue
		}

		if pi.approval != nil {
			done := c.handleApprovalKey(pi.approval, &lineInput, b)
			if done {
				pi = pendingInteraction{}
				lineInput.Reset()
			}
			continue
		}

		if pi.question != nil {
			done := c.handleQuestionKey(&pi, &lineInput, b)
			if done {
				pi = pendingInteraction{}
				lineInput.Reset()
			}
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

func (c *runtimeController) respondQuestion(p *questionPrompt, resp *tools.QuestionResponse, err error) {
	if p == nil {
		return
	}
	select {
	case p.respCh <- questionResponse{resp: resp, err: err}:
	default:
	}
}

func (c *runtimeController) printQuestionPrompt(req tools.QuestionRequest, index int) {
	if c == nil || c.out == nil || index >= len(req.Questions) {
		return
	}
	q := req.Questions[index]
	total := len(req.Questions)

	_, _ = fmt.Fprint(c.out, "\r\n")
	if total > 1 {
		_, _ = fmt.Fprintf(c.out, "[Question %d/%d] %s\r\n", index+1, total, q.Question)
	} else {
		_, _ = fmt.Fprintf(c.out, "[Question] %s\r\n", q.Question)
	}
	for i, opt := range q.Options {
		label := opt.Label
		if i == 0 {
			label += " (Recommended)"
		}
		if strings.TrimSpace(opt.Description) != "" {
			_, _ = fmt.Fprintf(c.out, "  %d. %s — %s\r\n", i+1, label, opt.Description)
		} else {
			_, _ = fmt.Fprintf(c.out, "  %d. %s\r\n", i+1, label)
		}
	}
	_, _ = fmt.Fprint(c.out, "\r\n> ")
}

func (c *runtimeController) handleQuestionKey(pi *pendingInteraction, lineInput *strings.Builder, b byte) bool {
	switch b {
	case 0x03: // Ctrl+C
		c.interrupted.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
		c.respondQuestion(pi.question, nil, context.Canceled)
		return true
	case 0x1b: // Esc -> cancel all questions
		_, _ = fmt.Fprint(c.out, "\r\n")
		c.respondQuestion(pi.question, &tools.QuestionResponse{Cancelled: true}, nil)
		return true
	case '\r', '\n':
		input := strings.TrimSpace(lineInput.String())
		_, _ = fmt.Fprint(c.out, "\r\n")

		if input == "" {
			_, _ = fmt.Fprint(c.out, "> ")
			lineInput.Reset()
			return false
		}

		q := pi.question.req.Questions[pi.qIndex]
		answer := tools.ResolveQuestionAnswer(input, q.Options)
		pi.qAnswers[pi.qIndex] = answer
		pi.qIndex++
		lineInput.Reset()

		if pi.qIndex >= len(pi.question.req.Questions) {
			c.respondQuestion(pi.question, &tools.QuestionResponse{Answers: pi.qAnswers}, nil)
			return true
		}
		c.printQuestionPrompt(pi.question.req, pi.qIndex)
		return false
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
		if b < 0x20 || b > 0x7e {
			return false
		}
		lineInput.WriteByte(b)
		_, _ = c.out.Write([]byte{b})
		return false
	}
}

