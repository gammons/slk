package main

import (
	"sync"
	"testing"
)

func TestWorkspaceContextCustomEmojiDefaultsEmpty(t *testing.T) {
	var wctx WorkspaceContext
	if got := wctx.CustomEmoji(); got == nil || len(got) != 0 {
		t.Errorf("CustomEmoji() before load = %v, want empty non-nil map", got)
	}
}

// SetCustomEmoji replaces rather than merges: emoji.list returns the
// workspace's whole set, so the bootstrap subset it lands on top of is
// a strict subset and merging would only keep stale entries alive.
func TestWorkspaceContextCustomEmojiReplaces(t *testing.T) {
	wctx := &WorkspaceContext{}
	wctx.SetCustomEmoji(map[string]string{"from-boot": "https://example.test/boot.png"})
	wctx.SetCustomEmoji(map[string]string{"from-list": "https://example.test/list.png"})

	got := wctx.CustomEmoji()
	if _, ok := got["from-boot"]; ok {
		t.Errorf("CustomEmoji() = %v, want the bootstrap entry replaced, not merged", got)
	}
	if got["from-list"] != "https://example.test/list.png" {
		t.Errorf("CustomEmoji()[from-list] = %q, want the emoji.list URL", got["from-list"])
	}
}

// The emoji.list fetch publishes the map from a background goroutine
// while the workspace-switch cmd reads it to build WorkspaceSwitchedMsg.
// Run under -race to catch a regression back to a plain map field.
func TestWorkspaceContextCustomEmojiConcurrentAccess(t *testing.T) {
	wctx := &WorkspaceContext{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			wctx.SetCustomEmoji(map[string]string{"party-parrot": "https://example.test/parrot.gif"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = wctx.CustomEmoji()["party-parrot"]
		}
	}()
	wg.Wait()
	if got := wctx.CustomEmoji()["party-parrot"]; got != "https://example.test/parrot.gif" {
		t.Errorf("CustomEmoji()[party-parrot] = %q, want the published URL", got)
	}
}
