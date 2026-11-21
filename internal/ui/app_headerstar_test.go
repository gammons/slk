package ui

// The header's star glyph: the active channel's star state, drawn at
// the header line's right end (star-conversations design §Mouse), and
// clickable there to toggle it through the same service call /star
// uses.
import (
	"errors"
	"github.com/gammons/slk/internal/ui/wintree"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// starApp is an 120x24 app on C1 with the channel service recording
// SetStarred calls, plus one rendered frame so the layout bands and the
// message pane's chrome exist for hit-testing. Extra opts override the
// size (last wins).
func starApp(t *testing.T, extra ...testOpt) (*App, *[]bool) {
	t.Helper()
	a := newTestApp(t, append([]testOpt{
		withSize(120, 24), withActiveChannel("C1"),
		withMessages(messages.MessageItem{TS: "1.0", UserName: "alice", Text: "hello"}),
		withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}),
	}, extra...)...)
	calls := &[]bool{}
	fns := channelFuncsForTest(a)
	fns.SetStarred = func(teamID ids.TeamID, channelID ids.ChannelID, starred bool) core.Cmd {
		*calls = append(*calls, starred)
		return func() core.Msg {
			return ChannelStarSetMsg{TeamID: string(teamID), ChannelID: string(channelID), Starred: starred}
		}
	}
	setChannelFuncsForTest(a, fns)
	_ = a.View()
	_ = a.View()
	return a, calls
}

// headerStarX is the screen column of the header's star glyph.
func headerStarX(a *App) int {
	return a.layout.sidebarEnd + 1 + a.messagepane.ChromeWidth() - 2
}

func TestHeaderStar_ShowsAndTogglesState(t *testing.T) {
	a, calls := starApp(t)
	if !strings.Contains(a.View().Content, "☆") {
		t.Fatal("the header does not show the hollow star for an unstarred channel")
	}

	x := headerStarX(a)
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if len(*calls) != 1 || !(*calls)[0] {
		t.Fatalf("SetStarred calls = %v; want one true from the glyph click", *calls)
	}
	if starred, ok := a.sidebar.IsItemStarred("C1"); !ok || !starred {
		t.Fatal("C1 is not starred after clicking the glyph")
	}
	if !strings.Contains(a.View().Content, "★") {
		t.Fatal("the header does not show the filled star after the click")
	}
	_ = cmd

	// Click again: un-star.
	_, cmd = a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if len(*calls) != 2 || (*calls)[1] {
		t.Fatalf("SetStarred calls = %v; want a second false from the glyph click", *calls)
	}
	if starred, ok := a.sidebar.IsItemStarred("C1"); !ok || starred {
		t.Fatal("C1 is still starred after the second click")
	}
	_ = cmd
}

// A click one column left of the glyph is a header click, not a star
// toggle: it must not dispatch SetStarred.
func TestHeaderStar_MissDoesNotToggle(t *testing.T) {
	a, calls := starApp(t)
	x := headerStarX(a) - 2
	_, _ = a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if len(*calls) != 0 {
		t.Fatalf("SetStarred calls = %v; want none for a click off the glyph", *calls)
	}
}

// The glyph follows the channel on screen through the shared retarget
// path: switching between a starred and an unstarred conversation
// redraws the header with the new channel's star, not the previous
// one's.
func TestHeaderStar_FollowsChannelSwitch(t *testing.T) {
	a, _ := starApp(t)
	// A second channel, starred.
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel", IsStarred: true},
	})
	_ = a.View()

	_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})
	if !strings.Contains(a.View().Content, "★") {
		t.Fatal("the header does not show the filled star after switching to the starred channel")
	}

	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	if !strings.Contains(a.View().Content, "☆") {
		t.Fatal("the header does not show the hollow star after switching back to the unstarred channel")
	}
}

// A star reply for a workspace the user has since switched away from
// settles without touching the sidebar now on screen: the row in the
// other workspace is the echo's business, not this reply's.
func TestHeaderStar_ReplyForSwitchedAwayWorkspace(t *testing.T) {
	a, _ := starApp(t)
	a.setStarredChannel("C1", true) // optimistic move on T1's sidebar

	// The user switches workspaces before the reply lands.
	a.activeTeamID = "T2"
	a.setChannels([]sidebar.ChannelItem{{ID: "C9", Name: "other", Type: "channel"}}, nil)

	inner := a.setStarredChannel // not used; the reply is built by hand
	_ = inner
	reply := ChannelStarSetMsg{TeamID: "T1", ChannelID: "C1", Starred: true, Seq: 1,
		Err: errors.New("refused")}
	a.Update(reply)

	if starred, ok := a.sidebar.IsItemStarred("C9"); ok && starred {
		t.Fatal("the switched-away reply touched the new workspace's row")
	}
	// C1 is not in this workspace's sidebar at all; the reply must not
	// have tried to restore anything onto it (no panic, no row added).
	if _, ok := a.sidebar.IsItemStarred("C1"); ok {
		t.Fatal("the switched-away reply added C1 to the new workspace's sidebar")
	}
}

// Every open window's glyph follows its own channel's star, not only
// the focused pane's.
func TestHeaderStar_SplitWindowsEachFollowTheirChannel(t *testing.T) {
	// 160x40: a side-by-side split needs the room (a refused split
	// returns its toast cmd and leaves one window).
	a, _ := starApp(t, withSize(160, 40))
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel", IsStarred: true},
	})
	// The selection round trip is what writes the first window's tree
	// record (withActiveChannel alone does not), so the unfocused
	// window's channel is known after the split.
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.View()

	if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
		t.Fatalf("splitWindow refused: %v", cmd)
	}
	_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})
	_ = a.View()

	// Star C1 from wherever we are; both windows' glyphs must be right:
	// C1's pane fills, C2's pane keeps its star.
	a.setStarredChannel("C1", true)
	view := a.View().Content
	if !strings.Contains(view, "★") {
		t.Fatal("no filled star in the split after starring C1")
	}
	c1starred, _, _ := a.sidebar.ItemStarState("C1")
	c2starred, _, _ := a.sidebar.ItemStarState("C2")
	if !c1starred || !c2starred {
		t.Fatalf("C1=%v C2=%v; want both starred so both glyphs draw filled", c1starred, c2starred)
	}
	for _, id := range a.wins.Leaves() {
		if model := a.winModels[id]; model != nil && !model.HeaderStarred() {
			t.Fatal("an open window's header glyph did not follow its channel's star")
		}
	}
}

// The glyph keeps working with a split open, and only in the focused
// window: the hit test is translated into that window's rect, so a
// click on the other window's header (which PanelAt maps onto the live
// pane) does not toggle.
func TestHeaderStar_ClickWithSplitOpen(t *testing.T) {
	a, calls := starApp(t, withSize(160, 40))
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel", IsStarred: true},
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.View()
	if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
		t.Fatalf("splitWindow refused: %v", cmd)
	}
	_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})
	_ = a.View()

	rect := a.layout.focusedWinRect
	if rect.W == 0 {
		t.Fatal("the focused window's rect was not recorded during render")
	}
	model := a.winModels[a.focusedWin]
	if model == nil {
		t.Fatal("the focused window has no model")
	}
	// The glyph's pane-local column inside the focused window, back to
	// screen coordinates (panelAt's frame: sidebarEnd + 1, +1 row).
	glyphPX := rect.X + model.ChromeWidth() - 2
	glyphX := a.layout.sidebarEnd + 1 + glyphPX
	glyphY := 1 + rect.Y

	// A click on the OTHER window's header, same row, its own
	// glyph-ish column: must not toggle (the split guard swallows it).
	otherX := a.layout.sidebarEnd + 1 + 2
	_, _ = a.Update(tea.MouseClickMsg{X: otherX, Y: glyphY, Button: tea.MouseLeft})
	if len(*calls) != 0 {
		t.Fatalf("SetStarred calls = %v; want none for a click on the unfocused window", *calls)
	}

	_, _ = a.Update(tea.MouseClickMsg{X: glyphX, Y: glyphY, Button: tea.MouseLeft})
	// The focused window shows C2, starred in the fixture, so the click
	// un-stars it.
	if len(*calls) != 1 || (*calls)[0] {
		t.Fatalf("SetStarred calls = %v; want one false (un-starring the starred C2) from the focused window's glyph", *calls)
	}
}

// With the LEFT window focused in a side-by-side split, a click on the
// RIGHT window's header must not toggle the left conversation's star:
// the hit test is bounded by the focused chrome's width, so a column
// past it (where the right window's header lives) is a miss.
func TestHeaderStar_ClickOnRightWindowWithLeftFocused(t *testing.T) {
	a, calls := starApp(t, withSize(160, 40))
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.View()
	if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
		t.Fatalf("splitWindow refused: %v", cmd)
	}
	// The right window is focused after the split; move focus back to
	// the left so the right window is the unfocused one.
	_, _ = a.Update(keyCode(tea.KeyLeft))
	_ = a.View()

	rect := a.layout.focusedWinRect
	if rect.W == 0 {
		t.Fatal("the focused window's rect was not recorded during render")
	}
	// A column inside the RIGHT window's header, past the focused
	// window's right edge.
	rightX := a.layout.sidebarEnd + 1 + rect.X + rect.W + 5
	_, _ = a.Update(tea.MouseClickMsg{X: rightX, Y: 1 + rect.Y, Button: tea.MouseLeft})
	if len(*calls) != 0 {
		t.Fatalf("SetStarred calls = %v; want none for a click on the unfocused right window", *calls)
	}
}

// A long conversation name wraps in a narrow pane; the star stays on
// the FIRST line (where HitTestHeaderStar accepts clicks) and every
// wrapped line keeps the chrome's exact width.
func TestHeaderStar_WrappedTitleKeepsGlyphOnFirstLine(t *testing.T) {
	// 46 wide: after the sidebar and borders the pane title wraps a
	// long name to several lines.
	a, _ := starApp(t, withSize(46, 24))
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "an extremely long conversation name that wraps", Type: "channel"},
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "an extremely long conversation name that wraps", Type: "channel"})
	view := a.View().Content
	if !strings.Contains(view, "☆") {
		t.Fatal("the header does not show the star")
	}

	// The glyph's column on the first header line, per HitTestHeaderStar.
	x := headerStarX(a)
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("the glyph did not respond on a wrapped title; the star must stay on the first line")
	}
}
