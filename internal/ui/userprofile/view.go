package userprofile

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	slkemoji "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
)

// maxBoxWidth is the box's own cap, independent of the terminal: "min(56,
// termWidth-4)" per the global constraints.
const maxBoxWidth = 56

// detailLabelWidth is the column the "Local time" / "Email" / "Phone"
// values start at, wide enough for the longest label ("Local time",
// 10 cells) plus a 2-cell gap.
const detailLabelWidth = 12

// CopyIcon marks the email as copyable (click it, or press e).
const CopyIcon = "\U0001F4CB" // 📋

// copyFooter is the footer while there is an email to copy.
const copyFooter = "e copy email \u00b7 K / esc / q close"

// boxLayout is one rendered box and where its copy icon landed, in
// box-local cells (border included); iconRow is -1 when no icon was
// drawn (no email, still loading, or the details were dropped).
type boxLayout struct {
	box              string
	iconRow, iconCol int
}

// BoxSize returns the outer dimensions of the box the last frame drew,
// (0, 0) when hidden. It satisfies the modal-click router's geometry
// interface.
func (m *Model) BoxSize(termW, termH int) (int, int) {
	if !m.visible {
		return 0, 0
	}
	l := m.layout(termW, termH, m.lastLive)
	if l.box == "" {
		return 0, 0
	}
	return lipgloss.Width(l.box), lipgloss.Height(l.box)
}

// ClickAt reports whether the box-local cell (x, y) is on the email's
// copy icon as the last frame drew it.
func (m *Model) ClickAt(termW, termH, x, y int) bool {
	if !m.visible {
		return false
	}
	l := m.layout(termW, termH, m.lastLive)
	if l.iconRow < 0 || y != l.iconRow {
		return false
	}
	return x >= l.iconCol && x < l.iconCol+drawnWidth(CopyIcon)
}

// drawnWidth is the width the terminal draws s at. The copy icon is
// written as literal text, never swapped for a kitty image placement, so
// emoji.Width's image-mode footprint (emoji_cells may be 1) would not
// match what is on screen.
func drawnWidth(s string) int { return lipgloss.Width(s) }

// ViewOverlay composites the modal onto background. Returns background
// unchanged when hidden.
func (m *Model) ViewOverlay(termW, termH int, background string, live Live) string {
	if !m.visible {
		return background
	}
	m.lastLive = live
	box := m.layout(termW, termH, live).box
	if box == "" {
		return background
	}
	result := overlay.DimmedOverlay(termW, termH, background, box, 0.5)
	lines := strings.Split(result, "\n")
	if len(lines) > termH {
		lines = lines[:termH]
	}
	return strings.Join(lines, "\n")
}

// headerName resolves the name shown in the title row: display name,
// then real name, then handle once loaded; before the fetch returns,
// the seed's display name, falling back to the user ID.
func (m *Model) headerName() string {
	if m.state != stateLoading {
		switch {
		case m.profile.DisplayName != "":
			return m.profile.DisplayName
		case m.profile.RealName != "":
			return m.profile.RealName
		case m.profile.Handle != "":
			return m.profile.Handle
		}
	}
	if m.seed.DisplayName != "" {
		return m.seed.DisplayName
	}
	return m.seed.UserID
}

// layout builds the full modal box, given the render-time Live inputs,
// dropping rows bottom-up (details, then status, then title) when termH
// is too short to hold everything, and records where the email's copy
// icon landed. Name and handle rows are always kept.
func (m *Model) layout(termW, termH int, live Live) boxLayout {
	boxW := maxBoxWidth
	if termW-4 < boxW {
		boxW = termW - 4
	}
	if boxW < 1 {
		boxW = 1
	}
	innerW := boxW - 4 // border (2) + padding (2)
	if innerW < 1 {
		innerW = 1
	}

	bg := styles.Background

	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("Profile")

	nameRows := m.nameRows(innerW, live)
	titleRow := m.titleRow(innerW)
	statusRows := m.statusRows(innerW, live)
	detailRows, emailIdx, emailIconCol := m.detailRows(innerW, live)

	footerText := "K / esc / q close"
	if emailIdx >= 0 {
		footerText = copyFooter
	}
	footer := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render(padLeftTo(fit(footerText, innerW), innerW))

	// Height budget: title + blank + footer + blank, plus whatever
	// content rows fit. Drop bottom-up: details, then status, then
	// title. Name/handle rows are never dropped.
	type section struct {
		rows    []string
		details bool
	}
	sections := []section{}
	if titleRow != "" {
		sections = append(sections, section{rows: []string{titleRow}})
	}
	if len(statusRows) > 0 {
		sections = append(sections, section{rows: statusRows})
	}
	if len(detailRows) > 0 {
		sections = append(sections, section{rows: detailRows, details: true})
	}

	fixedRows := 4 // title line, blank, blank-before-footer, footer
	budget := termH - 2 - fixedRows - len(nameRows)
	if budget < 0 {
		budget = 0
	}

	// Drop whole sections from the tail (details, then status, then
	// title) until what remains fits budget. Each kept section costs
	// its own rows plus one blank separator before it.
	cost := func() int {
		total := 0
		for _, s := range sections {
			total += 1 + len(s.rows) // blank separator + its rows
		}
		return total
	}
	for cost() > budget && len(sections) > 0 {
		sections = sections[:len(sections)-1]
	}

	body := append([]string{}, nameRows...)
	iconBodyIdx := -1
	for _, sec := range sections {
		// Sections are never empty, so the separator is the only
		// blank row that can precede them; collapse it when the body
		// already ends blank (e.g. an empty line 2).
		if len(body) == 0 || strings.TrimSpace(body[len(body)-1]) != "" {
			body = append(body, "")
		}
		if sec.details && emailIdx >= 0 {
			iconBodyIdx = len(body) + emailIdx
		}
		body = append(body, sec.rows...)
	}

	content := title + "\n\n" + strings.Join(body, "\n") + "\n\n" + footer
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(0, 1).
		Width(boxW).
		Render(content)

	l := boxLayout{box: box, iconRow: -1}
	if iconBodyIdx >= 0 {
		// Box rows: top border, title, blank, then the body.
		l.iconRow = 3 + iconBodyIdx
		// Box columns: left border, left padding, then the row.
		l.iconCol = 2 + emailIconCol
	}
	return l
}

// nameRows builds the header (name + presence) and line-2 (@handle,
// pronouns, badges) rows. Always exactly 2 rows so avatar placement
// stays predictable, mirroring the spec's two-line header.
func (m *Model) nameRows(innerW int, live Live) []string {
	bg := styles.Background
	name := m.headerName()

	nameStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary).Bold(true)

	var presence string
	if live.Presence == "active" || live.Presence == "away" {
		glyph := "\u25cf"
		color := styles.PresenceAway
		if live.Presence == "active" {
			color = styles.PresenceOnline
		}
		presence = lipgloss.NewStyle().Background(bg).Render(" ") +
			color.Background(bg).Render(glyph+" "+live.Presence)
	}

	nameBudget := innerW - lipgloss.Width(presence)
	if nameBudget < 1 {
		nameBudget = 1
	}
	nameFitted := fit(name, nameBudget)
	row1 := nameStyle.Render(nameFitted) + presence
	row1 = padLine(row1, lipgloss.Width(nameFitted)+lipgloss.Width(presence), innerW, bg)

	line2 := m.line2Text()
	line2Styled := ""
	if line2 != "" {
		line2Styled = fit(line2, innerW)
	}
	row2 := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render(padLeftTo(line2Styled, innerW))

	rows := []string{row1, row2}
	if live.Avatar != "" {
		rows = withAvatar(rows, live.Avatar, bg)
	}
	return rows
}

// line2Text builds the "@handle · pronouns  APP  external  deactivated"
// line. Before the fetch returns, only the seed's external badge (if
// set) is shown, since handle/pronouns/APP/deactivated all come from
// the fetch.
func (m *Model) line2Text() string {
	if m.state == stateLoading {
		if m.seed.IsExternal {
			return "external"
		}
		return ""
	}

	var parts []string
	var head string
	if m.profile.Handle != "" {
		head = "@" + m.profile.Handle
	}
	if m.profile.Pronouns != "" {
		if head != "" {
			head += " \u00b7 " + m.profile.Pronouns
		} else {
			head = m.profile.Pronouns
		}
	}
	if head != "" {
		parts = append(parts, head)
	}
	if m.profile.IsBot {
		parts = append(parts, "APP")
	}
	if m.seed.IsExternal {
		parts = append(parts, "external")
	}
	if m.profile.Deleted {
		parts = append(parts, "deactivated")
	}
	return strings.Join(parts, "  ")
}

// titleRow renders the job title, "" when empty.
func (m *Model) titleRow(innerW int) string {
	if m.state == stateLoading || m.profile.Title == "" {
		return ""
	}
	bg := styles.Background
	return lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary).
		Render(padLeftTo(fit(m.profile.Title, innerW), innerW))
}

// statusRows renders the custom-status/DND/huddle block, one line per
// active facet (Slack's own ordering: huddle, then status, then DND),
// "" rows omitted and the whole block omitted when nothing applies.
func (m *Model) statusRows(innerW int, live Live) []string {
	st := live.Status
	now := live.Now
	bg := styles.Background
	style := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)

	var rows []string
	if st.InHuddle(now) {
		rows = append(rows, style.Render(padLeftTo(fit(peerstatus.HuddleGlyph+" In a huddle", innerW), innerW)))
	}
	if st.HasStatus(now) {
		line := strings.TrimSpace(st.StatusGlyph(now) + " " + st.Text)
		rows = append(rows, style.Render(padLeftTo(fit(line, innerW), innerW)))
	}
	if st.InDND(now) {
		line := peerstatus.DNDGlyph + " Do not disturb"
		if !st.DNDEnd.IsZero() {
			line += " until " + st.DNDEnd.In(now.Location()).Format("3:04 PM")
		}
		rows = append(rows, style.Render(padLeftTo(fit(line, innerW), innerW)))
	}
	return rows
}

// detailRows renders local time, email and phone, one row per non-empty
// field, labels padded to a common column. When an email is shown,
// emailIdx is its index in rows and iconCol the inner-row column the
// copy icon starts at; emailIdx is -1 otherwise. The email is truncated
// first, so the icon always fits.
func (m *Model) detailRows(innerW int, live Live) (rows []string, emailIdx, iconCol int) {
	bg := styles.Background
	valueStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
	mutedStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted)

	emailIdx = -1
	if m.state == stateLoading {
		return []string{mutedStyle.Render(padLeftTo(fit("Loading profile\u2026", innerW), innerW))}, -1, 0
	}
	if m.state == stateFailed {
		return []string{mutedStyle.Render(padLeftTo(fit(errorLine(m.err), innerW), innerW))}, -1, 0
	}

	addRow := func(label, value string) {
		if value == "" {
			return
		}
		line := padLabel(label, detailLabelWidth) + value
		rows = append(rows, valueStyle.Render(padLeftTo(fit(line, innerW), innerW)))
	}
	if m.profile.TZ != "" {
		addRow("Local time", formatLocalTime(live.Now, m.profile.TZOffset, m.profile.TZAbbrev))
	}
	if email := m.profile.Email; email != "" {
		iconW := drawnWidth(CopyIcon)
		head := fit(padLabel("Email", detailLabelWidth)+email, innerW-1-iconW)
		if head != "" {
			headW := drawnWidth(head)
			emailIdx = len(rows)
			iconCol = headW + 1
			pad := strings.Repeat(" ", max(0, innerW-headW-1-iconW))
			rows = append(rows, valueStyle.Render(head+" "+CopyIcon+pad))
		} else {
			// Too narrow for the icon: plain row, nothing to click.
			addRow("Email", email)
		}
	}
	addRow("Phone", m.profile.Phone)
	return rows, emailIdx, iconCol
}

// padLabel right-pads label with spaces to width columns.
func padLabel(label string, width int) string {
	w := slkemoji.Width(label)
	if w >= width {
		return label
	}
	return label + strings.Repeat(" ", width-w)
}

// fit truncates s with an ellipsis tail when it is wider than width.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if slkemoji.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "\u2026")
}

// padLeftTo right-pads s with spaces to exactly width display columns,
// assuming s is already <= width.
func padLeftTo(s string, width int) string {
	w := slkemoji.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// padLine pads a styled row (whose visible width is curWidth) out to
// width columns, so the padding shares the row's background.
func padLine(styled string, curWidth, width int, bg interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) string {
	if curWidth >= width {
		return styled
	}
	return styled + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", width-curWidth))
}

// withAvatar prepends the rendered avatar (4 cols + a 2-col gap) beside
// the first len(rows) lines. Avatar is expected to be exactly
// len(rows) lines tall (2, matching the header's own 2 rows); shorter
// is padded, taller is truncated to len(rows).
func withAvatar(rows []string, avatar string, bg interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) []string {
	avatarLines := strings.Split(avatar, "\n")
	gap := lipgloss.NewStyle().Background(bg).Render("  ")
	out := make([]string, len(rows))
	for i, row := range rows {
		var left string
		if i < len(avatarLines) {
			left = avatarLines[i] + gap
		} else {
			left = lipgloss.NewStyle().Background(bg).Width(6).Render("")
		}
		out[i] = left + row
	}
	return out
}
