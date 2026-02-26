package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"coder/internal/config"
	"coder/internal/permission"
	"coder/internal/security"
)

func TestReadToolSmallFileDefaultLimit(t *testing.T) {
	root := t.TempDir()
	// 10 行小文件
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line-"+strconv.Itoa(i))
	}
	content := strings.Join(lines, "\n") + "\n"
	target := filepath.Join(root, "small.txt")
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := permission.PresetConfig("build")
	policy := permission.New(cfg)
	tool := NewReadTool(ws, policy)

	// 仅传 path，不带 offset/limit，期望读出全部 10 行
	args, _ := json.Marshal(map[string]any{
		"path": "small.txt",
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute read: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("ok=false: %v", result)
	}
	contentOut, _ := result["content"].(string)
	gotLines := strings.Split(strings.TrimRight(contentOut, "\n"), "\n")
	if len(gotLines) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(gotLines))
	}
	if start, _ := result["start_line"].(float64); start != 1 {
		t.Fatalf("start_line=%v, want 1", start)
	}
	if end, _ := result["end_line"].(float64); end != 10 {
		t.Fatalf("end_line=%v, want 10", end)
	}
	if hasMore, _ := result["has_more"].(bool); hasMore {
		t.Fatalf("has_more=true for small file")
	}
}

func TestReadToolLargeFilePagination(t *testing.T) {
	root := t.TempDir()
	// 200 行大文件
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, "line-"+strconv.Itoa(i))
	}
	content := strings.Join(lines, "\n") + "\n"
	target := filepath.Join(root, "large.txt")
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := permission.PresetConfig("build")
	policy := permission.New(cfg)
	tool := NewReadTool(ws, policy)

	// 默认只读前 50 行
	args, _ := json.Marshal(map[string]any{
		"path": "large.txt",
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute read: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	contentOut, _ := result["content"].(string)
	gotLines := strings.Split(strings.TrimRight(contentOut, "\n"), "\n")
	if len(gotLines) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(gotLines))
	}
	if gotLines[0] != "line-1" || gotLines[49] != "line-50" {
		t.Fatalf("unexpected first/last line: %q, %q", gotLines[0], gotLines[49])
	}
	if start, _ := result["start_line"].(float64); start != 1 {
		t.Fatalf("start_line=%v, want 1", start)
	}
	if end, _ := result["end_line"].(float64); end != 50 {
		t.Fatalf("end_line=%v, want 50", end)
	}
	if hasMore, _ := result["has_more"].(bool); !hasMore {
		t.Fatalf("has_more=false for large file first page")
	}
}

func TestReadToolOffsetAndLimit(t *testing.T) {
	root := t.TempDir()
	// 200 行大文件
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, "line-"+strconv.Itoa(i))
	}
	content := strings.Join(lines, "\n") + "\n"
	target := filepath.Join(root, "paged.txt")
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := permission.PresetConfig("build")
	policy := permission.New(cfg)
	tool := NewReadTool(ws, policy)

	// 从第 51 行开始读 50 行
	args, _ := json.Marshal(map[string]any{
		"path":   "paged.txt",
		"offset": 51,
		"limit":  50,
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute read: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	contentOut, _ := result["content"].(string)
	gotLines := strings.Split(strings.TrimRight(contentOut, "\n"), "\n")
	if len(gotLines) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(gotLines))
	}
	if gotLines[0] != "line-51" || gotLines[49] != "line-100" {
		t.Fatalf("unexpected first/last line: %q, %q", gotLines[0], gotLines[49])
	}
	if start, _ := result["start_line"].(float64); start != 51 {
		t.Fatalf("start_line=%v, want 51", start)
	}
	if end, _ := result["end_line"].(float64); end != 100 {
		t.Fatalf("end_line=%v, want 100", end)
	}
	if hasMore, _ := result["has_more"].(bool); !hasMore {
		t.Fatalf("has_more=false for middle page")
	}
}

func TestReadToolOffsetBeyondEOFAndInvalidLimit(t *testing.T) {
	root := t.TempDir()
	content := "only-one-line\n"
	target := filepath.Join(root, "single.txt")
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := permission.PresetConfig("build")
	policy := permission.New(cfg)
	tool := NewReadTool(ws, policy)

	// offset 大于文件总行数，期望 content 为空、has_more=false
	argsBeyond, _ := json.Marshal(map[string]any{
		"path":   "single.txt",
		"offset": 10,
	})
	rawBeyond, err := tool.Execute(context.Background(), argsBeyond)
	if err != nil {
		t.Fatalf("execute read (beyond EOF): %v", err)
	}
	var resultBeyond map[string]any
	if err := json.Unmarshal([]byte(rawBeyond), &resultBeyond); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	contentOut, _ := resultBeyond["content"].(string)
	if contentOut != "" {
		t.Fatalf("expected empty content beyond EOF, got %q", contentOut)
	}
	if hasMore, _ := resultBeyond["has_more"].(bool); hasMore {
		t.Fatalf("has_more=true beyond EOF")
	}

	// 非法 limit（<=0）会被归一化为默认值；这里不直接检查归一化值，只验证不会报错
	argsInvalidLimit, _ := json.Marshal(map[string]any{
		"path":  "single.txt",
		"limit": -1,
	})
	if _, err := tool.Execute(context.Background(), argsInvalidLimit); err != nil {
		t.Fatalf("execute read (invalid limit): %v", err)
	}
}

// TestReadToolExternalPathApproval 测试外部路径审批流程
func TestReadToolExternalPathApproval(t *testing.T) {
	// 创建一个临时目录作为 workspace
	wsRoot := t.TempDir()
	ws, err := security.NewWorkspace(wsRoot)
	if err != nil {
		t.Fatal(err)
	}

	// 在工作区外创建一个文件
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "external.txt")
	externalContent := "external file content"
	if err := os.WriteFile(externalFile, []byte(externalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 测试策略为 ask 时，ApprovalRequest 应该返回需要审批
	t.Run("ask_policy_returns_approval_request", func(t *testing.T) {
		cfg := config.PermissionConfig{ExternalDir: "ask"}
		policy := permission.New(cfg)
		tool := NewReadTool(ws, policy)

		args, _ := json.Marshal(map[string]any{
			"path": externalFile,
		})

		req, err := tool.ApprovalRequest(args)
		if err != nil {
			t.Fatalf("ApprovalRequest error: %v", err)
		}
		if req == nil {
			t.Fatal("expected ApprovalRequest for external path, got nil")
		}
		if req.Tool != "read" {
			t.Fatalf("expected tool name 'read', got %q", req.Tool)
		}
	})

	// 测试策略为 allow 时，ApprovalRequest 应该返回 nil
	t.Run("allow_policy_no_approval_request", func(t *testing.T) {
		cfg := config.PermissionConfig{ExternalDir: "allow"}
		policy := permission.New(cfg)
		tool := NewReadTool(ws, policy)

		args, _ := json.Marshal(map[string]any{
			"path": externalFile,
		})

		req, err := tool.ApprovalRequest(args)
		if err != nil {
			t.Fatalf("ApprovalRequest error: %v", err)
		}
		if req != nil {
			t.Fatalf("expected no ApprovalRequest for allow policy, got %v", req)
		}
	})

	// 测试策略为 deny 时，ApprovalRequest 应该返回 nil（直接拒绝）
	t.Run("deny_policy_no_approval_request", func(t *testing.T) {
		cfg := config.PermissionConfig{ExternalDir: "deny"}
		policy := permission.New(cfg)
		tool := NewReadTool(ws, policy)

		args, _ := json.Marshal(map[string]any{
			"path": externalFile,
		})

		req, err := tool.ApprovalRequest(args)
		if err != nil {
			t.Fatalf("ApprovalRequest error: %v", err)
		}
		if req != nil {
			t.Fatalf("expected no ApprovalRequest for deny policy, got %v", req)
		}
	})

	// 测试 ~ 路径展开和审批
	t.Run("home_path_expansion_and_approval", func(t *testing.T) {
		cfg := config.PermissionConfig{ExternalDir: "ask"}
		policy := permission.New(cfg)
		tool := NewReadTool(ws, policy)

		args, _ := json.Marshal(map[string]any{
			"path": "~/test_file.txt",
		})

		req, err := tool.ApprovalRequest(args)
		if err != nil {
			t.Fatalf("ApprovalRequest error: %v", err)
		}
		if req == nil {
			t.Fatal("expected ApprovalRequest for ~ path, got nil")
		}
	})

	// 测试工作区内路径不需要审批
	t.Run("workspace_path_no_approval", func(t *testing.T) {
		cfg := config.PermissionConfig{ExternalDir: "ask"}
		policy := permission.New(cfg)
		tool := NewReadTool(ws, policy)

		// 在工作区内创建文件
		internalFile := filepath.Join(wsRoot, "internal.txt")
		if err := os.WriteFile(internalFile, []byte("internal content"), 0o644); err != nil {
			t.Fatal(err)
		}

		args, _ := json.Marshal(map[string]any{
			"path": "internal.txt",
		})

		req, err := tool.ApprovalRequest(args)
		if err != nil {
			t.Fatalf("ApprovalRequest error: %v", err)
		}
		if req != nil {
			t.Fatalf("expected no ApprovalRequest for workspace path, got %v", req)
		}
	})
}
