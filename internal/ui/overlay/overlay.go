package overlay

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/image"
)

// kittyPlaceholderPrefix is the leading byte sequence of any cell whose
// content begins with image.PlaceholderRune (U+10EEEE encoded as UTF-8:
// 0xF4 0x8E 0xBB 0xAE). We match against the Cell.Content string rather
// than the rune to handle the diacritic-suffixed graphemes kitty's
// renderer emits without taking a dependency on cluster segmentation.
var kittyPlaceholderPrefix = string(image.PlaceholderRune)

// Centered returns the top-left cell where a boxW×boxH box lands when
// DimmedOverlay centers it in a width×height surface: ((w-boxW)/2,
// (h-boxH)/2), clamped at 0. Surfaces that need to know where the
// composited box ended up — the profile dialog places the avatar's
// sixel image on its own box — compute the same spot through this
// instead of re-deriving the formula.
func Centered(width, height, boxW, boxH int) (x, y int) {
	x = (width - boxW) / 2
	y = (height - boxH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// DimmedOverlay composites a modal box on top of a dimmed background.
// The background string is rendered to a Canvas, all cell colors are
// darkened by dimPercent (0.0-1.0), then the modal box is placed centered
// on top by copying its cells.
//
// Cells whose content carries the kitty unicode-placeholder rune are
// blanked: their FG is a 24-bit encoding of an image ID (not a visual
// color), so darkening it would mutate the ID and surface a different
// image — see internal/image/kitty.go. Replacing the placeholder rune
// with a space drops the kitty placement for the duration of the
// overlay; the image data stays uploaded and the next non-overlay
// frame re-emits the placeholder cells, re-creating the placement
// without any image-state plumbing. Issue #18.
func DimmedOverlay(width, height int, background string, box string, dimPercent float64) string {
	// Step 1: Render background to canvas and dim all cells.
	//
	// Wide characters (emoji, CJK) need two pieces of care:
	//
	//   1. Continuation cells (Width==0 at column X+1 after a wide
	//      char at column X) are layout placeholders, not real
	//      content; skip them so we don't redundantly process them.
	//
	//   2. lipgloss's canvas has a quirk where calling SetCell(x, y,
	//      cell) on a wide-char position that ALREADY contains that
	//      wide char destroys the glyph (the canvas seems to drop the
	//      preserved continuation column). CellAt returns a live
	//      pointer into the canvas's internal cell storage, so
	//      mutating cell.Style.{Bg,Fg} in-place propagates without
	//      any SetCell call — and avoids the wide-char drop. Use that
	//      path here; do not call SetCell.
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewLayer(background))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := canvas.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			if strings.HasPrefix(cell.Content, kittyPlaceholderPrefix) {
				// Drop the kitty placement at this cell. Clear the FG
				// (was image-ID-as-RGB, not a real color) and let the
				// normal dim path tint any BG so the region blends
				// into the rest of the dimmed background.
				cell.Content = " "
				cell.Width = 1
				cell.Style.Fg = nil
			}
			if cell.Style.Bg != nil {
				cell.Style.Bg = lipgloss.Darken(cell.Style.Bg, dimPercent)
			}
			if cell.Style.Fg != nil {
				cell.Style.Fg = lipgloss.Darken(cell.Style.Fg, dimPercent)
			}
			// Intentionally NO canvas.SetCell here — see comment above.
		}
	}

	// Step 2: Render dimmed canvas, create output canvas
	dimmedStr := canvas.Render()
	outCanvas := lipgloss.NewCanvas(width, height)
	outCanvas.Compose(lipgloss.NewLayer(dimmedStr))

	// Step 3: Render modal to its own canvas, compute centered position
	modalW := lipgloss.Width(box)
	modalH := lipgloss.Height(box)
	startX, startY := Centered(width, height, modalW, modalH)

	modalCanvas := lipgloss.NewCanvas(modalW, modalH)
	modalCanvas.Compose(lipgloss.NewLayer(box))

	// Step 4: Copy modal cells onto output canvas.
	//
	// Wide characters (emoji, CJK) occupy two grid columns. lipgloss's
	// canvas reports the wide cell at column X with Width=2 and a
	// CONTINUATION cell at column X+1 with Content="" Width=0. The
	// continuation cell is a layout placeholder, not real content;
	// calling SetCell on it would overwrite the second column of the
	// wide glyph with empty content, erasing the emoji visually.
	// Skip Width==0 cells so the wide character that already landed
	// at column X stays intact across both of its columns.
	for my := 0; my < modalH; my++ {
		for mx := 0; mx < modalW; mx++ {
			cell := modalCanvas.CellAt(mx, my)
			if cell == nil || cell.Width == 0 {
				continue
			}
			outCanvas.SetCell(startX+mx, startY+my, cell)
		}
	}

	output := outCanvas.Render()

	// Step 5: Patch kitty-placement rows back to their original bytes.
	//
	// The canvas's cell-by-cell store/emit path bundles per-cell style
	// (e.g. Bold) into the SGR it writes before each rune, mutating the
	// pristine "\x1b[38;2;0;0;Bm" foreground that buildPlaceholderLines
	// emits into something like "\x1b[38;2;0;0;B;1m". The RGB triple
	// (which carries the kitty image ID) survives, but in practice
	// terminals will not detect the placeholder as a kitty image when
	// the SGR carries extra attributes — the cell renders blank.
	//
	// Fix: for any modal row that contains a kitty placeholder rune,
	// splice the ORIGINAL box row (exact byte-for-byte SGR + rune +
	// diacritic sequence) into the corresponding output row at column
	// startX. Use ansi-aware Truncate / TruncateLeft so the splice is
	// width-correct and doesn't break escape sequences in the
	// surrounding (dimmed background) content.
	hasPlacement := false
	boxLines := strings.Split(box, "\n")
	for _, ln := range boxLines {
		if strings.Contains(ln, kittyPlaceholderPrefix) {
			hasPlacement = true
			break
		}
	}
	if !hasPlacement {
		return output
	}

	outputLines := strings.Split(output, "\n")
	for my, boxLine := range boxLines {
		if !strings.Contains(boxLine, kittyPlaceholderPrefix) {
			continue
		}
		outRow := startY + my
		if outRow < 0 || outRow >= len(outputLines) {
			continue
		}
		original := outputLines[outRow]
		prefix := ansi.Truncate(original, startX, "")
		suffix := ansi.TruncateLeft(original, startX+modalW, "")
		// Reset between prefix and modal content so the dimmed
		// background's SGR doesn't leak into the placement; the
		// placement string itself begins with its own FG SGR. Same
		// for the trailing edge.
		outputLines[outRow] = prefix + "\x1b[m" + boxLine + "\x1b[m" + suffix
	}
	return strings.Join(outputLines, "\n")
}
