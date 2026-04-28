package emoji

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "emoji-widths-test.json")

	original := &Cache{
		Version:     1,
		Terminal:    "ghostty_1.0.0",
		ProbedAt:    "2026-04-27T16:00:00Z",
		CodemapHash: "abc123",
		Widths: map[string]int{
			"❤️":      1,
			"👍":       2,
			"🕵🏻\u200d♂️": 2,
		},
	}

	if err := SaveCache(path, original); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", loaded.Version, original.Version)
	}
	if loaded.Terminal != original.Terminal {
		t.Errorf("Terminal: got %q, want %q", loaded.Terminal, original.Terminal)
	}
	if loaded.CodemapHash != original.CodemapHash {
		t.Errorf("CodemapHash: got %q, want %q", loaded.CodemapHash, original.CodemapHash)
	}
	if len(loaded.Widths) != len(original.Widths) {
		t.Errorf("Widths length: got %d, want %d", len(loaded.Widths), len(original.Widths))
	}
	for k, v := range original.Widths {
		if loaded.Widths[k] != v {
			t.Errorf("Widths[%q]: got %d, want %d", k, loaded.Widths[k], v)
		}
	}
}

func TestLoadCacheMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadCache(filepath.Join(dir, "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got %v", err)
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCache(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
}

func TestCachePath(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/test-cache")
	got := CachePath("ghostty_1.0.0")
	want := "/tmp/test-cache/slk/emoji-widths-ghostty_1.0.0.json"
	if got != want {
		t.Errorf("CachePath: got %q, want %q", got, want)
	}
}

func TestCachePathFallback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := CachePath("kitty")
	want := "/home/test/.cache/slk/emoji-widths-kitty.json"
	if got != want {
		t.Errorf("CachePath fallback: got %q, want %q", got, want)
	}
}

func TestCachePathSanitizesKey(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp")
	// Terminal key with characters that could break a filename
	got := CachePath("Some Terminal/v1.0")
	want := "/tmp/slk/emoji-widths-Some_Terminal_v1.0.json"
	if got != want {
		t.Errorf("CachePath: got %q, want %q", got, want)
	}
}

func TestCodemapHashStable(t *testing.T) {
	// Same input must produce same hash.
	m1 := map[string]string{":a:": "A", ":b:": "B"}
	m2 := map[string]string{":b:": "B", ":a:": "A"} // different iteration order
	h1 := codemapHash(m1)
	h2 := codemapHash(m2)
	if h1 != h2 {
		t.Errorf("codemapHash not stable across map iteration: %q vs %q", h1, h2)
	}

	// Different input must produce different hash.
	m3 := map[string]string{":a:": "A", ":c:": "C"}
	h3 := codemapHash(m3)
	if h1 == h3 {
		t.Errorf("codemapHash collision: %q", h1)
	}
}
