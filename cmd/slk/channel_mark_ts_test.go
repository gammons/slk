package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
)

// TestChannelMarkTS pins the fetch outcomes that decide whether a
// channel is marked read, and at which timestamp.
func TestChannelMarkTS(t *testing.T) {
	// Slack's `latest` for a dormant channel: real, long past, and
	// naming a message conversations.history will not return.
	const slackLatest = "1776079647.792389"
	latest := func() string { return slackLatest }

	tests := []struct {
		name   string
		items  []messages.MessageItem
		state  cache.ReadState
		lookup func() string
		want   string
	}{
		{
			name:   "fetch failed: never mark, even when flagged unread",
			items:  nil,
			state:  cache.ReadState{HasUnread: true},
			lookup: latest,
			want:   "",
		},
		{
			name:   "empty and already read: nothing to do",
			items:  []messages.MessageItem{},
			state:  cache.ReadState{HasUnread: false},
			lookup: latest,
			want:   "",
		},
		{
			// The dormant / retention-limited channel. Marking at
			// Slack's own `latest` clears the unread without
			// fabricating recency the channel never had.
			name:   "empty but flagged unread: mark at Slack's latest",
			items:  []messages.MessageItem{},
			state:  cache.ReadState{LastReadTS: "1718264828.618929", HasUnread: true},
			lookup: latest,
			want:   slackLatest,
		},
		{
			name:   "empty, unread, but latest unknown: do not guess",
			items:  []messages.MessageItem{},
			state:  cache.ReadState{HasUnread: true},
			lookup: func() string { return "" },
			want:   "",
		},
		{
			name:   "empty, unread, no lookup wired: do not mark",
			items:  []messages.MessageItem{},
			state:  cache.ReadState{HasUnread: true},
			lookup: nil,
			want:   "",
		},
		{
			name:   "has messages: mark at the newest one",
			items:  []messages.MessageItem{{TS: "100.000001"}, {TS: "200.000002"}},
			state:  cache.ReadState{HasUnread: true},
			lookup: latest,
			want:   "200.000002",
		},
		{
			name:   "has messages while already read: still marks newest",
			items:  []messages.MessageItem{{TS: "300.000003"}},
			state:  cache.ReadState{HasUnread: false},
			lookup: latest,
			want:   "300.000003",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelMarkTS(tt.items, tt.state, tt.lookup); got != tt.want {
				t.Errorf("channelMarkTS() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestChannelMarkTSNoLookupWhenMessagesPresent guards the cost side:
// the `latest` lookup is a network call, and it must not fire on the
// ordinary path where the newest fetched message already answers the
// question.
func TestChannelMarkTSNoLookupWhenMessagesPresent(t *testing.T) {
	called := false
	items := []messages.MessageItem{{TS: "400.000004"}}
	got := channelMarkTS(items, cache.ReadState{HasUnread: true}, func() string {
		called = true
		return "999.000000"
	})
	if called {
		t.Error("looked up Slack's latest even though the fetch returned messages")
	}
	if got != "400.000004" {
		t.Errorf("got %q, want the newest fetched message", got)
	}
}

// TestChannelMarkTSNoLookupWhenAlreadyRead is the same guard for a
// channel that is empty but carries no unread flag.
func TestChannelMarkTSNoLookupWhenAlreadyRead(t *testing.T) {
	called := false
	channelMarkTS([]messages.MessageItem{}, cache.ReadState{HasUnread: false}, func() string {
		called = true
		return "999.000000"
	})
	if called {
		t.Error("looked up Slack's latest for a channel with nothing unread")
	}
}

// TestChannelMarkTSStaysStale is the regression guard for the bug this
// helper's second revision fixed. Marking a dormant channel at the wall
// clock cleared the dot but made the channel look freshly read, and the
// sidebar's staleness filter (hide_inactive_after_days) then pinned it
// to the sidebar for the whole threshold window. Marking at Slack's
// real `latest` keeps the channel as old as it actually is.
func TestChannelMarkTSStaysStale(t *testing.T) {
	const slackLatest = "1776079647.792389" // ~127 days before the `now` below
	const now = "1787053312.000000"

	got := channelMarkTS(
		[]messages.MessageItem{},
		cache.ReadState{LastReadTS: "1718264828.618929", HasUnread: true},
		func() string { return slackLatest },
	)
	if got == now {
		t.Fatal("marked at the wall clock; that fabricates recency and defeats the staleness filter")
	}
	if got != slackLatest {
		t.Fatalf("got %q, want Slack's latest %q", got, slackLatest)
	}
	// It must still be newer than the stale last_read, or
	// conversations.mark is a silent no-op and the dot survives.
	if got <= "1718264828.618929" {
		t.Fatalf("mark ts %q must be newer than last_read", got)
	}
}
