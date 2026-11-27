package messages

import (
	"github.com/gammons/slk/internal/bubbles/entrylist"
	imgpkg "github.com/gammons/slk/internal/image"
)

// PlaceSixel resolves the sixel images of a content pane's visible
// entries for one frame. Both panes call it after entrylist.Layout.
//
// An image whose whole height lies inside the window, and none of whose
// rows blocked reports, becomes a SixelPaint at its window row; the App
// paints those out-of-band after the frame (see SixelPlacements). Every
// other image that is at least partly visible gets its halfblock
// fallback grafted onto its visible rows of frame.Rows, because sixel
// cannot draw part of an image.
//
// offsets and yOffset are the list's (entrylist Offsets and YOffset),
// height is the window's row count, and images(i) returns the sixel
// images of list entry i, nil for an entry that has none. blocked may
// be nil; the channel pane uses it to keep an image off the rows its
// loading and "more below" indicators own.
func PlaceSixel(frame *entrylist.Frame, offsets []int, yOffset, height int, images func(i int) []SixelImage, blocked func(windowRow, rows int) bool, marginSGR func(i int) string) []imgpkg.SixelPaint {
	var paints []imgpkg.SixelPaint
	var fallback map[int]string
	for _, v := range frame.Visible {
		imgs := images(v.Index)
		if len(imgs) == 0 {
			continue
		}
		entryStart := offsets[v.Index]
		for _, sx := range imgs {
			absStart := entryStart + sx.row
			absEnd := absStart + sx.height
			fullyVisible := absStart >= yOffset && absEnd <= yOffset+height
			windowRow := v.Row + (sx.row - v.From)
			if fullyVisible && (blocked == nil || !blocked(windowRow, sx.height)) {
				if windowRow >= 0 && windowRow < len(frame.Rows) {
					margin := ""
					if sx.margin && marginSGR != nil {
						margin = marginSGR(v.Index)
					}
					paints = append(paints, imgpkg.SixelPaint{
						Key:       imgpkg.PlacementKey(sx.key, sx.bytes, sx.width, sx.height),
						Row:       windowRow,
						Col:       sx.col,
						Rows:      sx.height,
						Cols:      sx.width,
						Bytes:     sx.bytes,
						MarginSGR: margin,
					})
				}
				continue
			}
			for k := 0; k < sx.height; k++ {
				abs := absStart + k
				if abs < yOffset || abs >= yOffset+height {
					continue
				}
				row := abs - yOffset
				if row >= 0 && row < len(frame.Rows) && k < len(sx.fallback) {
					if fallback == nil {
						fallback = make(map[int]string)
					}
					// Compose onto a fallback already grafted on this
					// row. Two images share a row, and cutting the
					// original frame again would wipe the first one.
					base := frame.Rows[row]
					if prev, ok := fallback[row]; ok {
						base = prev
					}
					fallback[row] = composeFallbackRow(base, sx.fallback[k], sx.col)
				}
			}
		}
	}
	for row, fb := range fallback {
		if fb != "" {
			frame.Rows[row] = fb
		}
	}
	return paints
}
