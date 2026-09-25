package reactionpicker

import (
	goimage "image"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	slkemoji "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
)

// TestPickerRendersFireGlyph guards the composition-safe emoji
// fallback fix at /home/grant/local_code/slk/docs/superpowers/specs/2026-05-24-emoji-shortcode-fallback-design.md.
// Opens the picker, types "fire", renders the frame, and asserts the
// 🔥 glyph (U+1F525) is present in the output — confirming the
// picker did NOT fall back to literal ":fire:" text for a simple
// single-codepoint emoji.
//
// Regression target: the picker's previous len(runes)==1 rule also
// reported true for :fire: so this test would have passed before too.
// What it really guards is the broader ShouldRenderUnicode wiring:
// if a future refactor accidentally hides single-codepoint emoji
// behind the shortcode fallback, this test fails.
func TestPickerRendersFireGlyph(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]core.EmojiEntry{})
	m.Open("Cxxx", "1.0", nil)
	for _, ch := range "fire" {
		m.HandleKey(string(ch))
	}
	view := m.View(120)
	if !strings.Contains(view, "\U0001F525") {
		t.Errorf("rendered picker does NOT contain 🔥 glyph for :fire: search")
		lines := strings.Split(view, "\n")
		for i, ln := range lines {
			if i > 20 {
				break
			}
			t.Logf("line %d: %q", i, ln)
		}
	}
}

// TestViewOverlayPreservesFireGlyph guards that going through
// ViewOverlay (which uses lipgloss.NewCanvas cell-by-cell compositing)
// preserves the wide emoji glyph. Caught a real bug where the canvas
// overlay path was dropping wide-character cells.
func TestViewOverlayPreservesFireGlyph(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]core.EmojiEntry{})
	m.Open("Cxxx", "1.0", nil)
	for _, ch := range "fire" {
		m.HandleKey(string(ch))
	}
	background := strings.Repeat(" \n", 30)
	overlaid := m.ViewOverlay(120, 30, background)
	if !strings.Contains(overlaid, "\U0001F525") {
		t.Errorf("ViewOverlay output does NOT contain 🔥 glyph (canvas/overlay compositing stripped it)")
		// Compare against View() output to confirm it's the overlay path
		view := m.View(120)
		hasFireInView := strings.Contains(view, "\U0001F525")
		t.Logf("View() contains 🔥: %v (if true, bug is in ViewOverlay/DimmedOverlay)", hasFireInView)
	}
}

// TestPickerFallsBackForVS16Emoji guards that VS16-anchored emoji
// (single base + U+FE0F) now fall back to :name: text rather than
// rendering the Unicode glyph. Many terminal+font combos render
// VS16 emoji from legacy blocks at a different visual width than
// lipgloss reports, breaking border alignment; the shortcode form
// is layout-safe.
func TestPickerFallsBackForVS16Emoji(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]core.EmojiEntry{})
	m.Open("Cxxx", "1.0", nil)
	for _, ch := range "heart" {
		m.HandleKey(string(ch))
	}
	view := m.View(120)
	if !strings.Contains(view, ":heart:") {
		t.Errorf("rendered picker does NOT contain literal :heart: text for VS16-anchored emoji")
	}
	if strings.Contains(view, "\u2764\uFE0F") {
		t.Errorf("rendered picker contains ❤️ Unicode glyph; expected :heart: text fallback")
	}
}

// TestPickerFallsBackForZWJSequence guards the OTHER side of the
// fix: composition-fragile emoji (ZWJ sequences, here the pride
// flag) must fall back to the literal :name: text, NOT render as
// the broken-glyph Unicode sequence.
func TestPickerFallsBackForZWJSequence(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]core.EmojiEntry{})
	m.Open("Cxxx", "1.0", nil)
	for _, ch := range "rainbow-f" {
		m.HandleKey(string(ch))
	}
	view := m.View(120)
	if !strings.Contains(view, ":rainbow-flag:") {
		t.Errorf("rendered picker does NOT contain literal :rainbow-flag: for ZWJ-sequence emoji")
	}
	// And it must NOT contain the actual ZWJ pride flag glyph.
	if strings.Contains(view, "\U0001F3F3\uFE0F\u200D\U0001F308") {
		t.Errorf("rendered picker contains 🏳️‍🌈 Unicode glyph; expected :rainbow_flag: text fallback")
	}
}

// TestViewOverlayPreservesKittyPlacementSGR guards that the kitty
// image-ID encoding (a 24-bit RGB foreground SGR escape like
// \x1b[38;2;0;0;42m for image ID 42) survives the overlay
// compositing path. Lipgloss's cell-by-cell canvas storage was
// observed to palette-quantize truecolor foregrounds on modal
// cells, mutating the image-ID bytes and rendering blank cells in
// the reaction picker. See internal/ui/overlay/overlay.go.
//
// The test seeds a prerendered kitty placement string for
// :thumbsup: with image ID 42, drives the picker to a single
// matching entry, and asserts the placement's SGR bytes are
// preserved through both View() (direct) and ViewOverlay()
// (canvas-composited).
func TestViewOverlayPreservesKittyPlacementSGR(t *testing.T) {
	slkemoji.SetImageMode(true, 2)
	t.Cleanup(func() { slkemoji.SetImageMode(false, 2) })

	// Build a known kitty placement string: SGR with image ID 42
	// encoded as truecolor (0, 0, 42), two placeholder cells with
	// row/col diacritics, then SGR reset. Mirrors the byte layout
	// produced by internal/image/kitty.go:buildPlaceholderLines so
	// the test exercises the same characters the real warm path
	// would emit.
	const wantSGR = "\x1b[38;2;0;0;42m"
	placement := wantSGR + "\U0010EEEE\u0305\u0305\U0010EEEE\u0305\u0306\x1b[39m"

	thumbURL := slkemoji.CDNBaseURL + "1f44d.png"
	ff := newFakePickerFetcher()
	ff.setPrerendered(slkemoji.EmojiCacheKey(thumbURL), goimage.Pt(2, 1), imgpkg.Render{
		Cells: goimage.Pt(2, 1),
		Lines: []string{placement},
	})

	m := New()
	m.Open("C123", "1234.5678", nil)
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: slkemoji.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil,
	})

	for _, ch := range "thumbsup" {
		m.HandleKey(string(ch))
	}

	// Sanity: View() (no canvas compositing) preserves the SGR
	// byte-for-byte. If this fails, the warm path is broken before
	// the overlay even runs, and any overlay assertion is moot.
	view := m.View(120)
	if !strings.Contains(view, wantSGR) {
		t.Fatalf("picker View() does not contain image-ID SGR %q; warm path is broken before overlay\nview=%q", wantSGR, view)
	}

	// The regression: ViewOverlay() goes through overlay.DimmedOverlay,
	// which composites via lipgloss canvas. The canvas's cell-by-cell
	// store/emit path was observed to bundle extra attributes (e.g.
	// ";1" bold) into the cell SGR, mutating the pristine image-ID
	// foreground. The fix in overlay.go re-splices the original modal
	// row for any row containing kitty placeholder runes.
	background := strings.Repeat(strings.Repeat(" ", 120)+"\n", 30)
	overlaid := m.ViewOverlay(120, 30, background)
	if !strings.Contains(overlaid, "\U0010EEEE") {
		t.Errorf("ViewOverlay output missing kitty placeholder rune \\U0010EEEE")
	}
	if !strings.Contains(overlaid, wantSGR) {
		t.Errorf("ViewOverlay output does NOT contain image-ID SGR %q (overlay compositing corrupted it)", wantSGR)
		// Help diagnose: dump any 38;2 sequences present to show what
		// the canvas mutated to.
		idx := 0
		for {
			j := strings.Index(overlaid[idx:], "\x1b[38;2;")
			if j < 0 {
				break
			}
			start := idx + j
			end := start + 1
			for end < len(overlaid) && overlaid[end] != 'm' {
				end++
			}
			if end < len(overlaid) {
				end++
			}
			t.Logf("overlay 38;2 SGR found: %q", overlaid[start:end])
			idx = end
		}
	}
}
