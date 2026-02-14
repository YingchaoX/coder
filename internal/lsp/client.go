package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Client represents an LSP client connection
// Client 表示一个 LSP 客户端连接
type Client struct {
	command   string
	args      []string
	workspace string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	reader *bufio.Reader
	mu     sync.Mutex

	// Request ID counter
	idCounter int64

	// Pending requests
	pending   map[interface{}]chan *Response
	pendingMu sync.RWMutex

	// Server capabilities
	capabilities ServerCapabilities
	initialized  bool

	// Language ID (e.g., "python", "sh")
	languageID string
}

// NewClient creates a new LSP client
// NewClient 创建一个新的 LSP 客户端
func NewClient(languageID, command string, args []string, workspace string) *Client {
	return &Client{
		languageID: languageID,
		command:    command,
		args:       args,
		workspace:  workspace,
		pending:    make(map[interface{}]chan *Response),
	}
}

// Start starts the LSP server and initializes the connection
// Start 启动 LSP 服务器并初始化连接
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil {
		return fmt.Errorf("LSP client already started")
	}

	// Start the LSP server process
	c.cmd = exec.CommandContext(ctx, c.command, c.args...)
	c.cmd.Dir = c.workspace

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	c.stdin = stdin

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	c.stdout = stdout

	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start LSP server %s: %w", c.command, err)
	}

	// Discard stderr to avoid blocking
	go io.Copy(io.Discard, stderr)

	// Start reading responses
	c.reader = bufio.NewReader(c.stdout)
	go c.readLoop()

	// Initialize the connection
	if err := c.initialize(ctx); err != nil {
		c.Stop()
		return fmt.Errorf("failed to initialize LSP connection: %w", err)
	}

	return nil
}

// Stop stops the LSP server
// Stop 停止 LSP 服务器
func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil {
		return nil
	}

	// Send shutdown request
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	if _, err := c.request(ctx, MethodShutdown, nil); err != nil {
		// Log error but continue shutdown
		_ = err
	}

	// Close stdin to signal exit
	if c.stdin != nil {
		c.stdin.Close()
	}

	// Wait for process to exit
	if err := c.cmd.Wait(); err != nil {
		// Process might have already exited
		_ = err
	}

	c.cmd = nil
	c.initialized = false

	return nil
}

// IsInitialized returns whether the client is initialized
// IsInitialized 返回客户端是否已初始化
func (c *Client) IsInitialized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

// initialize sends the initialize request
// initialize 发送初始化请求
func (c *Client) initialize(ctx context.Context) error {
	workspaceURI := filepath.ToSlash(c.workspace)
	if !filepath.IsAbs(workspaceURI) {
		workspaceURI = "/" + workspaceURI
	}
	workspaceURI = "file://" + workspaceURI

	req := InitializeRequest{
		ProcessID: int(exec.Command("echo", "$$").ProcessState.Pid()),
		RootURI:   workspaceURI,
		ClientInfo: &ClientInfo{
			Name:    "coder",
			Version: "1.0.0",
		},
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Hover:              struct{}{},
				Definition:         struct{}{},
				DocumentSymbol:     struct{}{},
				PublishDiagnostics: struct{}{},
			},
		},
	}

	resp, err := c.request(ctx, MethodInitialize, req)
	if err != nil {
		return err
	}

	var initResp InitializeResponse
	if err := json.Unmarshal(resp.Result, &initResp); err != nil {
		return fmt.Errorf("failed to unmarshal initialize response: %w", err)
	}

	c.capabilities = initResp.Capabilities
	c.initialized = true

	// Send initialized notification
	if err := c.notify(MethodInitialized, struct{}{}); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}

	return nil
}

// request sends a request and waits for response
// request 发送请求并等待响应
func (c *Client) request(ctx context.Context, method string, params interface{}) (*Response, error) {
	id := atomic.AddInt64(&c.idCounter, 1)

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}

	respChan := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = respChan
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	if err := c.send(req); err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("LSP error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// notify sends a notification (no response expected)
// notify 发送通知（不期望响应）
func (c *Client) notify(method string, params interface{}) error {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	req := Request{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  paramsJSON,
	}

	return c.send(req)
}

// send sends a JSON-RPC message
// send 发送 JSON-RPC 消息
func (c *Client) send(req Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stdin == nil {
		return fmt.Errorf("LSP client not started")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// LSP uses Content-Length header format
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// readLoop reads responses from the server
// readLoop 从服务器读取响应
func (c *Client) readLoop() {
	for {
		resp, err := c.readResponse()
		if err != nil {
			// Connection closed or error
			break
		}

		c.pendingMu.RLock()
		ch, ok := c.pending[resp.ID]
		c.pendingMu.RUnlock()

		if ok {
			ch <- resp
		}
		// If no pending request, it might be a server notification
	}
}

// readResponse reads a single JSON-RPC response
// readResponse 读取单个 JSON-RPC 响应
func (c *Client) readResponse() (*Response, error) {
	// Read headers
	var contentLength int
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = line[:len(line)-2] // Remove \r\n
		if line == "" {
			break
		}
		if len(line) > 16 && line[:16] == "Content-Length: " {
			fmt.Sscanf(line[16:], "%d", &contentLength)
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("no Content-Length header")
	}

	// Read body
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, err
	}

	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// DidOpen sends textDocument/didOpen notification
// DidOpen 发送 textDocument/didOpen 通知
func (c *Client) DidOpen(uri, languageID, text string, version int) error {
	params := struct {
		TextDocument TextDocumentItem `json:"textDocument"`
	}{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	}
	return c.notify(MethodTextDocumentDidOpen, params)
}

// DidChange sends textDocument/didChange notification
// DidChange 发送 textDocument/didChange 通知
func (c *Client) DidChange(uri string, version int, changes []TextDocumentContentChangeEvent) error {
	params := struct {
		TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
		ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
	}{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: changes,
	}
	return c.notify(MethodTextDocumentDidChange, params)
}

// Hover sends textDocument/hover request
// Hover 发送 textDocument/hover 请求
func (c *Client) Hover(ctx context.Context, uri string, line, character int) (*Hover, error) {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}

	resp, err := c.request(ctx, MethodTextDocumentHover, params)
	if err != nil {
		return nil, err
	}

	var hover Hover
	if err := json.Unmarshal(resp.Result, &hover); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hover response: %w", err)
	}

	return &hover, nil
}

// Definition sends textDocument/definition request
// Definition 发送 textDocument/definition 请求
func (c *Client) Definition(ctx context.Context, uri string, line, character int) ([]Location, error) {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}

	resp, err := c.request(ctx, MethodTextDocumentDefinition, params)
	if err != nil {
		return nil, err
	}

	// Definition can return a single Location or an array
	var locations []Location
	if err := json.Unmarshal(resp.Result, &locations); err != nil {
		// Try single location
		var loc Location
		if err := json.Unmarshal(resp.Result, &loc); err != nil {
			return nil, fmt.Errorf("failed to unmarshal definition response: %w", err)
		}
		locations = []Location{loc}
	}

	return locations, nil
}

// DocumentSymbol sends textDocument/documentSymbol request
// DocumentSymbol 发送 textDocument/documentSymbol 请求
func (c *Client) DocumentSymbol(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	resp, err := c.request(ctx, MethodTextDocumentDocumentSymbol, params)
	if err != nil {
		return nil, err
	}

	var symbols []DocumentSymbol
	if err := json.Unmarshal(resp.Result, &symbols); err != nil {
		return nil, fmt.Errorf("failed to unmarshal documentSymbol response: %w", err)
	}

	return symbols, nil
}
