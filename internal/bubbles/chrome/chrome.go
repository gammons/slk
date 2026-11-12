// Package chrome is the dialog chrome slk's dialogs and composer popups
// share: the bordered box, the title / input / body / footer layout, the
// filter line, the selected-row marker, the list window and the
// scrollbar gutter. It holds no state. A dialog keeps its own fields,
// matching, row content and keys, renders its rows as strings, and
// draws them through this package. The theme arrives as a Styles; the
// App builds one from its palette (messages.ChromeStyles).
package chrome

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/ansi"
)

// Styles is the dialog palette: what chrome draws with, plus Row,
// RowSelected and Footer, which dialogs use for the row content and
// footers they render themselves. None of them sets a Width; chrome
// applies widths itself.
type Styles struct {
	Box         lipgloss.Style // a centered dialog's border, padding and background
	Popup       lipgloss.Style // a composer popup's border and background
	Title       lipgloss.Style
	Input       lipgloss.Style // the ▌-bordered filter line
	Placeholder lipgloss.Style // the filter line's placeholder
	Row         lipgloss.Style // a row's content
	RowSelected lipgloss.Style // the selected row's content
	Marker      lipgloss.Style // the ▌ before the selected row
	PopupMarker lipgloss.Style // the "▌ " before a popup's selected row
	Empty       lipgloss.Style // "No matching …", and other italic muted lines
	Footer      lipgloss.Style // footer hints, and other muted lines
	Thumb       lipgloss.Style // the scrollbar thumb █
	Track       lipgloss.Style // the scrollbar track │

	// BaseANSI is re-asserted after every SGR reset inside Box, so the
	// resets nested renders emit cannot drop the box's background.
	// Empty disables it.
	BaseANSI string
	// PopupANSI is BaseANSI for Popup: re-asserted after every reset in
	// the popup's rows. Empty disables it.
	PopupANSI string
}

// Box draws content inside s.Box at the given outer width, with
// s.BaseANSI re-asserted after every reset in content. It is the one box
// renderer; Frame lays a dialog out on top of it.
func Box(s Styles, content string, width int) string {
	return s.Box.Width(width).Render(ansi.ReapplyAfterResets(content, s.BaseANSI))
}

// Frame is a dialog's layout: the title, the input line, a blank line,
// the body, then a blank line and the footer. Title and Empty are plain
// text, drawn in s.Title and s.Empty; Input, Body and Footer arrive
// rendered. An empty Input or Footer is left out with its line; Empty is
// drawn in place of an empty Body.
type Frame struct {
	Title  string
	Input  string
	Body   []string
	Empty  string
	Footer string
}

// Render draws the frame inside Box at the given outer width.
func (f Frame) Render(s Styles, width int) string {
	body := f.Body
	if len(body) == 0 && f.Empty != "" {
		body = []string{s.Empty.Render(f.Empty)}
	}
	var b strings.Builder
	b.WriteString(s.Title.Render(f.Title))
	if f.Input != "" {
		b.WriteString("\n")
		b.WriteString(f.Input)
	}
	b.WriteString("\n\n")
	b.WriteString(strings.Join(body, "\n"))
	if f.Footer != "" {
		b.WriteString("\n\n")
		b.WriteString(f.Footer)
	}
	return Box(s, b.String(), width)
}

// BodyTop is the box-local row of a Frame's first body line drawn in
// s.Box (0 is the top border): the border and padding, the title, the
// input line when hasInput, and the blank line. A dialog's ClickRow
// subtracts it from the clicked row.
func BodyTop(s Styles, hasInput bool) int {
	top := s.Box.GetBorderTopSize() + s.Box.GetPaddingTop() + 2
	if hasInput {
		top++
	}
	return top
}

// FilterInput is the filter line: the placeholder after a block cursor
// while query is empty, the query followed by the cursor after.
func FilterInput(s Styles, query, placeholder string) string {
	text := query + "█"
	if query == "" {
		text = "█ " + s.Placeholder.Render(placeholder)
	}
	return s.Input.Render(text)
}

// Popup draws a composer popup's rows inside s.Popup, width-2 wide so it
// lines up with the composer under it. No rows draws nothing.
func Popup(s Styles, rows []string, width int) string {
	if len(rows) == 0 {
		return ""
	}
	return s.Popup.Width(width - 2).Render(ansi.ReapplyAfterResets(strings.Join(rows, "\n"), s.PopupANSI))
}

// PopupMarker is the two-column marker in front of a popup row: "▌ " in
// s.PopupMarker for the selected row, two spaces otherwise.
func PopupMarker(s Styles, selected bool) string {
	if selected {
		return s.PopupMarker.Render("▌ ")
	}
	return "  "
}
