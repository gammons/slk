package sidebar

import (
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

	// Render cache. cacheRows holds the pre-rendered (normal / selected) string
	// variants for every visible row including section headers and inter-section
	// blanks. Each row is exactly one rendered line, so we can build the visible
	// window by slicing this slice -- no string parsing, no width measurement.
	cacheRows   []renderRow
	cacheValid  bool
	cacheWidth  int
	cacheFiller string // pre-rendered empty row for vertical padding
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes.
func (m *Model) InvalidateCache() {
	m.cacheValid = false
}

func New(items []ChannelItem) Model {
	m := Model{items: items}
	m.rebuildFilter()
	return m
}

func (m *Model) SetItems(items []ChannelItem) {
	m.items = items
	m.rebuildFilter()
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
	m.cacheValid = false
}

func (m *Model) SelectedID() string {
	if len(m.filtered) == 0 {
		return ""
	}
	idx := m.filtered[m.selected]
	return m.items[idx].ID
}

func (m *Model) SelectedItem() (ChannelItem, bool) {
	if len(m.filtered) == 0 {
		return ChannelItem{}, false
	}
	idx := m.filtered[m.selected]
	return m.items[idx], true
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
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
}

// ScrollDown moves the viewport down by n rows. The next View() clamps to the
// max valid offset for the current content height.
func (m *Model) ScrollDown(n int) {
	if n > 0 {
		m.yOffset += n
	}
}

func (m *Model) GoToTop() {
	m.selected = 0
}

func (m *Model) GoToBottom() {
	if len(m.filtered) > 0 {
		m.selected = len(m.filtered) - 1
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
			}
			return
		}
	}
}

func (m *Model) SelectByID(id string) {
	for i, idx := range m.filtered {
		if m.items[idx].ID == id {
			m.selected = i
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
	if len(m.items) == 0 {
		return lipgloss.NewStyle().Width(width).Height(height).Render("No channels")
	}

	if !m.cacheValid || m.cacheWidth != width {
		m.buildCache(width)
	}

	// Each cacheRow is exactly one rendered line, so the line index of a row
	// is just its slice index. Find the selected row's line.
	selectedLine := -1
	for i, r := range m.cacheRows {
		if r.filterIdx == m.selected {
			selectedLine = i
			break
		}
	}

	// Snap yOffset to keep the selected row visible only when the selection
	// has actually changed since the last snap. This preserves mouse-wheel /
	// programmatic scroll positions across renders.
	if selectedLine >= 0 && (!m.hasSnapped || m.snappedSelection != m.selected) {
		if selectedLine >= m.yOffset+height {
			m.yOffset = selectedLine - height + 1
		}
		if selectedLine < m.yOffset {
			m.yOffset = selectedLine
		}
		m.snappedSelection = m.selected
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
		if r.filterIdx == m.selected {
			visible = append(visible, r.selected)
		} else if r.normal == "" {
			// Inter-section blank row -- emit a width-sized themed blank so
			// the panel background remains continuous.
			visible = append(visible, m.cacheFiller)
		} else {
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

	// Rebuild the section structure (same logic as View) to map y to filterIdx.
	// Each channel item = 1 line, each section header = 1 line, blank line between sections.
	sectionOrder := orderedSections(m.items, m.filtered)
	sectionMap := map[string][]int{} // section name -> list of filter indices

	for fi, idx := range m.filtered {
		item := m.items[idx]
		sectionName := sectionFor(item)
		sectionMap[sectionName] = append(sectionMap[sectionName], fi)
	}

	currentLine := 0
	for i, name := range sectionOrder {
		if i > 0 {
			currentLine++ // blank line between sections
		}
		currentLine++ // section header line

		for _, fi := range sectionMap[name] {
			if currentLine == absoluteY {
				m.selected = fi
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
