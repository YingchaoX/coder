package defaults

import "testing"

func TestDefaultSystemPrompt(t *testing.T) {
	if DefaultSystemPrompt == "" {
		t.Fatal("DefaultSystemPrompt must be non-empty")
	}
	if len(DefaultSystemPrompt) < 20 {
		t.Fatalf("DefaultSystemPrompt too short: %d", len(DefaultSystemPrompt))
	}
}
