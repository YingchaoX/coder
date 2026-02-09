package tools

import (
	"strings"
	"testing"
)

func TestBuildUnifiedDiffUpdate(t *testing.T) {
	diff, adds, dels := BuildUnifiedDiff("docs/a.md", "line1\nline2\n", "line1\nline3\n")
	if adds != 1 || dels != 1 {
		t.Fatalf("unexpected diff stats: +%d -%d", adds, dels)
	}
	for _, needle := range []string{"--- a/docs/a.md", "+++ b/docs/a.md", "-line2", "+line3"} {
		if !strings.Contains(diff, needle) {
			t.Fatalf("diff missing %q: %q", needle, diff)
		}
	}
}

func TestBuildUnifiedDiffCreate(t *testing.T) {
	diff, adds, dels := BuildUnifiedDiff("new.txt", "", "hello\nworld\n")
	if adds != 2 || dels != 0 {
		t.Fatalf("unexpected create stats: +%d -%d", adds, dels)
	}
	if !strings.Contains(diff, "@@ -0,0 +1,2 @@") {
		t.Fatalf("unexpected create hunk header: %q", diff)
	}
}

func TestTruncateUnifiedDiff(t *testing.T) {
	src := strings.Repeat("x\n", 120)
	out, truncated := TruncateUnifiedDiff(src, 10, 1000)
	if !truncated {
		t.Fatalf("expected truncation")
	}
	if !strings.Contains(out, "... (diff truncated)") {
		t.Fatalf("missing truncation marker: %q", out)
	}
}
