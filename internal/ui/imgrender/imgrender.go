// Package imgrender renders inline image attachments for any UI panel
// (messages pane, thread side panel) using the kitty / sixel /
// halfblock pipelines. Two callers — internal/ui/messages and
// internal/ui/thread — embed a Renderer (added in a follow-up task)
// to share the fetch-tracking and per-block encode logic.
//
// This file holds the standalone types and pure helpers. The Renderer
// itself comes in the next task.
package imgrender

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// ImageContext bundles the dependencies a Renderer needs. Configured
// at startup via Renderer.SetContext (added in the next task). SendMsg
// is optional; when nil, fetches still complete but no re-render is
// triggered when bytes arrive.
type ImageContext struct {
	Protocol    imgpkg.Protocol
	Fetcher     *imgpkg.Fetcher
	KittyRender *imgpkg.KittyRenderer
	CellPixels  image.Point
	// MaxRows caps the height of an inline image in terminal rows.
	MaxRows int
	// MaxCols caps the width of an inline image in terminal columns.
	// 0 disables the column cap (width-only bounded by the message
	// pane).
	MaxCols int
	SendMsg func(tea.Msg)
}

// ImageReadyMsg is dispatched by the prefetcher when an image
// attachment has finished downloading and decoding. The host panel
// uses Channel + TS to identify the affected message; Key clears the
// in-flight bit on the renderer that was tracking the fetch.
type ImageReadyMsg struct {
	Channel string
	TS      string
	Key     string
}

// ImageFailedMsg is dispatched when all auth attempts for an image
// have failed. Carries the cache key only; hosts use it to mark the
// key as permanently failed so RenderBlock won't re-spawn a fetch
// goroutine until the channel is switched.
type ImageFailedMsg struct {
	Key string
}

// ThumbSpec describes a single available thumbnail for an image.
// imgrender keeps its own copy (rather than importing messages.ThumbSpec)
// so messages can later import imgrender without creating an import
// cycle. messages converts at the call site.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}

// Hit is one inline-image hit-rect, expressed in coordinates relative
// to a single rendered block. The host panel translates these to its
// own per-entry / viewport coordinate system.
type Hit struct {
	RowStartInEntry int
	RowEndInEntry   int // exclusive
	ColStart        int
	ColEnd          int // exclusive
	FileID          string
	AttIdx          int
}

// SixelEntry holds the pre-computed sixel bytes for one inline image,
// plus the halfblock fallback used when the image is only partially
// visible (sixel cannot emit a half-image).
type SixelEntry struct {
	Bytes    []byte
	Fallback []string
	Height   int
}

// computeImageTarget chooses (cols, rows) for an inline image render.
// Aspect ratio is preserved. rows is capped at ctx.MaxRows; cols is
// capped at min(availWidth, ctx.MaxCols). Returns image.Point{} when
// the attachment has no usable thumbnail or the cell metrics are zero.
//
// The largest thumb in the slice is used as the source aspect ratio
// (matching the existing messages-pane behavior).
func computeImageTarget(thumbs []ThumbSpec, ctx ImageContext, availWidth int) image.Point {
	if len(thumbs) == 0 || ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		return image.Point{}
	}
	largest := thumbs[len(thumbs)-1]
	if largest.W <= 0 || largest.H <= 0 {
		return image.Point{}
	}
	aspect := float64(largest.W) / float64(largest.H)
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)

	rows := ctx.MaxRows
	if rows <= 0 {
		rows = 20
	}
	maxCols := availWidth
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	cols := int(float64(rows) * aspect / cellRatio)
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
	}
	if rows < 1 {
		rows = 1
	}
	return image.Pt(cols, rows)
}

// buildPlaceholder produces a target.Y-row block with theme-surface
// background and a centered "⏳ Loading <name>..." indicator on the
// middle row. Used while image bytes are being fetched.
func buildPlaceholder(name string, target image.Point) []string {
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	pad := strings.Repeat(" ", target.X)
	emptyRow := bg.Render(pad)

	lines := make([]string, target.Y)
	for i := range lines {
		lines[i] = emptyRow
	}

	label := "⏳ Loading " + name + "..."
	labelW := lipgloss.Width(label)
	if labelW > target.X {
		// Truncate to fit. Use rune-safe slicing via a runes round-trip
		// — image labels are user-controlled file names and can contain
		// multi-byte UTF-8.
		if target.X > 1 {
			runes := []rune(label)
			// Conservatively trim to target.X-1 runes plus an ellipsis;
			// some runes are wide so the result may still be slightly
			// over, in which case we re-trim.
			for len(runes) > 0 {
				candidate := string(runes[:len(runes)-1]) + "…"
				if lipgloss.Width(candidate) <= target.X {
					label = candidate
					labelW = lipgloss.Width(label)
					break
				}
				runes = runes[:len(runes)-1]
			}
			if labelW > target.X {
				return lines
			}
		} else {
			return lines
		}
	}
	leftPad := (target.X - labelW) / 2
	rightPad := target.X - labelW - leftPad
	mid := target.Y / 2
	lines[mid] = bg.Render(strings.Repeat(" ", leftPad)) + bg.Render(label) + bg.Render(strings.Repeat(" ", rightPad))
	return lines
}
