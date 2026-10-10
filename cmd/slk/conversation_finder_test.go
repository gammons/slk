package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestConversationOpenRefreshesFinderWithoutLosingVisits(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate finder", true: "missing finder"}[missing], func(t *testing.T) {
			db := newTestDB(t)
			if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Alice", Presence: "active"}); err != nil {
				t.Fatal(err)
			}
			wctx := &WorkspaceContext{
				UserNames:            newUserNameStore(map[string]string{"U1": "Alice"}),
				Channels:             []sidebar.ChannelItem{{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"}},
				LastVisitedByChannel: map[string]int64{"D1": 10},
			}
			if !missing {
				wctx.FinderItems = []core.ChannelFinderItem{{ID: "D1", Name: "U1", Type: "dm", Joined: true, LastVisited: 99}}
			}
			sender := &captureSender{}
			h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: sender}
			ch := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}
			h.OnConversationOpened(ch)
			wantVisit := int64(99)
			if missing {
				wantVisit = 10
			}
			if len(wctx.FinderItems) != 1 || len(wctx.Channels) != 1 {
				t.Fatalf("open duplicated/lost snapshots: %+v / %+v", wctx.Channels, wctx.FinderItems)
			}
			finder := wctx.FinderItems[0]
			if finder.Name != "Alice" || finder.Type != "dm" || finder.Presence != "active" || finder.LastVisited != wantVisit {
				t.Fatalf("open left stale finder or visits: %+v", finder)
			}
			if len(sender.sent) != 1 || sender.sent[0].(ui.ConversationOpenedMsg).FinderItem != finder {
				t.Fatalf("UI did not receive stored finder metadata: %+v", sender.sent)
			}
		})
	}
}
