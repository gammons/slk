package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// ---------------------------------------------------------------------
// handleSearchMode (mode_search.go:21) -- the in-channel `/` prompt.
//
// Two guards run first, both through key.Matches (i.e. against
// msg.String()): Escape cancels, Enter dispatches. Everything else
// falls through to a three-arm text editor keyed on
// normalizeFinderKey's output.
//
// The prompt buffer is App.searchInput and the only rendered evidence
// is the status bar's search segment, which statusbar.Model exposes via
// Search() (statusbar/model.go:145). Rows assert both: the buffer is
// the state, the segment is what the user sees, and they have drifted
// apart before (the Escape-with-active-search arm deliberately sets a
// segment that does NOT mirror the buffer).
// ---------------------------------------------------------------------

// searchOpts is the shared bundle: one channel, active, so the Enter
// arm has a real channel ID to hand the SearchService.
func searchOpts() []testOpt {
	return []testOpt{
		withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}),
		withActiveChannel("C1"),
	}
}

// typeSearch seeds the prompt buffer and the segment that mirrors it,
// exactly as a run of printable keys through this handler would have
// left them. Asserting afterwards is not paranoia: several rows below
// are about *editing* that buffer, and an empty buffer would make a
// backspace row pass by doing nothing.
func typeSearch(s string) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		t.Helper()
		a.searchInput = s
		a.statusbar.SetSearch("/" + s)
		if a.searchInput != s {
			t.Fatalf("precondition: searchInput = %q, want %q", a.searchInput, s)
		}
	}
}

// searchSvcRecorder captures what the Enter arm hands the
// SearchService. reply is what the service answers with.
type searchSvcRecorder struct {
	calls   int
	channel ids.ChannelID
	query   string
	reply   tea.Msg
}

func (r *searchSvcRecorder) install(a *App) {
	a.SetSearchService(NewSearchService(SearchServiceFuncs{
		SearchChannel: func(ch ids.ChannelID, q string) tea.Msg {
			r.calls++
			r.channel, r.query = ch, q
			return r.reply
		},
	}))
}

func TestSearchModeKeys(t *testing.T) {
	// One recorder per row that needs one; declared here so the
	// closures in setup and assert share it. runKeyCases' subtests are
	// sequential (nothing in this package calls t.Parallel).
	stamped := &searchSvcRecorder{reply: ChannelSearchResultsMsg{ChannelID: "C1", Query: "hello", TSes: []string{"1.0"}}}
	passthrough := &searchSvcRecorder{reply: ToastMsg{Text: "search failed"}}
	emptyEnter := &searchSvcRecorder{reply: ChannelSearchResultsMsg{}}

	runKeyCases(t, ModeSearch, []keyCase{
		// ---- Escape ------------------------------------------------
		{
			name:     "esc with no active search clears the buffer and blanks the segment",
			opts:     searchOpts(),
			setup:    typeSearch("part"),
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.searchInput != "" {
					t.Errorf("searchInput = %q, want empty", a.searchInput)
				}
				if got := a.statusbar.Search(); got != "" {
					t.Errorf("status search segment = %q, want empty", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %v, want nil", cmd)
				}
			},
		},
		{
			// Cancelling a re-entered prompt must not blank the i/N
			// indicator of the search that is still active underneath
			// (mode_search.go:28-29). idx is 0-based, so idx=1 of two
			// matches renders "2/2".
			name: "esc on top of an active search restores its i/N indicator",
			opts: searchOpts(),
			setup: func(t *testing.T, a *App) {
				typeSearch("part")(t, a)
				a.search = &activeSearch{
					query:   "hello",
					terms:   []string{"hello"},
					matches: []string{"1.0", "2.0"},
					idx:     1,
				}
				if a.search == nil || len(a.search.matches) != 2 {
					t.Fatal("precondition: active search not seeded")
				}
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "" {
					t.Errorf("searchInput = %q, want empty", a.searchInput)
				}
				if got, want := a.statusbar.Search(), "/hello  2/2"; got != want {
					t.Errorf("status search segment = %q, want %q", got, want)
				}
				if a.search == nil {
					t.Error("esc cleared the underlying active search; it should survive a cancelled re-entry")
				}
			},
		},
		{
			// key.Matches compares msg.String(), which prefixes the
			// modifier, so shift+esc misses the Escape binding and
			// lands in the text editor -- where normalizeFinderKey has
			// already rewritten it to "esc", a 3-rune string the
			// single-rune filter drops. Net effect: nothing but a
			// segment redraw, and the prompt stays open.
			//
			// BUG?: a user holding shift cannot cancel the prompt.
			// Recorded, not fixed.
			name:     "shift+esc does not cancel: key.Matches sees the modifier, the rune filter drops it",
			opts:     searchOpts(),
			setup:    typeSearch("part"),
			key:      keyMod(tea.KeyEscape, tea.ModShift),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "part" {
					t.Errorf("searchInput = %q, want %q unchanged", a.searchInput, "part")
				}
				if got, want := a.statusbar.Search(), "/part"; got != want {
					t.Errorf("status search segment = %q, want %q", got, want)
				}
			},
		},

		// ---- Enter -------------------------------------------------
		{
			name: "enter dispatches the trimmed query to the SearchService and stamps the generation",
			opts: searchOpts(),
			setup: func(t *testing.T, a *App) {
				typeSearch("  hello  ")(t, a)
				stamped.install(a)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.searchInput != "" {
					t.Errorf("searchInput = %q, want empty after dispatch", a.searchInput)
				}
				if got, want := a.statusbar.Search(), "/hello  …"; got != want {
					t.Errorf("status search segment = %q, want %q", got, want)
				}
				if a.searchGen != 1 {
					t.Errorf("searchGen = %d, want 1 (bumped once per dispatch)", a.searchGen)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the search dispatch cmd")
				}
				// The service is called lazily, by the cmd.
				if stamped.calls != 0 {
					t.Errorf("service called %d times before the cmd ran, want 0", stamped.calls)
				}
				got := cmd()
				if stamped.calls != 1 {
					t.Errorf("service called %d times, want 1", stamped.calls)
				}
				if stamped.channel != "C1" {
					t.Errorf("service got channel %q, want C1", stamped.channel)
				}
				if stamped.query != "hello" {
					t.Errorf("service got query %q, want %q (whitespace trimmed)", stamped.query, "hello")
				}
				res, ok := got.(ChannelSearchResultsMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want ChannelSearchResultsMsg", got)
				}
				if res.Gen != a.searchGen {
					t.Errorf("result Gen = %d, want %d (the dispatch generation)", res.Gen, a.searchGen)
				}
			},
		},
		{
			// The stamping wrapper only rewrites ChannelSearchResultsMsg;
			// anything else (an error toast from the service) is
			// returned untouched (mode_search.go:56-60).
			name: "a non-result answer from the service passes through unstamped",
			opts: searchOpts(),
			setup: func(t *testing.T, a *App) {
				typeSearch("hello")(t, a)
				passthrough.install(a)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the search dispatch cmd")
				}
				got := cmd()
				toast, ok := got.(ToastMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want the service's ToastMsg passed through", got)
				}
				if toast.Text != "search failed" {
					t.Errorf("toast text = %q, want %q", toast.Text, "search failed")
				}
			},
		},
		{
			// Empty (or whitespace-only) query is a "clear", not a
			// dispatch: clearActiveSearch drops the highlight terms and
			// the segment, and no cmd is produced.
			name: "enter on a whitespace-only query clears the active search instead of dispatching",
			opts: searchOpts(),
			setup: func(t *testing.T, a *App) {
				typeSearch("   ")(t, a)
				emptyEnter.install(a)
				a.search = &activeSearch{query: "old", matches: []string{"1.0"}}
				if a.search == nil {
					t.Fatal("precondition: active search not seeded")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %v, want nil for an empty query", cmd)
				}
				if emptyEnter.calls != 0 {
					t.Errorf("service called %d times, want 0", emptyEnter.calls)
				}
				if a.search != nil {
					t.Error("active search survived an empty-query Enter")
				}
				if got := a.statusbar.Search(); got != "" {
					t.Errorf("status search segment = %q, want empty", got)
				}
			},
		},
		{
			// Mirror of the shift+esc row: shift+enter misses the Enter
			// binding, then normalizeFinderKey turns it into "enter",
			// which the rune filter drops.
			//
			// BUG?: shift+enter neither submits nor types. Recorded.
			name:     "shift+enter does not submit: key.Matches sees the modifier, the rune filter drops it",
			opts:     searchOpts(),
			setup:    typeSearch("hello"),
			key:      keyMod(tea.KeyEnter, tea.ModShift),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %v, want nil", cmd)
				}
				if a.searchInput != "hello" {
					t.Errorf("searchInput = %q, want %q unchanged", a.searchInput, "hello")
				}
			},
		},

		// ---- text editing ------------------------------------------
		{
			name:     "a printable rune appends to the buffer and the segment",
			opts:     searchOpts(),
			setup:    typeSearch("hel"),
			key:      keyPress('p'),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "help" {
					t.Errorf("searchInput = %q, want %q", a.searchInput, "help")
				}
				if got, want := a.statusbar.Search(), "/help"; got != want {
					t.Errorf("status search segment = %q, want %q", got, want)
				}
			},
		},
		{
			// Key.String() renders a literal space as "space"
			// regardless of Key.Text (verified), so the dedicated arm
			// at mode_search.go:70 is live: without it the 5-rune
			// "space" would be dropped and multi-term queries would be
			// impossible to type.
			name:     "space appends a literal space, not the word \"space\"",
			opts:     searchOpts(),
			setup:    typeSearch("two"),
			key:      keyPress(' '),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "two " {
					t.Errorf("searchInput = %q, want %q", a.searchInput, "two ")
				}
			},
		},
		{
			name:     "backspace drops the last rune",
			opts:     searchOpts(),
			setup:    typeSearch("help"),
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "hel" {
					t.Errorf("searchInput = %q, want %q", a.searchInput, "hel")
				}
				if got, want := a.statusbar.Search(), "/hel"; got != want {
					t.Errorf("status search segment = %q, want %q", got, want)
				}
			},
		},
		{
			// Deletion is rune-wise, not byte-wise: a 4-byte grapheme
			// must go in one press. A byte-wise slice would leave
			// invalid UTF-8 here.
			name:     "backspace deletes a whole multi-byte rune",
			opts:     searchOpts(),
			setup:    typeSearch("aé"),
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "a" {
					t.Errorf("searchInput = %q, want %q", a.searchInput, "a")
				}
			},
		},
		{
			name:     "backspace on an empty buffer is a no-op and does not leave the mode",
			opts:     searchOpts(),
			setup:    typeSearch(""),
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "" {
					t.Errorf("searchInput = %q, want empty", a.searchInput)
				}
				if got, want := a.statusbar.Search(), "/"; got != want {
					t.Errorf("status search segment = %q, want %q (the bare prompt)", got, want)
				}
			},
		},
		{
			// The one input in this mode that separates live
			// normalisation from dead code. Unmodified Backspace
			// already stringifies to "backspace", so only the modified
			// form can tell: without normalizeFinderKey the handler
			// would see "shift+backspace" (15 runes, dropped) and the
			// buffer would still read "help".
			name:     "shift+backspace deletes: normalizeFinderKey strips the modifier",
			opts:     searchOpts(),
			setup:    typeSearch("help"),
			key:      keyMod(tea.KeyBackspace, tea.ModShift),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "hel" {
					t.Errorf("searchInput = %q, want %q: shift+backspace did not normalise to \"backspace\"", a.searchInput, "hel")
				}
			},
		},
		{
			// normalizeFinderKey's "up"/"down" arms are inert here:
			// handleSearchMode has no navigation, so both the
			// normalised "up" and the raw "shift+up" are multi-rune and
			// get dropped. The row pins that arrows do not corrupt the
			// query, which a naive `a.searchInput += msg.String()`
			// would.
			name:     "up is dropped by the single-rune filter and leaves the query intact",
			opts:     searchOpts(),
			setup:    typeSearch("help"),
			key:      keyCode(tea.KeyUp),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "help" {
					t.Errorf("searchInput = %q, want %q unchanged", a.searchInput, "help")
				}
			},
		},
		{
			name:     "a ctrl-modified key is dropped by the single-rune filter",
			opts:     searchOpts(),
			setup:    typeSearch("help"),
			key:      keyMod('v', tea.ModCtrl),
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "help" {
					t.Errorf("searchInput = %q, want %q unchanged (\"ctrl+v\" is 6 runes)", a.searchInput, "help")
				}
			},
		},
		{
			// Documented v1 limitation (mode_search.go:75-77): a
			// multi-rune grapheme is dropped rather than appended.
			// Pinned so a future widening of the filter is a deliberate
			// change, not an accident.
			name:     "a multi-rune grapheme is dropped -- the documented v1 limitation",
			opts:     searchOpts(),
			setup:    typeSearch("help"),
			key:      tea.KeyPressMsg{Code: '👍', Text: "👍🏽"},
			wantMode: ModeSearch,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.searchInput != "help" {
					t.Errorf("searchInput = %q, want %q unchanged", a.searchInput, "help")
				}
			},
		},
	})
}
