package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

const noticeToast = "Can't reply to a system message"

func noticeMsg(ts string, replies int) messages.MessageItem {
	return messages.MessageItem{TS: ts, Subtype: "huddle_thread", Text: "A huddle started", ReplyCount: replies}
}

// noticeApp has a normal message (index 0) and a notice (index 1) in C1
// with the notice selected in a focused messages pane.
func noticeApp(t *testing.T, replies int) (*App, *int) {
	t.Helper()
	a, _ := actionApp(t, nil,
		messages.MessageItem{TS: "1.000000", UserID: "U000", UserName: "alice", Text: "hi"},
		noticeMsg("2.000000", replies))
	fetched := 0
	a.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg {
		fetched++
		return nil
	})
	a.focusedPanel = PanelMessages
	a.messagepane.SelectByIndex(1)
	return a, &fetched
}

// toastWithClear sets the toast directly and returns only the clear tick,
// so the visible toast is read from the status bar.
func pressEnterToast(t *testing.T, a *App) string {
	t.Helper()
	// Not pressKey: it would also run the toast's clear tick.
	a.Update(keyCode(tea.KeyEnter))
	return statusbarText(a)
}

func TestEnterOnReplylessNoticeOpensNoThread(t *testing.T) {
	a, fetched := noticeApp(t, 0)
	if got := pressEnterToast(t, a); !strings.Contains(got, noticeToast) {
		t.Errorf("toast = %q, want %q", got, noticeToast)
	}
	if a.threadVisible || *fetched != 0 {
		t.Errorf("threadVisible=%v fetches=%d: no thread may open", a.threadVisible, *fetched)
	}
}

func TestEnterOnNoticeWithRepliesOpensThread(t *testing.T) {
	a, _ := noticeApp(t, 2)
	pressKey(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible || a.threadPanel.ThreadTS() != "2.000000" {
		t.Errorf("threadVisible=%v ts=%q, want the notice's thread open", a.threadVisible, a.threadPanel.ThreadTS())
	}
}

// A click on a row (and so on its reply line) goes through
// openThreadForSelectedMessage.
func TestClickOnReplylessNoticeOpensNoThread(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.focusedPanel = PanelSidebar
	a.openThreadForSelectedMessage()
	if got := statusbarText(a); !strings.Contains(got, noticeToast) {
		t.Errorf("toast = %q, want %q", got, noticeToast)
	}
	if a.threadVisible {
		t.Error("click must not open a thread on a reply-less notice")
	}
}

func TestPermalinkToReplylessNoticeOpensNoThread(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.openThreadForPermalink("C1", "2.000000", false)
	if got := statusbarText(a); !strings.Contains(got, noticeToast) {
		t.Errorf("toast = %q, want %q", got, noticeToast)
	}
	if a.threadVisible {
		t.Error("permalink must not open a thread on a reply-less notice")
	}
}

func TestNormalMessageStillOpensThread(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.messagepane.SelectByIndex(0)
	pressKey(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible || a.threadPanel.ThreadTS() != "1.000000" {
		t.Errorf("threadVisible=%v ts=%q, want the normal message's thread", a.threadVisible, a.threadPanel.ThreadTS())
	}
}

// With a thread open on a normal message, selecting a reply-less notice
// and pressing Enter leaves the panel (and its composer) on that thread.
func TestOpenPanelDoesNotRetargetToNotice(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.messagepane.SelectByIndex(0)
	pressKey(t, a, keyCode(tea.KeyEnter))
	a.focusedPanel = PanelMessages
	a.messagepane.SelectByIndex(1)

	if got := pressEnterToast(t, a); !strings.Contains(got, noticeToast) {
		t.Errorf("toast = %q, want %q", got, noticeToast)
	}
	if !a.threadVisible || a.threadPanel.ThreadTS() != "1.000000" {
		t.Errorf("threadVisible=%v ts=%q, want the panel kept on 1.000000", a.threadVisible, a.threadPanel.ThreadTS())
	}
}

// A cached huddle parent often has reply_count 0 (an edit or a live reply
// never updated it); a target that is itself a reply proves the replies.
func TestPermalinkToReplyOfStaleHuddleOpensThread(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.openThreadForPermalink("C1", "2.000000", true)
	if !a.threadVisible || a.threadPanel.ThreadTS() != "2.000000" {
		t.Errorf("threadVisible=%v ts=%q: a reply's permalink must open the thread", a.threadVisible, a.threadPanel.ThreadTS())
	}
}

// A refused cache pass leaves the nav armed; the authoritative pass, with
// the real reply count, then opens the thread.
func TestPendingNavRetriesAfterRefusedCachePass(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		ChannelID: "C1", MessageTS: "2.000000", ThreadTS: "2.000000",
	}}
	a.completePendingLinkNav("C1", false)
	if a.threadVisible {
		t.Fatal("reply-less notice opened on the cache pass")
	}
	if a.pendingLinkNav == nil || a.pendingLinkNav.threadOpened {
		t.Fatalf("refused open must leave the nav armed: %+v", a.pendingLinkNav)
	}
	a.messagepane.SetMessages([]messages.MessageItem{noticeMsg("2.000000", 2)})
	a.completePendingLinkNav("C1", true)
	if !a.threadVisible || a.threadPanel.ThreadTS() != "2.000000" {
		t.Errorf("threadVisible=%v ts=%q: authoritative pass must open the thread", a.threadVisible, a.threadPanel.ThreadTS())
	}
}

// An authoritative refusal still lands the channel on the linked row and
// clears the nav, keeping the refusal's toast command.
func TestPendingNavAuthoritativeRefusalSelectsLinkedRow(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.messagepane.SelectByIndex(0)
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		ChannelID: "C1", MessageTS: "2.000000", ThreadTS: "2.000000",
	}}
	cmd := a.completePendingLinkNav("C1", true)
	if a.threadVisible {
		t.Fatal("reply-less notice opened on the authoritative pass")
	}
	if a.pendingLinkNav != nil {
		t.Errorf("authoritative refusal must clear the nav: %+v", a.pendingLinkNav)
	}
	if cmd == nil {
		t.Error("refusal toast command was dropped")
	}
	if sel, ok := a.messagepane.SelectedMessage(); !ok || sel.TS != "2.000000" {
		t.Errorf("selected = %q (ok=%v), want the linked row 2.000000", sel.TS, ok)
	}
}

// The refused cache-only pass is silent: the toast belongs to the
// authoritative pass, which may still open the thread.
func TestPendingNavRefusedCachePassDoesNotToast(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		ChannelID: "C1", MessageTS: "2.000000", ThreadTS: "2.000000",
	}}
	if cmd := a.completePendingLinkNav("C1", false); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Errorf("cache pass returned %#v, want no toast", msg)
		}
	}
	if got := statusbarText(a); strings.Contains(got, noticeToast) {
		t.Errorf("statusbar = %q: toast fired on the cache pass", got)
	}
}

func TestPendingNavToReplyOfStaleHuddleOpensOnCachePass(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		ChannelID: "C1", MessageTS: "2.500000", ThreadTS: "2.000000",
	}}
	a.completePendingLinkNav("C1", false)
	if !a.threadVisible || a.threadPanel.ThreadTS() != "2.000000" {
		t.Errorf("threadVisible=%v ts=%q", a.threadVisible, a.threadPanel.ThreadTS())
	}
}
