package emoji

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

var (
	widthMu  sync.RWMutex
	widthMap map[string]int // emoji grapheme cluster → width
)

// Width returns the rendered cell width of s.
//
// For grapheme clusters present in the probed cache, returns the cached
// width. For pure ASCII or content with no emoji, delegates directly to
// lipgloss.Width(). For mixed content, segments by grapheme cluster and
// sums per-cluster widths (cache hit or lipgloss fallback per cluster).
func Width(s string) int {
	if !containsNonASCII(s) {
		return lipgloss.Width(s)
	}

	widthMu.RLock()
	cached := widthMap
	widthMu.RUnlock()

	if len(cached) == 0 {
		return lipgloss.Width(s)
	}

	// Segment by grapheme cluster, look up each.
	total := 0
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		cluster := gr.Str()
		if w, ok := cached[cluster]; ok {
			total += w
		} else {
			total += lipgloss.Width(cluster)
		}
	}
	return total
}

// IsCalibrated reports whether the probe succeeded and we have a
// terminal-specific width map loaded.
func IsCalibrated() bool {
	widthMu.RLock()
	defer widthMu.RUnlock()
	return len(widthMap) > 0
}

// setWidthMap installs a new width map. Used by Init() and tests.
func setWidthMap(m map[string]int) {
	widthMu.Lock()
	defer widthMu.Unlock()
	widthMap = m
}

// resetWidthMap clears the width map. Used by tests.
func resetWidthMap() {
	widthMu.Lock()
	defer widthMu.Unlock()
	widthMap = nil
}

// containsNonASCII returns true if s has any byte ≥ 0x80.
func containsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}
