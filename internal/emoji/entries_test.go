package emoji

import (
	"strings"
	"testing"
)

func TestBuildEntries_BuiltinsHaveTrimmedDisplay(t *testing.T) {
	entries := BuildEntries(nil)
	// Find :rocket:
	var rocket *EmojiEntry
	for i := range entries {
		if entries[i].Name == "rocket" {
			rocket = &entries[i]
			break
		}
	}
	if rocket == nil {
		t.Fatal("expected :rocket: among built-in entries")
	}
	if rocket.Display == "" {
		t.Error("expected non-empty display for :rocket:")
	}
	if strings.HasSuffix(rocket.Display, " ") {
		t.Errorf("display should be trimmed, got %q", rocket.Display)
	}
}

func TestBuildEntries_AlphabeticalOrder(t *testing.T) {
	entries := BuildEntries(nil)
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name > entries[i].Name {
			t.Fatalf("entries not sorted: %q before %q", entries[i-1].Name, entries[i].Name)
		}
	}
}

func TestBuildEntries_AliasToBuiltinResolves(t *testing.T) {
	customs := map[string]string{
		"thumbsup_alt": "alias:+1",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "thumbsup_alt")
	if got == nil {
		t.Fatal("expected thumbsup_alt entry")
	}
	// :+1: is "👍" in kyokomi codemap. We don't hard-code the glyph, just
	// require non-empty and not the placeholder.
	if got.Display == "" || got.Display == placeholderGlyph {
		t.Errorf("expected resolved glyph, got %q", got.Display)
	}
}

func TestBuildEntries_AliasChained(t *testing.T) {
	customs := map[string]string{
		"a": "alias:b",
		"b": "alias:c",
		"c": "alias:+1",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "a")
	if got == nil {
		t.Fatal("expected entry a")
	}
	if got.Display == placeholderGlyph || got.Display == "" {
		t.Errorf("expected chained alias to resolve, got %q", got.Display)
	}
}

func TestBuildEntries_AliasCycleFallsBackToPlaceholder(t *testing.T) {
	customs := map[string]string{
		"a": "alias:b",
		"b": "alias:a",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "a")
	if got == nil {
		t.Fatal("expected entry a")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected placeholder for cycle, got %q", got.Display)
	}
}

func TestBuildEntries_AliasToUnknownFallsBackToPlaceholder(t *testing.T) {
	customs := map[string]string{
		"orphan": "alias:nonexistent_emoji_name_xyz",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "orphan")
	if got == nil {
		t.Fatal("expected entry orphan")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected placeholder for unknown alias target, got %q", got.Display)
	}
}

func TestBuildEntries_URLCustomUsesPlaceholder(t *testing.T) {
	customs := map[string]string{
		"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "partyparrot")
	if got == nil {
		t.Fatal("expected entry partyparrot")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected placeholder for URL-backed custom, got %q", got.Display)
	}
}

func TestBuildEntries_CustomShadowsBuiltin(t *testing.T) {
	customs := map[string]string{
		"rocket": "https://emoji.slack-edge.com/T1/rocket/xyz.gif",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "rocket")
	if got == nil {
		t.Fatal("expected entry rocket")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected custom rocket to shadow built-in (placeholder), got %q", got.Display)
	}
	// And there should be exactly one :rocket: entry, not two.
	count := 0
	for _, e := range entries {
		if e.Name == "rocket" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 :rocket: entry after dedupe, got %d", count)
	}
}

func findEntry(entries []EmojiEntry, name string) *EmojiEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}
