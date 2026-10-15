package ui

import (
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func fakeRoom() *messages.HuddleRoom {
	return &messages.HuddleRoom{
		CreatedBy:    "UFAKE0001",
		DateStart:    1700000000,
		Participants: []string{"UFAKE0001", "UFAKE0002"},
		Raw:          []byte(`{"created_by":"UFAKE0001"}`),
	}
}

func huddleItem(ts string, room *messages.HuddleRoom) messages.MessageItem {
	return messages.MessageItem{
		TS: ts, UserID: "UFAKE0001", UserName: "fake", Text: "huddle",
		Timestamp: "1:00 PM", Subtype: "huddle_thread", HuddleRoom: room,
	}
}

func roomOf(t *testing.T, a *App, ts string) *messages.HuddleRoom {
	t.Helper()
	for _, m := range a.messagepane.Messages() {
		if m.TS == ts {
			return m.HuddleRoom
		}
	}
	t.Fatalf("message %s not in pane", ts)
	return nil
}

func TestHuddleRoom_SurvivesLoadLiveEditAndReaction(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})

	// Initial / cached / paged loads all arrive as MessagesLoadedMsg.
	_, _ = a.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: []messages.MessageItem{
		huddleItem("1.0", fakeRoom()),
		{TS: "2.0", UserID: "UFAKE0002", Text: "plain", Timestamp: "1:01 PM"},
	}})
	if r := roomOf(t, a, "1.0"); r == nil || r.CreatedBy != "UFAKE0001" {
		t.Fatalf("room lost on load: %+v", r)
	}
	if r := roomOf(t, a, "2.0"); r != nil {
		t.Fatalf("plain message gained a room: %+v", r)
	}

	// Live message.
	_, _ = a.Update(NewMessageMsg{ChannelID: "C1", Message: huddleItem("3.0", fakeRoom())})
	if r := roomOf(t, a, "3.0"); r == nil || len(r.Participants) != 2 {
		t.Fatalf("room lost on live message: %+v", r)
	}

	// An unrelated update (reaction) keeps it.
	a.messagepane.UpdateReaction("3.0", "tada", "UFAKE0002", false)
	if r := roomOf(t, a, "3.0"); r == nil {
		t.Fatal("room lost on reaction")
	}

	// A room-less edit keeps the room; the text changes.
	edit := huddleItem("3.0", nil)
	edit.Text = "edited"
	edit.IsEdited = true
	_, _ = a.Update(NewMessageMsg{ChannelID: "C1", Message: edit})
	if r := roomOf(t, a, "3.0"); r == nil || r.CreatedBy != "UFAKE0001" {
		t.Fatalf("room lost on room-less edit: %+v", r)
	}

	// An edit carrying a newer room (the huddle ended) replaces it.
	ended := fakeRoom()
	ended.HasEnded, ended.DateEnd = true, 1700000600
	edit.HuddleRoom = ended
	_, _ = a.Update(NewMessageMsg{ChannelID: "C1", Message: edit})
	r := roomOf(t, a, "3.0")
	if r == nil || !r.HasEnded || r.DateEnd != 1700000600 {
		t.Fatalf("newer room not applied: %+v", r)
	}
	if r == ended {
		t.Error("model shares the sender's room pointer")
	}
}

func TestHuddleRoom_ThreadRepliesLoad(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	parent := huddleItem("1.0", fakeRoom())
	parent.ThreadTS = "1.0"
	a.threadVisible = true
	a.threadPanel.SetThread(parent, nil, "C1", "1.0")

	reply := huddleItem("1.5", fakeRoom())
	reply.ThreadTS = "1.0"
	plain := messages.MessageItem{TS: "1.6", ThreadTS: "1.0", Text: "x", Timestamp: "1:02 PM"}
	_, _ = a.Update(ThreadRepliesLoadedMsg{ThreadTS: "1.0", Replies: []messages.MessageItem{reply, plain}})

	if r := a.threadPanel.ParentMsg().HuddleRoom; r == nil || r.CreatedBy != "UFAKE0001" {
		t.Fatalf("parent room lost: %+v", r)
	}
	got := a.threadPanel.Replies()
	if len(got) != 2 || got[0].HuddleRoom == nil || got[1].HuddleRoom != nil {
		t.Fatalf("reply rooms wrong: %+v", got)
	}

	// An edit that carries a newer room reaches the open thread too.
	ended := fakeRoom()
	ended.HasEnded, ended.DateEnd = true, 1700000600
	edit := huddleItem("1.0", ended)
	edit.IsEdited = true
	_, _ = a.Update(NewMessageMsg{ChannelID: "C1", Message: edit})
	if r := a.threadPanel.ParentMsg().HuddleRoom; r == nil || !r.HasEnded {
		t.Fatalf("thread parent room not updated: %+v", r)
	}
}

func TestCloneMessageItem_CopiesHuddleRoom(t *testing.T) {
	src := huddleItem("1.0", fakeRoom())
	c := cloneMessageItem(src)
	if c.HuddleRoom == src.HuddleRoom {
		t.Fatal("clone shares the room pointer")
	}
	c.HuddleRoom.Participants[0] = "UFAKE0009"
	c.HuddleRoom.Raw[0] = 'X'
	if src.HuddleRoom.Participants[0] != "UFAKE0001" || src.HuddleRoom.Raw[0] != '{' {
		t.Fatal("clone aliases Participants or Raw")
	}
	if cloneMessageItem(messages.MessageItem{TS: "1.0"}).HuddleRoom != nil {
		t.Fatal("nil room became non-nil")
	}
}

// A thread opened from a stub parent (permalink, first open) is backfilled
// from the cache even when the huddle parent has no text.
func TestHuddleThread_StubParentBackfillsFromCache(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	cachedParent := huddleItem("1.0", fakeRoom())
	cachedParent.UserID, cachedParent.Text = "USLACKBOT", ""
	cachedParent.ThreadTS = "1.0"
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{cachedParent}
		},
	}))
	a.threadVisible = true
	a.threadPanel.SetThread(messages.MessageItem{TS: "1.0", ThreadTS: "1.0"}, nil, "C1", "1.0")

	reply := messages.MessageItem{TS: "1.5", ThreadTS: "1.0", UserID: "UFAKE0002", Text: "note", Timestamp: "1:02 PM"}
	_, _ = a.Update(ThreadRepliesLoadedMsg{ThreadTS: "1.0", Replies: []messages.MessageItem{reply}})

	p := a.threadPanel.ParentMsg()
	if p.Subtype != "huddle_thread" || p.UserID != "USLACKBOT" || p.HuddleRoom == nil || p.HuddleRoom.CreatedBy != "UFAKE0001" {
		t.Errorf("parent not backfilled from cache: %+v", p)
	}
}
