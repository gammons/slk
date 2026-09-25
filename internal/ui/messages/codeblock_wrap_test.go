// Fenced code blocks must retain their whitespace and full-width surface
// background when the message body is wrapped.
package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// renderBody runs the same two steps the message panes run: render the
// markdown, then wrap the result to the content width.
func renderBody(t *testing.T, src string, width int) []string {
	t.Helper()
	out := RenderSlackMarkdownWith(src, RenderSlackMarkdownOpts{Width: width})
	return strings.Split(WordWrap(out, width), "\n")
}

func TestCodeBlock_EveryLineFillsTheFullWidth(t *testing.T) {
	const width = 40
	lines := renderBody(t, "```\nshort\nalso short\n```", width)

	var body []string
	for _, l := range lines {
		if ansi.Strip(l) != "" {
			body = append(body, l)
		}
	}
	if len(body) != 2 {
		t.Fatalf("got %d content lines, want 2:\n%q", len(body), lines)
	}
	for i, l := range body {
		if w := ansi.StringWidth(l); w != width {
			t.Errorf("line %d width = %d, want %d (background must reach the right edge): %q",
				i, w, width, ansi.Strip(l))
		}
	}
}

func TestCodeBlock_PreservesIndentation(t *testing.T) {
	lines := renderBody(t, "```\n    four spaces\nno indent\n```", 40)

	var found bool
	for _, l := range lines {
		if strings.Contains(ansi.Strip(l), "four spaces") {
			found = true
			// One column of style padding, then the source's own four.
			if !strings.HasPrefix(ansi.Strip(l), "     four spaces") {
				t.Errorf("indentation collapsed: %q", ansi.Strip(l))
			}
		}
	}
	if !found {
		t.Fatalf("indented line missing entirely:\n%q", lines)
	}
}

// A long line inside a block is hard-wrapped at the column, not
// re-flowed at word boundaries, and never exceeds the width.
func TestCodeBlock_LongLineHardWrapsWithinWidth(t *testing.T) {
	const width = 24
	lines := renderBody(t, "```\n"+strings.Repeat("x", 60)+"\n```", width)
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("line %d width = %d, exceeds %d: %q", i, w, width, ansi.Strip(l))
		}
	}
}

// Guard the prose side of the wrapLine change: a line that already fits
// is now emitted verbatim, so its internal spacing survives too.
func TestWordWrap_FittingLineIsUntouched(t *testing.T) {
	const in = "  leading and   internal   spaces"
	if got := WordWrap(in, 80); got != in {
		t.Errorf("WordWrap(%q) = %q, want it returned verbatim", in, got)
	}
}

// Lines that exceed the limit still reflow at word boundaries.
func TestWordWrap_OverlongLineStillReflows(t *testing.T) {
	const want = "aaa bbb\nccc ddd"
	if got := WordWrap("aaa bbb ccc ddd", 7); got != want {
		t.Errorf("WordWrap = %q, want %q", got, want)
	}
}

// Width: 0 keeps the pre-existing behaviour for callers that don't know
// their width (previews, tests), rather than failing.
func TestCodeBlock_ZeroWidthFallsBackToOldBehaviour(t *testing.T) {
	out := RenderSlackMarkdownWith("```\nhello\n```", RenderSlackMarkdownOpts{})
	if !strings.Contains(ansi.Strip(out), "hello") {
		t.Errorf("zero-width render lost the content: %q", ansi.Strip(out))
	}
}
