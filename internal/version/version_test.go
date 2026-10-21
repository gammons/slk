package version

import (
	"strings"
	"testing"
)

func TestModalFooter(t *testing.T) {
	got := ModalFooter("v1.2.3")

	// Version is interpolated.
	if !strings.Contains(got, "slk v1.2.3") {
		t.Errorf("footer missing version: %q", got)
	}
	// Attribution wording.
	if !strings.Contains(got, "Made with") {
		t.Errorf("footer missing 'Made with': %q", got)
	}
	if !strings.Contains(got, "\u2764") {
		t.Errorf("footer missing heart glyph: %q", got)
	}
	if !strings.Contains(got, "Grant Ammons") {
		t.Errorf("footer missing author: %q", got)
	}
	// URL is present and OSC-8 wrapped (clickable), with visible label.
	if !strings.Contains(got, "https://grant.dev") {
		t.Errorf("footer missing url: %q", got)
	}
	if !strings.Contains(got, "\x1b]8;;https://grant.dev\x1b\\") {
		t.Errorf("footer url not OSC-8 wrapped: %q", got)
	}
	// URL is parenthesized.
	if !strings.Contains(got, "(") || !strings.Contains(got, ")") {
		t.Errorf("footer url not parenthesized: %q", got)
	}
}

func TestModalFooterUsesGivenVersion(t *testing.T) {
	got := ModalFooter("dev")
	if !strings.Contains(got, "slk dev") {
		t.Errorf("expected 'slk dev' in %q", got)
	}
}

func TestLine(t *testing.T) {
	got := Line("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	want := "slk v1.2.3 (commit abc1234, built 2026-01-02T03:04:05Z)"
	if got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
}

func TestShort(t *testing.T) {
	for _, tc := range []struct{ commit, want string }{
		{"0123456789abcdef0123456789abcdef01234567", "v1.2.3 · 0123456"},
		{"abc1234", "v1.2.3 · abc1234"},
		{"none", "v1.2.3"},
		{"", "v1.2.3"},
	} {
		if got := Short("v1.2.3", tc.commit); got != tc.want {
			t.Errorf("Short(%q) = %q, want %q", tc.commit, got, tc.want)
		}
	}
}
