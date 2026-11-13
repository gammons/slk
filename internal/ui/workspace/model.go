package workspace

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
)

type WorkspaceItem struct {
	ID        string
	Name      string
	Initials  string
	HasUnread bool
}

type Model struct {
	items    []WorkspaceItem
	selected int
	version  int64
	// cursor is the keyboard highlight while the rail has focus (h from
	// the sidebar); it starts on the selected workspace and Enter
	// switches to it. focused draws it.
	cursor  int
	focused bool
	// height is the rail's drawn height (SetHeight, every render) and
	// offset the first item drawn: when the items don't fit, the rail
	// scrolls to keep the cursor (focused) or the selected workspace
	// on screen.
	height int
	offset int
	// unreadReader returns the set of workspace IDs whose dot should
	// be lit: those with at least one channel their own sidebar would
	// show as unread (mute-filtered) or one unread subscribed thread
	// (what the sidebar's Threads badge counts); see
	// railUnreadWorkspaces in cmd/slk. Set by App via SetUnreadReader;
	// called only by RefreshUnreads, since it queries SQLite on the UI
	// goroutine.
	unreadReader func() []string
	// lastUnread is the set the last RefreshUnreads read, which
	// OtherUnreadCount counts.
	lastUnread []string
}

// Version returns a counter that increments any time the View() output could
// change.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

func New(items []WorkspaceItem, selected int) Model {
	return Model{items: items, selected: selected}
}

func (m *Model) SelectedID() string {
	if len(m.items) == 0 {
		return ""
	}
	return m.items[m.selected].ID
}

// NameByID returns the workspace's display name, or "" if no item with
// the given ID is present. Used by the App to derive the window title's
// initials via WorkspaceInitials.
func (m *Model) NameByID(id string) string {
	for _, item := range m.items {
		if item.ID == id {
			return item.Name
		}
	}
	return ""
}

// OtherUnreadCount returns the number of workspaces with unreads,
// excluding activeID, as of the last RefreshUnreads; 0 before any. It
// does not call the reader itself, so a refresh followed by this call
// reads it once. Deciding what counts is the reader's job,
// not this method's: it applies the sidebar's IsVisiblyUnread predicate
// per workspace and asks the same thread query the Threads badge uses,
// so the title's "+N" and the rail dots agree with each workspace's
// own sidebar. Note the active workspace's "(N)" and $SLK_UNREAD count
// channels only; "+N" counts workspaces, and a workspace whose only
// unread is a thread counts.
func (m *Model) OtherUnreadCount(activeID string) int {
	count := 0
	for _, id := range m.lastUnread {
		if id != activeID {
			count++
		}
	}
	return count
}

func (m *Model) SelectedIndex() int {
	return m.selected
}

func (m *Model) Select(idx int) {
	if idx >= 0 && idx < len(m.items) && m.selected != idx {
		m.selected = idx
		m.dirty()
		m.scroll()
	}
}

// Items returns the rail's workspaces with their current HasUnread
// flags. The slice is the rail's own; callers must not modify it.
func (m *Model) Items() []WorkspaceItem { return m.items }

func (m *Model) SetItems(items []WorkspaceItem) {
	m.items = items
	if m.selected >= len(items) {
		m.selected = 0
	}
	if m.cursor >= len(items) {
		m.cursor = 0
	}
	m.dirty()
	m.scroll()
}

func (m *Model) SelectByID(teamID string) {
	for i, item := range m.items {
		if item.ID == teamID {
			if m.selected != i {
				m.selected = i
				m.dirty()
				m.scroll()
			}
			return
		}
	}
}

func (m *Model) SetUnread(teamID string, hasUnread bool) {
	for i := range m.items {
		if m.items[i].ID == teamID {
			if m.items[i].HasUnread != hasUnread {
				m.items[i].HasUnread = hasUnread
				m.dirty()
			}
			return
		}
	}
}

// SetFocused sets whether the rail has keyboard focus, which draws the
// cursor marker.
func (m *Model) SetFocused(focused bool) {
	if m.focused != focused {
		m.focused = focused
		m.dirty()
		m.scroll()
	}
}

// SetHeight records the rail's drawn height (View's height), which
// decides how many items fit and so where the rail scrolls to.
func (m *Model) SetHeight(height int) {
	if m.height != height {
		m.height = height
		m.dirty()
		m.scroll()
	}
}

// fit is how many items the rail draws: View pads one row above and
// below and puts a blank row between items. 0 before SetHeight, meaning
// all of them.
func (m *Model) fit() int {
	if m.height <= 0 {
		return len(m.items)
	}
	return max(1, (m.height-2+1)/2)
}

// scroll moves offset the least that keeps the cursor (while focused)
// or the selected workspace among the drawn items.
func (m *Model) scroll() {
	target := m.selected
	if m.focused {
		target = m.cursor
	}
	n := m.fit()
	off := m.offset
	if target < off {
		off = target
	}
	if target >= off+n {
		off = target - n + 1
	}
	off = max(0, min(off, len(m.items)-n))
	if off != m.offset {
		m.offset = off
		m.dirty()
	}
}

// ResetCursor puts the cursor on the selected workspace.
func (m *Model) ResetCursor() { m.SetCursor(m.selected) }

// SetCursor moves the cursor to idx, clamped to the items.
func (m *Model) SetCursor(idx int) {
	idx = max(0, min(idx, len(m.items)-1))
	if idx >= 0 && m.cursor != idx {
		m.cursor = idx
		m.dirty()
		m.scroll()
	}
}

// MoveCursor moves the cursor by delta, clamped to the items.
func (m *Model) MoveCursor(delta int) { m.SetCursor(m.cursor + delta) }

// CursorID returns the workspace under the cursor, or "" with no items.
func (m *Model) CursorID() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].ID
}

// SetUnreadReader installs the callback used by RefreshUnreads.
func (m *Model) SetUnreadReader(f func() []string) {
	m.unreadReader = f
	if f == nil {
		m.lastUnread = nil
	}
}

// RefreshUnreads pulls the latest set of workspaces-with-unreads from
// the reader and updates each item's HasUnread field. Called by App
// on ReadStateChangedMsg. No-op if no reader is installed.
func (m *Model) RefreshUnreads() {
	if m.unreadReader == nil {
		return
	}
	m.lastUnread = m.unreadReader()
	set := make(map[string]bool, len(m.items))
	for _, id := range m.lastUnread {
		set[id] = true
	}
	changed := false
	for i := range m.items {
		want := set[m.items[i].ID]
		if m.items[i].HasUnread != want {
			m.items[i].HasUnread = want
			changed = true
		}
	}
	if changed {
		m.dirty()
	}
}

func (m Model) View(height int) string {
	if len(m.items) == 0 {
		return ""
	}

	var rows []string
	end := min(len(m.items), m.offset+m.fit())
	for i := m.offset; i < end; i++ {
		item := m.items[i]
		var style lipgloss.Style
		if i == m.selected {
			style = styles.WorkspaceActive
		} else {
			style = styles.WorkspaceInactive
		}

		// A one-column slot on each side of the tile, the unread dot
		// in the left one and the keyboard cursor in the right, so
		// every row is the same width with or without them and
		// centering never shifts the initials. The slots carry the
		// rail background themselves: one after the tile follows its
		// reset and would otherwise show the terminal's.
		slot := styles.PresenceOnline.Background(styles.RailBackground)
		left := slot.Render(" ")
		if item.HasUnread && i != m.selected {
			left = slot.Render("●")
		}
		right := slot.Render(" ")
		if m.focused && i == m.cursor {
			right = slot.Render("◂")
		}
		rows = append(rows, left+style.Render(item.Initials)+right)
	}

	content := strings.Join(rows, "\n\n")

	// Height/MaxHeight in lipgloss include padding in the total,
	// so use the full height directly. Padding(1,0) adds 1 row
	// top + 1 row bottom inside that total, matching the visual
	// offset of RoundedBorder() on adjacent panels.
	rail := lipgloss.NewStyle().
		Width(6).
		Height(height).
		MaxHeight(height).
		Background(styles.RailBackground).
		Padding(1, 0).
		Align(lipgloss.Center).
		Render(content)

	return rail
}

// ClickAt returns the workspace item rendered at rail-local row y,
// or ok=false when the click landed on a padding row, a gap between
// items, or past the last item.
//
// Row layout mirrors View(): Padding(1,0) puts blank padding at row 0,
// and items are "\n\n"-joined so they occupy rows 1, 3, 5, ... with
// blank gap rows at 2, 4, 6, ..., starting from the first item drawn
// (offset) when the rail is scrolled. There is no horizontal column check
// because the rail has no border and uses its full 6-col width as the
// click target.
func (m Model) ClickAt(y int) (WorkspaceItem, bool) {
	if y < 1 || len(m.items) == 0 {
		return WorkspaceItem{}, false
	}
	rel := y - 1
	if rel%2 != 0 {
		return WorkspaceItem{}, false // gap between items
	}
	if rel/2 >= m.fit() {
		return WorkspaceItem{}, false // below the last item drawn
	}
	idx := m.offset + rel/2
	if idx < 0 || idx >= len(m.items) {
		return WorkspaceItem{}, false
	}
	return m.items[idx], true
}

func (m Model) Width() int {
	return 6 // 6 content, no border
}

func WorkspaceInitials(name string) string {
	words := strings.Fields(name)
	switch len(words) {
	case 0:
		return "?"
	case 1:
		if len(words[0]) >= 2 {
			return strings.ToUpper(words[0][:2])
		}
		return strings.ToUpper(words[0])
	default:
		return strings.ToUpper(fmt.Sprintf("%c%c", words[0][0], words[1][0]))
	}
}
