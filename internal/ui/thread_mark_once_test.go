package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// Opening a thread renders the cached replies at once and fetches the
// real ones. Both used to mark the thread read, concurrently, the first
// at the cached newest reply. When the cache lagged Slack that stale
// mark flipped the unread dot back on, and whichever mark's HTTP call
// finished last won the persisted cursor -- the dot flashed on and off,
// and could stick on. Only the fetched replies may mark.

const (
	markOnceParent = "1700000000.000100"
	markOnceStale  = "1700000010.000000" // newest reply the cache knows
	markOnceFresh  = "1700000020.000000" // newest reply Slack returns
)

// markOnceApp builds an App whose thread service serves a cache that
// lags Slack by one reply, records every Mark, and fetches with fetch.
func markOnceApp(t *testing.T, fetch func() []messages.MessageItem) (*App, *[]string) {
	t.Helper()
	parent := messages.MessageItem{TS: markOnceParent, UserID: "U1", Text: "parent"}
	stale := messages.MessageItem{TS: markOnceStale, UserID: "U2", ThreadTS: markOnceParent}
	app := newTestApp(t,
		withThreadsView([]cache.ThreadSummary{{
			ChannelID: "C1", ThreadTS: markOnceParent, ParentTS: markOnceParent,
			ParentUserID: "U1", ParentText: "parent", LastReplyTS: markOnceFresh, Unread: true,
		}}),
		withView(ViewThreads),
	)
	var marks []string
	app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{parent, stale}
		},
		Fetch: func(_ ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
			return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: fetch()}
		},
		Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) core.Cmd {
			marks = append(marks, fmt.Sprintf("%s/%s/%s", channelID, threadTS, ts))
			return nil
		},
	}))
	return app, &marks
}

func markOnceUnread(a *App) bool {
	for _, s := range a.threadsView.Summaries() {
		if s.ThreadTS == markOnceParent {
			return s.Unread
		}
	}
	return false
}

// feedThreadLoads runs cmd and delivers its results to the App in order
// (cache before fetch, as in production), failing if a cached load
// changes the thread's unread flag.
func feedThreadLoads(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	for _, m := range drainBatch(cmd) {
		loaded, ok := m.(ThreadRepliesLoadedMsg)
		if !ok {
			continue
		}
		before := markOnceUnread(a)
		a.Update(loaded)
		if loaded.FromCache && markOnceUnread(a) != before {
			t.Errorf("the cached load flipped the unread flag %v -> %v; only the fetched replies may settle it",
				before, markOnceUnread(a))
		}
	}
}

func TestThreadOpen_MarksOnceAtFetchedNewestReply(t *testing.T) {
	fresh := func() []messages.MessageItem {
		return []messages.MessageItem{
			{TS: markOnceStale, UserID: "U2", ThreadTS: markOnceParent},
			{TS: markOnceFresh, UserID: "U3", ThreadTS: markOnceParent},
		}
	}
	opens := []struct {
		name string
		open func(a *App) tea.Cmd
	}{
		{"threads view enter", func(a *App) tea.Cmd {
			a.focusedPanel = PanelMessages
			_, cmd := a.Update(keyCode(tea.KeyEnter))
			return cmd
		}},
		{"messages pane open", func(a *App) tea.Cmd {
			return a.openThreadPanel(messages.MessageItem{TS: markOnceParent}, "C1", markOnceParent)
		}},
		{"j/k debounce settling", func(a *App) tea.Cmd {
			a.threadVisible = true
			a.threadPanel.SetThread(messages.MessageItem{TS: markOnceParent}, nil, "C1", markOnceParent)
			a.lastOpenedChannelID, a.lastOpenedThreadTS = "C1", markOnceParent
			_, cmd := a.Update(threadFetchDebounceMsg{gen: a.pendingThreadFetchGen, channelID: "C1", threadTS: markOnceParent})
			return cmd
		}},
	}
	for _, tc := range opens {
		t.Run(tc.name, func(t *testing.T) {
			app, marks := markOnceApp(t, fresh)
			feedThreadLoads(t, app, tc.open(app))
			want := []string{"C1/" + markOnceParent + "/" + markOnceFresh}
			if fmt.Sprint(*marks) != fmt.Sprint(want) {
				t.Errorf("Mark calls = %v, want exactly %v", *marks, want)
			}
			if markOnceUnread(app) {
				t.Error("thread still unread after the fetched replies marked it read")
			}
		})
	}
}

// With no fetched replies to mark from, the thread is still marked read
// at the newest reply the user can see: the cached one.
func TestThreadOpen_FetchFailureMarksAtCachedNewestReply(t *testing.T) {
	app, marks := markOnceApp(t, func() []messages.MessageItem { return nil })
	app.focusedPanel = PanelMessages
	_, cmd := app.Update(keyCode(tea.KeyEnter))
	feedThreadLoads(t, app, cmd)

	want := []string{"C1/" + markOnceParent + "/" + markOnceStale}
	if fmt.Sprint(*marks) != fmt.Sprint(want) {
		t.Errorf("Mark calls = %v, want exactly %v", *marks, want)
	}
}
