package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// ---------------------------------------------------------------------
// handleChannelFinderMode (mode_channel_finder.go:21) -- the ctrl+t
// overlay.
//
// The handler owns four outcomes, all decided from what
// channelfinder.HandleKey did with the NORMALISED key:
//
//	result != nil  -> close + ModeNormal, then route by Type/Joined
//	                  (threads view / already-joined switch / join)
//	finder closed  -> ModeNormal (this is how Esc surfaces)
//	query changed  -> debounced server-side search
//	otherwise      -> nil
//
// This is one of the three modes sharing normalizeFinderKey, and one of
// the TWO where every arm of it is load-bearing -- handleWorkspaceSearchMode
// is the other, and mode_workspace_search_test.go ships rows for all five
// arms as well. (handleSearchMode is the odd one out: only its backspace
// arm has an effect.)
//
// Load-bearing because channelfinder matches the bare strings
// "enter"/"esc"/"up"/"down"/"backspace" (channelfinder/model.go:272-306)
// and its printable-key filter is BYTE-based
// (channelfinder/model.go:311: `len(keyStr) == 1 && keyStr[0] >= 32 &&
// keyStr[0] <= 126`), so an un-normalised "shift+down" is neither
// matched nor typed. Each modified-key row below therefore fails
// outright if its arm is removed -- the finder would stay open and the
// mode would not change.
// ---------------------------------------------------------------------

// channelFinderItems is the fixture list. LastVisited is set explicitly
// on the joined pair because the empty-query sort is Joined first, then
// LastVisited DESC (channelfinder/model.go:339 ff.); leaving both at 0
// would fall through to name ASC and make the row order depend on a
// tiebreak two levels down.
//
// NewApp already registers a synthetic "Threads" destination
// (app.go:561) and SetItems preserves it, so the finder holds FOUR rows,
// not three, and the empty-query order is: Threads (synthetic, pinned),
// general (joined, newest), random (joined), design (not joined).
func channelFinderItems() []channelfinder.Item {
	return []channelfinder.Item{
		{ID: "C1", Name: "general", Type: "channel", Joined: true, LastVisited: 300},
		{ID: "C2", Name: "random", Type: "channel", Joined: true, LastVisited: 200},
		{ID: "C3", Name: "design", Type: "channel", Joined: false},
	}
}

// joinedMsg is what the test's JoinChannelFunc returns, so running the
// handler's cmd proves which channel the join was aimed at.
type joinedMsg struct {
	id   ids.ChannelID
	name string
}

// channelFinderOpts opens the finder over the fixture list and wires a
// recording join service. The sidebar is seeded with a DIFFERENT
// channel selected (C2) so the "enter on a joined channel calls
// SelectByID" row has something to observe: if the handler skipped the
// call, SelectedID would still read C2.
func channelFinderOpts() []testOpt {
	return []testOpt{
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel"},
			sidebar.ChannelItem{ID: "C3", Name: "design", Type: "channel"},
		),
		withChannelService(ChannelServiceFuncs{
			Join: func(id ids.ChannelID, name string) tea.Msg {
				return joinedMsg{id: id, name: name}
			},
		}),
		withChannelFinderOpen(channelFinderItems()...),
	}
}

// assertFinderOpen asserts the fixture actually took: the overlay is
// visible, the query is empty, and the highlighted row is the one the
// tables below assume. Every row here is about moving or acting on that
// highlight, so a wrong starting row would silently retarget them.
func assertFinderOpen(t *testing.T, a *App) {
	t.Helper()
	if !a.channelFinder.IsVisible() {
		t.Fatal("precondition: channel finder is not visible")
	}
	if got := a.channelFinder.Query(); got != "" {
		t.Fatalf("precondition: finder query = %q, want empty", got)
	}
	if got := len(a.channelFinder.FilteredItems()); got != 4 {
		t.Fatalf("precondition: %d filtered items, want 4 (3 channels + the synthetic Threads row)", got)
	}
	if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "Threads") {
		t.Fatalf("precondition: highlighted row = %q, want it to contain %q", got, "Threads")
	}
	a.sidebar.SelectByID("C2")
	if got := a.sidebar.SelectedID(); got != "C2" {
		t.Fatalf("precondition: sidebar SelectedID = %q, want C2", got)
	}
}

// parkOnGeneral moves the highlight off the pinned synthetic row onto
// the first real channel, and asserts it landed. Rows that act on a
// channel need this; a row that acted on Threads by accident would
// produce a ThreadsViewActivatedMsg and fail confusingly.
func parkOnGeneral(t *testing.T, a *App) {
	t.Helper()
	assertFinderOpen(t, a)
	a.channelFinder.HandleKey("down")
	if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "general") {
		t.Fatalf("precondition: highlighted row = %q, want it parked on %q", got, "general")
	}
}

// typeFinderQuery drives characters through the finder's own HandleKey
// (not through the mode handler), so the row under test starts from a
// filtered list without that typing counting as the dispatch.
func typeFinderQuery(q string, wantFiltered int) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		t.Helper()
		assertFinderOpen(t, a)
		for _, r := range q {
			a.channelFinder.HandleKey(string(r))
		}
		if got := a.channelFinder.Query(); got != q {
			t.Fatalf("precondition: finder query = %q, want %q", got, q)
		}
		if got := len(a.channelFinder.FilteredItems()); got != wantFiltered {
			t.Fatalf("precondition: %d filtered items for query %q, want %d", got, q, wantFiltered)
		}
	}
}

func TestChannelFinderModeKeys(t *testing.T) {
	runKeyCases(t, ModeChannelFinder, []keyCase{
		// ---- Esc ---------------------------------------------------
		{
			// Esc is not handled by the mode at all: channelfinder
			// closes itself, and the handler notices by polling
			// IsVisible (mode_channel_finder.go:51).
			name:     "esc closes the finder, and the handler notices via IsVisible",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.channelFinder.IsVisible() {
					t.Error("finder still visible after esc")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// Load-bearing normalisation: Keystroke() renders this as
			// "shift+esc", which channelfinder does not match and its
			// printable-rune filter drops (9 runes). Without
			// normalizeFinderKey's KeyEscape arm the finder would stay
			// open and the mode would stay ModeChannelFinder.
			name:     "shift+esc still closes: normalizeFinderKey strips the modifier",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyMod(tea.KeyEscape, tea.ModShift),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.channelFinder.IsVisible() {
					t.Error("finder still visible after shift+esc: the esc arm did not normalise")
				}
			},
		},

		// ---- Enter: already-joined channel -------------------------
		{
			name:     "enter on a joined channel selects it in the sidebar and emits ChannelSelectedMsg",
			opts:     channelFinderOpts(),
			setup:    parkOnGeneral,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.channelFinder.IsVisible() {
					t.Error("finder still visible after a selection")
				}
				if got := a.sidebar.SelectedID(); got != "C1" {
					t.Errorf("sidebar SelectedID = %q, want C1 (SelectByID was not called)", got)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want a ChannelSelectedMsg cmd")
				}
				sel, ok := cmd().(ChannelSelectedMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want ChannelSelectedMsg", cmd())
				}
				if sel.ID != "C1" || sel.Name != "general" || sel.Type != "channel" {
					t.Errorf("ChannelSelectedMsg = %+v, want {ID:C1 Name:general Type:channel}", sel)
				}
			},
		},
		{
			// The Enter counterpart of the shift+esc row: without the
			// KeyEnter arm the finder sees "shift+enter", drops it, and
			// returns nil -- no selection, no mode change.
			name:     "shift+enter still selects: normalizeFinderKey strips the modifier",
			opts:     channelFinderOpts(),
			setup:    parkOnGeneral,
			key:      keyMod(tea.KeyEnter, tea.ModShift),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil: the enter arm did not normalise")
				}
				if sel, ok := cmd().(ChannelSelectedMsg); !ok || sel.ID != "C1" {
					t.Errorf("cmd() = %#v, want ChannelSelectedMsg for C1", cmd())
				}
			},
		},

		// ---- Enter: not-yet-joined channel -------------------------
		{
			// Row 3 (design) is the only non-joined fixture item, so
			// the query narrows to it rather than moving the cursor
			// three times.
			name:     "enter on a channel the user has not joined fires the join service",
			opts:     channelFinderOpts(),
			setup:    typeFinderQuery("design", 1),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.channelFinder.IsVisible() {
					t.Error("finder still visible after a selection")
				}
				// The join path deliberately does NOT touch the sidebar
				// -- ChannelJoinedMsg folds the channel in later
				// (mode_channel_finder.go:34-36).
				if got := a.sidebar.SelectedID(); got != "C2" {
					t.Errorf("sidebar SelectedID = %q, want C2 unchanged on the join path", got)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the join cmd")
				}
				j, ok := cmd().(joinedMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want the ChannelService's join result", cmd())
				}
				if j.id != "C3" || j.name != "design" {
					t.Errorf("join called with (%q, %q), want (C3, design)", j.id, j.name)
				}
			},
		},

		// ---- Enter: synthetic destination --------------------------
		{
			// The synthetic row is pinned to the top under an empty
			// query, so it is what the default highlight sits on.
			name:     "enter on the synthetic Threads row activates the threads view instead of a channel",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.channelFinder.IsVisible() {
					t.Error("finder still visible after a selection")
				}
				if got := a.sidebar.SelectedID(); got != "C2" {
					t.Errorf("sidebar SelectedID = %q, want C2 unchanged for a synthetic destination", got)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want ThreadsViewActivatedMsg")
				}
				if _, ok := cmd().(ThreadsViewActivatedMsg); !ok {
					t.Errorf("cmd() = %T, want ThreadsViewActivatedMsg", cmd())
				}
			},
		},
		{
			// With every item filtered out, HandleKey("enter") returns
			// nil and the finder stays open: the handler must NOT drop
			// to Normal, or the overlay would be stranded on screen
			// with no mode routing keys to it.
			name:     "enter with nothing matching leaves the finder open and the mode unchanged",
			opts:     channelFinderOpts(),
			setup:    typeFinderQuery("zzz", 0),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.channelFinder.IsVisible() {
					t.Error("finder closed on an empty-result enter")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// ---- navigation --------------------------------------------
		{
			name:     "down moves the highlight to the next row",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "general") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "general")
				}
				// Navigation does not change the query, so no server
				// search is scheduled.
				if cmd != nil {
					t.Errorf("cmd = %T, want nil (navigation must not schedule a search)", cmd)
				}
			},
		},
		{
			// Proves the "down" arm end to end: the normalised key has
			// to reach the widget for the highlight to move, and the
			// unnormalised "shift+down" would be dropped as a 10-rune
			// non-printable.
			name:     "shift+down moves the highlight: normalizeFinderKey strips the modifier",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyMod(tea.KeyDown, tea.ModShift),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "general") {
					t.Errorf("highlighted row = %q, want %q: the down arm did not normalise", got, "general")
				}
			},
		},
		{
			// Same for "up", from a cursor deliberately parked on row 2
			// so the move is observable. An up row starting at the top
			// would be inert and could not tell normalisation from a
			// dropped key.
			name:     "shift+up moves the highlight back: normalizeFinderKey strips the modifier",
			opts:     channelFinderOpts(),
			setup:    parkOnGeneral,
			key:      keyMod(tea.KeyUp, tea.ModShift),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "Threads") {
					t.Errorf("highlighted row = %q, want %q: the up arm did not normalise", got, "Threads")
				}
			},
		},
		{
			// The `m.selected > 0` guard (channelfinder/model.go:296).
			//
			// Honest limit, same as the unbound-ctrl-chord row below:
			// a pinned-and-unmoved highlight is also what a key that
			// never reached the widget produces, so the assertions
			// alone do not prove dispatch. What they DO catch is the
			// guard's removal -- `m.selected--` would run to -1, and
			// the render no longer highlights the Threads row. The
			// preceding shift+up row is what pins the "up" arm proper.
			name:     "up at the top row is inert but keeps the finder open",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyCode(tea.KeyUp),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "Threads") {
					t.Errorf("highlighted row = %q, want it to stay on %q", got, "Threads")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// ---- query editing + debounced server search ---------------
		{
			// The local filter runs synchronously inside HandleKey; only
			// the server query is deferred, so both effects are asserted
			// here. A 1ms debounce makes running the tick cheap enough
			// to prove the query is carried into the message.
			// "de" is pre-typed through the widget so the dispatched
			// key narrows to exactly one row. A bare "d" would not:
			// it is a subsequence of "Threads" and "random" too, and
			// the highlight would stay on the pinned synthetic row.
			name: "a printable rune filters locally and schedules a debounced server search",
			opts: channelFinderOpts(),
			setup: func(t *testing.T, a *App) {
				typeFinderQuery("de", 1)(t, a)
				a.channelSearchDebounce = time.Millisecond
			},
			key:      keyPress('s'),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.channelFinder.Query(); got != "des" {
					t.Errorf("finder query = %q, want %q", got, "des")
				}
				if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "design") {
					t.Errorf("highlighted row = %q, want the local filter to have run", got)
				}
				if a.pendingChannelSearchGen != 1 {
					t.Errorf("pendingChannelSearchGen = %d, want 1", a.pendingChannelSearchGen)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the debounce tick")
				}
				got, ok := cmd().(channelSearchDebounceMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want channelSearchDebounceMsg", cmd())
				}
				if got.query != "des" || got.gen != 1 {
					t.Errorf("debounce msg = %+v, want {query:des gen:1}", got)
				}
			},
		},
		{
			// The last normalizeFinderKey arm. Unmodified Backspace
			// already stringifies to "backspace", so only the modified
			// form separates live normalisation from dead code.
			name: "shift+backspace edits the query: normalizeFinderKey strips the modifier",
			opts: channelFinderOpts(),
			setup: func(t *testing.T, a *App) {
				typeFinderQuery("de", 1)(t, a)
			},
			key:      keyMod(tea.KeyBackspace, tea.ModShift),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.channelFinder.Query(); got != "d" {
					t.Errorf("finder query = %q, want %q: the backspace arm did not normalise", got, "d")
				}
				if cmd == nil {
					t.Error("cmd = nil, want a rescheduled search for the shortened query")
				}
			},
		},
		{
			// Backspacing to empty still counts as a query change, so
			// the generation is bumped (invalidating any in-flight
			// search) even though scheduleChannelSearch returns nil for
			// an empty query (app.go:3443-3445).
			name: "backspace to an empty query bumps the generation but schedules nothing",
			opts: channelFinderOpts(),
			setup: func(t *testing.T, a *App) {
				// "d" is a subsequence of Threads, random and design.
				typeFinderQuery("d", 3)(t, a)
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.channelFinder.Query(); got != "" {
					t.Errorf("finder query = %q, want empty", got)
				}
				if a.pendingChannelSearchGen != 1 {
					t.Errorf("pendingChannelSearchGen = %d, want 1 (bumped before the empty-query bail)", a.pendingChannelSearchGen)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil for an empty query", cmd)
				}
			},
		},
		{
			// Backspace on an already-empty query leaves everything
			// alone, including the generation counter -- the "did the
			// query change" guard (mode_channel_finder.go:59) is what
			// prevents a pointless reschedule.
			name:     "backspace on an empty query changes nothing at all",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.channelFinder.Query(); got != "" {
					t.Errorf("finder query = %q, want empty", got)
				}
				if a.pendingChannelSearchGen != 0 {
					t.Errorf("pendingChannelSearchGen = %d, want 0 (no query change, no reschedule)", a.pendingChannelSearchGen)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// UNBOUND is the operative word: channelfinder DOES bind two
			// ctrl chords -- "ctrl+n" alongside "down" and "ctrl+p"
			// alongside "up" (channelfinder/model.go:289, :295) -- so
			// "a ctrl chord does nothing" would be false as a general
			// claim. ctrl+x hits neither, falls past the switch, and is
			// then rejected by the printable filter at :311 because
			// "ctrl+x" is six bytes long.
			//
			// Pinned because a naive query append would insert the
			// literal "ctrl+x" into the filter.
			//
			// Honest limit: the assertions here would also hold if the
			// key never reached the widget. What establishes that it
			// does is a mutation -- dropping `len(keyStr) == 1` from
			// :311 makes the query read "ctrl+x" and this row fail.
			name:     "an unbound ctrl chord neither types nor navigates",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.channelFinder.Query(); got != "" {
					t.Errorf("finder query = %q, want empty", got)
				}
				if got := modalHighlightedRow(t, a.channelFinder.View(120)); !strings.Contains(got, "Threads") {
					t.Errorf("highlighted row = %q, want it to stay on %q", got, "Threads")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// BUG?: channelfinder's printable filter is BYTE-based --
			// `len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126`
			// (channelfinder/model.go:311) -- byte-for-byte the same
			// predicate as mode_command.go:57, which this commit already
			// flags via TestCommandMode_NonASCIIRuneIsDropped. "e" with
			// an acute accent is two bytes, fails the length test, and
			// never reaches the query: a channel whose name carries
			// non-ASCII characters cannot be searched by them.
			//
			// The two sites are materially identical, coupling included:
			// channelfinder's backspace at :303 slices `m.query` by
			// BYTE, exactly as mode_command.go:49 slices `a.cmdline`.
			// Widening either filter alone would let the matching
			// backspace cut a multi-byte rune in half. Neither is
			// fixable in isolation -- which is why both are recorded
			// rather than fixed.
			//
			// searchresults is NOT affected: its default arm counts
			// RUNES (searchresults/model.go:180).
			//
			// Tracked as https://github.com/gammons/slk/issues/187,
			// which covers both sites. WHEN THAT BUG IS FIXED: the rune
			// reaches the query, so re-pin Query() to "é", expect the
			// list to re-filter (0 items against this fixture) and a
			// non-nil reschedule cmd.
			name:     "a non-ASCII rune never reaches the query: the printable filter is byte-based",
			opts:     channelFinderOpts(),
			setup:    assertFinderOpen,
			key:      keyPress('é'),
			wantMode: ModeChannelFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.channelFinder.Query(); got != "" {
					t.Errorf("finder query = %q, want empty: the byte-based filter should have dropped it", got)
				}
				if got := len(a.channelFinder.FilteredItems()); got != 4 {
					t.Errorf("%d filtered items, want 4 (the list never re-filtered)", got)
				}
				// No query change means no reschedule, which is the
				// second observable that the rune was dropped rather
				// than accepted-then-filtered-to-nothing.
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
	})
}
