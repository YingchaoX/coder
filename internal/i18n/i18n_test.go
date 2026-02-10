package i18n

import "testing"

func TestNew_English(t *testing.T) {
	i := New("en")
	if i.Locale() != "en" {
		t.Fatalf("Locale()=%q, want en", i.Locale())
	}
	got := i.T("panel.chat")
	if got != "Chat" {
		t.Fatalf("T(panel.chat)=%q, want Chat", got)
	}
}

func TestNew_Chinese(t *testing.T) {
	i := New("zh-CN")
	if i.Locale() != "zh-CN" {
		t.Fatalf("Locale()=%q, want zh-CN", i.Locale())
	}
	got := i.T("panel.chat")
	if got != "对话" {
		t.Fatalf("T(panel.chat)=%q, want 对话", got)
	}
}

func TestNew_ChineseFromLang(t *testing.T) {
	i := New("zh_CN.UTF-8")
	if i.Locale() != "zh-CN" {
		t.Fatalf("Locale()=%q, want zh-CN", i.Locale())
	}
	got := i.T("panel.files")
	if got != "文件" {
		t.Fatalf("T(panel.files)=%q, want 文件", got)
	}
}

func TestT_WithArgs(t *testing.T) {
	i := New("en")
	got := i.T("error.provider", "timeout")
	if got != "Provider error: timeout" {
		t.Fatalf("T with args=%q, want Provider error: timeout", got)
	}
}

func TestT_MissingKey(t *testing.T) {
	i := New("en")
	got := i.T("nonexistent.key")
	if got != "nonexistent.key" {
		t.Fatalf("T missing key=%q, want key itself", got)
	}
}

func TestNormalizeLocale(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"en_US.UTF-8", "en"},
		{"zh_CN.UTF-8", "zh-CN"},
		{"zh_TW", "zh-CN"},
		{"en", "en"},
		{"", "en"},
		{"fr_FR", "fr-FR"},
	}
	for _, tt := range tests {
		got := normalizeLocale(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeLocale(%q)=%q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGlobal(t *testing.T) {
	g := Global()
	if g == nil {
		t.Fatal("Global() should not be nil")
	}
	// 应该返回同一实例 / Should return same instance
	g2 := Global()
	if g != g2 {
		t.Fatal("Global() should return same instance")
	}
}
