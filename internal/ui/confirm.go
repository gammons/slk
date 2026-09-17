// internal/ui/confirm.go
//
// Wiring for the confirm prompt: theme -> Styles, and the compositing
// and hit-testing the model no longer does for itself.
package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// confirmPromptStyles maps the active theme onto the prompt's styles. Re-read
// per open: ModeConfirm and ModeThemeSwitcher are mutually exclusive, so the
// prompt cannot be visible across a theme change.
func confirmPromptStyles() confirmprompt.Styles {
	bg := styles.Background
	return confirmprompt.Styles{
		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.Primary).
			BorderBackground(bg).
			Background(bg).
			Padding(1, 1),
		Title:    lipgloss.NewStyle().Background(bg).Foreground(styles.Primary).Bold(true),
		Body:     lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary),
		Footer:   lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted),
		BaseANSI: messages.BgANSI() + messages.FgANSI(),
	}
}

// openConfirmPrompt raises the prompt and switches to ModeConfirm.
func (a *App) openConfirmPrompt(title, body string, onConfirm confirmprompt.ConfirmFunc) {
	a.confirmPrompt.SetStyles(confirmPromptStyles())
	a.confirmPrompt.SetWidth(a.width)
	a.confirmPrompt.Open(title, body, onConfirm)
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

// confirmPromptBox adapts the size-owning prompt to the boxedOverlay
// interface the modal click router shares with the other modals.
type confirmPromptBox struct{ m *confirmprompt.Model }

var _ boxedOverlay = confirmPromptBox{}

func (b confirmPromptBox) BoxSize(int, int) (int, int) {
	box := b.m.View()
	return lipgloss.Width(box), lipgloss.Height(box)
}
