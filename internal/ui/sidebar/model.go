package sidebar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/text"
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
	ID           string
	Name         string
	Type         string // channel, dm, group_dm, private, app
	Section      string // section name for grouping (e.g. "Engineering", "Starred")
	SectionOrder int    // sort order from config; lower = higher in sidebar (custom sections only)
	// ChannelOrder is the per-channel sort position within a section,
	// sourced from the SectionDef "<pattern>:<N>" suffix syntax. >0
	// means the channel sorts ahead of un-annotated peers (which fall
	// back to input order); among annotated channels, lower wins.
	// Always 0 in Slack-native sections mode (config-glob feature only).
	ChannelOrder int
	IsStarred    bool
	Presence     string // for DMs: active, away, dnd
	DMUserID     string // for DMs: the user ID of the other party
	// IsMuted reports whether the user has muted this channel (via
	// Slack's muted_channels user pref). Muted channels render with a
	// dimmer foreground and suppress their unread dot; they also do
	// not contribute to the aggregate unread badges on collapsed
	// section headers. Sourced from service.MuteStore in
	// buildChannelItem.
	IsMuted bool
}

// IsVisiblyUnread reports whether this channel should render as having
// unread messages -- DB-level HasUnread AND the user hasn't muted it.
// This is the single source of truth for the "unread dot" predicate;
// both the sidebar View, section aggregates, and the App's tab-title
// counter MUST consult this helper rather than re-deriving the rule.
func (item ChannelItem) IsVisiblyUnread(state cache.ReadState) bool {
	return state.HasUnread && !item.IsMuted
}

// sectionFor is the package-level back-compat shim for callers
// (including tests) that don't have a *Model. It always uses
// config-mode logic. The Slack-mode dispatcher is *Model.sectionFor.
func sectionFor(item ChannelItem) string {
	return sectionForLegacy(item)
}

// sectionForLegacy returns the config-mode section name an item belongs
// to, applying default fallback rules for items that have no explicit
// Section set. The Slack-mode dispatcher is the *Model.sectionFor
// method; this function is the back-compat path for callers without a
// Model and the implementation Slack-mode delegates to when no
// provider is wired.
func sectionForLegacy(item ChannelItem) string {
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

// orderedSections is the package-level back-compat shim. Tests and
// other call sites that don't have a *Model continue to use this; it
// always uses config-mode logic. The Slack-mode dispatcher is
// *Model.modelOrderedSections.
func orderedSections(items []ChannelItem, filtered []int) []string {
	return orderedSectionsLegacy(items, filtered)
}

// orderedSectionsLegacy returns the section names in display order given the
// currently filtered items. Custom (user-defined) sections come first,
// sorted by SectionOrder ascending then by first-appearance for ties.
// The three built-in fallback sections are appended at the end in this
// order: "Direct Messages" (humans you talk to one-on-one), "Apps"
// (Slack apps and bots), then "Channels" (the firehose). Anything the
// user pinned to a custom section still wins the top spots.
func orderedSectionsLegacy(items []ChannelItem, filtered []int) []string {
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
		name := sectionForLegacy(item)
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
	navActivity
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
	width    int // configurable panel width; 0 means defaultWidth
	yOffset  int // own scroll state -- replaces bubbles/viewport
	filter   string
	filtered []int // indices into items that match filter

	// presenceByUser is the authoritative live presence for DM peers,
	// keyed by DMUserID ("active"/"away"). It is updated by presence_change
	// WS events (UpdatePresenceByUser) and re-applied onto items on every
	// SetItems, so a sidebar rebuild (sections refresh, DM-name resolution,
	// workspace-ready) can't wipe live presence back to the "away" default
	// that freshly-built ChannelItems carry. Cleared on workspace switch
	// via ResetPresence. Also remembers events that arrive before their DM
	// item exists (startup race) so they apply once the item is set.
	presenceByUser map[string]string

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

	// sectionsProvider is the Slack-native sections data source. Nil
	// means "use config-glob behavior". When non-nil and Ready, the
	// orderedSections function returns the provider's verbatim order
	// and headers are keyed by section ID instead of name.
	sectionsProvider SectionsProvider
	// readStateReader returns the per-channel read state map for the
	// active workspace, keyed by channel ID. Set by App via
	// SetReadStateReader. May be nil — nil means "treat everything as
	// no-unread" (used during early construction).
	readStateReader func() map[string]cache.ReadState
	// collapseByID parallels `collapsed` for Slack-mode (ID-keyed).
	// Renames preserve collapse state because the ID is stable.
	// Populated lazily; lookups treat nil as empty. Used in Task 9.
	collapseByID map[string]bool

	// Staleness filter: items whose last_read_ts (from the read-state
	// DB, fetched via readStateReader in rebuildFilter) is older than
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
	// activityActive reports that the Activity view is the currently
	// displayed view in the message pane. Mirrors threadsActive: when
	// true (and the cursor is on a different row), the synthetic
	// Activity row renders with the orange "active" indicator.
	activityActive bool
	nowFn          func() time.Time

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

	// Synthetic "Activity" row state. Rendered directly below the
	// Threads row. Like the Threads row it is selectable via j/k but is
	// NOT a channel — when the cursor sits on it, SelectedItem/SelectedID
	// return zero / empty and the App layer activates the Activity view.
	activityUnread int

	// focused tracks whether this panel currently has user focus. When
	// false, the cursor "▌" glyph dims from Accent to TextMuted (via
	// styles.SelectionBorderColor) so the unfocused selection doesn't
	// compete visually with the focused panel. Set by SetFocused() from
	// the App layer.
	focused bool
}

// SetSectionsProvider injects a Slack-native sections data source.
// When non-nil and Ready, the sidebar renders sections in the
// provider's order and keys collapse state by section ID. Pass nil
// to revert to config-glob behavior.
func (m *Model) SetSectionsProvider(p SectionsProvider) {
	m.sectionsProvider = p
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// SetReadStateReader installs a callback that returns the per-channel
// read state map for the workspace currently presented by this sidebar.
// Called by View() at render time. Setting it invalidates the row cache
// so the next render reflects the new source.
func (m *Model) SetReadStateReader(f func() map[string]cache.ReadState) {
	m.readStateReader = f
	m.cacheValid = false
	m.dirty()
}

// UnreadChannelCount returns the number of channels currently in the
// sidebar that should render as unread (HasUnread && !IsMuted), via
// ChannelItem.IsVisiblyUnread. Returns 0 when no reader is installed.
// Used by the App to compute the window title's active-workspace
// count -- guaranteed to match the dot population exactly.
func (m *Model) UnreadChannelCount() int {
	if m.readStateReader == nil {
		return 0
	}
	state := m.readStateReader()
	count := 0
	for _, item := range m.items {
		if item.IsVisiblyUnread(state[item.ID]) {
			count++
		}
	}
	return count
}

// NextUnread returns the visibly-unread channel (HasUnread && !IsMuted)
// that follows afterID, wrapping around, and the fields the App needs to
// raise a ChannelSelectedMsg. afterID itself is always skipped so
// repeated calls advance through the unread set; pass the currently-open
// channel ID. dir > 0 walks forward (next), dir < 0 walks backward
// (previous). ok is false when no *other* channel is unread (an empty
// inbox, or afterID is the only unread one).
//
// Walk order is m.filtered -- the section-sorted order the sidebar renders
// in -- so a/A traverse unread channels top-to-bottom rather than raw feed
// order. Unread channels are never removed by the staleness filter
// (IsStale returns false whenever hasUnread), so every unread channel is
// reachable.
//
// m.filtered is built by rebuildFilter, which does NOT apply collapse
// state (collapse is handled later, in rebuildNav). So the walk deliberately
// includes unread channels inside collapsed sections, even though those
// rows aren't currently visible -- and since the default layout collapses
// the "Channels" firehose, a/A will jump into it and the caller's
// SelectByID expands that section to reveal the target. This matches the
// desktop client, which also surfaces unread channels regardless of
// section collapse; it's an intentional "catch up on all unreads" skim,
// not just "what's on screen".
func (m *Model) NextUnread(afterID string, dir int) (id, name, chType string, ok bool) {
	if m.readStateReader == nil {
		return "", "", "", false
	}
	n := len(m.filtered)
	if n == 0 {
		return "", "", "", false
	}
	state := m.readStateReader()
	step := 1
	if dir < 0 {
		step = -1
	}
	// Anchor one step *behind* afterID so the loop's first probe (off=1)
	// is afterID's neighbour in the walk direction. When afterID isn't in
	// the visible set, the sentinel (-1 forward / n backward) makes the
	// first press land on the top / bottom row: the ((start+step*off)%n+n)%n
	// normalization maps -1+1 -> 0 and n-1 -> n-1.
	start := -1
	if step < 0 {
		start = n
	}
	for pos, idx := range m.filtered {
		if m.items[idx].ID == afterID {
			start = pos
			break
		}
	}
	for off := 1; off <= n; off++ {
		item := m.items[m.filtered[((start+step*off)%n+n)%n]]
		if item.ID == afterID {
			continue
		}
		if item.IsVisiblyUnread(state[item.ID]) {
			return item.ID, item.Name, item.Type, true
		}
	}
	return "", "", "", false
}

// Invalidate forces the next View() call to re-read read state from
// the installed reader. Called by App.Update on ReadStateChangedMsg.
func (m *Model) Invalidate() {
	m.cacheValid = false
	m.dirty()
}

// useSlackSections reports whether Slack-mode rendering is active.
// True iff a non-nil provider is set AND it currently Ready.
func (m *Model) useSlackSections() bool {
	return m.sectionsProvider != nil && m.sectionsProvider.Ready()
}

// sectionFor returns the section key (Slack section ID in Slack mode,
// section name in config mode) an item belongs to. Items without an
// explicit Section get a default based on type: in Slack mode, the
// default is whichever provider-returned section has the matching
// type (direct_messages / recent_apps / channels). In config mode,
// the existing string constants apply.
func (m *Model) sectionFor(item ChannelItem) string {
	if item.Section != "" {
		return item.Section
	}
	if m.useSlackSections() {
		// Find the appropriate default-type section in the provider.
		var dmID, appsID, channelsID string
		for _, meta := range m.sectionsProvider.OrderedSlackSections() {
			switch meta.Type {
			case "direct_messages":
				if dmID == "" {
					dmID = meta.ID
				}
			case "recent_apps":
				if appsID == "" {
					appsID = meta.ID
				}
			case "channels":
				if channelsID == "" {
					channelsID = meta.ID
				}
			}
		}
		switch item.Type {
		case "app":
			if appsID != "" {
				return appsID
			}
		case "dm", "group_dm":
			if dmID != "" {
				return dmID
			}
		}
		return channelsID // may be ""; rare edge case
	}
	return sectionForLegacy(item)
}

// modelOrderedSections returns the section keys in display order for
// the current model state. Slack mode: provider's verbatim list,
// returning IDs of sections that have at least one filtered item OR
// are standard-typed. Config mode: legacy algorithm.
func (m *Model) modelOrderedSections(filtered []int) []string {
	if !m.useSlackSections() {
		return orderedSectionsLegacy(m.items, filtered)
	}
	metas := m.sectionsProvider.OrderedSlackSections()
	hasItem := map[string]bool{}
	for _, idx := range filtered {
		hasItem[m.sectionFor(m.items[idx])] = true
	}
	out := make([]string, 0, len(metas))
	for _, meta := range metas {
		if meta.Type == "standard" || hasItem[meta.ID] {
			out = append(out, meta.ID)
		}
	}
	return out
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

// SetActivityActive marks the synthetic "Activity" row as the active
// destination in the message pane. Mirrors SetThreadsActive.
func (m *Model) SetActivityActive(active bool) {
	if m.activityActive == active {
		return
	}
	m.activityActive = active
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

// IsActivitySelected reports whether the synthetic "Activity" row is
// the selected entry. Mirrors IsThreadsSelected.
func (m *Model) IsActivitySelected() bool {
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return false
	}
	return m.nav[m.cursor].kind == navActivity
}

// SelectActivityRow moves the cursor to the synthetic Activity row.
func (m *Model) SelectActivityRow() {
	for i, n := range m.nav {
		if n.kind == navActivity {
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
// In Slack mode, the section parameter is a section ID and lookup uses
// collapseByID (so renames preserve state). In config mode, it's a
// section name and lookup uses collapsed.
func (m *Model) IsCollapsed(section string) bool {
	if m.useSlackSections() {
		if m.collapseByID == nil {
			return false
		}
		return m.collapseByID[section]
	}
	if m.collapsed == nil {
		return false
	}
	return m.collapsed[section]
}

// ToggleCollapse flips the collapsed state of the named section. The
// cursor stays on the section's header (if it was already there) so a
// subsequent toggle just expands again. Dispatches to collapseByID in
// Slack mode (ID-keyed; survives renames), collapsed in config mode.
func (m *Model) ToggleCollapse(section string) {
	if m.useSlackSections() {
		if m.collapseByID == nil {
			m.collapseByID = map[string]bool{}
		}
		m.collapseByID[section] = !m.collapseByID[section]
	} else {
		if m.collapsed == nil {
			m.collapsed = map[string]bool{}
		}
		m.collapsed[section] = !m.collapsed[section]
	}
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

// SetActivityUnreadCount updates the badge count shown next to the
// Activity row. Mirrors SetThreadsUnreadCount.
func (m *Model) SetActivityUnreadCount(n int) {
	if n < 0 {
		n = 0
	}
	if m.activityUnread != n {
		m.activityUnread = n
		m.cacheValid = false
		m.dirty()
	}
}

// ActivityUnreadCount returns the current Activity-row unread badge count.
func (m *Model) ActivityUnreadCount() int { return m.activityUnread }

// SetItems replaces the sidebar's channel list. It does NOT reset the
// cursor to the Threads row — SetItems is called on every routine
// refresh (presence updates, unread changes, channel-list resync, etc.)
// and clobbering selection on those refreshes would be wrong. Callers
// that want to reset selection to the default Threads row on a major
// context change (e.g. workspace switch) should explicitly call
// SelectThreadsRow() after SetItems.
func (m *Model) SetItems(items []ChannelItem) {
	m.items = items
	m.applyPresence()
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// applyPresence overwrites each DM item's Presence with the authoritative
// live value from presenceByUser, so a rebuild that supplies stale/default
// presence doesn't clobber the real state. No-op when no presence is known.
func (m *Model) applyPresence() {
	if len(m.presenceByUser) == 0 {
		return
	}
	for i := range m.items {
		uid := m.items[i].DMUserID
		if uid == "" {
			continue
		}
		if p, ok := m.presenceByUser[uid]; ok {
			m.items[i].Presence = p
		}
	}
}

// ResetPresence forgets all remembered per-user presence. Called on
// workspace switch so the newly-active workspace starts from its own
// cache-seeded item presence rather than the previous workspace's peers.
func (m *Model) ResetPresence() {
	m.presenceByUser = nil
}

// UpsertItem inserts a new ChannelItem keyed by ID, or updates an existing
// item in place if the ID is already present. On update, all descriptive
// fields are overwritten from the supplied item. Read state (HasUnread,
// LastReadTS) is no longer mirrored on ChannelItem -- it lives exclusively
// in the read-state DB and is read at render time via the readStateReader
// callback, so there's nothing to preserve here.
//
// Re-runs the staleness filter so a freshly-added item (or one whose
// staleness state may have changed) is reflected in the visible list
// immediately.
func (m *Model) UpsertItem(item ChannelItem) {
	// Seed the incoming item with known live presence so a DM opened at
	// runtime shows the right dot immediately instead of the "away"
	// default it was built with.
	if item.DMUserID != "" {
		if p, ok := m.presenceByUser[item.DMUserID]; ok {
			item.Presence = p
		}
	}
	for i := range m.items {
		if m.items[i].ID == item.ID {
			m.items[i] = item
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
// Marks the current cursor as already-snapped so View() leaves yOffset alone
// on the next render -- prevents the snap branch from yanking the viewport
// back to keep the (unchanged) selection visible.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.snappedSelection = m.cursor
	m.hasSnapped = true
	m.dirty()
}

// ScrollDown moves the viewport down by n rows. View() clamps to the
// max valid offset for the current content height. See ScrollUp for the
// snap-guard rationale.
func (m *Model) ScrollDown(n int) {
	if n > 0 {
		m.yOffset += n
		m.snappedSelection = m.cursor
		m.hasSnapped = true
		m.dirty()
	}
}

// ViewportAtTop reports whether the sidebar viewport is scrolled to the top.
func (m *Model) ViewportAtTop() bool {
	return m.yOffset == 0
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

// UpdatePresenceByUser records the authoritative live presence for a user
// and updates any DM item whose DMUserID matches. Recording in
// presenceByUser (even when no item matches yet) makes the value survive
// later SetItems rebuilds and applies it to items that appear afterward
// (startup race).
func (m *Model) UpdatePresenceByUser(userID, presence string) {
	if userID == "" {
		return
	}
	if m.presenceByUser == nil {
		m.presenceByUser = make(map[string]string)
	}
	m.presenceByUser[userID] = presence
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
		section := m.sectionFor(m.items[idx])
		if m.IsCollapsed(section) {
			// ToggleCollapse handles both name-keyed (config mode) and
			// ID-keyed (Slack mode) maps and rebuilds nav + invalidates
			// cache as a side effect.
			m.ToggleCollapse(section)
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
	// Perf instrumentation: rebuildFilter calls readStateReader (a
	// synchronous SQLite GetWorkspaceReadState) and runs a sort over the
	// channel list. It's called on every channel switch via
	// SetActiveChannelID -- one of two readStateReader hits per switch
	// (the other is inside buildCache). SLK_DEBUG-gated.
	var perfStart time.Time
	if debuglog.Enabled() {
		perfStart = time.Now()
		defer func() {
			debuglog.Perf("sidebar.rebuildFilter items=%d filtered=%d took=%s",
				len(m.items), len(m.filtered), time.Since(perfStart))
		}()
	}
	m.filtered = nil
	lower := text.Fold(m.filter)
	now := m.now()
	// Fetch read state once for the whole filter pass so IsStale can
	// see the same hasUnread/lastReadTS the rest of the sidebar does.
	// nil-safe: when no reader is installed (early construction, some
	// tests) every lookup returns the zero value and IsStale's
	// type-aware empty-LastReadTS branch handles it.
	var readState map[string]cache.ReadState
	if m.readStateReader != nil {
		readState = m.readStateReader()
	}
	for i, item := range m.items {
		if m.filter != "" && !strings.Contains(text.Fold(item.Name), lower) {
			continue
		}
		// Staleness filter: drop items the user hasn't read in a long
		// time, with the active channel always exempt so it can't
		// disappear out from under them.
		state := readState[item.ID]
		if item.ID != m.activeID && IsStale(item, state.HasUnread, state.LastReadTS, m.staleThreshold, now) {
			continue
		}
		m.filtered = append(m.filtered, i)
	}

	// Sort filtered indices to match the visual section display order so that
	// j/k navigation traverses items in the same order they're rendered.
	// Within a section:
	//   1. Channels with ChannelOrder > 0 come first, sorted ascending
	//      (from SectionDef "<pattern>:<N>" suffixes; config-mode only).
	//   2. Channels with ChannelOrder == 0 follow, in input order
	//      (preserves Slack-provided order in Slack mode, and
	//      bootstrap-order in config mode without ":N" suffixes).
	sectionOrder := m.modelOrderedSections(m.filtered)
	rank := make(map[string]int, len(sectionOrder))
	for i, name := range sectionOrder {
		rank[name] = i
	}
	sort.SliceStable(m.filtered, func(a, b int) bool {
		ia, ib := m.filtered[a], m.filtered[b]
		ra := rank[m.sectionFor(m.items[ia])]
		rb := rank[m.sectionFor(m.items[ib])]
		if ra != rb {
			return ra < rb
		}
		// Within a section: annotated (ChannelOrder > 0) before
		// un-annotated; among annotated, lower wins; among
		// un-annotated, preserve input order via stable sort.
		oa, ob := m.items[ia].ChannelOrder, m.items[ib].ChannelOrder
		if (oa > 0) != (ob > 0) {
			return oa > 0
		}
		if oa != ob {
			return oa < ob
		}
		return ia < ib
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
	case navActivity:
		return cursorKey{kind: navActivity}, true
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
	sectionOrder := m.modelOrderedSections(m.filtered)

	// Bucket filter indices by section in display order.
	bucket := map[string][]int{}
	for fi, idx := range m.filtered {
		key := m.sectionFor(m.items[idx])
		bucket[key] = append(bucket[key], fi)
	}

	nav := make([]navItem, 0, 2+len(sectionOrder))
	nav = append(nav, navItem{kind: navThreads})
	nav = append(nav, navItem{kind: navActivity})
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
		case key.kind == navActivity && n.kind == navActivity:
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

// aggregateUnreadForSection returns the count of channels-with-unreads
// in the named section that are currently in m.filtered. Used to render
// an aggregate badge on collapsed section headers. Muted channels are
// excluded so the aggregate matches the per-row treatment (no dot, dim
// foreground) — the user has explicitly asked Slack to ignore those
// channels' unread activity.
//
// After the read-state sync rewrite, integer unread counts are
// abandoned in favor of a boolean has_unread per channel; section
// aggregates count channels-with-unreads instead of summing per-channel
// counts. The DB (read via readStateReader) is the source of truth.
func (m *Model) aggregateUnreadForSection(section string) int {
	var readState map[string]cache.ReadState
	if m.readStateReader != nil {
		readState = m.readStateReader()
	}
	total := 0
	for _, idx := range m.filtered {
		item := m.items[idx]
		if m.sectionFor(item) != section {
			continue
		}
		if item.IsVisiblyUnread(readState[item.ID]) {
			total++
		}
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
	// isActivityRow flags the synthetic Activity row so View() can swap
	// in the `active` variant whenever m.activityActive is true.
	isActivityRow bool
}

// buildCache rebuilds m.cacheRows for the given width. Expensive; runs only
// when items, filter, width, or theme change.
//
// Each entry in m.nav corresponds to exactly one renderRow with
// row.navIdx == nav index. View() uses this to find the selected line
// and to substitute the "selected" variant in O(1).
func (m *Model) buildCache(width int) {
	// Perf instrumentation: buildCache walks m.nav, calls readStateReader
	// (second SQLite GetWorkspaceReadState per channel switch), and
	// produces a styled row per visible item. Called on every channel
	// switch via View() after the cache is invalidated by
	// SetActiveChannelID. SLK_DEBUG-gated.
	var perfStart time.Time
	if debuglog.Enabled() {
		perfStart = time.Now()
		defer func() {
			debuglog.Perf("sidebar.buildCache width=%d items=%d nav=%d rows=%d took=%s",
				width, len(m.items), len(m.nav), len(m.cacheRows), time.Since(perfStart))
		}()
	}
	m.cacheValid = true
	m.cacheWidth = width
	m.cacheRows = m.cacheRows[:0]
	m.cacheFiller = lipgloss.NewStyle().Width(width).Background(styles.SidebarBackground).Render("")

	// Per-channel read state for the active workspace. The DB is the
	// source of truth for unread indicators; ChannelItem.UnreadCount
	// is no longer consulted by rendering. A nil reader (early
	// construction, tests without wiring) means "treat everything as
	// no-unread" — lookups on a nil map return the zero ReadState.
	var readState map[string]cache.ReadState
	if m.readStateReader != nil {
		readState = m.readStateReader()
	}

	// Reverse-index nav by section name and by filter idx so we can map
	// each rendered row back to its nav index. Headers are uniquely
	// keyed by section name; channels by their filter idx.
	headerNavIdx := map[string]int{}
	channelNavIdx := map[int]int{} // filter idx -> nav idx
	threadsIdx := -1
	activityIdx := -1
	for i, n := range m.nav {
		switch n.kind {
		case navThreads:
			threadsIdx = i
		case navActivity:
			activityIdx = i
		case navHeader:
			headerNavIdx[n.header] = i
		case navChannel:
			channelNavIdx[n.fi] = i
		}
	}

	sectionOrder := m.modelOrderedSections(m.filtered)

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
	threadsAttrs := bgAnsi
	if m.threadsUnread > 0 {
		threadsAttrs += "\x1b[1m"
	}
	threadsLabel = messages.ReapplyBgAfterResets(threadsLabel, threadsAttrs)
	threadsCursor = messages.ReapplyBgAfterResets(threadsCursor, threadsAttrs)
	threadsActiveLabel = messages.ReapplyBgAfterResets(threadsActiveLabel, threadsAttrs)
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

	// Synthetic "Activity" row, rendered directly below the Threads row.
	// Same treatment as the Threads row (see above): the App layer
	// activates the Activity view when IsActivitySelected() is true.
	activityLabel := " 🔔 Activity"
	activityCursor := cursorSelected + "🔔 Activity"
	activityActiveLabel := activeBorder + "🔔 Activity"
	if m.activityUnread > 0 {
		badge := " " + dotStyle.Render("•"+fmt.Sprintf("%d", m.activityUnread))
		activityLabel += badge
		activityCursor += badge
		activityActiveLabel += badge
	}
	activityAttrs := bgAnsi
	if m.activityUnread > 0 {
		activityAttrs += "\x1b[1m"
	}
	activityLabel = messages.ReapplyBgAfterResets(activityLabel, activityAttrs)
	activityCursor = messages.ReapplyBgAfterResets(activityCursor, activityAttrs)
	activityActiveLabel = messages.ReapplyBgAfterResets(activityActiveLabel, activityAttrs)
	activityBaseStyle := styles.ChannelNormal
	if m.activityUnread > 0 {
		activityBaseStyle = styles.ChannelUnread
	}
	activityNormal := activityBaseStyle.Width(width - 2).Render(activityLabel)
	activitySelectedRow := styles.ChannelSelected.Width(width - 2).Render(activityCursor)
	activityActiveRow := styles.ChannelSelected.Width(width - 2).Render(activityActiveLabel)
	m.cacheRows = append(m.cacheRows, renderRow{
		normal:        activityNormal,
		selected:      activitySelectedRow,
		active:        activityActiveRow,
		height:        1,
		navIdx:        activityIdx,
		isActivityRow: true,
	})

	// Blank separator between the Threads/Activity rows and the first
	// section (or below them when there are no channels at all).
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
		sectionName := m.sectionFor(item)
		if m.IsCollapsed(sectionName) {
			continue
		}
		// Defense-in-depth: if sectionFor returns a section name that
		// modelOrderedSections didn't include (e.g. an item carries a
		// stale Section ID for a section that has been deleted or
		// filtered out as non-renderable), skip it rather than
		// nil-dereferencing sectionMap[sectionName] below. The user
		// will see the channel reappear once the next refresh cycle
		// (WS event or workspace switch) re-resolves its Section.
		if _, ok := sectionMap[sectionName]; !ok {
			continue
		}

		// hasUnread is the rendering-facing boolean: "should this row
		// pop visually?" It's a function of the DB's has_unread for
		// this channel AND the user's mute pref. Muted channels never
		// pop, even when they have new messages — Slack's contract is
		// "muted = no notification surface". The dimmer ChannelMuted
		// style below distinguishes muted-with-unreads from a fully
		// read row visually. Predicate lives on ChannelItem so the
		// App's tab-title counter and section aggregates agree.
		hasUnread := item.IsVisiblyUnread(readState[item.ID])

		// Unread dot indicator (same regardless of selection state).
		unreadDot := " "
		if hasUnread {
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
			// Muted channels use the plain glyph regardless of unread
			// state so the row reads as quiet across all of its parts.
			if hasUnread {
				prefix = privatePrefix
			} else {
				prefix = privatePrefixMuted
			}
		case "app":
			if hasUnread {
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

		// Re-apply theme attrs after ANSI resets emitted by inline styled
		// glyphs (cursor, prefix, unread dot) so the outer lipgloss
		// style isn't overwritten for the post-reset span. We need
		// THREE separate reapply payloads because each label variant
		// uses a different outer style and therefore a different
		// foreground:
		//
		//   labelNormal   → ChannelNormal (muted fg) / ChannelUnread
		//                   (bright fg + bold) / ChannelMuted (muted)
		//   labelSelected → ChannelSelected (bright fg + bold)
		//   labelActive   → ChannelSelected (bright fg + bold)
		//
		// The loop-invariant `bgAnsi` carries the bright SidebarFgANSI
		// (suitable for the Selected/Active variants), so we override
		// the foreground component for labelNormal whenever the row's
		// base style is muted (read or globally-muted rows). Without
		// this override, styled DM presence prefixes like green ●
		// reset the bright sidebar fg back in and the trailing name
		// renders visibly brighter than read public channels which
		// have no inline ANSI in their prefix at all.
		boldAttr := ""
		if hasUnread {
			boldAttr = "\x1b[1m"
		}
		normalFg := messages.SidebarMutedFgANSI()
		if hasUnread {
			normalFg = messages.SidebarFgANSI()
		}
		normalAttrs := messages.SidebarBgANSI() + normalFg + boldAttr
		selectedAttrs := bgAnsi + boldAttr
		labelNormal = messages.ReapplyBgAfterResets(labelNormal, normalAttrs)
		labelSelected = messages.ReapplyBgAfterResets(labelSelected, selectedAttrs)
		labelActive = messages.ReapplyBgAfterResets(labelActive, selectedAttrs)

		// Pick base style for non-selected state. Muted always wins
		// over Unread/Normal so muted-with-unreads renders dim, not
		// bright-and-bold. (hasUnread already excludes muted, but
		// keep the explicit IsMuted arm first for clarity.)
		var baseStyle lipgloss.Style
		switch {
		case item.IsMuted:
			baseStyle = styles.ChannelMuted
		case hasUnread:
			baseStyle = styles.ChannelUnread
		default:
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

// sectionDisplayMeta returns the user-visible name and emoji shortcode
// for a section as currently identified in the nav. In Slack mode the
// nav header is the section ID; in config mode it's the section name
// (no emoji available).
func (m *Model) sectionDisplayMeta(sectionKey string) (name, emoji string) {
	if m.useSlackSections() {
		for _, meta := range m.sectionsProvider.OrderedSlackSections() {
			if meta.ID == sectionKey {
				name = meta.Name
				if name == "" {
					// Built-in sections Slack sends without a user-set
					// name need a friendly fallback. Today only "stars"
					// reaches this branch (other unnamed system types
					// are filtered by SectionStore.includeInSidebar).
					if meta.Type == "stars" {
						name = "Starred"
					} else {
						name = "(unnamed)"
					}
				}
				return name, meta.Emoji
			}
		}
		// ID not found in provider (shouldn't happen if nav is fresh).
		return sectionKey, ""
	}
	return sectionKey, ""
}

// renderSectionHeaderLabel returns the (normal, selected) label
// strings for a section header. Headers show a triangle indicating
// expand/collapse state and, when collapsed, an aggregate unread badge
// counting channels-with-unreads across every visible item in the
// section (sourced from the read-state DB via readStateReader).
//
// In Slack mode, the `name` parameter is a section ID — we look up the
// user-visible name and (if any) emoji shortcode from the provider and
// prepend the resolved emoji.
func (m *Model) renderSectionHeaderLabel(name, cursor string, dotStyle lipgloss.Style, bgAnsi string) (string, string) {
	displayName, emojiCode := m.sectionDisplayMeta(name)
	emojiPrefix := ""
	if emojiCode != "" {
		// kyokomi/emoji.Sprint resolves :shortcode: to unicode; on
		// unknown shortcodes it returns the input unchanged (which
		// keeps the colons, giving a graceful textual fallback).
		token := ":" + emojiCode + ":"
		rendered := emojiutil.Sprint(token)
		if rendered != token {
			emojiPrefix = rendered + " "
		} else {
			emojiPrefix = token + " "
		}
	}

	glyph := "▾" // expanded
	if m.IsCollapsed(name) {
		glyph = "▸"
	}
	label := " " + glyph + " " + emojiPrefix + displayName
	if m.IsCollapsed(name) {
		if n := m.aggregateUnreadForSection(name); n > 0 {
			label += " " + dotStyle.Render("•"+fmt.Sprintf("%d", n))
		}
	}
	selected := cursor + glyph + " " + emojiPrefix + displayName
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
		case r.isActivityRow && m.activityActive && r.active != "":
			// Activity view is the currently displayed view; mark the
			// Activity row as active with the same orange indicator.
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

const (
	defaultWidth = 30
	minWidth     = 20
	maxWidth     = 60
	widthStep    = 5
)

func (m Model) Width() int {
	if m.width == 0 {
		return defaultWidth
	}
	return m.width
}

func (m *Model) SetWidth(w int) {
	if w < minWidth {
		w = minWidth
	}
	if w > maxWidth {
		w = maxWidth
	}
	m.width = w
	m.cacheValid = false
	m.dirty()
}

func (m *Model) GrowWidth() {
	w := m.Width()
	if w+widthStep <= maxWidth {
		m.width = w + widthStep
		m.cacheValid = false
		m.dirty()
	}
}

func (m *Model) ShrinkWidth() {
	w := m.Width()
	if w-widthStep >= minWidth {
		m.width = w - widthStep
		m.cacheValid = false
		m.dirty()
	}
}
