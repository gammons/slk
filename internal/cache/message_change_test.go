package cache

import (
	"encoding/json"
	"testing"
)

// rawKeys decodes a row's raw_json into its top-level keys.
func rawKeys(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatalf("raw_json %q: %v", raw, err)
	}
	return keys
}

// A thread parent as a history fetch caches it: reply_count in the
// column and in raw_json, thread_ts = ts, a preview attachment.
func seedParent(t *testing.T, db *DB) {
	t.Helper()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	raw := `{"type":"message","ts":"1.000000","user":"U1","text":"old","thread_ts":"1.000000","reply_count":3,"reply_users":["U2"],"bot_id":"B1","username":"buildbot","attachments":[{"id":1,"title":"old card"}]}`
	if err := db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1",
		Text: "old", ThreadTS: "1.000000", ReplyCount: 3, RawJSON: raw, CreatedAt: 100}); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
}

func TestApplyMessageChange_ReplacesContentKeepsTheRest(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedParent(t, db)

	found, err := db.ApplyMessageChange("C1", "1.000000", MessageChange{
		Text:        "new",
		Blocks:      json.RawMessage(`[{"type":"rich_text","block_id":"b1"}]`),
		Attachments: json.RawMessage(`[{"id":1,"title":"new card"}]`),
	})
	if err != nil || !found {
		t.Fatalf("ApplyMessageChange = %v, %v; want true, nil", found, err)
	}
	got, err := db.GetMessage("C1", "1.000000")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if got.Text != "new" {
		t.Errorf("text = %q, want new", got.Text)
	}
	if got.ReplyCount != 3 || got.ThreadTS != "1.000000" || got.UserID != "U1" || got.CreatedAt != 100 {
		t.Errorf("row lost what the change did not carry: %+v", got)
	}
	keys := rawKeys(t, got.RawJSON)
	if string(keys["text"]) != `"new"` {
		t.Errorf("raw text = %s, want \"new\"", keys["text"])
	}
	if string(keys["reply_count"]) != "3" || string(keys["thread_ts"]) != `"1.000000"` || keys["reply_users"] == nil ||
		string(keys["bot_id"]) != `"B1"` || string(keys["username"]) != `"buildbot"` {
		t.Errorf("raw_json lost what the change did not carry: %s", got.RawJSON)
	}
	if string(keys["attachments"]) != `[{"id":1,"title":"new card"}]` {
		t.Errorf("raw attachments = %s", keys["attachments"])
	}
	if string(keys["blocks"]) != `[{"type":"rich_text","block_id":"b1"}]` {
		t.Errorf("raw blocks = %s", keys["blocks"])
	}
	if _, ok := keys["files"]; ok {
		t.Errorf("raw files = %s, want absent", keys["files"])
	}
}

// The last preview removed: the change has no attachments, and the
// row must not keep the old one.
func TestApplyMessageChange_EmptyContentRemovesTheKey(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedParent(t, db)

	for _, empty := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`[]`)} {
		if _, err := db.ApplyMessageChange("C1", "1.000000", MessageChange{Text: "old", Attachments: empty}); err != nil {
			t.Fatalf("ApplyMessageChange: %v", err)
		}
		got, _ := db.GetMessage("C1", "1.000000")
		if a, ok := rawKeys(t, got.RawJSON)["attachments"]; ok {
			t.Errorf("attachments %q: raw attachments = %s, want absent", empty, a)
		}
		seedParent(t, db)
	}
}

func TestApplyMessageChange_KeepsBroadcastSubtype(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "old",
		ThreadTS: "1.000000", Subtype: "thread_broadcast",
		RawJSON: `{"type":"message","subtype":"thread_broadcast","ts":"2.000000","thread_ts":"1.000000","text":"old"}`})

	if _, err := db.ApplyMessageChange("C1", "2.000000", MessageChange{Text: "new"}); err != nil {
		t.Fatalf("ApplyMessageChange: %v", err)
	}
	feed, err := db.GetMessages("C1", 50, "")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(feed) != 1 || feed[0].Subtype != "thread_broadcast" || feed[0].Text != "new" {
		t.Fatalf("channel feed = %+v, want the broadcast with the new text", feed)
	}
	if string(rawKeys(t, feed[0].RawJSON)["subtype"]) != `"thread_broadcast"` {
		t.Errorf("raw_json lost the subtype: %s", feed[0].RawJSON)
	}
}

func TestApplyMessageChange_MissingRowWritesNothing(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})

	found, err := db.ApplyMessageChange("C1", "9.000000", MessageChange{Text: "new"})
	if err != nil || found {
		t.Fatalf("ApplyMessageChange = %v, %v; want false, nil", found, err)
	}
	if _, err := db.GetMessage("C1", "9.000000"); err == nil {
		t.Error("a change for an uncached message created a row")
	}
}

// A soft-deleted row stays as it was: a late echo must not change it.
func TestApplyMessageChange_LeavesDeletedRowAlone(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedParent(t, db)
	db.DeleteMessage("C1", "1.000000")

	found, err := db.ApplyMessageChange("C1", "1.000000", MessageChange{Text: "new"})
	if err != nil || found {
		t.Fatalf("ApplyMessageChange = %v, %v; want false, nil", found, err)
	}
	if got, _ := db.GetMessage("C1", "1.000000"); got.Text != "old" {
		t.Errorf("deleted row text = %q, want old", got.Text)
	}
}

// Rows cached before raw_json existed have it empty; the change gives
// them one with the content.
func TestApplyMessageChange_EmptyRawJSON(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "old", ReplyCount: 2})

	if _, err := db.ApplyMessageChange("C1", "1.000000", MessageChange{Text: "new", Files: json.RawMessage(`[{"id":"F1"}]`)}); err != nil {
		t.Fatalf("ApplyMessageChange: %v", err)
	}
	got, _ := db.GetMessage("C1", "1.000000")
	keys := rawKeys(t, got.RawJSON)
	if string(keys["text"]) != `"new"` || string(keys["files"]) != `[{"id":"F1"}]` || got.ReplyCount != 2 {
		t.Errorf("row = %+v, raw = %s", got, got.RawJSON)
	}
}

// A raw_json that does not parse is replaced by the patch, not kept.
func TestApplyMessageChange_MalformedRawJSON(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "old", RawJSON: `{"text":`})

	if _, err := db.ApplyMessageChange("C1", "1.000000", MessageChange{Text: "new"}); err != nil {
		t.Fatalf("ApplyMessageChange: %v", err)
	}
	got, _ := db.GetMessage("C1", "1.000000")
	if string(rawKeys(t, got.RawJSON)["text"]) != `"new"` {
		t.Errorf("raw_json = %s", got.RawJSON)
	}
}

// Messages are keyed by channel and ts: the same ts in another channel
// is another message.
func TestApplyMessageChange_OnlyThatChannel(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedParent(t, db)
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T1", Name: "random", Type: "channel"})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: "U1", Text: "other"})

	if _, err := db.ApplyMessageChange("C1", "1.000000", MessageChange{Text: "new"}); err != nil {
		t.Fatalf("ApplyMessageChange: %v", err)
	}
	if got, _ := db.GetMessage("C2", "1.000000"); got.Text != "other" {
		t.Errorf("C2 text = %q, want other", got.Text)
	}
}

// thread_ts is filled from the change only when the row has none: a
// parent's first WebSocket delivery has none, and its change echo
// carries its own ts. A thread_ts the row has is kept.
func TestApplyMessageChange_ThreadTSOnlyFillsAnEmptyOne(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "parent"})
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "reply", ThreadTS: "1.000000"})

	for _, c := range []struct {
		ts     string
		change MessageChange
	}{
		{"1.000000", MessageChange{Text: "parent", ThreadTS: "1.000000"}},
		{"2.000000", MessageChange{Text: "reply", ThreadTS: ""}},
		{"2.000000", MessageChange{Text: "reply", ThreadTS: "9.000000"}},
	} {
		if found, err := db.ApplyMessageChange("C1", c.ts, c.change); err != nil || !found {
			t.Fatalf("ApplyMessageChange(%s) = %v, %v; want true, nil", c.ts, found, err)
		}
	}

	if got, _ := db.GetMessage("C1", "1.000000"); got.ThreadTS != "1.000000" {
		t.Errorf("parent thread_ts = %q, want it filled with 1.000000", got.ThreadTS)
	}
	if got, _ := db.GetMessage("C1", "2.000000"); got.ThreadTS != "1.000000" {
		t.Errorf("reply thread_ts = %q, want 1.000000 kept", got.ThreadTS)
	}
}
