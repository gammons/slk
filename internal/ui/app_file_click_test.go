package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
)

// TestMouseClick_OnFileLineDispatchesDownload: clicking a
// "[File] <name>" line downloads the file, like `d`, in the channel
// pane and in the thread pane (parent and replies), instead of
// falling through to click-to-select.
func TestMouseClick_OnFileLineDispatchesDownload(t *testing.T) {
	file := func(name string) messages.Attachment {
		return messages.Attachment{
			Kind:        "file",
			Name:        name,
			URL:         "https://acme.slack.com/files/U1/F1/" + name,
			DownloadURL: "https://files.slack.com/files-pri/T1-F1/" + name,
		}
	}
	msg := func(ts, text string, atts ...messages.Attachment) messages.MessageItem {
		return messages.MessageItem{TS: ts, UserID: "U1", UserName: "alice", Text: text, Timestamp: "10:00 AM", Attachments: atts}
	}

	for _, tc := range []struct {
		name   string
		target string // file name to click
		setup  func(a *App)
		thread bool // look for the target in the thread pane
	}{
		{
			name:   "channel pane",
			target: "chan.pdf",
			setup: func(a *App) {
				a.focusedPanel = PanelMessages
				a.messagepane.SetMessages([]messages.MessageItem{msg("1700000000.000100", "here", file("chan.pdf"))})
			},
		},
		{
			name:   "thread reply",
			target: "second.pdf",
			thread: true,
			setup: func(a *App) {
				parent := msg("1700000000.000001", "thread")
				a.messagepane.SetMessages([]messages.MessageItem{parent})
				a.threadVisible = true
				a.focusedPanel = PanelThread
				a.threadPanel.SetThread(parent, []messages.MessageItem{
					msg("1700000000.000200", "two files", file("first.pdf"), file("second.pdf")),
				}, "C1", parent.TS)
			},
		},
		{
			name:   "thread parent",
			target: "parent.pdf",
			thread: true,
			setup: func(a *App) {
				parent := msg("1700000000.000001", "thread", file("parent.pdf"))
				a.messagepane.SetMessages([]messages.MessageItem{parent})
				a.threadVisible = true
				a.focusedPanel = PanelThread
				a.threadPanel.SetThread(parent, []messages.MessageItem{msg("1700000000.000200", "reply")}, "C1", parent.TS)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t, withSize(200, 60), withActiveChannel("C1"))
			tc.setup(a)
			_ = a.View()

			// Find the target's "[File]" marker on screen and click it.
			x, y := -1, -1
			for row, line := range strings.Split(ansi.Strip(a.View().Content), "\n") {
				i := strings.Index(line, "[File] "+tc.target)
				if tc.thread { // the channel pane may show it on the same row
					i = strings.LastIndex(line, "[File] "+tc.target)
				}
				if i < 0 {
					continue
				}
				if col := ansi.StringWidth(line[:i]); !tc.thread || col > a.layout.msgEnd {
					x, y = col, row
					break
				}
			}
			if x < 0 {
				t.Fatalf("[File] %s not on screen", tc.target)
			}

			_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd == nil {
				t.Fatal("click on the file line returned no cmd")
			}
			dl, ok := cmd().(DownloadFileMsg)
			if !ok {
				t.Fatalf("got %T, want DownloadFileMsg", cmd())
			}
			if want := file(tc.target).DownloadURL; dl.Attachment.DownloadURL != want {
				t.Errorf("DownloadURL = %q, want %q", dl.Attachment.DownloadURL, want)
			}
		})
	}
}

// TestMouseClick_OnFileLineWithoutDownloadURLFallsThrough: a "[File]"
// line with no DownloadURL records no hit, so the click falls through
// to click-to-select instead of dispatching a download.
func TestMouseClick_OnFileLineWithoutDownloadURLFallsThrough(t *testing.T) {
	a := newTestApp(t, withSize(200, 60), withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1700000000.000100", UserID: "U1", UserName: "alice", Text: "here", Timestamp: "10:00 AM",
		Attachments: []messages.Attachment{{Kind: "file", Name: "nolink.pdf", URL: "https://acme.slack.com/files/U1/F1/nolink.pdf"}},
	}))
	a.focusedPanel = PanelMessages
	_ = a.View()

	x, y := -1, -1
	for row, line := range strings.Split(ansi.Strip(a.View().Content), "\n") {
		if i := strings.Index(line, "[File] nolink.pdf"); i >= 0 {
			x, y = ansi.StringWidth(line[:i]), row
			break
		}
	}
	if x < 0 {
		t.Fatal("[File] nolink.pdf not on screen")
	}

	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		if _, ok := cmd().(DownloadFileMsg); ok {
			t.Fatal("click on a file line without DownloadURL dispatched DownloadFileMsg")
		}
	}
}
