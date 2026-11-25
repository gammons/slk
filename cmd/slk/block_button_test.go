package main

import (
	"context"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core/blocks"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// A live bot message keeps its bot_id, on the item and in the cache row
// the item is later rebuilt from: a press sends it as service_id.
func TestOnMessage_BotIDReachesItemAndCache(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "CFAKE0001", WorkspaceID: "T1", Name: "general", Type: "channel"})
	sender := &captureSender{}
	h := &rtmEventHandler{
		db:              db,
		program:         sender,
		workspaceID:     "T1",
		userNames:       newUserNameStore(nil),
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "CFAKE0001" },
	}
	h.OnMessage("CFAKE0001", "UFAKEBOT1", "1.000100", "card", "", "", false, false, nil, slack.Blocks{}, nil, "BFAKE0001", "")

	var item *ui.NewMessageMsg
	for _, m := range sender.sent {
		if nm, ok := m.(ui.NewMessageMsg); ok {
			item = &nm
		}
	}
	if item == nil || item.Message.BotID != "BFAKE0001" {
		t.Fatalf("item BotID not set: %+v", item)
	}
	row, err := db.GetMessage("CFAKE0001", "1.000100")
	if err != nil {
		t.Fatal(err)
	}
	cached := enrichCachedRow(db, "USELF", "CFAKE0001", row, newUserNameStore(nil), "", "test", nil, nil)
	if cached.BotID != "BFAKE0001" {
		t.Errorf("cached item BotID = %q, raw_json %s", cached.BotID, row.RawJSON)
	}
}

type fakeBlockActioner struct {
	got slackclient.BlockActionRequest
}

func (f *fakeBlockActioner) BlockAction(_ context.Context, req slackclient.BlockActionRequest) error {
	f.got = req
	return nil
}

func TestPressBlockAction_BuildsMessageRequest(t *testing.T) {
	f := &fakeBlockActioner{}
	act := blocks.ActionElement{Kind: "button", Label: "Verify", ActionID: "verify",
		Value: "TASK-1", Style: "primary", Confirm: &blocks.ActionConfirm{Title: "Sure?", OKText: "Yes"}}
	if err := pressBlockAction(context.Background(), f, "C1", "1.000100", "B1", "b1", act); err != nil {
		t.Fatal(err)
	}
	g := f.got
	if g.ViewID != "" || g.ChannelID != "C1" || g.MessageTS != "1.000100" || g.ServiceID != "B1" ||
		g.ActionID != "verify" || g.BlockID != "b1" || g.Label != "Verify" || g.Value != "TASK-1" || g.Style != "primary" {
		t.Errorf("request = %+v", g)
	}
	if g.Confirm == nil || g.Confirm.Title != "Sure?" || g.Confirm.OKText != "Yes" {
		t.Errorf("confirm = %+v", g.Confirm)
	}
}
