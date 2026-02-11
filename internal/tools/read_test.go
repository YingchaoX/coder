package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
	tool := NewReadTool(ws)

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
	tool := NewReadTool(ws)

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
	tool := NewReadTool(ws)

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
	tool := NewReadTool(ws)

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
