package contextmgr

import (
	"strings"
	"sync"

	"coder/internal/chat"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer 精确 token 计数器，支持 tiktoken 和启发式回退
// Tokenizer provides precise token counting with tiktoken and heuristic fallback
type Tokenizer struct {
	encoder      *tiktoken.Tiktoken
	encodingName string
	fallback     bool // 是否使用启发式回退 / Whether using heuristic fallback
	mu           sync.RWMutex
}

var (
	defaultTokenizer     *Tokenizer
	defaultTokenizerOnce sync.Once
)

// DefaultTokenizer 返回全局默认的 tokenizer 实例
// DefaultTokenizer returns the global default tokenizer instance
func DefaultTokenizer() *Tokenizer {
	defaultTokenizerOnce.Do(func() {
		defaultTokenizer = NewTokenizer("cl100k_base")
	})
	return defaultTokenizer
}

// NewTokenizer 创建 tokenizer，如果 tiktoken 初始化失败则回退到启发式
// NewTokenizer creates a tokenizer, falls back to heuristic if tiktoken init fails
func NewTokenizer(encodingName string) *Tokenizer {
	t := &Tokenizer{encodingName: encodingName}

	enc, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		// 离线环境可能没有 BPE 缓存，回退到启发式
		// Offline environments may lack BPE cache, fallback to heuristic
		t.fallback = true
		return t
	}
	t.encoder = enc
	return t
}

// NewTokenizerForModel 根据模型名自动选择编码
// NewTokenizerForModel auto-selects encoding based on model name
func NewTokenizerForModel(model string) *Tokenizer {
	encoding := modelToEncoding(model)
	return NewTokenizer(encoding)
}

// Count 计算消息列表的总 token 数
// Count returns total token count for a message list
func (t *Tokenizer) Count(messages []chat.Message) int {
	total := 0
	for _, msg := range messages {
		total += t.countMessage(msg)
	}
	return total
}

// CountText 计算单个文本的 token 数
// CountText counts tokens for a single text string
func (t *Tokenizer) CountText(text string) int {
	if text == "" {
		return 0
	}
	if t.fallback {
		return heuristicTokenCount(text)
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	tokens := t.encoder.Encode(text, nil, nil)
	return len(tokens)
}

// IsPrecise 返回是否使用精确计数
// IsPrecise returns whether precise counting is available
func (t *Tokenizer) IsPrecise() bool {
	return !t.fallback
}

// EncodingName 返回编码名称
// EncodingName returns the encoding name
func (t *Tokenizer) EncodingName() string {
	return t.encodingName
}

func (t *Tokenizer) countMessage(msg chat.Message) int {
	// OpenAI 消息 token 开销: ~4 tokens per message overhead
	// OpenAI message token overhead: ~4 tokens per message
	tokens := 4
	tokens += t.CountText(msg.Content)
	tokens += t.CountText(msg.Role)
	if msg.Name != "" {
		tokens += t.CountText(msg.Name)
		tokens++ // name 字段额外 1 token
	}
	if msg.Reasoning != "" {
		tokens += t.CountText(msg.Reasoning)
	}
	for _, tc := range msg.ToolCalls {
		tokens += t.CountText(tc.Function.Name)
		tokens += t.CountText(tc.Function.Arguments)
		tokens += 8 // tool call 结构开销 / tool call structure overhead
	}
	return tokens
}

// heuristicTokenCount 启发式 token 估算 (改进版: chars/3.5)
// heuristicTokenCount is an improved heuristic (chars/3.5 for mixed CJK/English)
func heuristicTokenCount(text string) int {
	if text == "" {
		return 0
	}
	// CJK 字符通常 1-2 token/字, 英文约 4 chars/token
	// CJK characters are typically 1-2 tokens each, English ~4 chars/token
	cjkCount := 0
	asciiCount := 0
	for _, r := range text {
		if isCJK(r) {
			cjkCount++
		} else {
			asciiCount++
		}
	}
	// CJK: ~1.5 tokens per character, ASCII: ~0.25 tokens per character
	estimate := int(float64(cjkCount)*1.5 + float64(asciiCount)*0.25)
	if estimate < 1 {
		estimate = 1
	}
	return estimate
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols
		(r >= 0xFF00 && r <= 0xFFEF) || // Fullwidth Forms
		(r >= 0xAC00 && r <= 0xD7AF) // Korean Hangul
}

// modelToEncoding 根据模型名推断编码
// modelToEncoding maps model name to encoding name
func modelToEncoding(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return "cl100k_base"
	}

	// o1/o3 系列使用 o200k_base
	// o1/o3 series use o200k_base
	if strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") {
		return "o200k_base"
	}

	// GPT-4o 和更新的模型
	// GPT-4o and newer models
	if strings.HasPrefix(m, "gpt-4o") || strings.HasPrefix(m, "chatgpt-4o") {
		return "o200k_base"
	}

	// GPT-4, GPT-3.5 系列
	if strings.HasPrefix(m, "gpt-4") || strings.HasPrefix(m, "gpt-3.5") {
		return "cl100k_base"
	}

	// Qwen 系列 (通义千问) 使用 cl100k_base 兼容
	if strings.HasPrefix(m, "qwen") {
		return "cl100k_base"
	}

	// Claude 系列
	if strings.HasPrefix(m, "claude") {
		return "cl100k_base"
	}

	// 默认 / Default
	return "cl100k_base"
}
