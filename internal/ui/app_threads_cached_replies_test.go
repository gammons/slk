package ui

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// threadsCalls counts the network-facing ThreadService calls a j/k
// press must not make before the debounce fires.
type threadsCalls struct{ fetch, mark int }

// cachedThreadsApp is a Threads-view App whose thread cache holds a
// parent plus two replies for every thread ("<ts>1" and "<ts>2" after the
// parent's ts), and whose thread last-read cursor is the first reply.
func cachedThreadsApp(t *testing.T, cachedReplies bool) (*App, *threadsCalls) {
	t.Helper()
	a := newTestAppWithThreadsView(t, []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", ParentTS: "1.0", ParentText: "p1", ChannelName: "g1"},
		{ChannelID: "C2", ThreadTS: "2.0", ParentTS: "2.0", ParentText: "p2", ChannelName: "g2"},
		{ChannelID: "C3", ThreadTS: "3.0", ParentTS: "3.0", ParentText: "p3", ChannelName: "g3"},
	})
	calls := &threadsCalls{}
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(_ ids.ChannelID, threadTS ids.ThreadTS) []core.MessageItem {
			ts := string(threadTS)
			out := []core.MessageItem{{TS: ts, Text: "parent " + ts}}
			if cachedReplies {
				out = append(out,
					core.MessageItem{TS: ts + "1", ThreadTS: ts, Text: "reply 1"},
					core.MessageItem{TS: ts + "2", ThreadTS: ts, Text: "reply 2"},
				)
			}
			return out
		},
		Fetch: func(_ ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
			calls.fetch++
			return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS)}
		},
		Mark: func(ids.ChannelID, ids.ThreadTS, ids.MessageTS) core.Cmd {
			calls.mark++
			return nil
		},
		ThreadLastRead: func(_ ids.ChannelID, threadTS ids.ThreadTS) string {
			return string(threadTS) + "1"
		},
	}))
	return a, calls
}

func replyTSs(replies []messages.MessageItem) []string {
	out := make([]string, len(replies))
	for i, r := range replies {
		out[i] = r.TS
	}
	return out
}

// j/k must show a thread's cached replies with its parent, in the same
// update, rather than 200ms later when the debounced fetch fires. The
// fetch, and the mark-read it leads to, still wait for the debounce.
func TestThreadsViewJK_ShowsCachedRepliesImmediately(t *testing.T) {
	a, calls := cachedThreadsApp(t, true)
	a.threadsView.MoveDown()
	if cmd := a.openSelectedThreadCmd(true); cmd == nil {
		t.Fatal("openSelectedThreadCmd(true) scheduled nothing")
	}

	if got := replyTSs(a.threadPanel.Replies()); len(got) != 2 || got[0] != "2.01" || got[1] != "2.02" {
		t.Errorf("thread panel replies = %v, want the cached [2.01 2.02]", got)
	}
	if got := a.threadPanel.ParentMsg().Text; got != "p2" {
		t.Errorf("parent text = %q, want the summary's p2", got)
	}
	if calls.fetch != 0 || calls.mark != 0 {
		t.Errorf("before the debounce: fetch=%d mark=%d, want 0 and 0", calls.fetch, calls.mark)
	}
}

// The "── new ──" landmark is positioned from the thread's own cursor;
// showing cached replies early must not lose it.
func TestThreadsViewJK_CachedRepliesKeepUnreadBoundary(t *testing.T) {
	a, _ := cachedThreadsApp(t, true)
	a.threadsView.MoveDown()
	_ = a.openSelectedThreadCmd(true)
	if got := a.threadPanel.UnreadBoundaryTS(); got != "2.01" {
		t.Errorf("UnreadBoundaryTS = %q, want 2.01", got)
	}
}

// Holding j shows each thread's cached replies as the cursor passes, and
// still makes no network call and marks nothing read until the user
// stops (TestThreadsViewDebouncesNetworkFetchOnRapidJK covers the single
// fetch after the stop).
func TestThreadsViewJK_HeldKeyShowsEachThreadWithoutFetching(t *testing.T) {
	a, calls := cachedThreadsApp(t, true)
	for _, want := range []string{"1.01", "2.01", "3.01"} {
		_ = a.openSelectedThreadCmd(true)
		if got := replyTSs(a.threadPanel.Replies()); len(got) == 0 || got[0] != want {
			t.Errorf("thread panel replies = %v, want first reply %s", got, want)
		}
		a.threadsView.MoveDown()
	}
	if calls.fetch != 0 || calls.mark != 0 {
		t.Errorf("during held j: fetch=%d mark=%d, want 0 and 0", calls.fetch, calls.mark)
	}
}

// A thread with only its parent cached opens reply-less, as before.
func TestThreadsViewJK_NoCachedRepliesShowsParentOnly(t *testing.T) {
	a, _ := cachedThreadsApp(t, false)
	_ = a.openSelectedThreadCmd(true)
	if got := a.threadPanel.Replies(); len(got) != 0 {
		t.Errorf("thread panel replies = %v, want none", replyTSs(got))
	}
	if got := a.threadPanel.ParentMsg().Text; got != "p1" {
		t.Errorf("parent text = %q, want p1", got)
	}
}
