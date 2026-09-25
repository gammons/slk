package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUpdateUserStatus_WritesAndClears(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice"}); err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateUserStatus("U1", ":calendar:", "In a meeting", 1700000000, "", 0); err != nil {
		t.Fatalf("UpdateUserStatus: %v", err)
	}
	u := getUserRow(t, db, "U1")
	if u.StatusEmoji != ":calendar:" || u.StatusText != "In a meeting" || u.StatusExpiration != 1700000000 {
		t.Errorf("status not written: %+v", u)
	}

	if err := db.UpdateUserStatus("U1", "", "", 0, "", 0); err != nil {
		t.Fatalf("UpdateUserStatus clear: %v", err)
	}
	u = getUserRow(t, db, "U1")
	if u.StatusEmoji != "" || u.StatusText != "" || u.StatusExpiration != 0 {
		t.Errorf("status not cleared: %+v", u)
	}
}

func TestListUserStatuses_OnlyUsersWithAStatusInTheWorkspace(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.UpsertWorkspace(Workspace{ID: "T2", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []User{
		{ID: "U1", WorkspaceID: "T1", Name: "alice", StatusEmoji: ":calendar:", StatusText: "In a meeting", StatusExpiration: 5},
		{ID: "U2", WorkspaceID: "T1", Name: "bob"},
		{ID: "U3", WorkspaceID: "T1", Name: "carol", StatusText: "Heads down"},
		{ID: "U4", WorkspaceID: "T2", Name: "dave", StatusEmoji: ":palm_tree:"},
	} {
		if err := db.UpsertUser(u); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.ListUserStatuses("T1")
	if err != nil {
		t.Fatalf("ListUserStatuses: %v", err)
	}
	byID := map[string]User{}
	for _, u := range got {
		byID[u.ID] = u
	}
	if len(byID) != 2 {
		t.Fatalf("got users %v; want U1 and U3 only", byID)
	}
	if u := byID["U1"]; u.StatusEmoji != ":calendar:" || u.StatusText != "In a meeting" || u.StatusExpiration != 5 {
		t.Errorf("U1 = %+v", u)
	}
	if u := byID["U3"]; u.StatusText != "Heads down" {
		t.Errorf("U3 = %+v", u)
	}
}

// UpsertUser callers include placeholder writes built without a
// profile. On conflict they must not blank a status another path wrote.
func TestUpsertUser_WritesStatusOnInsertButNotOnConflict(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.UpsertUser(User{
		ID: "U1", WorkspaceID: "T1", Name: "alice",
		StatusEmoji: ":palm_tree:", StatusText: "Vacation", StatusExpiration: 42,
	}); err != nil {
		t.Fatal(err)
	}
	if u := getUserRow(t, db, "U1"); u.StatusEmoji != ":palm_tree:" || u.StatusText != "Vacation" || u.StatusExpiration != 42 {
		t.Fatalf("insert did not write status: %+v", u)
	}

	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice", Presence: "away"}); err != nil {
		t.Fatal(err)
	}
	if u := getUserRow(t, db, "U1"); u.StatusEmoji != ":palm_tree:" || u.StatusText != "Vacation" || u.StatusExpiration != 42 {
		t.Errorf("conflicting upsert without a profile blanked the status: %+v", u)
	}
}

// The two UpdateUserFromEdge branches are separate SQL statements, so
// each must write status, and write an empty one as a clear.
func TestUpdateUserFromEdge_WritesAndClearsStatusOnBothBranches(t *testing.T) {
	for _, avatar := range []string{"", "https://example.invalid/a.png"} {
		db := openEdgeSyncTestDB(t)
		seedWorkspace(t, db, "T1")
		if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice"}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdateUserStatus("U1", ":calendar:", "In a meeting", 99, "", 0); err != nil {
			t.Fatal(err)
		}

		if err := db.UpdateUserFromEdge(EdgeUserUpdate{
			ID: "U1", Name: "alice", AvatarURL: avatar,
			StatusEmoji: ":house:", StatusText: "Working remotely", StatusExpiration: 123,
			Version: sampleUserVersion,
		}); err != nil {
			t.Fatalf("avatar=%q: UpdateUserFromEdge: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.StatusEmoji != ":house:" || u.StatusText != "Working remotely" || u.StatusExpiration != 123 {
			t.Errorf("avatar=%q: status not written: %+v", avatar, u)
		}

		if err := db.UpdateUserFromEdge(EdgeUserUpdate{
			ID: "U1", Name: "alice", AvatarURL: avatar, Version: sampleUserVersion,
		}); err != nil {
			t.Fatalf("avatar=%q: UpdateUserFromEdge clear: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.StatusEmoji != "" || u.StatusText != "" || u.StatusExpiration != 0 {
			t.Errorf("avatar=%q: an empty edge status must clear, got %+v", avatar, u)
		}
	}
}

func TestUpsertUserFromEdge_WritesStatusOnInsertAndConflict(t *testing.T) {
	for _, avatar := range []string{"", "https://example.invalid/a.png"} {
		db := openEdgeSyncTestDB(t)
		seedWorkspace(t, db, "T1")
		upd := EdgeUserUpdate{
			ID: "U1", Name: "alice", AvatarURL: avatar,
			StatusEmoji: ":calendar:", StatusText: "In a meeting", StatusExpiration: 7,
			Version: sampleUserVersion,
		}
		if err := db.UpsertUserFromEdge("T1", upd); err != nil {
			t.Fatalf("avatar=%q: insert: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.StatusEmoji != ":calendar:" || u.StatusText != "In a meeting" || u.StatusExpiration != 7 {
			t.Errorf("avatar=%q: insert did not write status: %+v", avatar, u)
		}

		upd.StatusEmoji, upd.StatusText, upd.StatusExpiration = "", "", 0
		if err := db.UpsertUserFromEdge("T1", upd); err != nil {
			t.Fatalf("avatar=%q: conflict: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.StatusEmoji != "" || u.StatusText != "" || u.StatusExpiration != 0 {
			t.Errorf("avatar=%q: conflict did not clear status: %+v", avatar, u)
		}
	}
}

// Users cached before the status columns existed keep a version that
// conditional revalidation treats as current, so they would never be
// refetched to fill the new columns. The migration zeroes versions the
// first time the columns appear, and never again.
func TestStatusMigration_ResetsUserVersionsOnlyOnce(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.db")

	db, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	seedWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`UPDATE users SET version = 5`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Turn it back into a database from before the status columns.
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"status_emoji", "status_text", "status_expiration"} {
		if _, err := conn.Exec("ALTER TABLE users DROP COLUMN " + col); err != nil {
			t.Fatalf("dropping %s: %v", col, err)
		}
	}
	conn.Close()

	db, err = New(dsn)
	if err != nil {
		t.Fatalf("New on pre-status db: %v", err)
	}
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U1"] != 0 {
		t.Errorf("version = %d after adding status columns; want 0 so the next revalidation refetches the user", vers["U1"])
	}
	if u := getUserRow(t, db, "U1"); u.StatusEmoji != "" || u.StatusText != "" || u.StatusExpiration != 0 {
		t.Errorf("migrated row status = %+v; want empty defaults", u)
	}
	if _, err := db.conn.Exec(`UPDATE users SET version = 7`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = New(dsn)
	if err != nil {
		t.Fatalf("re-opening migrated db: %v", err)
	}
	defer db.Close()
	vers, err = db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U1"] != 7 {
		t.Errorf("version = %d after reopening an already-migrated db; want 7, the reset must run only once", vers["U1"])
	}
}
