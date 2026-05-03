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

// TestSubtypeRoundTrip verifies that the Subtype field is persisted
// through UpsertMessage and read back via GetMessages, and that
// thread_broadcast messages appear in the main channel feed even
// though their thread_ts is non-empty.
func TestSubtypeRoundTrip(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	// Top-level message
	if err := db.UpsertMessage(Message{
		TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "top",
	}); err != nil {
		t.Fatal(err)
	}
	// Plain thread reply (should NOT appear in main feed)
	if err := db.UpsertMessage(Message{
		TS: "1700000002.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U2", Text: "thread reply", ThreadTS: "1700000001.000000",
	}); err != nil {
		t.Fatal(err)
	}
	// Thread broadcast (SHOULD appear in main feed with subtype set)
	if err := db.UpsertMessage(Message{
		TS: "1700000003.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U2", Text: "broadcast",
		ThreadTS: "1700000001.000000", Subtype: "thread_broadcast",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetMessages("C1", 10, "")
	if err != nil {
		t.Fatal(err)
	}

	var foundTop, foundBroadcast bool
	for _, m := range got {
		switch m.Text {
		case "top":
			foundTop = true
			if m.Subtype != "" {
				t.Errorf("top message Subtype=%q, want empty", m.Subtype)
			}
		case "broadcast":
			foundBroadcast = true
			if m.Subtype != "thread_broadcast" {
				t.Errorf("broadcast Subtype=%q, want thread_broadcast", m.Subtype)
			}
		case "thread reply":
			t.Errorf("plain thread reply should not appear in main feed")
		}
	}
	if !foundTop {
		t.Error("expected top-level message in main feed")
	}
	if !foundBroadcast {
		t.Error("expected thread_broadcast in main feed")
	}
}

func TestUpsertMessageRoundTripsRawJSON(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	payload := `{"type":"message","ts":"1.0","text":"hi","files":[{"id":"F1"}]}`
	if err := db.UpsertMessage(Message{
		TS:          "1.0",
		ChannelID:   "C1",
		WorkspaceID: "T1",
		UserID:      "U1",
		Text:        "hi",
		RawJSON:     payload,
		CreatedAt:   1,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := db.GetMessages("C1", 50, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].RawJSON != payload {
		t.Fatalf("raw_json round-trip mismatch:\nwant: %s\ngot:  %s", payload, got[0].RawJSON)
	}
}

// TestGetMessagesReturnsNewestN guards against a regression where
// GetMessages picked the OLDEST N rows by doing `ORDER BY ts ASC LIMIT N`.
// Once the cache outgrows N rows (any active channel after a day or two),
// that returned a frozen window of the oldest history. Callers want the
// newest N, ascending in the result.
func TestGetMessagesReturnsNewestN(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	// Insert 60 messages with monotonically increasing ts.
	for i := 0; i < 60; i++ {
		if err := db.UpsertMessage(Message{
			TS:          fmt.Sprintf("17000000%02d.000000", i),
			ChannelID:   "C1",
			WorkspaceID: "T1",
			UserID:      "U1",
			Text:        fmt.Sprintf("msg %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.GetMessages("C1", 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Fatalf("want 50 rows, got %d", len(got))
	}
	// Newest 50 = msgs 10..59. Oldest of those = msg 10. Newest = msg 59.
	if got[0].Text != "msg 10" {
		t.Errorf("first row should be the OLDEST of the newest 50 = 'msg 10', got %q", got[0].Text)
	}
	if got[len(got)-1].Text != "msg 59" {
		t.Errorf("last row should be the newest = 'msg 59', got %q", got[len(got)-1].Text)
	}
	// Sanity: result is ASC.
	for i := 1; i < len(got); i++ {
		if got[i-1].TS >= got[i].TS {
			t.Fatalf("result not ascending at index %d: %s >= %s", i, got[i-1].TS, got[i].TS)
		}
	}
}

// TestGetMessagesWithCursorReturnsNewestBeforeCursor verifies the
// cursor variant also picks the newest N below the cursor (used by the
// older-messages backfill path).
func TestGetMessagesWithCursorReturnsNewestBeforeCursor(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	for i := 0; i < 60; i++ {
		db.UpsertMessage(Message{
			TS:          fmt.Sprintf("17000000%02d.000000", i),
			ChannelID:   "C1",
			WorkspaceID: "T1",
			UserID:      "U1",
			Text:        fmt.Sprintf("msg %d", i),
		})
	}

	// Cursor at ts of msg 40 -> candidates are msgs 0..39. Newest 10 = msgs 30..39.
	got, err := db.GetMessages("C1", 10, "1700000040.000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("want 10 rows, got %d", len(got))
	}
	if got[0].Text != "msg 30" {
		t.Errorf("first row should be 'msg 30', got %q", got[0].Text)
	}
	if got[len(got)-1].Text != "msg 39" {
		t.Errorf("last row should be 'msg 39', got %q", got[len(got)-1].Text)
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
