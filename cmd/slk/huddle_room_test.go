package main

import (
	"encoding/json"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

const fakeHuddleRoom = `{"created_by":"UFAKE0001","date_start":1700000000,"date_end":0,"has_ended":false}`

func TestOnMessage_HuddleRoomReachesCacheAndItem(t *testing.T) {
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
	h.OnMessageRoom("CFAKE0001", "1.000100", json.RawMessage(fakeHuddleRoom))
	h.OnMessage("CFAKE0001", "USLACKBOT", "1.000100", "huddle", "", "huddle_thread", false, nil, slack.Blocks{}, nil, "", "")

	var item *ui.NewMessageMsg
	for _, m := range sender.sent {
		if nm, ok := m.(ui.NewMessageMsg); ok {
			item = &nm
		}
	}
	if item == nil || item.Message.HuddleRoom == nil || item.Message.HuddleRoom.CreatedBy != "UFAKE0001" {
		t.Fatalf("item room not set: %+v", item)
	}
	row, err := db.GetMessage("CFAKE0001", "1.000100")
	if err != nil {
		t.Fatal(err)
	}
	if r := roomFromRawJSON(row.RawJSON); r == nil || r.CreatedBy != "UFAKE0001" {
		t.Errorf("cached raw_json has no room: %s", row.RawJSON)
	}
	if len(h.pendingRooms) != 0 {
		t.Error("pending room not consumed")
	}

	// A later typed (room-less) re-cache of the same message keeps the room.
	db.UpsertMessage(cache.Message{TS: "1.000100", ChannelID: "CFAKE0001", WorkspaceID: "T1", Subtype: "huddle_thread", RawJSON: `{"ts":"1.000100","subtype":"huddle_thread"}`})
	row, _ = db.GetMessage("CFAKE0001", "1.000100")
	if r := roomFromRawJSON(row.RawJSON); r == nil || r.CreatedBy != "UFAKE0001" {
		t.Errorf("room lost on typed re-cache: %s", row.RawJSON)
	}
}

func TestMarshalWithRoom_NilClientAndNonHuddle(t *testing.T) {
	raw, room := marshalWithRoom(nil, "CFAKE0001", slack.Message{Msg: slack.Msg{Timestamp: "1.1"}})
	if room != nil || len(raw) == 0 {
		t.Errorf("non-huddle: raw=%s room=%v", raw, room)
	}
	raw, room = marshalWithRoom(nil, "CFAKE0001", slack.Message{Msg: slack.Msg{Timestamp: "1.1", SubType: "huddle_thread"}})
	if room != nil || len(raw) == 0 {
		t.Errorf("huddle without capture: raw=%s room=%v", raw, room)
	}
}

func TestRoomFromRawJSON_Malformed(t *testing.T) {
	for _, raw := range []string{"", "{bad", `{"room":"x"}`, `{"text":"a"}`} {
		if r := roomFromRawJSON(raw); r != nil {
			t.Errorf("%q: got %+v", raw, r)
		}
	}
}
