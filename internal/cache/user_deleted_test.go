package cache

import (
	"path/filepath"
	"testing"
)

func TestUserDeletedIsPersistedByEveryWriter(t *testing.T) {
	for _, writer := range []string{"profile", "edge insert", "edge update"} {
		for _, avatar := range []string{"", "https://example.invalid/avatar.png"} {
			t.Run(writer+"/"+avatar, func(t *testing.T) {
				db := setupDBWithWorkspace(t)
				defer db.Close()
				// Every writer must create or overwrite the flag, both ways:
				// a reactivated user has to leave the set again.
				if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1"}); err != nil {
					t.Fatal(err)
				}
				for _, deleted := range []bool{true, false} {
					var err error
					switch writer {
					case "profile":
						err = db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", AvatarURL: avatar, Deleted: deleted})
					case "edge insert":
						err = db.UpsertUserFromEdge("T1", EdgeUserUpdate{ID: "U1", AvatarURL: avatar, Deleted: deleted})
					case "edge update":
						err = db.UpdateUserFromEdge(EdgeUserUpdate{ID: "U1", AvatarURL: avatar, Deleted: deleted})
					}
					if err != nil {
						t.Fatal(err)
					}
					if got := getUserRow(t, db, "U1"); got.Deleted != deleted {
						t.Fatalf("after writing deleted=%v the row has Deleted=%v", deleted, got.Deleted)
					}
				}
			})
		}
	}
}

func TestEdgeInsertCreatesDeletedRow(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{ID: "U9", Deleted: true}); err != nil {
		t.Fatal(err)
	}
	if got := getUserRow(t, db, "U9"); !got.Deleted {
		t.Fatalf("new row lost the flag: %+v", got)
	}
}

func TestMarkUserDeletedNeverCreatesARow(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.MarkUserDeleted("U404"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetUser("U404"); err == nil {
		t.Fatal("MarkUserDeleted created a row")
	}
	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "ann"}); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkUserDeleted("U1"); err != nil {
		t.Fatal(err)
	}
	got := getUserRow(t, db, "U1")
	if !got.Deleted || got.Name != "ann" {
		t.Fatalf("MarkUserDeleted changed more than the flag: %+v", got)
	}
}

func TestClearUserDeletedNeverCreatesARow(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.ClearUserDeleted("U404"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetUser("U404"); err == nil {
		t.Fatal("ClearUserDeleted created a row")
	}
	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Deleted: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearUserDeleted("U1"); err != nil {
		t.Fatal(err)
	}
	if got := getUserRow(t, db, "U1"); got.Deleted {
		t.Fatalf("flag not cleared: %+v", got)
	}
}

func TestDeactivatedUsersIsGlobalAndCallerOwned(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedWorkspace(t, db, "T2")
	for _, u := range []User{
		{ID: "U1", WorkspaceID: "T1", Deleted: true},
		{ID: "U2", WorkspaceID: "T2", Deleted: true},
		{ID: "U3", WorkspaceID: "T1"},
	} {
		if err := db.UpsertUser(u); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.DeactivatedUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["U1"] || !got["U2"] || got["U3"] {
		t.Fatalf("DeactivatedUsers = %v; want exactly U1 and U2", got)
	}
	delete(got, "U1")
	again, err := db.DeactivatedUsers()
	if err != nil || !again["U1"] {
		t.Fatal("DeactivatedUsers snapshot was not caller-owned")
	}
}

func TestDeletedMigrationBackfillsOnlyOnce(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.db")
	db, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	seedWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`UPDATE users SET version = 5`); err != nil {
		t.Fatal(err)
	}
	// Reproduce a cache from before the column existed.
	if _, err := db.conn.Exec(`ALTER TABLE users DROP COLUMN deleted`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := db.UserVersions("T1")
	if err != nil || versions["U1"] != 0 {
		t.Fatalf("old profile will not be revalidated: %v, %v", versions, err)
	}
	if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{ID: "U1", Deleted: true, Version: 7}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	versions, err = db.UserVersions("T1")
	if err != nil || versions["U1"] != 7 {
		t.Fatalf("reopening reset an already-backfilled version: %v, %v", versions, err)
	}
	if got := getUserRow(t, db, "U1"); !got.Deleted {
		t.Fatalf("reopening lost the flag: %+v", got)
	}
}
