// Package emoji provides utilities for measuring emoji display width
// based on probed terminal behavior, with caching across sessions.
package emoji

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

var (
	widthMu  sync.RWMutex
	widthMap map[string]int // emoji grapheme cluster → width
)

// Width returns the rendered cell width of s.
//
// ANSI escape sequences are stripped before measurement (matching
// lipgloss.Width's behavior). For grapheme clusters present in the
// probed cache, returns the cached width. For pure ASCII or content
// with no emoji, delegates directly to lipgloss.Width(). For mixed
// content, segments by grapheme cluster and sums per-cluster widths
// (cache hit or lipgloss fallback per cluster).
func Width(s string) int {
	// Strip ANSI escape sequences first. Without this, uniseg would
	// segment ESC bytes and parameter bytes as individual graphemes,
	// each measuring as width 1 — wildly inflating the result for any
	// styled string from lipgloss.
	stripped := ansi.Strip(s)

	if !containsNonASCII(stripped) {
		return lipgloss.Width(stripped)
	}

	widthMu.RLock()
	cached := widthMap
	widthMu.RUnlock()

	if len(cached) == 0 {
		return lipgloss.Width(stripped)
	}

	// Segment by grapheme cluster, look up each.
	total := 0
	gr := uniseg.NewGraphemes(stripped)
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
//
// IMPORTANT: The caller must not mutate m after passing it here. Width()
// uses a snapshot pattern (acquire RLock, copy map header, release lock,
// then dereference) which is only safe when installed maps are treated
// as immutable. Replace the map by calling setWidthMap again with a new
// instance rather than mutating the existing map.
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
