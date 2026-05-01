package sidebar

import (
	"fmt"
	"sort"
	"strings"
	"time"

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
	// defaultAppsSection groups DMs whose peer is a Slack app or bot,
	// matching the behavior of the official Slack desktop client. Falls
	// between "Direct Messages" (humans) and "Channels" (firehose).
	defaultAppsSection = "Apps"
)

type ChannelItem struct {
	ID            string
	Name          string
	Type          string // channel, dm, group_dm, private, app
	Section       string // section name for grouping (e.g. "Engineering", "Starred")
	SectionOrder  int    // sort order from config; lower = higher in sidebar (custom sections only)
	UnreadCount   int
	IsStarred     bool
	Presence      string // for DMs: active, away, dnd
	DMUserID      string // for DMs: the user ID of the other party
	// LastReadTS is the Slack-format timestamp of the user's last
	// read marker for this channel ("1700000001.000000"). Used by
	// the staleness filter; empty means "never read" or "no signal".
	LastReadTS string
}

// sectionFor returns the section name an item belongs to, applying default
// fallback rules for items that have no explicit Section set.
func sectionFor(item ChannelItem) string {
	if item.Section != "" {
		return item.Section
	}
	if item.Type == "app" {
		return defaultAppsSection
	}
	if item.Type == "dm" || item.Type == "group_dm" {
		return defaultDMSection
	}
	return defaultChannelsSection
}

// orderedSections returns the section names in display order given the
// currently filtered items. Custom (user-defined) sections come first,
// sorted by SectionOrder ascending then by first-appearance for ties.
// The three built-in fallback sections are appended at the end in this
// order: "Direct Messages" (humans you talk to one-on-one), "Apps"
// (Slack apps and bots), then "Channels" (the firehose). Anything the
// user pinned to a custom section still wins the top spots.
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
	hasApps := false

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
		case name == defaultAppsSection:
			hasApps = true
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

	out := make([]string, 0, len(customs)+3)
	for _, c := range customs {
		out = append(out, c.name)
	}
	if hasDMs {
		out = append(out, defaultDMSection)
	}
	if hasApps {
		out = append(out, defaultAppsSection)
	}
	if hasChannels {
		out = append(out, defaultChannelsSection)
	}
	return out
}

// navKind identifies the type of stop the cursor is sitting on. The
// sidebar is a flat list of stops (threads row, section headers, and
// channel rows), and the cursor is just an index into that list.
type navKind int

const (
	navThreads navKind = iota
	navHeader
	navChannel
)

// navItem is one selectable stop in the sidebar. Inter-section blank
// rows are NOT navItems — only things the user can land on with j/k.
type navItem struct {
	kind   navKind
	header string // section name when kind == navHeader
	fi     int    // index into m.filtered when kind == navChannel
}

type Model struct {
	items    []ChannelItem
	yOffset  int // own scroll state -- replaces bubbles/viewport
	filter   string
	filtered []int // indices into items that match filter

	// Flat list of navigable stops in display order: threads row,
	// section headers, and channel rows belonging to expanded sections.
	// cursor indexes into nav and is the single source of truth for what
	// is currently selected. nav is rebuilt by rebuildNav() any time
	// items, filter, or collapse state changes.
	nav    []navItem
	cursor int

	// collapsed maps section name -> true when the section is collapsed.
	// Children of collapsed sections are excluded from nav and from the
	// rendered output; the section header still renders with a glyph
	// indicating collapse state and an aggregate unread badge when any
	// child has unreads.
	collapsed map[string]bool

	// Staleness filter: items whose LastReadTS is older than
	// staleThreshold are dropped from `filtered` (i.e. hidden from the
	// rendered sidebar). 0 disables the feature. activeID names the
	// channel currently displayed in the message pane and is always
	// exempt from staleness so the user doesn't lose track of it. nowFn
	// is injectable for deterministic tests; defaults to time.Now.
	staleThreshold time.Duration
	activeID       string
	// threadsActive reports that the Threads view is the currently
	// displayed view in the message pane. When true (and the cursor
	// is on a different row), the synthetic Threads row renders with
	// the same orange "active" indicator used for active channels.
	threadsActive bool
	nowFn         func() time.Time

	// snappedSelection lets View() avoid snapping yOffset back to the
	// selected row on every render. While snappedSelection == cursor,
	// mouse-wheel / programmatic scrolls (ScrollUp/ScrollDown) are
	// preserved.
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

	// Synthetic "Threads" row state. The Threads row is rendered at the
	// very top of the sidebar and is the first entry in nav. It is
	// selectable via j/k like a channel, but it is NOT a channel — when
	// the cursor sits on it, SelectedItem/SelectedID return zero / empty
	// and the App layer activates the threads view instead.
	threadsUnread int

	// focused tracks whether this panel currently has user focus. When
	// false, the cursor "▌" glyph dims from Accent to TextMuted (via
	// styles.SelectionBorderColor) so the unfocused selection doesn't
	// compete visually with the focused panel. Set by SetFocused() from
	// the App layer.
	focused bool
}

// SetFocused records whether the sidebar currently holds user focus and
// invalidates the render cache so the cursor glyph re-renders with the
// appropriate color (Accent when focused, TextMuted when not). The cursor
// color is baked into cacheRows during buildCache, so a focus change
// requires a full rebuild — but only when the value actually flips, to
// avoid spurious cache invalidations on every render.
func (m *Model) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.cacheValid = false
	m.dirty()
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes.
func (m *Model) InvalidateCache() {
	m.cacheValid = false
	m.version++
}

// now returns the current time, using the injected clock if set so
// tests can produce deterministic staleness results.
func (m *Model) now() time.Time {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
}

// SetNowFunc injects a clock for tests. Pass nil to revert to time.Now.
func (m *Model) SetNowFunc(fn func() time.Time) {
	m.nowFn = fn
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// SetStaleThreshold sets the maximum age for a channel's LastReadTS
// before the channel is auto-hidden from the sidebar. Pass 0 (or
// negative) to disable. Re-runs the filter immediately so the change
// is reflected on the next View().
func (m *Model) SetStaleThreshold(d time.Duration) {
	if m.staleThreshold == d {
		return
	}
	m.staleThreshold = d
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// SetActiveChannelID names the channel currently shown in the message
// pane. The active channel is always exempt from staleness hiding so
// the user can never have the visible chat disappear from the sidebar
// while they're reading it.
func (m *Model) SetActiveChannelID(id string) {
	if m.activeID == id {
		return
	}
	m.activeID = id
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// SetThreadsActive marks the synthetic "Threads" row as the active
// destination in the message pane. When true (and cursor is elsewhere),
// the Threads row renders with the orange active indicator. Mirrors
// SetActiveChannelID for normal channels.
func (m *Model) SetThreadsActive(active bool) {
	if m.threadsActive == active {
		return
	}
	m.threadsActive = active
	m.dirty()
}

// Version returns a counter that increments any time the View() output could
// change. Callers can compare against a previously-seen version to know
// whether to recompute downstream layout / wrapping.
func (m *Model) Version() int64 { return m.version }

// dirty bumps the version. Called from every state-mutating method.
func (m *Model) dirty() { m.version++ }

func New(items []ChannelItem) Model {
	m := Model{items: items}
	// Default collapse state: the firehose "Channels" section and the
	// "Apps" section start collapsed so the sidebar opens on a tidy
	// view of pinned sections + human DMs. Custom sections and DMs
	// default to expanded. Users toggle individual sections with
	// Enter/Space on the header.
	m.collapsed = map[string]bool{
		defaultChannelsSection: true,
		defaultAppsSection:     true,
	}
	m.rebuildFilter()
	m.rebuildNav()
	// Default selection is the synthetic Threads row at the top.
	m.cursor = 0
	return m
}

// IsThreadsSelected reports whether the synthetic "Threads" row is the
// selected entry.
func (m *Model) IsThreadsSelected() bool {
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return false
	}
	return m.nav[m.cursor].kind == navThreads
}

// SelectThreadsRow moves the cursor to the synthetic Threads row.
func (m *Model) SelectThreadsRow() {
	for i, n := range m.nav {
		if n.kind == navThreads {
			if m.cursor != i {
				m.cursor = i
				m.dirty()
			}
			return
		}
	}
}

// IsSectionHeaderSelected reports whether the cursor is on a section
// header. When ok is true, name is the section name (e.g. "Channels",
// "Direct Messages", or a custom section name).
func (m *Model) IsSectionHeaderSelected() (name string, ok bool) {
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return "", false
	}
	n := m.nav[m.cursor]
	if n.kind != navHeader {
		return "", false
	}
	return n.header, true
}

// IsCollapsed reports whether the named section is currently collapsed.
func (m *Model) IsCollapsed(section string) bool {
	if m.collapsed == nil {
		return false
	}
	return m.collapsed[section]
}

// ToggleCollapse flips the collapsed state of the named section. The
// cursor stays on the section's header (if it was already there) so a
// subsequent toggle just expands again.
func (m *Model) ToggleCollapse(section string) {
	if m.collapsed == nil {
		m.collapsed = map[string]bool{}
	}
	m.collapsed[section] = !m.collapsed[section]
	// Rebuild nav since the set of selectable rows changed. Preserve
	// the cursor's logical target (header / threads / channel ID) so
	// the user keeps their place.
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// ToggleCollapseSelected toggles the section currently under the cursor.
// No-op if the cursor isn't on a section header.
func (m *Model) ToggleCollapseSelected() bool {
	name, ok := m.IsSectionHeaderSelected()
	if !ok {
		return false
	}
	m.ToggleCollapse(name)
	return true
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

// SetItems replaces the sidebar's channel list. It does NOT reset the
// cursor to the Threads row — SetItems is called on every routine
// refresh (presence updates, unread changes, channel-list resync, etc.)
// and clobbering selection on those refreshes would be wrong. Callers
// that want to reset selection to the default Threads row on a major
// context change (e.g. workspace switch) should explicitly call
// SelectThreadsRow() after SetItems.
func (m *Model) SetItems(items []ChannelItem) {
	m.items = items
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// UpsertItem inserts a new ChannelItem keyed by ID, or updates an existing
// item in place if the ID is already present. On update, all fields EXCEPT
// UnreadCount and LastReadTS are overwritten from the supplied item; those
// two are preserved because Slack's mpim_open / im_created / group_joined
// payloads do not carry unread state, so clobbering would erase live
// indicators we want to keep visible.
//
// Re-runs the staleness filter so a freshly-added item (or one whose
// staleness state may have changed) is reflected in the visible list
// immediately.
func (m *Model) UpsertItem(item ChannelItem) {
	for i := range m.items {
		if m.items[i].ID == item.ID {
			// Preserve unread/last-read; overwrite descriptive fields.
			preservedUnread := m.items[i].UnreadCount
			preservedLastRead := m.items[i].LastReadTS
			m.items[i] = item
			m.items[i].UnreadCount = preservedUnread
			m.items[i].LastReadTS = preservedLastRead
			m.rebuildFilter()
			m.rebuildNavPreserveCursor()
			m.cacheValid = false
			m.dirty()
			return
		}
	}
	m.items = append(m.items, item)
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// AllItems returns a copy of the full unfiltered item slice, including
// items currently hidden by the staleness filter. Use this when you need
// to inspect every conversation the model knows about (e.g. tests, or
// upsert paths that key off ID); use VisibleItems for what's actually
// rendered. The returned slice is a defensive copy — mutations do not
// affect the model. Items() (uncopied) is the cheaper read-only choice
// on hot production paths.
func (m *Model) AllItems() []ChannelItem {
	out := make([]ChannelItem, len(m.items))
	copy(out, m.items)
	return out
}

func (m *Model) SelectedID() string {
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return ""
	}
	n := m.nav[m.cursor]
	if n.kind != navChannel {
		return ""
	}
	if n.fi < 0 || n.fi >= len(m.filtered) {
		return ""
	}
	return m.items[m.filtered[n.fi]].ID
}

func (m *Model) SelectedItem() (ChannelItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return ChannelItem{}, false
	}
	n := m.nav[m.cursor]
	if n.kind != navChannel {
		return ChannelItem{}, false
	}
	if n.fi < 0 || n.fi >= len(m.filtered) {
		return ChannelItem{}, false
	}
	return m.items[m.filtered[n.fi]], true
}

func (m *Model) MoveDown() {
	if m.cursor+1 < len(m.nav) {
		m.cursor++
		m.dirty()
	}
}

func (m *Model) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
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
	if m.cursor != 0 && len(m.nav) > 0 {
		m.cursor = 0
		m.dirty()
	}
}

func (m *Model) GoToBottom() {
	if last := len(m.nav) - 1; last >= 0 && m.cursor != last {
		m.cursor = last
		m.dirty()
	}
}

// Items returns all channel items.
func (m *Model) Items() []ChannelItem {
	return m.items
}

func (m *Model) SetFilter(filter string) {
	m.filter = filter
	m.rebuildFilter()
	m.rebuildNav()
	// Filter changes reset the cursor to the top so j/k starts from a
	// sensible place after a typed search.
	m.cursor = 0
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

// MarkUnread increments the unread count for the given channel by 1.
// No-op if the channel is not in the sidebar's items. Re-runs the filter
// so a previously stale-hidden channel becomes visible again as soon as
// its UnreadCount becomes non-zero (the staleness filter exempts items
// with UnreadCount > 0).
func (m *Model) MarkUnread(channelID string) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			m.items[i].UnreadCount++
			m.rebuildFilter()
			m.rebuildNavPreserveCursor()
			m.cacheValid = false
			m.dirty()
			return
		}
	}
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

// SetUnreadCount sets the unread count for the given channel to an exact
// value (use n=0 to clear). Re-runs the staleness filter so a channel that
// becomes unread reappears in the sidebar (the staleness rule exempts items
// with UnreadCount > 0). No-op if the channel is not in the sidebar.
//
// Differs from MarkUnread (which only increments by 1) and ClearUnread
// (which only sets to 0): this setter is used by mark-unread and by the
// channel_marked WS event reconciliation, both of which know the desired
// final count.
func (m *Model) SetUnreadCount(channelID string, n int) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			if m.items[i].UnreadCount == n {
				return
			}
			m.items[i].UnreadCount = n
			m.rebuildFilter()
			m.rebuildNavPreserveCursor()
			m.cacheValid = false
			m.dirty()
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
	// Fast path: the channel is already in nav (its section is expanded).
	for i, n := range m.nav {
		if n.kind != navChannel {
			continue
		}
		if m.items[m.filtered[n.fi]].ID == id {
			if m.cursor != i {
				m.cursor = i
				m.dirty()
			}
			return
		}
	}
	// Slow path: the channel is in m.filtered but its section is
	// collapsed and was therefore omitted from nav. Expand the section
	// (so the channel becomes navigable) and re-select.
	for fi, idx := range m.filtered {
		if m.items[idx].ID != id {
			continue
		}
		section := sectionFor(m.items[idx])
		if m.collapsed != nil && m.collapsed[section] {
			m.collapsed[section] = false
			m.rebuildNav()
			m.cacheValid = false
		}
		// Now find the channel in the freshly-rebuilt nav.
		for i, n := range m.nav {
			if n.kind == navChannel && n.fi == fi {
				if m.cursor != i {
					m.cursor = i
					m.dirty()
				}
				return
			}
		}
		return
	}
}

func (m *Model) rebuildFilter() {
	m.filtered = nil
	lower := strings.ToLower(m.filter)
	now := m.now()
	for i, item := range m.items {
		if m.filter != "" && !strings.Contains(strings.ToLower(item.Name), lower) {
			continue
		}
		// Staleness filter: drop items the user hasn't read in a long
		// time, with the active channel always exempt so it can't
		// disappear out from under them.
		if item.ID != m.activeID && IsStale(item, m.staleThreshold, now) {
			continue
		}
		m.filtered = append(m.filtered, i)
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

// cursorKey captures what the cursor is logically pointing at, in a
// form that survives a rebuild of m.nav. After rebuildNav() we look the
// key up in the new nav and re-set the cursor so the user keeps their
// place across collapse toggles, item refreshes, etc.
type cursorKey struct {
	kind   navKind
	header string // for navHeader
	id     string // channel ID for navChannel
}

func (m *Model) currentCursorKey() (cursorKey, bool) {
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return cursorKey{}, false
	}
	n := m.nav[m.cursor]
	switch n.kind {
	case navThreads:
		return cursorKey{kind: navThreads}, true
	case navHeader:
		return cursorKey{kind: navHeader, header: n.header}, true
	case navChannel:
		if n.fi < 0 || n.fi >= len(m.filtered) {
			return cursorKey{}, false
		}
		return cursorKey{kind: navChannel, id: m.items[m.filtered[n.fi]].ID}, true
	}
	return cursorKey{}, false
}

// rebuildNav rebuilds m.nav from the current items, filter, and
// collapse state. m.cursor is left unchanged; callers that want to
// preserve the user's selection across rebuilds should use
// rebuildNavPreserveCursor instead.
func (m *Model) rebuildNav() {
	sectionOrder := orderedSections(m.items, m.filtered)

	// Bucket filter indices by section in display order.
	bucket := map[string][]int{}
	for fi, idx := range m.filtered {
		bucket[sectionFor(m.items[idx])] = append(bucket[sectionFor(m.items[idx])], fi)
	}

	nav := make([]navItem, 0, 1+len(sectionOrder))
	nav = append(nav, navItem{kind: navThreads})
	for _, name := range sectionOrder {
		nav = append(nav, navItem{kind: navHeader, header: name})
		if m.IsCollapsed(name) {
			continue
		}
		for _, fi := range bucket[name] {
			nav = append(nav, navItem{kind: navChannel, fi: fi})
		}
	}
	m.nav = nav
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		m.cursor = 0
	}
}

// rebuildNavPreserveCursor rebuilds m.nav and tries to keep the cursor
// pointing at the same logical target (Threads / a section header / a
// specific channel). Falls back to the Threads row when the previous
// target no longer exists.
func (m *Model) rebuildNavPreserveCursor() {
	key, hadKey := m.currentCursorKey()
	m.rebuildNav()
	if !hadKey {
		return
	}
	for i, n := range m.nav {
		switch {
		case key.kind == navThreads && n.kind == navThreads:
			m.cursor = i
			return
		case key.kind == navHeader && n.kind == navHeader && n.header == key.header:
			m.cursor = i
			return
		case key.kind == navChannel && n.kind == navChannel:
			if n.fi >= 0 && n.fi < len(m.filtered) && m.items[m.filtered[n.fi]].ID == key.id {
				m.cursor = i
				return
			}
		}
	}
	// Previous target gone (e.g. the channel was filtered out, or its
	// section now collapsed). Fall back to the Threads row.
	m.cursor = 0
}

// aggregateUnreadForSection returns the sum of UnreadCount across every
// item in the named section that is currently in m.filtered. Used to
// render an aggregate badge on collapsed section headers.
func (m *Model) aggregateUnreadForSection(section string) int {
	total := 0
	for _, idx := range m.filtered {
		if sectionFor(m.items[idx]) != section {
			continue
		}
		total += m.items[idx].UnreadCount
	}
	return total
}

// renderRow describes a single rendered row in the sidebar.
//
// For navigable rows (Threads, section headers, channel items) we
// pre-render BOTH the selected and unselected variants in buildCache
// so that selection movement (j/k) needs no lipgloss work in View().
// For inter-section blank rows the two variants are identical.
//
// navIdx links the row back to its entry in m.nav so View() can find
// the selected row in O(1) using the cursor. -1 means "not navigable"
// (blank separators, the optional 'No channels' placeholder).
type renderRow struct {
	normal   string // rendered as a non-selected row
	selected string // rendered with the selection cursor + selected style
	active   string // rendered with the orange "currently-entered" indicator; empty when not applicable
	height   int    // rendered terminal height (always 1 for headers/blanks)
	navIdx   int    // index into m.nav, or -1
	// channelID is set on channel rows and used by View() to swap in the
	// `active` variant when the row matches m.activeID. Empty for headers,
	// blanks, and the Threads row.
	channelID string
	// isThreadsRow flags the synthetic Threads row so View() can swap in
	// the `active` variant whenever m.threadsActive is true (mirroring
	// the channelID-based check used for channels).
	isThreadsRow bool
}

// buildCache rebuilds m.cacheRows for the given width. Expensive; runs only
// when items, filter, width, or theme change.
//
// Each entry in m.nav corresponds to exactly one renderRow with
// row.navIdx == nav index. View() uses this to find the selected line
// and to substitute the "selected" variant in O(1).
func (m *Model) buildCache(width int) {
	m.cacheValid = true
	m.cacheWidth = width
	m.cacheRows = m.cacheRows[:0]
	m.cacheFiller = lipgloss.NewStyle().Width(width).Background(styles.SidebarBackground).Render("")

	// Reverse-index nav by section name and by filter idx so we can map
	// each rendered row back to its nav index. Headers are uniquely
	// keyed by section name; channels by their filter idx.
	headerNavIdx := map[string]int{}
	channelNavIdx := map[int]int{} // filter idx -> nav idx
	threadsIdx := -1
	for i, n := range m.nav {
		switch n.kind {
		case navThreads:
			threadsIdx = i
		case navHeader:
			headerNavIdx[n.header] = i
		case navChannel:
			channelNavIdx[n.fi] = i
		}
	}

	sectionOrder := orderedSections(m.items, m.filtered)

	// Combine sidebar bg + fg so styled glyphs (private/DM prefixes, cursor,
	// unread dots) restore both colors after their ANSI reset.
	bgAnsi := messages.SidebarBgANSI() + messages.SidebarFgANSI() // compute once outside loop

	// Style objects allocated once per cache build.
	cursorStyle := lipgloss.NewStyle().Foreground(styles.SelectionBorderColor(m.focused))
	activeBorderStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	dotStyle := lipgloss.NewStyle().Foreground(styles.Primary)
	privateStyle := lipgloss.NewStyle().Foreground(styles.Warning)

	cursorSelected := cursorStyle.Render("▌")
	activeBorder := activeBorderStyle.Render("▌")
	unreadDotStr := dotStyle.Render("●")
	privatePrefix := privateStyle.Render("◆ ")
	// Read private channels use a *plain* "◆ " glyph (no inline ANSI
	// styling) so the prefix inherits the surrounding row style the
	// same way the public-channel "# " does. Inline-styled prefixes
	// emit an ANSI reset; ReapplyBgAfterResets then re-injects the
	// brighter SidebarFgANSI for everything after the reset, which
	// overrides ChannelNormal's TextMuted foreground and made read
	// private rows appear lighter than read public rows.
	privatePrefixMuted := "◆ "
	dmActivePrefix := styles.PresenceOnline.Render("● ")
	dmAwayPrefix := styles.PresenceAway.Render("○ ")
	// Apps use a filled square glyph to visually distinguish them from
	// human DMs (which use a circle). No presence concept for apps --
	// they're always "available". Two variants mirror the private-channel
	// treatment: a styled (warning-colored) glyph for unread rows that
	// pop, and a plain glyph for read rows so they inherit
	// ChannelNormal's muted foreground without the SidebarFgANSI
	// reset-and-reinject bumping the row text brighter than read
	// public/dm rows.
	appPrefix := lipgloss.NewStyle().Foreground(styles.Warning).Render("▣ ")
	appPrefixMuted := "▣ "
	groupDMPrefix := styles.PresenceAway.Render("● ")

	// Synthetic "Threads" row, always rendered at the very top of the sidebar
	// (before any section). Selectable like a channel; the App layer activates
	// the threads view when IsThreadsSelected() is true.
	threadsLabel := " ⚑ Threads"
	threadsCursor := cursorSelected + "⚑ Threads"
	threadsActiveLabel := activeBorder + "⚑ Threads"
	if m.threadsUnread > 0 {
		// Render "•N" as a single styled span so the dot glyph and the digits
		// stay adjacent in the output (no ANSI reset splits them). Tests rely
		// on the literal substring "•N" being searchable in View() output.
		badge := " " + dotStyle.Render("•"+fmt.Sprintf("%d", m.threadsUnread))
		threadsLabel += badge
		threadsCursor += badge
		threadsActiveLabel += badge
	}
	threadsLabel = messages.ReapplyBgAfterResets(threadsLabel, bgAnsi)
	threadsCursor = messages.ReapplyBgAfterResets(threadsCursor, bgAnsi)
	threadsActiveLabel = messages.ReapplyBgAfterResets(threadsActiveLabel, bgAnsi)
	threadsBaseStyle := styles.ChannelNormal
	if m.threadsUnread > 0 {
		threadsBaseStyle = styles.ChannelUnread
	}
	threadsNormal := threadsBaseStyle.Width(width - 2).Render(threadsLabel)
	threadsSelectedRow := styles.ChannelSelected.Width(width - 2).Render(threadsCursor)
	threadsActiveRow := styles.ChannelSelected.Width(width - 2).Render(threadsActiveLabel)
	m.cacheRows = append(m.cacheRows, renderRow{
		normal:       threadsNormal,
		selected:     threadsSelectedRow,
		active:       threadsActiveRow,
		height:       1,
		navIdx:       threadsIdx,
		isThreadsRow: true,
	})
	// Blank separator between the Threads row and the first section (or below
	// the Threads row when there are no channels at all).
	m.cacheRows = append(m.cacheRows, renderRow{height: 1, navIdx: -1})

	// Pre-build the per-section channel rows so we can flatten with
	// section headers below. Channels in a collapsed section are still
	// pre-rendered? No — they're not in nav, so we skip them entirely
	// here to avoid wasted work.
	type sectionGroup struct {
		name string
		rows []renderRow
	}
	sectionMap := map[string]*sectionGroup{}
	for _, name := range sectionOrder {
		sectionMap[name] = &sectionGroup{name: name}
	}

	for fi, idx := range m.filtered {
		item := m.items[idx]
		sectionName := sectionFor(item)
		if m.IsCollapsed(sectionName) {
			continue
		}

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
			if item.UnreadCount > 0 {
				prefix = privatePrefix
			} else {
				prefix = privatePrefixMuted
			}
		case "app":
			if item.UnreadCount > 0 {
				prefix = appPrefix
			} else {
				prefix = appPrefixMuted
			}
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

		// Three label variants: selected (cursor, green ▌), active
		// (entered/open channel, orange ▌), and normal (just a space).
		// View() picks: selected if cursor is on this row, else active
		// if this row's channelID matches activeID, else normal. The
		// cursor takes precedence so j/k feedback stays unambiguous.
		labelNormal := " " + prefix + name + " " + unreadDot
		labelSelected := cursorSelected + prefix + name + " " + unreadDot
		labelActive := activeBorder + prefix + name + " " + unreadDot

		// Re-apply theme background after ANSI resets from inline styled
		// glyphs (cursor, prefix, unread dot) so the outer channel style's
		// background isn't interrupted.
		labelNormal = messages.ReapplyBgAfterResets(labelNormal, bgAnsi)
		labelSelected = messages.ReapplyBgAfterResets(labelSelected, bgAnsi)
		labelActive = messages.ReapplyBgAfterResets(labelActive, bgAnsi)

		// Pick base style for non-selected state.
		var baseStyle lipgloss.Style
		if item.UnreadCount > 0 {
			baseStyle = styles.ChannelUnread
		} else {
			baseStyle = styles.ChannelNormal
		}

		rowNormal := baseStyle.Width(width - 2).Render(labelNormal)
		rowSelected := styles.ChannelSelected.Width(width - 2).Render(labelSelected)
		// Active uses the same bright/bold treatment as Selected so the
		// entered channel reads as "current" even when its unread count
		// is zero (which is the common case after MarkChannel runs).
		rowActive := styles.ChannelSelected.Width(width - 2).Render(labelActive)

		ni, ok := channelNavIdx[fi]
		if !ok {
			ni = -1
		}
		sectionMap[sectionName].rows = append(sectionMap[sectionName].rows, renderRow{
			normal:    rowNormal,
			selected:  rowSelected,
			active:    rowActive,
			height:    1, // every channel row is exactly one line
			navIdx:    ni,
			channelID: item.ID,
		})
	}

	// When there are no channel items at all, render a single muted
	// "No channels" placeholder below the Threads row + separator so the
	// Threads row remains globally visible even on an empty workspace.
	if len(m.items) == 0 {
		placeholder := styles.SectionHeader.Render("No channels")
		m.cacheRows = append(m.cacheRows, renderRow{
			normal:   placeholder,
			selected: placeholder,
			height:   1,
			navIdx:   -1,
		})
	}

	// Flatten into a single row list with section headers.
	// Add a blank line between sections for visual separation.
	for i, name := range sectionOrder {
		if i > 0 {
			m.cacheRows = append(m.cacheRows, renderRow{height: 1, navIdx: -1})
		}
		group := sectionMap[name]
		headerLabel, headerLabelSelected := m.renderSectionHeaderLabel(name, cursorSelected, dotStyle, bgAnsi)
		ni, ok := headerNavIdx[name]
		if !ok {
			ni = -1
		}
		m.cacheRows = append(m.cacheRows, renderRow{
			normal:   styles.SectionHeader.Width(width - 2).Render(headerLabel),
			selected: styles.SectionHeader.Width(width - 2).Render(headerLabelSelected),
			height:   1,
			navIdx:   ni,
		})
		m.cacheRows = append(m.cacheRows, group.rows...)
	}
}

// renderSectionHeaderLabel returns the (normal, selected) label
// strings for a section header. Headers show a triangle indicating
// expand/collapse state and, when collapsed, an aggregate unread badge
// summing UnreadCount across every visible item in the section.
func (m *Model) renderSectionHeaderLabel(name, cursor string, dotStyle lipgloss.Style, bgAnsi string) (string, string) {
	glyph := "▾" // expanded
	if m.IsCollapsed(name) {
		glyph = "▸"
	}
	label := " " + glyph + " " + name
	if m.IsCollapsed(name) {
		if n := m.aggregateUnreadForSection(name); n > 0 {
			label += " " + dotStyle.Render("•"+fmt.Sprintf("%d", n))
		}
	}
	selected := cursor + glyph + " " + name
	if m.IsCollapsed(name) {
		if n := m.aggregateUnreadForSection(name); n > 0 {
			selected += " " + dotStyle.Render("•"+fmt.Sprintf("%d", n))
		}
	}
	return messages.ReapplyBgAfterResets(label, bgAnsi),
		messages.ReapplyBgAfterResets(selected, bgAnsi)
}

func (m *Model) View(height, width int) string {
	// Note: we no longer early-return on len(m.items)==0. The synthetic
	// Threads row is globally present (even on an empty workspace), so we
	// always go through buildCache, which handles the empty-items case by
	// emitting a muted "No channels" placeholder below the Threads row.
	if !m.cacheValid || m.cacheWidth != width {
		m.buildCache(width)
	}

	// Each cacheRow is exactly one rendered line, so the line index of a
	// row is just its slice index. Find the selected row by matching the
	// cursor's nav index against renderRow.navIdx.
	selectedLine := -1
	for i, r := range m.cacheRows {
		if r.navIdx == m.cursor {
			selectedLine = i
			break
		}
	}

	// Snap yOffset to keep the selected row visible only when the
	// cursor has actually changed since the last snap. This preserves
	// mouse-wheel / programmatic scroll positions across renders.
	if selectedLine >= 0 && (!m.hasSnapped || m.snappedSelection != m.cursor) {
		if selectedLine >= m.yOffset+height {
			m.yOffset = selectedLine - height + 1
		}
		if selectedLine < m.yOffset {
			m.yOffset = selectedLine
		}
		m.snappedSelection = m.cursor
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
		case r.navIdx >= 0 && r.navIdx == m.cursor:
			visible = append(visible, r.selected)
		case r.channelID != "" && r.channelID == m.activeID && r.active != "":
			// This row is the channel the user is currently viewing in
			// the message pane (but the cursor is elsewhere). Show the
			// orange ▌ "active" indicator so they know where they are.
			visible = append(visible, r.active)
		case r.isThreadsRow && m.threadsActive && r.active != "":
			// Threads view is the currently displayed view; mark the
			// Threads row as active with the same orange indicator.
			visible = append(visible, r.active)
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

// ClickAt handles a mouse click at the given y-coordinate (relative to
// sidebar content top). Updates the cursor to whatever sits at that
// position. Returns the channel item and true ONLY when the click
// landed on a channel row; section-header / threads-row / blank clicks
// return (zero, false). Callers consult IsThreadsSelected /
// IsSectionHeaderSelected after this call when ok == false.
func (m *Model) ClickAt(y int) (ChannelItem, bool) {
	absoluteY := y + m.yOffset
	if absoluteY < 0 || absoluteY >= len(m.cacheRows) {
		return ChannelItem{}, false
	}
	r := m.cacheRows[absoluteY]
	if r.navIdx < 0 || r.navIdx >= len(m.nav) {
		// Inter-section blank or "No channels" placeholder.
		return ChannelItem{}, false
	}
	if m.cursor != r.navIdx {
		m.cursor = r.navIdx
		m.dirty()
	}
	n := m.nav[r.navIdx]
	if n.kind != navChannel {
		// Threads row or section header — nothing to return; caller
		// inspects IsThreadsSelected / IsSectionHeaderSelected.
		return ChannelItem{}, false
	}
	if n.fi < 0 || n.fi >= len(m.filtered) {
		return ChannelItem{}, false
	}
	return m.items[m.filtered[n.fi]], true
}

func (m Model) Width() int {
	return 30
}
