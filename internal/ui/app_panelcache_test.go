// internal/ui/app_panelcache_test.go
//
// Phase 0 characterization tests for panelCache. These pin the current
// behavior so an upcoming extraction (Phase 2: panelRenderCache) cannot
// silently change the cache-key semantics. panelCache is the per-panel
// memoization that returns a previously-rendered fully-wrapped panel
// string when nothing meaningful has changed.
package ui

import "testing"

func TestPanelCacheMissWhenInvalid(t *testing.T) {
	var c panelCache
	if c.hit(1, 10, 20, 0) {
		t.Fatal("zero-value panelCache must miss")
	}
}

func TestPanelCacheHitAfterStore(t *testing.T) {
	var c panelCache
	c.store("rendered", 7, 80, 24, 42)

	if !c.hit(7, 80, 24, 42) {
		t.Fatal("expected hit on exact key match")
	}
	if c.output != "rendered" {
		t.Errorf("output: want %q, got %q", "rendered", c.output)
	}
}

func TestPanelCacheMissOnAnyKeyChange(t *testing.T) {
	cases := []struct {
		name                           string
		version, width, height, layout int64
		w, h                           int
	}{
		{"different version", 9, 80, 24, 42, 80, 24},
		{"different width", 7, 81, 24, 42, 81, 24},
		{"different height", 7, 80, 25, 42, 80, 25},
		{"different layout", 7, 80, 24, 99, 80, 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c panelCache
			c.store("rendered", 7, 80, 24, 42)
			if c.hit(tc.version, tc.w, tc.h, tc.layout) {
				t.Errorf("expected miss when %s", tc.name)
			}
		})
	}
}

func TestPanelCacheStoreOverwritesPriorEntry(t *testing.T) {
	var c panelCache
	c.store("first", 1, 80, 24, 0)
	c.store("second", 2, 80, 24, 0)

	if c.hit(1, 80, 24, 0) {
		t.Error("stale key (version=1) should not hit after overwrite")
	}
	if !c.hit(2, 80, 24, 0) {
		t.Error("new key (version=2) should hit after overwrite")
	}
	if c.output != "second" {
		t.Errorf("output: want %q, got %q", "second", c.output)
	}
}

func TestBoolToInt(t *testing.T) {
	if got := boolToInt(true); got != 1 {
		t.Errorf("boolToInt(true) = %d, want 1", got)
	}
	if got := boolToInt(false); got != 0 {
		t.Errorf("boolToInt(false) = %d, want 0", got)
	}
}
