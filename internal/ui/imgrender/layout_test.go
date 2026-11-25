package imgrender

import (
	"image"
	"testing"
)

// TestPackImageRows_SingleImageKeepsSize is the regression the gallery
// must not trip: one image is returned at the size the caller already
// chose, including when that size is wider than the pane. computeImageTarget
// is what clamps a real image to the pane; packing must not do it again.
func TestPackImageRows_SingleImageKeepsSize(t *testing.T) {
	got := packImageRows([]image.Point{image.Pt(40, 20)}, 1)
	want := []imagePlacement{{Index: 0, Col: 0, Row: 0, Cols: 40, Rows: 20}}
	if !samePlacements(got, want) {
		t.Fatalf("single image at width 1 = %+v, want %+v", got, want)
	}

	got = packImageRows([]image.Point{image.Pt(40, 20)}, 80)
	want = []imagePlacement{{Index: 0, Col: 0, Row: 0, Cols: 40, Rows: 20}}
	if !samePlacements(got, want) {
		t.Fatalf("single image at width 80 = %+v, want %+v", got, want)
	}
}

// TestPackImageRows_ScalesWideImagesOntoOneRow pins the size two images
// take when each one's natural footprint is too wide to share the pane.
// slot = (80-1)/2 = 39; rows = int(20*39/60) = 13.
func TestPackImageRows_ScalesWideImagesOntoOneRow(t *testing.T) {
	got := packImageRows([]image.Point{image.Pt(60, 20), image.Pt(60, 20)}, 80)
	want := []imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 39, Rows: 13},
		{Index: 1, Col: 40, Row: 0, Cols: 39, Rows: 13},
	}
	if !samePlacements(got, want) {
		t.Fatalf("two wide images = %+v, want %+v", got, want)
	}
}

// TestPackImageRows_WrapsTheImageThatDoesNotFit: a third copy of the
// same image starts the next row, directly under the first, and keeps
// the shared thumbnail size rather than growing back to its natural one.
func TestPackImageRows_WrapsTheImageThatDoesNotFit(t *testing.T) {
	got := packImageRows([]image.Point{
		image.Pt(60, 20), image.Pt(60, 20), image.Pt(60, 20),
	}, 80)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantThird := imagePlacement{Index: 2, Col: 0, Row: 13, Cols: 39, Rows: 13}
	if got[2] != wantThird {
		t.Fatalf("third image = %+v, want %+v", got[2], wantThird)
	}
	if got[0].Row != 0 || got[1].Row != 0 {
		t.Fatalf("first two images did not share a row: %+v", got[:2])
	}
}

// TestPackImageRows_DoesNotScaleImagesThatAlreadyFit: portraits narrower
// than the per-image slot stay at their natural size and pack three
// across, with one column of gap.
func TestPackImageRows_DoesNotScaleImagesThatAlreadyFit(t *testing.T) {
	got := packImageRows([]image.Point{
		image.Pt(5, 8), image.Pt(5, 8), image.Pt(5, 8),
	}, 80)
	want := []imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 5, Rows: 8},
		{Index: 1, Col: 6, Row: 0, Cols: 5, Rows: 8},
		{Index: 2, Col: 12, Row: 0, Cols: 5, Rows: 8},
	}
	if !samePlacements(got, want) {
		t.Fatalf("small images = %+v, want %+v", got, want)
	}
}

// TestPackImageRows_TallerImageSetsTheNextRow: the second row starts
// below the tallest image on the first, and the short image does not
// stretch.
func TestPackImageRows_TallerImageSetsTheNextRow(t *testing.T) {
	// Natural widths 50 and 50 in a 40-col pane: slot = (40-1)/2 = 19,
	// so they share the row. A third image of the same size wraps.
	// rows = int(12*19/50) = 4 for the tall one, int(4*19/50) = 1 for
	// the short one.
	got := packImageRows([]image.Point{
		image.Pt(50, 4), image.Pt(50, 12), image.Pt(50, 4),
	}, 40)
	want := []imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 19, Rows: 1},
		{Index: 1, Col: 20, Row: 0, Cols: 19, Rows: 4},
		{Index: 2, Col: 0, Row: 4, Cols: 19, Rows: 1},
	}
	if !samePlacements(got, want) {
		t.Fatalf("mixed heights = %+v, want %+v", got, want)
	}
}

func TestPackImageRows_NarrowWidths(t *testing.T) {
	sizes := []image.Point{image.Pt(40, 20), image.Pt(30, 10), image.Pt(8, 8)}
	// Widths that cannot hold two images plus the gap put one image on
	// each row. Width 0 is treated as 1 so the packer never divides by
	// zero or emits a zero-size cell.
	for _, width := range []int{0, 1, 2} {
		got := packImageRows(sizes, width)
		if len(got) != len(sizes) {
			t.Fatalf("width %d: len = %d, want %d", width, len(got), len(sizes))
		}
		row := 0
		for i, p := range got {
			if p.Index != i || p.Col != 0 || p.Cols != 1 || p.Row != row {
				t.Fatalf("width %d: image %d = %+v, want index %d col 0 cols 1 row %d",
					width, i, p, i, row)
			}
			if p.Rows < 1 {
				t.Fatalf("width %d: image %d has %d rows", width, i, p.Rows)
			}
			row += p.Rows
		}
	}
}

// TestPackImageRows_SeveralPaneWidths checks the invariants that have
// to hold at every width the message pane can take, including ones
// where several images share a row and ones where they cannot.
func TestPackImageRows_SeveralPaneWidths(t *testing.T) {
	sets := [][]image.Point{
		{image.Pt(60, 20)},
		{image.Pt(60, 20), image.Pt(60, 20)},
		{image.Pt(60, 20), image.Pt(10, 20), image.Pt(60, 20), image.Pt(4, 4)},
		{image.Pt(1, 1), image.Pt(1, 1), image.Pt(1, 1), image.Pt(1, 1)},
		{image.Pt(100, 40), image.Pt(20, 80), image.Pt(8, 8), image.Pt(80, 5)},
	}
	widths := []int{1, 2, 3, 4, 19, 20, 40, 80, 120}
	for _, sizes := range sets {
		for _, width := range widths {
			assertPacked(t, sizes, width)
		}
	}
}

func TestPackImageRows_ZeroSizeClampsToOneCell(t *testing.T) {
	got := packImageRows([]image.Point{{}, {}}, 40)
	want := []imagePlacement{
		{Index: 0, Col: 0, Row: 0, Cols: 1, Rows: 1},
		{Index: 1, Col: 2, Row: 0, Cols: 1, Rows: 1},
	}
	if !samePlacements(got, want) {
		t.Fatalf("zero sizes = %+v, want %+v", got, want)
	}
}

func TestPackImageRows_Empty(t *testing.T) {
	if got := packImageRows(nil, 80); got != nil {
		t.Fatalf("empty = %+v, want nil", got)
	}
}

func assertPacked(t *testing.T, sizes []image.Point, width int) {
	t.Helper()
	got := packImageRows(sizes, width)
	pane := width
	if pane < 1 {
		pane = 1
	}
	if len(got) != len(sizes) {
		t.Fatalf("sizes %v width %d: len %d, want %d", sizes, width, len(got), len(sizes))
	}
	seen := make([]bool, len(sizes))
	type box struct{ col, row, cols, rows int }
	boxes := make([]box, len(got))
	for i, p := range got {
		if p.Index < 0 || p.Index >= len(sizes) || seen[p.Index] {
			t.Fatalf("sizes %v width %d: bad index %+v", sizes, width, p)
		}
		seen[p.Index] = true
		if p.Cols < 1 || p.Rows < 1 {
			t.Fatalf("sizes %v width %d: zero cell %+v", sizes, width, p)
		}
		if p.Col < 0 || p.Row < 0 {
			t.Fatalf("sizes %v width %d: negative origin %+v", sizes, width, p)
		}
		natural := sizes[p.Index]
		if natural.X < 1 {
			natural.X = 1
		}
		if natural.Y < 1 {
			natural.Y = 1
		}
		if len(sizes) == 1 {
			if p.Cols != natural.X || p.Rows != natural.Y || p.Col != 0 || p.Row != 0 {
				t.Fatalf("single image changed at width %d: %+v, natural %v", width, p, natural)
			}
		} else {
			if p.Cols > natural.X || p.Rows > natural.Y {
				t.Fatalf("sizes %v width %d: scaled up %+v from %v", sizes, width, p, natural)
			}
			if p.Col+p.Cols > pane {
				t.Fatalf("sizes %v width %d: overflows pane %+v", sizes, width, p)
			}
		}
		boxes[i] = box{p.Col, p.Row, p.Cols, p.Rows}
	}
	for i := range boxes {
		for j := i + 1; j < len(boxes); j++ {
			if overlaps(boxes[i].col, boxes[i].row, boxes[i].cols, boxes[i].rows,
				boxes[j].col, boxes[j].row, boxes[j].cols, boxes[j].rows) {
				t.Fatalf("sizes %v width %d: overlap %+v and %+v", sizes, width, got[i], got[j])
			}
		}
	}
	// Images on one row keep at least the gap between them. An image on
	// a later row starts at or below every image that began above it,
	// so a tall neighbour cannot stick into the next row.
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			a, b := got[i], got[j]
			if a.Row == b.Row {
				left, right := a, b
				if right.Col < left.Col {
					left, right = right, left
				}
				if gap := right.Col - (left.Col + left.Cols); gap < imageGap {
					t.Fatalf("sizes %v width %d: gap %d between %+v and %+v", sizes, width, gap, left, right)
				}
				continue
			}
			above, below := a, b
			if below.Row < above.Row {
				above, below = below, above
			}
			if below.Row < above.Row+above.Rows {
				t.Fatalf("sizes %v width %d: %+v extends into %+v", sizes, width, above, below)
			}
		}
	}
}

func overlaps(c1, r1, w1, h1, c2, r2, w2, h2 int) bool {
	return c1 < c2+w2 && c2 < c1+w1 && r1 < r2+h2 && r2 < r1+h1
}

func samePlacements(got, want []imagePlacement) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
