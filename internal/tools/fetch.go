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

type FetchArgs struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	TimeoutSec int               `json:"timeout_sec"`
	MaxSizeKB  int               `json:"max_size_kb"`
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
			Description: "Fetch content from HTTP/HTTPS URL, supports text and images",
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
					"timeout_sec": map[string]any{
						"type":        "integer",
						"description": "Request timeout in seconds",
						"default":     30,
					},
					"max_size_kb": map[string]any{
						"type":        "integer",
						"description": "Maximum response size in KB",
						"default":     1000, // 1MB
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
		// For images, use max image size; for text, use max text size
		maxSizeKB = t.cfg.MaxTextSizeKB * 1000 // Convert MB to KB for images
		if maxSizeKB <= 0 {
			maxSizeKB = 1000 // 1MB default
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
		maxSizeBytes = t.cfg.MaxTextSizeKB * 1024 // Convert KB to bytes
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

	var content string
	if isImage {
		// For images, encode as base64
		content = base64.StdEncoding.EncodeToString(responseData)
	} else {
		// For text content, truncate to max text size if needed
		textContent := string(responseData)
		maxTextBytes := t.cfg.MaxTextSizeKB * 1024
		if len(textContent) > maxTextBytes {
			textContent = textContent[:maxTextBytes]
		}
		content = textContent
	}

	result := FetchResult{
		URL:         in.URL,
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		IsImage:     isImage,
		Content:     content,
		SizeBytes:   len(responseData),
	}

	// Handle 401 Unauthorized - try again with auth if available
	if resp.StatusCode == 401 && in.Auth == nil {
		return "", fmt.Errorf("authentication required (received 401), please provide auth credentials")
	}

	return mustJSON(result), nil
}
