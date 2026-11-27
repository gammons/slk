// Package presencemenu provides the Ctrl+S overlay for setting
// presence (active/away) and DND snooze state on the active workspace.
package presencemenu

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/ansi"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	slkemoji "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

const (
	ActionSetActive    = core.PresenceSetActive
	ActionSetAway      = core.PresenceSetAway
	ActionSnooze       = core.PresenceSnooze
	ActionCustomSnooze = core.PresenceCustomSnooze
	ActionEndDND       = core.PresenceEndDND
	ActionSetWFH       = core.PresenceSetWFH
	ActionSetVacation  = core.PresenceSetVacation
	ActionClearStatus  = core.PresenceClearStatus
	ActionSetCustom    = core.PresenceSetCustom
	ActionSetRecent    = core.PresenceSetRecent
)

// maxVisibleRows is how many rows the menu shows before it scrolls:
// with the separator and its chrome it is 22 lines, inside 80x24.
const maxVisibleRows = 14

// MaxRecentRows caps the recent statuses shown, as Slack's own menu does.
const MaxRecentRows = 5

// Result is returned when the user commits a selection.
type Result struct {
	Action        core.PresenceAction
	SnoozeMinutes int               // populated when Action == ActionSnooze
	Recent        core.RecentStatus // populated when Action == ActionSetRecent
}

// item is a single row in the menu.
type item struct {
	label   string
	action  core.PresenceAction
	minutes int                // for ActionSnooze
	recent  *core.RecentStatus // for ActionSetRecent
	current bool               // currently-active row (decorated, still selectable)
}

// Model is the picker overlay.
type Model struct {
	items         []item
	filtered      []int // indices into items matching query
	query         string
	selected      int // index into filtered
	visible       bool
	workspaceName string
	currentPres   string // "active" / "away" / ""
	dndActive     bool

	// currentStatusText/currentStatusEmoji are the profile status
	// currently applied (e.g. "Working from home" / "🏠"), set via
	// SetCurrentCustomStatus before OpenWith so the matching preset row
	// renders with a checkmark and the Clear-status row appears. Slack
	// allows an emoji-only status with no text, so Clear-status
	// visibility must check both, not text alone.
	currentStatusText  string
	currentStatusEmoji string
	// currentStatusName is that status's emoji name without colons
	// (SetCurrentStatusEmojiName): recent rows match on it, since
	// distinct custom emoji can share a fallback glyph.
	currentStatusName string

	// recent is the user's recent statuses from Slack, newest first,
	// set via SetRecent; deduped and capped when the rows are built.
	recent []core.RecentStatus

	// emojiCtx draws a recent status's emoji as an image when image
	// mode is on (SetEmojiContext, SetEmojiCustoms).
	emojiCtx messages.EmojiContext
}

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// New creates a new presence menu.
func New() Model {
	return Model{}
}

// OpenWith shows the overlay populated for the current workspace state.
// presence is "active", "away", or "" (unknown). dndEnabled with a future
// dndEnd is treated as DND active; an expired or zero dndEnd cancels DND.
func (m *Model) OpenWith(workspaceName, presence string, dndEnabled bool, dndEnd time.Time) {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.workspaceName = workspaceName
	m.currentPres = presence
	m.dndActive = dndEnabled && (dndEnd.IsZero() || time.Now().Before(dndEnd))
	m.items = buildItems(presence, m.dndActive, m.currentStatusText, m.currentStatusEmoji, m.currentStatusName, m.recent)
	m.filter()
}

// SetEmojiContext configures drawing recent-status emoji as images.
// The menu caches no rows, so the next View uses it.
func (m *Model) SetEmojiContext(ctx messages.EmojiContext) { m.emojiCtx = ctx }

// SetEmojiCustoms replaces only the workspace custom emoji map.
func (m *Model) SetEmojiCustoms(customs map[string]string) { m.emojiCtx.Customs = customs }

// SetRecent sets the recent statuses shown after the presets. Call it
// before OpenWith, or while the menu is open when a fetch lands: the
// selection stays on the row it was on.
func (m *Model) SetRecent(recent []core.RecentStatus) {
	m.recent = recent
	if !m.visible {
		return
	}
	var keep *item
	if m.selected < len(m.filtered) {
		it := m.items[m.filtered[m.selected]]
		keep = &it
	}
	m.items = buildItems(m.currentPres, m.dndActive, m.currentStatusText, m.currentStatusEmoji, m.currentStatusName, m.recent)
	m.filter()
	m.selected = min(m.selected, max(len(m.filtered)-1, 0))
	if keep == nil {
		return
	}
	for i, idx := range m.filtered {
		if sameRow(m.items[idx], *keep) {
			m.selected = i
			return
		}
	}
}

// sameRow reports whether a and b are the same menu row across a rebuild.
func sameRow(a, b item) bool {
	// Not minutes: "Snooze until tomorrow morning" recomputes them.
	return a.label == b.label && a.action == b.action
}

// SetCurrentStatusEmojiName records the current status's emoji name
// (no colons, "" for none). Call before OpenWith.
func (m *Model) SetCurrentStatusEmojiName(name string) { m.currentStatusName = name }

// SetCurrentCustomStatus records the profile status currently applied
// (from presenceController's cache), so the next OpenWith call marks
// the matching preset row as current and shows the Clear-status row.
// Call before OpenWith.
func (m *Model) SetCurrentCustomStatus(text, emoji string) {
	m.currentStatusText = text
	m.currentStatusEmoji = emoji
}

// hasEndDNDItem reports whether the End-DND row is present. Exposed for tests.
func (m Model) hasEndDNDItem() bool {
	for _, it := range m.items {
		if it.action == ActionEndDND {
			return true
		}
	}
	return false
}

// hasClearStatusItem reports whether the Clear-status row is present.
// Exposed for tests.
func (m Model) hasClearStatusItem() bool {
	for _, it := range m.items {
		if it.action == ActionClearStatus {
			return true
		}
	}
	return false
}

// currentItemLabels returns the labels of every item flagged current.
// Presence (Active/Away) and a custom status preset are independent in
// real Slack, so both can be current at once — this is not a
// single-selection concept. Exposed for tests.
func (m Model) currentItemLabels() []string {
	var labels []string
	for _, it := range m.items {
		if it.current {
			labels = append(labels, it.label)
		}
	}
	return labels
}

// buildItems composes the menu rows based on current state.
//
// Labels are plain ASCII (no leading emoji glyphs) so that lipgloss's
// width measurement and the terminal's actual cell width agree. Mixed
// emoji width — particularly the moon — caused the modal's right
// border to not draw correctly in some terminals.
func buildItems(presence string, dndActive bool, statusText, statusEmoji, statusName string, recent []core.RecentStatus) []item {
	rows := []item{
		{label: "Active", action: ActionSetActive, current: presence == "active" && !dndActive},
		{label: "Away", action: ActionSetAway, current: presence == "away" && !dndActive},
		{label: "Working from home", action: ActionSetWFH, current: statusText == "Working from home"},
		{label: "On vacation", action: ActionSetVacation, current: statusText == "On vacation"},
		{label: "Set a status...", action: ActionSetCustom},
	}
	rows = append(rows, recentItems(recent, statusText, statusName)...)
	rows = append(rows, []item{
		{label: "Snooze for 20 minutes", action: ActionSnooze, minutes: 20},
		{label: "Snooze for 1 hour", action: ActionSnooze, minutes: 60},
		{label: "Snooze for 2 hours", action: ActionSnooze, minutes: 120},
		{label: "Snooze for 4 hours", action: ActionSnooze, minutes: 240},
		{label: "Snooze for 8 hours", action: ActionSnooze, minutes: 480},
		{label: "Snooze for 24 hours", action: ActionSnooze, minutes: 1440},
		{label: "Snooze until tomorrow morning", action: ActionSnooze, minutes: minutesUntilTomorrowMorning(time.Now())},
		{label: "Snooze custom...", action: ActionCustomSnooze},
	}...)
	if statusText != "" || statusEmoji != "" {
		rows = append(rows, item{label: "Clear status", action: ActionClearStatus})
	}
	if dndActive {
		rows = append(rows, item{label: "End snooze / DND", action: ActionEndDND})
	}
	return rows
}

// recentItems turns the recent statuses into rows: newest first, at
// most MaxRecentRows, without entries that repeat a preset or a newer
// entry. A row is current when its text and emoji name match the
// status now set (statusName: emoji name without colons).
func recentItems(recent []core.RecentStatus, statusText, statusName string) []item {
	seen := map[[2]string]bool{
		{"Working from home", ":house:"}: true,
		{"On vacation", ":palm_tree:"}:   true,
	}
	var rows []item
	for _, r := range recent {
		key := [2]string{r.Text, r.Emoji}
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, item{
			label:   strings.TrimSpace(r.Emoji + " " + r.Text),
			action:  ActionSetRecent,
			recent:  &r,
			current: r.Text == statusText && strings.Trim(r.Emoji, ":") == statusName,
		})
		if len(rows) == MaxRecentRows {
			break
		}
	}
	return rows
}

// recentLabel is how a recent row draws: the emoji as an image (returned
// separately, to go after the text) when image mode can place it, else
// its Unicode glyph before the text, else its :shortcode:.
func (m Model) recentLabel(r core.RecentStatus) (line, image string) {
	name := strings.Trim(r.Emoji, ":")
	if name == "" {
		return r.Text, ""
	}
	if placed, flush, ok := m.emojiCtx.PlaceShortcode(name); ok {
		if flush != nil {
			_ = flush(imgpkg.KittyOutput)
		}
		return r.Text, placed
	}
	glyph := slkemoji.CodeMap()[":"+slkemoji.StripSkinTone(name)+":"]
	if !slkemoji.ShouldRenderUnicode(glyph) {
		glyph = ":" + name + ":"
	}
	return strings.TrimSpace(glyph + " " + r.Text), ""
}

// window is the range of m.filtered on screen.
func (m Model) window() (start, end int) {
	return chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
}

// recentSeparator is the index into m.filtered of the first recent row
// when a line separates the recent rows from the presets above them
// inside the window [start, end); -1 otherwise. Only the unfiltered
// list has the line.
func (m Model) recentSeparator(start, end int) int {
	if m.query != "" {
		return -1
	}
	for i, idx := range m.filtered {
		if m.items[idx].action == ActionSetRecent {
			if i > start && i < end {
				return i
			}
			return -1
		}
	}
	return -1
}

// minutesUntilTomorrowMorning returns the number of minutes from now until
// 09:00 local time on the next weekday (Mon–Thu → tomorrow; Fri/Sat/Sun → Monday).
// Always >= 1.
func minutesUntilTomorrowMorning(now time.Time) int {
	loc := now.Location()
	target := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc).AddDate(0, 0, 1)
	for target.Weekday() == time.Saturday || target.Weekday() == time.Sunday {
		target = target.AddDate(0, 0, 1)
	}
	d := target.Sub(now)
	mins := int(d.Minutes())
	if mins < 1 {
		mins = 1
	}
	return mins
}

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
}

// IsVisible returns whether the overlay is currently showing.
func (m Model) IsVisible() bool { return m.visible }

// listTopOffset is the box-local row of the first list row.
func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), true) }

// boxWidth returns the modal's outer width for a given terminal width.
func boxWidth(termWidth int) int {
	w := termWidth / 2
	if w < 36 {
		w = 36
	}
	if w > 60 {
		w = 60
	}
	return w
}

// BoxSize returns the rendered modal box's outer dimensions: the rows in
// the scroll window, plus the separator line when it is on screen.
// termHeight is accepted for interface symmetry.
func (m Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := m.window()
	nRows := end - start
	if nRows < 1 {
		nRows = 1
	}
	if m.recentSeparator(start, end) >= 0 {
		nRows++
	}
	return boxWidth(termWidth), nRows + 7
}

// ClickRow moves the selection to the menu row at box-local localY and
// returns true when the click lands on a visible row.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	start, end := m.window()
	if sep := m.recentSeparator(start, end); sep >= 0 {
		switch {
		case row == sep-start:
			return false
		case row > sep-start:
			row--
		}
	}
	if row < 0 || row >= end-start {
		return false
	}
	m.selected = start + row
	return true
}

// HandleKey processes a key event and returns a non-nil Result on selection.
func (m *Model) HandleKey(keyStr string) *Result {
	switch keyStr {
	case "enter":
		if len(m.filtered) == 0 {
			return nil
		}
		it := m.items[m.filtered[m.selected]]
		res := &Result{Action: it.action, SnoozeMinutes: it.minutes}
		if it.recent != nil {
			res.Recent = *it.recent
		}
		return res
	case "esc":
		m.Close()
		return nil
	case "down", "ctrl+n", "j":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil
	case "up", "ctrl+p", "k":
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
	if r := []rune(keyStr); len(r) == 1 && unicode.IsPrint(r[0]) {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}
	return nil
}

// filter rebuilds the filtered list based on the current query.
func (m *Model) filter() {
	m.filtered = nil
	q := text.Fold(m.query)
	if q == "" {
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}
	var prefix, sub []int
	for i, it := range m.items {
		name := text.Fold(it.label)
		switch {
		case strings.HasPrefix(name, q):
			prefix = append(prefix, i)
		case strings.Contains(name, q):
			sub = append(sub, i)
		}
	}
	m.filtered = append(prefix, sub...)
}

// ViewOverlay renders the dimmed centered modal.
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
	s := messages.ChromeStyles()
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4

	titleText := "Status"
	if m.workspaceName != "" {
		titleText = "Status — " + m.workspaceName
	}

	start, end := m.window()
	list := chrome.List{Start: start, End: end, Total: len(m.filtered)}
	width := list.ContentWidth(innerWidth)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		it := m.items[m.filtered[i]]
		line, image := it.label, ""
		if it.recent != nil {
			line, image = m.recentLabel(*it.recent)
		}
		if it.current {
			line = "✓ " + line
		}
		budget := width
		if image != "" {
			budget = max(width-1-m.emojiCtx.ImageCells(), 0)
		}
		// Cut between grapheme clusters: a recent status's text can hold
		// ZWJ emoji, which a per-rune cut breaks into stray glyphs.
		line = ansi.Truncate(line, budget)
		if image != "" {
			// After truncation, so it is never cut: an image is charged its cells.
			line += " " + image
		}
		style := s.Row
		switch {
		case i == m.selected:
			style = s.RowSelected
		case it.current:
			style = s.Row.Foreground(styles.Accent)
		}
		rows = append(rows, style.Width(width).Render(line))
	}

	body := list.Rows(s, m.selected, rows)
	if sep := m.recentSeparator(start, end); sep >= 0 {
		line := " " + lipgloss.NewStyle().Background(styles.ThemeBackground()).Foreground(styles.Border).
			Render(strings.Repeat("─", width)) + list.Gutter(s, sep)
		body = slices.Insert(body, sep-start, line)
	}
	return chrome.Frame{
		Title: titleText,
		Input: chrome.FilterInput(s, m.query, "Type to filter…"),
		Body:  body,
		Empty: "No matching options",
	}.Render(s, overlayWidth)
}

// CustomSnoozeView returns a centered input box used by the
// ModePresenceCustomSnooze sub-mode in app.go. The App tracks the input
// state itself; this helper just renders the box. query is the digits
// typed so far.
func CustomSnoozeView(termWidth, termHeight int, background, query string) string {
	s := messages.ChromeStyles()
	box := chrome.Frame{
		Title:  "Snooze for how many minutes?",
		Body:   []string{s.Input.Render(query + "█")},
		Footer: s.Empty.Render("Enter to snooze · Esc to cancel"),
	}.Render(s, boxWidth(termWidth))
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}
