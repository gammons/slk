package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/slack/edge"
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
	h.OnMessage("CFAKE0001", "USLACKBOT", "1.000100", "huddle", "", "huddle_thread", false, false, nil, slack.Blocks{}, nil, "", "")

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

func TestOnMessage_EditKeepsReplyCountHistoryOverwrites(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "CFAKE0001", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db: db, program: &captureSender{}, workspaceID: "T1",
		userNames: newUserNameStore(nil), wsCtx: &WorkspaceContext{},
		isActive: func() bool { return true }, activeChannelID: func() string { return "CFAKE0001" },
	}
	// A history fetch writes the real count.
	db.UpsertMessage(cache.Message{TS: "1.000100", ChannelID: "CFAKE0001", WorkspaceID: "T1", Subtype: "huddle_thread", ReplyCount: 2, RawJSON: `{}`})

	// The edit (huddle ended) must not zero it.
	h.OnMessage("CFAKE0001", "USLACKBOT", "1.000100", "huddle", "1.000100", "huddle_thread", true, false, nil, slack.Blocks{}, nil, "", "")
	if row, _ := db.GetMessage("CFAKE0001", "1.000100"); row.ReplyCount != 2 {
		t.Errorf("edit write reply_count = %d, want 2 kept", row.ReplyCount)
	}
	// A new (non-edit) message still writes 0, and history stays authoritative.
	h.OnMessage("CFAKE0001", "USLACKBOT", "1.000200", "x", "", "", false, false, nil, slack.Blocks{}, nil, "", "")
	if row, _ := db.GetMessage("CFAKE0001", "1.000200"); row.ReplyCount != 0 {
		t.Errorf("new message reply_count = %d", row.ReplyCount)
	}
	db.UpsertMessage(cache.Message{TS: "1.000100", ChannelID: "CFAKE0001", WorkspaceID: "T1", Subtype: "huddle_thread", ReplyCount: 0, RawJSON: `{}`})
	if row, _ := db.GetMessage("CFAKE0001", "1.000100"); row.ReplyCount != 0 {
		t.Errorf("history write reply_count = %d, want 0", row.ReplyCount)
	}
}

func TestResolveHuddleUsers_RequestsOnlyUnknown(t *testing.T) {
	names := newUserNameStore(nil)
	names.Set("UFAKE0001", "alice")
	room := core.ParseHuddleRoom(json.RawMessage(`{"created_by":"UFAKE0001","participants":["UFAKE0001","UFAKE0002"]}`))
	var asked []string
	got := resolveHuddleUsers(room, names, newTestDB(t), func(id string) { asked = append(asked, id) })
	if got != room {
		t.Error("room not returned")
	}
	if len(asked) != 1 || asked[0] != "UFAKE0002" {
		t.Errorf("requested %v, want only the unknown UFAKE0002", asked)
	}
	if resolveHuddleUsers(nil, names, nil, nil) != nil {
		t.Error("nil room")
	}
}

// noopBatcher makes Request queue misses instead of taking the network path.
type noopBatcher struct{}

func (noopBatcher) UsersInfo(context.Context, map[string]int64) ([]edge.User, error) {
	return nil, nil
}

// A huddle's unknown names are requested on the resolver of the workspace
// the row belongs to, even when another workspace is active.
func TestEnrichCachedRow_HuddleNamesRequestedOnOwningWorkspace(t *testing.T) {
	db := newTestDB(t)
	router := newWorkspaceRouter()
	mk := func(team string) *WorkspaceContext {
		r := newUserResolver(team, nil, db, nil, nil, noopBatcher{}, nil)
		// A running flush timer keeps Request from scheduling a network flush.
		r.flushTimer = time.NewTimer(time.Hour)
		return &WorkspaceContext{TeamID: team, UserResolver: r}
	}
	active, owner := mk("T1"), mk("T2")
	router.Add(active)
	router.Add(owner)
	router.Set(active)

	row := cache.Message{
		TS: "1.000100", ChannelID: "CFAKE0001", WorkspaceID: "T2", Subtype: "huddle_thread",
		RawJSON: `{"room":` + fakeHuddleRoom + `}`,
	}
	enrichCachedRow(db, "USELF", "CFAKE0001", row, newUserNameStore(nil), "3:04 PM", "test", router, &enrichPerfStats{})

	if _, ok := owner.UserResolver.pending["UFAKE0001"]; !ok {
		t.Error("starter not requested on the owning workspace T2")
	}
	if _, ok := active.UserResolver.pending["UFAKE0001"]; ok {
		t.Error("starter requested on the active workspace T1")
	}
}

func TestRoomFromRawJSON_Malformed(t *testing.T) {
	for _, raw := range []string{"", "{bad", `{"room":"x"}`, `{"text":"a"}`} {
		if r := roomFromRawJSON(raw); r != nil {
			t.Errorf("%q: got %+v", raw, r)
		}
	}
}
