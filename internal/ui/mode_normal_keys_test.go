package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/help"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/workspace"
)

// ---------------------------------------------------------------------
// Fixtures
//
// handleNormalMode reads almost everything it does out of App state
// that newTestApp does not set by default: focusedPanel starts at
// PanelSidebar (app.go:508), threadVisible starts false, and the
// message pane starts empty. Every row below therefore states its
// precondition explicitly and t.Fatal's when it does not take -- a
// silently-absent precondition turns a row into a vacuous pass (the
// failure mode Task 15 hit).
// ---------------------------------------------------------------------

// normalMessages is the fixture message buffer. Index 4 (the LAST
// message, which is what messages.Model.SetMessages selects --
// messages/model.go:691) is deliberately the "rich" one: it carries a
// reaction, a link and both a downloadable file and an image
// attachment, so the message-op arms (L/o/d/O/r/R) have a target
// without any extra setup. Rows that need a *plain* message call
// SelectByIndex to move off it.
func normalMessages() []messages.MessageItem {
	msgs := testMessageItems(5)
	// Slack mrkdwn angle-bracket form: messages.ExtractLinks matches the
	// renderer's regexes (messages/links.go:3-5), so a bare
	// "https://..." would extract nothing.
	msgs[4].Text = "look at <https://example.com/a>"
	msgs[4].Reactions = []messages.ReactionItem{
		{Emoji: "thumbsup", Count: 1, UserIDs: []string{"U2"}},
		{Emoji: "eyes", Count: 2, UserIDs: []string{"U2", "U3"}},
	}
	msgs[4].Attachments = []messages.Attachment{
		{Kind: "file", Name: "report.pdf", DownloadURL: "https://files/report.pdf", Size: 2048},
		{Kind: "image", Name: "pic.png", FileID: "F1"},
	}
	return msgs
}

// normalOpts is the shared construction bundle: two sidebar channels,
// the fixture messages, C1 active, and a render so a.layout's page
// bands are populated (pageSize/halfPageSize read them --
// app.go:1428).
func normalOpts() []testOpt {
	return []testOpt{
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel"},
		),
		withMessages(normalMessages()...),
		withActiveChannel("C1"),
		withRender(),
	}
}

// unreadOpts is a SEPARATE bundle from normalOpts, used only by the
// a/A (NextUnread/PrevUnread) rows.
//
// Those two arms differ solely in the direction they pass to
// jumpToUnread (mode_normal.go:285 / :288), so distinguishing them
// requires a sidebar where forward and backward land on *different*
// channels. normalOpts' two channels cannot do that: from C1 the
// forward and backward walks both reach C2 (sidebar/model.go:377-385
// wraps modulo n, and with n==2 both offsets are the same row).
//
// Adding channels and unread state to normalOpts instead would push
// them into the ~85 rows that share it -- the sidebar nav, section
// header, enter-select and render-dependent rows all read the channel
// list, and unread state additionally changes what View() draws. A
// dedicated bundle keeps the blast radius at two rows.
//
// Section is set explicitly on every item: the staleness filter in
// rebuildFilter (sidebar/model.go:985) exempts sectioned items
// outright (staleness.go:49-51), so the walk order is the input order
// and cannot drift with the clock.
func unreadOpts() []testOpt {
	return []testOpt{
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel", Section: "Eng"},
			sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel", Section: "Eng"},
			sidebar.ChannelItem{ID: "C3", Name: "design", Type: "channel", Section: "Eng"},
			sidebar.ChannelItem{ID: "C4", Name: "ops", Type: "channel", Section: "Eng"},
		),
		withMessages(normalMessages()...),
		withActiveChannel("C1"),
		withRender(),
	}
}

// seedUnreads marks C2 and C4 (the active channel's forward and
// backward neighbours in unreadOpts' walk order) unread, then asserts
// the two directions really do resolve to different channels. Without
// that assertion the a/A rows could both be green against a handler
// that passed the same direction twice, which is precisely the mutation
// they exist to catch.
//
// SetReadStateReader does not rebuild the sidebar's filtered list
// (sidebar/model.go:301-305), which is fine here: unreadOpts' items are
// sectioned, so the list built at SetChannels time already contains all
// four in input order.
func seedUnreads(t *testing.T, a *App) {
	t.Helper()
	a.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C2": {HasUnread: true},
			"C4": {HasUnread: true},
		}
	})
	next, _, _, ok := a.sidebar.NextUnread("C1", 1)
	if !ok || next != "C2" {
		t.Fatalf("precondition: NextUnread(C1, +1) = %q ok=%v, want C2", next, ok)
	}
	prev, _, _, ok := a.sidebar.NextUnread("C1", -1)
	if !ok || prev != "C4" {
		t.Fatalf("precondition: NextUnread(C1, -1) = %q ok=%v, want C4", prev, ok)
	}
}

// focusMessages puts focus on the channel message pane and asserts a
// message is actually selected. Most message-op arms return nil
// silently when selectedMessageContext fails (app.go:2960), so without
// this check a "cmd != nil" row would report a handler bug when the
// real fault was the fixture.
func focusMessages(t *testing.T, a *App) {
	t.Helper()
	a.focusedPanel = PanelMessages
	if _, ok := a.messagepane.SelectedMessage(); !ok {
		t.Fatal("precondition: messages pane has no selected message")
	}
}

// focusMessageAt focuses the message pane on a specific index.
func focusMessageAt(t *testing.T, a *App, i int) {
	t.Helper()
	focusMessages(t, a)
	a.messagepane.SelectByIndex(i)
	if got := a.messagepane.SelectedIndex(); got != i {
		t.Fatalf("precondition: selected index = %d, want %d", got, i)
	}
}

// focusThreadPanel loads a two-reply thread, marks it visible and
// focuses it. SetThread selects the newest reply (thread/model.go:322),
// so SelectedReply is non-nil -- asserted, because every thread-side
// message op no-ops on nil.
func focusThreadPanel(t *testing.T, a *App) {
	t.Helper()
	a.threadPanel.SetThread(
		messages.MessageItem{TS: "10.0", UserID: "U1", UserName: "alice", Text: "parent", ThreadTS: "10.0"},
		[]messages.MessageItem{
			{TS: "11.0", UserID: "U1", UserName: "alice", Text: "reply-1"},
			{TS: "12.0", UserID: "U2", UserName: "bob", Text: "reply-2",
				Reactions: []messages.ReactionItem{
					{Emoji: "thumbsup", Count: 1, UserIDs: []string{"U1"}},
					{Emoji: "eyes", Count: 1, UserIDs: []string{"U1"}},
				}},
		},
		"C1", "10.0")
	a.threadVisible = true
	a.focusedPanel = PanelThread
	if a.threadPanel.SelectedReply() == nil {
		t.Fatal("precondition: thread panel has no selected reply")
	}
}

// selectSidebarSectionHeader walks the sidebar nav from the top until
// the cursor lands on a section header, returning its key. The section
// set is derived from the seeded channels, so hard-coding a name here
// would silently stop matching if that derivation changed; scanning and
// failing loudly is the durable form.
func selectSidebarSectionHeader(t *testing.T, a *App) string {
	t.Helper()
	a.sidebar.GoToTop()
	for range 50 {
		if name, ok := a.sidebar.IsSectionHeaderSelected(); ok {
			return name
		}
		a.sidebar.MoveDown()
	}
	t.Fatal("precondition: no sidebar section header is reachable")
	return ""
}

// seedActiveSearch installs an in-channel `/` search with two matches
// positioned at index 0, so `n` steps forward without wrapping and `N`
// wraps. Mirrors what reduceChannelSearchResults builds.
func seedActiveSearch(t *testing.T, a *App) {
	t.Helper()
	msgs := a.messagepane.Messages()
	if len(msgs) < 3 {
		t.Fatalf("precondition: %d messages, want at least 3 for a match list", len(msgs))
	}
	a.search = &activeSearch{
		query:   "msg",
		terms:   []string{"msg"},
		matches: []string{msgs[1].TS, msgs[2].TS},
		idx:     0,
	}
	a.statusbar.SetSearch("/msg  1/2")
}

// TestNormalModeKeys characterizes handleNormalMode (mode_normal.go:39).
//
// One row per `case key.Matches(...)` arm (49 of them) plus the three
// early guards ahead of the switch and the numeric workspace-switch
// default arm. Rows record CURRENT behaviour; where that behaviour
// looks wrong the row carries a `// BUG?:` and still asserts reality.
//
// Reachability note, which the runner's doc comment spells out in
// full: runKeyCases calls dispatchModeKey directly. For normal mode
// only ONE key is genuinely unreachable in production --  ctrl+c,
// which App.handleKey intercepts at app.go:708 and turns into the quit
// prompt before dispatch. ctrl+c has no arm in handleNormalMode at all
// (keys.go:96 binds Quit to ctrl+c only), so there is no arm here to
// mis-characterize. No reducer in the app.go:609 chain claims
// tea.KeyMsg, so nothing else is pre-empted; the remaining
// pre-emptions (image-preview swallow at app.go:643, bootstrap gate at
// :715) are state-conditional rather than key-specific.
func TestNormalModeKeys(t *testing.T) {
	// The sidebar width rows need to observe the widthSaveFn callback,
	// which is only reachable from inside setup. Recording into these
	// (rows run sequentially -- nothing in this repo calls t.Parallel)
	// is what separates "the width changed" from "the width changed AND
	// the persistence hook fired", the second half of both arms.
	var grewTo, shrankTo []int
	// Carried from setup to assert for the space/section-header row.
	var toggledSection string
	var wantCollapsed bool

	runKeyCases(t, ModeNormal, []keyCase{
		// -------------------------------------------------------------
		// Early guards, ahead of the switch (mode_normal.go:42-54).
		// -------------------------------------------------------------
		{
			// mode_normal.go:42. The chord is disarmed and the
			// transient "ctrl+w …" hint reverts to the resting hint
			// BEFORE the chord key is delegated. Asserting the hint
			// (not just the flag) is what separates "consumed" from
			// "never armed": the flag starts false either way.
			name: "ctrl+w s: the pending-chord guard consumes the next key and restores the help hint",
			opts: []testOpt{withSize(200, 50)},
			setup: func(t *testing.T, a *App) {
				_ = dispatchModeKey(a, keyMod('w', tea.ModCtrl))
				if !a.pendingWinCmd {
					t.Fatal("precondition: ctrl+w did not arm the chord")
				}
				if !strings.Contains(statusbarText(a), "ctrl+w") {
					t.Fatalf("precondition: statusbar = %q, want the ctrl+w hint", statusbarText(a))
				}
			},
			key:      keyPress('s'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.pendingWinCmd {
					t.Error("pendingWinCmd still armed")
				}
				if got := a.wins.Len(); got != 2 {
					t.Errorf("window count = %d, want 2 (ctrl+w s splits stacked)", got)
				}
				if got := statusbarText(a); !strings.Contains(got, "? for keybindings") {
					t.Errorf("statusbar = %q, want the restored default help hint", got)
				}
			},
		},
		{
			// mode_normal.go:49. While reaction nav is active on the
			// message pane the whole normal switch is bypassed: esc
			// exits the sub-state instead of closing the thread /
			// clearing search.
			name: "esc in message reaction-nav is routed to handleReactionNav, not the normal esc arm",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				a.messagepane.EnterReactionNav()
				if !a.messagepane.ReactionNavActive() {
					t.Fatal("precondition: reaction nav did not activate")
				}
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.messagepane.ReactionNavActive() {
					t.Error("reaction nav still active after esc")
				}
			},
		},
		{
			// mode_normal.go:52, the thread-panel twin of the guard
			// above. `right` moves the reaction cursor; in the normal
			// switch `right` would be FocusNext instead, so the
			// focusedPanel assertion is what proves the guard ran.
			name: "right in thread reaction-nav is routed to handleThreadReactionNav, not the Right arm",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusThreadPanel(t, a)
				a.threadPanel.EnterReactionNav()
				if !a.threadPanel.ReactionNavActive() {
					t.Fatal("precondition: thread reaction nav did not activate")
				}
				if got, _ := a.threadPanel.SelectedReaction(); got != "thumbsup" {
					t.Fatalf("precondition: selected reaction = %q, want %q", got, "thumbsup")
				}
			},
			key:      keyCode(tea.KeyRight),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.focusedPanel != PanelThread {
					t.Errorf("focusedPanel = %v, want PanelThread: the guard should have swallowed Right", a.focusedPanel)
				}
				if got, _ := a.threadPanel.SelectedReaction(); got != "eyes" {
					t.Errorf("selected reaction = %q, want %q", got, "eyes")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 1: InsertMode `i` (mode_normal.go:57)
		// -------------------------------------------------------------
		{
			name:     "i enters insert mode and focuses the channel compose",
			opts:     normalOpts(),
			key:      keyPress('i'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.focusedPanel != PanelMessages {
					t.Errorf("focusedPanel = %v, want PanelMessages", a.focusedPanel)
				}
				// compose.Model exposes no Focused() getter, so the
				// focus itself is only observable as the non-nil cmd
				// Focus() returns; focusedPanel above is what says
				// WHICH compose was focused (mode_normal.go:64 vs :67).
				if cmd == nil {
					t.Error("cmd = nil, want compose.Focus()'s cmd")
				}
			},
		},
		{
			// mode_normal.go:63: with the thread panel focused, `i`
			// forces focus to the thread compose instead.
			name: "i with the thread panel focused focuses the thread compose",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusThreadPanel(t, a)
			},
			key:      keyPress('i'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.focusedPanel != PanelThread {
					t.Errorf("focusedPanel = %v, want PanelThread", a.focusedPanel)
				}
				if cmd == nil {
					t.Error("cmd = nil, want threadCompose.Focus()'s cmd")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 2: CommandMode `:` (mode_normal.go:70)
		// -------------------------------------------------------------
		{
			name:     ": enters command mode with an empty buffer and a bare prompt",
			opts:     normalOpts(),
			key:      keyPress(':'),
			wantMode: ModeCommand,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.cmdline != "" {
					t.Errorf("cmdline = %q, want empty", a.cmdline)
				}
				if got := statusbarText(a); !strings.Contains(got, ":") {
					t.Errorf("statusbar = %q, want it to show the ':' prompt", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 3: Escape (mode_normal.go:73)
		// -------------------------------------------------------------
		{
			// mode_normal.go:81 -- an active `/` search absorbs the
			// first esc and returns early, so the thread stays open.
			// Both halves are asserted: the early return is only
			// observable as "search cleared AND thread untouched".
			name: "esc with an active search clears the search and leaves the thread open",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				focusThreadPanel(t, a)
				a.focusedPanel = PanelMessages
				seedActiveSearch(t, a)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.search != nil {
					t.Error("a.search still set after esc")
				}
				if got := a.statusbar.Search(); got != "" {
					t.Errorf("statusbar search = %q, want empty", got)
				}
				if !a.threadVisible {
					t.Error("thread closed: the search branch must return before CloseThread")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The statusbar-only half of the same guard: a lingering
			// "no matches" segment with a.search == nil still absorbs
			// the esc.
			name: "esc with only a lingering search segment still absorbs the key",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusThreadPanel(t, a)
				a.statusbar.SetSearch("/nope  no matches")
				if a.search != nil {
					t.Fatal("precondition: a.search should be nil for this row")
				}
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.statusbar.Search(); got != "" {
					t.Errorf("statusbar search = %q, want empty", got)
				}
				if !a.threadVisible {
					t.Error("thread closed: the statusbar branch must return before CloseThread")
				}
			},
		},
		{
			name:     "esc with no search closes the thread",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadVisible {
					t.Error("thread still visible after esc")
				}
				if !a.threadPanel.IsEmpty() {
					t.Error("thread panel not cleared by CloseThread")
				}
				if a.focusedPanel != PanelMessages {
					t.Errorf("focusedPanel = %v, want PanelMessages after CloseThread", a.focusedPanel)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 4: WindowPrefix `ctrl+w` (mode_normal.go:92)
		// -------------------------------------------------------------
		{
			name:     "ctrl+w arms the window chord and shows the transient hint",
			opts:     normalOpts(),
			key:      keyMod('w', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.pendingWinCmd {
					t.Error("pendingWinCmd not armed")
				}
				if got := statusbarText(a); !strings.Contains(got, "ctrl+w") {
					t.Errorf("statusbar = %q, want the \"ctrl+w …\" hint", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 5: SearchMode `/` (mode_normal.go:97)
		// -------------------------------------------------------------
		{
			name: "/ opens the search prompt from the messages pane",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				a.searchInput = "stale"
			},
			key:      keyPress('/'),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.searchInput != "" {
					t.Errorf("searchInput = %q, want empty", a.searchInput)
				}
				if got := a.statusbar.Search(); got != "/" {
					t.Errorf("statusbar search = %q, want %q", got, "/")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// mode_normal.go:100 -- v1 scopes `/` to the channel pane.
			name:     "/ with the thread panel focused is a no-op",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyPress('/'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.statusbar.Search(); got != "" {
					t.Errorf("statusbar search = %q, want empty", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 6: SearchNext `n` (mode_normal.go:110)
		// -------------------------------------------------------------
		{
			name: "n steps to the next match and updates the status segment",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				seedActiveSearch(t, a)
			},
			key:      keyPress('n'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.search == nil {
					t.Fatal("a.search cleared")
				}
				if a.search.idx != 1 {
					t.Errorf("search idx = %d, want 1", a.search.idx)
				}
				if got := a.statusbar.Search(); got != "/msg  2/2" {
					t.Errorf("statusbar search = %q, want %q", got, "/msg  2/2")
				}
			},
		},
		{
			// The arm carries two extra conjuncts. Without an active
			// search `n` falls all the way through to the numeric
			// default arm and does nothing at all.
			name: "n without an active search falls through to the default arm",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				if a.search != nil {
					t.Fatal("precondition: a.search should be nil")
				}
			},
			key:      keyPress('n'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.statusbar.Search(); got != "" {
					t.Errorf("statusbar search = %q, want empty", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The `focusedPanel != PanelThread` conjunct: with an
			// active search but thread focus, `n` is inert.
			name: "n with the thread focused leaves the match index alone",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				seedActiveSearch(t, a)
				focusThreadPanel(t, a)
			},
			key:      keyPress('n'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.search.idx != 0 {
					t.Errorf("search idx = %d, want 0 (unchanged)", a.search.idx)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 7: SearchPrev `N` (mode_normal.go:113)
		// -------------------------------------------------------------
		{
			// idx starts at 0, so -1 wraps to the last match and the
			// handler batches a "Search wrapped" toast in.
			name: "N wraps backwards to the last match",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				seedActiveSearch(t, a)
			},
			key:      keyPress('N'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.search.idx != 1 {
					t.Errorf("search idx = %d, want 1 (wrapped)", a.search.idx)
				}
				if cmd == nil {
					t.Error("cmd = nil, want the batched \"Search wrapped\" toast")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 8: WorkspaceSearch `ctrl+f` (mode_normal.go:116)
		// -------------------------------------------------------------
		{
			name:     "ctrl+f opens the workspace search modal",
			opts:     normalOpts(),
			key:      keyMod('f', tea.ModCtrl),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.searchResults.IsVisible() {
					t.Error("workspace search modal not visible")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 9 & 10: Tab / shift+tab (mode_normal.go:121, :124)
		//
		// Both are exercised from PanelMessages with the thread panel
		// visible, the one focus state where FocusNext and FocusPrev
		// disagree (app.go:1709 vs :1740). From PanelSidebar they both
		// land on PanelMessages and no row could tell them apart.
		// -------------------------------------------------------------
		{
			name:     "tab moves focus messages -> thread",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusThreadPanel(t, a); focusMessages(t, a) },
			key:      keyCode(tea.KeyTab),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.focusedPanel != PanelThread {
					t.Errorf("focusedPanel = %v, want PanelThread", a.focusedPanel)
				}
			},
		},
		{
			name:     "shift+tab moves focus messages -> sidebar",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusThreadPanel(t, a); focusMessages(t, a) },
			key:      keyMod(tea.KeyTab, tea.ModShift),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.focusedPanel != PanelSidebar {
					t.Errorf("focusedPanel = %v, want PanelSidebar", a.focusedPanel)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 11: ToggleSidebar `ctrl+b` (mode_normal.go:127)
		// -------------------------------------------------------------
		{
			name: "ctrl+b hides the sidebar and moves focus off it",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if !a.sidebarVisible || a.focusedPanel != PanelSidebar {
					t.Fatalf("precondition: sidebarVisible=%v focusedPanel=%v, want true/PanelSidebar",
						a.sidebarVisible, a.focusedPanel)
				}
			},
			key:      keyMod('b', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.sidebarVisible {
					t.Error("sidebar still visible")
				}
				if a.focusedPanel != PanelMessages {
					t.Errorf("focusedPanel = %v, want PanelMessages", a.focusedPanel)
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 12 & 13: SidebarGrow `]` / SidebarShrink `[`
		// (mode_normal.go:130, :136)
		// -------------------------------------------------------------
		{
			name: "] widens the sidebar and notifies the width saver",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				grewTo = nil
				a.SetWidthSaver(func(w int) { grewTo = append(grewTo, w) })
				if a.widthSaveFn == nil {
					t.Fatal("precondition: width saver not wired")
				}
				if got := a.sidebar.Width(); got != sidebarBaseWidth(t) {
					t.Fatalf("precondition: sidebar width = %d, want the default %d", got, sidebarBaseWidth(t))
				}
			},
			key:      keyPress(']'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.sidebar.Width(); got <= sidebarBaseWidth(t) {
					t.Errorf("sidebar width = %d, want it grown past the default %d", got, sidebarBaseWidth(t))
				}
				if len(grewTo) != 1 {
					t.Fatalf("width saver called %d times, want 1", len(grewTo))
				}
				if grewTo[0] != a.sidebar.Width() {
					t.Errorf("width saver got %d, want the new width %d", grewTo[0], a.sidebar.Width())
				}
			},
		},
		{
			name: "[ narrows the sidebar and notifies the width saver",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				shrankTo = nil
				a.SetWidthSaver(func(w int) { shrankTo = append(shrankTo, w) })
				if got := a.sidebar.Width(); got != sidebarBaseWidth(t) {
					t.Fatalf("precondition: sidebar width = %d, want the default %d", got, sidebarBaseWidth(t))
				}
			},
			key:      keyPress('['),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.sidebar.Width(); got >= sidebarBaseWidth(t) {
					t.Errorf("sidebar width = %d, want it shrunk below the default %d", got, sidebarBaseWidth(t))
				}
				if len(shrankTo) != 1 {
					t.Fatalf("width saver called %d times, want 1", len(shrankTo))
				}
				if shrankTo[0] != a.sidebar.Width() {
					t.Errorf("width saver got %d, want the new width %d", shrankTo[0], a.sidebar.Width())
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 14: ToggleThread `ctrl+]` (mode_normal.go:142)
		// -------------------------------------------------------------
		{
			name:     "ctrl+] closes a visible thread",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyMod(']', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadVisible {
					t.Error("thread still visible")
				}
				if a.focusedPanel != PanelMessages {
					t.Errorf("focusedPanel = %v, want PanelMessages", a.focusedPanel)
				}
			},
		},
		{
			// app.go:1755-1760: ToggleThread only ever CLOSES. There is
			// no open-on-toggle path, which is why the key reads as
			// dead when no thread is loaded.
			name: "ctrl+] with no thread loaded does not open one",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if a.threadVisible {
					t.Fatal("precondition: thread should start hidden")
				}
			},
			key:      keyMod(']', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadVisible {
					t.Error("thread opened: ToggleThread is close-only")
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 15 & 16: NavBack `ctrl+h` / NavForward `ctrl+k`
		// (mode_normal.go:145, :150)
		// -------------------------------------------------------------
		{
			name:     "ctrl+h walks the nav history backward",
			opts:     append(normalOpts(), withActiveTeam("T1"), navLookupOpt()),
			setup:    seedNavHistory,
			key:      keyMod('h', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ChannelSelectedMsg cmd")
				}
				msg, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ChannelSelectedMsg", cmd())
				}
				if msg.ID != "C1" || !msg.FromHistory {
					t.Errorf("cmd() = %+v, want {ID:C1 FromHistory:true}", msg)
				}
			},
		},
		{
			name: "ctrl+k walks the nav history forward",
			opts: append(normalOpts(), withActiveTeam("T1"), navLookupOpt()),
			setup: func(t *testing.T, a *App) {
				seedNavHistory(t, a)
				if _, _, _, ok := a.navHistory.Walk("T1", -1, a.channels.Lookup); !ok {
					t.Fatal("precondition: could not step back before stepping forward")
				}
			},
			key:      keyMod('k', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ChannelSelectedMsg cmd")
				}
				msg, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ChannelSelectedMsg", cmd())
				}
				if msg.ID != "C2" || !msg.FromHistory {
					t.Errorf("cmd() = %+v, want {ID:C2 FromHistory:true}", msg)
				}
			},
		},
		{
			// The arm's `if cmd != nil` guard falls THROUGH on an empty
			// stack rather than returning, so the switch ends at the
			// bottom `return nil`. Same observable either way; recorded
			// so a future change to that guard has a witness.
			name: "ctrl+h with an empty history stack is inert",
			opts: append(normalOpts(), withActiveTeam("T1")),
			setup: func(t *testing.T, a *App) {
				if a.navHistory.Stack("T1") != nil {
					t.Fatal("precondition: nav stack should start empty")
				}
			},
			key:      keyMod('h', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 17 & 18: Down `j` / Up `k` (mode_normal.go:155, :160)
		// -------------------------------------------------------------
		{
			name: "j moves the sidebar cursor down",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				a.sidebar.SelectByID("C1")
				if got := a.sidebar.SelectedID(); got != "C1" {
					t.Fatalf("precondition: sidebar SelectedID = %q, want %q", got, "C1")
				}
			},
			key:      keyPress('j'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.sidebar.SelectedID(); got == "C1" {
					t.Error("sidebar cursor did not move off C1")
				}
			},
		},
		{
			name: "j in the messages pane moves the selection down and arms the scroll coalescer",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessageAt(t, a, 0)
				if a.scrollFlushScheduled {
					t.Fatal("precondition: scroll coalescer already armed")
				}
			},
			key:      keyPress('j'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 1 {
					t.Errorf("selected index = %d, want 1", got)
				}
				if !a.scrollFlushScheduled {
					t.Error("scroll flush not scheduled: the first move in a burst arms the tick")
				}
				if cmd == nil {
					t.Error("cmd = nil, want the batched flush tick")
				}
			},
		},
		{
			name: "k in the messages pane moves the selection up",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessageAt(t, a, 3)
			},
			key:      keyPress('k'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 2 {
					t.Errorf("selected index = %d, want 2", got)
				}
			},
		},
		{
			// `up` is the second key on the Up binding (keys.go:70) and
			// reaches the same arm.
			name: "up arrow reaches the same arm as k",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessageAt(t, a, 3)
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 2 {
					t.Errorf("selected index = %d, want 2", got)
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 19 & 20: Left `h` / Right `l` (mode_normal.go:165, :168)
		// -------------------------------------------------------------
		{
			name:     "h focuses the previous panel (messages -> sidebar)",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusThreadPanel(t, a); focusMessages(t, a) },
			key:      keyPress('h'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.focusedPanel != PanelSidebar {
					t.Errorf("focusedPanel = %v, want PanelSidebar", a.focusedPanel)
				}
			},
		},
		{
			name:     "l focuses the next panel (messages -> thread)",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusThreadPanel(t, a); focusMessages(t, a) },
			key:      keyPress('l'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.focusedPanel != PanelThread {
					t.Errorf("focusedPanel = %v, want PanelThread", a.focusedPanel)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 21: Enter (mode_normal.go:171)
		// -------------------------------------------------------------
		{
			name: "enter on a sidebar channel emits ChannelSelectedMsg",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				a.sidebar.SelectByID("C2")
				if got := a.sidebar.SelectedID(); got != "C2" {
					t.Fatalf("precondition: sidebar SelectedID = %q, want %q", got, "C2")
				}
				if _, ok := a.sidebar.IsSectionHeaderSelected(); ok {
					t.Fatal("precondition: cursor is on a header, not a channel")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ChannelSelectedMsg cmd")
				}
				msg, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ChannelSelectedMsg", cmd())
				}
				if msg.ID != "C2" || msg.Name != "random" {
					t.Errorf("cmd() = %+v, want {ID:C2 Name:random}", msg)
				}
				if msg.FromHistory {
					t.Error("FromHistory = true, want false for a plain enter")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 22: ToggleSection space (mode_normal.go:174)
		// -------------------------------------------------------------
		{
			// BUG?: this arm is DEAD, and not because a reducer eats the
			// key -- the binding itself can never match. keys.go:120
			// declares ToggleSection as key.WithKeys(" "), and
			// key.Matches compares against tea.KeyMsg.String(). Key.String
			// explicitly refuses to return a lone space (ultraviolet
			// key.go:392: `if len(k.Text) > 0 && k.Text != " "`) and
			// falls through to Keystroke, whose KeySpace arm writes
			// "space" (:445-447, and keyTypeString at :463). So a space
			// press arrives as "space", " " is unreachable, and the
			// section-toggle documented at mode_normal.go:175-178 never
			// happens. Enter on a header still toggles (app.go:1549) --
			// see the row below -- so the feature has a working path;
			// only this arm is dead. Characterized as-is, not fixed.
			//
			// The section is recorded in setup rather than re-read after
			// the press because ToggleCollapse rebuilds the nav rows
			// (sidebar/model.go:647) and the selected header afterwards
			// can name a DIFFERENT section.
			name: "space does NOT toggle a sidebar section: the \" \" binding can never match",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				toggledSection = selectSidebarSectionHeader(t, a)
				wantCollapsed = a.sidebar.IsCollapsed(toggledSection)
				if got := keyPress(' ').String(); got != "space" {
					t.Fatalf("precondition: a space press stringifies to %q, want %q -- "+
						"if this changed, the dead arm may have come alive", got, "space")
				}
			},
			key:      keyPress(' '),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.sidebar.IsCollapsed(toggledSection); got != wantCollapsed {
					t.Errorf("section %q collapsed = %v, want %v (unchanged): the arm is expected to be dead",
						toggledSection, got, wantCollapsed)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The reachable half of the same feature, via the Enter arm
			// (mode_normal.go:171 -> app.go:1549). Also pins that Enter
			// on a header returns nil rather than a ChannelSelectedMsg.
			name: "enter on a sidebar section header toggles its collapse state",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				toggledSection = selectSidebarSectionHeader(t, a)
				wantCollapsed = !a.sidebar.IsCollapsed(toggledSection)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.sidebar.IsCollapsed(toggledSection); got != wantCollapsed {
					t.Errorf("section %q collapsed = %v, want %v", toggledSection, got, wantCollapsed)
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil: a header toggle must not emit a channel selection", cmd())
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 23: Bottom `G` (mode_normal.go:185)
		// -------------------------------------------------------------
		{
			name: "G jumps the messages pane to the newest message",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessageAt(t, a, 0)
			},
			key:      keyPress('G'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 4 {
					t.Errorf("selected index = %d, want 4", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil in the channel view", cmd())
				}
			},
		},
		{
			// The arm's `if cmd != nil { return cmd }` inner branch:
			// handleGoToBottom only returns a cmd in the threads-list
			// view, where G also opens the thread it lands on
			// (app.go:1412-1416).
			name: "G in the threads view opens the thread it lands on",
			opts: append(normalOpts(),
				withView(ViewThreads),
				withThreadsView([]cache.ThreadSummary{
					{ChannelID: "C1", ThreadTS: "1.0", ParentTS: "1.0", ParentText: "first", ReplyCount: 1},
					{ChannelID: "C1", ThreadTS: "2.0", ParentTS: "2.0", ParentText: "second", ReplyCount: 2},
				})),
			setup: func(t *testing.T, a *App) {
				a.focusedPanel = PanelMessages
				if _, ok := a.threadsView.SelectedSummary(); !ok {
					t.Fatal("precondition: threads view has no selected summary")
				}
			},
			key:      keyPress('G'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the thread-open cmd")
				}
				if !a.threadVisible {
					t.Error("thread panel not opened")
				}
				if got := a.lastOpenedThreadTS; got != "2.0" {
					t.Errorf("opened thread ts = %q, want %q (the bottom row)", got, "2.0")
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 24-27: PageUp / PageDown / HalfPageUp / HalfPageDown
		// (mode_normal.go:190, :195, :200, :205)
		//
		// Each row pins the EXACT viewport delta, which is the only way
		// to separate the page arms from the half-page arms: both move
		// the same direction, and "moved a bit" would pass for either.
		// messages.Model.ScrollDown does not clamp (clamping happens in
		// View), so the arithmetic is deterministic.
		// -------------------------------------------------------------
		{
			name:     "pgdown scrolls the messages viewport down one page",
			opts:     normalOpts(),
			setup:    scrollTo(0),
			key:      keyCode(tea.KeyPgDown),
			wantMode: ModeNormal,
			assert:   wantYOffset(func(a *App) int { return a.pageSize() }),
		},
		{
			name:     "pgup scrolls the messages viewport up one page",
			opts:     normalOpts(),
			setup:    scrollTo(400),
			key:      keyCode(tea.KeyPgUp),
			wantMode: ModeNormal,
			assert:   wantYOffset(func(a *App) int { return 400 - a.pageSize() }),
		},
		{
			name:     "ctrl+d scrolls the messages viewport down half a page",
			opts:     normalOpts(),
			setup:    scrollTo(0),
			key:      keyMod('d', tea.ModCtrl),
			wantMode: ModeNormal,
			assert:   wantYOffset(func(a *App) int { return a.halfPageSize() }),
		},
		{
			name:     "ctrl+u scrolls the messages viewport up half a page",
			opts:     normalOpts(),
			setup:    scrollTo(400),
			key:      keyMod('u', tea.ModCtrl),
			wantMode: ModeNormal,
			assert:   wantYOffset(func(a *App) int { return 400 - a.halfPageSize() }),
		},
		{
			// The `if cmd != nil` inner branch of the PageUp arm.
			// scrollFocusedPanel returns a cmd only when an up-scroll
			// lands the viewport at line 0 (app.go:1486), which kicks
			// the older-history backfill.
			name:     "pgup that lands the viewport at the top kicks the history backfill",
			opts:     normalOpts(),
			setup:    scrollTo(2),
			key:      keyCode(tea.KeyPgUp),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.messagepane.YOffset(); got != 0 {
					t.Errorf("yOffset = %d, want 0 (clamped)", got)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the backfill batch")
				}
				if !a.fetchingOlder["C1"] {
					t.Error("fetchingOlder gate not set for C1")
				}
				if !a.messagepane.IsLoading() {
					t.Error("messages pane not marked loading")
				}
			},
		},
		{
			// Same inner branch for the HalfPageUp arm.
			name:     "ctrl+u that lands the viewport at the top kicks the history backfill",
			opts:     normalOpts(),
			setup:    scrollTo(1),
			key:      keyMod('u', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.messagepane.YOffset(); got != 0 {
					t.Errorf("yOffset = %d, want 0 (clamped)", got)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the backfill batch")
				}
			},
		},
		{
			// BUG?-adjacent, recorded rather than fixed: the
			// `if cmd := ...; cmd != nil { return cmd }` wrappers on the
			// PageDown (mode_normal.go:196) and HalfPageDown (:206) arms
			// are unreachable. scrollFocusedPanel returns a non-nil cmd
			// on exactly one path -- the messages-pane UP-scroll backfill
			// at app.go:1486 -- so for a positive delta it always returns
			// nil and the wrapper falls through to the switch's bottom
			// `return nil`. Same observable either way; this row pins the
			// nil so a future change to that helper has a witness.
			name:     "pgdown never returns a cmd: the down path has no backfill",
			opts:     normalOpts(),
			setup:    scrollTo(0),
			key:      keyCode(tea.KeyPgDown),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				// The offset assertion is what keeps this row honest:
				// "cmd == nil" alone would pass against a handler that
				// never ran the arm at all.
				if got := a.messagepane.YOffset(); got != a.pageSize() {
					t.Errorf("yOffset = %d, want %d: the arm did not scroll", got, a.pageSize())
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
				if a.fetchingOlder["C1"] {
					t.Error("fetchingOlder gate set by a down-scroll")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 28: Help `?` (mode_normal.go:210)
		// -------------------------------------------------------------
		{
			name:     "? opens the help overlay populated from the keymap",
			opts:     normalOpts(),
			key:      keyPress('?'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.help.IsVisible() {
					t.Error("help overlay not visible")
				}
				// mode_normal.go:211 seeds the overlay from
				// help.FromKeyMap(a.keys) with no query set, and
				// help.VisibleEntries returns the unfiltered list when
				// the query is empty (help/model.go:186-195). So the
				// overlay carries the WHOLE keymap, in FromKeyMap's
				// desc-sorted order -- element-for-element, not "at
				// least a few". Rebuilding the want from a.keys keeps
				// this stable as bindings are added.
				want := help.FromKeyMap(a.keys)
				got := a.help.VisibleEntries()
				if len(got) != len(want) {
					t.Fatalf("help entries = %d, want %d (the full keymap)", len(got), len(want))
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
					}
				}
				// Absolute floor: len(got) == len(want) alone would
				// still hold if the keymap itself collapsed, since both
				// sides derive from a.keys. The real map has 59
				// help-bearing bindings; 40 leaves ample room for
				// removals without noticing a gutted keymap.
				if len(got) < 40 {
					t.Errorf("help entries = %d, want at least 40: the keymap looks gutted", len(got))
				}
				// Spot-check one entry that only the real keymap
				// produces, so the row fails if FromKeyMap starts
				// returning placeholder rows.
				if !slices.Contains(got, help.Entry{Key: "Y/C", Desc: "copy permalink"}) {
					t.Error("help entries do not include the Y/C copy-permalink row")
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 29 & 30: ThemeSwitcher / ThemeSwitcherGlobal
		// (mode_normal.go:215, :222)
		// -------------------------------------------------------------
		{
			name:     "ctrl+y opens the theme switcher scoped to the workspace",
			opts:     activeTeamOpts(),
			key:      keyMod('y', tea.ModCtrl),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.themeSwitcher.IsVisible() {
					t.Fatal("theme switcher not visible")
				}
				if got := a.themeSwitcher.Scope(); got != themeswitcher.ScopeWorkspace {
					t.Errorf("scope = %v, want ScopeWorkspace", got)
				}
				if got := a.themeSwitcher.HeaderText(); got != "Theme for alpha" {
					t.Errorf("header = %q, want %q", got, "Theme for alpha")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "ctrl+shift+y opens the theme switcher scoped globally",
			opts:     normalOpts(),
			key:      keyMod('y', tea.ModCtrl|tea.ModShift),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.themeSwitcher.IsVisible() {
					t.Fatal("theme switcher not visible")
				}
				if got := a.themeSwitcher.Scope(); got != themeswitcher.ScopeGlobal {
					t.Errorf("scope = %v, want ScopeGlobal", got)
				}
				if got := a.themeSwitcher.HeaderText(); got != "Default theme for new workspaces" {
					t.Errorf("header = %q, want the global header", got)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 31: PresenceMenu `ctrl+s` (mode_normal.go:227)
		// -------------------------------------------------------------
		{
			name:     "ctrl+s opens the presence menu",
			opts:     activeTeamOpts(),
			key:      keyMod('s', tea.ModCtrl),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.presenceMenu.IsVisible() {
					t.Error("presence menu not visible")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 32: FuzzyFinder / FuzzyFinderAlt (mode_normal.go:233)
		// -------------------------------------------------------------
		{
			name:     "ctrl+t opens the channel finder",
			opts:     normalOpts(),
			key:      keyMod('t', tea.ModCtrl),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.channelFinder.IsVisible() {
					t.Error("channel finder not visible")
				}
			},
		},
		{
			name:     "ctrl+p is the alternate channel-finder binding",
			opts:     normalOpts(),
			key:      keyMod('p', tea.ModCtrl),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.channelFinder.IsVisible() {
					t.Error("channel finder not visible")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 33: NewMessage `ctrl+n` (mode_normal.go:237)
		// -------------------------------------------------------------
		{
			// The mode change happens in reduceNewMessagePicker, not
			// here, so the handler leaves the mode alone.
			name:     "ctrl+n emits EnterNewMessageMsg and stays in Normal",
			opts:     normalOpts(),
			key:      keyMod('n', tea.ModCtrl),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want EnterNewMessageMsg")
				}
				if _, ok := cmd().(EnterNewMessageMsg); !ok {
					t.Errorf("cmd() = %#v, want EnterNewMessageMsg", cmd())
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 34: Reaction `r` (mode_normal.go:240)
		// -------------------------------------------------------------
		{
			name:     "r opens the reaction picker for the selected message",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('r'),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.reactionPicker.IsVisible() {
					t.Error("reaction picker not visible")
				}
			},
		},
		{
			name:     "r with the thread focused opens the picker for the selected reply",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyPress('r'),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.reactionPicker.IsVisible() {
					t.Error("reaction picker not visible")
				}
			},
		},
		{
			// Neither branch matches with sidebar focus, so the arm is
			// entered and falls straight through.
			name:     "r with the sidebar focused opens nothing",
			opts:     normalOpts(),
			key:      keyPress('r'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.reactionPicker.IsVisible() {
					t.Error("reaction picker opened from the sidebar")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 35: ReactionNav `R` (mode_normal.go:247)
		// -------------------------------------------------------------
		{
			name:     "R enters reaction-nav on the messages pane",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('R'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.messagepane.ReactionNavActive() {
					t.Error("message reaction nav not active")
				}
			},
		},
		{
			name:     "R enters reaction-nav on the thread panel",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyPress('R'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.threadPanel.ReactionNavActive() {
					t.Error("thread reaction nav not active")
				}
			},
		},
		{
			// messages.Model.EnterReactionNav silently refuses when the
			// selected message has no reactions (messages/model.go:1036).
			name:     "R on a message with no reactions does not enter the sub-state",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 0) },
			key:      keyPress('R'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.messagepane.ReactionNavActive() {
					t.Error("reaction nav active on a reaction-less message")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 36: ListReactions `L` (mode_normal.go:254)
		// -------------------------------------------------------------
		{
			name:     "L opens the read-only reactions view",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('L'),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.reactionsView.IsVisible() {
					t.Error("reactions view not visible")
				}
			},
		},
		{
			// app.go:898 -- no reactions means no modal. This is also
			// the row that would have been vacuous under the default
			// focusedPanel (PanelSidebar hits the `default: return nil`
			// arm at app.go:895 for an entirely different reason), so
			// focusMessageAt pins the focus explicitly.
			name:     "L on a message with no reactions is a no-op",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 0) },
			key:      keyPress('L'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.reactionsView.IsVisible() {
					t.Error("reactions view opened for a reaction-less message")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 37: SaveThread `S` (mode_normal.go:257)
		// -------------------------------------------------------------
		{
			name:     "S outside the thread panel toasts instead of saving",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('S'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ToastMsg")
				}
				msg, ok := cmd().(ToastMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ToastMsg", cmd())
				}
				if msg.Text != "Open a thread first" {
					t.Errorf("toast = %q, want %q", msg.Text, "Open a thread first")
				}
			},
		},
		{
			// The write itself is deliberately NOT executed: running
			// the cmd would touch the real export directory. Asserting
			// the cmd exists separates "took the save path" from "took
			// the toast path", which is all this arm decides.
			name:     "S with the thread focused returns the save cmd",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyPress('S'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the thread-save cmd")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 38: CopyMessage `y` (mode_normal.go:260)
		// -------------------------------------------------------------
		{
			name:     "y returns the clipboard-write batch for the selected message",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('y'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the clipboard batch")
				}
			},
		},
		{
			name:     "y with the sidebar focused is a no-op",
			opts:     normalOpts(),
			key:      keyPress('y'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 39: CopyPermalink `Y` / `C` (mode_normal.go:263)
		// -------------------------------------------------------------
		{
			name:     "Y returns the permalink-fetch cmd",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('Y'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the permalink cmd")
				}
			},
		},
		{
			name:     "C is the alternate copy-permalink binding",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('C'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the permalink cmd")
				}
			},
		},
		{
			// The negative sibling for both permalink rows above.
			// copyPermalinkOfSelected switches on focusedPanel and its
			// default arm returns nil (app.go:1063-1064), so with the
			// sidebar focused the arm is reached and produces nothing.
			// Without this row "cmd != nil" would not discriminate the
			// permalink arm from any other arm returning a cmd.
			name: "Y with the sidebar focused is a no-op",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if a.focusedPanel != PanelSidebar {
					t.Fatalf("precondition: focusedPanel = %v, want PanelSidebar", a.focusedPanel)
				}
			},
			key:      keyPress('Y'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			name: "C with the sidebar focused is a no-op",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if a.focusedPanel != PanelSidebar {
					t.Fatalf("precondition: focusedPanel = %v, want PanelSidebar", a.focusedPanel)
				}
			},
			key:      keyPress('C'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 40: Edit `E` (mode_normal.go:266)
		// -------------------------------------------------------------
		{
			// app.go:2953: isOwnMessage is false whenever
			// currentUserID is "" -- which is newTestApp's default, so
			// this is the path an unconfigured App takes.
			name:     "E on someone else's message reports EditNotOwn",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('E'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want EditNotOwnMsg")
				}
				if a.editing.IsActive() {
					t.Error("edit began on a message the user does not own")
				}
			},
		},
		{
			name: "E on an own message begins the edit in insert mode",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				a.currentUserID = "U1"
				msg, _ := a.messagepane.SelectedMessage()
				if msg.UserID != "U1" {
					t.Fatalf("precondition: selected message userID = %q, want U1", msg.UserID)
				}
			},
			key:      keyPress('E'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.editing.IsActive() {
					t.Fatal("edit did not begin")
				}
				if got := a.editing.TS(); got != "5.0" {
					t.Errorf("editing TS = %q, want %q", got, "5.0")
				}
				if got := a.compose.Value(); !strings.Contains(got, "example.com") {
					t.Errorf("compose value = %q, want the message text loaded for editing", got)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 41: Delete `D` (mode_normal.go:269)
		// -------------------------------------------------------------
		{
			name:     "D on someone else's message reports DeleteNotOwn",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('D'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want DeleteNotOwnMsg")
				}
				if a.confirmPrompt.IsVisible() {
					t.Error("confirm prompt opened for a message the user does not own")
				}
			},
		},
		{
			name: "D on an own message opens the delete confirm",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				focusMessages(t, a)
				a.currentUserID = "U1"
			},
			key:      keyPress('D'),
			wantMode: ModeConfirm,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.confirmPrompt.IsVisible() {
					t.Error("confirm prompt not visible")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 42: OpenPreview `O` / `v` (mode_normal.go:272)
		// -------------------------------------------------------------
		{
			name:     "O emits OpenImagePreviewMsg for the first image attachment",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('O'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want OpenImagePreviewMsg")
				}
				msg, ok := cmd().(messages.OpenImagePreviewMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want OpenImagePreviewMsg", cmd())
				}
				// AttIdx 1: the file attachment is index 0, the image
				// index 1 (app.go:3334 scans in order).
				if msg.Channel != "C1" || msg.TS != "5.0" || msg.AttIdx != 1 {
					t.Errorf("cmd() = %+v, want {Channel:C1 TS:5.0 AttIdx:1}", msg)
				}
			},
		},
		{
			name:     "v is the alternate open-preview binding",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('v'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want OpenImagePreviewMsg")
				}
				if _, ok := cmd().(messages.OpenImagePreviewMsg); !ok {
					t.Errorf("cmd() = %#v, want OpenImagePreviewMsg", cmd())
				}
			},
		},
		{
			name:     "O on a message with no image attachment is a silent no-op",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 0) },
			key:      keyPress('O'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 43: OpenLink `o` (mode_normal.go:275)
		// -------------------------------------------------------------
		{
			name:     "o with exactly one link emits OpenLinkMsg directly",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('o'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want OpenLinkMsg")
				}
				msg, ok := cmd().(OpenLinkMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want OpenLinkMsg", cmd())
				}
				if msg.URL != "https://example.com/a" {
					t.Errorf("URL = %q, want %q", msg.URL, "https://example.com/a")
				}
				if a.linkPicker.IsVisible() {
					t.Error("link picker opened for a single link")
				}
			},
		},
		{
			name:     "o with no links toasts",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 0) },
			key:      keyPress('o'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ToastMsg")
				}
				msg, ok := cmd().(ToastMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ToastMsg", cmd())
				}
				if msg.Text != "No links in message" {
					t.Errorf("toast = %q, want %q", msg.Text, "No links in message")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 44: DownloadFile `d` (mode_normal.go:278)
		// -------------------------------------------------------------
		{
			name:     "d with exactly one file emits DownloadFileMsg directly",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('d'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want DownloadFileMsg")
				}
				msg, ok := cmd().(DownloadFileMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want DownloadFileMsg", cmd())
				}
				if msg.Attachment.Name != "report.pdf" {
					t.Errorf("attachment = %q, want %q", msg.Attachment.Name, "report.pdf")
				}
			},
		},
		{
			name:     "d with no files toasts",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 0) },
			key:      keyPress('d'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ToastMsg")
				}
				msg, ok := cmd().(ToastMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ToastMsg", cmd())
				}
				if msg.Text != "No files in message" {
					t.Errorf("toast = %q, want %q", msg.Text, "No files in message")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 45: MarkUnread `U` (mode_normal.go:281)
		// -------------------------------------------------------------
		{
			name:     "U emits MarkUnreadMsg with the boundary before the selection",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 2) },
			key:      keyPress('U'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want MarkUnreadMsg")
				}
				msg, ok := cmd().(MarkUnreadMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want MarkUnreadMsg", cmd())
				}
				want := MarkUnreadMsg{ChannelID: "C1", ThreadTS: "", BoundaryTS: "2.0", UnreadCount: 3}
				if msg != want {
					t.Errorf("cmd() = %+v, want %+v", msg, want)
				}
			},
		},

		// -------------------------------------------------------------
		// Arms 46 & 47: NextUnread `a` / PrevUnread `A`
		// (mode_normal.go:284, :287)
		// -------------------------------------------------------------
		{
			// The direction argument is the whole content of these two
			// arms (mode_normal.go:285 passes 1, :288 passes -1), so
			// the fixture has to make the two directions land on
			// DIFFERENT channels. unreadOpts/seedUnreads do that: from
			// C1, forward is C2 and backward wraps to C4.
			name:     "a opens the next unread channel below the active one",
			opts:     unreadOpts(),
			setup:    seedUnreads,
			key:      keyPress('a'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ChannelSelectedMsg cmd")
				}
				msg, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ChannelSelectedMsg", cmd())
				}
				want := ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"}
				if msg != want {
					t.Errorf("cmd() = %+v, want %+v", msg, want)
				}
				// jumpToUnread also moves the sidebar cursor onto the
				// target before returning (mode_normal.go:335).
				if got := a.sidebar.SelectedID(); got != "C2" {
					t.Errorf("sidebar SelectedID = %q, want %q", got, "C2")
				}
			},
		},
		{
			name:     "A opens the previous unread channel, wrapping to the bottom",
			opts:     unreadOpts(),
			setup:    seedUnreads,
			key:      keyPress('A'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a ChannelSelectedMsg cmd")
				}
				msg, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ChannelSelectedMsg", cmd())
				}
				want := ChannelSelectedMsg{ID: "C4", Name: "ops", Type: "channel"}
				if msg != want {
					t.Errorf("cmd() = %+v, want %+v", msg, want)
				}
				if got := a.sidebar.SelectedID(); got != "C4" {
					t.Errorf("sidebar SelectedID = %q, want %q", got, "C4")
				}
			},
		},
		{
			name: "a with nothing unread toasts",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if _, _, _, ok := a.sidebar.NextUnread("C1", 1); ok {
					t.Fatal("precondition: fixture channels should have no unreads")
				}
			},
			key:      keyPress('a'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := statusbarText(a); !strings.Contains(got, "No other unread channels") {
					t.Errorf("statusbar = %q, want the no-unreads toast", got)
				}
				if cmd == nil {
					t.Error("cmd = nil, want the toast-clear tick")
				}
			},
		},
		{
			name: "A with nothing unread toasts the same way",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if _, _, _, ok := a.sidebar.NextUnread("C1", -1); ok {
					t.Fatal("precondition: fixture channels should have no unreads")
				}
			},
			key:      keyPress('A'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := statusbarText(a); !strings.Contains(got, "No other unread channels") {
					t.Errorf("statusbar = %q, want the no-unreads toast", got)
				}
				if cmd == nil {
					t.Error("cmd = nil, want the toast-clear tick")
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 48: CloseThreadView `q` (mode_normal.go:290)
		//
		// NOTE for the keymap record: keys.go:98 binds `q` to
		// CloseThreadView and keys.go:97 binds `Q` to QuitConfirm.
		// Quit is ctrl+c ONLY (keys.go:96) and never reaches this
		// handler -- App.handleKey intercepts it at app.go:708.
		// -------------------------------------------------------------
		{
			name:     "q closes a visible thread",
			opts:     normalOpts(),
			setup:    focusThreadPanel,
			key:      keyPress('q'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.threadVisible {
					t.Error("thread still visible")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "q with no thread open does not quit",
			opts: normalOpts(),
			setup: func(t *testing.T, a *App) {
				if a.threadVisible {
					t.Fatal("precondition: thread should start hidden")
				}
			},
			key:      keyPress('q'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.confirmPrompt.IsVisible() {
					t.Error("quit confirm opened: lowercase q must not quit")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Arm 49: QuitConfirm `Q` (mode_normal.go:300)
		// -------------------------------------------------------------
		{
			name:     "Q opens the quit confirm prompt",
			opts:     normalOpts(),
			key:      keyPress('Q'),
			wantMode: ModeConfirm,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.confirmPrompt.IsVisible() {
					t.Error("quit confirm not visible")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// -------------------------------------------------------------
		// Default arm: numeric workspace switch (mode_normal.go:304)
		// -------------------------------------------------------------
		{
			name:     "2 switches to the second workspace",
			opts:     workspaceOpts(),
			setup:    wireSwitcher,
			key:      keyPress('2'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the workspace-switch cmd")
				}
				msg, ok := cmd().(switchedTeamMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want switchedTeamMsg", cmd())
				}
				if msg.teamID != "T2" {
					t.Errorf("switched to %q, want %q", msg.teamID, "T2")
				}
			},
		},
		{
			// mode_normal.go:310 -- re-picking the workspace you are
			// already on is suppressed.
			name:     "1 on the already-active workspace does not switch",
			opts:     workspaceOpts(),
			setup:    wireSwitcher,
			key:      keyPress('1'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			// mode_normal.go:309 -- the index guard. Only two
			// workspaces are seeded, so `9` addresses nothing.
			name:     "9 with only two workspaces is inert",
			opts:     workspaceOpts(),
			setup:    wireSwitcher,
			key:      keyPress('9'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			// `0` is outside the '1'..'9' range tested at
			// mode_normal.go:307, so it never reaches the switcher.
			name:     "0 is not a workspace key",
			opts:     workspaceOpts(),
			setup:    wireSwitcher,
			key:      keyPress('0'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			// BUG?: keys.go:90 binds Top to `g` with help text "gg  top",
			// and help.FromKeyMap therefore advertises it in the `?`
			// overlay -- but handleNormalMode declares NO arm for
			// a.keys.Top. `g` falls into the numeric default arm,
			// fails the '1'..'9' test, and does nothing. The
			// counterpart `G` (Bottom, mode_normal.go:185) works.
			// Recorded, not fixed: this is a characterization test.
			name:     "g is bound to Top but handleNormalMode has no arm for it",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { focusMessageAt(t, a, 3) },
			key:      keyPress('g'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 3 {
					t.Errorf("selected index = %d, want 3 (unchanged): `g` should still be inert", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			name:     "an unbound printable key is inert",
			opts:     normalOpts(),
			setup:    focusMessages,
			key:      keyPress('z'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 4 {
					t.Errorf("selected index = %d, want 4 (unchanged)", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
	})
}

// ---------------------------------------------------------------------
// Row helpers
// ---------------------------------------------------------------------

// sidebarBaseWidth is the sidebar width a freshly built test App
// starts at, used as the reference point by the ]/[ rows so they
// assert a direction rather than a magic number.
func sidebarBaseWidth(t *testing.T) int {
	t.Helper()
	return newTestApp(t).sidebar.Width()
}

// scrollTo returns a setup that focuses the messages pane and parks the
// viewport at an exact yOffset, asserting it took. The page/half-page
// rows need a known starting offset to assert an exact delta.
func scrollTo(off int) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		focusMessages(t, a)
		if off > 0 {
			a.messagepane.ScrollDown(off)
		}
		if got := a.messagepane.YOffset(); got != off {
			t.Fatalf("precondition: yOffset = %d, want %d", got, off)
		}
		if a.pageSize() < 8 {
			t.Fatalf("precondition: pageSize = %d, too small to distinguish page from half-page", a.pageSize())
		}
	}
}

// wantYOffset asserts the viewport landed on exactly want(a) lines.
func wantYOffset(want func(*App) int) func(*testing.T, *App, tea.Cmd) {
	return func(t *testing.T, a *App, _ tea.Cmd) {
		if got, w := a.messagepane.YOffset(), want(a); got != w {
			t.Errorf("yOffset = %d, want %d", got, w)
		}
	}
}

// activeTeamOpts is normalOpts plus a single named workspace, T1, made
// active. Two arms need a resolvable a.activeTeamName(): ctrl+y (the
// theme switcher's "Theme for <name>" header, mode_normal.go:218) and
// ctrl+s (the presence menu).
func activeTeamOpts() []testOpt {
	return append(normalOpts(),
		withWorkspaces(workspace.WorkspaceItem{ID: "T1", Name: "alpha", Initials: "AL"}),
		withActiveTeam("T1"))
}

// workspaceOpts is normalOpts plus the two-workspace rail the numeric
// default arm (mode_normal.go:304-318) indexes into. T1 is first, so
// the rail selects it and `1` exercises the already-active guard while
// `2` exercises the switch.
func workspaceOpts() []testOpt {
	return append(normalOpts(), withWorkspaces(
		workspace.WorkspaceItem{ID: "T1", Name: "alpha", Initials: "AL"},
		workspace.WorkspaceItem{ID: "T2", Name: "beta", Initials: "BE"},
	))
}

// wireSwitcher installs an observable workspace switcher and asserts the
// rail starts on T1. Both halves matter: mode_normal.go:309 requires a
// non-nil a.workspaceSwitcher before it will emit anything, and :310
// compares against a.workspaceRail.SelectedID(), so a rail that did not
// start on T1 would silently turn the `1` row into a different test.
func wireSwitcher(t *testing.T, a *App) {
	t.Helper()
	a.SetWorkspaceSwitcher(func(teamID string) tea.Msg {
		return switchedTeamMsg{teamID: teamID}
	})
	if got := a.workspaceRail.SelectedID(); got != "T1" {
		t.Fatalf("precondition: rail SelectedID = %q, want %q", got, "T1")
	}
}

// navLookupOpt wires a ChannelService whose Lookup resolves the two
// fixture channels. navHistoryStore.Walk drops entries whose lookup
// fails (navhistory.go:116-122), so without this every nav-history row
// would silently report "no valid earlier entry".
func navLookupOpt() testOpt {
	return withChannelService(ChannelServiceFuncs{
		Lookup: func(id ids.ChannelID) (string, string, bool) {
			switch string(id) {
			case "C1":
				return "general", "channel", true
			case "C2":
				return "random", "channel", true
			}
			return "", "", false
		},
	})
}

// seedNavHistory pushes C1 then C2 for team T1, leaving the cursor on
// C2 so one step back lands on C1.
func seedNavHistory(t *testing.T, a *App) {
	t.Helper()
	a.navHistory.Push("T1", "C1")
	a.navHistory.Push("T1", "C2")
	stack := a.navHistory.Stack("T1")
	if stack == nil {
		t.Fatal("precondition: nav stack was not created")
	}
	if len(stack.entries) != 2 || stack.cursor != 1 {
		t.Fatalf("precondition: nav stack = %v cursor=%d, want [C1 C2] cursor=1",
			stack.entries, stack.cursor)
	}
}
