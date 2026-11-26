package appview

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/bubbles/selection"
	"github.com/gammons/slk/internal/ui/styles"
)

// Text selection inside a modal: the dialog body is not an entrylist,
// but it is a list of rendered lines, so it carries the same plain
// mirror and column math and copies on mouse release like a message
// pane.

// BeginSelectionAt anchors a selection at a box-local cell. ok is false
// when the cell is not on a rendered body line.
func (m *Model) BeginSelectionAt(localX, localY int) bool {
	line, col, ok := m.bodyPos(localX, localY)
	if !ok {
		return false
	}
	a := selection.Anchor{Line: line, Col: col}
	m.sel = selection.Range{Start: a, End: a, Active: true}
	m.hasSel = true
	return true
}

// ExtendSelectionAt moves the selection's end to a box-local cell,
// clamped to the body when the cursor is dragged outside it.
func (m *Model) ExtendSelectionAt(localX, localY int) {
	if !m.hasSel {
		return
	}
	line, col, ok := m.bodyPos(localX, localY)
	if !ok {
		// Clamp to the nearest body cell: a drag past an edge keeps
		// extending to that edge.
		row := localY - m.lastBodyTop
		if row < 0 {
			row = 0
		}
		if row >= m.lastBody {
			row = m.lastBody - 1
		}
		line = m.yOffset + row
		if line < 0 {
			line = 0
		}
		if line >= len(m.plain) {
			line = len(m.plain) - 1
		}
		if line < 0 {
			return
		}
		col = localX - m.lastLeftPad
		if col < 0 {
			col = 0
		}
		if w := entrylist.DisplayWidth(m.plain[line]); col > w {
			col = w
		}
	}
	m.sel.End = selection.Anchor{Line: line, Col: col}
}

// Selecting reports whether a drag selection is in progress.
func (m *Model) Selecting() bool { return m.hasSel && m.sel.Active }

// EndSelection finishes a drag and returns the selected text; ok is
// false when nothing was selected. An empty selection is cleared.
func (m *Model) EndSelection() (string, bool) {
	if !m.hasSel {
		return "", false
	}
	m.sel.Active = false
	text := m.SelectionText()
	if text == "" {
		m.hasSel = false
		m.sel = selection.Range{}
		return "", false
	}
	return text, true
}

// HasSelection reports whether a selection is active or pinned.
func (m *Model) HasSelection() bool { return m.hasSel }

// SelectionText returns the selected text: each line trimmed of
// trailing spaces, lines joined by newlines. An empty selected line
// keeps its separator, so copying across a blank paragraph does not
// lose the break.
func (m *Model) SelectionText() string {
	if !m.hasSel || m.sel.IsEmpty() {
		return ""
	}
	lo, hi := m.sel.Normalize()
	var b strings.Builder
	first := true
	for line := lo.Line; line <= hi.Line && line < len(m.plain); line++ {
		if line < 0 {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		w := entrylist.DisplayWidth(m.plain[line])
		from, to := 0, w
		if line == lo.Line {
			from = lo.Col
		}
		if line == hi.Line {
			to = hi.Col
		}
		from, to = max(from, 0), min(to, w)
		if from >= to {
			continue
		}
		b.WriteString(strings.TrimRight(entrylist.SliceColumns(m.plain[line], from, to), " "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// clearSelection forgets a selection when the content changes.
func (m *Model) clearSelection() {
	m.hasSel = false
	m.sel = selection.Range{}
}

// bodyPos maps a box-local cell to a content line and display column.
func (m *Model) bodyPos(localX, localY int) (line, col int, ok bool) {
	row := localY - m.lastBodyTop
	if row < 0 || row >= m.lastBody {
		return 0, 0, false
	}
	line = m.yOffset + row
	if line < 0 || line >= len(m.plain) {
		return 0, 0, false
	}
	col = localX - m.lastLeftPad
	if col < 0 {
		col = 0
	}
	if w := entrylist.DisplayWidth(m.plain[line]); col > w {
		col = w
	}
	return line, col, true
}

// overlaySelection highlights the selected columns of the visible body
// lines, which start at content line top.
func (m *Model) overlaySelection(body []string, top int) {
	if !m.hasSel {
		return
	}
	lo, hi := m.sel.Normalize()
	for j, styled := range body {
		line := top + j
		if line < lo.Line || line > hi.Line || line < 0 || line >= len(m.plain) {
			continue
		}
		plain := m.plain[line]
		w := entrylist.DisplayWidth(plain)
		from, to := 0, w
		if line == lo.Line {
			from = lo.Col
		}
		if line == hi.Line {
			to = hi.Col
		}
		from, to = max(from, 0), min(to, w)
		if from >= to {
			continue
		}
		dispW := ansi.StringWidth(styled)
		dispFrom, dispTo := min(from, dispW), min(to, dispW)
		if dispFrom >= dispTo {
			continue
		}
		body[j] = ansi.Cut(styled, 0, dispFrom) +
			styles.SelectionStyle().Render(entrylist.SliceColumns(plain, from, to)) +
			ansi.Cut(styled, dispTo, dispW)
	}
}
