package imgrender

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/config"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// A short image beside a tall one stays in its column on every row,
// including the rows the short image does not cover.
func TestJoinImageLines_UnequalHeightsKeepColumns(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	src := solid(8, 8)
	short := imgpkg.HalfBlockRenderer{}.Render(src, image.Pt(4, 2))
	tall := imgpkg.HalfBlockRenderer{}.Render(src, image.Pt(6, 5))
	places := []imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 4, Rows: 2},
		{Index: 1, Col: 5, Row: 0, Cols: 6, Rows: 5},
	}
	lines := joinImageLines(places, [][]string{short.Lines, tall.Lines})
	if len(lines) != 5 {
		t.Fatalf("rows = %d, want 5", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != 11 {
			t.Fatalf("row %d width = %d, want 11 (4 + gap + 6)", i, w)
		}
		if strings.Count(ln, "▀") != 4+6 && i < 2 {
			t.Fatalf("row %d has %d half-blocks, want both images", i, strings.Count(ln, "▀"))
		}
		if i >= 2 && strings.Count(ln, "▀") != 6 {
			t.Fatalf("row %d has %d half-blocks, want only the tall image", i, strings.Count(ln, "▀"))
		}
	}
}

// Kitty placeholders survive being placed next to each other, and
// ResolvePlaceholders (the WezTerm path) reports two rectangles.
func TestJoinImageLines_KittyPlaceholdersStayDistinct(t *testing.T) {
	t.Setenv("TMUX", "")
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	kr := imgpkg.NewKittyRenderer(imgpkg.NewRegistry())
	a := solid(8, 8)
	b := solid(8, 4)
	kr.SetSource("a", a)
	kr.SetSource("b", b)
	ra := kr.RenderKey("a", image.Pt(3, 2))
	rb := kr.RenderKey("b", image.Pt(4, 2))
	lines := joinImageLines([]imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 3, Rows: 2},
		{Index: 1, Col: 4, Row: 0, Cols: 4, Rows: 2},
	}, [][]string{ra.Lines, rb.Lines})
	frame := strings.Join(lines, "\n")
	_, rects := imgpkg.ResolvePlaceholders(frame, 8, len(lines), ansi.WcWidth)
	if len(rects) != 2 {
		t.Fatalf("rects = %d, want 2", len(rects))
	}
	if rects[0].X > rects[1].X {
		rects[0], rects[1] = rects[1], rects[0]
	}
	if rects[0].X != 0 || rects[0].Cols != 3 || rects[1].X != 4 || rects[1].Cols != 4 {
		t.Fatalf("rects = %+v %+v, want x=0 cols=3 and x=4 cols=4", rects[0], rects[1])
	}
	if rects[0].Y != rects[1].Y || rects[0].ID == rects[1].ID {
		t.Fatalf("rects share a row and an id: %+v %+v", rects[0], rects[1])
	}
}

// Sixel reserves a blank cell box. Joining two of them must leave each
// box the width the painter will erase.
func TestJoinImageLines_SixelBoxesKeepTheirWidth(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	src := solid(4, 4)
	a := imgpkg.NewSixelRenderer().Render(src, image.Pt(5, 3))
	b := imgpkg.NewSixelRenderer().Render(src, image.Pt(2, 3))
	lines := joinImageLines([]imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 5, Rows: 3},
		{Index: 1, Col: 6, Row: 0, Cols: 2, Rows: 3},
	}, [][]string{a.Lines, b.Lines})
	if len(lines) != 3 {
		t.Fatalf("rows = %d, want 3", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != 8 {
			t.Fatalf("row %d width = %d, want 8", i, w)
		}
	}
}

func solid(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(40 + x), G: uint8(80 + y), B: 160, A: 255})
		}
	}
	return img
}
