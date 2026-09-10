package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/slack-go/slack"
)

// onMessageMentionFixture builds the minimal handler OnMessage needs to
// reach its read-state block: a db, a channel-type map, the self user ID,
// and an active-channel getter. notifier is nil so the notification
// branch is skipped, and program is nil so no tea.Program is required.
//
// The cached row and the channelTypes map are given the SAME type on
// purpose. Production reads the map, so only the map decides behaviour,
// but a fixture whose row says "channel" while its map says "dm" is a
// self-contradiction that hides type-dispatch bugs from anyone reading
// the test.
func onMessageMentionFixture(t *testing.T, chType, activeChannelID string) (*rtmEventHandler, *cache.DB) {
	t.Helper()
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: chType}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	h := &rtmEventHandler{
		db:              db,
		workspaceID:     "T1",
		currentUserID:   "USELF",
		channelTypes:    map[string]string{"C1": chType},
		userNames:       map[string]string{},
		channelNames:    map[string]string{"C1": "general"},
		isActive:        func() bool { return false }, // stop before the UI dispatch
		activeChannelID: func() string { return activeChannelID },
	}
	return h, db
}

func mentionCount(t *testing.T, db *cache.DB, channelID string) int {
	t.Helper()
	state, err := db.GetChannelReadState(channelID)
	if err != nil {
		t.Fatalf("GetChannelReadState(%s): %v", channelID, err)
	}
	return state.MentionCount
}

func TestOnMessage_MentionIncrementsCount(t *testing.T) {
	tests := []struct {
		name     string
		chType   string
		author   string
		text     string
		threadTS string
		subtype  string
		active   string
		want     int
	}{
		{
			name:   "explicit self mention in channel",
			chType: "channel", author: "UOTHER",
			text: "hey <@USELF> ship it", want: 1,
		},
		{
			name:   "here broadcast in channel",
			chType: "channel", author: "UOTHER",
			text: "<!here> standup", want: 1,
		},
		{
			name:   "plain channel message does not count",
			chType: "channel", author: "UOTHER",
			text: "unrelated chatter", want: 0,
		},
		{
			// Slack badges every DM message, and client.counts reports
			// them in mention_count, so local detection must agree.
			name:   "any dm message counts",
			chType: "dm", author: "UOTHER",
			text: "no markup here", want: 1,
		},
		{
			name:   "any group dm message counts",
			chType: "group_dm", author: "UOTHER",
			text: "no markup here", want: 1,
		},
		{
			// "app" is slk's own label for a DM whose peer is a bot; it
			// is not a Slack conversation kind. Slack reports app DMs in
			// the same `ims` block as human DMs, so they get identical
			// all-unread semantics and must increment on plain text.
			name:   "any app dm message counts",
			chType: "app", author: "UBOT",
			text: "deploy finished", want: 1,
		},
		{
			name:   "self-authored mention does not count",
			chType: "channel", author: "USELF",
			text: "note to <@USELF>", want: 0,
		},
		{
			// Mirrors the has_unread gate: a non-broadcast thread reply
			// does not touch the parent channel. Thread mentions surface
			// in the Threads section instead.
			name:   "plain thread reply does not count",
			chType: "channel", author: "UOTHER",
			text: "<@USELF> in a thread", threadTS: "1.0000", want: 0,
		},
		{
			name:   "thread broadcast counts",
			chType: "channel", author: "UOTHER",
			text: "<@USELF> broadcasting", threadTS: "1.0000", subtype: "thread_broadcast", want: 1,
		},
		{
			// The active channel no longer suppresses the write. Focus
			// reporting replaced that gate in #159: "this channel is
			// selected" never meant "the user can see it" — slk may be
			// in a background terminal or an unfocused tmux pane. The
			// badge follows has_unread here rather than re-deriving a
			// visibility rule of its own, and markChannelRead clears
			// both together once slk actually marks the channel read.
			name:   "mention in the active channel still counts",
			chType: "channel", author: "UOTHER",
			text: "<@USELF> hi", active: "C1", want: 1,
		},
		{
			name:   "usergroup mention is not detected",
			chType: "channel", author: "UOTHER",
			text: "<!subteam^S1|@eng> ship it", want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, db := onMessageMentionFixture(t, tt.chType, tt.active)
			h.OnMessage("C1", tt.author, "1.0001", tt.text, tt.threadTS, tt.subtype,
				false, nil, slack.Blocks{}, nil, "", "")
			if got := mentionCount(t, db, "C1"); got != tt.want {
				t.Errorf("MentionCount = %d, want %d", got, tt.want)
			}
		})
	}
}

// Two mentions arriving before the channel is read must both count: the
// second increment adds to the first instead of overwriting it. This pins
// additivity only. It says nothing about atomicity — two sequential calls
// yield 2 under a read-modify-write implementation just as readily as
// under the SQL increment the cache actually uses.
func TestOnMessage_MentionsAccumulate(t *testing.T) {
	h, db := onMessageMentionFixture(t, "channel", "")
	h.OnMessage("C1", "UOTHER", "1.0001", "<@USELF> first", "", "", false, nil, slack.Blocks{}, nil, "", "")
	h.OnMessage("C1", "UOTHER", "1.0002", "<!channel> second", "", "", false, nil, slack.Blocks{}, nil, "", "")
	if got := mentionCount(t, db, "C1"); got != 2 {
		t.Errorf("MentionCount = %d, want 2", got)
	}
}
