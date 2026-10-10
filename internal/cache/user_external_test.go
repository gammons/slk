package cache

import (
	"path/filepath"
	"testing"
)

func TestExternalUsersAreRelativeToWorkspace(t *testing.T) {
	for _, writer := range []string{"profile", "edge insert", "edge update"} {
		for _, avatar := range []string{"", "https://example.invalid/avatar.png"} {
			for _, tc := range []struct {
				name       string
				homeTeamID string
				isExternal bool
			}{
				{name: "known home team", homeTeamID: "T1"},
				{name: "unknown home team internal"},
				{name: "unknown home team external", isExternal: true},
			} {
				t.Run(writer+"/"+avatar+"/"+tc.name, func(t *testing.T) {
					db := setupDBWithWorkspace(t)
					defer db.Close()
					seedWorkspace(t, db, "T2")
					if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", IsExternal: tc.isExternal}); err != nil {
						t.Fatal(err)
					}
					// T2 discovers a profile first cached by T1. Classification
					// must use the home team rather than overwrite T1's flag.
					// Without a home team, neither SQL branch may change that
					// flag in either direction on behalf of another workspace.
					u := EdgeUserUpdate{ID: "U1", WorkspaceID: "T2", HomeTeamID: tc.homeTeamID, IsExternal: !tc.isExternal, AvatarURL: avatar}
					var err error
					switch writer {
					case "profile":
						err = db.UpsertUser(User{ID: u.ID, WorkspaceID: "T2", HomeTeamID: u.HomeTeamID, IsExternal: u.IsExternal, AvatarURL: avatar})
					case "edge insert":
						err = db.UpsertUserFromEdge("T2", u)
					case "edge update":
						err = db.UpdateUserFromEdge(u)
					}
					if err != nil {
						t.Fatal(err)
					}
					got := getUserRow(t, db, "U1")
					if got.WorkspaceID != "T1" || got.HomeTeamID != tc.homeTeamID || got.IsExternal != tc.isExternal {
						t.Fatalf("T1 row corrupted by T2 resolution: %+v", got)
					}
					for _, team := range []string{"T1", "T2"} {
						external, err := db.ExternalUsers(team)
						wantExternal := tc.isExternal
						if team == "T2" {
							wantExternal = tc.homeTeamID != ""
						}
						if err != nil || external["U1"] != wantExternal {
							t.Fatalf("external users in %s = %v, %v; want U1 external = %v", team, external, err, wantExternal)
						}
					}
					if tc.homeTeamID == "" {
						return
					}
					// A placeholder has no home team; it must not erase the
					// classification data a real profile supplied.
					if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1"}); err != nil {
						t.Fatal(err)
					}
					if got := getUserRow(t, db, "U1"); got.HomeTeamID != "T1" {
						t.Fatalf("placeholder erased home team: %+v", got)
					}
					external, err := db.ExternalUsers("T2")
					if err != nil || !external["U1"] {
						t.Fatalf("placeholder lost T2 classification: %v, %v", external, err)
					}
					delete(external, "U1")
					external, err = db.ExternalUsers("T2")
					if err != nil || !external["U1"] {
						t.Fatal("external snapshot was not caller-owned")
					}
				})
			}
		}
	}
}

func TestExternalUsersLegacyFlagsStayWorkspaceLocal(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedWorkspace(t, db, "T2")
	for _, u := range []User{
		{ID: "legacy", WorkspaceID: "T1", IsExternal: true},
		{ID: "unknown", WorkspaceID: "T1"},
		{ID: "home-known", WorkspaceID: "T2", HomeTeamID: "T1", IsExternal: true},
	} {
		if err := db.UpsertUser(u); err != nil {
			t.Fatal(err)
		}
	}
	// With no home team to derive from, neither upsert nor revalidation
	// may apply a different workspace's flag to the original row.
	if err := db.UpsertUser(User{ID: "legacy", WorkspaceID: "T2"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertUserFromEdge("T2", EdgeUserUpdate{ID: "unknown", IsExternal: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{ID: "legacy", WorkspaceID: "T2"}); err != nil {
		t.Fatal(err)
	}
	for _, team := range []string{"T1", "T2"} {
		external, err := db.ExternalUsers(team)
		if err != nil || external["unknown"] || external["legacy"] != (team == "T1") || external["home-known"] != (team == "T2") {
			t.Fatalf("legacy/unknown flags leaked into %s: %v, %v", team, external, err)
		}
	}
}

func TestUserReadsDeriveIsExternalFromHomeTeam(t *testing.T) {
	for _, tc := range []struct {
		name           string
		homeTeamID     string
		storedExternal bool
		wantExternal   bool
	}{
		{name: "same workspace", homeTeamID: "T1", storedExternal: true},
		{name: "other workspace", homeTeamID: "T2", wantExternal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupDBWithWorkspace(t)
			defer db.Close()
			seedWorkspace(t, db, "T2")
			// Deliberately contradict the known home team: readers must
			// derive the flag, not return the legacy stored value.
			if err := db.UpsertUser(User{
				ID: "U1", WorkspaceID: "T1", HomeTeamID: tc.homeTeamID, IsExternal: tc.storedExternal,
			}); err != nil {
				t.Fatal(err)
			}
			got := getUserRow(t, db, "U1")
			if got.HomeTeamID != tc.homeTeamID || got.IsExternal != tc.wantExternal {
				t.Errorf("GetUser = %+v; want home team %s, external = %v", got, tc.homeTeamID, tc.wantExternal)
			}
			users, err := db.ListUsers("T1")
			if err != nil {
				t.Fatal(err)
			}
			if len(users) != 1 {
				t.Fatalf("ListUsers returned %d users; want 1", len(users))
			}
			if users[0].ID != "U1" || users[0].HomeTeamID != tc.homeTeamID || users[0].IsExternal != tc.wantExternal {
				t.Errorf("ListUsers = %+v; want U1 with home team %s, external = %v", users, tc.homeTeamID, tc.wantExternal)
			}
		})
	}
}

func TestHomeTeamMigrationBackfillsOnlyOnce(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.db")
	db, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	seedWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", IsExternal: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`UPDATE users SET version = 5`); err != nil {
		t.Fatal(err)
	}
	// Reproduce a pre-home-team cache while retaining all older migrations.
	if _, err := db.conn.Exec(`ALTER TABLE users DROP COLUMN home_team_id`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	got := getUserRow(t, db, "U1")
	if got.HomeTeamID != "" || !got.IsExternal {
		t.Fatalf("migration invented a home team/lost legacy fallback: %+v", got)
	}
	versions, err := db.UserVersions("T1")
	if err != nil || versions["U1"] != 0 {
		t.Fatalf("old profile will not be revalidated: %v, %v", versions, err)
	}
	if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{ID: "U1", HomeTeamID: "T1", Version: 7}); err != nil {
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
		t.Fatalf("reopening reset already-backfilled version: %v, %v", versions, err)
	}
	if got := getUserRow(t, db, "U1"); got.HomeTeamID != "T1" || got.IsExternal {
		t.Fatalf("reopening lost home-team classification: %+v", got)
	}
}
