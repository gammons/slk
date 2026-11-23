package main

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui"
)

// applyCellPixels re-points the image encoders and sends the App a
// CellMetricsChangedMsg only when the terminal now reports a size the
// encoders are not working against. Consecutive calls with the same size
// are no-ops, so a SIGWINCH burst applies once.
func TestApplyCellPixels(t *testing.T) {
	imgpkg.SetCellPixels(18, 39)
	t.Cleanup(func() { imgpkg.SetCellPixels(0, 0) })

	var sent []tea.Msg
	send := func(m tea.Msg) { sent = append(sent, m) }

	if applyCellPixels(send, nil, 18, 39) {
		t.Error("applyCellPixels applied the size the encoders already use")
	}
	if len(sent) != 0 {
		t.Errorf("sent %d messages for an unchanged size, want 0", len(sent))
	}

	if !applyCellPixels(send, nil, 9, 20) {
		t.Error("applyCellPixels ignored a changed size")
	}
	if w, h := imgpkg.MeasuredCellPixels(); w != 9 || h != 20 {
		t.Errorf("MeasuredCellPixels() = %dx%d, want 9x20", w, h)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(sent))
	}
	changed, ok := sent[0].(ui.CellMetricsChangedMsg)
	if !ok {
		t.Fatalf("sent %T, want ui.CellMetricsChangedMsg", sent[0])
	}
	if changed.CellW != 9 || changed.CellH != 20 {
		t.Errorf("CellMetricsChangedMsg = %dx%d, want 9x20", changed.CellW, changed.CellH)
	}
	// The clear comes right after the metrics message: the renderer's
	// whole-screen redraw is what restores the text the erase blanks.
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2 (metrics + clear)", len(sent))
	}
	if !reflect.DeepEqual(sent[1], tea.ClearScreen()) {
		t.Errorf("second message = %T, want tea.ClearScreen()'s msg", sent[1])
	}

	if applyCellPixels(send, nil, 9, 20) {
		t.Error("applyCellPixels applied an unchanged size again")
	}
	if len(sent) != 2 {
		t.Errorf("sent %d messages after the repeat, want 2", len(sent))
	}
}
