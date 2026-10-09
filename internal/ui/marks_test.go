// internal/ui/marks_test.go
//
// Tests for the merged marks storage (marksStore): the session tier
// (lowercase) and the SQLite tier (uppercase, and lowercase under
// persist_all) behind one accessor. A "restart" is a fresh marksStore
// sharing the same persistence store: the session map is empty, the
// marks table is not.
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ids"
)

// marksPersistForTest returns a MarksPersistStore over a fresh
// in-memory cache.DB. A new marksStore sharing the same persist store
// simulates an application restart: the session tier is gone, the
// marks table is not.
func marksPersistForTest(t *testing.T) MarksPersistStore {
	t.Helper()
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewMarksPersistStore(MarksPersistStoreFuncs{
		Upsert: func(teamID, letter string, m Mark) error {
			return db.UpsertMark(teamID, letter, cache.Mark{
				ChannelID:   string(m.ChannelID),
				MessageTS:   string(m.MessageTS),
				ThreadTS:    string(m.ThreadTS),
				ChannelName: m.ChannelName,
				AuthorName:  m.AuthorName,
				Excerpt:     m.Excerpt,
			})
		},
		List: func(teamID string) ([]Mark, error) {
			rows, err := db.ListMarks(teamID)
			if err != nil {
				return nil, err
			}
			out := make([]Mark, 0, len(rows))
			for _, r := range rows {
				out = append(out, Mark{
					Location: Location{
						TeamID:    ids.TeamID(r.WorkspaceID),
						ChannelID: ids.ChannelID(r.ChannelID),
						MessageTS: ids.MessageTS(r.MessageTS),
						ThreadTS:  ids.ThreadTS(r.ThreadTS),
					},
					Letter:      r.Letter,
					ChannelName: r.ChannelName,
					AuthorName:  r.AuthorName,
					Excerpt:     r.Excerpt,
				})
			}
			return out, nil
		},
		Delete: func(teamID, letter string) error {
			return db.DeleteMark(teamID, letter)
		},
	})
}

func testMark(letter, channelID, ts string) Mark {
	return Mark{
		Location: Location{
			TeamID:    ids.TeamID("T1"),
			ChannelID: ids.ChannelID(channelID),
			MessageTS: ids.MessageTS(ts),
		},
		Letter:      letter,
		ChannelName: "general",
		AuthorName:  "alice",
		Excerpt:     "hello world",
	}
}

func TestMarksStore_UppercasePersistsLowercaseDoesNot(t *testing.T) {
	persist := marksPersistForTest(t)
	s1 := newMarksStore()
	s1.persist = persist
	if err := s1.Set("T1", "a", testMark("a", "C1", "1.0")); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	if err := s1.Set("T1", "A", testMark("A", "C1", "1.0")); err != nil {
		t.Fatalf("Set A: %v", err)
	}

	// Restart: fresh session over the same persistence.
	s2 := newMarksStore()
	s2.persist = persist

	if m, ok := s2.Load("T1", "A"); !ok || m.ChannelID != "C1" {
		t.Fatalf("uppercase mark after restart = %+v ok=%v, want the persisted location", m, ok)
	}
	if _, ok := s2.Load("T1", "a"); ok {
		t.Fatal("lowercase mark must not survive a restart with persist_all off")
	}
}

func TestMarksStore_PersistAll_KeepsLowercaseAcrossRestart(t *testing.T) {
	persist := marksPersistForTest(t)
	s1 := newMarksStore()
	s1.persist = persist
	s1.persistAll = true
	s1.Set("T1", "a", testMark("a", "C1", "1.0"))

	s2 := newMarksStore()
	s2.persist = persist
	s2.persistAll = true
	if m, ok := s2.Load("T1", "a"); !ok || m.MessageTS != "1.0" {
		t.Fatalf("lowercase mark after restart with persist_all on = %+v ok=%v", m, ok)
	}
}

// Turning persist_all off must not delete persisted lowercase rows:
// they stop loading, and flipping the option back on restores them.
func TestMarksStore_PersistAllOff_IsNonDestructive(t *testing.T) {
	persist := marksPersistForTest(t)

	s1 := newMarksStore()
	s1.persist = persist
	s1.persistAll = true
	s1.Set("T1", "a", testMark("a", "C1", "1.0"))

	// Restart with the option off: the lowercase mark is not loaded,
	// and does not appear in the list.
	s2 := newMarksStore()
	s2.persist = persist
	if _, ok := s2.Load("T1", "a"); ok {
		t.Fatal("lowercase mark must not load while persist_all is off")
	}
	if marks := s2.List("T1"); len(marks) != 0 {
		t.Fatalf("lowercase mark must not surface while persist_all is off, got %+v", marks)
	}

	// Restart with the option back on: the row was never deleted, so
	// the mark comes back.
	s3 := newMarksStore()
	s3.persist = persist
	s3.persistAll = true
	if m, ok := s3.Load("T1", "a"); !ok || m.MessageTS != "1.0" {
		t.Fatalf("lowercase mark after toggling persist_all back on = %+v ok=%v", m, ok)
	}
}

func TestMarksStore_PerWorkspaceIsolation(t *testing.T) {
	persist := marksPersistForTest(t)
	s := newMarksStore()
	s.persist = persist

	s.Set("T1", "a", testMark("a", "C1", "1.0"))
	s.Set("T2", "a", testMark("a", "C9", "9.0"))

	if m, ok := s.Load("T1", "a"); !ok || m.ChannelID != "C1" {
		t.Fatalf("T1 mark a = %+v ok=%v, want C1", m, ok)
	}
	if m, ok := s.Load("T2", "a"); !ok || m.ChannelID != "C9" {
		t.Fatalf("T2 mark a = %+v ok=%v, want C9", m, ok)
	}
	t1 := s.List("T1")
	if len(t1) != 1 || t1[0].ChannelID != "C1" {
		t.Fatalf("T1 list = %+v, want only the T1 mark", t1)
	}
}

func TestMarksStore_OverwriteReplaces(t *testing.T) {
	persist := marksPersistForTest(t)
	s := newMarksStore()
	s.persist = persist

	s.Set("T1", "a", testMark("a", "C1", "1.0"))
	s.Set("T1", "a", testMark("a", "C2", "2.0"))

	if m, ok := s.Load("T1", "a"); !ok || m.ChannelID != "C2" || m.MessageTS != "2.0" {
		t.Fatalf("mark after overwrite = %+v ok=%v, want the new location", m, ok)
	}
	if marks := s.List("T1"); len(marks) != 1 {
		t.Fatalf("list after overwrite = %+v, want exactly one entry", marks)
	}
}

func TestMarksStore_DeleteRemovesFromBothTiers(t *testing.T) {
	persist := marksPersistForTest(t)
	s := newMarksStore()
	s.persist = persist

	if err := s.Set("T1", "A", testMark("A", "C1", "1.0")); err != nil {
		t.Fatalf("Set A: %v", err)
	}
	s.Delete("T1", "A")

	if _, ok := s.Load("T1", "A"); ok {
		t.Fatal("deleted uppercase mark still loads")
	}
	// Restart: the deletion reached the table, so the mark stays gone.
	s2 := newMarksStore()
	s2.persist = persist
	if _, ok := s2.Load("T1", "A"); ok {
		t.Fatal("deleted uppercase mark resurrected after restart")
	}
}

func TestMarksStore_ListMergesTiers(t *testing.T) {
	persist := marksPersistForTest(t)
	s := newMarksStore()
	s.persist = persist
	s.persistAll = true

	for _, letter := range []string{"a", "b", "A", "B"} {
		if err := s.Set("T1", letter, testMark(letter, "C1", "1.0")); err != nil {
			t.Fatalf("Set %s: %v", letter, err)
		}
	}

	got := s.List("T1")
	if len(got) != 4 {
		t.Fatalf("list = %+v, want 4 entries", got)
	}
	wantOrder := "abAB"
	for i, m := range got {
		if m.Letter != string(wantOrder[i]) {
			t.Fatalf("list order = %q, want %q (lowercase then uppercase)", lettersOf(got), wantOrder)
		}
	}
}

// An uppercase mark with no persistence backend must surface the
// failure: it has no session copy, so a silent drop would make the
// mark appear set when it does not exist.
func TestMarksStore_Set_UppercaseWithoutPersistReportsError(t *testing.T) {
	s := newMarksStore() // persist stays nil
	if err := s.Set("T1", "A", testMark("A", "C1", "1.0")); err == nil {
		t.Fatal("expected an error for an uppercase mark with no persistence backend")
	}
	// A lowercase mark still works session-only.
	if err := s.Set("T1", "a", testMark("a", "C1", "1.0")); err != nil {
		t.Fatalf("Set a: %v", err)
	}
}

func lettersOf(marks []Mark) string {
	out := ""
	for _, m := range marks {
		out += m.Letter
	}
	return out
}
