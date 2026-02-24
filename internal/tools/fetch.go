package tools

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"coder/internal/chat"
	"coder/internal/security"
	"golang.org/x/net/html"
)

type FetchTool struct {
	ws  *security.Workspace
	cfg FetchConfig
}

type FetchConfig struct {
	TimeoutSec     int
	MaxTextSizeKB  int
	MaxImageSizeMB int
	SkipTLSVerify  bool
	DefaultHeaders map[string]string
}

// maxJSONTextBytes 是非图片响应在 JSON 结果中可占用的最大文本字节数上限，
// 进一步防止工具返回体本身过大把整个对话请求体撑爆。
const maxJSONTextBytes = 512 * 1024

type FetchArgs struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	TimeoutSec int               `json:"timeout_sec"`
	MaxSizeKB  int               `json:"max_size_kb"`
	Format     string            `json:"format"`
	Auth       *FetchAuth        `json:"auth"`
}

type FetchAuth struct {
	Type     string `json:"type"`     // "basic", "bearer", "cookie"
	Username string `json:"username"` // for basic auth
	Password string `json:"password"` // for basic auth
	Token    string `json:"token"`    // for bearer auth
}

type FetchResult struct {
	URL         string `json:"url"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type"`
	IsImage     bool   `json:"is_image"`
	Content     string `json:"content"`
	SizeBytes   int    `json:"size_bytes"`
	Error       string `json:"error,omitempty"`
}

func NewFetchTool(ws *security.Workspace, cfg FetchConfig) *FetchTool {
	return &FetchTool{ws: ws, cfg: cfg}
}

func (t *FetchTool) Name() string {
	return "fetch"
}

func (t *FetchTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Fetch content from HTTP/HTTPS URL. Returns text/HTML as markdown or plain text, images as base64, and only metadata (no raw bytes) for PDFs and other binary content.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Target URL to fetch (must be http or https)",
					},
					"method": map[string]any{
						"type":        "string",
						"description": "HTTP method (GET, POST, PUT, DELETE, etc.)",
						"default":     "GET",
					},
					"headers": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
						"description":          "Custom headers to include in the request",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "Request body for POST/PUT requests",
					},
					"format": map[string]any{
						"type": "string",
						"enum": []string{
							"text",
							"markdown",
							"html",
						},
						"description": "Preferred format for non-image content. For HTML responses, 'markdown' converts to Markdown, 'text' extracts plain text, and 'html' returns raw HTML. Defaults to 'markdown' for HTML responses and 'text' for others.",
					},
					"timeout_sec": map[string]any{
						"type":        "integer",
						"description": "Request timeout in seconds",
						"default":     30,
					},
					"max_size_kb": map[string]any{
						"type":        "integer",
						"description": "Maximum response size in KB for non-image content",
						"default":     5120, // 5MB
					},
					"auth": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type": map[string]any{
								"type": "string",
								"enum": []string{"basic", "bearer", "cookie"},
							},
							"username": map[string]any{"type": "string"},
							"password": map[string]any{"type": "string"},
							"token":    map[string]any{"type": "string"},
						},
						"required": []string{"type"},
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (t *FetchTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in FetchArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("fetch args: %w", err)
	}

	// Validate URL
	parsedURL, err := url.Parse(in.URL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("URL must use http or https scheme")
	}

	// Set defaults
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = "GET"
	}

	timeout := in.TimeoutSec
	if timeout <= 0 {
		timeout = t.cfg.TimeoutSec
		if timeout <= 0 {
			timeout = 30
		}
	}

	maxSizeKB := in.MaxSizeKB
	if maxSizeKB <= 0 {
		maxSizeKB = t.cfg.MaxTextSizeKB
		if maxSizeKB <= 0 {
			maxSizeKB = 5 * 1024 // 5MB default for non-image content
		}
	}

	// Create HTTP client
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: t.cfg.SkipTLSVerify},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(timeout) * time.Second,
	}

	// Create request
	var bodyReader io.Reader
	if in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	}

	req, err := http.NewRequest(method, in.URL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	for k, v := range t.cfg.DefaultHeaders {
		req.Header.Set(k, v)
	}

	// Set custom headers
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	// Set auth headers
	if in.Auth != nil {
		switch strings.ToLower(in.Auth.Type) {
		case "basic":
			if in.Auth.Username != "" && in.Auth.Password != "" {
				auth := in.Auth.Username + ":" + in.Auth.Password
				req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
			}
		case "bearer":
			if in.Auth.Token != "" {
				req.Header.Set("Authorization", "Bearer "+in.Auth.Token)
			}
		case "cookie":
			if in.Auth.Token != "" {
				req.Header.Set("Cookie", in.Auth.Token)
			}
		}
	}

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	isImage := strings.HasPrefix(mediaType, "image/")

	// Determine max size based on content type
	var maxSizeBytes int
	if isImage {
		maxSizeBytes = t.cfg.MaxImageSizeMB * 1024 * 1024 // Convert MB to bytes
	} else {
		maxSizeBytes = maxSizeKB * 1024 // Convert KB to bytes for non-image content
	}

	// Limit response size
	limitReader := io.LimitReader(resp.Body, int64(maxSizeBytes)+1)
	responseData, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check if response was truncated
	if len(responseData) > maxSizeBytes {
		return "", fmt.Errorf("response exceeds maximum size of %d bytes", maxSizeBytes)
	}

	sizeBytes := len(responseData)

	var content string
	if isImage {
		// For images, encode as base64
		content = base64.StdEncoding.EncodeToString(responseData)
	} else if strings.EqualFold(mediaType, "application/pdf") {
		// For PDFs, do not inline raw bytes into the context.
		// Only return a short metadata description and let pdf_parser handle text extraction.
		content = fmt.Sprintf("PDF content omitted. type=%s size=%d bytes. Use pdf_parser tool to extract text.", mediaType, sizeBytes)
	} else if !isTextLikeMediaType(mediaType) {
		// For other binary-like content, avoid inlining bytes into the chat context.
		content = fmt.Sprintf("Binary content omitted. type=%s size=%d bytes", mediaType, sizeBytes)
	} else {
		// For non-image content, optionally transform based on format and content type
		rawText := string(responseData)

		format := strings.ToLower(strings.TrimSpace(in.Format))
		if format == "" {
			if isHTMLMediaType(mediaType) {
				format = "markdown"
			} else {
				format = "text"
			}
		}

		switch format {
		case "html":
			content = rawText
		case "markdown":
			if isHTMLMediaType(mediaType) {
				content = convertHTMLToMarkdown(rawText)
			} else {
				content = rawText
			}
		case "text":
			if isHTMLMediaType(mediaType) {
				content = extractTextFromHTML(rawText)
			} else {
				content = rawText
			}
		default:
			content = rawText
		}

		// Truncate text to a conservative upper bound to keep the JSON result small enough.
		maxTextBytes := maxSizeKB * 1024
		if maxTextBytes <= 0 || maxTextBytes > maxJSONTextBytes {
			maxTextBytes = maxJSONTextBytes
		}
		if len(content) > maxTextBytes {
			content = content[:maxTextBytes]
		}
	}

	result := FetchResult{
		URL:         in.URL,
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		IsImage:     isImage,
		Content:     content,
		SizeBytes:   sizeBytes,
	}

	// Handle 401 Unauthorized - try again with auth if available
	if resp.StatusCode == 401 && in.Auth == nil {
		return "", fmt.Errorf("authentication required (received 401), please provide auth credentials")
	}

	return mustJSON(result), nil
}

func isHTMLMediaType(mediaType string) bool {
	if mediaType == "" {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return strings.HasSuffix(strings.ToLower(mediaType), "+html")
	}
}

// isTextLikeMediaType 判定一个 mediaType 是否“文本型”，用于决定是否尝试将响应体作为文本解码并送入上下文。
func isTextLikeMediaType(mediaType string) bool {
	if strings.TrimSpace(mediaType) == "" {
		// 没有声明类型时，保守按文本处理，由 maxJSONTextBytes 再兜底截断。
		return true
	}
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	if isHTMLMediaType(mt) {
		return true
	}
	switch mt {
	case "application/json",
		"application/xml",
		"application/javascript",
		"application/x-www-form-urlencoded":
		return true
	}
	return false
}

func extractTextFromHTML(htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		// Fallback to raw string if parsing fails
		return htmlStr
	}

	var b strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.ElementNode {
			if isIgnoredHTMLTag(n.Data) {
				skip = true
			}
		}

		if n.Type == html.TextNode && !skip {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(text)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}

	walk(doc, false)
	return b.String()
}

func convertHTMLToMarkdown(htmlStr string) string {
	// Very lightweight HTML -> Markdown conversion built on top of text extraction.
	// For now, we focus on producing readable markdown without heavy dependencies.
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		// Fallback to plain text extraction on parse failure
		return extractTextFromHTML(htmlStr)
	}

	var b strings.Builder

	var walk func(*html.Node, bool, string)
	walk = func(n *html.Node, skip bool, prefix string) {
		if n.Type == html.ElementNode {
			if isIgnoredHTMLTag(n.Data) {
				skip = true
			}
		}

		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "h1":
				prefix = "# "
			case "h2":
				prefix = "## "
			case "h3":
				prefix = "### "
			case "h4":
				prefix = "#### "
			case "h5":
				prefix = "##### "
			case "h6":
				prefix = "###### "
			case "li":
				prefix = "- "
			}
		}

		if n.Type == html.TextNode && !skip {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				if prefix != "" {
					b.WriteString(prefix)
				}
				b.WriteString(text)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip, prefix)
		}
	}

	walk(doc, false, "")
	return b.String()
}

func isIgnoredHTMLTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "script", "style", "noscript", "iframe", "object", "embed":
		return true
	default:
		return false
	}
}
