package image

import (
	"strings"
	"testing"
)

// go-sixel writes the DECSIXEL introducer followed immediately by the
// raster attributes: "\x1bP0;0;8q"1;1;W;H". SixelPixelSize reads W and H
// back out, because the encoded pixel size -- not the cell footprint the
// layout reserved -- is what a sixel payload paints on screen.
func TestSixelPixelSize(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })

	cases := []struct {
		name    string
		payload string
		w, h    int
		ok      bool
	}{
		{
			name:    "go-sixel header",
			payload: "\x1bP0;0;8q\"1;1;72;78#0;2;255;255;255#0!72~-\x1b\\",
			w:       72, h: 78, ok: true,
		},
		{
			name:    "data after RA carries q",
			payload: "\x1bP0;1;9q\"1;1;10;20?~q\x1b\\",
			w:       10, h: 20, ok: true,
		},
		{
			name:    "no raster attributes",
			payload: "\x1bPq#0;2;0;0;0!10~\x1b\\",
			ok:      false,
		},
		{
			name:    "truncated RA (no height)",
			payload: "\x1bPq\"1;1;72!10~\x1b\\",
			ok:      false,
		},
		{
			name:    "zero dimensions",
			payload: "\x1bPq\"0;0;0;0\x1b\\",
			ok:      false,
		},
		{
			name:    "no introducer",
			payload: "not sixel at all",
			ok:      false,
		},
		{
			name:    "empty",
			payload: "",
			ok:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h, ok := SixelPixelSize([]byte(tc.payload))
			if w != tc.w || h != tc.h || ok != tc.ok {
				t.Errorf("SixelPixelSize = (%d, %d, %v), want (%d, %d, %v)", w, h, ok, tc.w, tc.h, tc.ok)
			}
		})
	}
}

// A payload encoded at a cell size the terminal no longer has paints
// past the footprint the layout reserved for it: foot splits a sixel
// whose cells are partly erased and keeps the remainder, so the erase
// must cover the payload's pixel extent measured against the cell size
// in force now. This is the shape of the startup race this package's
// cell metrics exist for: slk cached 18x39 while the terminal settled
// at 9x20, so a 4x2-cell avatar was encoded at 72x78 pixels and painted
// 8x4 cells.
func TestSixelPainter_EraseCoversPixelsPaintedAtOldCellSize(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })
	SetCellPixels(9, 20)

	p := NewSixelPainter()
	// Recorded as a 4x2-cell avatar at row 5, col 12; the payload's RA
	// says 72x78 pixels.
	old := SixelPlacement{
		Key: "a", Row: 5, Col: 12, Rows: 2, Cols: 4,
		Bytes: []byte("\x1bP0;0;8q\"1;1;72;78\x1b\\"),
	}
	applyPlan(t, p, []SixelPlacement{old}, "", false)

	plan := p.Plan(nil, "", false)
	if plan.Erased != 1 {
		t.Fatalf("erased=%d, want 1", plan.Erased)
	}
	out := string(plan.Erase)
	// ceil(78/20) = 4 rows, ceil(72/9) = 8 columns, not the recorded 2x4.
	for _, want := range []string{
		"\x1b[5;12H", "\x1b[6;12H", "\x1b[7;12H", "\x1b[8;12H",
		"\x1b[8X",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("erase output missing %q; got %q", want, out)
		}
	}
	if strings.Contains(out, "\x1b[2X") {
		t.Errorf("erase still used the recorded 2-row footprint; got %q", out)
	}
}

// The other direction: a payload encoded at a smaller cell size than the
// terminal now has paints inside its recorded footprint, and the erase
// stays at the recorded cells -- the layout reserved them, so they are
// what must be cleared.
func TestSixelPainter_EraseStaysAtRecordedFootprintWhenCellsGrow(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })
	SetCellPixels(18, 39)

	p := NewSixelPainter()
	// Encoded for 9x20 cells (36x40 pixels), recorded as a 4x2 footprint.
	old := SixelPlacement{
		Key: "a", Row: 5, Col: 12, Rows: 2, Cols: 4,
		Bytes: []byte("\x1bP0;0;8q\"1;1;36;40\x1b\\"),
	}
	applyPlan(t, p, []SixelPlacement{old}, "", false)

	plan := p.Plan(nil, "", false)
	if plan.Erased != 1 {
		t.Fatalf("erased=%d, want 1", plan.Erased)
	}
	out := string(plan.Erase)
	if !strings.Contains(out, "\x1b[6;12H") || !strings.Contains(out, "\x1b[4X") {
		t.Errorf("erase output missing the recorded 2x4 footprint; got %q", out)
	}
	if strings.Contains(out, "\x1b[3;12H") {
		t.Errorf("erase grew past the recorded footprint; got %q", out)
	}
}

// A payload sized exactly for the recorded footprint erases exactly the
// recorded footprint.
func TestSixelPainter_EraseExactWhenPayloadMatchesCells(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })
	SetCellPixels(9, 20)

	p := NewSixelPainter()
	old := SixelPlacement{
		Key: "a", Row: 5, Col: 12, Rows: 2, Cols: 4,
		Bytes: []byte("\x1bP0;0;8q\"1;1;36;40\x1b\\"),
	}
	applyPlan(t, p, []SixelPlacement{old}, "", false)

	plan := p.Plan(nil, "", false)
	out := string(plan.Erase)
	if !strings.Contains(out, "\x1b[6;12H") || !strings.Contains(out, "\x1b[4X") {
		t.Errorf("erase output missing the 2x4 footprint; got %q", out)
	}
	if strings.Contains(out, "\x1b[7;12H") {
		t.Errorf("erase grew past the footprint; got %q", out)
	}
}
