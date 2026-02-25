package permission

import (
	"encoding/json"
	"testing"

	"coder/internal/config"
)

func TestPolicyDecide(t *testing.T) {
	p := New(config.PermissionConfig{
		Default:   "ask",
		Read:      "allow",
		Edit:      "deny",
		Write:     "deny",
		TodoRead:  "allow",
		TodoWrite: "deny",
		Bash: map[string]string{
			"*":      "ask",
			"ls *":   "allow",
			"rm *":   "deny",
			"grep *": "allow",
		},
	})

	if got := p.Decide("read", nil).Decision; got != DecisionAllow {
		t.Fatalf("read decision=%s", got)
	}
	if got := p.Decide("edit", nil).Decision; got != DecisionDeny {
		t.Fatalf("edit decision=%s", got)
	}
	if got := p.Decide("write", nil).Decision; got != DecisionDeny {
		t.Fatalf("write decision=%s", got)
	}
	if got := p.Decide("skill", nil).Decision; got != DecisionAsk {
		t.Fatalf("skill decision=%s", got)
	}
	if got := p.Decide("todoread", nil).Decision; got != DecisionAllow {
		t.Fatalf("todoread decision=%s", got)
	}
	if got := p.Decide("todowrite", nil).Decision; got != DecisionDeny {
		t.Fatalf("todowrite decision=%s", got)
	}

	allowArgs := json.RawMessage(`{"command":"ls -la"}`)
	if got := p.Decide("bash", allowArgs).Decision; got != DecisionAllow {
		t.Fatalf("bash allow decision=%s", got)
	}

	denyArgs := json.RawMessage(`{"command":"rm -rf build"}`)
	if got := p.Decide("bash", denyArgs).Decision; got != DecisionDeny {
		t.Fatalf("bash deny decision=%s", got)
	}
}

func TestPolicyDecide_CommandAllowlist(t *testing.T) {
	p := New(config.PermissionConfig{
		Default: "ask",
		Bash: map[string]string{
			"*": "ask",
		},
		CommandAllowlist: []string{"ls"},
	})

	args := json.RawMessage(`{"command":"FOO=1 ls -la"}`)
	if got := p.Decide("bash", args).Decision; got != DecisionAllow {
		t.Fatalf("bash decision with allowlist=%s, want allow", got)
	}
}

func TestPresetConfigModes(t *testing.T) {
	if _, ok := PresetConfig("build"); !ok {
		t.Fatal("build preset should exist")
	}
	if _, ok := PresetConfig("plan"); !ok {
		t.Fatal("plan preset should exist")
	}
	if _, ok := PresetConfig("balanced"); ok {
		t.Fatal("balanced preset should not exist")
	}
	if _, ok := PresetConfig("yolo"); ok {
		t.Fatal("yolo preset should not exist")
	}
}

func TestPresetConfigPlanBashRules(t *testing.T) {
	cfg, ok := PresetConfig("plan")
	if !ok {
		t.Fatal("plan preset should exist")
	}
	p := New(cfg)

	cases := []struct {
		command string
		want    Decision
	}{
		{command: "uname", want: DecisionAllow},
		{command: "uname -a", want: DecisionAllow},
		{command: "python -V", want: DecisionAsk},
		{command: "touch a.txt", want: DecisionAsk},
		{command: "echo hi > a.txt", want: DecisionAsk},
		{command: "git add .", want: DecisionAsk},
	}

	for _, tc := range cases {
		raw := json.RawMessage(`{"command":"` + tc.command + `"}`)
		got := p.Decide("bash", raw).Decision
		if got != tc.want {
			t.Fatalf("plan bash decision for %q = %s, want %s", tc.command, got, tc.want)
		}
	}
}

func TestPresetConfigPlanReadToolsAllowed(t *testing.T) {
	cfg, ok := PresetConfig("plan")
	if !ok {
		t.Fatal("plan preset should exist")
	}
	p := New(cfg)

	cases := []struct {
		tool string
		want Decision
	}{
		{tool: "read", want: DecisionAllow},
		{tool: "list", want: DecisionAllow},
		{tool: "glob", want: DecisionAllow},
		{tool: "grep", want: DecisionAllow},
		{tool: "lsp_diagnostics", want: DecisionAllow},
		{tool: "lsp_definition", want: DecisionAllow},
		{tool: "lsp_hover", want: DecisionAllow},
		{tool: "git_status", want: DecisionAllow},
		{tool: "git_diff", want: DecisionAllow},
		{tool: "git_log", want: DecisionAllow},
		{tool: "pdf_parser", want: DecisionAllow},
		{tool: "git_add", want: DecisionDeny},
		{tool: "git_commit", want: DecisionDeny},
	}

	for _, tc := range cases {
		got := p.Decide(tc.tool, nil).Decision
		if got != tc.want {
			t.Fatalf("plan decision for %q = %s, want %s", tc.tool, got, tc.want)
		}
	}
}
