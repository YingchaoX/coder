package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAndLoad(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: demo desc\n---\n\nhello"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	list := m.List()
	if len(list) != 1 || list[0].Name != "demo-skill" {
		t.Fatalf("unexpected list: %+v", list)
	}
	loaded, err := m.Load("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == "" {
		t.Fatalf("empty loaded content")
	}
}
