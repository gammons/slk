// Package reactionsview provides a read-only modal overlay that lists the
// reactions on a message, grouped by emoji, with the display names of the
// users who reacted with it. Data is supplied by the App (assembled from the
// cached per-user reaction data); the modal does not fetch anything itself.
package reactionsview

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/chrome"
	slkemoji "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// ReactionGroup is one emoji and the resolved display names of the users who
// reacted with it. The current user's name is expected to already carry a
// "(you)" suffix when assembled by the caller.
//
// Count is the authoritative reaction count reported by Slack. It can exceed
// len(Users) because Slack truncates the per-reaction users list in
// conversations.history responses (we are cache-only by design). When they
// differ the header renders "known/total" so the modal never silently
// under-reports.
type ReactionGroup struct {
	Emoji string
	Users []string
	Count int
}

// Model is the reactions-list overlay state.
type Model struct {
	groups  []ReactionGroup
	visible bool
	offset  int // scroll offset in rendered content lines
	maxOff  int // last computed maximum offset (set during render)
}

var _ chrome.Dialog = (*Model)(nil)

// New creates an empty, hidden modal.
func New() *Model { return &Model{} }

// Open shows the modal for the given reaction groups and resets scroll.
func (m *Model) Open(groups []ReactionGroup) {
	m.groups = groups
	m.offset = 0
	m.maxOff = 0
	m.visible = true
}

// Close hides the modal and clears state.
func (m *Model) Close() {
	m.visible = false
	m.groups = nil
	m.offset = 0
	m.maxOff = 0
}

// IsVisible reports whether the modal is showing.
func (m *Model) IsVisible() bool { return m.visible }

// Offset returns the current scroll offset (exported for tests).
func (m *Model) Offset() int { return m.offset }

// HandleKey processes a key for the modal. esc/q/L closes it; up/down and j/k
// scroll. Scroll is clamped to [0, maxOff], where maxOff is recomputed on each
// render; before the first render maxOff is 0 so scrolling is inert.
func (m *Model) HandleKey(keyStr string) {
	switch keyStr {
	case "esc", "escape", "q", "L":
		m.Close()
	case "up", "k":
		if m.offset > 0 {
			m.offset--
		}
	case "down", "j":
		if m.offset < m.maxOff {
			m.offset++
		}
	}
}

// emojiGlyph renders an emoji name as a Unicode glyph when it is a
// composition-safe single codepoint, falling back to the :shortcode: form
// (same primitive the reaction picker uses). Workspace custom emoji are not in
// the built-in CodeMap and fall back to :name: which is the desired behavior.
func emojiGlyph(name string) string {
	code := ":" + name + ":"
	if u, ok := slkemoji.CodeMap()[code]; ok {
		u = strings.TrimRight(u, " ")
		if slkemoji.ShouldRenderUnicode(u) {
			return u
		}
	}
	return code
}

// contentLines builds the full (unwindowed) list of rendered content lines:
// an emoji header per group followed by one indented line per user.
func (m *Model) contentLines(bg color.Color, innerWidth int) []string {
	headerStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Primary).Bold(true)
	userStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)

	var lines []string
	for _, g := range m.groups {
		header := emojiGlyph(g.Emoji) + "  (" + countLabel(len(g.Users), g.Count) + ")"
		lines = append(lines, headerStyle.Width(innerWidth).Render(fit(header, innerWidth)))
		for _, u := range g.Users {
			lines = append(lines, userStyle.Width(innerWidth).Render(fit("  "+u, innerWidth)))
		}
	}
	return lines
}

// countLabel renders the header count for a reaction group. It shows the plain
// number of listed reactors when that matches the authoritative total, and
// "known/total" when Slack reported a higher count than the users we have
// cached (truncated history responses), so the modal never silently
// under-reports. A zero/unset total falls back to the known count.
func countLabel(known, total int) string {
	if total > known {
		return strconv.Itoa(known) + "/" + strconv.Itoa(total)
	}
	return strconv.Itoa(known)
}

// fit truncates s with an ellipsis tail when it is wider than width, so a long
// display name cannot wrap and throw off the modal's line accounting. Matches
// the truncation discipline of the help/reactionpicker modals.
func fit(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "\u2026")
}

// ViewOverlay composites the modal onto background. Returns background
// unchanged when hidden.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth, termHeight)
	if box == "" {
		return background
	}
	result := overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
	lines := strings.Split(result, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBox(termWidth, termHeight int) string {
	if !m.visible {
		return ""
	}
	s := messages.ChromeStyles()

	overlayWidth := termWidth * 6 / 10
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	if overlayWidth > termWidth-2 {
		overlayWidth = termWidth - 2
	}
	innerWidth := overlayWidth - 4 // border + padding

	bg := styles.ThemeBackground()

	all := m.contentLines(bg, innerWidth)

	// Visible window: leave headroom for title, blank, footer (~6 lines).
	maxVisible := termHeight - 8
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > 24 {
		maxVisible = 24
	}

	m.maxOff = len(all) - maxVisible
	if m.maxOff < 0 {
		m.maxOff = 0
	}
	if m.offset > m.maxOff {
		m.offset = m.maxOff
	}

	end := m.offset + maxVisible
	if end > len(all) {
		end = len(all)
	}
	window := all[m.offset:end]

	return chrome.Frame{
		Title:  "Reactions",
		Body:   window,
		Footer: s.Footer.Render("\u2191/\u2193 scroll   esc close"),
	}.Render(s, overlayWidth)
}
