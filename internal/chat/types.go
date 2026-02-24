package chat

// ToolFunction describes an OpenAI-compatible function tool definition.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolDef describes one function tool exposed to the model.
type ToolDef struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolCallFunction is the function payload of a model tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall is an OpenAI-compatible tool call.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ContentPart represents a part of a multi-modal message content
type ContentPart interface {
	isContentPart()
}

// TextContent represents text content in a multi-modal message
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (t TextContent) isContentPart() {}

// ImageContent represents image content in a multi-modal message
type ImageContent struct {
	Type     string   `json:"type"`
	ImageURL ImageURL `json:"image_url"`
}

func (i ImageContent) isContentPart() {}

// ImageURL represents an image URL in multi-modal messages
type ImageURL struct {
	URL    string `json:"url"`              // URL or data URL
	Detail string `json:"detail,omitempty"` // "low", "high", or "auto"
}

// Message is an OpenAI-compatible chat message.
type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content,omitempty"` // For backward compatibility
	MultiContent []ContentPart `json:"-"`                 // Multi-modal content (takes precedence over Content)
	Reasoning    string        `json:"reasoning,omitempty"`
	Name         string        `json:"name,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
}
