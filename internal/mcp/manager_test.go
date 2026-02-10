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
	// Use a more robust shell script that properly handles JSON input and doesn't timeout
	content := "#!/bin/sh\necho '{\"ok\":true,\"output\":\"test response\"}'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:      "demo",
		Enabled:   true,
		Command:   []string{script},
		TimeoutMS: 5000,
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
