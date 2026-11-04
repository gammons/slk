package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/linkpicker"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/channelpicker"
	"github.com/gammons/slk/internal/ui/help"
	"github.com/gammons/slk/internal/ui/marks"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/newmessagepicker"
	"github.com/gammons/slk/internal/ui/reactionsview"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/gammons/slk/internal/ui/workspacefinder"
)

// dialogGolden is one dialog or composer popup drawn over the standard
// fixture and pinned as testdata/golden/<name>.ansi. Phase 4 moves every
// dialog's chrome into internal/bubbles/chrome; these goldens are what
// prove the move changed nothing on screen (Phase 4 spec §6).
type dialogGolden struct {
	name string
	w, h int // 0: the base fixture's size
	// open puts the dialog on screen, after the fixture is built and
	// before the frame is drawn.
	open func(t *testing.T, a *App)
	// shown reports whether the dialog is open, so a scenario whose
	// dialog failed to open cannot be blessed as coverage.
	shown func(a *App) bool
	// want is text the dialog draws, checked on the stripped frame.
	// Empty for popups, whose rows also appear in the sidebar;
	// TestGolden_DialogsChangeTheFrame covers those.
	want string
}

// numbered returns n labels "<prefix> 01" … "<prefix> NN".
func numbered(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s %02d", prefix, i+1)
	}
	return out
}

// repeat calls f n times.
func repeat(n int, f func()) {
	for range n {
		f()
	}
}

// typeKeys sends each rune of s to a string-keyed HandleKey.
func typeKeys[R any](handle func(string) R, s string) {
	for _, r := range s {
		handle(string(r))
	}
}

// typeInCompose types s into the main composer through its own Update,
// which is what opens the @, # and : popups.
func typeInCompose(a *App, s string) {
	for _, r := range s {
		a.compose, _ = a.compose.Update(keyPress(r))
	}
}

// scrollingMarkRows is 14 marks, more than the list window holds, one
// with an excerpt too long for the box.
func scrollingMarkRows() []marks.Row {
	out := make([]marks.Row, 0, 14)
	for i := range 14 {
		r := marks.Row{
			Letter:      string(rune('a' + i)),
			ChannelName: fmt.Sprintf("channel-%02d", i+1),
			AuthorName:  "alice",
			Excerpt:     fmt.Sprintf("message %02d", i+1),
		}
		if i == 6 {
			r.Excerpt = "an excerpt much too long for the marks box to hold on a single row"
		}
		out = append(out, r)
	}
	return out
}

const longThemeName = "Theme with a name far too long to fit inside the switcher box"

func scrollingFinderItems() []core.ChannelFinderItem {
	out := make([]core.ChannelFinderItem, 0, 15)
	for i := range 15 {
		name := fmt.Sprintf("channel-%02d", i+1)
		if i == 7 {
			name = "channel-with-a-name-much-too-long-for-the-finder-box-to-hold"
		}
		typ := "channel"
		if i == 3 {
			typ = "private"
		}
		out = append(out, core.ChannelFinderItem{
			ID: fmt.Sprintf("C%02d", i+10), Name: name, Type: typ,
			Joined: i != 5, LastVisited: int64(100 - i),
		})
	}
	return out
}

func searchItems(n int) []searchresults.Item {
	out := make([]searchresults.Item, 0, n)
	for i := range n {
		out = append(out, searchresults.Item{
			ChannelID: "C1", ChannelName: fmt.Sprintf("eng-%d", i+1),
			UserName: "alice", TS: fmt.Sprintf("%d.0", i+1),
			Text: "we deploy the service on friday afternoons and then watch the " +
				"dashboards for a long while before anyone is allowed to go home",
			Timestamp: "Mar 14", IsDM: i == 2,
		})
	}
	return out
}

func dialogGoldens() []dialogGolden {
	themeShown := func(a *App) bool { return a.themeSwitcher.IsVisible() }
	wsShown := func(a *App) bool { return a.workspaceFinder.IsVisible() }
	presShown := func(a *App) bool { return a.presenceMenu.IsVisible() }
	finderShown := func(a *App) bool { return a.channelFinder.IsVisible() }
	marksShown := func(a *App) bool { return a.marksOverlay.IsVisible() }
	reactShown := func(a *App) bool { return a.reactionPicker.IsVisible() }
	newMsgShown := func(a *App) bool { return a.newMessagePicker.IsVisible() }
	helpShown := func(a *App) bool { return a.help.IsVisible() }
	searchShown := func(a *App) bool { return a.searchResults.IsVisible() }
	linkShown := func(a *App) bool { return a.linkPicker.IsVisible() }

	openHelp := func(t *testing.T, a *App) {
		a.help.SetEntries(help.FromKeyMap(a.keys))
		a.help.SetFooter("slk golden")
		a.help.Open()
		repeat(25, func() { a.help.HandleKey("j") })
		a.SetMode(ModeHelp)
	}
	openLinks := func(t *testing.T, a *App) {
		a.openLinkPicker("Open link", []linkpicker.Item{
			{URL: "https://example.com/a", Label: "Example"},
			{URL: "https://example.com/a/very/long/path/that/will/not/fit/inside/the/link/picker/box?query=1"},
			{URL: "https://slack.com/archives/C1/p1", InApp: true},
			{URL: "https://example.com/report.pdf", Label: "report.pdf", Detail: "1.2 MB"},
		})
		a.linkPicker.HandleKey("j")
		a.SetMode(ModeLinkPicker)
	}
	openSearch := func(a *App) {
		a.searchResults.Open()
		typeKeys(a.searchResults.HandleKey, "deploy")
		a.searchResults.HandleKey("enter") // input -> loading
		a.SetMode(ModeWorkspaceSearch)
	}

	return []dialogGolden{
		{
			name: "dialog_theme_switcher", want: "Switch Theme", shown: themeShown,
			open: func(t *testing.T, a *App) {
				items := numbered("Theme", 16)
				items[5] = longThemeName // inside the scrolled window [2, 14)
				a.themeSwitcher.SetItems(items)
				a.themeSwitcher.Open()
				repeat(13, func() { a.themeSwitcher.HandleKey("down") })
				a.SetMode(ModeThemeSwitcher)
			},
		},
		{
			name: "dialog_theme_switcher_no_match", want: "No matching themes", shown: themeShown,
			open: func(t *testing.T, a *App) {
				a.themeSwitcher.SetItems(numbered("Theme", 3))
				a.themeSwitcher.Open()
				typeKeys(a.themeSwitcher.HandleKey, "zz")
				a.SetMode(ModeThemeSwitcher)
			},
		},
		{
			name: "dialog_workspace_finder", want: "Switch Workspace", shown: wsShown,
			open: func(t *testing.T, a *App) {
				items := make([]workspacefinder.Item, 0, 14)
				for i, n := range numbered("Workspace", 14) {
					items = append(items, workspacefinder.Item{
						ID: fmt.Sprintf("T%02d", i+1), Name: n, Initials: fmt.Sprintf("W%d", (i+1)%10),
					})
				}
				a.workspaceFinder.SetItems(items)
				a.workspaceFinder.Open()
				repeat(11, func() { a.workspaceFinder.HandleKey("down") })
				a.SetMode(ModeWorkspaceFinder)
			},
		},
		{
			name: "dialog_workspace_finder_no_match", want: "No matching workspaces", shown: wsShown,
			open: func(t *testing.T, a *App) {
				a.workspaceFinder.SetItems([]workspacefinder.Item{{ID: "T1", Name: "Acme", Initials: "A"}})
				a.workspaceFinder.Open()
				typeKeys(a.workspaceFinder.HandleKey, "zz")
				a.SetMode(ModeWorkspaceFinder)
			},
		},
		{
			name: "dialog_presence_menu", want: "Status — Acme", shown: presShown,
			open: func(t *testing.T, a *App) {
				a.presenceMenu.OpenWith("Acme", "active", false, time.Time{})
				repeat(3, func() { a.presenceMenu.HandleKey("down") })
				a.SetMode(ModePresenceMenu)
			},
		},
		{
			name: "dialog_presence_menu_no_match", want: "No matching options", shown: presShown,
			open: func(t *testing.T, a *App) {
				a.presenceMenu.OpenWith("Acme", "active", false, time.Time{})
				typeKeys(a.presenceMenu.HandleKey, "zz")
				a.SetMode(ModePresenceMenu)
			},
		},
		{
			name: "dialog_presence_snooze", want: "Snooze for how many minutes?",
			shown: func(a *App) bool { return a.mode == ModePresenceCustomSnooze },
			open: func(t *testing.T, a *App) {
				a.SetMode(ModePresenceCustomSnooze)
				a.presence.AppendSnoozeDigit("4")
				a.presence.AppendSnoozeDigit("5")
			},
		},
		{
			name: "dialog_channel_finder_scroll", want: "Switch Channel", shown: finderShown,
			open: func(t *testing.T, a *App) {
				a.SetChannelFinderItems(scrollingFinderItems())
				a.channelFinder.Open()
				repeat(12, func() { a.channelFinder.HandleKey("down") })
				a.SetMode(ModeChannelFinder)
			},
		},
		{
			name: "dialog_channel_finder_no_match", want: "No matching channels", shown: finderShown,
			open: func(t *testing.T, a *App) {
				a.SetChannelFinderItems(goldenFinderItems())
				a.channelFinder.Open()
				typeKeys(a.channelFinder.HandleKey, "zqz")
				a.SetMode(ModeChannelFinder)
			},
		},
		{
			name: "dialog_marks_scroll", want: "Marks", shown: marksShown,
			open: func(t *testing.T, a *App) {
				a.marksOverlay.SetRows(scrollingMarkRows())
				a.marksOverlay.Open()
				repeat(11, func() { a.marksOverlay.HandleKey("down") })
				a.SetMode(ModeMarks)
			},
		},
		{
			// Too short for ten rows: the window shrinks so the box and
			// the selected row stay on screen.
			name: "dialog_marks_short", w: 80, h: 12, want: "Marks", shown: marksShown,
			open: func(t *testing.T, a *App) {
				a.marksOverlay.SetRows(scrollingMarkRows())
				a.marksOverlay.Open()
				repeat(11, func() { a.marksOverlay.HandleKey("down") })
				a.SetMode(ModeMarks)
			},
		},
		{
			name: "dialog_marks_empty", want: "No marks set", shown: marksShown,
			open: func(t *testing.T, a *App) {
				a.marksOverlay.SetRows(nil)
				a.marksOverlay.Open()
				a.SetMode(ModeMarks)
			},
		},
		{
			name: "dialog_reaction_picker", want: "Add Reaction", shown: reactShown,
			open: func(t *testing.T, a *App) {
				a.reactionPicker.Open("C1", "1.0", []string{"smile"})
				typeKeys(a.reactionPicker.HandleKey, "sm")
				repeat(12, func() { a.reactionPicker.HandleKey("down") })
				a.SetMode(ModeReactionPicker)
			},
		},
		{
			name: "dialog_reaction_picker_no_match", want: "No matching emoji", shown: reactShown,
			open: func(t *testing.T, a *App) {
				a.reactionPicker.Open("C1", "1.0", nil)
				typeKeys(a.reactionPicker.HandleKey, "zqzq")
				a.SetMode(ModeReactionPicker)
			},
		},
		{
			name: "dialog_new_message", want: "New message", shown: newMsgShown,
			open: func(t *testing.T, a *App) {
				users := make([]newmessagepicker.User, 0, 12)
				for i, n := range numbered("User", 12) {
					users = append(users, newmessagepicker.User{
						ID: fmt.Sprintf("U%02d", i+1), DisplayName: n,
						Username: fmt.Sprintf("user%02d", i+1), IsExternal: i == 4,
					})
				}
				a.newMessagePicker.SetUsers(users)
				a.newMessagePicker.Open()
				a.newMessagePicker.HandleKey(" ") // first user becomes a pill
				repeat(11, func() { a.newMessagePicker.HandleKey("down") })
				a.SetMode(ModeNewMessage)
			},
		},
		{
			name: "dialog_new_message_no_match", want: `No users match "zqz"`, shown: newMsgShown,
			open: func(t *testing.T, a *App) {
				a.newMessagePicker.SetUsers([]newmessagepicker.User{{ID: "U1", DisplayName: "alice", Username: "alice"}})
				a.newMessagePicker.Open()
				typeKeys(a.newMessagePicker.HandleKey, "zqz")
				a.SetMode(ModeNewMessage)
			},
		},
		{name: "dialog_help", want: "Keybindings", shown: helpShown, open: openHelp},
		{
			name: "dialog_help_search_no_match", want: "No matching keybindings", shown: helpShown,
			open: func(t *testing.T, a *App) {
				a.help.SetEntries(help.FromKeyMap(a.keys))
				a.help.Open()
				a.help.HandleKey("/")
				typeKeys(func(k string) struct{} { a.help.HandleKey(k); return struct{}{} }, "zqz")
				a.SetMode(ModeHelp)
			},
		},
		{name: "dialog_help_narrow", w: 40, h: 20, want: "Keybindings", shown: helpShown, open: openHelp},
		{
			name: "dialog_search_results", want: "Search Workspace", shown: searchShown,
			open: func(t *testing.T, a *App) {
				openSearch(a)
				a.searchResults.SetHighlightTerms([]string{"deploy"})
				a.searchResults.SetResults(searchItems(8), 20)
				repeat(5, func() { a.searchResults.HandleKey("down") })
			},
		},
		{
			name: "dialog_search_loading", want: "Searching…", shown: searchShown,
			open: func(t *testing.T, a *App) { openSearch(a) },
		},
		{
			name: "dialog_search_no_results", want: "No results", shown: searchShown,
			open: func(t *testing.T, a *App) {
				openSearch(a)
				a.searchResults.SetResults(nil, 0)
			},
		},
		{name: "dialog_link_picker", want: "Open link", shown: linkShown, open: openLinks},
		{name: "dialog_link_picker_narrow", w: 40, h: 20, want: "Open link", shown: linkShown, open: openLinks},
		{
			name: "dialog_reactions_view", want: "Reactions",
			shown: func(a *App) bool { return a.reactionsView.IsVisible() },
			open: func(t *testing.T, a *App) {
				var groups []reactionsview.ReactionGroup
				for i, e := range []string{"thumbsup", "tada", "eyes", "rocket", "heart", "fire"} {
					g := reactionsview.ReactionGroup{Emoji: e, Users: numbered("user", 4), Count: 4}
					if i == 2 {
						g.Count = 9
					}
					groups = append(groups, g)
				}
				a.reactionsView.Open(groups)
				a.SetMode(ModeReactionsView)
				_ = a.View() // HandleKey scrolls only after a draw has set the limit
				repeat(3, func() { a.reactionsView.HandleKey("j") })
			},
		},
		{
			name: "dialog_confirm", want: "Leave #general?",
			shown: func(a *App) bool { return a.confirmPrompt.IsVisible() },
			open: func(t *testing.T, a *App) {
				a.openConfirmPrompt("Leave #general?",
					"You will stop receiving messages from #general. You can rejoin at any time from the channel finder.",
					func() tea.Msg { return nil })
			},
		},
		{
			name:  "popup_mention",
			shown: func(a *App) bool { return a.compose.MentionPickerView(80) != "" },
			open: func(t *testing.T, a *App) {
				a.compose.SetUsers([]mentionpicker.User{
					{ID: "U1", DisplayName: "alice", Username: "alice", InChannel: true},
					{ID: "U2", DisplayName: "bob", Username: "bob", InChannel: true},
					{ID: "U3", DisplayName: "carol", Username: "carol"},
					{ID: "U4", DisplayName: "dave", Username: "dave", IsExternal: true, InChannel: true},
				})
				a.SetMode(ModeInsert)
				_ = a.compose.Focus()
				typeInCompose(a, "@")
			},
		},
		{
			name:  "popup_channel",
			shown: func(a *App) bool { return a.compose.ChannelPickerView(80) != "" },
			open: func(t *testing.T, a *App) {
				a.compose.SetChannels([]channelpicker.Channel{
					{ID: "C1", Name: "general", Type: "channel"},
					{ID: "C2", Name: "engineering", Type: "channel"},
					{ID: "G1", Name: "secret-plans", Type: "private"},
					{ID: "D1", Name: "bob", Type: "dm"},
				})
				a.SetMode(ModeInsert)
				_ = a.compose.Focus()
				typeInCompose(a, "#")
			},
		},
		{
			name:  "popup_emoji",
			shown: func(a *App) bool { return a.compose.EmojiPickerView(80) != "" },
			open: func(t *testing.T, a *App) {
				a.SetMode(ModeInsert)
				_ = a.compose.Focus()
				typeInCompose(a, ":sm")
			},
		},
	}
}

// renderDialogGolden builds the fixture at d's size, opens d and returns
// the frame, failing when the dialog is not actually on screen.
func renderDialogGolden(t *testing.T, d dialogGolden) string {
	t.Helper()
	opts := goldenFixtureOpts()
	if d.w != 0 {
		opts = append(opts, withSize(d.w, d.h))
	}
	a := newGoldenApp(t, opts...)
	d.open(t, a)
	frame := a.View().Content
	if !d.shown(a) {
		t.Fatalf("%s: the dialog is not open; the golden would pin the bare fixture", d.name)
	}
	if d.want != "" && !strings.Contains(stripANSI(frame), d.want) {
		t.Fatalf("%s: %q is not on screen", d.name, d.want)
	}
	return frame
}

func TestGolden_Dialogs(t *testing.T) {
	for _, d := range dialogGoldens() {
		t.Run(d.name, func(t *testing.T) {
			compareGolden(t, d.name, renderDialogGolden(t, d))
		})
	}
}

// TestGolden_DialogsChangeTheFrame proves each scenario draws something:
// its frame differs from the same fixture with nothing opened.
func TestGolden_DialogsChangeTheFrame(t *testing.T) {
	for _, d := range dialogGoldens() {
		t.Run(d.name, func(t *testing.T) {
			opts := goldenFixtureOpts()
			if d.w != 0 {
				opts = append(opts, withSize(d.w, d.h))
			}
			bare := newGoldenApp(t, opts...).View().Content
			if renderDialogGolden(t, d) == bare {
				t.Fatalf("%s renders the same frame as the bare fixture", d.name)
			}
		})
	}
}

func TestGolden_DialogsArePairwiseDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, d := range dialogGoldens() {
		frame := renderDialogGolden(t, d)
		if other, ok := seen[frame]; ok {
			t.Errorf("%q and %q render byte-identical frames", other, d.name)
		}
		seen[frame] = d.name
	}
}
