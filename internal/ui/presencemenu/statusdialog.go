package presencemenu

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// MaxStatusText is Slack's limit on a status's text, in characters.
const MaxStatusText = 100

// statusDialogEmptyText is the inline message for a status with
// neither text nor emoji.
const statusDialogEmptyText = "A status needs text or an emoji"

// StatusDialog is the "Set a status…" dialog: an emoji (picked in the
// emoji picker, which the App opens on Tab), a line of text, and when
// the status clears. The App owns key routing; this holds the state
// and draws it.
type StatusDialog struct {
	visible   bool
	workspace string
	emoji     string // shortcode without colons; "" for none
	text      string
	clear     int // index into ClearAfterChoices
	err       string
}

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*StatusDialog)(nil)

// Open shows an empty dialog for workspace. Clear after starts at
// Today, Slack's own default.
func (d *StatusDialog) Open(workspace string) {
	*d = StatusDialog{visible: true, workspace: workspace, clear: int(ClearToday)}
}

func (d *StatusDialog) Close()         { *d = StatusDialog{} }
func (d StatusDialog) IsVisible() bool { return d.visible }
func (d StatusDialog) Emoji() string   { return d.emoji }
func (d StatusDialog) Text() string    { return d.text }
func (d StatusDialog) ErrText() string { return d.err }
func (d StatusDialog) ClearAfter() ClearAfter {
	return ClearAfterChoices[d.clear]
}

// SetEmoji sets the emoji shortcode, with or without colons.
func (d *StatusDialog) SetEmoji(name string) {
	d.emoji = strings.Trim(name, ":")
	d.err = ""
}

// Insert appends typed or pasted text. Line breaks become spaces, other
// control characters are dropped, and the text stops at MaxStatusText.
func (d *StatusDialog) Insert(s string) {
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if !unicode.IsPrint(r) || utf8.RuneCountInString(d.text) >= MaxStatusText {
			continue
		}
		d.text += string(r)
	}
	d.err = ""
}

// Backspace deletes the last character of the text.
func (d *StatusDialog) Backspace() {
	if d.text != "" {
		_, n := utf8.DecodeLastRuneInString(d.text)
		d.text = d.text[:len(d.text)-n]
	}
}

// MoveClearAfter moves the clear-after selection by delta, clamped.
func (d *StatusDialog) MoveClearAfter(delta int) {
	d.clear = min(max(d.clear+delta, 0), len(ClearAfterChoices)-1)
}

// Submit returns the status to set: the trimmed text, the emoji as
// ":name:" (or ""), and the expiry for the chosen clear-after at now.
// ok is false, with the dialog showing why, when there is nothing to set.
func (d *StatusDialog) Submit(now time.Time) (text, emoji string, expiration int64, ok bool) {
	text = strings.TrimSpace(d.text)
	if text == "" && d.emoji == "" {
		d.err = statusDialogEmptyText
		return "", "", 0, false
	}
	if d.emoji != "" {
		emoji = ":" + d.emoji + ":"
	}
	return text, emoji, d.ClearAfter().Expiry(now), true
}

// clearRowsTop is the box-local row of the first clear-after row: the
// body starts with the emoji line and the "Clear after" heading.
func clearRowsTop() int { return chrome.BodyTop(messages.ChromeStyles(), true) + 2 }

// BoxSize is the rendered box's outer size.
func (d *StatusDialog) BoxSize(termWidth, termHeight int) (int, int) {
	box := d.renderBox(termWidth)
	return lipgloss.Width(box), lipgloss.Height(box)
}

// ClickRow selects the clear-after row at box-local row localY.
func (d *StatusDialog) ClickRow(termWidth, termHeight, localY int) bool {
	i := localY - clearRowsTop()
	if i < 0 || i >= len(ClearAfterChoices) {
		return false
	}
	d.clear = i
	return true
}

// ViewOverlay draws the dialog centred over a dimmed background.
func (d StatusDialog) ViewOverlay(termWidth, termHeight int, background string) string {
	if !d.visible {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, d.renderBox(termWidth), 0.5)
}

func (d StatusDialog) renderBox(termWidth int) string {
	s := messages.ChromeStyles()
	width := boxWidth(termWidth)
	list := chrome.List{Start: 0, End: len(ClearAfterChoices), Total: len(ClearAfterChoices)}
	content := list.ContentWidth(width - 4)

	title := "Set a status"
	if d.workspace != "" {
		title += " — " + d.workspace
	}
	text := d.text
	if budget := width - 6; lipgloss.Width(text) > budget {
		// Keep the end in view while typing.
		r := []rune(text)
		for lipgloss.Width(string(r)) > budget-1 {
			r = r[1:]
		}
		text = "…" + string(r)
	}

	emojiLine := s.Empty.Render("Emoji: none · Tab to pick one")
	if d.emoji != "" {
		emojiLine = s.Row.Render("Emoji: :"+d.emoji+":") + s.Empty.Render(" · Tab to change")
	}
	body := []string{emojiLine, s.Empty.Render("Clear after")}
	rows := make([]string, 0, len(ClearAfterChoices))
	for i, c := range ClearAfterChoices {
		style := s.Row
		if i == d.clear {
			style = s.RowSelected
		}
		line := c.String()
		if lipgloss.Width(line) > content {
			line = truncate.StringWithTail(line, uint(content), "…")
		}
		rows = append(rows, style.Width(content).Render(line))
	}
	body = append(body, list.Rows(s, d.clear, rows)...)

	footer := s.Empty.Render("Enter to set · ↑↓ clear after · Esc to cancel")
	if d.err != "" {
		footer = s.Empty.Foreground(styles.Error).Render(d.err)
	}
	return chrome.Frame{
		Title:  title,
		Input:  chrome.FilterInput(s, text, "What's your status?"),
		Body:   body,
		Footer: footer,
	}.Render(s, width)
}
