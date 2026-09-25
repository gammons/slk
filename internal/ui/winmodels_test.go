package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

func TestWinModels_RootWindowHasModelAndPointerInvariant(t *testing.T) {
	a := newWideTestApp(t)
	if a.winModels[a.focusedWin] == nil {
		t.Fatal("root window must have a model at construction")
	}
	if a.messagepane != a.winModels[a.focusedWin] {
		t.Fatal("messagepane must point at the focused window's model")
	}
}

func TestSplitWindow_NewWindowGetsSeededClone(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello", Timestamp: "1:00 PM"},
	})
	src := a.messagepane
	_ = a.splitWindow(wintree.SplitSideBySide)
	if a.messagepane == src {
		t.Fatal("focused model must be the NEW window's model after split")
	}
	if got := len(a.messagepane.Messages()); got != 1 {
		t.Fatalf("new window should be seeded with the source's messages, got %d", got)
	}
	if a.winModels[a.focusedWin] != a.messagepane {
		t.Fatal("pointer invariant broken after split")
	}
}

func TestFocusWindow_IsPointerSwapNoDispatch(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	first := a.focusedWin
	_ = a.splitWindow(wintree.SplitSideBySide)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "ops", Type: "channel"})
	// Focus back: NO ChannelSelectedMsg dispatch (per-window models),
	// but active-channel context retargets to the window's channel.
	cmd := a.focusWindow(first)
	if cmd != nil {
		t.Fatal("focusWindow must not dispatch channel selection in Phase 3")
	}
	if a.activeChannelID != "C1" {
		t.Fatalf("activeChannelID = %q, want C1 (focused window's channel)", a.activeChannelID)
	}
	if a.messagepane != a.winModels[first] {
		t.Fatal("messagepane must follow focus")
	}
}

// TestFocusWindow_ClearsStrandedSyncingIndicator: a window-focus
// change must clear the syncing indicator left by a tier-2 verify
// fetch that hasn't landed yet — the SetSyncing(false) in the
// MessagesLoadedMsg arm is gated on the then-active channel, so
// without a retarget-time clear the "○" glyph would persist until
// the next channel selection. ("○" is unambiguous here: the only
// other producer is the "○ Away" presence segment, and presence is
// unset in the test app.)
func TestFocusWindow_ClearsStrandedSyncingIndicator(t *testing.T) {
	a, w1, _ := twoWindowApp(t)
	a.statusbar.SetSyncing(true) // simulate in-flight tier-2 verify on C2
	_ = a.focusWindow(w1)
	if out := ansi.Strip(a.statusbar.View(200)); strings.Contains(out, "○") {
		t.Fatalf("syncing indicator must clear on window focus change:\n%s", out)
	}
}

func TestCloseWindow_EvictsModel(t *testing.T) {
	a := newWideTestApp(t)
	_ = a.splitWindow(wintree.SplitSideBySide)
	closed := a.focusedWin
	_ = a.closeWindow()
	if _, ok := a.winModels[closed]; ok {
		t.Fatal("closed window's model must be evicted")
	}
	if len(a.winModels) != a.wins.Len() {
		t.Fatalf("winModels len %d != tree len %d", len(a.winModels), a.wins.Len())
	}
}

func TestOnlyWindow_EvictsOthers(t *testing.T) {
	a := newWideTestApp(t)
	_ = a.splitWindow(wintree.SplitSideBySide)
	_ = a.splitWindow(wintree.SplitStacked)
	a.onlyWindow()
	if len(a.winModels) != 1 {
		t.Fatalf("winModels len = %d, want 1 after :only", len(a.winModels))
	}
	if a.winModels[a.focusedWin] != a.messagepane {
		t.Fatal("pointer invariant broken after :only")
	}
}

// TestFocusSwap_RenderShowsFocusedWindowAtEqualVersions guards the
// msgTop render cache against cross-window collisions. Per-window
// models have INDEPENDENT version counters, so after a pointer-swap
// focus change the new focused model can present the exact same
// (version, width, height, layoutKey) tuple the cache stored for the
// OTHER window's frame — serving window B's pixels while window A is
// focused. The cache key must therefore mix in the window identity.
// The test forces the collision: equal dims (same frame), equal
// layout bits (same focus/view/theme), and version counters driven
// to exact equality via InvalidateCache bumps.
func TestFocusSwap_RenderShowsFocusedWindowAtEqualVersions(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	first := a.focusedWin
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "alpha-marker", Timestamp: "1:00 PM"},
	})
	_ = a.splitWindow(wintree.SplitSideBySide)
	second := a.focusedWin
	_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "ops", Type: "channel"})
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "beta-marker", Timestamp: "1:00 PM"},
	})

	// Render the messages panel with a FIXED frame so both renders see
	// identical dims — the same inputs the panel cache keys on.
	frame := a.computeFrame()
	render := func() string { return ansi.Strip(a.renderMessagesRegion(frame, 0, false)) }

	if out := render(); !strings.Contains(out, "beta-marker") {
		t.Fatalf("sanity: focused second window should render beta-marker:\n%s", out)
	}

	// Swap focus to the first window and align its version counter to
	// the exact version the cache stored for the second window's
	// frame. Pre-set the focused flag so render-time SetFocused can't
	// bump past the target.
	_ = a.focusWindow(first)
	a.winModels[first].SetFocused(true)
	target := a.renderCache.msgTop.panelVersion
	for a.winModels[first].Version() < target {
		a.winModels[first].InvalidateCache()
	}
	if a.winModels[first].Version() != target {
		// Overshot: raise the second window's counter to match, re-warm
		// the cache at the new version, and swap back.
		for a.winModels[second].Version() < a.winModels[first].Version() {
			a.winModels[second].InvalidateCache()
		}
		_ = a.focusWindow(second)
		_ = render()
		_ = a.focusWindow(first)
		target = a.renderCache.msgTop.panelVersion
	}
	if a.winModels[first].Version() != target {
		t.Fatalf("test setup: could not align versions (first=%d, cached=%d)",
			a.winModels[first].Version(), target)
	}

	out := render()
	if strings.Contains(out, "beta-marker") {
		t.Fatalf("focused first window served the second window's cached frame:\n%s", out)
	}
	if !strings.Contains(out, "alpha-marker") {
		t.Fatalf("focused first window must render its own content:\n%s", out)
	}
}

// TestSplitWindow_ReactionUpdateDoesNotLeakAcrossClones guards the
// seed clone's deep copy: UpdateReaction mutates Reactions elements
// in place (and the remove path shifts the slice in place), so a
// clone sharing the source's Reactions backing array corrupts the
// source's view when either window receives a reaction event.
func TestSplitWindow_ReactionUpdateDoesNotLeakAcrossClones(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	a.messagepane.SetMessages([]messages.MessageItem{{
		TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello", Timestamp: "1:00 PM",
		Reactions: []messages.ReactionItem{
			{Emoji: "thumbsup", Count: 1, UserIDs: []string{"U1"}},
			{Emoji: "tada", Count: 1, UserIDs: []string{"U1"}},
		},
	}})
	src := a.messagepane
	_ = a.splitWindow(wintree.SplitSideBySide)
	clone := a.messagepane

	// Removing thumbsup entirely shifts the clone's Reactions slice in
	// place; with a shared backing array the source would see
	// [tada, tada]. Incrementing tada writes the element in place.
	clone.UpdateReaction("1.0", "thumbsup", "U1", true)
	clone.UpdateReaction("1.0", "tada", "U2", false)

	got := src.Messages()[0].Reactions
	if len(got) != 2 || got[0].Emoji != "thumbsup" || got[1].Emoji != "tada" {
		t.Fatalf("source reactions corrupted by clone mutation: %+v", got)
	}
	if got[1].Count != 1 {
		t.Fatalf("source tada count mutated by clone reaction: %+v", got[1])
	}
}

func TestWorkspaceSwitch_RebuildsModels(t *testing.T) {
	a := newWideTestApp(t)
	_ = a.splitWindow(wintree.SplitSideBySide)
	_, _ = a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Other", Channels: nil})
	if len(a.winModels) != 1 {
		t.Fatalf("winModels len = %d, want 1 after workspace switch", len(a.winModels))
	}
	if a.messagepane == nil || a.messagepane != a.winModels[a.focusedWin] {
		t.Fatal("pointer invariant broken after workspace switch")
	}
}

func TestFocusWindow_ClearsActiveSearchOnOldModel(t *testing.T) {
	// In-channel search state (App match list + model highlights +
	// statusbar segment) is focused-pane state; a window focus swap
	// must clear it on the OUTGOING model, or n/N would step a match
	// list belonging to a different pane and stale highlights would
	// stay baked into the old window's render.
	a, w1, _ := twoWindowApp(t)
	a.search = &activeSearch{}
	a.messagepane.SetSearchTerms([]string{"needle"})
	a.statusbar.SetSearch("/needle  1/3")
	old := a.messagepane
	oldVer := old.Version()
	_ = a.focusWindow(w1)
	if a.search != nil {
		t.Fatal("focus swap must clear the active search match list")
	}
	if old.Version() == oldVer {
		t.Fatal("outgoing model's search highlights were not cleared (no version bump)")
	}
	if out := a.statusbar.View(200); strings.Contains(out, "/needle") {
		t.Fatalf("statusbar search segment must clear on focus swap:\n%s", out)
	}
}
