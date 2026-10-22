package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
)

// clickAtContent sends a left click at pane-content (col, row) of the
// messages pane and returns the command it produced. The inverse of
// the coordinate math the image-click tests use.
func clickAtContent(app *App, col, row int) tea.Cmd {
	chrome := app.messagepane.ChromeHeight()
	x := app.layout.sidebarEnd + 1 + col
	y := 1 + chrome + row
	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return cmd
}

// TestMouseClick_OnLinkDispatchesOpenLink pins the mouse half of the
// link feature: a labelled Slack link renders as an OSC 8 span, the
// span lands in the hit cache, and a click inside it produces the same
// OpenLinkMsg `o` issues.
func TestMouseClick_OnLinkDispatchesOpenLink(t *testing.T) {
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice",
		Text: "see <https://example.com/docs|the docs> please",
	}))
	_ = app.View()
	_ = app.View()

	rects := app.messagepane.LastHitsForKindForTest(entrylist.HitLink)
	if len(rects) == 0 {
		t.Fatal("expected a link hit rect after View() of a message with a link")
	}
	h := rects[0]
	if h.FileID != "https://example.com/docs" {
		t.Errorf("hit URL = %q; want https://example.com/docs", h.FileID)
	}
	colMid := (h.ColStart + h.ColEnd) / 2
	rowMid := (h.RowStart + h.RowEnd) / 2

	cmd := clickAtContent(app, colMid, rowMid)
	if cmd == nil {
		t.Fatal("expected a cmd from clicking a link")
	}
	out := cmd()
	m, ok := out.(OpenLinkMsg)
	if !ok {
		t.Fatalf("got %T, want OpenLinkMsg", out)
	}
	if m.URL != "https://example.com/docs" {
		t.Errorf("URL = %q; want https://example.com/docs", m.URL)
	}
}

// TestMouseClick_BesideLinkDoesNothing asserts the span is the
// clickable thing, not the row: a click in the padding right of a link
// label, and one on the border well left of it, fall through to the
// selection path and dispatch no link open.
func TestMouseClick_BesideLinkDoesNothing(t *testing.T) {
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice",
		Text: "see <https://example.com/docs|the docs> please",
	}))
	_ = app.View()
	_ = app.View()

	rects := app.messagepane.LastHitsForKindForTest(entrylist.HitLink)
	if len(rects) == 0 {
		t.Fatal("expected a link hit rect")
	}
	h := rects[0]
	rowMid := (h.RowStart + h.RowEnd) / 2
	for _, col := range []int{1, h.ColEnd + 2} {
		cmd := clickAtContent(app, col, rowMid)
		if cmd == nil {
			continue // fall-through to selection is also a nil cmd
		}
		if _, ok := cmd().(OpenLinkMsg); ok {
			t.Errorf("click at column %d (outside the span [%d,%d)) opened a link", col, h.ColStart, h.ColEnd)
		}
	}
}

// TestMouseClick_OnFileLineDispatchesDownload pins the file half: a
// file attachment's [File] line is a hit rect, and clicking it
// dispatches the DownloadFileMsg `d` would for that one attachment.
func TestMouseClick_OnFileLineDispatchesDownload(t *testing.T) {
	att := core.Attachment{
		Kind:        "file",
		Name:        "report.pdf",
		URL:         "https://example.com/perma",
		DownloadURL: "https://example.com/dl",
	}
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "here you go",
		Attachments: []core.Attachment{att},
	}))
	_ = app.View()
	_ = app.View()

	rects := app.messagepane.LastHitsForKindForTest(entrylist.HitFile)
	if len(rects) == 0 {
		t.Fatal("expected a file hit rect after View() of a message with a file attachment")
	}
	h := rects[0]
	colMid := (h.ColStart + h.ColEnd) / 2
	rowMid := (h.RowStart + h.RowEnd) / 2

	cmd := clickAtContent(app, colMid, rowMid)
	if cmd == nil {
		t.Fatal("expected a cmd from clicking a file line")
	}
	out := cmd()
	m, ok := out.(DownloadFileMsg)
	if !ok {
		t.Fatalf("got %T, want DownloadFileMsg", out)
	}
	if m.Attachment.DownloadURL != att.DownloadURL {
		t.Errorf("DownloadURL = %q; want %q", m.Attachment.DownloadURL, att.DownloadURL)
	}
	if m.Attachment.Name != att.Name {
		t.Errorf("Name = %q; want %q", m.Attachment.Name, att.Name)
	}
}

// TestMouseClick_OnWrappedFileLineTargetsRightFile pins the wrapped
// case: a filename longer than the pane wraps across rows, and the hit
// rects are classified from the final wrapped rows by URL, so clicking
// a continuation row still downloads that attachment - not whatever
// attachment's pre-wrap row it landed on.
func TestMouseClick_OnWrappedFileLineTargetsRightFile(t *testing.T) {
	longName := strings.Repeat("very-long-filename-", 8) + "a.pdf"
	first := core.Attachment{
		Kind: "file", Name: longName,
		URL: "https://example.com/first", DownloadURL: "https://example.com/first-dl",
	}
	second := core.Attachment{
		Kind: "file", Name: "second.pdf",
		URL: "https://example.com/second", DownloadURL: "https://example.com/second-dl",
	}
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "two files",
		Attachments: []core.Attachment{first, second},
	}))
	_ = app.View()
	_ = app.View()

	rects := app.messagepane.LastHitsForKindForTest(entrylist.HitFile)
	if len(rects) == 0 {
		t.Fatal("expected file hit rects")
	}
	byURL := map[string][]messages.HitRect{}
	for _, h := range rects {
		byURL[h.FileID] = append(byURL[h.FileID], h)
	}
	firstRects, ok := byURL["https://example.com/first"]
	if !ok || len(firstRects) == 0 {
		t.Fatalf("no hit rects for the wrapping attachment: %+v", rects)
	}
	// Click the LAST row of the wrapped first attachment: pre-wrap
	// registration would have put the second attachment's rows there.
	last := firstRects[len(firstRects)-1]
	cmd := clickAtContent(app, (last.ColStart+last.ColEnd)/2, (last.RowStart+last.RowEnd)/2)
	if cmd == nil {
		t.Fatal("expected a cmd from clicking the wrapped file line")
	}
	m, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("got %T, want DownloadFileMsg", cmd())
	}
	if m.Attachment.DownloadURL != first.DownloadURL {
		t.Errorf("DownloadURL = %q; want %q (the wrapped first attachment)", m.Attachment.DownloadURL, first.DownloadURL)
	}
}
