package channelfinder

import (
	"strings"

	"charm.land/lipgloss/v2"
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

// ChannelResult is returned when the user selects a channel.
type ChannelResult struct {
	ID     string
	Name   string
	Joined bool // false => caller should join the channel before opening it
}

// Item represents a searchable channel/DM entry.
type Item struct {
	ID       string
	Name     string
	Type     string // channel, dm, group_dm, private
	Presence string // for DMs: active, away
	Joined   bool   // true if the user is already a member; false for browseable public channels
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

// SetItems updates the searchable channel list.
func (m *Model) SetItems(items []Item) {
	m.items = items
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

// SetBrowseable replaces the non-joined channel entries in the finder.
// Joined items (added via SetItems) are preserved; previous non-joined items
// are dropped and replaced with the new set. Items whose IDs already appear
// among the joined entries are skipped to avoid duplicates.
func (m *Model) SetBrowseable(browseable []Item) {
	// Drop existing non-joined items and build an ID set of joined items.
	joined := m.items[:0]
	have := make(map[string]struct{}, len(m.items))
	for _, it := range m.items {
		if it.Joined {
			joined = append(joined, it)
			have[it.ID] = struct{}{}
		}
	}
	m.items = joined
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
// Matching is tiered, best-first:
//  1. Prefix matches  (e.g. "eng" matches "engineering")
//  2. Substring matches (e.g. "tomo" matches "ext-automote")
//  3. Subsequence matches (e.g. "csp" matches "cs-product-triage" because
//     c, s, p appear in order). Subsequence matches are further sorted so
//     that tighter matches with more word-boundary hits rank higher.
func (m *Model) filter() {
	m.filtered = nil
	q := strings.ToLower(m.query)

	if q == "" {
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}

	type scored struct {
		idx   int
		score int // higher is better
	}

	var prefixMatches, substringMatches []int
	var subsequenceMatches []scored
	for i, item := range m.items {
		name := strings.ToLower(item.Name)
		switch {
		case strings.HasPrefix(name, q):
			prefixMatches = append(prefixMatches, i)
		case strings.Contains(name, q):
			substringMatches = append(substringMatches, i)
		default:
			if score, ok := subsequenceScore(name, q); ok {
				subsequenceMatches = append(subsequenceMatches, scored{i, score})
			}
		}
	}

	// Stable sort subsequence matches by descending score so the tightest /
	// most word-boundary-aligned matches come first.
	for i := 1; i < len(subsequenceMatches); i++ {
		for j := i; j > 0 && subsequenceMatches[j-1].score < subsequenceMatches[j].score; j-- {
			subsequenceMatches[j-1], subsequenceMatches[j] = subsequenceMatches[j], subsequenceMatches[j-1]
		}
	}

	m.filtered = append(m.filtered, prefixMatches...)
	m.filtered = append(m.filtered, substringMatches...)
	for _, s := range subsequenceMatches {
		m.filtered = append(m.filtered, s.idx)
	}
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
	overlayWidth := termWidth / 2
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
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

	// Results (max 10)
	maxVisible := 10
	total := len(m.filtered)
	if maxVisible > total {
		maxVisible = total
	}

	// Adjust scroll window for results
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
