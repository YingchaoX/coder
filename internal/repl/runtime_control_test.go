package repl

import (
	"testing"

	"coder/internal/bootstrap"
)

func TestParseApprovalDecision(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		allowAlways bool
		want        bootstrap.ApprovalDecision
		ok          bool
	}{
		{name: "default deny", input: "", allowAlways: true, want: bootstrap.ApprovalDecisionDeny, ok: true},
		{name: "allow yes", input: "yes", allowAlways: false, want: bootstrap.ApprovalDecisionAllowOnce, ok: true},
		{name: "allow short yes", input: "y", allowAlways: false, want: bootstrap.ApprovalDecisionAllowOnce, ok: true},
		{name: "deny no", input: "n", allowAlways: true, want: bootstrap.ApprovalDecisionDeny, ok: true},
		{name: "always enabled", input: "always", allowAlways: true, want: bootstrap.ApprovalDecisionAllowAlways, ok: true},
		{name: "always disabled", input: "always", allowAlways: false, want: bootstrap.ApprovalDecisionDeny, ok: false},
		{name: "invalid", input: "later", allowAlways: true, want: bootstrap.ApprovalDecisionDeny, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseApprovalDecision(tc.input, tc.allowAlways)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("parseApprovalDecision(%q, allowAlways=%v) = (%v, %v), want (%v, %v)", tc.input, tc.allowAlways, got, ok, tc.want, tc.ok)
			}
		})
	}
}
