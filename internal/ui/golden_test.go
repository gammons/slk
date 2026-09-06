package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// updateGolden re-blesses every golden file this run touches.
//
//	go test ./internal/ui -run TestGolden -update
var updateGolden = flag.Bool("update", false, "rewrite golden files from current output")

// goldenDir is where .ansi goldens live, relative to this package.
const goldenDir = "testdata/golden"

// compareGolden asserts got matches testdata/golden/<name>.ansi byte
// for byte, or rewrites it under -update.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name+".ansi")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		t.Logf("updated %s (%d bytes, %d lines)", path, len(got), strings.Count(got, "\n")+1)
		return
	}

	wantB, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing or unreadable: %v\n"+
			"bless it with: go test ./internal/ui -run TestGolden -update", path, err)
	}

	if d := styleAwareDiff(string(wantB), got); d != "" {
		t.Errorf("golden %s: %s", path, d)
	}
}

// styleAwareDiff describes how got differs from want, or returns "" when
// they are identical.
//
// The output is deliberately two-tier. Goldens store raw ANSI, so a naive
// diff of a styling-only regression is an unreadable wall of escape
// sequences and gets blessed without being read. When the stripped text
// matches, we say so explicitly and point at the offending byte instead.
//
// Split out from compareGolden so both tiers are directly testable
// without needing to observe a *testing.T failing.
func styleAwareDiff(want, got string) string {
	if want == got {
		return ""
	}
	if d := firstLineDiff(stripANSI(want), stripANSI(got)); d != "" {
		return "rendered text differs\n" + d
	}
	return fmt.Sprintf("content identical, STYLING differs\n%s\n"+
		"A style regression (selection highlight, unread bold, muted dim) "+
		"is the usual cause. Do not bless this without reading it.",
		firstByteDiff(want, got))
}

// firstLineDiff returns a human-readable description of the first
// differing line, or "" when the inputs are equal.
func firstLineDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := min(len(wl), len(gl))
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			return fmt.Sprintf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, wl[i], gl[i])
		}
	}
	if len(wl) != len(gl) {
		return fmt.Sprintf("line count differs: want %d lines, got %d", len(wl), len(gl))
	}
	return ""
}

// firstByteDiff locates the first differing byte and prints a quoted
// window around it in both inputs.
func firstByteDiff(want, got string) string {
	n := min(len(want), len(got))
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			lo := max(i-40, 0)
			hiW := min(i+40, len(want))
			hiG := min(i+40, len(got))
			return fmt.Sprintf("first differing byte %d:\n  want: %q\n  got:  %q",
				i, want[lo:hiW], got[lo:hiG])
		}
	}
	return fmt.Sprintf("byte length differs: want %d, got %d", len(want), len(got))
}

// stripANSI removes SGR/OSC sequences so a text-level diff is readable.
func stripANSI(s string) string { return ansi.Strip(s) }

func TestFirstLineDiff_ReportsFirstDifferingLine(t *testing.T) {
	want := "alpha\nbravo\ncharlie"
	got := "alpha\nBRAVO\ncharlie"
	out := firstLineDiff(want, got)
	if !strings.Contains(out, "line 2") {
		t.Errorf("expected line 2 in %q", out)
	}
	if !strings.Contains(out, "bravo") || !strings.Contains(out, "BRAVO") {
		t.Errorf("expected both values in %q", out)
	}
}

func TestFirstLineDiff_ReportsLineCountMismatch(t *testing.T) {
	out := firstLineDiff("a\nb", "a\nb\nc")
	if !strings.Contains(out, "line count") {
		t.Errorf("expected line-count message in %q", out)
	}
}

func TestFirstLineDiff_EmptyWhenEqual(t *testing.T) {
	if out := firstLineDiff("same", "same"); out != "" {
		t.Errorf("expected empty diff, got %q", out)
	}
}

// TestFirstByteDiff_ReportsOffsetAndHex pins the offset at 9, not 8.
// In "plain \x1b[31m..." the bytes are p,l,a,i,n,space,ESC,[,3,1 — index
// 8 is '3' in both inputs; the first byte that actually differs is the
// '1' vs '2' at index 9. The task brief said 8; it was off by one.
func TestFirstByteDiff_ReportsOffsetAndHex(t *testing.T) {
	want := "plain \x1b[31mred\x1b[0m"
	got := "plain \x1b[32mred\x1b[0m"
	out := firstByteDiff(want, got)
	if !strings.Contains(out, "byte 9") {
		t.Errorf("expected byte offset 9 in %q", out)
	}
	if !strings.Contains(out, `\x1b[31m`) || !strings.Contains(out, `\x1b[32m`) {
		t.Errorf("expected quoted escape windows for both inputs in %q", out)
	}
}

func TestFirstByteDiff_ReportsLengthMismatch(t *testing.T) {
	out := firstByteDiff("abc", "abcdef")
	if !strings.Contains(out, "byte length differs") {
		t.Errorf("expected length message in %q", out)
	}
	if !strings.Contains(out, "want 3") || !strings.Contains(out, "got 6") {
		t.Errorf("expected both lengths in %q", out)
	}
}

// TestFirstByteDiff_WindowsAreBounded guards the slice arithmetic: a
// difference near either end must not panic and must stay inside both
// inputs.
func TestFirstByteDiff_WindowsAreBounded(t *testing.T) {
	long := strings.Repeat("x", 200)
	if out := firstByteDiff("a"+long, "b"+long); !strings.Contains(out, "byte 0") {
		t.Errorf("expected byte 0 in %q", out)
	}
	if out := firstByteDiff(long+"a", long+"b"); !strings.Contains(out, "byte 200") {
		t.Errorf("expected byte 200 in %q", out)
	}
}

func TestStyleAwareDiff_EmptyWhenEqual(t *testing.T) {
	s := "hello \x1b[1mworld\x1b[0m"
	if out := styleAwareDiff(s, s); out != "" {
		t.Errorf("expected empty diff, got %q", out)
	}
}

// TestStyleAwareDiff_TextDifferenceReportsLineDiff is tier one: the
// visible characters changed, so the reviewer gets a readable text diff
// and no talk of styling.
func TestStyleAwareDiff_TextDifferenceReportsLineDiff(t *testing.T) {
	want := "\x1b[1m#general\x1b[0m\nhello"
	got := "\x1b[1m#random\x1b[0m\nhello"
	out := styleAwareDiff(want, got)
	if !strings.Contains(out, "rendered text differs") {
		t.Errorf("expected text-differs header in %q", out)
	}
	if !strings.Contains(out, "line 1") {
		t.Errorf("expected line 1 in %q", out)
	}
	if strings.Contains(out, "STYLING") {
		t.Errorf("text diff should not mention styling: %q", out)
	}
	// The readable tier must be free of raw escapes.
	if strings.Contains(out, "\x1b") {
		t.Errorf("text diff leaked a raw escape byte: %q", out)
	}
}

// TestStyleAwareDiff_StylingOnlyIsCalledOut is tier two, the branch this
// whole helper exists for: identical characters, different attributes.
func TestStyleAwareDiff_StylingOnlyIsCalledOut(t *testing.T) {
	want := "\x1b[31m#general\x1b[0m"
	got := "\x1b[32m#general\x1b[0m"
	out := styleAwareDiff(want, got)
	if !strings.Contains(out, "content identical, STYLING differs") {
		t.Errorf("expected styling callout in %q", out)
	}
	if !strings.Contains(out, "first differing byte") {
		t.Errorf("expected a byte pointer in %q", out)
	}
	if strings.Contains(out, "rendered text differs") {
		t.Errorf("styling diff should not claim text differs: %q", out)
	}
}

// TestStyleAwareDiff_TrailingStyleOnlyDifference covers a styling change
// that adds bytes rather than substituting them: stripped text is still
// equal, so it must land in the styling tier and not be misreported as a
// line-count mismatch.
func TestStyleAwareDiff_TrailingStyleOnlyDifference(t *testing.T) {
	want := "#general"
	got := "\x1b[1m#general\x1b[0m"
	out := styleAwareDiff(want, got)
	if !strings.Contains(out, "content identical, STYLING differs") {
		t.Errorf("expected styling callout in %q", out)
	}
}

// TestCompareGolden_UpdateThenCompareRoundTrips exercises both modes of
// compareGolden against a scratch working directory, so no artifact is
// left in the repo's testdata.
func TestCompareGolden_UpdateThenCompareRoundTrips(t *testing.T) {
	t.Chdir(t.TempDir())

	content := "\x1b[1m#general\x1b[0m\nline two\n"

	defer func(prev bool) { *updateGolden = prev }(*updateGolden)

	*updateGolden = true
	compareGolden(t, "roundtrip", content)

	path := filepath.Join(goldenDir, "roundtrip.ansi")
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden written by -update: %v", err)
	}
	if string(onDisk) != content {
		t.Errorf("golden not written byte-for-byte:\n  want %q\n  got  %q", content, string(onDisk))
	}

	// Compare mode against the freshly blessed file must be silent.
	*updateGolden = false
	compareGolden(t, "roundtrip", content)
	if t.Failed() {
		t.Fatal("compareGolden reported a mismatch against its own -update output")
	}
}

// TestCompareGolden_UpdateCreatesMissingDir pins the MkdirAll: testdata/
// does not exist in a fresh tree, and a bless run must create it rather
// than fail.
func TestCompareGolden_UpdateCreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := os.Stat(filepath.Join(dir, goldenDir)); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist, stat err = %v", goldenDir, err)
	}

	defer func(prev bool) { *updateGolden = prev }(*updateGolden)
	*updateGolden = true
	compareGolden(t, "fresh", "content\n")

	if _, err := os.Stat(filepath.Join(dir, goldenDir, "fresh.ansi")); err != nil {
		t.Fatalf("expected golden created under a missing dir: %v", err)
	}
}
