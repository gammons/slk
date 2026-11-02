// internal/ui/username_fanout_test.go
//
// A user name resolved after the panes were handed their name map
// (UserResolvedMsg) must reach every pane that shows names: each
// window's messages, the thread panel, the Threads list and the
// Activity feed.
package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages"
)

// unresolved is a message whose author name is still the raw ID, as the
// history fetchers leave it for an unknown user.
func unresolved(ts string) messages.MessageItem {
	return messages.MessageItem{TS: ts, UserID: "U9", UserName: "U9", Text: "hi", Timestamp: "1:00 PM"}
}

func screenText(a *App) string { return ansi.Strip(a.View().Content) }

func TestUserResolved_ReachesThreadsList(t *testing.T) {
	a := newTestApp(t, withSize(160, 40), withView(ViewThreads),
		withThreadsView([]cache.ThreadSummary{{
			ChannelID: "C1", ChannelName: "general", ChannelType: "channel",
			ThreadTS: "1.0", ParentUserID: "U9", ParentText: "hello",
			ParentTS: "1.0", ReplyCount: 1, LastReplyTS: "2.0", LastReplyBy: "U9",
		}}))
	a.SetUserNames(map[string]string{"U1": "alice"})
	if got := screenText(a); !strings.Contains(got, "U9") {
		t.Fatalf("precondition: Threads list should show the raw ID U9:\n%s", got)
	}

	_, _ = a.Update(UserResolvedMsg{UserID: "U9", DisplayName: "zed"})
	if got := screenText(a); !strings.Contains(got, "zed") {
		t.Errorf("Threads list still lacks the resolved name zed:\n%s", got)
	}
}

func TestUserResolved_ReachesActivityFeed(t *testing.T) {
	a := newTestApp(t, withSize(160, 40), withView(ViewActivity))
	a.activityView.SetItems([]core.ActivityItem{{
		Key: "k1", Type: "at_user", FeedTS: "1.0", ChannelID: "C1", TS: "1.0", AuthorID: "U9",
	}})
	a.SetUserNames(map[string]string{"U1": "alice"})
	if got := screenText(a); !strings.Contains(got, "U9") {
		t.Fatalf("precondition: Activity feed should show the raw ID U9:\n%s", got)
	}

	_, _ = a.Update(UserResolvedMsg{UserID: "U9", DisplayName: "zed"})
	if got := screenText(a); !strings.Contains(got, "zed") {
		t.Errorf("Activity feed still lacks the resolved name zed:\n%s", got)
	}
}

func TestUserResolved_ReachesThreadPanel(t *testing.T) {
	a := newTestApp(t)
	a.SetUserNames(map[string]string{"U1": "alice"})
	a.messagepane.SetMessages([]messages.MessageItem{unresolved("1.0")})
	a.threadPanel.SetThread(unresolved("1.0"), []messages.MessageItem{unresolved("2.0")}, "C1", "1.0")

	_, _ = a.Update(UserResolvedMsg{UserID: "U9", DisplayName: "zed"})
	if got := a.threadPanel.Replies()[0].UserName; got != "zed" {
		t.Errorf("thread reply UserName = %q, want zed", got)
	}
}

func TestUserResolved_RefreshesForwardedMentions(t *testing.T) {
	a := newTestApp(t, withSize(160, 40), withActiveTeam("T1"))
	a.SetUserNames(map[string]string{"UAUTHOR": "Author"})
	parent := messages.MessageItem{
		TS: "1.0", UserID: "UAUTHOR", UserName: "Author", Timestamp: "1:00 PM",
		LegacyAttachments: []blocks.LegacyAttachment{{Text: "Parent: hi <@U123>"}},
	}
	reply := messages.MessageItem{
		TS: "2.0", UserID: "UAUTHOR", UserName: "Author", Timestamp: "1:01 PM",
		LegacyAttachments: []blocks.LegacyAttachment{{Text: "Reply: hi <@U123>"}},
	}
	a.messagepane.SetMessages([]messages.MessageItem{parent})
	a.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)

	// Render both surfaces before resolution so the event must refresh
	// already-cached attachment content, not just an author's name.
	if got := screenText(a); !strings.Contains(got, "Parent: hi @U123") {
		t.Fatalf("precondition: channel should show the unresolved forwarded mention:\n%s", got)
	}
	beforeThread := ansi.Strip(a.threadPanel.View(20, 80))
	for _, label := range []string{"Parent", "Reply"} {
		if !strings.Contains(beforeThread, label+": hi @U123") {
			t.Fatalf("precondition: thread %s should show the unresolved forwarded mention:\n%s", label, beforeThread)
		}
	}

	_, _ = a.Update(UserResolvedMsg{TeamID: "T1", UserID: "U123", DisplayName: "Alice"})

	if got := screenText(a); !strings.Contains(got, "Parent: hi @Alice") || strings.Contains(got, "hi @U123") {
		t.Errorf("channel should refresh the forwarded mention without reloading messages:\n%s", got)
	}
	afterThread := ansi.Strip(a.threadPanel.View(20, 80))
	for _, label := range []string{"Parent", "Reply"} {
		if !strings.Contains(afterThread, label+": hi @Alice") {
			t.Errorf("thread %s should refresh the forwarded mention without reloading messages:\n%s", label, afterThread)
		}
	}
	if strings.Contains(afterThread, "hi @U123") {
		t.Errorf("thread still shows an unresolved forwarded mention:\n%s", afterThread)
	}
}

// Panes apply every PatchUserName they're given, so the App must drop a
// resolution that changes nothing; names are re-resolved in bursts.
// Every pane shows U9, so each would re-render if the patch reached it:
// the Threads and Activity lists only bump for a user their cards show.
func TestUserResolved_UnchangedNameIsNoOp(t *testing.T) {
	a := newTestApp(t, withThreadsView([]cache.ThreadSummary{{
		ChannelID: "C1", ThreadTS: "1.0", ParentUserID: "U9", ParentText: "hello", ParentTS: "1.0",
	}}))
	a.activityView.SetItems([]core.ActivityItem{{
		Key: "k1", Type: "at_user", FeedTS: "1.0", ChannelID: "C1", TS: "1.0", AuthorID: "U9",
	}})
	a.SetUserNames(map[string]string{"U9": "zed"})
	resolved := unresolved("1.0")
	resolved.UserName = "zed"
	a.messagepane.SetMessages([]messages.MessageItem{resolved})
	a.threadPanel.SetThread(resolved, []messages.MessageItem{resolved}, "C1", "1.0")
	versions := func() [4]int64 {
		return [4]int64{a.messagepane.Version(), a.threadPanel.Version(), a.threadsView.Version(), a.activityView.Version()}
	}
	before := versions()

	_, _ = a.Update(UserResolvedMsg{UserID: "U9", DisplayName: "zed"})
	if after := versions(); after != before {
		t.Errorf("an unchanged name re-rendered a pane: versions [messages thread threads activity] %v -> %v", before, after)
	}
}

func TestUserResolved_ReachesEveryWindow(t *testing.T) {
	a, w1, w2 := twoWindowApp(t)
	a.SetUserNames(map[string]string{"U1": "alice"})
	a.winModels[w1].SetMessages([]messages.MessageItem{unresolved("1.0")})
	a.winModels[w2].SetMessages([]messages.MessageItem{unresolved("1.0")})

	_, _ = a.Update(UserResolvedMsg{UserID: "U9", DisplayName: "zed"})
	for _, w := range []struct {
		name string
		m    *messages.Model
	}{{"w1", a.winModels[w1]}, {"w2", a.winModels[w2]}} {
		if got := w.m.Messages()[0].UserName; got != "zed" {
			t.Errorf("%s message UserName = %q, want zed", w.name, got)
		}
	}
}
