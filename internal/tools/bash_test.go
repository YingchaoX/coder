package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashToolExecuteAndApproval(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "exists.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewBashTool(root, 2000, 32)

	req, err := tool.ApprovalRequest(json.RawMessage(`{"command":"echo hi > exists.txt"}`))
	if err != nil {
		t.Fatalf("ApprovalRequest failed: %v", err)
	}
	if req == nil || !strings.Contains(req.Reason, "overwrite redirection") {
		t.Fatalf("expected overwrite approval reason, got: %+v", req)
	}

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf 'hello world hello world'"}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(out, `"command":"printf 'hello world hello world'"`) {
		t.Fatalf("unexpected execute output: %s", out)
	}
	if !strings.Contains(out, "[output truncated]") {
		t.Fatalf("expected truncated marker: %s", out)
	}
}

func TestBashToolExecuteEmptyCommand(t *testing.T) {
	tool := NewBashTool(t.TempDir(), 1000, 64)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"   "}`))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty command error, got: %v", err)
	}
}

func TestCappedBufferWrite(t *testing.T) {
	b := newCappedBuffer(4)
	_, _ = b.Write([]byte("abcdef"))
	if !b.truncated {
		t.Fatalf("expected truncated")
	}
	if got := b.String(); !strings.Contains(got, "[output truncated]") {
		t.Fatalf("unexpected string: %q", got)
	}
}
