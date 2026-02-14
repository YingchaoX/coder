package lsp

import (
	"encoding/json"
	"time"
)

// DefaultTimeout is the default timeout for LSP requests
// DefaultTimeout 是 LSP 请求的默认超时
const DefaultTimeout = 10 * time.Second

// LSP Protocol definitions
// Based on Language Server Protocol Specification

// InitializeRequest represents the initialize request
// InitializeRequest 表示初始化请求
type InitializeRequest struct {
	ProcessID             int                `json:"processId,omitempty"`
	ClientInfo            *ClientInfo        `json:"clientInfo,omitempty"`
	RootPath              string             `json:"rootPath,omitempty"`
	RootURI               string             `json:"rootUri"`
	InitializationOptions interface{}        `json:"initializationOptions,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
}

// ClientInfo represents information about the client
// ClientInfo 表示客户端信息
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ClientCapabilities represents the capabilities of the client
// ClientCapabilities 表示客户端的能力
type ClientCapabilities struct {
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty"`
}

// TextDocumentClientCapabilities represents text document specific client capabilities
// TextDocumentClientCapabilities 表示文本文档特定的客户端能力
type TextDocumentClientCapabilities struct {
	Synchronization    interface{} `json:"synchronization,omitempty"`
	Completion         interface{} `json:"completion,omitempty"`
	Hover              interface{} `json:"hover,omitempty"`
	Definition         interface{} `json:"definition,omitempty"`
	DocumentSymbol     interface{} `json:"documentSymbol,omitempty"`
	CodeAction         interface{} `json:"codeAction,omitempty"`
	Formatting         interface{} `json:"formatting,omitempty"`
	Rename             interface{} `json:"rename,omitempty"`
	PublishDiagnostics interface{} `json:"publishDiagnostics,omitempty"`
}

// InitializeResponse represents the initialize response
// InitializeResponse 表示初始化响应
type InitializeResponse struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

// ServerInfo represents information about the server
// ServerInfo 表示服务器信息
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ServerCapabilities represents the capabilities of the server
// ServerCapabilities 表示服务器的能力
type ServerCapabilities struct {
	TextDocumentSync       interface{} `json:"textDocumentSync,omitempty"`
	CompletionProvider     interface{} `json:"completionProvider,omitempty"`
	HoverProvider          bool        `json:"hoverProvider,omitempty"`
	DefinitionProvider     bool        `json:"definitionProvider,omitempty"`
	DocumentSymbolProvider bool        `json:"documentSymbolProvider,omitempty"`
	CodeActionProvider     interface{} `json:"codeActionProvider,omitempty"`
	ExecuteCommandProvider interface{} `json:"executeCommandProvider,omitempty"`
}

// TextDocumentIdentifier identifies a text document
// TextDocumentIdentifier 标识一个文本文档
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem represents an open text document
// TextDocumentItem 表示一个打开的文本文件
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// VersionedTextDocumentIdentifier identifies a versioned text document
// VersionedTextDocumentIdentifier 标识一个带版本的文本文件
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentContentChangeEvent represents a change to a document
// TextDocumentContentChangeEvent 表示文档的变更
type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength int    `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

// Position represents a position in a text document
// Position 表示文本文件中的位置
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a range in a text document
// Range 表示文本文件中的范围
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location inside a resource
// Location 表示资源内的位置
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic represents a diagnostic, such as a compiler error or warning
// Diagnostic 表示诊断信息，如编译器错误或警告
type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           *DiagnosticSeverity            `json:"severity,omitempty"`
	Code               interface{}                    `json:"code,omitempty"`
	CodeDescription    *CodeDescription               `json:"codeDescription,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

// DiagnosticSeverity represents the severity of a diagnostic
// DiagnosticSeverity 表示诊断的严重程度
type DiagnosticSeverity int

const (
	Error       DiagnosticSeverity = 1
	Warning     DiagnosticSeverity = 2
	Information DiagnosticSeverity = 3
	Hint        DiagnosticSeverity = 4
)

// CodeDescription provides additional metadata about a diagnostic code
// CodeDescription 提供有关诊断代码的附加元数据
type CodeDescription struct {
	Href string `json:"href"`
}

// DiagnosticRelatedInformation represents related diagnostic information
// DiagnosticRelatedInformation 表示相关的诊断信息
type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// PublishDiagnosticsParams represents the parameters of a publish diagnostics notification
// PublishDiagnosticsParams 表示发布诊断通知的参数
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Hover represents the result of a hover request
// Hover 表示悬停请求的结果
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent represents a string value with a markup kind
// MarkupContent 表示带有标记类型的字符串值
type MarkupContent struct {
	Kind  string `json:"kind"` // plaintext or markdown
	Value string `json:"value"`
}

// DocumentSymbol represents a symbol in a document
// DocumentSymbol 表示文档中的符号
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Tags           []SymbolTag      `json:"tags,omitempty"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolKind represents the kind of a symbol
// SymbolKind 表示符号的类型
type SymbolKind int

const (
	SymbolFile          SymbolKind = 1
	SymbolModule        SymbolKind = 2
	SymbolNamespace     SymbolKind = 3
	SymbolPackage       SymbolKind = 4
	SymbolClass         SymbolKind = 5
	SymbolMethod        SymbolKind = 6
	SymbolProperty      SymbolKind = 7
	SymbolField         SymbolKind = 8
	SymbolConstructor   SymbolKind = 9
	SymbolEnum          SymbolKind = 10
	SymbolInterface     SymbolKind = 11
	SymbolFunction      SymbolKind = 12
	SymbolVariable      SymbolKind = 13
	SymbolConstant      SymbolKind = 14
	SymbolString        SymbolKind = 15
	SymbolNumber        SymbolKind = 16
	SymbolBoolean       SymbolKind = 17
	SymbolArray         SymbolKind = 18
	SymbolObject        SymbolKind = 19
	SymbolKey           SymbolKind = 20
	SymbolNull          SymbolKind = 21
	SymbolEnumMember    SymbolKind = 22
	SymbolStruct        SymbolKind = 23
	SymbolEvent         SymbolKind = 24
	SymbolOperator      SymbolKind = 25
	SymbolTypeParameter SymbolKind = 26
)

// SymbolTag represents tags associated with a symbol
// SymbolTag 表示与符号关联的标签
type SymbolTag int

const (
	SymbolDeprecated SymbolTag = 1
)

// Method names for LSP
// LSP 方法名称
const (
	MethodInitialize                     = "initialize"
	MethodInitialized                    = "initialized"
	MethodShutdown                       = "shutdown"
	MethodExit                           = "exit"
	MethodTextDocumentDidOpen            = "textDocument/didOpen"
	MethodTextDocumentDidChange          = "textDocument/didChange"
	MethodTextDocumentDidClose           = "textDocument/didClose"
	MethodTextDocumentHover              = "textDocument/hover"
	MethodTextDocumentDefinition         = "textDocument/definition"
	MethodTextDocumentDocumentSymbol     = "textDocument/documentSymbol"
	MethodTextDocumentPublishDiagnostics = "textDocument/publishDiagnostics"
)

// JSONRPC related constants
// JSONRPC 相关常量
const (
	JSONRPCVersion = "2.0"
)

// Request represents a JSON-RPC request
// Request 表示 JSON-RPC 请求
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response
// Response 表示 JSON-RPC 响应
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC error
// ResponseError 表示 JSON-RPC 错误
type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error codes for JSON-RPC
// JSON-RPC 错误代码
const (
	ParseError           = -32700
	InvalidRequest       = -32600
	MethodNotFound       = -32601
	InvalidParams        = -32602
	InternalError        = -32603
	ServerErrorStart     = -32099
	ServerErrorEnd       = -32000
	ServerNotInitialized = -32002
	UnknownErrorCode     = -32001
)

// DocumentFilter represents a document filter
// DocumentFilter 表示文档过滤器
type DocumentFilter struct {
	Language string `json:"language,omitempty"`
	Scheme   string `json:"scheme,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
}
