package cache

import (
	"testing"
)

func testMark(letter string) Mark {
	return Mark{
		WorkspaceID: "T1",
		Letter:      letter,
		ChannelID:   "C1",
		MessageTS:   "1700000001.000000",
		ChannelName: "general",
		AuthorName:  "alice",
		Excerpt:     "hello world",
	}
}

func TestUpsertMark_ReplacesInPlace(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertMark("T1", "a", testMark("a")); err != nil {
		t.Fatalf("UpsertMark A: %v", err)
	}
	// Overwrite the same letter: previous location is discarded.
	replaced := testMark("a")
	replaced.ChannelID = "C2"
	replaced.MessageTS = "1700000002.000000"
	if err := db.UpsertMark("T1", "a", replaced); err != nil {
		t.Fatalf("UpsertMark B: %v", err)
	}

	marks, err := db.ListMarks("T1")
	if err != nil {
		t.Fatalf("ListMarks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("len(marks) = %d, want 1", len(marks))
	}
	if marks[0].ChannelID != "C2" || marks[0].MessageTS != "1700000002.000000" {
		t.Fatalf("mark after overwrite = %+v, want the new location", marks[0])
	}
}

func TestListMarks_PerWorkspace(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, letter := range []string{"a", "b", "A"} {
		if err := db.UpsertMark("T1", letter, testMark(letter)); err != nil {
			t.Fatalf("UpsertMark %s: %v", letter, err)
		}
	}
	if err := db.UpsertMark("T2", "a", testMark("a")); err != nil {
		t.Fatalf("UpsertMark T2: %v", err)
	}

	t1, err := db.ListMarks("T1")
	if err != nil {
		t.Fatalf("ListMarks T1: %v", err)
	}
	if len(t1) != 3 {
		t.Fatalf("T1 marks = %d, want 3", len(t1))
	}
	t2, err := db.ListMarks("T2")
	if err != nil {
		t.Fatalf("ListMarks T2: %v", err)
	}
	if len(t2) != 1 || t2[0].Letter != "a" {
		t.Fatalf("T2 marks = %+v, want only the T2 'a'", t2)
	}
	// ListMarks is ordered by letter (SQLite BINARY collation:
	// codepoint order, so uppercase before lowercase).
	if got := t1[0].Letter + t1[1].Letter + t1[2].Letter; got != "Aab" {
		t.Fatalf("T1 letter order = %q, want Aab (SQLite codepoint order)", got)
	}
}

func TestDeleteMark(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertMark("T1", "A", testMark("A")); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteMark("T1", "A"); err != nil {
		t.Fatalf("DeleteMark: %v", err)
	}
	// Deleting an unset letter is harmless.
	if err := db.DeleteMark("T1", "A"); err != nil {
		t.Fatalf("DeleteMark again: %v", err)
	}
	marks, err := db.ListMarks("T1")
	if err != nil {
		t.Fatalf("ListMarks: %v", err)
	}
	if len(marks) != 0 {
		t.Fatalf("marks after delete = %+v, want none", marks)
	}
}

func TestUpsertMark_RejectsEmptyKey(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertMark("", "a", testMark("a")); err == nil {
		t.Fatal("expected an error for an empty workspace_id")
	}
	if err := db.UpsertMark("T1", "", testMark("a")); err == nil {
		t.Fatal("expected an error for an empty letter")
	}
}
