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
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/text"
)

var _ chrome.Dialog = (*Model)(nil)

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

// Styles is everything the picker draws with. The composer maps the theme
// onto it and pushes it at construction and on every theme change.
type Styles struct {
	// Chrome is the shared popup styling (box, rows, marker).
	Chrome chrome.Styles
	// Name styles a row's glyph and name; the selected row is also bold.
	Name lipgloss.Style
}

// Option configures a Model at construction.
type Option func(*Model)

// WithStyles sets the initial styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithWidth sets the popup width.
func WithWidth(w int) Option { return func(m *Model) { m.width = w } }

type Model struct {
	channels []Channel
	filtered []Channel
	query    string
	selected int
	visible  bool
	styles   Styles
	width    int
}

func New(opts ...Option) Model {
	m := Model{}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// SetStyles replaces the picker's styles.
func (m *Model) SetStyles(s Styles) { m.styles = s }

// SetWidth sets the popup width the composer allots.
func (m *Model) SetWidth(w int) { m.width = w }

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
	q := text.Fold(m.query)
	var results []Channel
	for _, c := range m.channels {
		if q == "" || strings.HasPrefix(text.Fold(c.Name), q) {
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

// View renders the popup at the width set by SetWidth/WithWidth, or ""
// when the picker is hidden or has no matches.
func (m *Model) View() string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	s := m.styles.Chrome
	rows := make([]string, 0, len(m.filtered))
	for i, c := range m.filtered {
		nameStyle := m.styles.Name
		if i == m.selected {
			nameStyle = nameStyle.Bold(true)
		}
		label := fmt.Sprintf("%s %s", glyphFor(c), c.Name)
		rows = append(rows, chrome.PopupMarker(s, i == m.selected)+nameStyle.Render(label))
	}
	return chrome.Popup(s, rows, m.width)
}
