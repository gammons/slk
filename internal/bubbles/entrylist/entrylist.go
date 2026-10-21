// Package entrylist is a scrollable, selectable list of pre-rendered
// entries: the substrate the channel and thread panes share.
//
// It owns line offsets, the scroll offset, the visible window, text
// selection, hit-rectangle translation and the scrollbar. It knows
// nothing about messages: an owner renders each item to an Entry and
// keeps everything domain-specific (chrome, empty states, what an entry
// means) to itself.
//
// Every behaviour the two panes do differently is an Option, set by each
// owner so that it behaves exactly as it did before this package existed.
// The options are the executable form of the divergence list that
// internal/ui/thread/lockstep_test.go used to carry.
package entrylist

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/selection"
)

// Entry is one pre-rendered item: a message, or a decoration row such as
// a day divider.
type Entry struct {
	// ID is the stable key a text selection anchors to (a Slack ts).
	// "" marks a decoration.
	ID string
	// Ref is the owner's index for the entry (a message or reply index),
	// or -1. Hit tests and cursor moves report it.
	Ref int
	// Selectable entries can hold the cursor. Snapping, ClickAt-style
	// lookups and cursor-follows-scroll consider only these.
	Selectable bool
	// Lines are the rendered rows, each exactly the list width.
	Lines []string
	// Selected is the cursor-on variant of Lines, the same length. nil
	// means draw Lines.
	Selected []string
	// Plain mirrors the rows a text selection can cover. Rows at or past
	// len(Plain) (every row, when Plain is nil) are never selected.
	Plain []PlainLine
	// ColOffset is the number of display columns of entry chrome (the
	// "▌" border) before the content Plain mirrors.
	ColOffset int
	// Trail is the number of trailing rows that belong to the entry but
	// are not scrolled into view when the cursor lands on it (the channel
	// pane's spacer row).
	Trail int
	// Hits are clickable rectangles, relative to Lines.
	Hits []Hit
}

// HitKind says what a clickable rectangle is.
type HitKind uint8

const (
	// HitReaction is a reaction pill; Key is the emoji name.
	HitReaction HitKind = iota + 1
	// HitImage is an inline image; Key is the file ID, Aux the
	// attachment index.
	HitImage
)

// Hit is a clickable rectangle relative to an Entry's Lines.
type Hit struct {
	RowStart, RowEnd int // [RowStart, RowEnd)
	ColStart, ColEnd int // [ColStart, ColEnd), display columns
	Kind             HitKind
	Key              string
	Aux              int
}

// FrameHit is a Hit translated into the last frame's window: Row 0 is
// the first row of content under the owner's chrome. Ref is the owning
// entry's Ref.
type FrameHit struct {
	RowStart, RowEnd int
	ColStart, ColEnd int
	Kind             HitKind
	Key              string
	Aux              int
	Ref              int
}

// Styles are the colours the list draws with itself; entries carry their
// own styling.
type Styles struct {
	// Background fills padding rows and the scrollbar gutter.
	Background color.Color
	// Track and Thumb colour the scrollbar.
	Track, Thumb color.Color
	// Selection highlights text-selected columns.
	Selection lipgloss.Style
}

// DefaultStyles returns usable styles for a dark background. Owners with
// a theme build Styles directly.
func DefaultStyles() Styles {
	return Styles{
		Background: lipgloss.Color("#1A1A2E"),
		Track:      lipgloss.Color("#3A3A5C"),
		Thumb:      lipgloss.Color("#4A9EFF"),
		Selection:  lipgloss.NewStyle().Reverse(true),
	}
}

// ReanchorPolicy says how SetEntries keeps the scroll position when the
// width changes (every entry rewraps): a viewport at the bottom stays at
// the bottom, otherwise the entry under the first visible line stays
// there, at the same row within it.
type ReanchorPolicy struct {
	// BottomUsesNewHeight measures "was at the bottom" with this frame's
	// height instead of the previous frame's.
	BottomUsesNewHeight bool
	// AnchorRefsOnly anchors only on entries with Ref >= 0. A first
	// visible line above the first such entry keeps the raw offset.
	AnchorRefsOnly bool
}

// Option configures a Model.
type Option func(*Model)

// WithStyles sets the initial styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithCursorFollowsScroll moves the cursor onto the window when a scroll
// leaves it off-screen (the channel pane).
func WithCursorFollowsScroll() Option { return func(m *Model) { m.follow = true } }

// WithPinBottomOnShrink keeps a bottom-anchored window at the bottom when
// SetHeight shrinks it (the channel pane).
func WithPinBottomOnShrink() Option { return func(m *Model) { m.pinBottom = true } }

// WithSnapDecorationAnchors lets a selection that starts or extends on a
// decoration snap to the nearest selectable entry (the channel pane).
// Without it such a press anchors nothing.
func WithSnapDecorationAnchors() Option { return func(m *Model) { m.snapDecor = true } }

// WithDirtyOnDrag bumps Version on every selection change during a drag
// (the thread pane). Without it only the first press and an empty
// release do, because the channel pane repaints the selection as a
// post-pass (ApplySelectionToBordered).
func WithDirtyOnDrag() Option { return func(m *Model) { m.dirtyOnDrag = true } }

// WithEagerClamp clamps the offset on every assignment and makes a scroll
// past either end a no-op (the thread pane, which used bubbles/viewport).
// Without it the offset is clamped once per frame, in Layout.
func WithEagerClamp() Option { return func(m *Model) { m.eager = true } }

// WithReanchor sets the width-change policy.
func WithReanchor(p ReanchorPolicy) Option { return func(m *Model) { m.reanchor = p } }

// Model is the list. The zero value is not usable; call New.
type Model struct {
	styles                                           Styles
	follow, pinBottom, snapDecor, dirtyOnDrag, eager bool
	reanchor                                         ReanchorPolicy

	entries []Entry
	live    int // entries lookups may see; -1 = all (see KeepLookups)
	offsets []int
	total   int
	ids     map[string]int
	width   int

	height, prevHeight int
	yOffset            int
	snapped            int
	hasSnapped         bool
	holdBottom         bool
	selStart, selEnd   int

	sel    selection.Range
	hasSel bool

	hits                []FrameHit
	overTop, overBottom bool

	spacer      string
	spacerWidth int
	spacerOK    bool

	version int64
}

// New returns an empty list.
func New(opts ...Option) *Model {
	m := &Model{styles: DefaultStyles(), ids: map[string]int{}, live: -1}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// SetStyles replaces the styles. It does not bump Version: owners call it
// every frame with their theme, and bump their own version when the
// theme changes.
func (m *Model) SetStyles(s Styles) {
	m.styles = s
	m.spacerOK = false
}

// Version increases whenever the list's own state changes in a way that
// can change the next frame (scrolling, selection).
func (m *Model) Version() int64 { return m.version }

// Entries returns the current entries. The slice is the list's own.
func (m *Model) Entries() []Entry { return m.entries }

// Offsets returns the absolute line each entry starts on.
func (m *Model) Offsets() []int { return m.offsets }

// Total returns the number of lines in all entries.
func (m *Model) Total() int { return m.total }

// Width returns the width of the current entries.
func (m *Model) Width() int { return m.width }

// Height returns the window height last set.
func (m *Model) Height() int { return m.height }

// YOffset returns the first visible line.
func (m *Model) YOffset() int { return m.yOffset }

// SetYOffset assigns the first visible line (clamped when eager). It does
// not bump Version, matching viewport.SetYOffset. It cancels HoldBottom.
func (m *Model) SetYOffset(n int) {
	m.holdBottom = false
	m.setY(n)
}

// IndexOf returns the index of the entry with id.
func (m *Model) IndexOf(id string) (int, bool) {
	i, ok := m.ids[id]
	return i, ok
}

// ResetSnap makes the next Layout snap the window to the cursor even if
// the cursor has not moved (used when the content is replaced). It
// cancels HoldBottom.
func (m *Model) ResetSnap() { m.hasSnapped, m.holdBottom = false, false }

// HoldBottom keeps a window that shows the last line now on the last
// line at the next Layout, however the entries change in between. An
// owner calls it before an entry grows (or shrinks) in place: an unfurl,
// an edit, a first reaction. Without it the offset stays and the new rows
// land below the window, since Layout snaps only when the cursor moves.
//
// "Now" is the screen as last laid out. While a re-snap is pending
// (ResetSnap) that screen is stale and the cursor positions the next
// frame, so HoldBottom does nothing. ScrollUp, SetYOffset, ResetSnap
// and Static cancel it (ScrollDown does not: from the bottom it can only
// ask for the bottom); a cursor move still snaps after it.
func (m *Model) HoldBottom() {
	if m.hasSnapped && m.atBottom() {
		m.holdBottom = true
	}
}

// atBottom reports whether the window reaches the last line.
func (m *Model) atBottom() bool {
	return m.total > 0 && m.height > 0 && m.yOffset+m.height >= m.total
}

// HasSnapped reports whether a Layout has snapped since the last
// ResetSnap.
func (m *Model) HasSnapped() bool { return m.hasSnapped }

// CursorSpan returns the last Layout's scroll target for the cursor:
// lines [start, end) of its entry, excluding Trail rows. Both are 0 when
// no entry matched.
func (m *Model) CursorSpan() (start, end int) { return m.selStart, m.selEnd }

// MarkSnapped records sel as already snapped, so the next Layout leaves
// the offset alone unless the cursor moves.
func (m *Model) MarkSnapped(sel int) { m.snapped, m.hasSnapped = sel, true }

// SetHeight sets this frame's window height. With WithPinBottomOnShrink, a
// window that was at the bottom and gets shorter moves down by the
// difference and marks sel snapped, so the cursor does not drag it back.
func (m *Model) SetHeight(h, sel int) {
	if m.pinBottom && m.height > 0 && h < m.height && m.total > 0 && m.yOffset+m.height >= m.total {
		m.yOffset += m.height - h
		m.MarkSnapped(sel)
	}
	m.prevHeight, m.height = m.height, h
}

// SetEntries replaces the entries, rendered at width. The list takes
// ownership of es.
//
// When keepPosition is true and the width changed, the scroll position is
// kept per the ReanchorPolicy. Owners pass true only when the previous
// layout is still a valid description of the screen (the channel pane:
// its render cache was not discarded; the thread pane: its view cache is
// valid).
func (m *Model) SetEntries(es []Entry, width int, keepPosition bool) {
	reanchor := keepPosition && m.width != width
	var bottom, anchored bool
	var ord, within int
	if reanchor {
		h := m.prevHeight
		if m.reanchor.BottomUsesNewHeight {
			h = m.height
		}
		bottom = m.total > 0 && m.yOffset+h >= m.total
		ord, within, anchored = m.lineAnchor(m.yOffset)
	}
	m.entries, m.width, m.live = es, width, -1
	m.recompute()
	if m.eager {
		m.setY(m.yOffset)
	}
	if !reanchor {
		return
	}
	switch {
	case bottom:
		m.setY(m.total)
	case anchored:
		if i, ok := m.nthAnchor(ord); ok {
			h := len(m.entries[i].Lines)
			within = max(min(within, h-1), 0)
			m.setY(m.offsets[i] + within)
		}
	}
}

// KeepLookups says that, until the next SetEntries, only the first n
// entries still describe the screen: EntryAt, text selection and anchor
// resolution ignore the rest. The scroll extent (offsets, total) is kept.
// Owners call it when they discard their render cache but have not
// rebuilt it yet (0 for the channel pane; 1 for the thread pane, whose
// parent entry stays current).
func (m *Model) KeepLookups(n int) { m.live = max(n, 0) }

// lookupLen is the number of entries lookups may see.
func (m *Model) lookupLen() int {
	if m.live < 0 || m.live > len(m.entries) {
		return len(m.entries)
	}
	return m.live
}

// ReplaceEntries swaps the entries at the given indexes, keeping every
// other entry (a partial rebuild). Offsets are recomputed once, after all
// of them; the position is not re-anchored.
func (m *Model) ReplaceEntries(updates map[int]Entry) {
	for i, e := range updates {
		if old := m.entries[i].ID; old != e.ID {
			if old != "" && m.ids[old] == i {
				delete(m.ids, old)
			}
			if e.ID != "" {
				m.ids[e.ID] = i
			}
		}
		m.entries[i] = e
	}
	m.recomputeOffsets()
}

// ScrollUp moves the window up n lines and marks sel snapped. Eager: a
// no-op at the top or with no content (Version still bumps).
func (m *Model) ScrollUp(n, sel int) {
	if n <= 0 {
		return
	}
	m.holdBottom = false
	if m.eager {
		if m.yOffset > 0 && m.total > 0 {
			m.setY(m.yOffset - n)
		}
	} else {
		m.yOffset = max(m.yOffset-n, 0)
	}
	m.MarkSnapped(sel)
	m.version++
}

// ScrollDown moves the window down n lines and marks sel snapped. Eager:
// a no-op at the bottom or with no content. Lazy: may overshoot until the
// next Layout clamps it.
func (m *Model) ScrollDown(n, sel int) {
	if n <= 0 {
		return
	}
	if m.eager {
		if m.yOffset < m.maxOffset() && m.total > 0 {
			m.setY(m.yOffset + n)
		}
	} else {
		m.yOffset += n
	}
	m.MarkSnapped(sel)
	m.version++
}

// ScrollHint reports whether a drag at content row contentY should
// auto-scroll: -1 at (or above) the top row, +1 at the bottom row, else 0.
func (m *Model) ScrollHint(contentY int) int {
	switch {
	case m.height <= 0:
		return 0
	case contentY <= 0:
		return -1
	case contentY >= m.height-1:
		return +1
	}
	return 0
}

func (m *Model) maxOffset() int { return max(0, m.total-m.height) }

func (m *Model) setY(n int) {
	if m.eager {
		n = min(max(n, 0), m.maxOffset())
	}
	m.yOffset = n
}

// clamp bounds the offset to [0, total-height] once per frame.
func (m *Model) clamp() {
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	if mx := max(0, m.total-m.height); m.yOffset > mx {
		m.yOffset = mx
	}
}

func (m *Model) recompute() {
	m.recomputeOffsets()
	clear(m.ids)
	for i := range m.entries {
		if id := m.entries[i].ID; id != "" {
			m.ids[id] = i
		}
	}
}

func (m *Model) recomputeOffsets() {
	m.offsets = m.offsets[:0]
	off := 0
	for i := range m.entries {
		m.offsets = append(m.offsets, off)
		off += len(m.entries[i].Lines)
	}
	m.total = off
}

// lineAnchor maps an absolute line to the anchor candidate covering it:
// its ordinal among candidates and the line's offset from its start.
func (m *Model) lineAnchor(line int) (ord, within int, ok bool) {
	best, n := -1, 0
	for i := range m.entries {
		if m.reanchor.AnchorRefsOnly && m.entries[i].Ref < 0 {
			continue
		}
		if m.offsets[i] > line {
			break
		}
		best, ord = i, n
		n++
	}
	if best < 0 {
		return 0, 0, false
	}
	return ord, line - m.offsets[best], true
}

// nthAnchor returns the index of the ord-th anchor candidate.
func (m *Model) nthAnchor(ord int) (int, bool) {
	n := 0
	for i := range m.entries {
		if m.reanchor.AnchorRefsOnly && m.entries[i].Ref < 0 {
			continue
		}
		if n == ord {
			return i, true
		}
		n++
	}
	return 0, false
}
