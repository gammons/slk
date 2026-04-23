package service

import (
	"testing"

	"github.com/gammons/slack-tui/internal/cache"
)

func setupTestDB(t *testing.T) *cache.DB {
	t.Helper()
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "Test", Domain: "test"})
	db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	return db
}

func TestMessageServiceGetCachedMessages(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	db.UpsertMessage(cache.Message{TS: "1.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "hello"})
	db.UpsertMessage(cache.Message{TS: "2.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "world"})

	svc := NewMessageService(db)
	msgs, err := svc.GetMessages("C1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestMessageServiceCacheMessage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewMessageService(db)
	svc.CacheMessage(cache.Message{TS: "1.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "new msg"})

	msgs, _ := svc.GetMessages("C1", 50)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Text != "new msg" {
		t.Errorf("expected 'new msg', got %q", msgs[0].Text)
	}
}
