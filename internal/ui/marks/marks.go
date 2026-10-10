// internal/ui/marks/marks.go
//
// The marks overlay widget: a scrollable list of the active
// workspace's marks, one row per mark. It follows the repo's overlay
// convention — HandleKey(keyStr string) *MarksResult (nil = stay
// open), ViewOverlay(termWidth, termHeight, background) string —
// copied structurally from the channel finder (channelfinder/model.go).
//
// Every row renders exclusively from the preview snapshot stored at
// mark time: the widget never fetches, never resolves a channel or
// message live. That is the whole reason the snapshot exists — the
// list must draw correctly offline and immediately after a restart.
//
// A mark letter jumps immediately (the overlay must not add a
// keystroke to the 'a flow); Enter selects the highlighted row; and
// backspace/Delete removes the highlighted row (the same removal the
// :delmarks command uses).
package marks

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// Row is one mark as the overlay renders it: the letter, the channel
// name, and the message preview (author + excerpt). All fields come
// from the snapshot recorded at mark time; the jump resolves live.
type Row struct {
	Letter      string
	ChannelName string
	AuthorName  string
	Excerpt     string
}

// MarksResult is returned by HandleKey when the user acts on a row.
// Exactly one of Select / Delete is non-empty; nil means the overlay
// stays open and nothing happened.
type MarksResult struct {
	// Select names the mark to jump to through the same path as the
	// ' chord (applyLocation), so the overlay and the 'a jump cannot diverge.
	Select string
	// Delete names the mark to remove — the same removal the
	// :delmarks command will use, so the two surfaces stay in sync.
	Delete string
}

// Model is the marks overlay widget.
type Model struct {
	rows     []Row
	selected int // index into rows
	visible  bool
}

// New creates the widget in its closed state.
func New() Model {
	return Model{}
}

// SetRows replaces the mark list (e.g. after a deletion) and clamps
// the selection into range.
func (m *Model) SetRows(rows []Row) {
	m.rows = rows
	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// Open shows the overlay with the first row selected.
func (m *Model) Open() {
	m.visible = true
	m.selected = 0
}

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
}

// IsVisible reports whether the overlay is showing.
func (m Model) IsVisible() bool {
	return m.visible
}

// HandleKey processes a key and returns a result when the user acts
// on a row, nil otherwise. A mark letter (a-z, A-Z) jumps immediately
// — the overlay is a reference list, not a filter, so 'a never takes
// an extra keystroke. backspace/Delete removes the highlighted row.
func (m *Model) HandleKey(keyStr string) *MarksResult {
	if len(keyStr) == 1 && isMarkLetter(keyStr[0]) {
		return &MarksResult{Select: keyStr}
	}

	switch keyStr {
	case "enter":
		if len(m.rows) == 0 {
			return nil
		}
		return &MarksResult{Select: m.rows[m.selected].Letter}

	case "esc":
		m.Close()
		return nil

	case "down", "ctrl+n":
		if m.selected < len(m.rows)-1 {
			m.selected++
		}
		return nil

	case "up", "ctrl+p":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "backspace", "delete":
		if len(m.rows) == 0 {
			return nil
		}
		return &MarksResult{Delete: m.rows[m.selected].Letter}
	}
	return nil
}

func isMarkLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// maxVisibleRows is the height of the scroll window for the list.
const maxVisibleRows = 10

// boxWidth returns the modal's outer width for a given terminal
// width, matching the channel finder's sizing.
func boxWidth(termWidth int) int {
	w := termWidth / 2
	if w < 30 {
		w = 30
	}
	if w > 80 {
		w = 80
	}
	return w
}

// visibleWindow returns the [start, end) slice of rows currently
// shown in the list, applying the same scroll-window math the
// renderer uses.
func (m *Model) visibleWindow() (int, int) {
	maxVisible := maxVisibleRows
	total := len(m.rows)
	if maxVisible > total {
		maxVisible = total
	}
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
	return startIdx, endIdx
}

// View renders just the overlay box (no backdrop). Used by tests.
func (m Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// ViewOverlay renders the overlay as a centered modal with a dimmed
// backdrop, following the repo's overlay convention.
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

	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4 // border + padding

	// All inner spans share the modal bg so the dimmed app behind the
	// overlay doesn't bleed through where styled fragments end.
	bg := styles.Background

	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("Marks")

	total := len(m.rows)
	startIdx, endIdx := m.visibleWindow()
	maxVisible := endIdx - startIdx

	// Scrollbar, shown only when the list overflows the window.
	showScrollbar := total > maxVisible
	contentWidth := innerWidth - 1 // 1 col letter prefix
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

	var rows []string
	for i := startIdx; i < endIdx; i++ {
		row := m.rows[i]
		isSelected := i == m.selected

		letterStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Bold(true)
		contentStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
		if isSelected {
			letterStyle = letterStyle.Foreground(styles.Primary)
			contentStyle = contentStyle.Foreground(styles.Primary).Bold(true)
		}

		preview := row.Excerpt
		if row.AuthorName != "" {
			preview = row.AuthorName + ": " + preview
		}
		line := letterStyle.Render(row.Letter) + "  " +
			contentStyle.Render("#"+row.ChannelName) + "  " +
			contentStyle.Render(preview)

		// Truncate on measured width (lipgloss.Width), not rune count:
		// excerpts are arbitrary user text and can carry wide glyphs
		// (emoji). truncate.StringWithTail is ANSI-aware.
		if lipgloss.Width(line) > contentWidth {
			line = truncate.StringWithTail(line, uint(contentWidth), "…")
		}
		// Right-pad with spaces to fill the row.
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}

		var rowStr string
		if isSelected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("▌")
			rowStr = indicator + line
		} else {
			rowStr = " " + line
		}

		if showScrollbar {
			rel := i - startIdx
			if rel >= thumbStart && rel < thumbEnd {
				rowStr += thumbStyle.Render("\u2588") // █ thumb
			} else {
				rowStr += trackStyle.Render("\u2502") // │ track
			}
		}
		rows = append(rows, rowStr)
	}

	if total == 0 {
		noMarks := lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No marks set")
		rows = append(rows, noMarks)
	}

	content := title + "\n\n" + strings.Join(rows, "\n")

	// Re-paint modal bg+fg after every ANSI reset emitted by inner
	// styled spans, mirroring the channel finder.
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
