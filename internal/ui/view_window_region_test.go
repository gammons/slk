package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

// renderRegion renders the messages region exactly as App.View does,
// without depending on the tea.View wrapper API.
func renderRegion(a *App) string {
	frame := a.computeFrame()
	return a.renderWindowsRegion(frame, 0, false)
}

func TestRegion_SingleWindowUnchanged(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	frame := a.computeFrame()
	multi := a.renderWindowsRegion(frame, 0, false)
	direct := a.renderMessagesRegion(frame, 0, false)
	if multi != direct {
		t.Fatal("single-window region must be byte-identical to the direct messages render")
	}
}

func TestRegion_SplitRendersLiveContentInBothWindows(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	_, _ = a.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: testMessageItems(2)})
	out := ansi.Strip(renderRegion(a))
	// Unfocused window (C1/general) must show real message text, not a
	// placeholder; focused window shows ops.
	if !strings.Contains(out, "msg-1") {
		t.Fatalf("unfocused window should render its channel's messages:\n%s", out)
	}
	if strings.Contains(out, "(no channel)") {
		t.Fatal("no placeholders may remain in Phase 3")
	}
}

func TestRegion_UnfocusedWindowUpdatesOnNewMessage(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	_, _ = a.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "9.0", UserID: "U9", UserName: "zoe", Text: "live-update-proof", Timestamp: "1:00 PM",
	}})
	out := ansi.Strip(renderRegion(a))
	if !strings.Contains(out, "live-update-proof") {
		t.Fatalf("unfocused window must re-render new content:\n%s", out)
	}
}

func TestRegion_UnfocusedPaneCacheInvalidatesOnContent(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	_ = renderRegion(a) // warm caches
	_, _ = a.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "9.1", UserID: "U9", UserName: "zoe", Text: "second-proof", Timestamp: "1:01 PM",
	}})
	out := ansi.Strip(renderRegion(a))
	if !strings.Contains(out, "second-proof") {
		t.Fatal("stale cached frame served after content change")
	}
}

// TestRegion_UnfocusedPaneDoesNotRewrapModelLines is the root-cause
// regression for the unfocused-pane garbling: lipgloss v2's
// Style.Width is border-inclusive, so the old
// UnfocusedBorder.Width(W-2).Render(view) gave the model's exactly
// (W-2)-cell lines a (W-4)-cell budget. Any line with real content in
// its last 2 cells wrapped, injecting a remainder row after it —
// and the scrollbar (rendered whenever messages overflow the pane)
// puts a glyph in the LAST cell of every line, so in practice every
// line of a scrolled-back pane garbled; exactSize's MaxHeight then
// clamped the doubled row count, eating the bottom border. Lines
// whose final 2 cells were blank padding were "only" silently
// truncated by 2 cells (trailing whitespace overflow is dropped, not
// wrapped) — which is why a sparse pane looked fine and dimension
// tests stayed green.
//
// The contract: every line the model emits at the pane's inner dims
// must appear INTACT on a single output row between the side border
// verticals, and the bottom border edge must survive.
func TestRegion_UnfocusedPaneDoesNotRewrapModelLines(t *testing.T) {
	a, w1, _ := twoWindowApp(t)
	// Enough messages to overflow the pane: the scrollbar then puts
	// real content in every line's last cell (the wrap trigger).
	_, _ = a.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: testMessageItems(60)})
	out := ansi.Strip(renderRegion(a))

	// Reference: the model's own render at the SAME inner dims the
	// renderer uses — derive w1's rect exactly as renderWindowsRegion
	// does (focused window is w2/C2 "ops": empty, no interference).
	frame := a.computeFrame()
	bounds := wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
	r := a.wins.ComputeRects(bounds)[w1]
	bare := ansi.Strip(a.winModels[w1].ViewBare(r.H-2, r.W-2))
	for i, line := range strings.Split(bare, "\n") {
		if !strings.Contains(out, "│"+line+"│") {
			t.Fatalf("model line %d not intact in unfocused pane (re-wrapped or truncated):\nwant bracketed |%s|", i, line)
		}
	}
	bottom := "╰" + strings.Repeat("─", r.W-2) + "╯"
	if !strings.Contains(out, bottom) {
		t.Fatal("unfocused pane bottom border missing (row overflow clamped it away)")
	}
}

func TestRegion_SplitOutputDimensionsStable(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	before := renderRegion(a)
	_ = a.splitWindow(wintree.SplitSideBySide)
	after := renderRegion(a)
	if lipgloss.Height(before) != lipgloss.Height(after) {
		t.Fatalf("row count changed after split: %d -> %d", lipgloss.Height(before), lipgloss.Height(after))
	}
	if lipgloss.Width(before) != lipgloss.Width(after) {
		t.Fatalf("width changed after split: %d -> %d", lipgloss.Width(before), lipgloss.Width(after))
	}
	// EVERY row must be exactly the region width (lipgloss.Width only
	// reports the max). A row-count-preserving clamp that eats a
	// pane's bottom border, or a pane row short of its rect width,
	// slips past the aggregate checks above but not this.
	wantW := lipgloss.Width(before)
	for i, line := range strings.Split(after, "\n") {
		if w := ansi.StringWidth(line); w != wantW {
			t.Fatalf("split row %d width = %d, want %d", i, w, wantW)
		}
	}
}

// TestRegion_SurvivesHardShrinkAfterSplits guards the resize-after-
// split crash: with several side-by-side columns, a hard terminal
// shrink produces leaf rects too narrow for the messages panel's
// chrome (W-2 < 2), which used to flow a negative width into
// borderedTopPane's strings.Repeat and panic. The render must
// survive AND keep the exact region dimensions.
func TestRegion_SurvivesHardShrinkAfterSplits(t *testing.T) {
	a := NewApp()
	a.width = 400
	a.height = 50
	for i := 0; i < 3; i++ {
		if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
			t.Fatalf("split %d refused at width 400", i)
		}
	}
	// Hard shrink: must not panic, must keep exact region dimensions.
	a.width, a.height = 30, 10
	frame := a.computeFrame()
	out := a.renderWindowsRegion(frame, 0, false) // panics before the fix
	wantW := frame.MsgWidth + frame.MsgBorder
	if lipgloss.Height(out) != frame.ContentHeight {
		t.Fatalf("height = %d, want %d", lipgloss.Height(out), frame.ContentHeight)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w != wantW {
			t.Fatalf("line %d width = %d, want %d", i, w, wantW)
		}
	}
}

// TestRegion_SurvivesHardShrinkAfterStackedSplits is the vertical
// twin: ContentHeight < window count yields zero-extent (H=0) rects,
// which exactSize treats as "unset" (natural height) — breaking the
// region height contract unless zero-extent leaves are skipped.
func TestRegion_SurvivesHardShrinkAfterStackedSplits(t *testing.T) {
	a := NewApp()
	a.width = 200
	a.height = 50
	for i := 0; i < 3; i++ {
		if cmd := a.splitWindow(wintree.SplitStacked); cmd != nil {
			t.Fatalf("split %d refused at height 50", i)
		}
	}
	// ContentHeight (3) < window count (4) → at least one H=0 rect.
	a.width, a.height = 30, 4
	frame := a.computeFrame()
	out := a.renderWindowsRegion(frame, 0, false)
	wantW := frame.MsgWidth + frame.MsgBorder
	if lipgloss.Height(out) != frame.ContentHeight {
		t.Fatalf("height = %d, want %d", lipgloss.Height(out), frame.ContentHeight)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w != wantW {
			t.Fatalf("line %d width = %d, want %d", i, w, wantW)
		}
	}
}

func TestRegion_CloseRestoresSingleWindowPath(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.splitWindow(wintree.SplitStacked)
	_ = a.closeWindow()
	if a.wins.Len() != 1 {
		t.Fatalf("Len = %d, want 1", a.wins.Len())
	}
	frame := a.computeFrame()
	multi := a.renderWindowsRegion(frame, 0, false)
	direct := a.renderMessagesRegion(frame, 0, false)
	if multi != direct {
		t.Fatal("after closing back to one window the region must take the direct single-window path")
	}
}
