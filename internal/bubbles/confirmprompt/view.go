package confirmprompt

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/buttonrow"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// Box width as a share of the terminal, and the bounds it is clamped to.
const (
	widthPercent = 35
	minWidth     = 40
	maxWidth     = 60
	// frameWidth is the border and horizontal padding the content sits inside.
	frameWidth = 4
	// buttonGap is the number of spaces between buttons.
	buttonGap = 3
	// maxBodyLines caps the wrapped body.
	maxBodyLines = 8
)

// layout is the prompt's content measured at a content width: the title
// and body lines, and where each button sits in the button rows.
type layout struct {
	title, body []string
	buttons     []buttonrow.Placed
}

// buttonRow is the content row of the first button row: after the title and
// its blank line, and the body and its blank line when there is a body.
func (l layout) buttonRow() int {
	row := len(l.title) + 1
	if len(l.body) > 0 {
		row += len(l.body) + 1
	}
	return row
}

func (m Model) layoutAt(width int) layout {
	labels := make([]string, len(m.choice.Buttons))
	for i := range labels {
		labels[i] = m.label(i)
	}
	l := layout{
		title:   wrapLines(m.choice.Title, width, 0),
		buttons: buttonrow.Layout(labels, width, buttonGap),
	}
	if m.choice.Body != "" {
		if limit := m.bodyLimit(l); limit > 0 {
			l.body = wrapLines(m.choice.Body, width, limit)
		}
	}
	return l
}

// bodyLimit is how many body lines fit beside l's title and buttons: the
// box frame, the title, the button rows and the two blank separators come
// off the terminal height first. With the height unknown only maxBodyLines
// applies; a result <= 0 means no room for any body.
func (m Model) bodyLimit(l layout) int {
	if m.height <= 0 {
		return maxBodyLines
	}
	box := m.styles.Box
	frame := box.GetBorderTopSize() + box.GetBorderBottomSize() + box.GetPaddingTop() + box.GetPaddingBottom()
	buttonRows := 0
	for _, p := range l.buttons {
		buttonRows = max(buttonRows, p.Row+1)
	}
	return min(maxBodyLines, m.height-frame-len(l.title)-buttonRows-2)
}

// labelPart is a button's resolved hint and label, as the cache key sees it.
type labelPart struct{ hint, label string }

// renderCache is the last rendered box and every input that shaped it. The
// styles are not part of the key (lipgloss styles are not comparable):
// SetStyles drops the cache instead. Everything else is compared on each
// View, which allocates nothing.
type renderCache struct {
	valid     bool
	width     int
	height    int
	highlight int
	title     string
	body      string
	parts     []labelPart // replaced, never mutated: copies of the Model share it
	out       string
}

// matches reports whether the cache was rendered from m's current inputs.
func (c *renderCache) matches(m *Model) bool {
	if !c.valid || c.width != m.width || c.height != m.height || c.highlight != m.highlight ||
		c.title != m.choice.Title || c.body != m.choice.Body || len(c.parts) != len(m.choice.Buttons) {
		return false
	}
	for i, p := range c.parts {
		if hint, label := m.labelParts(i); hint != p.hint || label != p.label {
			return false
		}
	}
	return true
}

// View renders the prompt box, or "" when hidden: the title, a blank line,
// the body and a blank line (both omitted with no body), then the button
// rows. The box is cached, so a frame that changes nothing it depends on
// returns the previous string without rendering.
func (m *Model) View() string {
	if !m.visible {
		return ""
	}
	if m.cache.matches(m) {
		return m.cache.out
	}
	out := m.render()
	parts := make([]labelPart, len(m.choice.Buttons))
	for i := range parts {
		parts[i].hint, parts[i].label = m.labelParts(i)
	}
	m.cache = renderCache{
		valid: true, width: m.width, height: m.height, highlight: m.highlight,
		title: m.choice.Title, body: m.choice.Body, parts: parts, out: out,
	}
	return out
}

func (m Model) render() string {
	width := boxWidth(m.width)
	l := m.layoutAt(width - frameWidth)
	sections := []string{m.styles.Title.Render(strings.Join(l.title, "\n"))}
	if len(l.body) > 0 {
		sections = append(sections, m.styles.Body.Render(strings.Join(l.body, "\n")))
	}
	sections = append(sections, m.renderButtons(l.buttons))
	content := strings.Join(sections, "\n\n")
	return chrome.Box(chrome.Styles{Box: m.styles.Box, BaseANSI: m.styles.BaseANSI}, content, width)
}

// ButtonAt reports which button, if any, is at cell (x, y) relative to the
// box's top-left corner. Hosts use it to turn a click into a press.
func (m Model) ButtonAt(x, y int) (int, bool) {
	if !m.visible {
		return 0, false
	}
	l := m.layoutAt(boxWidth(m.width) - frameWidth)
	top := m.styles.Box.GetBorderTopSize() + m.styles.Box.GetPaddingTop()
	left := m.styles.Box.GetBorderLeftSize() + m.styles.Box.GetPaddingLeft()
	firstRow := top + l.buttonRow()
	for _, p := range l.buttons {
		if y == firstRow+p.Row && x >= left+p.Col && x < left+p.Col+p.Width {
			return p.Index, true
		}
	}
	return 0, false
}

func boxWidth(termWidth int) int {
	return min(max(termWidth*widthPercent/100, minWidth), maxWidth)
}

// renderButtons draws placed buttons as rows, the highlighted one in
// ButtonActive.
func (m Model) renderButtons(placed []buttonrow.Placed) string {
	gap := m.styles.Footer.Render(strings.Repeat(" ", buttonGap))
	rows := buttonrow.RowsFunc(placed, gap, func(p buttonrow.Placed) string {
		if p.Index == m.highlight {
			return m.styles.ButtonActive.Render(p.Text)
		}
		return m.styles.Button.Render(p.Text)
	})
	return strings.Join(rows, "\n")
}

// wrapLines word-wraps s to width, keeping paragraph breaks and turning tabs
// into spaces; no line is wider than width. With max > 0 it keeps max lines, ending the last with "…".
func wrapLines(s string, width, max int) []string {
	s = strings.ReplaceAll(s, "\t", " ")
	var out []string
	for _, para := range strings.Split(s, "\n") {
		for _, line := range strings.Split(xansi.Wrap(para, width, ""), "\n") {
			// Wrap can leave a line one column too wide (a leading space
			// before a long token); the box would re-wrap it.
			if xansi.StringWidth(line) > width {
				out = append(out, strings.Split(xansi.Hardwrap(line, width, true), "\n")...)
				continue
			}
			out = append(out, line)
		}
	}
	if max > 0 && len(out) > max {
		out = out[:max]
		last := strings.TrimRight(out[max-1], " ")
		if xansi.StringWidth(last) >= width {
			last = xansi.Truncate(last, width-1, "")
		}
		out[max-1] = last + "…"
	}
	return out
}
