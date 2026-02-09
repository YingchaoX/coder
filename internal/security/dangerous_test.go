package security

import "testing"

func TestAnalyzeCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantAsk bool
	}{
		{
			name:    "safe",
			cmd:     "ls -la",
			wantAsk: false,
		},
		{
			name:    "dangerous rm",
			cmd:     "rm -rf build",
			wantAsk: true,
		},
		{
			name:    "parse failure",
			cmd:     `echo "abc`,
			wantAsk: true,
		},
		{
			name:    "command substitution",
			cmd:     "echo $(cat secret.txt)",
			wantAsk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnalyzeCommand(tt.cmd)
			if got.RequireApproval != tt.wantAsk {
				t.Fatalf("AnalyzeCommand(%q).RequireApproval = %v, want %v", tt.cmd, got.RequireApproval, tt.wantAsk)
			}
		})
	}
}
