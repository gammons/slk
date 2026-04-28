package emoji

import (
	"testing"
)

func TestWidthASCIIBypass(t *testing.T) {
	resetWidthMap()
	// Even with empty cache, ASCII should work via lipgloss fallback
	if got := Width("hello"); got != 5 {
		t.Errorf("Width(\"hello\") = %d, want 5", got)
	}
	if got := Width(""); got != 0 {
		t.Errorf("Width(\"\") = %d, want 0", got)
	}
	if got := Width("a"); got != 1 {
		t.Errorf("Width(\"a\") = %d, want 1", got)
	}
}

func TestWidthCacheHit(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"❤️": 1,
		"👍":  2,
	})

	if got := Width("❤️"); got != 1 {
		t.Errorf("Width(❤️) = %d, want 1 (cache hit)", got)
	}
	if got := Width("👍"); got != 2 {
		t.Errorf("Width(👍) = %d, want 2 (cache hit)", got)
	}
}

func TestWidthCacheMissFallback(t *testing.T) {
	resetWidthMap()
	// Empty cache; emoji not present → fall back to lipgloss
	got := Width("👍")
	if got < 1 || got > 2 {
		t.Errorf("Width(👍) fallback = %d, want 1 or 2", got)
	}
}

func TestWidthMixedContent(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"❤️": 1,
	})

	// "abc❤️def" → 3 + 1 + 3 = 7
	if got := Width("abc❤️def"); got != 7 {
		t.Errorf("Width(\"abc❤️def\") = %d, want 7", got)
	}
}

func TestWidthStripsANSI(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"👍": 2,
	})

	// Lipgloss-style ANSI-wrapped pill: red foreground + reset.
	// Visible content is "👍5" → emoji (2) + "5" (1) = 3.
	styled := "\x1b[31m👍5\x1b[0m"
	if got := Width(styled); got != 3 {
		t.Errorf("Width(%q) = %d, want 3 (ANSI must be stripped)", styled, got)
	}

	// True pill string with background+foreground+padding.
	// Visible content " 👍 5 " = space + emoji(2) + space + "5" + space = 6.
	pill := "\x1b[38;2;100;100;100m\x1b[48;2;26;46;26m 👍 5 \x1b[0m"
	if got := Width(pill); got != 6 {
		t.Errorf("Width(pill) = %d, want 6 (ANSI must be stripped)", got)
	}

	// Empty cache + ANSI: should fall through to lipgloss.Width on stripped content.
	resetWidthMap()
	if got := Width("\x1b[31mhello\x1b[0m"); got != 5 {
		t.Errorf("Width(ansi 'hello') = %d, want 5", got)
	}
}

func TestIsCalibrated(t *testing.T) {
	resetWidthMap()
	if IsCalibrated() {
		t.Error("IsCalibrated should be false with empty map")
	}

	setWidthMap(map[string]int{"👍": 2})
	if !IsCalibrated() {
		t.Error("IsCalibrated should be true after setWidthMap")
	}
}
