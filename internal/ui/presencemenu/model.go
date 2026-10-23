// Package presencemenu provides the Ctrl+S overlay for setting
// presence (active/away) and DND snooze state on the active workspace.
package presencemenu

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
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
)

// Result is returned when the user commits a selection.
type Result struct {
	Action        core.PresenceAction
	SnoozeMinutes int // populated when Action == ActionSnooze
}

// item is a single row in the menu.
type item struct {
	label   string
	action  core.PresenceAction
	minutes int  // for ActionSnooze
	current bool // currently-active row (decorated, still selectable)
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
	m.items = buildItems(presence, m.dndActive, m.currentStatusText, m.currentStatusEmoji)
	m.filter()
}

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
func buildItems(presence string, dndActive bool, statusText, statusEmoji string) []item {
	rows := []item{
		{label: "Active", action: ActionSetActive, current: presence == "active" && !dndActive},
		{label: "Away", action: ActionSetAway, current: presence == "away" && !dndActive},
		{label: "Working from home", action: ActionSetWFH, current: statusText == "Working from home"},
		{label: "On vacation", action: ActionSetVacation, current: statusText == "On vacation"},
		{label: "Snooze for 20 minutes", action: ActionSnooze, minutes: 20},
		{label: "Snooze for 1 hour", action: ActionSnooze, minutes: 60},
		{label: "Snooze for 2 hours", action: ActionSnooze, minutes: 120},
		{label: "Snooze for 4 hours", action: ActionSnooze, minutes: 240},
		{label: "Snooze for 8 hours", action: ActionSnooze, minutes: 480},
		{label: "Snooze for 24 hours", action: ActionSnooze, minutes: 1440},
		{label: "Snooze until tomorrow morning", action: ActionSnooze, minutes: minutesUntilTomorrowMorning(time.Now())},
		{label: "Snooze custom...", action: ActionCustomSnooze},
	}
	if statusText != "" || statusEmoji != "" {
		rows = append(rows, item{label: "Clear status", action: ActionClearStatus})
	}
	if dndActive {
		rows = append(rows, item{label: "End snooze / DND", action: ActionEndDND})
	}
	return rows
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

// BoxSize returns the rendered modal box's outer dimensions. The presence
// menu renders every filtered row (no scroll window), so height grows with
// the row count. termHeight is accepted for interface symmetry.
func (m Model) BoxSize(termWidth, termHeight int) (int, int) {
	nRows := len(m.filtered)
	if nRows < 1 {
		nRows = 1
	}
	return boxWidth(termWidth), nRows + 7
}

// ClickRow moves the selection to the menu row at box-local localY and
// returns true when the click lands on a visible row.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	if row < 0 || row >= len(m.filtered) {
		return false
	}
	m.selected = row
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
		return &Result{Action: it.action, SnoozeMinutes: it.minutes}
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
			m.query = m.query[:len(m.query)-1]
			m.selected = 0
			m.filter()
		}
		return nil
	}
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
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

	list := chrome.List{Start: 0, End: len(m.filtered), Total: len(m.filtered)}
	width := list.ContentWidth(innerWidth)
	rows := make([]string, 0, len(m.filtered))
	for i, idx := range m.filtered {
		it := m.items[idx]
		line := it.label
		if it.current {
			line = "✓ " + line
		}
		if lipgloss.Width(line) > width {
			line = truncate.StringWithTail(line, uint(width), "…")
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

	return chrome.Frame{
		Title: titleText,
		Input: chrome.FilterInput(s, m.query, "Type to filter…"),
		Body:  list.Rows(s, m.selected, rows),
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
