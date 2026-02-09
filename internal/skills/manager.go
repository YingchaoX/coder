package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

type Manager struct {
	items map[string]Info
}

func Discover(paths []string) (*Manager, error) {
	items := map[string]Info{}
	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}

		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.ToUpper(d.Name()) != "SKILL.MD" {
				return nil
			}
			info, perr := parseSkill(path)
			if perr != nil {
				return nil
			}
			if _, ok := items[info.Name]; ok {
				return fmt.Errorf("duplicate skill name: %s", info.Name)
			}
			items[info.Name] = info
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return &Manager{items: items}, nil
}

func (m *Manager) List() []Info {
	if m == nil {
		return nil
	}
	out := make([]Info, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (m *Manager) Get(name string) (Info, bool) {
	if m == nil {
		return Info{}, false
	}
	v, ok := m.items[name]
	return v, ok
}

func (m *Manager) Load(name string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("skill manager unavailable")
	}
	item, ok := m.items[name]
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	data, err := os.ReadFile(item.Path)
	if err != nil {
		return "", fmt.Errorf("read skill: %w", err)
	}
	return string(data), nil
}

func parseSkill(path string) (Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	content := string(data)
	name := ""
	desc := ""

	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "---") {
		front, _ := splitFrontmatter(trimmed)
		for _, line := range strings.Split(front, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "name:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			}
			if strings.HasPrefix(strings.ToLower(line), "description:") {
				desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			}
		}
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	if desc == "" {
		desc = firstParagraph(content)
	}
	if desc == "" {
		desc = "No description"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return Info{Name: name, Description: desc, Path: abs}, nil
}

func splitFrontmatter(content string) (string, string) {
	parts := strings.SplitN(content, "\n", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != "---" {
		return "", content
	}
	rest := parts[1]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}
	front := rest[:idx]
	body := rest[idx+4:]
	return front, body
}

func firstParagraph(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}
