package channelfinder

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// deactivatedTag follows the name of a DM with a deactivated user.
const deactivatedTag = "deactivated"

// nonJoinedColor is a hard-coded dim grey used for channels the user is not a
// member of, so the dim treatment is unmistakable across all themes and does
// not rely on terminal Faint support (which many emulators render weakly or
// not at all).
var nonJoinedColor = lipgloss.Color("#5a5a5a")

// ThreadsViewID is the sentinel ID used for the synthetic "Threads" entry.
// Callers detect this in a ChannelResult (Type=="threads") to activate the
// threads-list view instead of switching channels.
const ThreadsViewID = "__slk_view_threads"

// ChannelResult is returned when the user selects a channel.
type ChannelResult struct {
	ID     string
	Name   string
	Type   string // channel, dm, group_dm, private, threads
	Joined bool   // false => caller should join the channel before opening it
}

// Model is the fuzzy channel finder overlay.
type Model struct {
	items      []core.ChannelFinderItem
	filtered   []int // indices into items matching query
	query      string
	selected   int // index into filtered
	visible    bool
	forwarding bool

	// statuses holds each DM channel's custom status and DND, keyed by
	// channel ID (see SetStatus). Pruned to the current item set on
	// every SetItems.
	statuses map[string]peerstatus.Status

	// emojiCtx draws a DM peer's status emoji as an image when image
	// mode is on (SetEmojiContext, SetEmojiCustoms).
	emojiCtx messages.EmojiContext

	// deactivated is the set of user IDs whose DMs the finder offers
	// only to a search, marked and below every other match, and never
	// for forwarding. The App owns it: it is published whole and never
	// written here, and nil treats everyone the same (the option is off).
	deactivated map[string]bool
	// unsendable is every deactivated user, whatever the option says:
	// their DMs are never a forwarding destination, as Slack takes no
	// messages there. Owned by the App, like deactivated.
	unsendable map[string]bool
}

// SetDeactivatedUsers sets the user IDs whose DMs are offered only to a
// search; nil offers everyone the same way. The map is kept, not copied,
// so the caller must not write it afterwards. An open overlay is
// refiltered.
func (m *Model) SetDeactivatedUsers(users map[string]bool) {
	m.deactivated = users
	if m.visible {
		m.filter()
	}
}

// SetEmojiContext configures drawing DM status emoji as images. The
// finder caches no rows, so the next View uses it.
func (m *Model) SetEmojiContext(ctx messages.EmojiContext) {
	m.emojiCtx = ctx
}

// SetEmojiCustoms replaces only the workspace custom emoji map, so a
// custom status emoji that arrives after startup shows its artwork.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiCtx.Customs = customs
}

// SetUnsendableUsers sets the user IDs whose DMs forwarding never offers:
// every deactivated user, also with the option off. The map is kept, not
// copied, like SetDeactivatedUsers'. An open overlay is refiltered.
func (m *Model) SetUnsendableUsers(users map[string]bool) {
	m.unsendable = users
	if m.visible {
		m.filter()
	}
}

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// New creates a new channel finder.
func New() Model {
	return Model{}
}

// SetItems updates the searchable channel list. Synthetic items previously
// registered via SetSyntheticItems are preserved at the front of the list so
// non-channel destinations (e.g. the Threads view) remain reachable across
// workspace bootstraps.
func (m *Model) SetItems(items []core.ChannelFinderItem) {
	synth := m.extractSynthetic()
	m.items = append(synth, items...)
	m.pruneStatuses()
	if m.visible && m.forwarding {
		m.filter()
	}
}

// pruneStatuses drops any status entry whose channel ID is no longer in
// m.items, so a status never outlives the row it belongs to.
func (m *Model) pruneStatuses() {
	if len(m.statuses) == 0 {
		return
	}
	have := make(map[string]struct{}, len(m.items))
	for _, it := range m.items {
		have[it.ID] = struct{}{}
	}
	for id := range m.statuses {
		if _, ok := have[id]; !ok {
			delete(m.statuses, id)
		}
	}
}

// SetSyntheticItems replaces the set of non-channel destinations the finder
// offers alongside channels (e.g. the Threads view). These rows are pinned
// above real channels under empty-query and preserved across SetItems /
// SetBrowseable. Pass nil to clear.
func (m *Model) SetSyntheticItems(items []core.ChannelFinderItem) {
	// Drop existing synthetic rows; keep real channels intact.
	keep := m.items[:0]
	for _, it := range m.items {
		if !it.Synthetic {
			keep = append(keep, it)
		}
	}
	// Prepend the new synthetic rows, marking each.
	merged := make([]core.ChannelFinderItem, 0, len(items)+len(keep))
	for _, it := range items {
		it.Synthetic = true
		merged = append(merged, it)
	}
	merged = append(merged, keep...)
	m.items = merged
	if m.visible {
		m.filter()
	}
}

// extractSynthetic returns the currently registered synthetic items in their
// existing order; used by SetItems / SetBrowseable to preserve them across
// list mutations.
func (m *Model) extractSynthetic() []core.ChannelFinderItem {
	var synth []core.ChannelFinderItem
	for _, it := range m.items {
		if it.Synthetic {
			synth = append(synth, it)
		}
	}
	return synth
}

// Upsert adds item to the finder, or replaces the existing entry with the
// same ID in place. Used when a single new conversation becomes known
// outside of a full SetItems refresh (e.g. a live im_created/mpim_open WS
// event or a Ctrl+N-initiated DM), so it shows up in Ctrl+P immediately
// instead of waiting for the next workspace activation to pick up
// WorkspaceContext.FinderItems. LastVisited keeps the newer of the existing
// and incoming visit times.
func (m *Model) Upsert(item core.ChannelFinderItem) {
	for i := range m.items {
		if m.items[i].ID == item.ID {
			if item.LastVisited < m.items[i].LastVisited {
				item.LastVisited = m.items[i].LastVisited
			}
			item.Synthetic = m.items[i].Synthetic
			m.items[i] = item
			if m.visible {
				m.filter()
			}
			return
		}
	}
	m.items = append(m.items, item)
	if m.visible {
		m.filter()
	}
}

// MarkJoined flips the Joined bit on a channel that the user just joined,
// so it stops rendering as dimmed and the next Enter on it skips the join
// step.
func (m *Model) MarkJoined(channelID string) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			m.items[i].Joined = true
			return
		}
	}
}

// UpdateLastVisited sets the LastVisited timestamp for the matching
// item, if any, and re-runs filter() if the overlay is currently
// visible so the new ordering takes effect on the next render. No-op
// for an unknown ID.
func (m *Model) UpdateLastVisited(channelID string, ts int64) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			m.items[i].LastVisited = ts
			if m.visible {
				m.filter()
			}
			return
		}
	}
}

// SetBrowseable replaces the non-joined channel entries in the finder.
// Joined items (added via SetItems) and synthetic destinations (added via
// SetSyntheticItems) are preserved; previous non-joined items are dropped
// and replaced with the new set. Items whose IDs already appear among the
// joined / synthetic entries are skipped to avoid duplicates.
func (m *Model) SetBrowseable(browseable []core.ChannelFinderItem) {
	// Drop existing non-joined items; keep joined + synthetic rows.
	keep := m.items[:0]
	have := make(map[string]struct{}, len(m.items))
	for _, it := range m.items {
		if it.Joined || it.Synthetic {
			keep = append(keep, it)
			have[it.ID] = struct{}{}
		}
	}
	m.items = keep
	for _, it := range browseable {
		if _, dup := have[it.ID]; dup {
			continue
		}
		it.Joined = false
		m.items = append(m.items, it)
	}
	// Re-filter against current query so the new items appear immediately if
	// the overlay is open.
	if m.visible {
		m.filter()
	}
}

// listTopOffset is the box-local row of the first list row.
func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), true) }

// maxVisibleRows is the height of the scroll window for the results list.
const maxVisibleRows = 10

// boxWidth returns the modal's outer width for a given terminal width.
// Single source of truth for renderBox and BoxSize.
func boxWidth(termWidth int) int {
	w := termWidth / 2
	if w < 30 {
		w = 30
	}
	if w > 80 {
		w = 80
	}
	return w
}

// BoxSize returns the outer dimensions of the rendered modal box for the
// given terminal size. termHeight is unused (this modal's height depends
// only on its row count) but kept for interface symmetry with overlays
// whose height is terminal-dependent.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	nRows := end - start
	if nRows < 1 {
		nRows = 1 // empty list still renders one (message/placeholder) row
	}
	// height = top border + top padding + title + input + blank + rows +
	// bottom padding + bottom border = nRows + 7.
	return boxWidth(termWidth), nRows + 7
}

// ClickRow maps a box-local row (localY, 0 = box top border) to a result
// row. When the click lands on a visible list row it moves the selection
// there and returns true; otherwise it returns false. termWidth/termHeight
// are accepted for interface symmetry and currently unused.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	if row < 0 {
		return false
	}
	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	if row >= end-start {
		return false
	}
	m.selected = start + row
	return true
}

// Open shows the channel-switching overlay and resets state.
func (m *Model) Open() {
	m.open(false)
}

// OpenForForwarding shows the overlay with only joined, non-synthetic
// destinations and resets the query and selection. The full item list is
// preserved for the next Open; selections use the same ChannelResult API.
func (m *Model) OpenForForwarding() {
	m.open(true)
}

func (m *Model) open(forwarding bool) {
	m.forwarding = forwarding
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
	m.forwarding = false
}

// IsVisible returns whether the overlay is showing.
func (m Model) IsVisible() bool {
	return m.visible
}

// HandleKey processes a key event and returns a ChannelResult if the user
// selected a channel, or nil otherwise.
func (m *Model) HandleKey(keyStr string) *ChannelResult {
	switch keyStr {
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &ChannelResult{
				ID:     m.items[idx].ID,
				Name:   m.items[idx].Name,
				Type:   m.items[idx].Type,
				Joined: m.items[idx].Joined,
			}
		}
		return nil

	case "esc":
		m.Close()
		return nil

	case "down", "ctrl+n":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil

	case "up", "ctrl+p":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "backspace":
		if len(m.query) > 0 {
			_, n := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-n]
			m.selected = 0
			m.filter()
		}
		return nil
	}

	// If it's a single printable rune, add to query
	if r := []rune(keyStr); len(r) == 1 && unicode.IsPrint(r[0]) {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}

	return nil
}

// filter rebuilds the filtered list based on the current query.
//
// Ranking precedence (most significant first):
//  0. A DM with a deactivated user is left out under empty-query and, with
//     a query, comes after every other match.
//  1. Synthetic destinations (e.g. Threads view) under empty-query — pinned
//     to the top so non-channel views are always discoverable. With a query,
//     synthetic items rank by name match like everything else.
//  2. Joined (members of the channel/DM come before non-members)
//  3. Match tier: prefix > substring > subsequence (only when querying)
//  4. LastVisited DESC (recency of user's last visit)
//  5. Subsequence score DESC (only relevant in the subsequence tier)
//  6. typeRank ASC (group_dm demoted; 1:1 DMs and channels equal)
//  7. Name ASC (case-insensitive)
//
// Matching tiers:
//  1. Prefix matches  (e.g. "eng" matches "engineering")
//  2. Substring matches (e.g. "tomo" matches "ext-automote")
//  3. Subsequence matches (e.g. "csp" matches "cs-product-triage" because
//     c, s, p appear in order). Tighter matches with more word-boundary
//     hits score higher.
func (m *Model) filter() {
	// Live updates can remove the selected forwarding destination.
	defer func() {
		if m.selected >= len(m.filtered) {
			m.selected = max(0, len(m.filtered)-1)
		}
	}()
	m.filtered = nil
	q := text.Fold(m.query)
	idxs := make([]int, 0, len(m.items))
	for i, item := range m.items {
		if m.forwarding && (!item.Joined || item.Synthetic) {
			continue
		}
		// A deactivated DM is found only by searching for it, and is
		// never a forwarding destination: Slack takes no messages there,
		// whether or not the option hides it.
		if strings.TrimSpace(q) == "" && m.isDeactivated(item) {
			continue
		}
		if m.forwarding && core.IsHiddenPeerConversation(item.Type, item.DMUserID, m.unsendable) {
			continue
		}
		idxs = append(idxs, i)
	}

	if q == "" {
		sort.SliceStable(idxs, func(i, j int) bool {
			return m.lessNoQuery(idxs[i], idxs[j])
		})
		m.filtered = idxs
		return
	}

	type match struct {
		idx   int
		tier  int // 0 prefix, 1 substring, 2 subsequence
		score int // subsequence score; 0 for prefix/substring
	}

	var matches []match
	for _, i := range idxs {
		name := text.Fold(m.items[i].Name)
		switch {
		case strings.HasPrefix(name, q):
			matches = append(matches, match{idx: i, tier: 0})
		case strings.Contains(name, q):
			matches = append(matches, match{idx: i, tier: 1})
		default:
			if score, ok := subsequenceScore(name, q); ok {
				matches = append(matches, match{idx: i, tier: 2, score: score})
			}
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		ai, bi := matches[i].idx, matches[j].idx
		a, b := m.items[ai], m.items[bi]
		// 0. A deactivated DM after every other match.
		if ad, bd := m.isDeactivated(a), m.isDeactivated(b); ad != bd {
			return bd
		}
		// 1. Joined first.
		if a.Joined != b.Joined {
			return a.Joined
		}
		// 2. Match tier.
		if matches[i].tier != matches[j].tier {
			return matches[i].tier < matches[j].tier
		}
		// 3. Recency.
		if a.LastVisited != b.LastVisited {
			return a.LastVisited > b.LastVisited
		}
		// 4. Subsequence score (only meaningful inside tier 2; ties at 0 elsewhere).
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		// 5. typeRank (group_dm demoted).
		if ar, br := m.typeRank(ai), m.typeRank(bi); ar != br {
			return ar < br
		}
		// 6. Name.
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	m.filtered = make([]int, len(matches))
	for i, mm := range matches {
		m.filtered[i] = mm.idx
	}
}

// isDeactivated reports whether item is a DM with a deactivated user.
func (m *Model) isDeactivated(item core.ChannelFinderItem) bool {
	return core.IsHiddenPeerConversation(item.Type, item.DMUserID, m.deactivated)
}

// typeRank returns a sort key for an item: lower comes first. 1:1 DMs
// and channels are considered equal weight; only group DMs are demoted.
func (m *Model) typeRank(idx int) int {
	if m.items[idx].Type == "group_dm" {
		return 1
	}
	return 0
}

// subsequenceScore returns a score and true if every rune of q appears in
// name in order. The score rewards:
//   - matches that hit word boundaries (start of name, or after a separator
//     like '-', '_', '.', ' ', or '/')
//   - tighter matches (smaller span between first and last matched rune)
//
// Both name and q are expected to already be lowercased.
func subsequenceScore(name, q string) (int, bool) {
	if q == "" {
		return 0, true
	}

	score := 0
	qi := 0
	qrunes := []rune(q)
	first, last := -1, -1
	prevWasSep := true // start of string counts as a word boundary
	for i, r := range name {
		if qi >= len(qrunes) {
			break
		}
		if r == qrunes[qi] {
			if first < 0 {
				first = i
			}
			last = i
			score += 10
			if prevWasSep {
				score += 25 // word-boundary bonus
			}
			qi++
		}
		prevWasSep = isSeparator(r)
	}
	if qi < len(qrunes) {
		return 0, false
	}
	// Tightness bonus: the closer first and last are, the better. Cap so a
	// pathological long name can't dominate.
	span := last - first + 1
	if span > 0 {
		// Up to ~50 points for a perfectly tight match (span == len(q)).
		score += 50 * len(qrunes) / span
	}
	return score, true
}

func isSeparator(r rune) bool {
	switch r {
	case '-', '_', '.', ' ', '/', ':':
		return true
	}
	return false
}

// lessNoQuery reports whether item a should sort before item b when no
// search query is active. Order: Synthetic DESC (pinned top), Joined DESC,
// LastVisited DESC, typeRank ASC, Name ASC (case-insensitive).
func (m *Model) lessNoQuery(ai, bi int) bool {
	a, b := m.items[ai], m.items[bi]
	if a.Synthetic != b.Synthetic {
		return a.Synthetic
	}
	if a.Joined != b.Joined {
		return a.Joined
	}
	if a.LastVisited != b.LastVisited {
		return a.LastVisited > b.LastVisited
	}
	if ar, br := m.typeRank(ai), m.typeRank(bi); ar != br {
		return ar < br
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

// View renders just the overlay box.
func (m Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// ViewOverlay renders the overlay as a centered modal with a dark backdrop.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}

	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}

	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func (m Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}
	s := messages.ChromeStyles()
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4 // border + padding

	// All inner spans share the modal bg so the dimmed app behind the
	// overlay doesn't bleed through where individual styled fragments end.
	bg := styles.ThemeBackground()

	titleText := "Switch Channel"
	if m.forwarding {
		titleText = "Forward message to…"
	}

	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	list := chrome.List{Start: start, End: end, Total: len(m.filtered)}
	contentWidth := list.ContentWidth(innerWidth)

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		idx := m.filtered[i]
		item := m.items[idx]
		st := m.statuses[item.ID]

		isSelected := i == m.selected

		// Render prefix and name as SEPARATE styled fragments. If we built a
		// single string and ran one outer style over it, the prefix's own
		// ANSI reset (\x1b[0m) would drop the outer foreground / faint
		// attributes for everything after it, defeating the dim treatment.
		var prefix, name string
		// image is the status emoji placement, imageStyle the name's
		// style it is drawn in (for the row background under it).
		var image string
		var imageStyle lipgloss.Style
		switch {
		case m.isDeactivated(item):
			// Dim like a non-joined channel, with no presence or
			// status, and a tag after the name. The tag is charged to
			// the name's budget so a long name truncates first.
			dim := lipgloss.NewStyle().Background(bg).Foreground(nonJoinedColor)
			prefix = dim.Render("○")
			label := item.Name
			nameBudget := max(contentWidth-lipgloss.Width(prefix)-1-len(deactivatedTag)-1, 1)
			if lipgloss.Width(label) > nameBudget {
				label = truncate.StringWithTail(label, uint(nameBudget), "…")
			}
			name = dim.Render(label) + dim.Render(" ") + dim.Italic(true).Render(deactivatedTag)
		case item.Joined:
			prefix = channelPrefix(item, st)
			nameStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
			if isSelected {
				nameStyle = nameStyle.Background(bg).Foreground(styles.Primary).Bold(true)
			}
			// Charge the status glyph to the name's own truncation
			// budget (like the sidebar's buildCache) so a long name
			// truncates before the glyph is lost.
			// In image mode the emoji is drawn as an image, charged its
			// cell footprint and appended after every truncation below,
			// so truncation never cuts it. Its upload fires here: the
			// finder renders every frame and caches nothing.
			label := item.Name
			glyph := st.Glyph(time.Now())
			glyphCells := 0
			if glyph != "" {
				glyphCells = 1 + st.GlyphWidth(time.Now())
				if placed, flush, ok := m.emojiCtx.PlaceShortcode(st.GlyphShortcode(time.Now())); ok {
					image = placed
					glyphCells = 1 + m.emojiCtx.ImageCells()
					if flush != nil {
						_ = flush(imgpkg.KittyOutput)
					}
				}
			}
			nameBudget := contentWidth - lipgloss.Width(prefix) - 1 - glyphCells
			if nameBudget < 1 {
				nameBudget = 1
			}
			if lipgloss.Width(label) > nameBudget {
				label = truncate.StringWithTail(label, uint(nameBudget), "…")
			}
			if glyph != "" && image == "" {
				label += " " + glyph
			}
			name = nameStyle.Render(label)
			imageStyle = nameStyle
		default:
			// Non-joined: dim grey for everything, including the prefix.
			dim := lipgloss.NewStyle().Background(bg).Foreground(nonJoinedColor)
			prefix = dim.Render("#")
			name = dim.Render(item.Name)
		}

		line := prefix + " " + name
		// Backstop for the non-joined branch; the joined branch above
		// already budgets to fit. An image is appended after it, in
		// the room left for it.
		lineWidth := contentWidth
		if image != "" {
			lineWidth = max(contentWidth-1-m.emojiCtx.ImageCells(), 0)
		}
		if lipgloss.Width(line) > lineWidth {
			line = truncate.StringWithTail(line, uint(lineWidth), "…")
		}
		if image != "" {
			line += imageStyle.Render(" " + image)
		}
		// Right-pad with spaces to fill the row.
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}

		rows = append(rows, line)
	}

	f := chrome.Frame{
		Title: titleText,
		Input: chrome.FilterInput(s, m.query, "Type to filter..."),
		Body:  list.Rows(s, m.selected, rows),
	}
	if m.query != "" {
		f.Empty = "No matching channels"
	}
	return f.Render(s, overlayWidth)
}

// channelPrefix returns the display prefix for a channel type. st is
// the DM peer's status (zero value for anything else).
func channelPrefix(item core.ChannelFinderItem, st peerstatus.Status) string {
	switch item.Type {
	case "threads":
		// Single-cell flag glyph marks the synthetic "Threads" row as
		// a destination, not a channel. We use ⚑ (U+2691) rather than
		// the 🚩 emoji because emoji glyphs occupy two terminal cells
		// in most fonts, which shifts the row's name one column to
		// the right and breaks alignment with the channel/DM prefixes
		// around it. Accent color keeps it visually distinct without
		// double-width.
		return lipgloss.NewStyle().Foreground(styles.Accent).Render("⚑")
	case "private":
		return lipgloss.NewStyle().Foreground(styles.Warning).Render("◆")
	case "dm":
		if st.InDND(time.Now()) {
			return lipgloss.NewStyle().Foreground(styles.Warning).Render(peerstatus.DNDGlyph)
		}
		if item.Presence == "active" {
			return lipgloss.NewStyle().Foreground(styles.Accent).Render("●")
		}
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("○")
	case "group_dm":
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("●")
	default:
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("#")
	}
}

// SetStatus updates the custom status and DND shown on the row with the
// given channel ID. No-op for an empty ID; a zero-value st deletes the
// entry instead of storing it.
func (m *Model) SetStatus(channelID string, st peerstatus.Status) {
	if channelID == "" {
		return
	}
	if st == (peerstatus.Status{}) {
		delete(m.statuses, channelID)
		return
	}
	if m.statuses == nil {
		m.statuses = make(map[string]peerstatus.Status)
	}
	m.statuses[channelID] = st
}

// Query returns the text the user has typed. Callers outside this
// package need it to decide whether a keystroke changed the query and
// therefore whether a server-side search is worth scheduling — the
// finder's own matching is local and does not.
func (m Model) Query() string {
	return m.query
}

// Items returns every row the finder holds: joined, synthetic and
// browseable alike, in insertion order and unfiltered.
func (m Model) Items() []core.ChannelFinderItem {
	return append([]core.ChannelFinderItem(nil), m.items...)
}

// StatusFor returns the DM status set on channelID via SetStatus, the
// zero value when none is set.
func (m Model) StatusFor(channelID string) peerstatus.Status {
	return m.statuses[channelID]
}

// FilteredItems returns the rows matching the current query, in the
// order they are rendered.
func (m Model) FilteredItems() []core.ChannelFinderItem {
	out := make([]core.ChannelFinderItem, 0, len(m.filtered))
	for _, idx := range m.filtered {
		out = append(out, m.items[idx])
	}
	return out
}
