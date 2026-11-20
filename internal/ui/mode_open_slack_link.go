package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// slackLinkErrorText is the inline validation the dialog shows when
// Enter is pressed on something slackurl.Parse rejects.
const slackLinkErrorText = "Not a Slack message link"

func handleOpenSlackLinkMode(a *App, msg tea.KeyMsg) tea.Cmd {
	k := msg.Key()
	if k.Mod&tea.ModCtrl != 0 && (k.Code == 'v' || k.Code == 'V') {
		return a.pasteSlackLinkFromClipboard()
	}
	switch k.Code {
	case tea.KeyEscape:
		a.clearSlackLink()
		a.SetMode(ModeNormal)
		return nil
	case tea.KeyEnter:
		return a.submitSlackLink()
	case tea.KeyBackspace:
		if a.slackLinkInput != "" {
			_, n := utf8.DecodeLastRuneInString(a.slackLinkInput)
			a.slackLinkInput = a.slackLinkInput[:len(a.slackLinkInput)-n]
			a.slackLinkError = ""
		}
		return nil
	}
	s := msg.String()
	if r := []rune(s); len(r) == 1 && unicode.IsPrint(r[0]) && !unicode.IsSpace(r[0]) {
		a.slackLinkInput += s
		a.slackLinkError = ""
	}
	return nil
}

func (a *App) submitSlackLink() tea.Cmd {
	raw := strings.TrimSpace(a.slackLinkInput)
	if _, ok := slackurl.Parse(raw); !ok {
		a.slackLinkError = slackLinkErrorText
		return nil
	}
	a.clearSlackLink()
	a.SetMode(ModeNormal)
	return a.openFinderPermalink(raw)
}

func (a *App) clearSlackLink() {
	a.slackLinkInput = ""
	a.slackLinkError = ""
}

// pasteSlackLinkFromClipboard reads the OS clipboard the way the
// composer's smart-paste text fallback does, and inserts it into the
// dialog. Images and file paths are not attachments here.
func (a *App) pasteSlackLinkFromClipboard() tea.Cmd {
	if !a.clipboardAvailable || a.desktop == nil {
		return nil
	}
	a.appendSlackLinkPaste(string(a.desktop.ReadClipboard(core.ClipboardText)))
	return nil
}

// appendSlackLinkPaste inserts a bracketed paste or clipboard read.
// Whitespace and newlines are dropped so a wrapped copy stays one URL.
func (a *App) appendSlackLinkPaste(content string) {
	flat := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, content)
	if flat == "" {
		return
	}
	a.slackLinkInput += flat
	a.slackLinkError = ""
}

// openSlackLinkView draws the Open Slack link dialog. It follows the
// custom-snooze box: one chrome frame, a single input line, a footer.
func openSlackLinkView(termWidth, termHeight int, background, query, errText string) string {
	s := messages.ChromeStyles()
	width := openSlackLinkWidth(termWidth)
	// Border and padding take four columns; the block cursor takes one.
	budget := width - 5
	if budget < 1 {
		budget = 1
	}
	shown := query
	if lipgloss.Width(shown) > budget {
		shown = truncate.StringWithTail(shown, uint(budget), "…")
	}
	footer := "Enter to open · Esc to cancel · Ctrl+V to paste"
	if errText != "" {
		footer = errText
	}
	footerStyle := s.Empty
	if errText != "" {
		footerStyle = footerStyle.Foreground(styles.Error)
	}
	box := chrome.Frame{
		Title:  "Open Slack link",
		Body:   []string{s.Input.Render(shown + "█")},
		Footer: footerStyle.Render(footer),
	}.Render(s, width)
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func openSlackLinkWidth(termWidth int) int {
	w := termWidth * 2 / 3
	if w < 40 {
		w = 40
	}
	if w > 80 {
		w = 80
	}
	return w
}
