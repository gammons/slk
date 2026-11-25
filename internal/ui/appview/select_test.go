package appview

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func selectionModel(t *testing.T) (*Model, int) {
	t.Helper()
	m := New()
	m.SetView(&core.AppView{
		ViewID: "V1",
		Title:  "AI Budget Support",
		Blocks: []blocks.Block{
			blocks.HeaderBlock{Text: "Your AI Budget"},
			blocks.SectionBlock{Text: "Justification: security bugs"},
		},
	})
	m.View(80, 24, blockkit.Context{})
	line := -1
	for i, p := range m.plain {
		if strings.Contains(p.Text, "Justification") {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("the justification line is not in the rendered body")
	}
	return &m, line
}

// Selecting across a blank body line keeps the paragraph break.
func TestSelectionKeepsBlankLines(t *testing.T) {
	m := New()
	m.SetView(&core.AppView{
		ViewID: "V1",
		Title:  "Dialog",
		Blocks: []blocks.Block{blocks.SectionBlock{Text: "one\n\nthree"}},
	})
	m.View(80, 24, blockkit.Context{})
	one, three := -1, -1
	for i, p := range m.plain {
		if p.Text == "one" {
			one = i
		}
		if p.Text == "three" {
			three = i
		}
	}
	if one < 0 || three < 0 {
		t.Fatalf("body lines = %+v, want one/blank/three", m.plain)
	}
	m.BeginSelectionAt(m.lastLeftPad, m.lastBodyTop+one-m.yOffset)
	m.ExtendSelectionAt(m.lastLeftPad+len("three"), m.lastBodyTop+three-m.yOffset)
	text, ok := m.EndSelection()
	if !ok || text != "one\n\nthree" {
		t.Errorf("selection = %q, %v, want one\\n\\nthree", text, ok)
	}
}

func TestSelectionCopiesTheDraggedCells(t *testing.T) {
	m, line := selectionModel(t)
	row := m.lastBodyTop + line - m.yOffset
	start := strings.Index(m.plain[line].Text, "security")
	if start < 0 {
		t.Fatal("no security text")
	}
	if !m.BeginSelectionAt(m.lastLeftPad+start, row) {
		t.Fatal("BeginSelectionAt missed the body")
	}
	if !m.Selecting() {
		t.Fatal("no selection in progress")
	}
	m.ExtendSelectionAt(m.lastLeftPad+start+len("security"), row)
	text, ok := m.EndSelection()
	if !ok || text != "security" {
		t.Fatalf("selection = %q, %v, want security", text, ok)
	}
	if m.Selecting() {
		t.Error("the selection is still active after release")
	}
}

func TestSelectionOutsideTheBodyIsIgnored(t *testing.T) {
	m, _ := selectionModel(t)
	if m.BeginSelectionAt(m.lastLeftPad, m.lastBodyTop-1) {
		t.Error("a click above the body started a selection")
	}
	if m.HasSelection() {
		t.Error("a rejected click made a selection")
	}
}

func TestSelectionClampsToTheBody(t *testing.T) {
	m, line := selectionModel(t)
	row := m.lastBodyTop + line - m.yOffset
	m.BeginSelectionAt(m.lastLeftPad, row)
	// Drag far past the right edge and below the body.
	m.ExtendSelectionAt(m.lastLeftPad+1000, m.lastBodyTop+1000)
	text, ok := m.EndSelection()
	if !ok || text == "" {
		t.Fatal("a clamped drag produced no text")
	}
	if !strings.Contains(text, "Justification") {
		t.Errorf("clamped selection = %q, want the line", text)
	}
}

func TestSetViewClearsSelection(t *testing.T) {
	m, line := selectionModel(t)
	row := m.lastBodyTop + line - m.yOffset
	m.BeginSelectionAt(m.lastLeftPad, row)
	m.SetView(&core.AppView{ViewID: "V2", Title: "Other"})
	if m.HasSelection() {
		t.Error("a new view kept the old selection")
	}
}
