package messages

import (
	"testing"
)

func TestPlainLines_StripsANSI(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m world"
	got := plainLines(in)
	if len(got) != 1 || got[0].Text != "hello world" {
		t.Fatalf("plainLines unexpected: %#v", got)
	}
}

func TestPlainLines_PreservesNewlines(t *testing.T) {
	in := "a\nbb\nccc"
	got := plainLines(in)
	if len(got) != 3 || got[0].Text != "a" || got[1].Text != "bb" || got[2].Text != "ccc" {
		t.Fatalf("plainLines: %#v", got)
	}
}

func TestPlainLines_WideCharColumnMap(t *testing.T) {
	got := plainLines("x🚀y")
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %d", len(got))
	}
	pl := got[0]
	if displayWidthOfPlain(pl) != 4 {
		t.Fatalf("want width 4, got %d", displayWidthOfPlain(pl))
	}
	// Cols 0..3 should slice to: "x", "🚀" (cols 1+2 same cluster), "y".
	if got := sliceColumns(pl, 0, 1); got != "x" {
		t.Fatalf("col [0,1): want %q got %q", "x", got)
	}
	if got := sliceColumns(pl, 1, 3); got != "🚀" {
		t.Fatalf("col [1,3): want %q got %q", "🚀", got)
	}
	if got := sliceColumns(pl, 3, 4); got != "y" {
		t.Fatalf("col [3,4): want %q got %q", "y", got)
	}
	if got := sliceColumns(pl, 0, 4); got != "x🚀y" {
		t.Fatalf("col [0,4): want %q got %q", "x🚀y", got)
	}
}

func TestPlainLines_ZWJClusterAlignment(t *testing.T) {
	// "x👨‍👩‍👧y" — three clusters: x(W=1), family(W=2), y(W=1) ⇒ 4 cols.
	in := "x\U0001F468\u200D\U0001F469\u200D\U0001F467y"
	got := plainLines(in)
	pl := got[0]
	if displayWidthOfPlain(pl) != 4 {
		t.Fatalf("ZWJ family: want width 4, got %d", displayWidthOfPlain(pl))
	}
	// The family cluster spans cols 1..2; both must produce the SAME
	// starting byte offset and slicing [1:3) yields the entire cluster.
	if got := sliceColumns(pl, 1, 3); got != "\U0001F468\u200D\U0001F469\u200D\U0001F467" {
		t.Fatalf("ZWJ slice: want family bytes; got %q", got)
	}
	if got := sliceColumns(pl, 0, 4); got != in {
		t.Fatalf("full slice: want %q got %q", in, got)
	}
}

func TestPlainLines_SkinToneModifierAlignment(t *testing.T) {
	// "a👍🏽b" — three clusters: a, thumbs-up-with-modifier(W=2), b ⇒ 4 cols.
	in := "a\U0001F44D\U0001F3FDb"
	got := plainLines(in)
	pl := got[0]
	if displayWidthOfPlain(pl) != 4 {
		t.Fatalf("skin-tone: want width 4, got %d", displayWidthOfPlain(pl))
	}
	if got := sliceColumns(pl, 1, 3); got != "\U0001F44D\U0001F3FD" {
		t.Fatalf("skin-tone slice: want modifier-attached emoji; got %q", got)
	}
}

func TestPlainLines_VariationSelectorHeart(t *testing.T) {
	// "a❤️b" — uniseg may treat ❤️ (U+2764 U+FE0F) as one cluster of W=1
	// or W=2 depending on version. Either way the test must hold:
	// slicing the heart's columns must yield the FULL byte sequence
	// (both the base and the variation selector).
	in := "a\u2764\uFE0Fb"
	got := plainLines(in)
	pl := got[0]
	w := displayWidthOfPlain(pl)
	// Width is implementation-dependent but must be >= 3 (a + heart + b).
	if w < 3 {
		t.Fatalf("heart: width must be >=3, got %d", w)
	}
	// The 'a' must slice to "a"; the 'b' must slice to "b" at the last col.
	if got := sliceColumns(pl, 0, 1); got != "a" {
		t.Fatalf("col 0: want %q got %q", "a", got)
	}
	if got := sliceColumns(pl, w-1, w); got != "b" {
		t.Fatalf("col last: want %q got %q", "b", got)
	}
	// Slicing the middle (everything between a and b) yields the heart
	// bytes, complete with variation selector.
	if got := sliceColumns(pl, 1, w-1); got != "\u2764\uFE0F" {
		t.Fatalf("heart middle slice: want %q got %q", "\u2764\uFE0F", got)
	}
}

func TestPlainLines_EmptyAndBlankLines(t *testing.T) {
	got := plainLines("")
	if len(got) != 1 || got[0].Text != "" || displayWidthOfPlain(got[0]) != 0 {
		t.Fatalf("empty input: got %#v", got)
	}
	got = plainLines("\n")
	if len(got) != 2 || got[0].Text != "" || got[1].Text != "" {
		t.Fatalf("\\n input: got %#v", got)
	}
}

func TestBuildCache_LinesPlainAlignsWithLinesNormal(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "x🚀y", Timestamp: "1:01 PM"},
	}, "general")
	m.buildCache(60)
	if len(m.cache) == 0 {
		t.Fatal("buildCache produced no entries")
	}
	for i, e := range m.cache {
		if len(e.linesNormal) != len(e.linesPlain) {
			t.Fatalf("entry %d: linesNormal/linesPlain length mismatch: %d vs %d",
				i, len(e.linesNormal), len(e.linesPlain))
		}
	}
}
