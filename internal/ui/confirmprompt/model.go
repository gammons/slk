// Package confirmprompt provides a small centered yes/no confirmation
// overlay used for destructive actions (e.g. deleting a message).
//
// The shape mirrors reactionpicker.Model: Open / Close / IsVisible /
// HandleKey / View / ViewOverlay so the App can composite it with the
// same overlay.DimmedOverlay path.
package confirmprompt

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// Result is returned by HandleKey to describe the outcome of a single
// key press.
type Result struct {
	// Confirmed is true if the user pressed y/Y/Enter.
	Confirmed bool
	// Cancelled is true if the user pressed n/N/Esc/any other key.
	Cancelled bool
	// Cmd, when non-nil, is the tea.Cmd produced by the registered
	// onConfirm callback. The caller should append it to its returned
	// command list. Always nil on cancel or when onConfirm was nil.
	Cmd tea.Cmd
}

// Model is the confirmation overlay.
type Model struct {
	visible   bool
	title     string
	body      string
	onConfirm func() tea.Msg
}

// New creates an empty, hidden prompt.
func New() *Model {
	return &Model{}
}

// Open shows the prompt with the given title (e.g. "Delete message?")
// and body (typically a short preview of the affected content).
// onConfirm is invoked as a tea.Cmd when the user confirms; pass nil
// for confirmation that has no follow-up action.
func (m *Model) Open(title, body string, onConfirm func() tea.Msg) {
	m.title = title
	m.body = body
	m.onConfirm = onConfirm
	m.visible = true
}

// Close hides the prompt and clears state.
func (m *Model) Close() {
	m.visible = false
	m.title = ""
	m.body = ""
	m.onConfirm = nil
}

// IsVisible returns whether the prompt is showing.
func (m *Model) IsVisible() bool { return m.visible }

// HandleKey processes a single key event. y/Y/Enter confirm; n/N/Esc
// and any other key cancel. The caller should restore the previous
// Mode after this returns. The prompt is always closed after HandleKey.
func (m *Model) HandleKey(keyStr string) Result {
	if !m.visible {
		return Result{}
	}
	switch keyStr {
	case "y", "Y", "enter":
		var cmd tea.Cmd
		if m.onConfirm != nil {
			fn := m.onConfirm
			cmd = func() tea.Msg { return fn() }
		}
		m.Close()
		return Result{Confirmed: true, Cmd: cmd}
	default:
		// n/N/esc/escape/anything else cancels.
		m.Close()
		return Result{Cancelled: true}
	}
}

// View renders the box content. Returns "" when the prompt is hidden.
func (m *Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// ViewOverlay composites the prompt over the given background. Returns
// the background unchanged when the prompt is hidden.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	result := overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
	// Clamp to exactly termHeight lines so unpredictable Unicode widths
	// don't cause terminal scrolling. Same caveat as reactionpicker.
	lines := strings.Split(result, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth * 35 / 100
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	innerWidth := overlayWidth - 4 // border + padding

	bg := styles.Background

	title := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.Primary).
		Bold(true).
		Render(m.title)

	// Collapse newlines and tabs to single spaces so multi-line bodies
	// render in a single-line preview rather than breaking the box.
	bodyText := m.body
	bodyText = strings.ReplaceAll(bodyText, "\n", " ")
	bodyText = strings.ReplaceAll(bodyText, "\t", " ")
	if lipgloss.Width(bodyText) > innerWidth {
		bodyText = truncate.StringWithTail(bodyText, uint(innerWidth), "…")
	}
	body := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextPrimary).
		Render("> " + bodyText)

	footer := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("[y] confirm   [n/Esc] cancel")

	content := title + "\n\n" + body + "\n\n" + footer
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
