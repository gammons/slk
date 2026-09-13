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
	// unreadReader returns the set of workspace IDs whose dot should
	// be lit: those with at least one channel their own sidebar would
	// show as unread (mute-filtered; see railUnreadWorkspaces in
	// cmd/slk). Set by App via SetUnreadReader; called by
	// RefreshUnreads and OtherUnreadCount.
	unreadReader func() []string
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
// excluding activeID. Reads through the installed unreadReader; returns
// 0 when no reader is set. Mute filtering is the reader's job, not
// this method's: the reader applies the sidebar's IsVisiblyUnread
// predicate per workspace, so the title's "+N", the rail dots and the
// active workspace's "(N)" all agree on what counts as unread.
func (m *Model) OtherUnreadCount(activeID string) int {
	if m.unreadReader == nil {
		return 0
	}
	count := 0
	for _, id := range m.unreadReader() {
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
	}
}

func (m *Model) SetItems(items []WorkspaceItem) {
	m.items = items
	if m.selected >= len(items) {
		m.selected = 0
	}
	m.dirty()
}

func (m *Model) SelectByID(teamID string) {
	for i, item := range m.items {
		if item.ID == teamID {
			if m.selected != i {
				m.selected = i
				m.dirty()
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

// SetUnreadReader installs the callback used by RefreshUnreads.
func (m *Model) SetUnreadReader(f func() []string) {
	m.unreadReader = f
}

// RefreshUnreads pulls the latest set of workspaces-with-unreads from
// the reader and updates each item's HasUnread field. Called by App
// on ReadStateChangedMsg. No-op if no reader is installed.
func (m *Model) RefreshUnreads() {
	if m.unreadReader == nil {
		return
	}
	set := make(map[string]bool, len(m.items))
	for _, id := range m.unreadReader() {
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
	for i, item := range m.items {
		var style lipgloss.Style
		if i == m.selected {
			style = styles.WorkspaceActive
		} else {
			style = styles.WorkspaceInactive
		}

		initials := item.Initials
		if item.HasUnread && i != m.selected {
			initials = initials + styles.PresenceOnline.Render("●")
		}
		label := style.Render(initials)
		rows = append(rows, label)
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
// blank gap rows at 2, 4, 6, .... There is no horizontal column check
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
	idx := rel / 2
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
