package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// A huddle with replies has no text, so opening it by permalink fills the
// parent from the cache. Live replies raise only the pane's reply count, so
// the cached row's 0 must not lower it and trip the open-thread gate.
func TestPermalinkToHuddleParentKeepsPaneReplyCount(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "2.000000", Subtype: "huddle_thread", ReplyCount: 3},
	})
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{{TS: "2.000000", Subtype: "huddle_thread", ReplyCount: 0}}
		},
	}))
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		ChannelID: "C1", MessageTS: "2.000000", ThreadTS: "2.000000",
	}}
	a.completePendingLinkNav("C1", false)
	if got := statusbarText(a); strings.Contains(got, noticeToast) {
		t.Errorf("toast = %q: the thread has replies", got)
	}
	if !a.threadVisible || a.threadPanel.ThreadTS() != "2.000000" {
		t.Errorf("threadVisible=%v ts=%q, want the thread open", a.threadVisible, a.threadPanel.ThreadTS())
	}
}

// A cache holding replies but not the parent must not turn a reply into the
// parent (nor take its count).
func TestPermalinkIgnoresCachedReplyAsParent(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "2.000000", Subtype: "huddle_thread", ReplyCount: 3},
	})
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{{TS: "3.000000", Text: "a reply", ReplyCount: 9}}
		},
	}))
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		ChannelID: "C1", MessageTS: "2.000000", ThreadTS: "2.000000",
	}}
	a.completePendingLinkNav("C1", false)
	if got := a.threadPanel.ParentMsg(); got.TS != "2.000000" || got.ReplyCount != 3 {
		t.Errorf("parent TS=%q count=%d, want the pane's huddle (2.000000, 3)", got.TS, got.ReplyCount)
	}
}

// When the fetched thread backfills a stub parent from the cache, the pane's
// reply count survives the cached row's 0.
func TestThreadRepliesLoadedKeepsPaneReplyCount(t *testing.T) {
	a, _ := noticeApp(t, 0)
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{{TS: "2.000000", Subtype: "huddle_thread", ReplyCount: 0}}
		},
	}))
	a.threadPanel.SetThread(messages.MessageItem{TS: "2.000000", ThreadTS: "2.000000", ReplyCount: 3}, nil, "C1", "2.000000")
	a.threadVisible = true
	a.Update(ThreadRepliesLoadedMsg{ThreadTS: "2.000000", Replies: []messages.MessageItem{}})
	if got := a.threadPanel.ParentMsg(); got.Subtype != "huddle_thread" || got.ReplyCount != 3 {
		t.Errorf("parent subtype=%q count=%d, want the cached parent with the pane's count 3", got.Subtype, got.ReplyCount)
	}
}
