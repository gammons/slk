package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
)

// TestImageReadyMsg_RoutesToThread asserts that an ImageReadyMsg
// matching a thread reply's TS triggers the thread panel's cache
// invalidation. Without this routing, thread images stay as
// placeholders forever even after the bytes land in the cache.
func TestImageReadyMsg_RoutesToThread(t *testing.T) {
	app := NewApp()

	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice"}
	reply := messages.MessageItem{
		TS: "1.001", UserID: "U2", UserName: "bob",
		Attachments: []messages.Attachment{{Kind: "image", FileID: "F999", Name: "x.png"}},
	}
	app.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C1", "1.0")

	// Force a View() so HasReply works (replyIDToIdx populates lazily).
	_ = app.threadPanel.View(20, 60)

	versionBefore := app.threadPanel.Version()

	var cmd tea.Cmd
	_, cmd = app.Update(imgrender.ImageReadyMsg{Channel: "C1", TS: "1.001", Key: "F999-720"})
	_ = cmd

	if app.threadPanel.Version() == versionBefore {
		t.Fatal("ImageReadyMsg for a thread reply did not invalidate the thread cache (Version did not bump)")
	}
}

// TestImageReadyMsg_DoesNotInvalidateThreadForUnknownTS asserts that
// the routing only fires when the open thread actually contains the
// referenced reply — otherwise we'd churn the thread cache on every
// messages-pane image arrival.
func TestImageReadyMsg_DoesNotInvalidateThreadForUnknownTS(t *testing.T) {
	app := NewApp()

	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice"}
	reply := messages.MessageItem{TS: "1.001", UserID: "U2", UserName: "bob"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C1", "1.0")
	_ = app.threadPanel.View(20, 60)

	versionBefore := app.threadPanel.Version()

	var cmd tea.Cmd
	_, cmd = app.Update(imgrender.ImageReadyMsg{Channel: "C9", TS: "999.999", Key: "F1-720"})
	_ = cmd

	if app.threadPanel.Version() != versionBefore {
		t.Fatal("ImageReadyMsg for a non-thread TS unexpectedly bumped thread Version")
	}
}
