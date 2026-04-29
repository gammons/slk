// Package presencemenu provides the Ctrl+S overlay for setting
// presence (active/away) and DND snooze state on the active workspace.
package presencemenu

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// Action is the high-level operation the user picked.
type Action int

const (
	ActionSetActive Action = iota
	ActionSetAway
	ActionSnooze       // SnoozeMinutes is set
	ActionCustomSnooze // open the custom-snooze input
	ActionEndDND
)

// Result is returned when the user commits a selection.
type Result struct {
	Action        Action
	SnoozeMinutes int // populated when Action == ActionSnooze
}

// item is a single row in the menu.
type item struct {
	label   string
	action  Action
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
}

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
	m.items = buildItems(presence, m.dndActive)
	m.filter()
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

// buildItems composes the menu rows based on current state.
//
// Labels are plain ASCII (no leading emoji glyphs) so that lipgloss's
// width measurement and the terminal's actual cell width agree. Mixed
// emoji width — particularly the moon — caused the modal's right
// border to not draw correctly in some terminals.
func buildItems(presence string, dndActive bool) []item {
	rows := []item{
		{label: "Active", action: ActionSetActive, current: presence == "active" && !dndActive},
		{label: "Away", action: ActionSetAway, current: presence == "away" && !dndActive},
		{label: "Snooze for 20 minutes", action: ActionSnooze, minutes: 20},
		{label: "Snooze for 1 hour", action: ActionSnooze, minutes: 60},
		{label: "Snooze for 2 hours", action: ActionSnooze, minutes: 120},
		{label: "Snooze for 4 hours", action: ActionSnooze, minutes: 240},
		{label: "Snooze for 8 hours", action: ActionSnooze, minutes: 480},
		{label: "Snooze for 24 hours", action: ActionSnooze, minutes: 1440},
		{label: "Snooze until tomorrow morning", action: ActionSnooze, minutes: minutesUntilTomorrowMorning(time.Now())},
		{label: "Snooze custom...", action: ActionCustomSnooze},
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
	q := strings.ToLower(m.query)
	if q == "" {
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}
	var prefix, sub []int
	for i, it := range m.items {
		name := strings.ToLower(it.label)
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
	overlayWidth := termWidth / 2
	if overlayWidth < 36 {
		overlayWidth = 36
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	innerWidth := overlayWidth - 4
	bg := styles.Background

	titleText := "Status"
	if m.workspaceName != "" {
		titleText = "Status — " + m.workspaceName
	}
	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render(titleText)

	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("Type to filter…")
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

	var rows []string
	for i, idx := range m.filtered {
		it := m.items[idx]
		line := it.label
		if it.current {
			line = "✓ " + line
		}
		if lipgloss.Width(line) > innerWidth-1 {
			line = truncate.StringWithTail(line, uint(innerWidth-1), "…")
		}
		var row string
		if i == m.selected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("▌")
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.Primary).
				Bold(true).
				Width(innerWidth - 1).
				Render(line)
			row = indicator + label
		} else {
			fg := styles.TextPrimary
			if it.current {
				fg = styles.Accent
			}
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(fg).
				Width(innerWidth - 1).
				Render(line)
			row = " " + label
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching options"))
	}

	content := title + "\n" + input + "\n\n" + strings.Join(rows, "\n")
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}

// CustomSnoozeView returns a centered input box used by the
// ModePresenceCustomSnooze sub-mode in app.go. The App tracks the input
// state itself; this helper just renders the box. query is the digits
// typed so far.
func CustomSnoozeView(termWidth, termHeight int, background, query string) string {
	overlayWidth := termWidth / 2
	if overlayWidth < 36 {
		overlayWidth = 36
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	bg := styles.Background

	title := lipgloss.NewStyle().Bold(true).Background(bg).Foreground(styles.Primary).
		Render("Snooze for how many minutes?")

	cursor := query + "█"
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		PaddingLeft(1).
		Background(bg).
		Foreground(styles.TextPrimary).
		Render(cursor)

	hint := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Italic(true).
		Render("Enter to snooze · Esc to cancel")

	content := title + "\n\n" + input + "\n\n" + hint
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)

	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}
