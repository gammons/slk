package image

import (
	"bytes"
	"strings"
	"testing"
)

func pl(key string, row, col, rows, cols int) SixelPlacement {
	return SixelPlacement{
		Key: key, Row: row, Col: col, Rows: rows, Cols: cols,
		Bytes: []byte("\x1bPq" + key + "\x1b\\"),
	}
}

// applyPlan plans a reconciliation and commits it, mirroring what
// FrameOutput does after a successful terminal write. Tests use it to
// build painter state and inspect the plan that would be emitted.
func applyPlan(t *testing.T, p *SixelPainter, want []SixelPlacement, eraseSGR string, force bool) SixelPlan {
	t.Helper()
	plan := p.Plan(want, eraseSGR, force)
	p.Commit(plan)
	return plan
}

// A new placement is painted at its absolute position, with the cursor
// saved and restored around it.
func TestSixelPainter_PaintsNewPlacement(t *testing.T) {
	p := NewSixelPainter()

	plan := applyPlan(t, p, []SixelPlacement{pl("a", 5, 12, 4, 20)}, "", false)
	if plan.Painted != 1 || plan.Erased != 0 {
		t.Fatalf("painted=%d erased=%d, want 1/0", plan.Painted, plan.Erased)
	}
	out := string(plan.Bytes)
	for _, want := range []string{"\x1b7", "\x1b[5;12H", "\x1bPq", "\x1b8"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if len(plan.Erase) != 0 {
		t.Errorf("new placement must not emit erases; got %q", plan.Erase)
	}
}

// The whole point: an unchanged placement must not be re-emitted. This is
// what stopped the per-frame 283KB flood.
func TestSixelPainter_UnchangedPlacementNotRepainted(t *testing.T) {
	p := NewSixelPainter()
	want := []SixelPlacement{pl("a", 5, 12, 4, 20)}

	applyPlan(t, p, want, "", false)
	plan := p.Plan(want, "", false)

	if plan.Painted != 0 || plan.Erased != 0 || len(plan.Bytes) != 0 || len(plan.Erase) != 0 {
		t.Fatalf("painted=%d erased=%d bytes=%d erase=%d, want all zero", plan.Painted, plan.Erased, len(plan.Bytes), len(plan.Erase))
	}
}

// A placement that disappears must be erased, not merely forgotten.
func TestSixelPainter_ErasesVanishedPlacement(t *testing.T) {
	p := NewSixelPainter()
	applyPlan(t, p, []SixelPlacement{pl("a", 5, 12, 3, 20)}, "", false)

	plan := p.Plan(nil, "", false)
	if plan.Painted != 0 || plan.Erased != 1 {
		t.Fatalf("painted=%d erased=%d, want 0/1", plan.Painted, plan.Erased)
	}
	out := string(plan.Erase)
	// One ECH per occupied row, positioned at the region's columns.
	for _, want := range []string{"\x1b[5;12H", "\x1b[6;12H", "\x1b[7;12H", "\x1b[20X"} {
		if !strings.Contains(out, want) {
			t.Errorf("erase output missing %q; got %q", want, out)
		}
	}
	p.Commit(plan)
	if p.Live() != 0 {
		t.Errorf("Live()=%d after erase, want 0", p.Live())
	}
}

// Scrolling moves an image: the old region's erase lands in plan.Erase
// (emitted BEFORE the text diff) and the new paint in plan.Bytes
// (emitted AFTER the text diff). If the erase ever followed the diff it
// would wipe the text that scrolled into the vacated region.
func TestSixelPainter_MovedPlacementErasesThenPaints(t *testing.T) {
	p := NewSixelPainter()
	applyPlan(t, p, []SixelPlacement{pl("a", 10, 5, 3, 20)}, "", false)

	plan := p.Plan([]SixelPlacement{pl("a", 8, 5, 3, 20)}, "", false)
	if plan.Painted != 1 || plan.Erased != 1 {
		t.Fatalf("painted=%d erased=%d on move, want 1/1", plan.Painted, plan.Erased)
	}
	if !strings.Contains(string(plan.Erase), "\x1b[10;5H") {
		t.Errorf("erase of the old row missing from plan.Erase; got %q", plan.Erase)
	}
	if !strings.Contains(string(plan.Bytes), "\x1b[8;5H") {
		t.Errorf("paint of the new row missing from plan.Bytes; got %q", plan.Bytes)
	}
}

// Same slot, different image (cache key changes) must repaint.
func TestSixelPainter_ContentChangeRepaints(t *testing.T) {
	p := NewSixelPainter()
	applyPlan(t, p, []SixelPlacement{pl("a", 5, 12, 4, 20)}, "", false)

	plan := p.Plan([]SixelPlacement{pl("b", 5, 12, 4, 20)}, "", false)
	if plan.Painted != 1 || plan.Erased != 1 {
		t.Errorf("painted=%d erased=%d on content change, want 1/1", plan.Painted, plan.Erased)
	}
}

// An empty desired frame erases everything previously live — the same
// contract the old Clear() provided (closing the preview, hiding the
// pane, quitting must not leave pixels behind).
func TestSixelPainter_ClearToEmptyErasesEverything(t *testing.T) {
	p := NewSixelPainter()
	applyPlan(t, p, []SixelPlacement{pl("a", 5, 12, 2, 10), pl("b", 20, 4, 2, 10)}, "", false)

	plan := p.Plan(nil, "", false)
	if plan.Erased != 2 {
		t.Fatalf("Erased=%d, want 2", plan.Erased)
	}
	out := string(plan.Erase)
	if !strings.Contains(out, "\x1b[5;12H") || !strings.Contains(out, "\x1b[20;4H") {
		t.Errorf("erase did not clear both regions; got %q", out)
	}
	p.Commit(plan)
	if p.Live() != 0 {
		t.Errorf("Live()=%d after clear-to-empty, want 0", p.Live())
	}
}

// Garbage in must not touch the screen.
func TestSixelPainter_IgnoresInvalidPlacements(t *testing.T) {
	p := NewSixelPainter()
	plan := p.Plan([]SixelPlacement{
		pl("zero-rows", 5, 5, 0, 10),
		pl("zero-cols", 5, 5, 3, 0),
		pl("row-zero", 0, 5, 3, 10),
		pl("col-zero", 5, 0, 3, 10),
	}, "", false)
	if plan.Painted != 0 || plan.Erased != 0 || len(plan.Bytes) != 0 || len(plan.Erase) != 0 {
		t.Errorf("painted=%d erased=%d bytes=%d erase=%d; invalid placements must be ignored",
			plan.Painted, plan.Erased, len(plan.Bytes), len(plan.Erase))
	}
}

// A restyled region (focus change, selection tint) rewrites the frame
// lines under an image and paints over the pixels. The placement is
// otherwise identical, so only the guard can catch it — without this the
// image disappears for good.
func TestSixelPainter_GuardChangeForcesRepaint(t *testing.T) {
	p := NewSixelPainter()

	before := pl("a", 5, 12, 4, 20)
	before.Guard = "frame-1"
	applyPlan(t, p, []SixelPlacement{before}, "", false)

	after := pl("a", 5, 12, 4, 20)
	after.Guard = "frame-2" // same image, same slot, rewritten underneath
	plan := p.Plan([]SixelPlacement{after}, "", false)
	if plan.Painted != 1 || plan.Erased != 1 {
		t.Errorf("painted=%d erased=%d after an underlying rewrite, want 1/1", plan.Painted, plan.Erased)
	}
}

// The guard must not defeat the change detection it sits beside: a stable
// frame still means zero bytes.
func TestSixelPainter_StableGuardStaysQuiet(t *testing.T) {
	p := NewSixelPainter()
	want := pl("a", 5, 12, 4, 20)
	want.Guard = "frame-1"

	applyPlan(t, p, []SixelPlacement{want}, "", false)
	plan := p.Plan([]SixelPlacement{want}, "", false)

	if plan.Painted != 0 || plan.Erased != 0 || len(plan.Bytes) != 0 || len(plan.Erase) != 0 {
		t.Errorf("painted=%d erased=%d bytes=%d erase=%d with an unchanged guard, want all zero",
			plan.Painted, plan.Erased, len(plan.Bytes), len(plan.Erase))
	}
}

// ECH clears to the CURRENT background, so an erase with no SGR leaves a
// bright rectangle where the image was. The pane background must be set
// before the ECH run and reset after.
func TestSixelPainter_ErasesWithPaneBackground(t *testing.T) {
	p := NewSixelPainter()
	const bg = "\x1b[48;2;30;30;46m"

	applyPlan(t, p, []SixelPlacement{pl("a", 5, 12, 2, 20)}, "", false)
	plan := p.Plan(nil, bg, false)

	out := string(plan.Erase)
	bgAt := strings.Index(out, bg)
	echAt := strings.Index(out, "\x1b[20X")
	if bgAt < 0 {
		t.Fatalf("erase did not set the pane background; got %q", out)
	}
	if echAt < 0 || bgAt > echAt {
		t.Error("background must be set BEFORE the ECH run, or cells clear to the terminal default")
	}
	if !strings.Contains(out, "\x1b[0m") {
		t.Error("erase must reset SGR afterwards so it does not leak into later output")
	}
}

// Without a style configured the erase stays byte-for-byte as before.
func TestSixelPainter_EraseWithoutStyleUnchanged(t *testing.T) {
	p := NewSixelPainter()
	applyPlan(t, p, []SixelPlacement{pl("a", 5, 12, 2, 20)}, "", false)

	plan := p.Plan(nil, "", false)
	if strings.Contains(string(plan.Erase), "\x1b[0m") {
		t.Error("no erase style configured, so no SGR should be emitted")
	}
}

// Plan must not mutate the painter: state changes only on Commit, so a
// failed underlying write never leaves the reconciler out of sync.
func TestSixelPainter_PlanDoesNotMutateUntilCommit(t *testing.T) {
	p := NewSixelPainter()
	plan := p.Plan([]SixelPlacement{pl("a", 5, 12, 4, 20)}, "", false)
	if p.Live() != 0 {
		t.Fatalf("Live before Commit = %d, want 0", p.Live())
	}
	p.Commit(plan)
	if p.Live() != 1 {
		t.Fatalf("Live after Commit = %d, want 1", p.Live())
	}
}

// A resize-force frame must erase and repaint an otherwise unchanged
// placement exactly once. The erase and paint are separate buffers with
// separate placements in the write: erase before the text diff, paint
// after it (see FrameOutput).
func TestSixelPainter_ForceRepaintsStablePlacement(t *testing.T) {
	p := NewSixelPainter()
	want := []SixelPlacement{pl("a", 5, 12, 4, 20)}
	p.Commit(p.Plan(want, "", false))

	plan := p.Plan(want, "", true)
	if plan.Painted != 1 || plan.Erased != 1 {
		t.Fatalf("force painted=%d erased=%d, want 1/1", plan.Painted, plan.Erased)
	}
	if !bytes.Contains(plan.Erase, []byte("\x1b[20X")) {
		t.Fatalf("force erase missing from plan.Erase: %q", plan.Erase)
	}
	if !bytes.Contains(plan.Bytes, want[0].Bytes) {
		t.Fatalf("force paint missing from plan.Bytes: %q", plan.Bytes)
	}
}

// A force frame must not touch placements that are gone: only wanted
// placements are repainted; vanished ones are merely erased once.
func TestSixelPainter_ForceDoesNotResurrectVanishedPlacement(t *testing.T) {
	p := NewSixelPainter()
	p.Commit(p.Plan([]SixelPlacement{pl("a", 5, 12, 2, 4)}, "", false))

	plan := p.Plan(nil, "", true)
	if plan.Painted != 0 || plan.Erased != 1 {
		t.Fatalf("force with empty want: painted=%d erased=%d, want 0/1", plan.Painted, plan.Erased)
	}
}

// kittyPl is a placement that removes itself with its own bytes, like
// the kitty direct placements of emulated mode.
func kittyPl(key string, row, col int) SixelPlacement {
	p := pl(key, row, col, 1, 2)
	p.Erase = []byte("\x1b_Gdel-" + key + "\x1b\\")
	return p
}

// A placement with Erase bytes is removed with them, not with ECH: on
// WezTerm ECH does not remove a kitty image that has a placement ID,
// and ECH would also paint the pane background over a reaction pill.
func TestSixelPainter_ErasesWithPlacementEraseBytes(t *testing.T) {
	p := NewSixelPainter()
	applyPlan(t, p, []SixelPlacement{kittyPl("a", 3, 4)}, "\x1b[48;2;1;2;3m", false)
	plan := applyPlan(t, p, nil, "\x1b[48;2;1;2;3m", false)
	if got, want := string(plan.Erase), "\x1b_Gdel-a\x1b\\"; got != want {
		t.Fatalf("erase = %q, want exactly the placement's Erase bytes %q", got, want)
	}
}

func TestFrameOutput_RepaintOnScrollForcesRepaintWhenDiffMovesLines(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)
	want := []SixelPlacement{kittyPl("a", 3, 4)}

	publish := func(repaintOnScroll bool) uint64 {
		return frames.Publish(SixelFrame{Placements: want, RepaintOnScroll: repaintOnScroll})
	}
	write := func(id uint64, body string) string {
		t.Helper()
		raw.Reset()
		if _, err := out.Write(markedFlush("slk", id, body)); err != nil {
			t.Fatal(err)
		}
		return raw.String()
	}

	write(publish(true), "first")
	if got := write(publish(true), "plain diff"); strings.Contains(got, "\x1bPqa") {
		t.Fatalf("unchanged placement repainted without a scroll: %q", got)
	}
	got := write(publish(true), "\x1b[2;20r\x1b[1Sscrolled\x1b[r")
	del, scroll, paint := strings.Index(got, "\x1b_Gdel-a"), strings.Index(got, "\x1b[1S"), strings.Index(got, "\x1bPqa")
	if del < 0 || paint < 0 || !(del < scroll && scroll < paint) {
		t.Fatalf("scrolled frame: want delete, then the scroll, then the paint; got %q", got)
	}
	if got := write(publish(false), "\x1b[1Sscrolled"); strings.Contains(got, "\x1bPqa") {
		t.Fatalf("frame without RepaintOnScroll repainted on a scroll: %q", got)
	}
}

// A write that clears the screen or moves lines with no frame plan
// applied (bubbletea re-entering the alternate screen after the external
// editor, or a flush whose frame id was already taken) leaves the images
// somewhere the painter does not know about, so the next frame repaints.
func TestFrameOutput_RepaintOnScrollAfterMoveWithoutPlan(t *testing.T) {
	want := []SixelPlacement{kittyPl("a", 3, 4)}
	for _, tc := range []struct {
		name string
		miss func(out *FrameOutput, stale uint64) error
	}{
		{"unmarked alternate screen", func(out *FrameOutput, _ uint64) error {
			_, err := out.Write([]byte("\x1b[?1049h\x1b[2J"))
			return err
		}},
		{"stale frame id", func(out *FrameOutput, stale uint64) error {
			_, err := out.Write(markedFlush("slk", stale, "\x1b[2Jredrawn"))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw bytes.Buffer
			frames := NewSixelFrameStore()
			out := NewFrameOutput(&raw, frames)
			first := frames.Publish(SixelFrame{Placements: want, RepaintOnScroll: true})
			if _, err := out.Write(markedFlush("slk", first, "first")); err != nil {
				t.Fatal(err)
			}
			if err := tc.miss(out, first); err != nil {
				t.Fatal(err)
			}
			raw.Reset()
			next := frames.Publish(SixelFrame{Placements: want, RepaintOnScroll: true})
			if _, err := out.Write(markedFlush("slk", next, "plain diff")); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(raw.String(), "\x1bPqa") {
				t.Fatalf("frame after the missed clear did not repaint: %q", raw.String())
			}
			raw.Reset()
			again := frames.Publish(SixelFrame{Placements: want, RepaintOnScroll: true})
			if _, err := out.Write(markedFlush("slk", again, "plain diff")); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw.String(), "\x1bPqa") {
				t.Fatalf("repaint repeated after it was done: %q", raw.String())
			}
		})
	}
}

// Without RepaintOnScroll (sixel) an unmarked clear changes nothing.
func TestFrameOutput_NoRepaintOnScrollIgnoresMoveWithoutPlan(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)
	want := []SixelPlacement{kittyPl("a", 3, 4)}
	if _, err := out.Write(markedFlush("slk", frames.Publish(SixelFrame{Placements: want}), "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("\x1b[?1049h\x1b[2J")); err != nil {
		t.Fatal(err)
	}
	raw.Reset()
	if _, err := out.Write(markedFlush("slk", frames.Publish(SixelFrame{Placements: want}), "plain diff")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw.String(), "\x1bPqa") {
		t.Fatalf("sixel frame repainted after an unmarked clear: %q", raw.String())
	}
}

func TestDiffMovesLines(t *testing.T) {
	cases := []struct {
		diff string
		want bool
	}{
		{"\x1b[3S", true},     // SU
		{"\x1b[T", true},      // SD
		{"\x1b[2L", true},     // IL
		{"\x1b[M", true},      // DL
		{"\x1b[5;20r", true},  // DECSTBM
		{"\x1b[2J", true},     // ED
		{"\x1b[?1049h", true}, // alternate screen
		{"\x1b[?1049l", true},
		{"\x1b[5;7H\x1b[38;2;1;2;3mtext\x1b[0m", false}, // CUP, SGR
		{"\x1b[K\x1b[3X\x1b[2P\x1b[4@", false},          // EL, ECH, DCH, ICH stay on their line
		{"\x1b[?2026h\x1b[?25l\x1b[?2026l", false},      // other private modes
		{"\x1b[?1;2;3S", false},                         // XTSMGRAPHICS, not SU
		{"no escapes at all", false},
	}
	for _, tc := range cases {
		if got := diffMovesLines([]byte(tc.diff)); got != tc.want {
			t.Errorf("diffMovesLines(%q) = %v, want %v", tc.diff, got, tc.want)
		}
	}
}
