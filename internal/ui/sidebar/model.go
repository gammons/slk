package sidebar

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// Default section names used when an item has no custom Section assigned.
// These always sort after any user-defined custom sections.
const (
	defaultChannelsSection = "Channels"
	defaultDMSection       = "Direct Messages"
)

type ChannelItem struct {
	ID            string
	Name          string
	Type          string // channel, dm, group_dm, private
	Section       string // section name for grouping (e.g. "Engineering", "Starred")
	SectionOrder  int    // sort order from config; lower = higher in sidebar (custom sections only)
	UnreadCount   int
	IsStarred     bool
	Presence      string // for DMs: active, away, dnd
	DMUserID      string // for DMs: the user ID of the other party
}

// sectionFor returns the section name an item belongs to, applying default
// fallback rules for items that have no explicit Section set.
func sectionFor(item ChannelItem) string {
	if item.Section != "" {
		return item.Section
	}
	if item.Type == "dm" || item.Type == "group_dm" {
		return defaultDMSection
	}
	return defaultChannelsSection
}

// orderedSections returns the section names in display order given the
// currently filtered items. Custom (user-defined) sections come first,
// sorted by SectionOrder ascending then by first-appearance for ties.
// The two built-in fallback sections ("Channels", "Direct Messages") are
// always appended at the end in that order, so DMs are always last.
func orderedSections(items []ChannelItem, filtered []int) []string {
	type customInfo struct {
		name      string
		order     int
		firstSeen int
	}
	var customs []customInfo
	customSeen := map[string]int{} // name -> index into customs
	hasChannels := false
	hasDMs := false

	for pos, idx := range filtered {
		item := items[idx]
		name := sectionFor(item)
		switch {
		case item.Section != "":
			if existing, ok := customSeen[name]; ok {
				// Prefer the smallest SectionOrder seen across items in this section.
				if item.SectionOrder < customs[existing].order {
					customs[existing].order = item.SectionOrder
				}
				continue
			}
			customSeen[name] = len(customs)
			customs = append(customs, customInfo{
				name:      name,
				order:     item.SectionOrder,
				firstSeen: pos,
			})
		case name == defaultDMSection:
			hasDMs = true
		default:
			hasChannels = true
		}
	}

	sort.SliceStable(customs, func(i, j int) bool {
		if customs[i].order != customs[j].order {
			return customs[i].order < customs[j].order
		}
		return customs[i].firstSeen < customs[j].firstSeen
	})

	out := make([]string, 0, len(customs)+2)
	for _, c := range customs {
		out = append(out, c.name)
	}
	if hasChannels {
		out = append(out, defaultChannelsSection)
	}
	if hasDMs {
		out = append(out, defaultDMSection)
	}
	return out
}

type Model struct {
	items    []ChannelItem
	selected int
	yOffset  int // own scroll state -- replaces bubbles/viewport
	filter   string
	filtered []int // indices into items that match filter

	// snappedSelection lets View() avoid snapping yOffset back to the selected
	// row on every render. While snappedSelection == selected, mouse-wheel /
	// programmatic scrolls (ScrollUp/ScrollDown) are preserved.
	snappedSelection int
	hasSnapped       bool

	// version increments on every state change that could alter the rendered
	// View() output. The App layer caches the WRAPPED panel output (border +
	// exactSize) keyed on version + layout, so on compose keystrokes (where
	// version is unchanged) we reuse the previous frame's wrapped string.
	version int64

	// Render cache. cacheRows holds the pre-rendered (normal / selected) string
	// variants for every visible row including section headers and inter-section
	// blanks. Each row is exactly one rendered line, so we can build the visible
	// window by slicing this slice -- no string parsing, no width measurement.
	cacheRows   []renderRow
	cacheValid  bool
	cacheWidth  int
	cacheFiller string // pre-rendered empty row for vertical padding

	// Synthetic "Threads" row state. The Threads row is rendered at the very
	// top of the sidebar (above all sections). It is selectable via j/k like a
	// channel, but it is NOT a channel — when threadsSelected is true,
	// SelectedItem/SelectedID return zero / empty and the App layer should
	// activate the threads view instead of opening a channel.
	//
	// threadsSelected is intentionally NOT modified by SetItems: callers that
	// want a fresh default selection on a major context change (e.g. workspace
	// switch) must explicitly call SelectThreadsRow() after SetItems. This
	// keeps routine refreshes (e.g. presence updates that re-call SetItems)
	// from clobbering the user's current selection.
	threadsUnread   int
	threadsSelected bool
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes.
func (m *Model) InvalidateCache() {
	m.cacheValid = false
	m.version++
}

// Version returns a counter that increments any time the View() output could
// change. Callers can compare against a previously-seen version to know
// whether to recompute downstream layout / wrapping.
func (m *Model) Version() int64 { return m.version }

// dirty bumps the version. Called from every state-mutating method.
func (m *Model) dirty() { m.version++ }

func New(items []ChannelItem) Model {
	m := Model{items: items}
	// Default selection is the synthetic Threads row at the top of the sidebar.
	m.threadsSelected = true
	m.rebuildFilter()
	return m
}

// IsThreadsSelected reports whether the synthetic "Threads" row is currently
// the selected entry in the sidebar.
func (m *Model) IsThreadsSelected() bool { return m.threadsSelected }

// SelectThreadsRow explicitly moves selection to the synthetic Threads row.
func (m *Model) SelectThreadsRow() {
	if !m.threadsSelected {
		m.threadsSelected = true
		m.dirty()
	}
}

// SetThreadsUnreadCount updates the badge count shown next to the Threads row.
// Invalidates the render cache when the count changes.
func (m *Model) SetThreadsUnreadCount(n int) {
	if n < 0 {
		n = 0
	}
	if m.threadsUnread != n {
		m.threadsUnread = n
		m.cacheValid = false
		m.dirty()
	}
}

// ThreadsUnreadCount returns the current Threads-row unread badge count.
func (m *Model) ThreadsUnreadCount() int { return m.threadsUnread }

// SetItems replaces the sidebar's channel list. It clamps m.selected to the
// new filtered range but does NOT touch threadsSelected: SetItems is called on
// every routine refresh (presence updates, unread changes, channel-list
// resync, etc.) and clobbering selection on those refreshes would be wrong.
// Callers that want to reset selection to the default Threads row on a major
// context change (e.g. workspace switch) should explicitly call
// SelectThreadsRow() after SetItems.
func (m *Model) SetItems(items []ChannelItem) {
	m.items = items
	m.rebuildFilter()
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
	m.cacheValid = false
	m.dirty()
}

func (m *Model) SelectedID() string {
	if m.threadsSelected {
		return ""
	}
	if len(m.filtered) == 0 {
		return ""
	}
	idx := m.filtered[m.selected]
	return m.items[idx].ID
}

func (m *Model) SelectedItem() (ChannelItem, bool) {
	if m.threadsSelected {
		return ChannelItem{}, false
	}
	if len(m.filtered) == 0 {
		return ChannelItem{}, false
	}
	idx := m.filtered[m.selected]
	return m.items[idx], true
}

func (m *Model) MoveDown() {
	// From the synthetic Threads row, MoveDown lands on the first channel.
	if m.threadsSelected {
		if len(m.filtered) > 0 {
			m.threadsSelected = false
			m.selected = 0
			m.dirty()
		}
		return
	}
	if m.selected < len(m.filtered)-1 {
		m.selected++
		m.dirty()
	}
}

func (m *Model) MoveUp() {
	// Already on the Threads row -- no-op.
	if m.threadsSelected {
		return
	}
	// From the first channel, MoveUp returns to the synthetic Threads row.
	if m.selected == 0 {
		m.threadsSelected = true
		m.dirty()
		return
	}
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

// ScrollUp moves the viewport up by n rows without changing the selection.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.dirty()
}

// ScrollDown moves the viewport down by n rows. The next View() clamps to the
// max valid offset for the current content height.
func (m *Model) ScrollDown(n int) {
	if n > 0 {
		m.yOffset += n
		m.dirty()
	}
}

func (m *Model) GoToTop() {
	// Top of the sidebar is the synthetic Threads row.
	m.threadsSelected = true
	m.selected = 0
	m.dirty()
}

func (m *Model) GoToBottom() {
	if len(m.filtered) > 0 {
		m.threadsSelected = false
		m.selected = len(m.filtered) - 1
		m.dirty()
	}
}

// Items returns all channel items.
func (m *Model) Items() []ChannelItem {
	return m.items
}

func (m *Model) SetFilter(filter string) {
	m.filter = filter
	m.selected = 0
	m.rebuildFilter()
	m.cacheValid = false
	m.dirty()
}

func (m *Model) VisibleItems() []ChannelItem {
	var result []ChannelItem
	for _, idx := range m.filtered {
		result = append(result, m.items[idx])
	}
	return result
}

// ClearUnread sets the unread count to 0 for the given channel.
func (m *Model) ClearUnread(channelID string) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			if m.items[i].UnreadCount != 0 {
				m.items[i].UnreadCount = 0
				m.cacheValid = false
				m.dirty()
			}
			return
		}
	}
}

// UpdatePresenceByUser updates the presence for any DM item whose DMUserID matches.
func (m *Model) UpdatePresenceByUser(userID, presence string) {
	for i := range m.items {
		if m.items[i].DMUserID == userID {
			if m.items[i].Presence != presence {
				m.items[i].Presence = presence
				m.cacheValid = false
				m.dirty()
			}
			return
		}
	}
}

func (m *Model) SelectByID(id string) {
	for i, idx := range m.filtered {
		if m.items[idx].ID == id {
			if m.selected != i || m.threadsSelected {
				m.selected = i
				m.threadsSelected = false
				m.dirty()
			}
			return
		}
	}
}

func (m *Model) rebuildFilter() {
	m.filtered = nil
	lower := strings.ToLower(m.filter)
	for i, item := range m.items {
		if m.filter == "" || strings.Contains(strings.ToLower(item.Name), lower) {
			m.filtered = append(m.filtered, i)
		}
	}

	// Sort filtered indices to match the visual section display order so that
	// j/k navigation traverses items in the same order they're rendered.
	// Within a section, preserve the original (Slack-provided) item order.
	sectionOrder := orderedSections(m.items, m.filtered)
	rank := make(map[string]int, len(sectionOrder))
	for i, name := range sectionOrder {
		rank[name] = i
	}
	sort.SliceStable(m.filtered, func(a, b int) bool {
		ra := rank[sectionFor(m.items[m.filtered[a]])]
		rb := rank[sectionFor(m.items[m.filtered[b]])]
		if ra != rb {
			return ra < rb
		}
		return m.filtered[a] < m.filtered[b]
	})
}

// renderRow describes a single rendered row in the sidebar.
//
// For channel rows we pre-render BOTH the selected and unselected variants in
// buildCache so that selection movement (j/k) needs no lipgloss work in View().
// For section headers and inter-section blanks the two variants are identical.
type renderRow struct {
	normal    string // rendered as a non-selected row
	selected  string // rendered with the selection cursor + selected style
	height    int    // rendered terminal height (always 1 for headers/blanks)
	filterIdx int    // index into m.filtered, or -1 for headers/blanks
	isThreads bool   // true for the synthetic "Threads" row at the top
}

// buildCache rebuilds m.cacheRows for the given width. Expensive; runs only
// when items, filter, width, or theme change.
func (m *Model) buildCache(width int) {
	m.cacheValid = true
	m.cacheWidth = width
	m.cacheRows = m.cacheRows[:0]
	m.cacheFiller = lipgloss.NewStyle().Width(width).Background(styles.SidebarBackground).Render("")

	// Build all rows: section headers + channel items.
	type sectionGroup struct {
		name string
		rows []renderRow
	}
	sectionOrder := orderedSections(m.items, m.filtered)
	sectionMap := map[string]*sectionGroup{}
	for _, name := range sectionOrder {
		sectionMap[name] = &sectionGroup{name: name}
	}

	// Combine sidebar bg + fg so styled glyphs (private/DM prefixes, cursor,
	// unread dots) restore both colors after their ANSI reset.
	bgAnsi := messages.SidebarBgANSI() + messages.SidebarFgANSI() // compute once outside loop

	// Style objects allocated once per cache build.
	cursorStyle := lipgloss.NewStyle().Foreground(styles.Accent)
	dotStyle := lipgloss.NewStyle().Foreground(styles.Primary)
	privateStyle := lipgloss.NewStyle().Foreground(styles.Warning)

	cursorSelected := cursorStyle.Render("▌")
	unreadDotStr := dotStyle.Render("●")
	privatePrefix := privateStyle.Render("◆ ")
	dmActivePrefix := styles.PresenceOnline.Render("● ")
	dmAwayPrefix := styles.PresenceAway.Render("○ ")
	groupDMPrefix := styles.PresenceAway.Render("● ")

	// Synthetic "Threads" row, always rendered at the very top of the sidebar
	// (before any section). Selectable like a channel; the App layer activates
	// the threads view when IsThreadsSelected() is true.
	threadsLabel := " ⚑ Threads"
	threadsCursor := cursorSelected + "⚑ Threads"
	if m.threadsUnread > 0 {
		// Render "•N" as a single styled span so the dot glyph and the digits
		// stay adjacent in the output (no ANSI reset splits them). Tests rely
		// on the literal substring "•N" being searchable in View() output.
		badge := " " + dotStyle.Render("•"+fmt.Sprintf("%d", m.threadsUnread))
		threadsLabel += badge
		threadsCursor += badge
	}
	threadsLabel = messages.ReapplyBgAfterResets(threadsLabel, bgAnsi)
	threadsCursor = messages.ReapplyBgAfterResets(threadsCursor, bgAnsi)
	threadsNormal := styles.ChannelNormal.Width(width - 2).Render(threadsLabel)
	threadsSelectedRow := styles.ChannelSelected.Width(width - 2).Render(threadsCursor)
	m.cacheRows = append(m.cacheRows, renderRow{
		normal:    threadsNormal,
		selected:  threadsSelectedRow,
		height:    1,
		filterIdx: -1,
		isThreads: true,
	})
	// Blank separator between the Threads row and the first section (or below
	// the Threads row when there are no channels at all).
	m.cacheRows = append(m.cacheRows, renderRow{height: 1, filterIdx: -1})

	for fi, idx := range m.filtered {
		item := m.items[idx]

		// Unread dot indicator (same regardless of selection state).
		unreadDot := " "
		if item.UnreadCount > 0 {
			unreadDot = unreadDotStr
		}

		var prefix string
		switch item.Type {
		case "dm":
			if item.Presence == "active" {
				prefix = dmActivePrefix
			} else {
				prefix = dmAwayPrefix
			}
		case "group_dm":
			prefix = groupDMPrefix
		case "private":
			prefix = privatePrefix
		default:
			prefix = "# "
		}

		// Truncate name to fit sidebar width.
		// Unicode chars like ● (U+25CF), ○, ◆, ▌ have East Asian Width
		// "Ambiguous" — terminals may render them as 2 columns wide, but
		// lipgloss.Width() reports them as 1. We can't trust lipgloss
		// measurements for these chars, so use a conservative fixed budget:
		//   cursor(2) + prefix(3) + name + space(1) + dot(2) = name + 8
		// This assumes worst-case 2-col rendering for every ambiguous char.
		name := item.Name
		maxNameLen := (width - 2) - 8
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if lipgloss.Width(name) > maxNameLen {
			name = truncate.StringWithTail(name, uint(maxNameLen), "…")
		}

		// Two label variants: selected (with cursor glyph) and normal (with space).
		labelNormal := " " + prefix + name + " " + unreadDot
		labelSelected := cursorSelected + prefix + name + " " + unreadDot

		// Re-apply theme background after ANSI resets from inline styled
		// glyphs (cursor, prefix, unread dot) so the outer channel style's
		// background isn't interrupted.
		labelNormal = messages.ReapplyBgAfterResets(labelNormal, bgAnsi)
		labelSelected = messages.ReapplyBgAfterResets(labelSelected, bgAnsi)

		// Pick base style for non-selected state.
		var baseStyle lipgloss.Style
		if item.UnreadCount > 0 {
			baseStyle = styles.ChannelUnread
		} else {
			baseStyle = styles.ChannelNormal
		}

		rowNormal := baseStyle.Width(width - 2).Render(labelNormal)
		rowSelected := styles.ChannelSelected.Width(width - 2).Render(labelSelected)

		sectionName := sectionFor(item)
		sectionMap[sectionName].rows = append(sectionMap[sectionName].rows, renderRow{
			normal:    rowNormal,
			selected:  rowSelected,
			height:    1, // every channel row is exactly one line
			filterIdx: fi,
		})
	}

	// When there are no channel items at all, render a single muted
	// "No channels" placeholder below the Threads row + separator so the
	// Threads row remains globally visible even on an empty workspace.
	if len(m.items) == 0 {
		placeholder := styles.SectionHeader.Render("No channels")
		m.cacheRows = append(m.cacheRows, renderRow{
			normal:    placeholder,
			selected:  placeholder,
			height:    1,
			filterIdx: -1,
		})
	}

	// Flatten into a single row list with section headers.
	// Add a blank line between sections for visual separation.
	for i, name := range sectionOrder {
		if i > 0 {
			m.cacheRows = append(m.cacheRows, renderRow{height: 1, filterIdx: -1})
		}
		group := sectionMap[name]
		header := styles.SectionHeader.Render(group.name)
		m.cacheRows = append(m.cacheRows, renderRow{
			normal:    header,
			selected:  header,
			height:    1,
			filterIdx: -1,
		})
		m.cacheRows = append(m.cacheRows, group.rows...)
	}
}

func (m *Model) View(height, width int) string {
	// Note: we no longer early-return on len(m.items)==0. The synthetic
	// Threads row is globally present (even on an empty workspace), so we
	// always go through buildCache, which handles the empty-items case by
	// emitting a muted "No channels" placeholder below the Threads row.
	if !m.cacheValid || m.cacheWidth != width {
		m.buildCache(width)
	}

	// Each cacheRow is exactly one rendered line, so the line index of a row
	// is just its slice index. Find the selected row's line. The synthetic
	// Threads row is identified via r.isThreads when threadsSelected is set.
	selectedLine := -1
	for i, r := range m.cacheRows {
		if m.threadsSelected && r.isThreads {
			selectedLine = i
			break
		}
		if !m.threadsSelected && r.filterIdx == m.selected {
			selectedLine = i
			break
		}
	}

	// Snap yOffset to keep the selected row visible only when the selection
	// has actually changed since the last snap. This preserves mouse-wheel /
	// programmatic scroll positions across renders. We use a sentinel of -1
	// to represent "currently on the synthetic Threads row" so the snapped
	// state is distinct from any real channel index.
	currentSelection := m.selected
	if m.threadsSelected {
		currentSelection = -1
	}
	if selectedLine >= 0 && (!m.hasSnapped || m.snappedSelection != currentSelection) {
		if selectedLine >= m.yOffset+height {
			m.yOffset = selectedLine - height + 1
		}
		if selectedLine < m.yOffset {
			m.yOffset = selectedLine
		}
		m.snappedSelection = currentSelection
		m.hasSnapped = true
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	maxOffset := len(m.cacheRows) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}

	// Build visible window by slicing cacheRows. No lipgloss work per frame.
	end := m.yOffset + height
	if end > len(m.cacheRows) {
		end = len(m.cacheRows)
	}

	visible := make([]string, 0, height)
	for i := m.yOffset; i < end; i++ {
		r := m.cacheRows[i]
		switch {
		case r.isThreads:
			if m.threadsSelected {
				visible = append(visible, r.selected)
			} else {
				visible = append(visible, r.normal)
			}
		case !m.threadsSelected && r.filterIdx == m.selected:
			visible = append(visible, r.selected)
		case r.normal == "":
			// Inter-section blank row -- emit a width-sized themed blank so
			// the panel background remains continuous.
			visible = append(visible, m.cacheFiller)
		default:
			visible = append(visible, r.normal)
		}
	}
	for len(visible) < height {
		visible = append(visible, m.cacheFiller)
	}

	return strings.Join(visible, "\n")
}

// ClickAt handles a mouse click at the given y-coordinate (relative to sidebar content top).
// Selects the item at that position. Returns the item and true if a selectable item was clicked.
func (m *Model) ClickAt(y int) (ChannelItem, bool) {
	absoluteY := y + m.yOffset

	// y=0 is the synthetic Threads row; y=1 is the blank separator before the
	// first section. A click on y=0 selects/keeps the Threads row.
	if absoluteY == 0 {
		if !m.threadsSelected {
			m.threadsSelected = true
			m.dirty()
		}
		// Caller (App) consults IsThreadsSelected -- no ChannelItem to return.
		return ChannelItem{}, false
	}
	// Explicit no-op for the Threads-row separator at y=1. Inter-section
	// blank rows further down are also no-ops, but via fall-through: the
	// per-section loop below never matches a blank row's y, so the function
	// just returns (ChannelItem{}, false) at the end. This explicit branch
	// just makes the parity for the Threads separator obvious.
	if absoluteY == 1 {
		return ChannelItem{}, false
	}

	// Rebuild the section structure (same logic as View) to map y to filterIdx.
	// Each channel item = 1 line, each section header = 1 line, blank line between sections.
	sectionOrder := orderedSections(m.items, m.filtered)
	sectionMap := map[string][]int{} // section name -> list of filter indices

	for fi, idx := range m.filtered {
		item := m.items[idx]
		sectionName := sectionFor(item)
		sectionMap[sectionName] = append(sectionMap[sectionName], fi)
	}

	// Skip the Threads row + its trailing blank separator (2 lines).
	currentLine := 2
	for i, name := range sectionOrder {
		if i > 0 {
			currentLine++ // blank line between sections
		}
		currentLine++ // section header line

		for _, fi := range sectionMap[name] {
			if currentLine == absoluteY {
				if m.selected != fi || m.threadsSelected {
					m.selected = fi
					m.threadsSelected = false
					m.dirty()
				}
				idx := m.filtered[fi]
				return m.items[idx], true
			}
			currentLine++
		}
	}
	return ChannelItem{}, false
}

func (m Model) Width() int {
	return 30
}
