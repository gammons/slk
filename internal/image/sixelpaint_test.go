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
