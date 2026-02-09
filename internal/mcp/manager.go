package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"coder/internal/config"
)

type Status string

const (
	StatusConfigured Status = "configured"
	StatusStarting   Status = "starting"
	StatusReady      Status = "ready"
	StatusDegraded   Status = "degraded"
)

type Server struct {
	cfg         config.MCPServerConfig
	status      Status
	lastError   string
	restartLeft int

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

type Manager struct {
	servers map[string]*Server
}

type Snapshot struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Status    Status `json:"status"`
	Error     string `json:"error,omitempty"`
	TimeoutMS int    `json:"timeout_ms"`
}

func NewManager(cfg config.MCPConfig) *Manager {
	m := &Manager{servers: map[string]*Server{}}
	for _, s := range cfg.Servers {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		m.servers[name] = &Server{
			cfg:         s,
			status:      StatusConfigured,
			restartLeft: 3,
		}
	}
	return m
}

func (m *Manager) StartEnabled(ctx context.Context) {
	for _, s := range m.servers {
		if !s.cfg.Enabled {
			continue
		}
		_ = s.Start(ctx)
	}
}

func (m *Manager) Servers() []*Server {
	out := make([]*Server, 0, len(m.servers))
	for _, s := range m.servers {
		out = append(out, s)
	}
	return out
}

func (m *Manager) Snapshots() []Snapshot {
	out := make([]Snapshot, 0, len(m.servers))
	for _, s := range m.servers {
		s.mu.Lock()
		out = append(out, Snapshot{
			Name:      s.cfg.Name,
			Enabled:   s.cfg.Enabled,
			Status:    s.status,
			Error:     s.lastError,
			TimeoutMS: s.cfg.TimeoutMS,
		})
		s.mu.Unlock()
	}
	return out
}

func (m *Manager) ToolNames() []string {
	out := []string{}
	for _, s := range m.servers {
		if !s.cfg.Enabled {
			continue
		}
		out = append(out, "mcp_"+sanitizeName(s.cfg.Name))
	}
	return out
}

func (m *Manager) ServerByTool(toolName string) (*Server, bool) {
	for _, s := range m.servers {
		if toolName == "mcp_"+sanitizeName(s.cfg.Name) {
			return s, true
		}
	}
	return nil, false
}

func (s *Server) ToolName() string {
	return "mcp_" + sanitizeName(s.cfg.Name)
}

func (s *Server) Enabled() bool {
	return s.cfg.Enabled
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.cfg.Enabled {
		s.status = StatusConfigured
		return nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.status = StatusReady
		return nil
	}
	if len(s.cfg.Command) == 0 {
		s.status = StatusDegraded
		s.lastError = "missing command"
		return fmt.Errorf("mcp %s command empty", s.cfg.Name)
	}

	s.status = StatusStarting
	cmd := exec.CommandContext(ctx, s.cfg.Command[0], s.cfg.Command[1:]...)
	if len(s.cfg.Environment) > 0 {
		env := []string{}
		for k, v := range s.cfg.Environment {
			env = append(env, k+"="+v)
		}
		cmd.Env = append(cmd.Env, env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.status = StatusDegraded
		s.lastError = err.Error()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.status = StatusDegraded
		s.lastError = err.Error()
		return err
	}
	if err := cmd.Start(); err != nil {
		s.status = StatusDegraded
		s.lastError = err.Error()
		return err
	}

	s.cmd = cmd
	s.stdin = stdin
	s.stdout = bufio.NewReader(stdout)
	s.status = StatusReady
	s.lastError = ""
	return nil
}

func (s *Server) Call(ctx context.Context, input map[string]any) (string, error) {
	s.mu.Lock()
	if s.status != StatusReady || s.cmd == nil || s.cmd.Process == nil {
		s.mu.Unlock()
		if err := s.Start(ctx); err != nil {
			return "", err
		}
		s.mu.Lock()
	}

	payload := map[string]any{"input": input}
	line, err := json.Marshal(payload)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		s.markErrorLocked(fmt.Errorf("write request: %w", err))
		s.mu.Unlock()
		return "", err
	}

	reader := s.stdout
	timeout := s.cfg.TimeoutMS
	if timeout <= 0 {
		timeout = 5000
	}
	s.mu.Unlock()

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, rerr := reader.ReadString('\n')
		ch <- result{line: strings.TrimSpace(resp), err: rerr}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(time.Duration(timeout) * time.Millisecond):
		s.mu.Lock()
		s.markErrorLocked(fmt.Errorf("timeout after %dms", timeout))
		s.mu.Unlock()
		return "", fmt.Errorf("mcp %s timeout", s.cfg.Name)
	case r := <-ch:
		if r.err != nil {
			s.mu.Lock()
			s.markErrorLocked(fmt.Errorf("read response: %w", r.err))
			s.mu.Unlock()
			return "", r.err
		}
		if r.line == "" {
			return `{\"ok\":true,\"output\":\"\"}`, nil
		}
		return r.line, nil
	}
}

func (s *Server) markErrorLocked(err error) {
	s.lastError = err.Error()
	s.status = StatusDegraded
	if s.restartLeft > 0 {
		s.restartLeft--
		s.stopLocked()
		s.status = StatusConfigured
	}
}

func (s *Server) stopLocked() {
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	s.stdin = nil
	s.stdout = nil
	s.cmd = nil
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}
