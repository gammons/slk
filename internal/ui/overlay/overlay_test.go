package overlay_test

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/overlay"
)

// TestDimmedOverlayDropsKittyPlaceholders guards issue #18.
//
// Kitty's unicode-placeholder protocol encodes an image's ID in each
// placeholder cell's SGR foreground color (R = byte 2, G = byte 1,
// B = byte 0 of the 24-bit ID). The original bug: DimmedOverlay's
// per-cell FG darken mutated those IDs, surfacing whatever image
// happened to own the darkened ID — users saw avatars and image
// previews "go wonky" while any modal was open.
//
// The chosen remediation is to drop the kitty placement entirely for
// the duration of the overlay by blanking placeholder cells. The
// image data stays uploaded in the terminal; the next non-overlay
// frame re-emits the placeholder cells and the placement re-appears
// with no image-state plumbing. This test asserts that contract:
// after DimmedOverlay, the rendered output contains zero placeholder
// runes and zero "image ID-as-RGB" SGR escapes from the synthetic
// background we provided.
func TestDimmedOverlayDropsKittyPlaceholders(t *testing.T) {
	const id uint32 = 42
	r := byte((id >> 16) & 0xFF)
	g := byte((id >> 8) & 0xFF)
	b := byte(id & 0xFF)
	idFG := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)

	placeholders := idFG +
		string(image.PlaceholderRune) +
		string(image.PlaceholderRune) +
		string(image.PlaceholderRune) +
		"\x1b[39m"

	const width, height = 12, 3
	bg := strings.Join([]string{
		strings.Repeat("a", width),
		placeholders + strings.Repeat(" ", width-3),
		strings.Repeat("b", width),
	}, "\n")

	// Narrow box so part of the placeholder row survives to the right
	// of the modal — that's the region the bug used to corrupt.
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Width(2).
		Height(1).
		Render("X")

	out := overlay.DimmedOverlay(width, height, bg, box, 0.5)

	if strings.ContainsRune(out, image.PlaceholderRune) {
		t.Fatalf("dim left kitty placeholder rune in output (image would render through dim):\n%q", out)
	}
	if strings.Contains(out, idFG) {
		t.Fatalf("dim left raw image-ID FG escape %q in output:\n%q", idFG, out)
	}
}

// TestDimmedOverlayKeepsBoxKittyPlaceholders is the sibling assertion
// TestDimmedOverlayDropsKittyPlaceholders' own doc comment calls out as
// untested: a placeholder rune that sits INSIDE the modal box itself
// (as the user-profile dialog's avatar does, DimmedOverlay's step 5
// splices the box's own bytes back onto the output row verbatim after
// the dim-and-copy pass corrupts their SGR-bundled form (see the "Step
// 5" comment in overlay.go). This test pins that the placeholder rune
// and its original image-ID-as-RGB foreground survive byte-for-byte on
// a box row, in contrast to the background case above where both are
// dropped.
func TestDimmedOverlayKeepsBoxKittyPlaceholders(t *testing.T) {
	const id uint32 = 7
	r := byte((id >> 16) & 0xFF)
	g := byte((id >> 8) & 0xFF)
	b := byte(id & 0xFF)
	idFG := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	placeholder := idFG + string(image.PlaceholderRune) + "\x1b[39m"

	const width, height = 20, 6
	bg := strings.Join([]string{
		strings.Repeat("a", width),
		strings.Repeat("a", width),
		strings.Repeat("a", width),
		strings.Repeat("a", width),
		strings.Repeat("a", width),
		strings.Repeat("a", width),
	}, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Width(4).
		Render(placeholder)

	out := overlay.DimmedOverlay(width, height, bg, box, 0.5)

	if !strings.Contains(out, placeholder) {
		t.Fatalf("box's kitty placeholder was not preserved byte-for-byte in the composited output:\n%q\nwant to contain:\n%q", out, placeholder)
	}
}
