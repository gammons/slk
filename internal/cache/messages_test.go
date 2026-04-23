package cache

import (
	"fmt"
	"testing"
)

func TestUpsertAndGetMessages(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	msgs := []Message{
		{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "Hello"},
		{TS: "1700000002.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "World"},
		{TS: "1700000003.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "!"},
	}

	for _, m := range msgs {
		if err := db.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.GetMessages("C1", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 messages, got %d", len(got))
	}
	// Should be ordered by ts ascending
	if got[0].Text != "Hello" {
		t.Errorf("expected first message 'Hello', got %q", got[0].Text)
	}
}

func TestGetMessagesWithCursor(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	for i := 0; i < 5; i++ {
		db.UpsertMessage(Message{
			TS:          fmt.Sprintf("170000000%d.000000", i),
			ChannelID:   "C1",
			WorkspaceID: "T1",
			UserID:      "U1",
			Text:        fmt.Sprintf("msg %d", i),
		})
	}

	// Get only messages before ts 1700000003
	got, err := db.GetMessages("C1", 10, "1700000003.000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 messages before cursor, got %d", len(got))
	}
}

func TestGetThreadReplies(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	// Parent message
	db.UpsertMessage(Message{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "parent"})
	// Thread replies
	db.UpsertMessage(Message{TS: "1700000002.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "reply 1", ThreadTS: "1700000001.000000"})
	db.UpsertMessage(Message{TS: "1700000003.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "reply 2", ThreadTS: "1700000001.000000"})

	replies, err := db.GetThreadReplies("C1", "1700000001.000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Errorf("expected 2 replies, got %d", len(replies))
	}
}
