package cache

import (
	"testing"
)

func TestRecordAndGetFrecentEmoji(t *testing.T) {
	db := setupDBWithWorkspace(t)

	if err := db.RecordEmojiUse("thumbsup"); err != nil {
		t.Fatalf("RecordEmojiUse: %v", err)
	}
	if err := db.RecordEmojiUse("thumbsup"); err != nil {
		t.Fatalf("RecordEmojiUse: %v", err)
	}
	if err := db.RecordEmojiUse("rocket"); err != nil {
		t.Fatalf("RecordEmojiUse: %v", err)
	}

	results, err := db.GetFrecentEmoji(10)
	if err != nil {
		t.Fatalf("GetFrecentEmoji: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 frecent emoji, got %d", len(results))
	}
	if results[0] != "thumbsup" {
		t.Errorf("expected thumbsup first, got %s", results[0])
	}
	if results[1] != "rocket" {
		t.Errorf("expected rocket second, got %s", results[1])
	}
}

func TestGetFrecentEmojiLimit(t *testing.T) {
	db := setupDBWithWorkspace(t)

	emojis := []string{"a", "b", "c", "d", "e"}
	for _, e := range emojis {
		if err := db.RecordEmojiUse(e); err != nil {
			t.Fatalf("RecordEmojiUse(%s): %v", e, err)
		}
	}

	results, err := db.GetFrecentEmoji(3)
	if err != nil {
		t.Fatalf("GetFrecentEmoji: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestGetFrecentEmojiEmpty(t *testing.T) {
	db := setupDBWithWorkspace(t)

	results, err := db.GetFrecentEmoji(10)
	if err != nil {
		t.Fatalf("GetFrecentEmoji: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}
