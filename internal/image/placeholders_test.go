package image

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// placeholderRow is row r of image id at cols cells wide, exactly as the
// kitty renderer emits it.
func placeholderRow(id uint32, cols, rows, r int) string {
	return buildPlaceholderLines(id, image.Pt(cols, rows))[r]
}

// placeholderCells is image row row of image id, keeping only image
// columns [c0, c1). It builds the cells by hand, so a cut image can be
// drawn the way a pane clips it.
func placeholderCells(id uint32, row, c0, c1 int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", byte(id>>16), byte(id>>8), byte(id))
	for c := c0; c < c1; c++ {
		b.WriteRune(PlaceholderRune)
		b.WriteRune(diacritic(row))
		b.WriteRune(diacritic(c))
	}
	b.WriteString("\x1b[39m")
	return b.String()
}

func resolve(t *testing.T, screen string, w, h int) (string, []PlaceholderRect) {
	t.Helper()
	return ResolvePlaceholders(screen, w, h, ansi.WcWidth)
}

func TestResolvePlaceholders_NoPlaceholdersIsIdentity(t *testing.T) {
	screen := "\x1b[31mhello\x1b[0m\nworld"
	out, rects := resolve(t, screen, 10, 2)
	if out != screen || rects != nil {
		t.Fatalf("got (%q, %v), want the screen back and no rects", out, rects)
	}
}

func TestResolvePlaceholders_Emoji(t *testing.T) {
	screen := "ab" + placeholderRow(5, 2, 1, 0) + "cd"
	out, rects := resolve(t, screen, 10, 1)
	want := []PlaceholderRect{{ID: 5, X: 2, Y: 0, Cols: 2, Rows: 1}}
	if !reflect.DeepEqual(rects, want) {
		t.Fatalf("rects = %+v, want %+v", rects, want)
	}
	if strings.ContainsRune(out, PlaceholderRune) {
		t.Fatalf("placeholder left in %q", out)
	}
	if got := ansi.Strip(out); got != "ab  cd" {
		t.Fatalf("stripped frame = %q, want %q", got, "ab  cd")
	}
}

// Two of the same emoji side by side share an image ID; the column
// index restarting is what splits them.
func TestResolvePlaceholders_AdjacentIdenticalEmojiAreTwoImages(t *testing.T) {
	emoji := placeholderRow(5, 2, 1, 0)
	_, rects := resolve(t, emoji+emoji, 10, 1)
	want := []PlaceholderRect{
		{ID: 5, X: 0, Y: 0, Cols: 2, Rows: 1},
		{ID: 5, X: 2, Y: 0, Cols: 2, Rows: 1},
	}
	if !reflect.DeepEqual(rects, want) {
		t.Fatalf("rects = %+v, want %+v", rects, want)
	}
}

func TestResolvePlaceholders_MultiRowImage(t *testing.T) {
	screen := "x" + placeholderRow(9, 3, 2, 0) + "\n" + "x" + placeholderRow(9, 3, 2, 1)
	_, rects := resolve(t, screen, 8, 2)
	want := []PlaceholderRect{{ID: 9, X: 1, Y: 0, Cols: 3, Rows: 2}}
	if !reflect.DeepEqual(rects, want) {
		t.Fatalf("rects = %+v, want %+v", rects, want)
	}
}

// An image scrolled half out of the top of its pane shows its later
// rows only; the rectangle records where in the image it starts.
func TestResolvePlaceholders_CutImages(t *testing.T) {
	cases := []struct {
		name   string
		screen string
		want   PlaceholderRect
	}{
		{"top rows scrolled away", placeholderCells(9, 1, 0, 3) + "\n" + placeholderCells(9, 2, 0, 3),
			PlaceholderRect{ID: 9, X: 0, Y: 0, Cols: 3, Rows: 2, SrcRow: 1}},
		{"cut on the right", placeholderCells(9, 0, 0, 2),
			PlaceholderRect{ID: 9, X: 0, Y: 0, Cols: 2, Rows: 1}},
		{"cut on the left", "ab" + placeholderCells(9, 0, 1, 3),
			PlaceholderRect{ID: 9, X: 2, Y: 0, Cols: 2, Rows: 1, SrcCol: 1}},
	}
	for _, tc := range cases {
		_, rects := resolve(t, tc.screen, 8, 3)
		if len(rects) != 1 || rects[0] != tc.want {
			t.Errorf("%s: rects = %+v, want [%+v]", tc.name, rects, tc.want)
		}
	}
}

// Kitty decodes the ID from a 256-color index or the 24 bits of an RGB
// foreground, in either SGR spelling, with other attributes in the same
// SGR (the lipgloss canvas bundles them). The resolver must agree.
func TestResolvePlaceholders_DecodesIDFromAnyForegroundForm(t *testing.T) {
	cell := string(PlaceholderRune) + string(diacritic(0)) + string(diacritic(0)) +
		string(PlaceholderRune) + string(diacritic(0)) + string(diacritic(1))
	cases := []struct {
		sgr  string
		want uint32
	}{
		{"\x1b[38;5;42m", 42},
		{"\x1b[38;2;0;1;2m", 0x000102},
		{"\x1b[38:2:0:0:7m", 7},
		{"\x1b[1;38;2;0;0;5m", 5},
	}
	for _, tc := range cases {
		_, rects := resolve(t, tc.sgr+cell+"\x1b[0m", 4, 1)
		if len(rects) != 1 || rects[0].ID != tc.want || rects[0].Cols != 2 {
			t.Errorf("sgr %q: rects = %+v, want one 2-cell image %d", tc.sgr, rects, tc.want)
		}
	}
}

// Without a foreground there is no image to show; the cell is still
// blanked, so no stray U+10EEEE reaches the terminal.
func TestResolvePlaceholders_NoForegroundIsBlankedButNotPlaced(t *testing.T) {
	screen := string(PlaceholderRune) + string(diacritic(0)) + string(diacritic(0)) + "z"
	out, rects := resolve(t, screen, 4, 1)
	if len(rects) != 0 {
		t.Errorf("rects = %+v, want none", rects)
	}
	if out != " z" {
		t.Errorf("frame = %q, want %q", out, " z")
	}
}

// The kitty spec lets a cell leave out its diacritics: the column
// follows the cell to the left, and a third diacritic carries the ID's
// high byte.
func TestResolvePlaceholders_InfersDiacriticsAndHighByte(t *testing.T) {
	ph := string(PlaceholderRune)
	screen := "\x1b[38;2;0;0;3m" + ph + string(diacritic(0)) + string(diacritic(0)) + ph + ph + "\x1b[39m"
	_, rects := resolve(t, screen, 5, 1)
	if want := []PlaceholderRect{{ID: 3, Cols: 3, Rows: 1}}; !reflect.DeepEqual(rects, want) {
		t.Errorf("inferred: rects = %+v, want %+v", rects, want)
	}

	hi := "\x1b[38;2;0;0;5m" + ph + string(diacritic(0)) + string(diacritic(0)) + string(diacritic(1)) + "\x1b[39m"
	_, rects = resolve(t, hi, 3, 1)
	if len(rects) != 1 || rects[0].ID != 5|1<<24 {
		t.Errorf("high byte: rects = %+v, want ID %d", rects, uint32(5|1<<24))
	}
}

// Bubbletea lays a frame out with wcwidth until the terminal reports
// unicode core mode (2027), then with grapheme widths. A VS16 emoji
// glyph before the image is one cell under the first and two under the
// second, and the image must land where bubbletea puts it either way.
func TestResolvePlaceholders_FollowsTheWidthMethod(t *testing.T) {
	screen := "❤️" + placeholderRow(5, 2, 1, 0)
	for _, tc := range []struct {
		method ansi.Method
		wantX  int
	}{{ansi.WcWidth, 1}, {ansi.GraphemeWidth, 2}} {
		_, rects := ResolvePlaceholders(screen, 6, 1, tc.method)
		if len(rects) != 1 || rects[0].X != tc.wantX {
			t.Errorf("method %v: rects = %+v, want X=%d", tc.method, rects, tc.wantX)
		}
	}
}

// Blanking must not move anything: every cell after an image stays in
// the column bubbletea would have put it in, under both width methods.
func TestResolvePlaceholders_BlankingKeepsLayout(t *testing.T) {
	screen := "a" + placeholderRow(5, 2, 1, 0) + "\x1b[1mZ\x1b[0m" + placeholderRow(6, 3, 1, 0) + "Q"
	for _, m := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		out, _ := ResolvePlaceholders(screen, 12, 1, m)
		if colOf(screen, "Q", m) != colOf(out, "Q", m) || colOf(screen, "Z", m) != colOf(out, "Z", m) {
			t.Errorf("method %v: blanking moved text: %q -> %q", m, screen, out)
		}
	}
}

// colOf is the column bubbletea's parser puts the first cell holding
// text in, or -1.
func colOf(screen, text string, m ansi.Method) int {
	buf := uv.NewScreenBuffer(20, 1)
	buf.Method = m
	uv.NewStyledString(screen).Draw(buf, buf.Bounds())
	for x := 0; x < 20; x++ {
		if c := buf.CellAt(x, 0); c != nil && c.Content == text {
			return x
		}
	}
	return -1
}

func TestEmulatedPlacements_DirectPlacementBytes(t *testing.T) {
	rects := []PlaceholderRect{
		{ID: 5, X: 2, Y: 0, Cols: 2, Rows: 1},
		{ID: 5, X: 6, Y: 3, Cols: 2, Rows: 1},
	}
	pls := EmulatedPlacements(rects, nil)
	if len(pls) != 2 {
		t.Fatalf("got %d placements, want 2", len(pls))
	}
	p0, p1 := slotPlacementID(2, 0), slotPlacementID(6, 3)
	if p0 == p1 || p0 == 0 {
		t.Fatalf("placement IDs %d and %d must differ and be non-zero", p0, p1)
	}
	want := fmt.Sprintf("\x1b_Ga=p,i=5,p=%d,c=2,r=1,C=1,q=2\x1b\\", p0)
	if got := string(pls[0].Bytes); got != want {
		t.Errorf("bytes = %q, want %q", got, want)
	}
	wantErase := fmt.Sprintf("\x1b_Ga=d,d=i,i=5,p=%d,q=2\x1b\\", p0)
	if got := string(pls[0].Erase); got != wantErase {
		t.Errorf("erase = %q, want %q", got, wantErase)
	}
	if pls[1].Row != 4 || pls[1].Col != 7 || pls[1].Rows != 1 || pls[1].Cols != 2 {
		t.Errorf("second placement slot = %+v, want 1-based (4,7) 2x1", pls[1])
	}
}

func TestEmulatedPlacements_CropsPartialImages(t *testing.T) {
	geom := func(id uint32) (ImageGeometry, bool) {
		return ImageGeometry{Cols: 3, Rows: 2, PxW: 30, PxH: 40}, id == 9
	}
	cases := []struct {
		name string
		rect PlaceholderRect
		want string
	}{
		{"whole image", PlaceholderRect{ID: 9, Cols: 3, Rows: 2}, ",c=3,r=2,C=1"},
		{"bottom row", PlaceholderRect{ID: 9, Cols: 3, Rows: 1, SrcRow: 1}, ",x=0,y=20,w=30,h=20,C=1"},
		{"right two columns", PlaceholderRect{ID: 9, Cols: 2, Rows: 2, SrcCol: 1}, ",x=10,y=0,w=20,h=40,C=1"},
	}
	for _, tc := range cases {
		got := string(EmulatedPlacements([]PlaceholderRect{tc.rect}, geom)[0].Bytes)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: bytes = %q, want them to contain %q", tc.name, got, tc.want)
		}
	}

	// No geometry: a partial image is cropped by the cell size it was
	// encoded at.
	cw, ch := measuredCellPixels()
	got := string(EmulatedPlacements([]PlaceholderRect{{ID: 1, Cols: 1, Rows: 1, SrcCol: 1}}, geom)[0].Bytes)
	if want := fmt.Sprintf(",x=%d,y=0,w=%d,h=%d", cw, cw, ch); !strings.Contains(got, want) {
		t.Errorf("no geometry: bytes = %q, want them to contain %q", got, want)
	}
}

func TestKittyRenderer_RecordsGeometry(t *testing.T) {
	kr := NewKittyRenderer(NewRegistry())
	kr.SetSource("k", image.NewRGBA(image.Rect(0, 0, 4, 4)))
	r := kr.RenderKey("k", image.Pt(3, 2))
	cw, ch := measuredCellPixels()
	g, ok := kr.Geometry(r.ID)
	if !ok || g != (ImageGeometry{Cols: 3, Rows: 2, PxW: 3 * cw, PxH: 2 * ch}) {
		t.Fatalf("Geometry(%d) = %+v, %v", r.ID, g, ok)
	}
}

// BenchmarkResolvePlaceholders is a large emoji-heavy frame: 240x60,
// every line styled, an emoji every 12 columns on every other line.
func BenchmarkResolvePlaceholders(b *testing.B) {
	const w, h = 240, 60
	var sb strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			if y%2 == 0 && x%12 == 4 && x+2 <= w {
				sb.WriteString(placeholderRow(uint32(2+x%7), 2, 1, 0))
				x += 2
				continue
			}
			sb.WriteString("\x1b[38;2;200;200;200;48;2;30;30;40mword\x1b[0m")
			x += 4
		}
		if y < h-1 {
			sb.WriteByte('\n')
		}
	}
	screen := sb.String()
	b.SetBytes(int64(len(screen)))
	var r PlaceholderResolver
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, rects := r.Resolve(screen, w, h, ansi.GraphemeWidth)
		_ = EmulatedPlacements(rects, nil)
		_ = out
	}
}

// A reused resolver gives the same answer as a fresh one, frame after
// frame and across a resize, including cells the new frame no longer
// writes.
func TestPlaceholderResolver_ReuseMatchesFresh(t *testing.T) {
	frames := []struct {
		screen string
		w, h   int
	}{
		{"ab" + placeholderRow(5, 2, 1, 0) + "\n" + placeholderRow(6, 2, 1, 0), 8, 2},
		{"x", 8, 2},
		{placeholderRow(7, 3, 1, 0), 5, 3},
	}
	var r PlaceholderResolver
	for i, f := range frames {
		gotOut, gotRects := r.Resolve(f.screen, f.w, f.h, ansi.WcWidth)
		wantOut, wantRects := ResolvePlaceholders(f.screen, f.w, f.h, ansi.WcWidth)
		if gotOut != wantOut || !reflect.DeepEqual(gotRects, wantRects) {
			t.Errorf("frame %d: reused (%q, %+v), fresh (%q, %+v)", i, gotOut, gotRects, wantOut, wantRects)
		}
	}
}
