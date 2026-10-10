package cache

import "testing"

// FillUserAvatarURL backfills a user Slack's edge endpoints left without
// an avatar (they only carry image_original, absent for users who never
// uploaded one). It must fill only an empty URL: a real one written in
// the meantime, e.g. by an edge revalidation, wins.
func TestFillUserAvatarURL(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	for _, u := range []User{
		{ID: "U_EMPTY", WorkspaceID: "T1", Name: "ray", DisplayName: "Ray", Presence: "active"},
		{ID: "U_SET", WorkspaceID: "T1", Name: "pat", DisplayName: "Pat", Presence: "active", AvatarURL: "https://keep/me.png"},
	} {
		if err := db.UpsertUser(u); err != nil {
			t.Fatal(err)
		}
	}

	const url = "https://secure.gravatar.com/avatar/abc.jpg?s=72"
	for _, id := range []string{"U_EMPTY", "U_SET", "U_MISSING"} {
		if err := db.FillUserAvatarURL(id, url); err != nil {
			t.Fatalf("FillUserAvatarURL(%s): %v", id, err)
		}
	}

	if got, _ := db.GetUser("U_EMPTY"); got.AvatarURL != url {
		t.Errorf("empty avatar_url = %q after fill; want %q", got.AvatarURL, url)
	}
	got, _ := db.GetUser("U_EMPTY")
	if got.Presence != "active" || got.DisplayName != "Ray" {
		t.Errorf("fill touched other columns: presence=%q display_name=%q", got.Presence, got.DisplayName)
	}
	if got, _ := db.GetUser("U_SET"); got.AvatarURL != "https://keep/me.png" {
		t.Errorf("existing avatar_url = %q after fill; want it kept", got.AvatarURL)
	}
	if _, err := db.GetUser("U_MISSING"); err == nil {
		t.Error("fill created a row for an unknown user")
	}
}
