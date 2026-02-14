package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"coder/internal/security"
)

func TestGitManager_NotGitRepo(t *testing.T) {
	root := t.TempDir()
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)

	available, isRepo, _ := manager.Check()
	if !available {
		t.Skip("git not installed")
	}

	if isRepo {
		t.Fatal("expected not a git repo")
	}
}

func TestGitManager_GitRepo(t *testing.T) {
	root := t.TempDir()
	// Initialize git repository
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)

	available, isRepo, _ := manager.Check()
	if !available {
		t.Skip("git not installed")
	}

	if !isRepo {
		t.Fatal("expected a git repo")
	}
}

func TestGitStatusTool_NotRepo(t *testing.T) {
	root := t.TempDir()
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitStatusTool(ws, manager)

	args, _ := json.Marshal(map[string]any{})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Should return ok=false for non-git repo
	if result["ok"].(bool) {
		t.Fatal("expected ok=false for non-git repo")
	}

	if result["error"].(string) != "not a git repository" {
		t.Fatalf("expected 'not a git repository' error, got: %v", result["error"])
	}

	if _, ok := result["hint"]; !ok {
		t.Fatal("expected hint in error response")
	}
}

func TestGitStatusTool_Repo(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create a file
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitStatusTool(ws, manager)

	args, _ := json.Marshal(map[string]any{})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	content := result["content"].(string)
	if !strings.Contains(content, "test.txt") {
		t.Fatal("expected status to mention test.txt")
	}
}

func TestGitStatusTool_ShortFormat(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create and stage a file
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", root, "add", ".").Run()

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitStatusTool(ws, manager)

	args, _ := json.Marshal(map[string]any{"short": true})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	// Short format should be concise
	content := result["content"].(string)
	if len(content) == 0 {
		t.Fatal("expected non-empty short status")
	}
}

func TestGitDiffTool_Repo(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create initial commit
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "initial").Run()

	// Modify file
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitDiffTool(ws, manager)

	args, _ := json.Marshal(map[string]any{})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	content := result["content"].(string)
	if !strings.Contains(content, "initial") || !strings.Contains(content, "modified") {
		t.Fatal("expected diff to show changes")
	}
}

func TestGitDiffTool_Staged(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create and stage a file
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("staged content"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", root, "add", ".").Run()

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitDiffTool(ws, manager)

	args, _ := json.Marshal(map[string]any{"staged": true})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	content := result["content"].(string)
	if !strings.Contains(content, "staged content") {
		t.Fatal("expected staged diff to show staged content")
	}
}

func TestGitLogTool_Repo(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create a commit
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "test commit").Run()

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitLogTool(ws, manager)

	args, _ := json.Marshal(map[string]any{"limit": 5})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	content := result["content"].(string)
	if !strings.Contains(content, "test commit") {
		t.Fatal("expected log to contain commit message")
	}
}

func TestGitLogTool_Oneline(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create a commit
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", root, "add", ".").Run()
	exec.Command("git", "-C", root, "commit", "-m", "test commit").Run()

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitLogTool(ws, manager)

	args, _ := json.Marshal(map[string]any{"oneline": true, "limit": 5})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}
}

func TestGitAddTool_ApprovalRequired(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitAddTool(ws, manager)

	// Check that approval is required
	args, _ := json.Marshal(map[string]any{"path": "."})
	req, err := tool.ApprovalRequest(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req == nil {
		t.Fatal("expected approval request for git add")
	}

	if req.Tool != "git_add" {
		t.Fatalf("expected tool=git_add, got: %s", req.Tool)
	}

	if !strings.Contains(req.Reason, "staging") {
		t.Fatalf("expected reason to mention staging, got: %s", req.Reason)
	}
}

func TestGitAddTool_Execute(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create a file
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitAddTool(ws, manager)

	args, _ := json.Marshal(map[string]any{"path": "test.txt"})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	// Verify file was staged
	cmd := exec.Command("git", "-C", root, "diff", "--staged", "--name-only")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "test.txt") {
		t.Fatal("expected test.txt to be staged")
	}
}

func TestGitCommitTool_DangerousArgs(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitCommitTool(ws, manager)

	// Test dangerous parameters detection
	testCases := []string{
		"test --amend",
		"test --force",
		"test --no-verify",
		"test -n",
		"test --allow-empty",
	}

	for _, msg := range testCases {
		args, _ := json.Marshal(map[string]any{"message": msg})
		req, err := tool.ApprovalRequest(args)
		if err != nil {
			t.Fatalf("unexpected error for message '%s': %v", msg, err)
		}

		if req == nil {
			t.Fatalf("expected approval request for dangerous message: %s", msg)
		}

		if !strings.Contains(req.Reason, "dangerous") {
			t.Fatalf("expected reason to mention dangerous for message '%s', got: %s", msg, req.Reason)
		}
	}
}

func TestGitCommitTool_NormalArgs(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitCommitTool(ws, manager)

	// Test normal message (no dangerous flags)
	args, _ := json.Marshal(map[string]any{"message": "normal commit message"})
	req, err := tool.ApprovalRequest(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req == nil {
		t.Fatal("expected approval request for git commit")
	}

	if strings.Contains(req.Reason, "dangerous") {
		t.Fatalf("expected normal approval reason, got dangerous: %s", req.Reason)
	}

	if !strings.Contains(req.Reason, "creates a new commit") {
		t.Fatalf("expected reason to mention 'creates a new commit', got: %s", req.Reason)
	}
}

func TestGitCommitTool_Execute(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create and stage a file
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", root, "add", ".").Run()

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitCommitTool(ws, manager)

	args, _ := json.Marshal(map[string]any{"message": "test commit"})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	if result["message"].(string) != "test commit" {
		t.Fatalf("expected message='test commit', got: %v", result["message"])
	}

	// Verify commit was created
	cmd := exec.Command("git", "-C", root, "log", "-1", "--oneline")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), "test commit") {
		t.Fatal("expected commit to be created with correct message")
	}
}

func TestGitTools_NonGitRepo(t *testing.T) {
	root := t.TempDir()
	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)

	// Test all tools return proper error for non-git repo
	tools := []Tool{
		NewGitStatusTool(ws, manager),
		NewGitDiffTool(ws, manager),
		NewGitLogTool(ws, manager),
		NewGitAddTool(ws, manager),
		NewGitCommitTool(ws, manager),
	}

	for _, tool := range tools {
		var args []byte
		if tool.Name() == "git_add" {
			args, _ = json.Marshal(map[string]any{"path": "."})
		} else if tool.Name() == "git_commit" {
			args, _ = json.Marshal(map[string]any{"message": "test"})
		} else {
			args, _ = json.Marshal(map[string]any{})
		}
		out, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("tool %s: unexpected error: %v", tool.Name(), err)
		}

		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("tool %s: unmarshal result: %v", tool.Name(), err)
		}

		if result["ok"].(bool) {
			t.Fatalf("tool %s: expected ok=false for non-git repo", tool.Name())
		}

		if result["error"].(string) != "not a git repository" && result["error"].(string) != "git not installed" {
			t.Fatalf("tool %s: expected git-related error, got: %v", tool.Name(), result["error"])
		}
	}
}

func TestGitLogTool_Limit(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skip("git not available")
	}
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	// Create multiple commits
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("file%d.txt", i)
		if err := os.WriteFile(filepath.Join(root, filename), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "-C", root, "add", ".").Run()
		exec.Command("git", "-C", root, "commit", "-m", fmt.Sprintf("commit %d", i)).Run()
	}

	ws, err := security.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewGitManager(ws)
	tool := NewGitLogTool(ws, manager)

	// Test with limit=3
	args, _ := json.Marshal(map[string]any{"limit": 3, "oneline": true})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result["ok"].(bool) {
		t.Fatalf("expected ok=true, got error: %v", result["error"])
	}

	// Verify limit is respected (output should contain 3 commits + initial)
	content := result["content"].(string)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) > 4 { // 3 commits + possible empty line
		t.Fatalf("expected at most 4 lines with limit=3, got %d", len(lines))
	}
}
