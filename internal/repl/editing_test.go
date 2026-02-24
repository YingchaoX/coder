package repl

import "testing"

func TestDeleteLastRuneAndWidth(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		want_out  string
		want_wmin int
		want_wmax int
	}{
		{
			name:      "empty",
			in:        "",
			want_out:  "",
			want_wmin: 0,
			want_wmax: 0,
		},
		{
			name:      "ascii",
			in:        "abc",
			want_out:  "ab",
			want_wmin: 1,
			want_wmax: 1,
		},
		{
			name:      "chinese_wide",
			in:        "你",
			want_out:  "",
			want_wmin: 2,
			want_wmax: 2,
		},
		{
			name:      "mixed_last_ascii",
			in:        "你a",
			want_out:  "你",
			want_wmin: 1,
			want_wmax: 1,
		},
		{
			name:      "emoji",
			in:        "🙂",
			want_out:  "",
			want_wmin: 1,
			want_wmax: 2, // depends on terminal; allow 1-2
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got_out, got_w := deleteLastRuneAndWidth(tc.in)
			if got_out != tc.want_out {
				t.Fatalf("out mismatch: got=%q want=%q", got_out, tc.want_out)
			}
			if got_w < tc.want_wmin || got_w > tc.want_wmax {
				t.Fatalf("width out of range: got=%d want=[%d,%d]", got_w, tc.want_wmin, tc.want_wmax)
			}
		})
	}
}

func TestHistoryNavigator_Empty(t *testing.T) {
	nav := newHistoryNavigator(nil)
	if got, ok := nav.Prev(); ok || got != "" {
		t.Fatalf("empty Prev = (%q,%v), want (\"\",false)", got, ok)
	}
	if got, ok := nav.Next(); ok || got != "" {
		t.Fatalf("empty Next = (%q,%v), want (\"\",false)", got, ok)
	}
}

func TestHistoryNavigator_BasicWalk(t *testing.T) {
	nav := newHistoryNavigator([]string{"one", "two", "three"})

	// Start at fresh input, first Up -> last entry.
	if got, ok := nav.Prev(); !ok || got != "three" {
		t.Fatalf("first Prev = (%q,%v), want (\"three\",true)", got, ok)
	}
	// Second Up -> second entry.
	if got, ok := nav.Prev(); !ok || got != "two" {
		t.Fatalf("second Prev = (%q,%v), want (\"two\",true)", got, ok)
	}
	// Third Up -> first entry.
	if got, ok := nav.Prev(); !ok || got != "one" {
		t.Fatalf("third Prev = (%q,%v), want (\"one\",true)", got, ok)
	}
	// Further Up stays on first entry.
	if got, ok := nav.Prev(); !ok || got != "one" {
		t.Fatalf("fourth Prev = (%q,%v), want (\"one\",true)", got, ok)
	}

	// Down from first -> second.
	if got, ok := nav.Next(); !ok || got != "two" {
		t.Fatalf("first Next = (%q,%v), want (\"two\",true)", got, ok)
	}
	// Down from second -> third.
	if got, ok := nav.Next(); !ok || got != "three" {
		t.Fatalf("second Next = (%q,%v), want (\"three\",true)", got, ok)
	}
	// Down from third -> empty (fresh input).
	if got, ok := nav.Next(); !ok || got != "" {
		t.Fatalf("third Next = (%q,%v), want (\"\",true)", got, ok)
	}
	// Further Down keeps empty.
	if got, ok := nav.Next(); !ok || got != "" {
		t.Fatalf("fourth Next = (%q,%v), want (\"\",true)", got, ok)
	}
}

func TestAppendPrintableToPaste(t *testing.T) {
	body := "line1\nline2\n"

	// Accept printable ASCII.
	nb, ok := appendPrintableToPaste(body, '3')
	if !ok {
		t.Fatalf("appendPrintableToPaste should accept '3'")
	}
	if nb != "line1\nline2\n3" {
		t.Fatalf("appendPrintableToPaste wrong result: %q", nb)
	}

	// Reject non-printable (e.g. newline).
	nb2, ok := appendPrintableToPaste(nb, '\n')
	if ok {
		t.Fatalf("appendPrintableToPaste should reject '\\n'")
	}
	if nb2 != nb {
		t.Fatalf("appendPrintableToPaste modified body for non-printable: %q", nb2)
	}
}

func TestHistoryDisplayString_SingleLine(t *testing.T) {
	if got := historyDisplayString("echo 1"); got != "echo 1" {
		t.Fatalf("historyDisplayString(single line) = %q, want %q", got, "echo 1")
	}
}

func TestHistoryDisplayString_MultiLine(t *testing.T) {
	body := "line1\nline2\nline3\n"
	got := historyDisplayString(body)
	if got != "[copy 3 lines]" {
		t.Fatalf("historyDisplayString(multi line) = %q, want %q", got, "[copy 3 lines]")
	}
}
