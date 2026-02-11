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
