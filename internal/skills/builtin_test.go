package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeBuiltin(t *testing.T) {
	m, err := Discover([]string{})
	if err != nil {
		t.Fatal(err)
	}
	MergeBuiltin(m)
	list := m.List()
	var found bool
	for _, info := range list {
		if info.Name == "create-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("create-skill not in list after MergeBuiltin: %+v", list)
	}
	content, err := m.Load("create-skill")
	if err != nil {
		t.Fatal(err)
	}
	if content == "" {
		t.Fatal("create-skill content empty")
	}
	if !strings.Contains(content, "Creating Skills in coder") {
		t.Errorf("create-skill content should mention coder, got snippet: %s", content[:min(200, len(content))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestMergeBuiltin_UserOverridesBuiltin(t *testing.T) {
	// When a user path already has create-skill, MergeBuiltin must not overwrite it.
	root := t.TempDir()
	skillDir := filepath.Join(root, "create-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userContent := "---\nname: create-skill\ndescription: user overrides\n---\n\nuser content"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	MergeBuiltin(m)
	content, err := m.Load("create-skill")
	if err != nil {
		t.Fatal(err)
	}
	if content != userContent {
		t.Fatalf("user create-skill should override builtin; got %q", content)
	}
}
