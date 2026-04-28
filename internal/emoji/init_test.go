package emoji

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitWithValidCache(t *testing.T) {
	resetWidthMap()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("TERM_PROGRAM", "test_terminal")
	t.Setenv("TERM_PROGRAM_VERSION", "1.0")

	// Pre-write a valid cache
	codemap := map[string]string{":a:": "a "}
	cachePath := CachePath("test_terminal_1.0")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	c := &Cache{
		Version:     CacheVersion,
		Terminal:    "test_terminal_1.0",
		ProbedAt:    "2026-04-27T16:00:00Z",
		CodemapHash: codemapHash(codemap),
		Widths:      map[string]int{"a": 1, "👍": 2},
	}
	if err := SaveCache(cachePath, c); err != nil {
		t.Fatal(err)
	}

	// Init with cache hit
	opts := InitOptions{
		Codemap:         codemap,
		PerProbeTimeout: 100,
		ProgressFunc:    nil,
		SkipProbe:       false,
		ForceProbe:      false,
	}
	loaded, probed, err := initWithIO(opts, nil, nil)
	if err != nil {
		t.Fatalf("initWithIO: %v", err)
	}
	if !loaded {
		t.Error("expected cache to be loaded")
	}
	if probed {
		t.Error("expected probe to be skipped (cache hit)")
	}
	if !IsCalibrated() {
		t.Error("expected IsCalibrated to be true")
	}
}

func TestInitWithStaleCache(t *testing.T) {
	resetWidthMap()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("TERM_PROGRAM", "test_terminal")
	t.Setenv("TERM_PROGRAM_VERSION", "1.0")

	// Write cache with WRONG codemap hash
	cachePath := CachePath("test_terminal_1.0")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	c := &Cache{
		Version:     CacheVersion,
		Terminal:    "test_terminal_1.0",
		ProbedAt:    "2026-04-27T16:00:00Z",
		CodemapHash: "stale-hash",
		Widths:      map[string]int{"a": 1},
	}
	if err := SaveCache(cachePath, c); err != nil {
		t.Fatal(err)
	}

	// Init with stale hash → should detect mismatch and not use cache.
	// Without a fake terminal, probe will fail; we expect SkipProbe behavior.
	opts := InitOptions{
		Codemap:         map[string]string{":a:": "a "},
		PerProbeTimeout: 100,
		SkipProbe:       true, // skip probe; just verify cache was rejected
	}
	loaded, probed, _ := initWithIO(opts, nil, nil)
	if loaded {
		t.Error("expected stale cache to be rejected")
	}
	if probed {
		t.Error("expected probe to be skipped (SkipProbe=true)")
	}
}

func TestInitSkipProbe(t *testing.T) {
	resetWidthMap()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	opts := InitOptions{
		Codemap:         map[string]string{":a:": "a "},
		PerProbeTimeout: 100,
		SkipProbe:       true,
	}
	loaded, probed, err := initWithIO(opts, nil, nil)
	if err != nil {
		t.Fatalf("expected no error with SkipProbe, got %v", err)
	}
	if loaded || probed {
		t.Error("expected neither loaded nor probed with SkipProbe and no cache")
	}
	if IsCalibrated() {
		t.Error("expected IsCalibrated to be false")
	}
}
