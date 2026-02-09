package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"coder/internal/chat"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(ts ...Tool) *Registry {
	m := make(map[string]Tool, len(ts))
	for _, t := range ts {
		m[t.Name()] = t
	}
	return &Registry{tools: m}
}

func (r *Registry) Definitions() []chat.ToolDef {
	return r.DefinitionsFiltered(nil)
}

func (r *Registry) DefinitionsFiltered(allowed map[string]bool) []chat.ToolDef {
	out := make([]chat.ToolDef, 0, len(r.tools))
	names := r.Names()
	for _, name := range names {
		if allowed != nil {
			enabled, ok := allowed[name]
			if ok && !enabled {
				continue
			}
		}
		out = append(out, r.tools[name].Definition())
	}
	return out
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return t.Execute(ctx, args)
}

func (r *Registry) ApprovalRequest(name string, args json.RawMessage) (*ApprovalRequest, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	aa, ok := t.(ApprovalAware)
	if !ok {
		return nil, nil
	}
	return aa.ApprovalRequest(args)
}
