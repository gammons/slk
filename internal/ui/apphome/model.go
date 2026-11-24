// Package apphome is the UI model for an app's Home tab, shown in the
// messages region of the app's DM. Slack renders the tab from a Block
// Kit view; the App renders the blocks with the shared message renderer
// and this model adds the tab header, the scroll window and the list of
// buttons the user can press.
package apphome

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// Model is the Home tab's presentation state: the view, its channel's
// name for the header, and the scroll offset. It holds no rendering
// cache: the App renders it through its own panel cache, keyed on
// Version, so a theme or name change misses that cache and redraws.
type Model struct {
	home        *core.AppHome
	channelName string

	// actionAt maps a rendered button's (block_id, action_id) to its
	// index in home.Actions. Identity, not position, so a hit presses the
	// button it was drawn from even if the renderer and the action list
	// ever disagree on an ordinal.
	actionAt map[actionKey]int
	// hits are the visible buttons' footprints in View coordinates,
	// recorded by the last View.
	hits []actionHit

	yOffset int
	// lastHeight and lastTotal are what the last View drew: the content
	// rows it had, and the block lines it scrolled. GoToBottom and the
	// scroll clamp use them; both are zero before the first View.
	lastHeight int
	lastTotal  int

	version int64
}

// actionKey identifies a rendered button the way a press echoes it.
type actionKey struct{ blockID, actionID string }

// actionHit is one visible button's footprint in View coordinates.
type actionHit struct {
	action           int
	rowStart, rowEnd int
	colStart, colEnd int
}

// New returns an empty model, which renders nothing.
func New() Model { return Model{} }

// Version increases whenever what View draws changes.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// SetHome installs a fetched Home tab. A nil home clears it: the
// conversation has no Home tab.
func (m *Model) SetHome(home *core.AppHome) {
	m.home = home
	m.actionAt = actionIndexMap(home)
	m.hits = nil
	m.yOffset = 0
	m.dirty()
}

// actionIndexMap maps each action's (block_id, action_id) to its index in
// home.Actions. A duplicate key keeps the first.
func actionIndexMap(home *core.AppHome) map[actionKey]int {
	if home == nil || len(home.Actions) == 0 {
		return nil
	}
	out := make(map[actionKey]int, len(home.Actions))
	for i, act := range home.Actions {
		k := actionKey{blockID: act.BlockID, actionID: act.ActionID}
		if _, dup := out[k]; !dup {
			out[k] = i
		}
	}
	return out
}

// HitAction returns the index in Home.Actions of the button drawn at
// (row, col) in the last View's coordinates, with the tab header rows
// included. ok is false when that cell is not on a button.
func (m *Model) HitAction(row, col int) (int, bool) {
	for _, h := range m.hits {
		if row >= h.rowStart && row < h.rowEnd && col >= h.colStart && col < h.colEnd {
			return h.action, true
		}
	}
	return 0, false
}

// Clear forgets the Home tab, for instance when the user moves to a
// conversation that has none.
func (m *Model) Clear() {
	if m.home == nil && m.channelName == "" {
		return
	}
	m.home = nil
	m.channelName = ""
	m.yOffset = 0
	m.dirty()
}

// Home returns the installed view, nil when there is none.
func (m *Model) Home() *core.AppHome { return m.home }

// SetChannelName names the conversation whose Home tab this is; it
// heads the panel. An unchanged name costs no redraw.
func (m *Model) SetChannelName(name string) {
	if m.channelName == name {
		return
	}
	m.channelName = name
	m.dirty()
}

// Invalidate redraws the tab after something View depends on changed
// outside the model (the theme-independent name and emoji maps the App
// renders mentions with).
func (m *Model) Invalidate() {
	if m.home == nil {
		return
	}
	m.dirty()
}

// ScrollUp moves the block content up n lines.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.setOffset(m.yOffset - n)
}

// ScrollDown moves the block content down n lines, clamped in View.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.setOffset(m.yOffset + n)
}

// GoToTop scrolls to the first block line.
func (m *Model) GoToTop() { m.setOffset(0) }

// GoToBottom scrolls to the last page of block lines.
func (m *Model) GoToBottom() { m.setOffset(m.maxOffset()) }

func (m *Model) maxOffset() int {
	max := m.lastTotal - m.contentHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (m *Model) contentHeight() int {
	if m.lastHeight < headerLines+1 {
		return 1
	}
	return m.lastHeight - headerLines
}

func (m *Model) setOffset(off int) {
	if max := m.maxOffset(); off > max {
		off = max
	}
	if off < 0 {
		off = 0
	}
	if off == m.yOffset {
		return
	}
	m.yOffset = off
	m.dirty()
}

// headerLines is the tab header: the title/tab row and the key hint.
const headerLines = 2

// View renders the Home tab to height lines of width columns. ctx is
// the Block Kit host context the App builds (names, emoji, wrapping);
// the model does not own it because it needs the pane's image renderer.
func (m *Model) View(height, width int, ctx blockkit.Context) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	if height < headerLines {
		height = headerLines
	}
	contentHeight := height - headerLines
	m.lastHeight = height

	var blockLines []string
	var res blockkit.RenderResult
	if m.home != nil {
		res = blockkit.Render(m.home.Blocks, ctx, width)
		blockLines = res.Lines
	}
	m.lastTotal = len(blockLines)

	max := len(blockLines) - contentHeight
	if max < 0 {
		max = 0
	}
	if m.yOffset > max {
		m.yOffset = max
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.hits = m.hits[:0]
	for _, ah := range res.ActionHits {
		action, ok := m.actionAt[actionKey{blockID: ah.BlockID, actionID: ah.ActionID}]
		if !ok {
			continue
		}
		rowStart := headerLines + ah.RowStart - m.yOffset
		if rowStart < headerLines || rowStart >= height {
			continue
		}
		m.hits = append(m.hits, actionHit{
			action:   action,
			rowStart: rowStart, rowEnd: rowStart + (ah.RowEnd - ah.RowStart),
			colStart: ah.ColStart, colEnd: ah.ColEnd,
		})
	}
	end := m.yOffset + contentHeight
	if end > len(blockLines) {
		end = len(blockLines)
	}

	out := make([]string, 0, height)
	out = append(out, m.header(width)...)
	for i := 0; i < contentHeight; i++ {
		if at := m.yOffset + i; at < end {
			out = append(out, blockLines[at])
			continue
		}
		out = append(out, blankLine(width))
	}
	return strings.Join(out, "\n")
}

// header is the fixed top rows: the app name with the active Home tab,
// then the key hint.
func (m *Model) header(width int) []string {
	tabs := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true).Render("Home") +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" · Messages")
	name := m.channelName
	if avail := width - lipgloss.Width(tabs) - 2; avail > 0 && name != "" {
		name = truncate.String(name, uint(avail))
	} else if avail <= 0 {
		name = ""
	}
	first := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true).Render(name)
	if name != "" {
		first += "  "
	}
	first += tabs
	hint := "t messages"
	if m.home != nil && len(m.home.Actions) > 0 {
		hint = "b buttons · " + hint
	}
	return []string{
		first,
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(hint),
	}
}

func blankLine(width int) string {
	return lipgloss.NewStyle().Width(width).Render("")
}
