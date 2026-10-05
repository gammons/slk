package main

import (
	"sync"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/slack-go/slack"
)

func TestWorkspaceBotClassificationZeroValue(t *testing.T) {
	var absent *WorkspaceContext
	absent.MarkBotUser("U1")
	if absent.IsBotUser("U1") {
		t.Fatal("nil workspace reported a bot")
	}
	var w WorkspaceContext
	w.MarkBotUser("")
	if w.IsBotUser("") || w.IsBotUser("U1") {
		t.Fatal("unknown/empty peer reported a bot")
	}
	w.MarkBotUser("U1")
	w.MarkBotUser("U1")
	if !w.IsBotUser("U1") {
		t.Fatal("positive classification was not retained")
	}
}

// Exercise the existing DM sweep against ordinary conversation construction,
// without relying on starred membership or its reconnect hydration path.
func TestWorkspaceBotClassificationConcurrentDMSweep(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Bot", IsBot: true}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{TeamID: "T1", UserNames: newUserNameStore(map[string]string{"U1": "Bot"}), UnresolvedDMs: []UnresolvedDM{{ChannelID: "D1", UserID: "U1"}}}
	wctx.UserResolver = newUserResolver("T1", nil, db, nil, nil, &fakeBatcher{res: []edge.User{edgeUserRecord("U1", "bot", "Bot", "", "T1", 1, true)}}, nil)
	wctx.UserResolver.names = wctx.UserNames
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 64; i++ {
			resolveDMNames(wctx, db, nil, nil)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 64; i++ {
			h.addConversation(ch)
		}
	}()
	close(start)
	wg.Wait()
	item, _, ok := h.addConversation(ch)
	if !ok || item.Type != "app" || !wctx.IsBotUser("U1") {
		t.Fatalf("concurrent sweep lost classification: %+v", item)
	}
}
