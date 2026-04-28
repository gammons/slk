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
