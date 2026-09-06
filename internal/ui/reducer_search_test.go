// internal/ui/reducer_search_test.go
//
// Tests for the in-channel `/` search: the ChannelSearchResultsMsg
// reducer (jump-to-nearest, stale-drop, no-match, off-buffer fetch),
// n/N navigation with wrap, Esc clearing, and the ModeSearch prompt
// (enter/cancel) flow.
package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/gammons/slk/internal/ui/styles"
)

func searchTestApp(t *testing.T) *App {
	t.Helper()
	// withSize(0, 0) preserves NewApp's unsized state: the original
	// builder set no dimensions and these tests never render.
	return newTestApp(t,
		withSize(0, 0),
		withActiveChannel("C1"),
		withMessages(
			messages.MessageItem{TS: "1700000001.000000", Text: "deploy went fine"},
			messages.MessageItem{TS: "1700000002.000000", Text: "lunch?"},
			messages.MessageItem{TS: "1700000003.000000", Text: "deployment failed"},
		),
	)
}

func resultsMsg(tses ...string) ChannelSearchResultsMsg {
	return ChannelSearchResultsMsg{
		ChannelID: "C1",
		Query:     "deploy",
		Terms:     []string{"deploy"},
		TSes:      tses, // newest first
	}
}

func TestSearchResults_JumpsToNearestAtOrOlderThanCursor(t *testing.T) {
	app := searchTestApp(t)
	app.messagepane.SelectByTS("1700000002.000000") // cursor between the two matches

	app.Update(resultsMsg("1700000003.000000", "1700000001.000000"))

	sel, _ := app.messagepane.SelectedMessage()
	if sel.TS != "1700000001.000000" {
		t.Fatalf("selected %s, want nearest at-or-older match", sel.TS)
	}
	if app.search == nil || app.search.idx != 1 {
		t.Fatalf("active search idx = %+v", app.search)
	}
}

func TestSearchResults_NoMatchesSetsStatusAndNoState(t *testing.T) {
	app := searchTestApp(t)
	app.Update(ChannelSearchResultsMsg{ChannelID: "C1", Query: "zzz"})
	if app.search != nil {
		t.Fatal("no-match search should not leave active state")
	}
}

func TestSearchResults_StaleChannelDropped(t *testing.T) {
	app := searchTestApp(t)
	app.Update(ChannelSearchResultsMsg{ChannelID: "C9", Query: "deploy", TSes: []string{"1.0"}})
	if app.search != nil {
		t.Fatal("stale channel results applied")
	}
}

func TestSearchNextPrev_WrapAndNavigate(t *testing.T) {
	app := searchTestApp(t)
	app.Update(resultsMsg("1700000003.000000", "1700000001.000000"))
	// jumped to newest match (cursor starts at bottom) -> idx 0

	app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}) // older
	sel, _ := app.messagepane.SelectedMessage()
	if sel.TS != "1700000001.000000" {
		t.Fatalf("n: selected %s", sel.TS)
	}

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}) // wrap to newest
	sel, _ = app.messagepane.SelectedMessage()
	if sel.TS != "1700000003.000000" {
		t.Fatalf("n wrap: selected %s", sel.TS)
	}
	wrapped := false
	for _, m := range drainCmd(cmd) {
		if tm, ok := m.(ToastMsg); ok && tm.Text == "Search wrapped" {
			wrapped = true
		}
	}
	if !wrapped {
		t.Fatal("expected 'Search wrapped' toast")
	}

	app.Update(tea.KeyPressMsg{Code: 'N', Text: "N"}) // newer wraps to oldest
	sel, _ = app.messagepane.SelectedMessage()
	if sel.TS != "1700000001.000000" {
		t.Fatalf("N wrap: selected %s", sel.TS)
	}
}

func TestSearchEscClears(t *testing.T) {
	app := searchTestApp(t)
	app.Update(resultsMsg("1700000003.000000"))
	if app.search == nil {
		t.Fatal("precondition: active search")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.search != nil {
		t.Fatal("Esc did not clear active search")
	}
}

func TestSearchOffBufferMatchTriggersFetchAround(t *testing.T) {
	app := searchTestApp(t)
	var fetchedTS string
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
		fetchedTS = string(ts)
		return nil
	})
	// Match older than anything in the buffer.
	_, cmd := app.Update(resultsMsg("1600000000.000000"))
	drainCmd(cmd)
	if fetchedTS != "1600000000.000000" {
		t.Fatalf("FetchAround not dispatched for off-buffer match (got %q)", fetchedTS)
	}
}

func TestSlashEntersSearchModeAndEnterExecutes(t *testing.T) {
	app := searchTestApp(t)
	var gotChannel, gotQuery string
	app.SetSearchService(NewSearchService(SearchServiceFuncs{
		SearchChannel: func(channelID ids.ChannelID, query string) tea.Msg {
			gotChannel, gotQuery = string(channelID), query
			return nil
		},
	}))

	app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if app.mode != ModeSearch {
		t.Fatalf("mode = %v, want ModeSearch", app.mode)
	}
	for _, r := range "deploy" {
		app.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	drainCmd(cmd)

	if app.mode != ModeNormal {
		t.Fatalf("mode after Enter = %v", app.mode)
	}
	if gotChannel != "C1" || gotQuery != "deploy" {
		t.Fatalf("SearchChannel(%q, %q)", gotChannel, gotQuery)
	}
}

func TestSearchModeEscCancels(t *testing.T) {
	app := searchTestApp(t)
	app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	app.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.mode != ModeNormal || app.searchInput != "" {
		t.Fatalf("Esc: mode=%v input=%q", app.mode, app.searchInput)
	}
}

// After a no-match search there is no active search state (a.search ==
// nil) but the `/foo  no matches` segment lingers in the status bar;
// Esc must clear it instead of falling through to thread/edit handling.
func TestEscClearsNoMatchesStatus(t *testing.T) {
	app := searchTestApp(t)
	app.Update(ChannelSearchResultsMsg{ChannelID: "C1", Query: "zzz"})
	if app.statusbar.Search() == "" {
		t.Fatal("precondition: no-matches status segment set")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := app.statusbar.Search(); got != "" {
		t.Fatalf("statusbar search segment = %q after Esc, want empty", got)
	}
}

func TestChannelSwitchClearsNoMatchesStatus(t *testing.T) {
	app := searchTestApp(t)
	app.Update(ChannelSearchResultsMsg{ChannelID: "C1", Query: "zzz"})
	if app.statusbar.Search() == "" {
		t.Fatal("precondition: no-matches status segment set")
	}
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "other"})
	if got := app.statusbar.Search(); got != "" {
		t.Fatalf("statusbar search segment = %q after channel switch, want empty", got)
	}
}

// searchDispatch drives the real `/` prompt flow: enters search mode,
// types query, presses Enter, and returns the dispatch cmd (unrun, so
// tests control when the "network" result lands).
func searchDispatch(t *testing.T, app *App, query string) tea.Cmd {
	t.Helper()
	app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range query {
		app.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return cmd
}

func TestSearchClearWhilePendingDropsLateResult(t *testing.T) {
	app := searchTestApp(t)
	app.SetSearchService(NewSearchService(SearchServiceFuncs{
		SearchChannel: func(channelID ids.ChannelID, query string) tea.Msg {
			return resultsMsg("1700000003.000000")
		},
	}))
	cmd := searchDispatch(t, app, "deploy")
	// User cancels (Esc / channel switch) while the query is in flight.
	app.clearActiveSearch()
	for _, m := range drainCmd(cmd) {
		app.Update(m)
	}
	if app.search != nil {
		t.Fatal("late result applied after clearActiveSearch")
	}
	if got := app.statusbar.Search(); got != "" {
		t.Fatalf("statusbar search segment = %q, want empty", got)
	}
}

func TestSearchNewDispatchSupersedesOldResult(t *testing.T) {
	app := searchTestApp(t)
	app.SetSearchService(NewSearchService(SearchServiceFuncs{
		SearchChannel: func(channelID ids.ChannelID, query string) tea.Msg {
			m := resultsMsg("1700000003.000000")
			m.Query = query
			return m
		},
	}))
	cmdA := searchDispatch(t, app, "alpha")
	cmdB := searchDispatch(t, app, "beta")
	// A's result arrives after B was dispatched: superseded, dropped.
	for _, m := range drainCmd(cmdA) {
		app.Update(m)
	}
	if app.search != nil {
		t.Fatalf("superseded result applied: %+v", app.search)
	}
	for _, m := range drainCmd(cmdB) {
		app.Update(m)
	}
	if app.search == nil || app.search.query != "beta" {
		t.Fatalf("current result not applied: %+v", app.search)
	}
}

func TestPasteInSearchModeAppendsToInput(t *testing.T) {
	app := searchTestApp(t)
	app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	app.Update(tea.PasteMsg{Content: "deploy\r\nfailed"})
	if app.searchInput != "deploy failed" {
		t.Fatalf("searchInput = %q, want pasted text with newlines stripped", app.searchInput)
	}
	if got := app.statusbar.Search(); got != "/deploy failed" {
		t.Fatalf("statusbar search segment = %q", got)
	}
}

func TestSearchModeEscRestoresMatchIndicator(t *testing.T) {
	app := searchTestApp(t)
	app.Update(resultsMsg("1700000003.000000", "1700000001.000000"))
	// Re-enter the `/` prompt, then bail out: the active search
	// survives, so its i/N indicator should come back.
	app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := app.statusbar.Search(); got != "/deploy  1/2" {
		t.Fatalf("statusbar search segment = %q, want restored match indicator", got)
	}
}

func TestSearchNextGatedOffThreadPanel(t *testing.T) {
	app := searchTestApp(t)
	app.Update(resultsMsg("1700000003.000000", "1700000001.000000"))
	app.focusedPanel = PanelThread
	app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if app.search == nil || app.search.idx != 0 {
		t.Fatalf("n advanced search while thread focused: %+v", app.search)
	}
}

func TestCtrlFOpensWorkspaceSearch(t *testing.T) {
	app := searchTestApp(t)
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if app.mode != ModeWorkspaceSearch || !app.searchResults.IsVisible() {
		t.Fatalf("mode=%v visible=%v", app.mode, app.searchResults.IsVisible())
	}
}

func TestWorkspaceSearchSubmitAndResults(t *testing.T) {
	app := searchTestApp(t)
	var gotQuery string
	app.SetSearchService(NewSearchService(SearchServiceFuncs{
		SearchWorkspace: func(query string) tea.Msg {
			gotQuery = query
			return WorkspaceSearchResultsMsg{Query: query, Items: []searchresults.Item{
				{ChannelID: "C2", ChannelName: "ops", TS: "2.0", Text: "hit"},
			}, Total: 1}
		},
	}))
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	app.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, m := range drainCmd(cmd) {
		app.Update(m)
	}
	if gotQuery != "q" {
		t.Fatalf("query = %q", gotQuery)
	}
	if sel, ok := app.searchResults.Selected(); !ok || sel.ChannelID != "C2" {
		t.Fatalf("results not installed: %+v ok=%v", sel, ok)
	}
}

func TestWorkspaceSearchSelectNavigates(t *testing.T) {
	app := searchTestApp(t)
	// Lookup hit: the channel is known to the sidebar/finder (member).
	app.setChannelLookupFuncForTest(func(id ids.ChannelID) (string, string, bool) {
		if id == "C2" {
			return "ops", "im", true
		}
		return "", "", false
	})
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	app.searchResults.HandleKey("q")
	app.searchResults.HandleKey("enter")
	app.searchResults.SetResults([]searchresults.Item{
		{ChannelID: "C2", ChannelName: "ops", TS: "2.0"},
	}, 1)

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := drainCmd(cmd)
	var selected *ChannelSelectedMsg
	for _, m := range msgs {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			selected = &cs
		}
	}
	if selected == nil || selected.ID != "C2" {
		t.Fatalf("no ChannelSelectedMsg dispatched: %v", msgs)
	}
	if selected.Name != "ops" || selected.Type != "im" {
		t.Fatalf("ChannelSelectedMsg not resolved via Lookup: %+v", selected)
	}
	if app.pendingLinkNav == nil || app.pendingLinkNav.messageTS != "2.0" {
		t.Fatalf("pending nav = %+v", app.pendingLinkNav)
	}
	if app.mode != ModeNormal || app.searchResults.IsVisible() {
		t.Fatal("modal not closed")
	}
}

func TestWorkspaceSearchSelectNonMemberToastsInsteadOfNavigating(t *testing.T) {
	app := searchTestApp(t)
	// No Lookup wired -> every Lookup misses: the hit is in a public
	// channel the user hasn't joined (unknown to the sidebar/finder).
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	app.searchResults.HandleKey("q")
	app.searchResults.HandleKey("enter")
	app.searchResults.SetResults([]searchresults.Item{
		{ChannelID: "C9", ChannelName: "random", TS: "3.0"},
	}, 1)

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := drainCmd(cmd)
	var toast string
	for _, m := range msgs {
		switch tm := m.(type) {
		case ChannelSelectedMsg:
			t.Fatalf("navigated to non-member channel: %+v", tm)
		case ToastMsg:
			toast = tm.Text
		}
	}
	if !strings.Contains(toast, "Not a member of #random") {
		t.Fatalf("expected non-member toast, got %q (msgs: %v)", toast, msgs)
	}
	if app.pendingLinkNav != nil {
		t.Fatalf("pendingLinkNav leaked: %+v", app.pendingLinkNav)
	}
	if app.mode != ModeNormal || app.searchResults.IsVisible() {
		t.Fatal("modal not closed")
	}
}

func TestWorkspaceHighlightTermsDerivation(t *testing.T) {
	cases := []struct {
		query string
		want  []string
	}{
		// Plain terms are folded (lowercased, diacritics stripped).
		{"Deploy Café", []string{"deploy", "cafe"}},
		// Slack modifiers (anything with a ':') are skipped.
		{"deploy from:@bob in:#general before:2026-01-01", []string{"deploy"}},
		// Modifiers-only query yields no highlightable terms.
		{"from:@bob in:#general", nil},
		{"", nil},
		{"   ", nil},
		// Quoted phrases highlight their words: the quote runes are
		// stripped so they can't poison matching.
		{`"foo bar" deploy`, []string{"foo", "bar", "deploy"}},
		// A bare quote token strips to empty and is dropped.
		{`"`, nil},
		{`"" deploy`, []string{"deploy"}},
	}
	for _, c := range cases {
		got := workspaceHighlightTerms(c.query)
		if len(got) != len(c.want) {
			t.Errorf("workspaceHighlightTerms(%q) = %v, want %v", c.query, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("workspaceHighlightTerms(%q) = %v, want %v", c.query, got, c.want)
				break
			}
		}
	}
}

// TestWorkspaceSearchResultsInstallHighlightTerms verifies the reducer
// pushes the query's highlightable terms into the modal when results
// land: matched words in snippets render with the search-highlight SGR,
// modifier tokens do not.
func TestWorkspaceSearchResultsInstallHighlightTerms(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	hlStart, _, ok := messages.SearchHighlightSGR()
	if !ok {
		t.Fatal("could not derive highlight SGR")
	}

	app := searchTestApp(t)
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	query := "deploy in:#general"
	for _, r := range query {
		app.searchResults.HandleKey(string(r))
	}
	app.searchResults.HandleKey("enter")
	app.Update(WorkspaceSearchResultsMsg{Query: query, Items: []searchresults.Item{
		{ChannelID: "C2", ChannelName: "ops", UserName: "sam", TS: "2.0",
			Text: "deploy to general"},
	}, Total: 1})

	out := app.searchResults.View(80, 30)
	if !strings.Contains(out, hlStart+"deploy") {
		t.Errorf("snippet term not highlighted after results landed:\n%q", out)
	}
	if strings.Contains(out, hlStart+"general") {
		t.Errorf("modifier token must not be highlighted:\n%q", out)
	}
}

func TestWorkspaceSearchErrorShownInModal(t *testing.T) {
	app := searchTestApp(t)
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	app.searchResults.HandleKey("q")
	app.searchResults.HandleKey("enter")
	app.Update(WorkspaceSearchResultsMsg{Query: "q", Err: errors.New("rate limited")})
	if !app.searchResults.IsVisible() || app.searchResults.Query() != "q" {
		t.Fatal("error must keep modal open with query intact")
	}
}

func TestWorkspaceSearchEscWhilePendingDropsLateResult(t *testing.T) {
	app := searchTestApp(t)
	app.SetSearchService(NewSearchService(SearchServiceFuncs{
		SearchWorkspace: func(query string) tea.Msg {
			return WorkspaceSearchResultsMsg{Query: query, Items: []searchresults.Item{
				{ChannelID: "C2", ChannelName: "ops", TS: "2.0", Text: "hit"},
			}, Total: 1}
		},
	}))
	app.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	app.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Esc closes the modal while the search is in flight.
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.mode != ModeNormal || app.searchResults.IsVisible() {
		t.Fatal("Esc should close the modal")
	}
	for _, m := range drainCmd(cmd) {
		app.Update(m)
	}
	if app.searchResults.IsVisible() {
		t.Fatal("late result re-opened the closed modal")
	}
	if _, ok := app.searchResults.Selected(); ok {
		t.Fatal("late result was installed after Esc")
	}
}
