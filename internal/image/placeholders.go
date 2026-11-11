package image

import (
	"fmt"
	"image/color"
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// This file is the KittyEmulated half of the kitty renderer: it does in
// slk what a terminal with unicode placeholders does itself. The UI
// keeps emitting placeholder cells exactly as for kitty and Ghostty;
// the App passes each finished frame through ResolvePlaceholders, which
// finds the cells, blanks them, and reports where each image sits, and
// EmulatedPlacements turns that into direct placements for the frame
// painter (FrameOutput).

// placeholderUTF8 is PlaceholderRune encoded as UTF-8.
var placeholderUTF8 = string(PlaceholderRune)

// diacriticIndex inverts kittyDiacritics: rune to row/column index.
var diacriticIndex = func() map[rune]int {
	m := make(map[rune]int, len(kittyDiacritics))
	for i, r := range kittyDiacritics {
		m[r] = i
	}
	return m
}()

// PlaceholderRect is one visible rectangle of a kitty image in a frame.
type PlaceholderRect struct {
	// ID is the kitty image ID, decoded from the cells' foreground.
	ID uint32
	// X and Y are the 0-based screen cell of the top-left corner.
	X, Y int
	// Cols and Rows are the visible footprint in cells.
	Cols, Rows int
	// SrcCol and SrcRow are the image cell shown at the top-left corner,
	// from the cells' diacritics. Both are 0 unless the image is cut on
	// the left or the top (scrolled partly out of its pane).
	SrcCol, SrcRow int
}

// ResolvePlaceholders finds every kitty placeholder cell in screen, a
// complete frame of width x height cells, and returns the frame with
// each placeholder replaced by a space, plus the image rectangles the
// cells formed.
//
// The frame is parsed with ultraviolet and method, the same parser and
// width method bubbletea uses to lay the frame out, so a rectangle's
// position is where bubbletea will put those cells. The image ID is
// decoded the way kitty does, from a 24-bit or 256-color foreground,
// so whatever SGR re-encoding happened while compositing (lipgloss
// canvas, overlays) decodes the same as on a native terminal.
//
// Only the placeholder graphemes change in the returned frame: each
// becomes one space (both are one cell wide) and every SGR stays, so
// the background behind a transparent image is still drawn.
func ResolvePlaceholders(screen string, width, height int, method ansi.Method) (string, []PlaceholderRect) {
	var r PlaceholderResolver
	return r.Resolve(screen, width, height, method)
}

// PlaceholderResolver is ResolvePlaceholders keeping its cell buffer
// from one frame to the next, so resolving every frame of a scroll
// does not allocate a screenful of cells each time. Not safe for
// concurrent use; the App owns one.
type PlaceholderResolver struct {
	buf uv.ScreenBuffer
}

// Resolve is ResolvePlaceholders on r's buffer.
func (r *PlaceholderResolver) Resolve(screen string, width, height int, method ansi.Method) (string, []PlaceholderRect) {
	if width <= 0 || height <= 0 || !strings.Contains(screen, placeholderUTF8) {
		return screen, nil
	}
	switch {
	case r.buf.RenderBuffer == nil:
		r.buf = uv.NewScreenBuffer(width, height)
	case r.buf.Width() != width || r.buf.Height() != height:
		r.buf.Resize(width, height)
	}
	buf := r.buf
	buf.Method = method
	uv.NewStyledString(screen).Draw(buf, buf.Bounds())

	var rects []PlaceholderRect
	// open holds the indexes in rects of rectangles that reached the
	// previous row and may continue on this one.
	var open, next []int
	for y := 0; y < height; y++ {
		next = next[:0]
		var run *placeholderCell
		runX, runLen := 0, 0
		flush := func() {
			if run == nil {
				return
			}
			rects, next = addRun(rects, open, next, *run, runX, y, runLen)
			run = nil
		}
		var prev *placeholderCell
		for x := 0; x < width; x++ {
			cell := buf.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			pc, ok := decodePlaceholderCell(cell, prev)
			if !ok {
				flush()
				prev = nil
				continue
			}
			if run != nil && pc.id == run.id && pc.row == run.row &&
				pc.col == run.col+runLen && x == runX+runLen {
				runLen++
			} else {
				flush()
				start := pc
				run, runX, runLen = &start, x, 1
			}
			cur := pc
			prev = &cur
		}
		flush()
		open, next = next, open
	}
	return blankPlaceholders(screen), rects
}

// placeholderCell is one decoded placeholder cell.
type placeholderCell struct {
	id       uint32
	row, col int
}

// decodePlaceholderCell decodes cell when it holds a placeholder.
// Missing diacritics are inferred from prev, the placeholder cell just
// to the left, the way the kitty spec infers them: same row, next
// column.
func decodePlaceholderCell(cell *uv.Cell, prev *placeholderCell) (placeholderCell, bool) {
	if !strings.HasPrefix(cell.Content, placeholderUTF8) {
		return placeholderCell{}, false
	}
	id, ok := imageIDFromColor(cell.Style.Fg)
	if !ok {
		return placeholderCell{}, false
	}
	var idx []int
	for _, r := range cell.Content[len(placeholderUTF8):] {
		i, known := diacriticIndex[r]
		if !known {
			break
		}
		idx = append(idx, i)
	}
	pc := placeholderCell{id: id}
	if len(idx) >= 3 {
		pc.id |= uint32(idx[2]) << 24
	}
	switch {
	case len(idx) >= 2:
		pc.row, pc.col = idx[0], idx[1]
	case len(idx) == 1:
		pc.row = idx[0]
		if prev != nil && prev.id == pc.id && prev.row == pc.row {
			pc.col = prev.col + 1
		}
	case prev != nil && prev.id == pc.id:
		pc.row, pc.col = prev.row, prev.col+1
	}
	return pc, true
}

// imageIDFromColor decodes a kitty image ID from a cell foreground: the
// index of a 256-color (or basic) color, or the 24 bits of an RGB one.
func imageIDFromColor(c color.Color) (uint32, bool) {
	switch c := c.(type) {
	case nil:
		return 0, false
	case ansi.IndexedColor:
		return uint32(c), c != 0
	case ansi.BasicColor:
		return uint32(c), c != 0
	case color.RGBA:
		id := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		return id, id != 0
	}
	r, g, b, _ := c.RGBA()
	id := (r>>8)<<16 | (g>>8)<<8 | b>>8
	return id, id != 0
}

// addRun adds one row's run of placeholder cells to rects: it extends
// the open rectangle directly above when the run continues it (same
// image, columns and position, next image row), and starts a new
// rectangle otherwise. next collects the rectangles that reach row y.
func addRun(rects []PlaceholderRect, open, next []int, run placeholderCell, x, y, n int) ([]PlaceholderRect, []int) {
	for _, i := range open {
		r := &rects[i]
		if r.ID == run.id && r.X == x && r.Cols == n && r.SrcCol == run.col &&
			r.Y+r.Rows == y && r.SrcRow+r.Rows == run.row {
			r.Rows++
			return rects, append(next, i)
		}
	}
	rects = append(rects, PlaceholderRect{
		ID: run.id, X: x, Y: y, Cols: n, Rows: 1,
		SrcCol: run.col, SrcRow: run.row,
	})
	return rects, append(next, len(rects)-1)
}

// blankPlaceholders replaces each placeholder grapheme in s (the rune
// and up to three row, column and ID diacritics after it) with one
// space.
func blankPlaceholders(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for {
		i := strings.Index(s, placeholderUTF8)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteByte(' ')
		s = s[i+len(placeholderUTF8):]
		for n := 0; n < 3; n++ {
			r, size := utf8.DecodeRuneInString(s)
			if _, known := diacriticIndex[r]; !known {
				break
			}
			s = s[size:]
		}
	}
}

// EmulatedPlacements turns rects into frame-painter placements whose
// bytes are kitty direct placements. geom looks up an image's uploaded
// geometry (KittyRenderer.Geometry); a partly visible rectangle is
// cropped in the image's own pixels, so a cell size that changed since
// the upload does not skew the crop.
//
// Every placement carries an explicit placement ID derived from its
// screen slot. That is required: a placement without one removes every
// other placement of the same image, so one emoji could only appear
// once on screen. Erase deletes exactly that placement; on WezTerm text
// and ECH do not remove an image that has a placement ID.
func EmulatedPlacements(rects []PlaceholderRect, geom func(uint32) (ImageGeometry, bool)) []SixelPlacement {
	if len(rects) == 0 {
		return nil
	}
	out := make([]SixelPlacement, 0, len(rects))
	for _, r := range rects {
		pid := slotPlacementID(r.X, r.Y)
		place := fmt.Sprintf("a=p,i=%d,p=%d,c=%d,r=%d", r.ID, pid, r.Cols, r.Rows)
		if crop, ok := placementCrop(r, geom); ok {
			place += crop
		}
		out = append(out, SixelPlacement{
			Key:   fmt.Sprintf("kitty:%d@%d,%d", r.ID, r.SrcCol, r.SrcRow),
			Row:   r.Y + 1,
			Col:   r.X + 1,
			Rows:  r.Rows,
			Cols:  r.Cols,
			Bytes: []byte("\x1b_G" + place + ",C=1,q=2\x1b\\"),
			Erase: []byte(fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,p=%d,q=2\x1b\\", r.ID, pid)),
		})
	}
	return out
}

// slotPlacementID derives a placement ID from a screen cell. It is
// unique per slot, never 0, and stable while the slot holds the image,
// so repainting the same slot replaces the placement instead of
// stacking a second one.
func slotPlacementID(x, y int) uint32 {
	return (uint32(y)<<16 | uint32(x&0xFFFF)) + 1
}

// placementCrop returns the x,y,w,h source-rectangle keys for a
// rectangle that shows only part of its image, and false for one that
// shows all of it.
func placementCrop(r PlaceholderRect, geom func(uint32) (ImageGeometry, bool)) (string, bool) {
	g, known := ImageGeometry{}, false
	if geom != nil {
		g, known = geom(r.ID)
	}
	if known && g.Cols > 0 && g.Rows > 0 {
		if r.SrcCol == 0 && r.SrcRow == 0 && r.Cols == g.Cols && r.Rows == g.Rows {
			return "", false
		}
		x, w := r.SrcCol*g.PxW/g.Cols, r.Cols*g.PxW/g.Cols
		y, h := r.SrcRow*g.PxH/g.Rows, r.Rows*g.PxH/g.Rows
		return fmt.Sprintf(",x=%d,y=%d,w=%d,h=%d", x, y, w, h), true
	}
	if r.SrcCol == 0 && r.SrcRow == 0 {
		// No geometry (not uploaded by this renderer): place it whole.
		return "", false
	}
	cw, ch := measuredCellPixels()
	return fmt.Sprintf(",x=%d,y=%d,w=%d,h=%d", r.SrcCol*cw, r.SrcRow*ch, r.Cols*cw, r.Rows*ch), true
}
