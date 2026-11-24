package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Slk lays every frame out with lipgloss, which measures with grapheme
// widths, so the renderer must measure the same way or a row holding an
// emoji whose wcwidth and grapheme widths differ is written a column
// off. Init declares the mode; this pins that it is still declared.
func TestInitDeclaresGraphemeWidths(t *testing.T) {
	a := newTestApp(t)

	var found bool
	for _, msg := range drainBatch(a.Init()) {
		report, ok := msg.(tea.ModeReportMsg)
		if !ok {
			continue
		}
		if report.Mode != ansi.ModeUnicodeCore || report.Value != ansi.ModeSet {
			t.Errorf("report = %+v, want mode %v set", report, ansi.ModeUnicodeCore)
		}
		found = true
	}
	if !found {
		t.Fatal("Init did not declare grapheme widths; partial row updates can land a column off")
	}
}
