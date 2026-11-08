package cache

import (
	"maps"
	"testing"
)

func TestCustomEmojiRoundTripKeepsAliases(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	want := map[string]string{
		"party-parrot": "https://example.test/parrot.gif",
		"parrot":       "alias:party-parrot",
	}
	if err := db.ReplaceCustomEmoji("T1", want, 100); err != nil {
		t.Fatal(err)
	}
	got, err := db.CustomEmoji("T1")
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Errorf("CustomEmoji = %v, want %v", got, want)
	}
	if ts, ok, err := db.CustomEmojiFetchedAt("T1"); err != nil || !ok || ts != 100 {
		t.Errorf("CustomEmojiFetchedAt = %d, %v, %v, want 100, true, nil", ts, ok, err)
	}
}

func TestCustomEmojiNeverFetched(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	got, err := db.CustomEmoji("T1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("CustomEmoji = %v, want empty non-nil map", got)
	}
	if _, ok, err := db.CustomEmojiFetchedAt("T1"); err != nil || ok {
		t.Errorf("CustomEmojiFetchedAt ok = %v, err = %v, want false, nil", ok, err)
	}
}

// A workspace with no custom emoji must read as fetched, or it would
// call emoji.list on every launch.
func TestCustomEmojiEmptySetCountsAsFetched(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.ReplaceCustomEmoji("T1", map[string]string{}, 100); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.CustomEmojiFetchedAt("T1"); err != nil || !ok {
		t.Errorf("CustomEmojiFetchedAt ok = %v, err = %v, want true, nil", ok, err)
	}
}

func TestReplaceCustomEmojiReplacesAndIsolatesWorkspaces(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.ReplaceCustomEmoji("T1", map[string]string{"old": "u1"}, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceCustomEmoji("T2", map[string]string{"other": "u2"}, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceCustomEmoji("T1", map[string]string{"new": "u3"}, 200); err != nil {
		t.Fatal(err)
	}

	got, _ := db.CustomEmoji("T1")
	if !maps.Equal(got, map[string]string{"new": "u3"}) {
		t.Errorf("T1 = %v, want only the new set", got)
	}
	other, _ := db.CustomEmoji("T2")
	if !maps.Equal(other, map[string]string{"other": "u2"}) {
		t.Errorf("T2 = %v, want it untouched", other)
	}
}

func TestCustomEmojiDeltasLeaveFetchedAtAlone(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.ReplaceCustomEmoji("T1", map[string]string{"a": "u1", "b": "u2"}, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertCustomEmoji("T1", "a", "u1b"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertCustomEmoji("T1", "c", "u3"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCustomEmoji("T1", []string{"b", "missing"}); err != nil {
		t.Fatal(err)
	}

	got, _ := db.CustomEmoji("T1")
	if !maps.Equal(got, map[string]string{"a": "u1b", "c": "u3"}) {
		t.Errorf("CustomEmoji = %v", got)
	}
	if ts, _, _ := db.CustomEmojiFetchedAt("T1"); ts != 100 {
		t.Errorf("fetched_at = %d, want 100: a delta must not reset the TTL", ts)
	}
}

func TestZeroCustomEmojiFetchedAtKeepsRows(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.ReplaceCustomEmoji("T1", map[string]string{"a": "u1"}, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.ZeroCustomEmojiFetchedAt("T1"); err != nil {
		t.Fatal(err)
	}
	if ts, ok, _ := db.CustomEmojiFetchedAt("T1"); !ok || ts != 0 {
		t.Errorf("CustomEmojiFetchedAt = %d, %v, want 0, true", ts, ok)
	}
	if got, _ := db.CustomEmoji("T1"); got["a"] != "u1" {
		t.Errorf("CustomEmoji = %v, want the row kept", got)
	}
}
