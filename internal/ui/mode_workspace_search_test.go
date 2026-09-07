package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// ---------------------------------------------------------------------
// handleWorkspaceSearchMode (mode_workspace_search.go:20) -- the ctrl+f
// modal.
//
// The handler is a four-way switch on searchresults.Action. Only
// ActionSelect branches further, and it branches on where the hit lives:
// the active channel (navigate in place), a channel the user is a member
// of (switch to it), or one they are not (refuse with a toast, because
// navigating would fail with not_in_channel).
//
// Third of the three normalizeFinderKey consumers. searchresults matches
// bare "enter"/"esc"/"up"/"down"/"backspace"/"space" and drops anything
// longer than one rune (searchresults/model.go:138-185), so every
// modified-key row below would come back ActionNone without its arm.
// ---------------------------------------------------------------------

// fetchAroundMsg is what the test's FetchAround returns, so a row can
// prove the off-buffer navigation path was taken and with which target.
type fetchAroundMsg struct {
	channel ids.ChannelID
	ts      ids.MessageTS
}

// workspaceSearchDispatchedMsg is what the test's SearchWorkspace
// returns. A distinct type keeps a submit row from being satisfied by
// any other command the App might produce.
type workspaceSearchDispatchedMsg struct{ query string }

// wsSearchOpts is the shared bundle: three messages in C1 (TS 1.0, 2.0,
// 3.0 -- SetMessages selects the LAST, so 3.0 is the starting
// selection), C1 active, and services that record rather than act.
//
// Lookup answers for C2 only. C9 is therefore the "public channel the
// user has not joined" case that the toast arm exists for.
func wsSearchOpts() []testOpt {
	return []testOpt{
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel"},
		),
		withMessages(testMessageItems(3)...),
		withActiveChannel("C1"),
		withChannelService(ChannelServiceFuncs{
			Lookup: func(id ids.ChannelID) (string, string, bool) {
				if id == "C2" {
					return "random", "channel", true
				}
				return "", "", false
			},
			FetchAround: func(id ids.ChannelID, ts ids.MessageTS) tea.Msg {
				return fetchAroundMsg{channel: id, ts: ts}
			},
		}),
	}
}

// openWorkspaceSearch opens the modal in its input state and asserts it
// took. Every row here depends on the modal being visible: HandleKey is
// state-driven, and a closed modal would answer ActionNone to
// everything, turning the inert rows into vacuous passes.
func openWorkspaceSearch(t *testing.T, a *App) {
	t.Helper()
	a.searchResults.Open()
	if !a.searchResults.IsVisible() {
		t.Fatal("precondition: workspace search modal is not visible")
	}
	if a.searchResults.Loading() {
		t.Fatal("precondition: modal should open in the input state, not loading")
	}
	if got := a.searchResults.Query(); got != "" {
		t.Fatalf("precondition: query = %q, want empty", got)
	}
}

// typeWorkspaceQuery opens the modal and drives characters through the
// widget's own HandleKey, so the row's dispatched key is the only one
// the mode handler sees.
func typeWorkspaceQuery(q string) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		t.Helper()
		openWorkspaceSearch(t, a)
		for _, r := range q {
			a.searchResults.HandleKey(string(r))
		}
		if got := a.searchResults.Query(); got != q {
			t.Fatalf("precondition: query = %q, want %q", got, q)
		}
	}
}

// seedWorkspaceResults drives the modal all the way into its results
// state: query, submit (which is the only transition into stateLoading),
// then SetResults -- which silently no-ops from any other state
// (searchresults/model.go:107-109). The assertions below catch that.
func seedWorkspaceResults(items []searchresults.Item) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		t.Helper()
		typeWorkspaceQuery("hello")(t, a)
		if got := a.searchResults.HandleKey("enter"); got != searchresults.ActionSubmit {
			t.Fatalf("precondition: submit returned %v, want ActionSubmit", got)
		}
		if !a.searchResults.Loading() {
			t.Fatal("precondition: modal is not in the loading state")
		}
		a.searchResults.SetResults(items, len(items))
		sel, ok := a.searchResults.Selected()
		if !ok {
			t.Fatal("precondition: results did not install (Selected reports nothing highlighted)")
		}
		if sel.TS != items[0].TS {
			t.Fatalf("precondition: highlighted result TS = %q, want the first row %q", sel.TS, items[0].TS)
		}
		if got, ok := a.messagepane.SelectedMessage(); !ok || got.TS != "3.0" {
			t.Fatalf("precondition: message pane selection = %q (ok=%v), want 3.0", got.TS, ok)
		}
	}
}

func TestWorkspaceSearchModeKeys(t *testing.T) {
	// Result fixtures. Each row picks the one that steers it down the
	// branch under test.
	inActiveChannel := []searchresults.Item{
		{ChannelID: "C1", ChannelName: "general", TS: "2.0", Text: "hello there"},
	}
	offBuffer := []searchresults.Item{
		{ChannelID: "C1", ChannelName: "general", TS: "99.0", Text: "hello there"},
	}
	memberChannel := []searchresults.Item{
		{ChannelID: "C2", ChannelName: "random", TS: "5.0", Text: "hello there"},
	}
	nonMemberChannel := []searchresults.Item{
		{ChannelID: "C9", ChannelName: "secrets", TS: "5.0", Text: "hello there"},
	}
	twoRows := []searchresults.Item{
		{ChannelID: "C1", ChannelName: "general", TS: "2.0", Text: "first hit"},
		{ChannelID: "C2", ChannelName: "random", TS: "5.0", Text: "second hit"},
	}

	var dispatched []string
	installSearchSvc := func(a *App) {
		a.SetSearchService(NewSearchService(SearchServiceFuncs{
			SearchWorkspace: func(q string) tea.Msg {
				dispatched = append(dispatched, q)
				return workspaceSearchDispatchedMsg{query: q}
			},
		}))
	}

	runKeyCases(t, ModeWorkspaceSearch, []keyCase{
		// ---- ActionClose -------------------------------------------
		{
			name:     "esc closes the modal and returns to Normal",
			opts:     wsSearchOpts(),
			setup:    typeWorkspaceQuery("hello"),
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.searchResults.IsVisible() {
					t.Error("modal still visible after esc")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// Without normalizeFinderKey's KeyEscape arm the widget
			// would see "shift+esc", drop it as a 9-rune non-printable,
			// answer ActionNone, and the modal would stay open.
			name:     "shift+esc still closes: normalizeFinderKey strips the modifier",
			opts:     wsSearchOpts(),
			setup:    openWorkspaceSearch,
			key:      keyMod(tea.KeyEscape, tea.ModShift),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchResults.IsVisible() {
					t.Error("modal still visible after shift+esc: the esc arm did not normalise")
				}
			},
		},

		// ---- ActionSubmit ------------------------------------------
		{
			name: "enter on a non-empty query dispatches the workspace search",
			opts: wsSearchOpts(),
			setup: func(t *testing.T, a *App) {
				typeWorkspaceQuery("hello")(t, a)
				installSearchSvc(a)
				dispatched = nil
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.searchResults.Loading() {
					t.Error("modal is not in the loading state after submit")
				}
				if !a.searchResults.IsVisible() {
					t.Error("modal closed on submit; it should stay up showing the spinner")
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the workspace-search cmd")
				}
				if len(dispatched) != 0 {
					t.Errorf("service called %d times before the cmd ran, want 0", len(dispatched))
				}
				got, ok := cmd().(workspaceSearchDispatchedMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want the SearchService's answer", cmd())
				}
				if got.query != "hello" {
					t.Errorf("service got query %q, want %q", got.query, "hello")
				}
			},
		},
		{
			name: "shift+enter still submits: normalizeFinderKey strips the modifier",
			opts: wsSearchOpts(),
			setup: func(t *testing.T, a *App) {
				typeWorkspaceQuery("hello")(t, a)
				installSearchSvc(a)
			},
			key:      keyMod(tea.KeyEnter, tea.ModShift),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil: the enter arm did not normalise")
				}
				if !a.searchResults.Loading() {
					t.Error("modal is not loading; shift+enter did not reach the widget as \"enter\"")
				}
			},
		},
		{
			name:     "enter on an empty query does nothing and keeps the modal up",
			opts:     wsSearchOpts(),
			setup:    openWorkspaceSearch,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				if a.searchResults.Loading() {
					t.Error("an empty query started a search")
				}
				if !a.searchResults.IsVisible() {
					t.Error("modal closed on an empty-query enter")
				}
			},
		},
		{
			// Re-submitting while a search is in flight would fire a
			// duplicate rate-limited search.messages call, so the widget
			// swallows it (searchresults/model.go:143-147).
			name: "enter while a search is in flight is swallowed",
			opts: wsSearchOpts(),
			setup: func(t *testing.T, a *App) {
				typeWorkspaceQuery("hello")(t, a)
				installSearchSvc(a)
				if got := a.searchResults.HandleKey("enter"); got != searchresults.ActionSubmit {
					t.Fatalf("precondition: first submit returned %v, want ActionSubmit", got)
				}
				if !a.searchResults.Loading() {
					t.Fatal("precondition: modal is not loading")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil for a duplicate submit", cmd)
				}
			},
		},

		// ---- ActionSelect ------------------------------------------
		{
			// Hit in the channel already on screen whose message is in
			// the pane buffer: completePendingLinkNav selects it
			// in place and consumes the pending nav, so no command is
			// needed.
			name:     "enter on a hit in the active channel selects it in place",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(inActiveChannel),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.searchResults.IsVisible() {
					t.Error("modal still visible after a selection")
				}
				if got, ok := a.messagepane.SelectedMessage(); !ok || got.TS != "2.0" {
					t.Errorf("message pane selection = %q (ok=%v), want 2.0", got.TS, ok)
				}
				if a.pendingLinkNav != nil {
					t.Errorf("pendingLinkNav = %+v, want nil once consumed in place", a.pendingLinkNav)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil when the target is already in the buffer", cmd)
				}
			},
		},
		{
			// Same channel, but the target predates the loaded buffer:
			// the nav is authoritative, so it falls through to
			// FetchAround.
			name:     "enter on an off-buffer hit in the active channel fetches around it",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(offBuffer),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got, ok := a.messagepane.SelectedMessage(); !ok || got.TS != "3.0" {
					t.Errorf("message pane selection = %q (ok=%v), want 3.0 unchanged", got.TS, ok)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the FetchAround cmd")
				}
				got, ok := cmd().(fetchAroundMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want fetchAroundMsg", cmd())
				}
				if got.channel != "C1" || got.ts != "99.0" {
					t.Errorf("FetchAround called with (%q, %q), want (C1, 99.0)", got.channel, got.ts)
				}
			},
		},
		{
			name:     "enter on a hit in another channel the user is a member of switches to it",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(memberChannel),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.searchResults.IsVisible() {
					t.Error("modal still visible after a selection")
				}
				if a.pendingLinkNav == nil {
					t.Fatal("pendingLinkNav = nil, want it armed for the post-switch jump")
				}
				if a.pendingLinkNav.channelID != "C2" || a.pendingLinkNav.messageTS != "5.0" {
					t.Errorf("pendingLinkNav = %+v, want {channelID:C2 messageTS:5.0}", *a.pendingLinkNav)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want ChannelSelectedMsg")
				}
				sel, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want ChannelSelectedMsg", cmd())
				}
				// Name and Type come from the ChannelService Lookup,
				// not from the search hit.
				if sel.ID != "C2" || sel.Name != "random" || sel.Type != "channel" {
					t.Errorf("ChannelSelectedMsg = %+v, want {ID:C2 Name:random Type:channel}", sel)
				}
			},
		},
		{
			// A Lookup miss is this layer's "not a member" signal.
			// Navigating would strand the user in an empty pane, so the
			// handler refuses and must NOT arm pendingLinkNav.
			name:     "enter on a hit in a channel the user has not joined toasts instead of navigating",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(nonMemberChannel),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.pendingLinkNav != nil {
					t.Errorf("pendingLinkNav = %+v, want nil: a refused nav must not be armed", *a.pendingLinkNav)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the not-a-member toast")
				}
				toast, ok := cmd().(ToastMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want ToastMsg", cmd())
				}
				want := "Not a member of #secrets — join via ctrl+t to view"
				if toast.Text != want {
					t.Errorf("toast = %q, want %q", toast.Text, want)
				}
			},
		},

		// ---- navigation and editing --------------------------------
		{
			name:     "down moves the highlight to the next result",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(twoRows),
			key:      keyCode(tea.KeyDown),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				sel, ok := a.searchResults.Selected()
				if !ok {
					t.Fatal("nothing highlighted after down")
				}
				if sel.TS != "5.0" {
					t.Errorf("highlighted TS = %q, want 5.0", sel.TS)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "shift+down moves the highlight: normalizeFinderKey strips the modifier",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(twoRows),
			key:      keyMod(tea.KeyDown, tea.ModShift),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				sel, _ := a.searchResults.Selected()
				if sel.TS != "5.0" {
					t.Errorf("highlighted TS = %q, want 5.0: the down arm did not normalise", sel.TS)
				}
			},
		},
		{
			// Parked on row 2 first, so the move is observable. An up
			// row starting at the top would be inert and could not tell
			// normalisation from a dropped key.
			name: "shift+up moves the highlight back: normalizeFinderKey strips the modifier",
			opts: wsSearchOpts(),
			setup: func(t *testing.T, a *App) {
				seedWorkspaceResults(twoRows)(t, a)
				a.searchResults.HandleKey("down")
				if sel, _ := a.searchResults.Selected(); sel.TS != "5.0" {
					t.Fatalf("precondition: highlighted TS = %q, want it parked on 5.0", sel.TS)
				}
			},
			key:      keyMod(tea.KeyUp, tea.ModShift),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				sel, _ := a.searchResults.Selected()
				if sel.TS != "2.0" {
					t.Errorf("highlighted TS = %q, want 2.0: the up arm did not normalise", sel.TS)
				}
			},
		},
		{
			name:     "shift+backspace edits the query: normalizeFinderKey strips the modifier",
			opts:     wsSearchOpts(),
			setup:    typeWorkspaceQuery("hello"),
			key:      keyMod(tea.KeyBackspace, tea.ModShift),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.searchResults.Query(); got != "hell" {
					t.Errorf("query = %q, want %q: the backspace arm did not normalise", got, "hell")
				}
			},
		},
		{
			// Backspace from the results state drops back to the input
			// state, which is the documented escape hatch when the
			// service never answers (mode_workspace_search.go:27-29).
			name:     "backspace from the results state returns to the input state",
			opts:     wsSearchOpts(),
			setup:    seedWorkspaceResults(twoRows),
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.searchResults.Query(); got != "hell" {
					t.Errorf("query = %q, want %q", got, "hell")
				}
				if _, ok := a.searchResults.Selected(); ok {
					t.Error("still in the results state after backspace")
				}
			},
		},
		{
			name:     "a printable rune appends to the query",
			opts:     wsSearchOpts(),
			setup:    typeWorkspaceQuery("hell"),
			key:      keyPress('o'),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.searchResults.Query(); got != "hello" {
					t.Errorf("query = %q, want %q", got, "hello")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// Key.String() renders a literal space as "space", so the
			// widget's dedicated arm is what makes multi-term queries
			// typeable.
			name:     "space appends a literal space, not the word \"space\"",
			opts:     wsSearchOpts(),
			setup:    typeWorkspaceQuery("two"),
			key:      keyPress(' '),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.searchResults.Query(); got != "two " {
					t.Errorf("query = %q, want %q", got, "two ")
				}
			},
		},
		{
			// UNBOUND, and searchresults binds MORE ctrl chords than
			// channelfinder does: "ctrl+k"/"ctrl+p" alongside "up" and
			// "ctrl+j"/"ctrl+n" alongside "down"
			// (searchresults/model.go:160, :164). ctrl+x hits none of
			// them and falls to the default arm, where the single-RUNE
			// test (:180 -- rune-based here, unlike channelfinder's
			// byte-based :311) rejects the six-rune "ctrl+x".
			name:     "an unbound ctrl chord neither types nor navigates",
			opts:     wsSearchOpts(),
			setup:    typeWorkspaceQuery("hello"),
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModeWorkspaceSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.searchResults.Query(); got != "hello" {
					t.Errorf("query = %q, want %q unchanged", got, "hello")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
	})
}
