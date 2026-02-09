package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"coder/internal/chat"
	"coder/internal/config"
)

type Client struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
	mu         sync.RWMutex
}

type Response struct {
	Content      string
	ToolCalls    []chat.ToolCall
	FinishReason string
}

type TextChunkFunc func(chunk string)

func NewClient(cfg config.ProviderConfig) *Client {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Chat(
	ctx context.Context,
	messages []chat.Message,
	tools []chat.ToolDef,
	onTextChunk TextChunkFunc,
) (Response, error) {
	model := c.Model()
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return Response{}, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("send chat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return Response{}, fmt.Errorf("chat request failed: status=%d (read error: %v)", resp.StatusCode, readErr)
		}
		return Response{}, fmt.Errorf("chat request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/event-stream") {
		return parseStreamResponse(resp.Body, onTextChunk)
	}
	return parseNonStreamResponse(resp.Body, onTextChunk)
}

func (c *Client) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

func (c *Client) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model is empty")
	}
	c.mu.Lock()
	c.model = model
	c.mu.Unlock()
	return nil
}

func parseNonStreamResponse(body io.Reader, onTextChunk TextChunkFunc) (Response, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return Response{}, fmt.Errorf("read chat response: %w", err)
	}

	var raw openAIResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return Response{}, fmt.Errorf("parse chat response: %w", err)
	}
	if len(raw.Choices) == 0 {
		return Response{}, fmt.Errorf("chat response has no choices")
	}

	msg := raw.Choices[0].Message
	content, err := parseContent(msg.Content)
	if err != nil {
		return Response{}, err
	}
	if onTextChunk != nil && content != "" {
		onTextChunk(content)
	}
	return Response{
		Content:      content,
		ToolCalls:    msg.ToolCalls,
		FinishReason: raw.Choices[0].FinishReason,
	}, nil
}

func parseStreamResponse(body io.Reader, onTextChunk TextChunkFunc) (Response, error) {
	reader := bufio.NewReader(body)
	var (
		contentBuilder strings.Builder
		toolCallsByIdx = map[int]*toolCallBuilder{}
		finishReason   string
		dataLines      []string
	)

	processEvent := func(payload string) error {
		payload = strings.TrimSpace(payload)
		if payload == "" || payload == "[DONE]" {
			return nil
		}

		var event openAIStreamEvent
		data := []byte(payload)
		if err := json.Unmarshal(data, &event); err != nil {
			// 流式 SSE 中某行可能被截断，缺根对象闭合 }，补全后重试
			errStr := err.Error()
			if len(data) > 0 &&
				(strings.Contains(errStr, "unexpected end of JSON input") ||
					strings.Contains(errStr, "after object key:value pair")) {
				data = append(data, '}')
				if retryErr := json.Unmarshal(data, &event); retryErr != nil {
					return fmt.Errorf("parse stream event: %w (retry: %v)", err, retryErr)
				}
			} else {
				return fmt.Errorf("parse stream event: %w", err)
			}
		}

		for _, choice := range event.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			text, err := parseDeltaContent(choice.Delta.Content)
			if err != nil {
				return err
			}
			if text != "" {
				contentBuilder.WriteString(text)
				if onTextChunk != nil {
					onTextChunk(text)
				}
			}
			for _, toolDelta := range choice.Delta.ToolCalls {
				appendToolCallDelta(toolCallsByIdx, toolDelta)
			}
		}
		return nil
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return Response{}, fmt.Errorf("read stream response: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(dataLines) > 0 {
				if err := processEvent(strings.Join(dataLines, "\n")); err != nil {
					return Response{}, err
				}
				dataLines = dataLines[:0]
			}
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}

		if err == io.EOF {
			break
		}
	}
	if len(dataLines) > 0 {
		if err := processEvent(strings.Join(dataLines, "\n")); err != nil {
			return Response{}, err
		}
	}

	return Response{
		Content:      contentBuilder.String(),
		ToolCalls:    buildToolCalls(toolCallsByIdx),
		FinishReason: finishReason,
	}, nil
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []chat.ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type openAIStreamEvent struct {
	Choices []struct {
		Delta struct {
			Content   json.RawMessage       `json:"content"`
			ToolCalls []streamToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolCallBuilder struct {
	ID        string
	Type      string
	Name      string
	Arguments strings.Builder
}

func appendToolCallDelta(target map[int]*toolCallBuilder, delta streamToolCallDelta) {
	current, ok := target[delta.Index]
	if !ok {
		current = &toolCallBuilder{}
		target[delta.Index] = current
	}

	if delta.ID != "" {
		current.ID = delta.ID
	}
	if delta.Type != "" {
		current.Type = delta.Type
	}
	if delta.Function.Name != "" {
		if current.Name == "" {
			current.Name = delta.Function.Name
		} else if strings.HasPrefix(delta.Function.Name, current.Name) {
			current.Name = delta.Function.Name
		} else {
			current.Name += delta.Function.Name
		}
	}
	if delta.Function.Arguments != "" {
		current.Arguments.WriteString(delta.Function.Arguments)
	}
}

func buildToolCalls(toolCallsByIdx map[int]*toolCallBuilder) []chat.ToolCall {
	if len(toolCallsByIdx) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(toolCallsByIdx))
	for idx := range toolCallsByIdx {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	calls := make([]chat.ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		b := toolCallsByIdx[idx]
		if b == nil {
			continue
		}
		id := strings.TrimSpace(b.ID)
		if id == "" {
			id = "call_" + strconv.Itoa(idx)
		}
		kind := strings.TrimSpace(b.Type)
		if kind == "" {
			kind = "function"
		}
		calls = append(calls, chat.ToolCall{
			ID:   id,
			Type: kind,
			Function: chat.ToolCallFunction{
				Name:      strings.TrimSpace(b.Name),
				Arguments: b.Arguments.String(),
			},
		})
	}
	return calls
}

func parseDeltaContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	var parts []struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		OutputText string `json:"output_text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil && len(parts) > 0 {
		var builder strings.Builder
		for _, part := range parts {
			text := part.Text
			if text == "" {
				text = part.OutputText
			}
			if text == "" {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(part.Type))
			if kind != "" && kind != "text" && kind != "output_text" {
				continue
			}
			builder.WriteString(text)
		}
		return builder.String(), nil
	}

	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", fmt.Errorf("parse stream delta content: %w", err)
	}
	return extractText(generic), nil
}

func parseContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	// Some providers may return content as typed parts instead of a plain string.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil && len(parts) > 0 {
		var builder strings.Builder
		for _, part := range parts {
			if part.Text == "" {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(part.Type))
			if kind != "" && kind != "text" && kind != "output_text" {
				continue
			}
			builder.WriteString(part.Text)
		}
		if builder.Len() > 0 {
			return builder.String(), nil
		}
	}

	// Fallback for providers that wrap text in nested array/object structures.
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", fmt.Errorf("parse response content: %w", err)
	}
	if extracted := extractText(generic); extracted != "" {
		return extracted, nil
	}
	compact, err := json.Marshal(generic)
	if err != nil {
		return "", fmt.Errorf("compact response content: %w", err)
	}
	return string(compact), nil
}

func extractText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		var builder strings.Builder
		for _, item := range val {
			text := extractText(item)
			if text == "" {
				continue
			}
			builder.WriteString(text)
		}
		return builder.String()
	case map[string]any:
		if kind, ok := val["type"].(string); ok {
			normalized := strings.ToLower(strings.TrimSpace(kind))
			if normalized != "" && normalized != "text" && normalized != "output_text" {
				if nested, ok := val["content"]; ok {
					return extractText(nested)
				}
				return ""
			}
		}
		if text, ok := val["text"].(string); ok && text != "" {
			return text
		}
		if outputText, ok := val["output_text"].(string); ok && outputText != "" {
			return outputText
		}
		if content, ok := val["content"]; ok {
			if text := extractText(content); text != "" {
				return text
			}
		}
		if value, ok := val["value"]; ok {
			if text := extractText(value); text != "" {
				return text
			}
		}
	}
	return ""
}
