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

