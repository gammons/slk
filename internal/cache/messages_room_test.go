package cache

import "testing"

func TestUpsertMessage_KeepsStoredRoomWhenRewrittenWithoutOne(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	withRoom := `{"ts":"1.000000","subtype":"huddle_thread","room":{"created_by":"UFAKE0001","has_ended":false}}`
	typed := `{"ts":"1.000000","subtype":"huddle_thread","text":"edited"}`
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", RawJSON: withRoom, Subtype: "huddle_thread"})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", RawJSON: typed, Subtype: "huddle_thread"})

	got, err := db.GetMessage("C1", "1.000000")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.conn.QueryRow(`SELECT json_extract(raw_json,'$.room.created_by') = 'UFAKE0001' AND json_extract(raw_json,'$.text') = 'edited' FROM messages WHERE ts='1.000000'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("raw_json after typed rewrite = %s, want new text and the old room", got.RawJSON)
	}

	// A newer room replaces the stored one.
	newer := `{"ts":"1.000000","room":{"created_by":"UFAKE0001","has_ended":true}}`
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", RawJSON: newer, Subtype: "huddle_thread"})
	if err := db.conn.QueryRow(`SELECT json_extract(raw_json,'$.room.has_ended') FROM messages WHERE ts='1.000000'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("newer room not stored (has_ended=%d, err=%v)", n, err)
	}

	// Non-JSON raw_json (legacy rows) must not break the upsert.
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", RawJSON: "", Subtype: ""})
	if err := db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", RawJSON: "not json"}); err != nil {
		t.Errorf("upsert over empty raw_json: %v", err)
	}
}
