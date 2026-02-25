package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coder/internal/chat"
	"coder/internal/security"

	pdf "github.com/ledongthuc/pdf"
)

// PDFParserTool 从 PDF 中提取纯文本内容（不包含图片），支持通过 URL 或工作区内文件路径读取。
type PDFParserTool struct {
	ws *security.Workspace
}

// NewPDFParserTool 创建 pdf_parser 工具实例。
func NewPDFParserTool(ws *security.Workspace) *PDFParserTool {
	return &PDFParserTool{ws: ws}
}

func (t *PDFParserTool) Name() string {
	return "pdf_parser"
}

func (t *PDFParserTool) Definition() chat.ToolDef {
	return chat.ToolDef{
		Type: "function",
		Function: chat.ToolFunction{
			Name:        t.Name(),
			Description: "Extract plain text content from a PDF file by URL or workspace path.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "HTTP/HTTPS URL of the PDF file. If provided, takes precedence over path.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Workspace-relative path to a local PDF file, used when url is not provided.",
					},
					"max_chars": map[string]any{
						"type":        "integer",
						"description": "Optional upper bound on the number of characters to return from extracted text.",
					},
				},
			},
		},
	}
}

type pdfParserArgs struct {
	URL      string `json:"url"`
	Path     string `json:"path"`
	MaxChars int    `json:"max_chars"`
}

type pdfParserResult struct {
	SourceURL  string `json:"source_url,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	SizeBytes  int64  `json:"size_bytes"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
}

const (
	defaultPDFTimeoutSec = 30
	maxPDFSizeBytes      = 10 * 1024 * 1024 // 10MB 上限，防止极大 PDF 导致内存与上下文压力。
	defaultMaxChars      = 250_000          // 单次抽取文本长度上限（按字符计），进一步防止上下文爆炸。
)

func (t *PDFParserTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in pdfParserArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("pdf_parser args: %w", err)
	}

	urlStr := strings.TrimSpace(in.URL)
	pathStr := strings.TrimSpace(in.Path)
	if urlStr == "" && pathStr == "" {
		return "", fmt.Errorf("either url or path must be provided")
	}

	maxChars := in.MaxChars
	if maxChars <= 0 || maxChars > defaultMaxChars {
		maxChars = defaultMaxChars
	}

	var (
		pdfPath   string
		sizeBytes int64
		sourceURL string
		sourceRel string
	)

	if urlStr != "" {
		u, err := url.Parse(urlStr)
		if err != nil {
			return "", fmt.Errorf("invalid url: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("url must use http or https scheme")
		}
		sourceURL = urlStr

		tmpFile, downloadedSize, err := downloadPDFToTemp(urlStr)
		if err != nil {
			return "", err
		}
		defer os.Remove(tmpFile)
		pdfPath = tmpFile
		sizeBytes = downloadedSize
	} else {
		// 本地路径：限制在 workspace 内。
		resolved, err := t.ws.Resolve(pathStr)
		if err != nil {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return "", fmt.Errorf("stat file: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("path is a directory, expected a PDF file")
		}
		if info.Size() > maxPDFSizeBytes {
			return "", fmt.Errorf("pdf file too large: %d bytes (limit %d bytes)", info.Size(), maxPDFSizeBytes)
		}
		if !strings.EqualFold(filepath.Ext(resolved), ".pdf") {
			return "", fmt.Errorf("expected a .pdf file, got %s", filepath.Ext(resolved))
		}
		pdfPath = resolved
		sizeBytes = info.Size()
		sourceRel = resolved
	}

	text, truncated, err := extractTextFromPDF(pdfPath, maxChars)
	if err != nil {
		return "", err
	}

	out := pdfParserResult{
		SourceURL:  sourceURL,
		SourcePath: sourceRel,
		SizeBytes:  sizeBytes,
		Content:    text,
		Truncated:  truncated,
	}
	return mustJSON(out), nil
}

func downloadPDFToTemp(urlStr string) (string, int64, error) {
	client := &http.Client{
		Timeout: time.Duration(defaultPDFTimeoutSec) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Get(urlStr)
	if err != nil {
		return "", 0, fmt.Errorf("download pdf: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("download pdf: unexpected status code %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if ct, _, _ := mime.ParseMediaType(contentType); ct != "" && !strings.EqualFold(ct, "application/pdf") {
		return "", 0, fmt.Errorf("expected application/pdf content type, got %s", contentType)
	}

	tmpFile, err := os.CreateTemp("", "coder-pdf-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	limited := io.LimitReader(resp.Body, maxPDFSizeBytes+1)
	written, err := io.Copy(tmpFile, limited)
	if err != nil {
		return "", 0, fmt.Errorf("write temp pdf: %w", err)
	}
	if written > maxPDFSizeBytes {
		return "", 0, fmt.Errorf("pdf file too large: %d bytes (limit %d bytes)", written, maxPDFSizeBytes)
	}
	return tmpFile.Name(), written, nil
}

func extractTextFromPDF(path string, maxChars int) (string, bool, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		return "", false, fmt.Errorf("extract text: %w", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", false, fmt.Errorf("read extracted text: %w", err)
	}

	text := buf.String()
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false, nil
	}
	return string(runes[:maxChars]), true, nil
}
