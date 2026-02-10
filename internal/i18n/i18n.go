package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// I18n 国际化支持
// I18n provides internationalization support
type I18n struct {
	locale   string
	messages map[string]string
	mu       sync.RWMutex
}

var (
	global     *I18n
	globalOnce sync.Once
)

// Global 返回全局 i18n 实例
// Global returns the global i18n instance
func Global() *I18n {
	globalOnce.Do(func() {
		global = New("")
	})
	return global
}

// Init 初始化全局 i18n 实例
// Init initializes the global i18n instance
func Init(locale string) {
	global = New(locale)
}

// T 全局翻译快捷函数
// T is a global translation shortcut
func T(key string, args ...any) string {
	return Global().T(key, args...)
}

// New 创建 i18n 实例
// New creates an i18n instance
func New(locale string) *I18n {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = DetectLocale()
	}
	locale = normalizeLocale(locale)

	i := &I18n{
		locale:   locale,
		messages: make(map[string]string),
	}

	// 先加载英文作为 fallback / Load English as fallback first
	for k, v := range EnMessages {
		i.messages[k] = v
	}

	// 如果是中文，覆盖 / If Chinese, overlay
	if locale == "zh-CN" || locale == "zh" {
		for k, v := range ZhCNMessages {
			i.messages[k] = v
		}
	}

	return i
}

// T 翻译函数 / Translation function
func (i *I18n) T(key string, args ...any) string {
	i.mu.RLock()
	tmpl, ok := i.messages[key]
	i.mu.RUnlock()

	if !ok {
		return key
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

// Locale 返回当前 locale
// Locale returns current locale
func (i *I18n) Locale() string {
	return i.locale
}

// DetectLocale 自动检测 locale
// DetectLocale auto-detects locale from environment
func DetectLocale() string {
	for _, env := range []string{"AGENT_LANG", "LANG", "LC_ALL", "LC_MESSAGES"} {
		v := strings.TrimSpace(os.Getenv(env))
		if v == "" {
			continue
		}
		return normalizeLocale(v)
	}
	return "en"
}

func normalizeLocale(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "en"
	}
	// 去掉 .UTF-8 等后缀 / Remove .UTF-8 suffix
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.ReplaceAll(s, "_", "-")
	lower := strings.ToLower(s)

	if strings.HasPrefix(lower, "zh") {
		return "zh-CN"
	}
	if strings.HasPrefix(lower, "en") {
		return "en"
	}
	// 默认返回原始值 / Default return original
	return s
}
