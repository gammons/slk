package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
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

// threadParentImageApp opens a thread on C1 whose PARENT carries an
// image, with the reply index built, and returns the app and the
// thread's cache version before the message under test.
func threadParentImageApp(t *testing.T) (*App, int64) {
	t.Helper()
	app := NewApp()
	parent := messages.MessageItem{
		TS: "1.0", UserID: "U1", UserName: "alice",
		Attachments: []messages.Attachment{{Kind: "image", FileID: "FPARENT", Name: "p.png"}},
	}
	reply := messages.MessageItem{TS: "1.001", UserID: "U2", UserName: "bob"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C1", "1.0")
	// Force a View() so replyIDToIdx populates (HasReply is lazy).
	_ = app.threadPanel.View(20, 60)
	return app, app.threadPanel.Version()
}

// TestBlockImageReadyMsg_RoutesToThreadParent covers the bug Greptile
// found on PR #467: the image-ready arms matched only replies, via
// HasReply, so a Block Kit or link-preview image on an open thread's
// PARENT kept its loading placeholder until something else redrew the
// thread.
func TestBlockImageReadyMsg_RoutesToThreadParent(t *testing.T) {
	app, before := threadParentImageApp(t)

	_, _ = app.Update(blockkit.BlockImageReadyMsg{
		Channel: "C1", TS: "1.0", URL: "https://example.com/card.png",
	})

	if app.threadPanel.Version() == before {
		t.Fatal("BlockImageReadyMsg for the thread parent did not invalidate the thread cache")
	}
}

// TestImageReadyMsg_RoutesToThreadParent is the same for the attachment
// arm: both arms had the bug, so fixing one would have left the other.
func TestImageReadyMsg_RoutesToThreadParent(t *testing.T) {
	app, before := threadParentImageApp(t)

	_, _ = app.Update(imgrender.ImageReadyMsg{Channel: "C1", TS: "1.0", Key: "FPARENT-720"})

	if app.threadPanel.Version() == before {
		t.Fatal("ImageReadyMsg for the thread parent did not invalidate the thread cache")
	}
}

// TestImageReadyMsg_IgnoresMatchingTSInAnotherChannel pins the other
// half of the fix. HasReply matched a bare TS with no channel
// comparison, so an image arriving for a different channel that happened
// to share a TS dropped an unrelated thread's cache. The pre-existing
// "unknown TS" test could not catch this: it varied the TS as well.
//
// Attachment images only: blockkit.BlockImageReadyMsg deliberately
// matches on the TS alone (see threadShowsTS, and the two tests below).
func TestImageReadyMsg_IgnoresMatchingTSInAnotherChannel(t *testing.T) {
	app, before := threadParentImageApp(t)

	// Same TS as the open thread's reply, different channel.
	_, _ = app.Update(imgrender.ImageReadyMsg{Channel: "C9", TS: "1.001", Key: "F1-720"})
	if app.threadPanel.Version() != before {
		t.Error("ImageReadyMsg from another channel invalidated the thread cache")
	}

	// And the same for the parent's TS.
	_, _ = app.Update(imgrender.ImageReadyMsg{Channel: "C9", TS: "1.0", Key: "FPARENT-720"})
	if app.threadPanel.Version() != before {
		t.Error("ImageReadyMsg from another channel invalidated the thread cache for the parent")
	}
}

// TestBlockImageReadyMsg_RoutesToThreadParentFromChannelPane covers the
// second-round review finding on PR #467. blockkit dedupes in-flight
// fetches by URL across every pane, and the channel pane stamps its
// fetches with the channel NAME while the thread pane uses the ID (plan
// decision P3). So when the channel pane wins the race for a Block Kit
// image on an open thread's parent, the only message that is ever sent
// carries the name — and comparing it against the thread's channel ID
// left the placeholder up, which is the very bug
// TestBlockImageReadyMsg_RoutesToThreadParent set out to fix.
func TestBlockImageReadyMsg_RoutesToThreadParentFromChannelPane(t *testing.T) {
	app, before := threadParentImageApp(t)

	// What the channel pane's renderer reports: the channel's name, not "C1".
	_, _ = app.Update(blockkit.BlockImageReadyMsg{
		Channel: "general", TS: "1.0", URL: "https://example.com/card.png",
	})

	if app.threadPanel.Version() == before {
		t.Fatal("BlockImageReadyMsg stamped with the channel name did not invalidate the thread cache for the parent")
	}
}

// TestBlockImageReadyMsg_RoutesToThreadReplyFromChannelPane is the same
// for a reply that was also sent to the channel. On main, HasReply
// ignored the channel and caught this; adding the comparison regressed
// it.
func TestBlockImageReadyMsg_RoutesToThreadReplyFromChannelPane(t *testing.T) {
	app, before := threadParentImageApp(t)

	_, _ = app.Update(blockkit.BlockImageReadyMsg{
		Channel: "general", TS: "1.001", URL: "https://example.com/card.png",
	})

	if app.threadPanel.Version() == before {
		t.Fatal("BlockImageReadyMsg stamped with the channel name did not invalidate the thread cache for a reply")
	}
}
