package channelfinder

import (
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

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

// Item represents a searchable channel/DM entry.
type Item struct {
	ID       string
	Name     string
	Type     string // channel, dm, group_dm, private, threads
	Presence string // for DMs: active, away
	Joined   bool   // true if the user is already a member; false for browseable public channels
	// LastVisited is the unix timestamp (seconds) of the user's most
	// recent visit to this channel; 0 means never visited. Drives the
	// recency-based sort used by filter(): empty-query order is by
	// LastVisited DESC, and on a query LastVisited breaks ties within
	// a match tier.
	LastVisited int64
	// Synthetic marks non-channel destinations (e.g. "Threads") that
	// the finder pins above real channels under empty-query and that
	// callers route differently (e.g. activating a view rather than
	// opening a channel). These items are preserved across SetItems
	// and SetBrowseable mutations so the finder always offers them.
	Synthetic bool
}

// Model is the fuzzy channel finder overlay.
type Model struct {
	items    []Item
	filtered []int // indices into items matching query
	query    string
	selected int // index into filtered
	visible  bool
}

// New creates a new channel finder.
func New() Model {
	return Model{}
}

// SetItems updates the searchable channel list. Synthetic items previously
// registered via SetSyntheticItems are preserved at the front of the list so
// non-channel destinations (e.g. the Threads view) remain reachable across
// workspace bootstraps.
func (m *Model) SetItems(items []Item) {
	synth := m.extractSynthetic()
	m.items = append(synth, items...)
}

// SetSyntheticItems replaces the set of non-channel destinations the finder
// offers alongside channels (e.g. the Threads view). These rows are pinned
// above real channels under empty-query and preserved across SetItems /
// SetBrowseable. Pass nil to clear.
func (m *Model) SetSyntheticItems(items []Item) {
	// Drop existing synthetic rows; keep real channels intact.
	keep := m.items[:0]
	for _, it := range m.items {
		if !it.Synthetic {
			keep = append(keep, it)
		}
	}
	// Prepend the new synthetic rows, marking each.
	merged := make([]Item, 0, len(items)+len(keep))
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
func (m *Model) extractSynthetic() []Item {
	var synth []Item
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
// WorkspaceContext.FinderItems. LastVisited is preserved from the existing
// entry when the incoming item doesn't specify one.
func (m *Model) Upsert(item Item) {
	for i := range m.items {
		if m.items[i].ID == item.ID {
			if item.LastVisited == 0 {
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
func (m *Model) SetBrowseable(browseable []Item) {
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

// listTopOffset is the box-local row of the first list row: top border
// (1) + top padding (1) + title (1) + input (1) + blank separator (1).
// Shared by renderBox (implicitly) and ClickRow's hit-testing.
const listTopOffset = 5

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

// visibleWindow returns the [start, end) slice of m.filtered currently
// shown in the results list, applying the same scroll-window math the
// renderer uses so hit-testing and rendering agree.
func (m *Model) visibleWindow() (int, int) {
	maxVisible := maxVisibleRows
	total := len(m.filtered)
	if maxVisible > total {
		maxVisible = total
	}
	startIdx := 0
	if m.selected >= maxVisible {
		startIdx = m.selected - maxVisible + 1
	}
	endIdx := startIdx + maxVisible
	if endIdx > total {
		endIdx = total
		startIdx = endIdx - maxVisible
		if startIdx < 0 {
			startIdx = 0
		}
	}
	return startIdx, endIdx
}

// BoxSize returns the outer dimensions of the rendered modal box for the
// given terminal size. termHeight is unused (this modal's height depends
// only on its row count) but kept for interface symmetry with overlays
// whose height is terminal-dependent.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := m.visibleWindow()
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
	row := localY - listTopOffset
	if row < 0 {
		return false
	}
	start, end := m.visibleWindow()
	if row >= end-start {
		return false
	}
	m.selected = start + row
	return true
}

// Open shows the overlay and resets state.
func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
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
			m.query = m.query[:len(m.query)-1]
			m.selected = 0
			m.filter()
		}
		return nil
	}

	// If it's a single printable rune, add to query
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}

	return nil
}

// filter rebuilds the filtered list based on the current query.
//
// Ranking precedence (most significant first):
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
	m.filtered = nil
	q := text.Fold(m.query)

	if q == "" {
		idxs := make([]int, len(m.items))
		for i := range m.items {
			idxs[i] = i
		}
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
	for i, item := range m.items {
		name := text.Fold(item.Name)
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

	// Overlay dimensions
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4 // border + padding

	// All inner spans share the modal bg so the dimmed app behind the
	// overlay doesn't bleed through where individual styled fragments end.
	bg := styles.Background

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("Switch Channel")

	// Query input with blue left border
	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "█ " + placeholder
	} else {
		inputText = m.query + "█"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		PaddingLeft(1).
		Background(bg).
		Foreground(styles.TextPrimary).
		Render(inputText)

	// Results (max 10). Scroll window shared with ClickRow hit-testing.
	total := len(m.filtered)
	startIdx, endIdx := m.visibleWindow()
	maxVisible := endIdx - startIdx

	// Scrollbar: shown only when the list overflows the visible window. We
	// reserve one column on the right; the row content shrinks by 1 to make
	// room. Thumb size and position are proportional so users see at a
	// glance how much more content is above/below the visible window.
	showScrollbar := total > maxVisible
	contentWidth := innerWidth - 1 // 1 col indicator/space prefix
	if showScrollbar {
		contentWidth-- // 1 col for the scrollbar gutter
	}

	var thumbStart, thumbEnd int
	if showScrollbar {
		thumbHeight := maxVisible * maxVisible / total
		if thumbHeight < 1 {
			thumbHeight = 1
		}
		denom := total - maxVisible
		if denom < 1 {
			denom = 1
		}
		thumbStart = startIdx * (maxVisible - thumbHeight) / denom
		if thumbStart < 0 {
			thumbStart = 0
		}
		if thumbStart > maxVisible-thumbHeight {
			thumbStart = maxVisible - thumbHeight
		}
		thumbEnd = thumbStart + thumbHeight
	}
	thumbStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Primary)
	trackStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Border)

	var resultRows []string
	for i := startIdx; i < endIdx; i++ {
		idx := m.filtered[i]
		item := m.items[idx]

		isSelected := i == m.selected

		// Render prefix and name as SEPARATE styled fragments. If we built a
		// single string and ran one outer style over it, the prefix's own
		// ANSI reset (\x1b[0m) would drop the outer foreground / faint
		// attributes for everything after it, defeating the dim treatment.
		var prefix, name string
		if item.Joined {
			prefix = channelPrefix(item)
			nameStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
			if isSelected {
				nameStyle = nameStyle.Background(bg).Foreground(styles.Primary).Bold(true)
			}
			name = nameStyle.Render(item.Name)
		} else {
			// Non-joined: dim grey for everything, including the prefix.
			dim := lipgloss.NewStyle().Background(bg).Foreground(nonJoinedColor)
			prefix = dim.Render("#")
			name = dim.Render(item.Name)
		}

		line := prefix + " " + name
		// Truncate to fit (truncate.StringWithTail is ANSI-aware).
		if lipgloss.Width(line) > contentWidth {
			line = truncate.StringWithTail(line, uint(contentWidth), "…")
		}
		// Right-pad with spaces to fill the row.
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}

		var row string
		if isSelected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("▌")
			row = indicator + line
		} else {
			row = " " + line
		}

		if showScrollbar {
			rel := i - startIdx
			if rel >= thumbStart && rel < thumbEnd {
				row += thumbStyle.Render("\u2588") // █ thumb
			} else {
				row += trackStyle.Render("\u2502") // │ track
			}
		}
		resultRows = append(resultRows, row)
	}

	if len(m.filtered) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching channels")
		resultRows = append(resultRows, noResults)
	}

	// Compose the overlay content
	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	// Re-paint modal bg+fg after every ANSI reset emitted by inner styled
	// spans (channelPrefix glyphs, name fragments, etc.). Without this,
	// trailing spaces and adjacent unstyled cells inherit the dimmed app
	// behind the overlay, producing visible "highlight boxes" behind
	// the styled text on the row.
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	// Wrap in a bordered box
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}

// channelPrefix returns the display prefix for a channel type.
func channelPrefix(item Item) string {
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

// Query returns the text the user has typed. Callers outside this
// package need it to decide whether a keystroke changed the query and
// therefore whether a server-side search is worth scheduling — the
// finder's own matching is local and does not.
func (m Model) Query() string {
	return m.query
}

// Items returns every row the finder holds: joined, synthetic and
// browseable alike, in insertion order and unfiltered.
func (m Model) Items() []Item {
	return append([]Item(nil), m.items...)
}

// FilteredItems returns the rows matching the current query, in the
// order they are rendered.
func (m Model) FilteredItems() []Item {
	out := make([]Item, 0, len(m.filtered))
	for _, idx := range m.filtered {
		out = append(out, m.items[idx])
	}
	return out
}
