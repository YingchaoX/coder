package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coder/internal/config"
)

func TestManagerStartAndCall(t *testing.T) {
	script := filepath.Join(t.TempDir(), "server.sh")
	content := "#!/bin/sh\nwhile IFS= read -r line; do\n  echo '{\"ok\":true,\"line\":'\"$line\"'}'\n  break\ndone\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:      "demo",
		Enabled:   true,
		Command:   []string{script},
		TimeoutMS: 1000,
	}}})
	mgr.StartEnabled(context.Background())
	server, ok := mgr.ServerByTool("mcp_demo")
	if !ok {
		t.Fatalf("tool server not found")
	}
	result, err := server.Call(context.Background(), map[string]any{"x": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "ok") {
		t.Fatalf("unexpected result: %s", result)
	}
}
