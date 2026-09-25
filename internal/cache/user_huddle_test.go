package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestHuddleState_WrittenByEveryStatusPath(t *testing.T) {
	for _, avatar := range []string{"", "https://example.invalid/a.png"} {
		db := openEdgeSyncTestDB(t)
		seedWorkspace(t, db, "T1")

		if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{
			ID: "U1", Name: "alice", AvatarURL: avatar, HuddleState: "in_a_huddle", HuddleExpiration: 11,
		}); err != nil {
			t.Fatalf("avatar=%q: UpsertUserFromEdge: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.HuddleState != "in_a_huddle" || u.HuddleExpiration != 11 {
			t.Errorf("avatar=%q: upsert huddle = %q %d", avatar, u.HuddleState, u.HuddleExpiration)
		}

		if err := db.UpdateUserFromEdge(EdgeUserUpdate{ID: "U1", Name: "alice", AvatarURL: avatar, HuddleState: "default_unset"}); err != nil {
			t.Fatalf("avatar=%q: UpdateUserFromEdge: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.HuddleState != "default_unset" || u.HuddleExpiration != 0 {
			t.Errorf("avatar=%q: update did not clear the huddle: %q %d", avatar, u.HuddleState, u.HuddleExpiration)
		}

		if err := db.UpdateUserStatus("U1", "", "", 0, "in_a_huddle", 22); err != nil {
			t.Fatalf("avatar=%q: UpdateUserStatus: %v", avatar, err)
		}
		if u := getUserRow(t, db, "U1"); u.HuddleState != "in_a_huddle" || u.HuddleExpiration != 22 {
			t.Errorf("avatar=%q: UpdateUserStatus huddle = %q %d", avatar, u.HuddleState, u.HuddleExpiration)
		}
	}
}

func TestListUserStatuses_IncludesHuddleOnlyUsers(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	for _, u := range []User{
		{ID: "U1", WorkspaceID: "T1", Name: "alice", HuddleState: "in_a_huddle", HuddleExpiration: 9},
		{ID: "U2", WorkspaceID: "T1", Name: "bob", HuddleState: "default_unset"},
	} {
		if err := db.UpsertUser(u); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListUserStatuses("T1")
	if err != nil {
		t.Fatalf("ListUserStatuses: %v", err)
	}
	if len(got) != 1 || got[0].ID != "U1" || got[0].HuddleState != "in_a_huddle" || got[0].HuddleExpiration != 9 {
		t.Errorf("ListUserStatuses = %+v; want only U1 with its huddle", got)
	}
}

// A database that already gained the status columns must still refetch
// every cached user once when the huddle columns appear.
func TestHuddleMigration_ResetsUserVersionsOnDatabasesWithStatusColumns(t *testing.T) {
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

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"huddle_state", "huddle_expiration"} {
		if _, err := conn.Exec("ALTER TABLE users DROP COLUMN " + col); err != nil {
			t.Fatalf("dropping %s: %v", col, err)
		}
	}
	conn.Close()

	db, err = New(dsn)
	if err != nil {
		t.Fatalf("New on pre-huddle db: %v", err)
	}
	defer db.Close()
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U1"] != 0 {
		t.Errorf("version = %d after adding huddle columns; want 0", vers["U1"])
	}
	if u := getUserRow(t, db, "U1"); u.HuddleState != "" || u.HuddleExpiration != 0 {
		t.Errorf("migrated row huddle = %q %d; want empty defaults", u.HuddleState, u.HuddleExpiration)
	}
}
