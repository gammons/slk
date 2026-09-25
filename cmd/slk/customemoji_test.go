package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui"
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

// fakeEmojiLister records ListCustomEmoji calls so a test can assert
// the fetch actually happened, not merely that state changed.
type fakeEmojiLister struct {
	mu     sync.Mutex
	calls  int
	result map[string]string
	err    error
}

func (f *fakeEmojiLister) ListCustomEmoji(_ context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

func (f *fakeEmojiLister) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureEmojiSender records messages dispatched into the tea loop.
type captureEmojiSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *captureEmojiSender) Send(m tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
}

func (c *captureEmojiSender) snapshot() []tea.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]tea.Msg(nil), c.msgs...)
}

// TestFetchWorkspaceEmoji_RunsEvenWhenBootstrapPublishedASubset is the
// regression guard for this whole change.
//
// conversations.view returns only the emoji the restored conversation
// uses. The previous code treated a non-empty CustomEmoji() as "already
// fetched" and returned early, so every other channel rendered custom
// emoji as literal :name:. Re-adding that short-circuit must fail here.
func TestFetchWorkspaceEmoji_RunsEvenWhenBootstrapPublishedASubset(t *testing.T) {
	wctx := &WorkspaceContext{}
	// Bootstrap publishes the one emoji the restored channel used.
	wctx.SetCustomEmoji(map[string]string{"from-boot": "https://example.test/boot.png"})

	api := &fakeEmojiLister{result: map[string]string{
		"from-boot":    "https://example.test/boot.png",
		"party-parrot": "https://example.test/parrot.gif",
	}}
	sender := &captureEmojiSender{}

	fetchWorkspaceEmoji(context.Background(), wctx, api, sender, "T1")

	if got := api.callCount(); got != 1 {
		t.Fatalf("ListCustomEmoji calls = %d, want 1 — a non-empty bootstrap subset must NOT suppress emoji.list", got)
	}
	if got := wctx.CustomEmoji(); len(got) != 2 || got["party-parrot"] == "" {
		t.Errorf("CustomEmoji() = %v, want the full workspace set", got)
	}
}

// A failed emoji.list must leave the bootstrap subset intact rather
// than clearing it: a partial set renders more emoji than none.
func TestFetchWorkspaceEmoji_ErrorKeepsBootstrapSubset(t *testing.T) {
	wctx := &WorkspaceContext{}
	wctx.SetCustomEmoji(map[string]string{"from-boot": "https://example.test/boot.png"})

	api := &fakeEmojiLister{err: errors.New("ratelimited")}
	sender := &captureEmojiSender{}

	fetchWorkspaceEmoji(context.Background(), wctx, api, sender, "T1")

	if got := wctx.CustomEmoji()["from-boot"]; got != "https://example.test/boot.png" {
		t.Errorf("CustomEmoji()[from-boot] = %q, want the bootstrap entry preserved on error", got)
	}
	if msgs := sender.snapshot(); len(msgs) != 0 {
		t.Errorf("sent %d messages on error, want 0 — a failed fetch must not tell the UI to re-render", len(msgs))
	}
}

// The UI only picks up the new set when the follow-up message lands,
// and it must be tagged with the workspace it belongs to.
func TestFetchWorkspaceEmoji_SendsLoadedMsgForItsTeam(t *testing.T) {
	wctx := &WorkspaceContext{}
	api := &fakeEmojiLister{result: map[string]string{"party-parrot": "https://example.test/parrot.gif"}}
	sender := &captureEmojiSender{}

	fetchWorkspaceEmoji(context.Background(), wctx, api, sender, "T_OTHER")

	msgs := sender.snapshot()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want 1", len(msgs))
	}
	loaded, ok := msgs[0].(ui.CustomEmojisLoadedMsg)
	if !ok {
		t.Fatalf("message type = %T, want ui.CustomEmojisLoadedMsg", msgs[0])
	}
	if loaded.TeamID != "T_OTHER" {
		t.Errorf("TeamID = %q, want T_OTHER — an untagged message would repaint the wrong workspace", loaded.TeamID)
	}
	if loaded.CustomEmoji["party-parrot"] == "" {
		t.Errorf("CustomEmoji = %v, want the fetched set", loaded.CustomEmoji)
	}
}

// A nil sender must not panic: the fetch is best-effort and runs in a
// bare goroutine, so a panic there would take the process down.
func TestFetchWorkspaceEmoji_NilSenderDoesNotPanic(t *testing.T) {
	wctx := &WorkspaceContext{}
	api := &fakeEmojiLister{result: map[string]string{"a": "b"}}
	fetchWorkspaceEmoji(context.Background(), wctx, api, nil, "T1")
	if wctx.CustomEmoji()["a"] != "b" {
		t.Error("CustomEmoji() should still be published with a nil sender")
	}
}
