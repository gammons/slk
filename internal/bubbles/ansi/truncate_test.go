package ansi

import "testing"

func TestTruncateFitsAndCutsOnClusterBoundaries(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Errorf("fitting string = %q, want hello", got)
	}
	if got := Truncate("hello", 4); got != "hel…" {
		t.Errorf("Truncate(hello, 4) = %q, want hel…", got)
	}
	if got := Truncate("hello", 0); got != "" {
		t.Errorf("budget 0 = %q, want empty", got)
	}
	// The combining mark stays with its base, or the whole cluster goes.
	if got := Truncate("cae\u0301", 3); Cells(got) > 3 {
		t.Errorf("Truncate of e+acute is %d cells (%q), wider than 3", Cells(got), got)
	}
	if got := Truncate("👨‍👩‍👧x", 2); Cells(got) > 2 {
		t.Errorf("Truncate of a family emoji is %d cells (%q), wider than 2", Cells(got), got)
	}
}

func TestCellsUsesTheWiderMeasure(t *testing.T) {
	if Cells("ab") != 2 {
		t.Errorf("Cells(ab) = %d, want 2", Cells("ab"))
	}
	if Cells("") != 0 {
		t.Errorf("Cells(empty) = %d, want 0", Cells(""))
	}
}

func TestTruncateLeftKeepsTheTailWhole(t *testing.T) {
	const fam = "👨‍👩‍👧"
	for _, tc := range []struct {
		s      string
		budget int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 4, "…llo"},
		{"hello", 0, ""},
		{"xxéllo", 5, "…éllo"}, // the accent stays on its letter
		{"xxéllo", 4, "…llo"},
		{"xx" + fam + "ab", 5, "…" + fam + "ab"},
		{"xx" + fam + "ab", 4, "…ab"},
	} {
		if got := TruncateLeft(tc.s, tc.budget); got != tc.want {
			t.Errorf("TruncateLeft(%q, %d) = %q, want %q", tc.s, tc.budget, got, tc.want)
		}
	}
}
