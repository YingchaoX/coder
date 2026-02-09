package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"coder/internal/security"
)

func TestNormalizedModels(t *testing.T) {
	existing := []string{"qwen-plus", "qwen-plus", " ", "qwen-max"}
	got := normalizedModels(existing, "qwen-turbo")
	want := []string{"qwen-turbo", "qwen-plus", "qwen-max"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizedModels()=%v, want %v", got, want)
	}
}

func TestExpandFileMentions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}

	got := expandFileMentions("请查看 @note.txt", ws)
	if !strings.Contains(got, "[FILE_MENTIONS]") || !strings.Contains(got, "hello") {
		t.Fatalf("expandFileMentions() missing file content: %q", got)
	}
}

func TestExpandFileMentionsSkipBangCommand(t *testing.T) {
	root := t.TempDir()
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	input := "!cat @note.txt"
	if got := expandFileMentions(input, ws); got != input {
		t.Fatalf("bang command should not expand mentions: got=%q", got)
	}
}
