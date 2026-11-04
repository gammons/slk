package entrylist

import (
	"math"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/scrollbar"
)

// NoCursor is a cursor value that matches no entry.
const NoCursor = math.MinInt

// Visible describes one entry's rows in a frame.
type Visible struct {
	Index    int // entry index
	From, To int // the entry's rows [From, To) that are on screen
	Row      int // window row the entry's row From lands on
}

// Frame is one laid-out window. An owner may rewrite Rows in place (to
// substitute image fallbacks, say) before passing the frame to Compose.
type Frame struct {
	Rows    []string
	Visible []Visible
	// Cursor is the cursor's Ref after this frame; WithCursorFollowsScroll
	// can move it, and the owner must adopt the new value.
	Cursor int
	// MoreBelow reports content below the window.
	MoreBelow bool
}

// Layout positions the window for this frame. It puts the window on the
// last line if HoldBottom asked for it, locates the cursor's entry (the
// selectable entry whose Ref is cursor), snaps the offset to it if the
// cursor moved since the last snap, clamps, lets the cursor follow the
// scroll when enabled, then slices the visible rows and translates the
// visible entries' hits.
//
// lines(i) returns the rows to draw for the cursor's entry i; a nil lines
// draws Entry.Selected, or Lines when that is nil.
func (m *Model) Layout(cursor int, lines func(i int) []string) *Frame {
	if m.holdBottom {
		m.holdBottom = false
		m.setY(m.maxOffset())
	}
	m.locate(cursor)
	if !m.hasSnapped || m.snapped != cursor {
		if m.selEnd > m.yOffset+m.height {
			m.setY(m.selEnd - m.height)
		}
		if m.selStart < m.yOffset {
			m.setY(m.selStart)
		}
		m.MarkSnapped(cursor)
	}
	m.clamp()
	if m.follow {
		cursor = m.followCursor(cursor)
	}
	f := &Frame{Cursor: cursor, MoreBelow: m.yOffset+m.height < m.total}
	m.hits = m.hits[:0]
	f.Rows, f.Visible = m.slice(cursor, lines, true)
	return f
}

// Static slices the window at the current offset with no snapping, no
// clamping beyond the eager clamp and no cursor. It translates the
// window's hits like Layout, replacing the last frame's, and cancels
// HoldBottom. The thread pane's zero-reply state uses it.
func (m *Model) Static() *Frame {
	m.holdBottom = false
	f := &Frame{Cursor: NoCursor, MoreBelow: m.yOffset+m.height < m.total}
	m.hits = m.hits[:0]
	f.Rows, f.Visible = m.slice(NoCursor, nil, true)
	return f
}

// Compose finishes a frame. It pads Rows to the window height with
// background rows; replaces the first row with top, and the last with
// bottom when there is content below the window; overlays the text
// selection when applySelection is set; and draws the scrollbar. An empty
// top or bottom leaves its row alone.
func (m *Model) Compose(f *Frame, top, bottom string, applySelection bool) []string {
	rows := f.Rows
	if len(rows) < m.height {
		sp := m.spacerRow()
		for len(rows) < m.height {
			rows = append(rows, sp)
		}
	}
	m.overTop = top != "" && len(rows) > 0
	if m.overTop {
		rows[0] = top
	}
	m.overBottom = bottom != "" && f.MoreBelow && len(rows) > 0
	if m.overBottom {
		rows[len(rows)-1] = bottom
	}
	if applySelection && m.hasSel {
		m.overlaySelection(rows, 0, 0)
	}
	return scrollbar.Overlay(rows, m.width, m.total, m.yOffset, m.height,
		m.styles.Background, m.styles.Track, m.styles.Thumb)
}

// HitTest returns the last frame's first hit of kind covering (row, col),
// with row relative to the first content row.
func (m *Model) HitTest(kind HitKind, row, col int) (FrameHit, bool) {
	for _, h := range m.hits {
		if h.Kind == kind && row >= h.RowStart && row < h.RowEnd && col >= h.ColStart && col < h.ColEnd {
			return h, true
		}
	}
	return FrameHit{}, false
}

// Hits returns the last frame's hits.
func (m *Model) Hits() []FrameHit { return m.hits }

// locate records the cursor entry's scroll target [selStart, selEnd),
// which excludes its Trail rows. Both are 0 when no entry matches.
func (m *Model) locate(cursor int) {
	m.selStart, m.selEnd = 0, 0
	for i := range m.entries {
		e := &m.entries[i]
		if e.Selectable && e.Ref == cursor {
			m.selStart = m.offsets[i]
			m.selEnd = m.selStart + len(e.Lines) - e.Trail
			return
		}
	}
}

// followCursor moves an off-screen cursor to the nearest visible
// selectable entry: the topmost when it went off the top, the bottommost
// when it went off the bottom.
func (m *Model) followCursor(cursor int) int {
	top, bottom := m.yOffset, m.yOffset+m.height
	above, below := m.selEnd <= top, m.selStart >= bottom
	if !above && !below {
		return cursor
	}
	next, found := 0, false
	for i := range m.entries {
		e := &m.entries[i]
		if !e.Selectable {
			continue
		}
		start := m.offsets[i]
		if start+len(e.Lines) <= top || start >= bottom {
			continue
		}
		next, found = e.Ref, true
		if above {
			break
		}
	}
	if found && next != cursor {
		cursor = next
		m.snapped = cursor
		m.locate(cursor)
	}
	return cursor
}

func (m *Model) slice(cursor int, lines func(int) []string, withHits bool) ([]string, []Visible) {
	rows := make([]string, 0, max(m.height, 0))
	var vis []Visible
	top, bottom := m.yOffset, m.yOffset+m.height
	for i := range m.entries {
		if len(rows) >= m.height {
			break
		}
		e := &m.entries[i]
		start := m.offsets[i]
		end := start + len(e.Lines)
		if end <= top {
			continue
		}
		if start >= bottom {
			break
		}
		ls := e.Lines
		if e.Selectable && e.Ref == cursor {
			switch {
			case lines != nil:
				ls = lines(i)
			case e.Selected != nil:
				ls = e.Selected
			}
		}
		from, to := 0, len(ls)
		if start < top {
			from = top - start
		}
		if end > bottom {
			to = len(ls) - (end - bottom)
		}
		vis = append(vis, Visible{Index: i, From: from, To: to, Row: len(rows)})
		rows = append(rows, ls[from:to]...)
		if !withHits {
			continue
		}
		for _, h := range e.Hits {
			as, ae := start+h.RowStart, start+h.RowEnd
			if ae <= top || as >= bottom {
				continue
			}
			m.hits = append(m.hits, FrameHit{
				RowStart: max(as-top, 0), RowEnd: min(ae-top, m.height),
				ColStart: h.ColStart, ColEnd: h.ColEnd,
				Kind: h.Kind, Key: h.Key, Aux: h.Aux, Ref: e.Ref,
			})
		}
	}
	return rows, vis
}

// spacerRow is a full-width background row, cached per width and styles.
func (m *Model) spacerRow() string {
	if !m.spacerOK || m.spacerWidth != m.width {
		m.spacer = lipgloss.NewStyle().Background(m.styles.Background).Width(m.width).Render("")
		m.spacerWidth, m.spacerOK = m.width, true
	}
	return m.spacer
}
