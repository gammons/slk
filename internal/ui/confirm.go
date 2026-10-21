// internal/ui/confirm.go
//
// Wiring for the confirm prompt: theme -> Styles, and the compositing
// and hit-testing the model no longer does for itself.
package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// confirmPromptStyles maps the active theme onto the prompt's styles. The
// prompt keeps a snapshot rather than reading the theme per frame, so it is
// pushed a fresh one on open and by applyTheme on every theme change. The
// latter matters while the prompt is open: the workspace reducers apply a
// per-workspace theme with no mode gate, so a quit prompt raised while a
// workspace connects is still visible when the theme changes.
func confirmPromptStyles() confirmprompt.Styles {
	bg := styles.Background
	cs := messages.ChromeStyles()
	return confirmprompt.Styles{
		Box:          cs.Box,
		Title:        lipgloss.NewStyle().Background(bg).Foreground(styles.Primary).Bold(true),
		Body:         lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary),
		Footer:       lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted),
		Button:       lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted),
		ButtonActive: lipgloss.NewStyle().Background(styles.Primary).Foreground(bg).Bold(true),
		BaseANSI:     cs.BaseANSI,
	}
}

// prepareConfirmPrompt pushes the current theme and size before the
// prompt opens.
func (a *App) prepareConfirmPrompt() {
	a.confirmPrompt.SetStyles(confirmPromptStyles())
	a.confirmPrompt.SetWidth(a.width)
	a.confirmPrompt.SetHeight(a.height)
}

// openConfirmPrompt raises the prompt and switches to ModeConfirm.
func (a *App) openConfirmPrompt(title, body string, onConfirm confirmprompt.ConfirmFunc) {
	a.prepareConfirmPrompt()
	a.confirmPrompt.Open(title, body, onConfirm)
	a.SetMode(ModeConfirm)
}

// openConfirmChoice raises the prompt with c (any buttons) and switches
// to ModeConfirm.
func (a *App) openConfirmChoice(c confirmprompt.Choice) {
	a.prepareConfirmPrompt()
	a.confirmPrompt.OpenChoice(c)
	a.SetMode(ModeConfirm)
}

// confirmPromptOverlay composites the prompt over background, clamped to
// height lines so wide runes cannot scroll the terminal.
func (a *App) confirmPromptOverlay(background string) string {
	box := a.confirmPrompt.View()
	if box == "" {
		return background
	}
	out := overlay.DimmedOverlay(a.width, a.height, background, box, 0.5)
	if lines := strings.Split(out, "\n"); len(lines) > a.height {
		return strings.Join(lines[:a.height], "\n")
	}
	return out
}

// confirmPromptBox adapts the size-owning prompt to chrome.Boxed, the
// interface the modal click router shares with the other modals.
type confirmPromptBox struct{ m *confirmprompt.Model }

var _ chrome.Boxed = confirmPromptBox{}

// BoxSize returns the rendered box's outer size, or (0, 0) when hidden, like
// the other modals. lipgloss.Height("") is 1, so the guard is needed.
func (b confirmPromptBox) BoxSize(int, int) (int, int) {
	box := b.m.View()
	if box == "" {
		return 0, 0
	}
	return lipgloss.Width(box), lipgloss.Height(box)
}

var _ pointClickable = confirmPromptBox{}

// ClickAt highlights the button under the box-local cell and reports
// whether there was one; the router then calls the target's onPoint.
func (b confirmPromptBox) ClickAt(_, _, localX, localY int) bool {
	i, ok := b.m.ButtonAt(localX, localY)
	if ok {
		b.m.Highlight(i)
	}
	return ok
}

// pressConfirmPromptHighlighted presses the button a click just
// highlighted. It does not go through a synthesised key: in a yes/no prompt
// Enter is a Confirm key, so "click Cancel, send Enter" would confirm.
func (a *App) pressConfirmPromptHighlighted() tea.Cmd {
	var cmd tea.Cmd
	a.confirmPrompt, cmd = a.confirmPrompt.PressHighlighted()
	return a.afterConfirmPrompt(cmd)
}
