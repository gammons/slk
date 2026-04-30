// Package channelpicker provides an inline autocomplete picker for
// #channel references in the compose box. It mirrors mentionpicker's
// API and behavior, with a few channel-specific differences:
//
//   - No "special" entries (mentionpicker has @here / @channel / @everyone;
//     there is no equivalent for channels).
//   - Items are typed (channel / private / dm / group_dm) so the picker
//     can render the same glyphs as the sidebar (#, ◆, ●, ●). A picker
//     that visually matches the sidebar's iconography reads as "the
//     same channels you see on the left".
//   - DMs and group DMs are accepted in the channel set but are
//     rendered with their presence/group glyphs and resolve, on send,
//     to <#CHANNELID> just like ordinary channels (Slack accepts the
//     same wire form for any conversation ID).
package channelpicker

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
)

const MaxVisible = 5

// Channel is a single autocomplete entry. Type mirrors the sidebar's
// ChannelItem.Type values: "channel", "private", "dm", "group_dm".
type Channel struct {
	ID   string
	Name string
	Type string
}

// ChannelResult is what Select() returns when the user accepts an entry.
// ChannelID is what insertChannel uses to build the <#ID> wire form;
// Name is what gets inserted into the compose buffer (so the user sees
// "#general" while typing).
type ChannelResult struct {
	ChannelID string
	Name      string
}

type Model struct {
	channels []Channel
	filtered []Channel
	query    string
	selected int
	visible  bool
}

func New() Model {
	return Model{}
}

func (m *Model) SetChannels(channels []Channel) {
	m.channels = channels
}

func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

func (m *Model) IsVisible() bool {
	return m.visible
}

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m *Model) Query() string {
	return m.query
}

func (m *Model) Filtered() []Channel {
	return m.filtered
}

func (m *Model) Selected() int {
	return m.selected
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

func (m *Model) Select() *ChannelResult {
	if len(m.filtered) == 0 {
		return nil
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return nil
	}
	c := m.filtered[m.selected]
	return &ChannelResult{
		ChannelID: c.ID,
		Name:      c.Name,
	}
}

func (m *Model) filter() {
	q := strings.ToLower(m.query)
	var results []Channel
	for _, c := range m.channels {
		if q == "" || strings.HasPrefix(strings.ToLower(c.Name), q) {
			results = append(results, c)
		}
	}
	if len(results) > MaxVisible {
		results = results[:MaxVisible]
	}
	m.filtered = results
}

// glyphFor returns the leading glyph for a channel of the given type.
// Mirrors the sidebar's prefixes so the picker reads as the same set of
// channels the user sees on the left rail.
func glyphFor(ch Channel) string {
	switch ch.Type {
	case "private":
		return "◆"
	case "dm", "group_dm":
		return "●"
	default:
		return "#"
	}
}

func (m *Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	var rows []string
	for i, c := range m.filtered {
		indicator := "  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		if i == m.selected {
			indicator = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌ ")
			nameStyle = nameStyle.Bold(true)
		}
		label := fmt.Sprintf("%s %s", glyphFor(c), c.Name)
		rows = append(rows, indicator+nameStyle.Render(label))
	}

	content := strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Background(styles.SurfaceDark).
		Width(width - 2).
		Render(content)

	return box
}
