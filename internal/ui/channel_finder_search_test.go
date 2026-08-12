package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/channelfinder"
)

// newFinderApp opens the channel finder over a local cache of joined
// channels, with a recording remote search wired in.
func newFinderApp(t *testing.T, searched *[]string, results []channelfinder.Item) *App {
	t.Helper()
	a := NewApp()
	a.activeTeamID = "T1"
	a.SetChannelService(NewChannelService(ChannelServiceFuncs{
		SearchRemote: func(query string) []channelfinder.Item {
			*searched = append(*searched, query)
			return results
		},
	}))
	a.SetChannelFinderItems([]channelfinder.Item{
		{ID: "C1", Name: "testing-local", Type: "channel", Joined: true},
		{ID: "C2", Name: "unrelated", Type: "channel", Joined: true},
	})
	a.channelFinder.Open()
	a.SetMode(ModeChannelFinder)
	return a
}

// typeIntoFinder feeds one printable key per rune and returns the
// debounce payload each keystroke scheduled, as the tick would emit it.
// Synthesised rather than waited on: the delay is 300 ms and the point
// of the test is which ticks survive, not how long they take.
func typeIntoFinder(a *App, s string) []channelSearchDebounceMsg {
	var emitted []channelSearchDebounceMsg
	for _, r := range s {
		a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		emitted = append(emitted, channelSearchDebounceMsg{
			query: a.channelFinder.Query(),
			gen:   a.pendingChannelSearchGen,
		})
	}
	return emitted
}

// fireDebounce delivers one debounce tick and runs whatever Cmd it
// produced, which is where the search actually happens.
func fireDebounce(a *App, m channelSearchDebounceMsg) {
	_, cmd := a.Update(m)
	if cmd == nil {
		return
	}
	for _, out := range drainBatch(cmd) {
		_ = out
	}
}

func TestChannelFinder_TypingBurstIssuesOneSearch(t *testing.T) {
	// The capture shows two channels/search requests for a four-second
	// typing session — roughly one per pause, never one per keystroke.
	// A finder that fired per keystroke would emit a request burst no
	// human hand produces, which is a worse fingerprint than the
	// enumeration it replaces.
	var searched []string
	a := newFinderApp(t, &searched, nil)

	emitted := typeIntoFinder(a, "test")
	if len(emitted) != 4 {
		t.Fatalf("typed 4 keys, scheduled %d debounce ticks", len(emitted))
	}
	for _, m := range emitted {
		if _, cmd := a.Update(m); cmd != nil {
			for _, out := range drainBatch(cmd) {
				_ = out
			}
		}
	}

	if len(searched) != 1 {
		t.Fatalf("four keystrokes issued %d searches (%v); want 1", len(searched), searched)
	}
	if searched[0] != "test" {
		t.Errorf("searched %q; want the final query \"test\" — a debounce that fires the first query is just a slower per-keystroke search", searched[0])
	}
}

func TestChannelFinder_PauseBetweenBurstsIssuesASecondSearch(t *testing.T) {
	// Debouncing must not mean "one search per session". Each pause is
	// a new query the user is waiting on.
	var searched []string
	a := newFinderApp(t, &searched, nil)

	first := typeIntoFinder(a, "te")
	fireDebounce(a, first[len(first)-1])
	second := typeIntoFinder(a, "st")
	fireDebounce(a, second[len(second)-1])

	if len(searched) != 2 {
		t.Fatalf("two bursts issued %d searches (%v); want 2", len(searched), searched)
	}
	if searched[0] != "te" || searched[1] != "test" {
		t.Errorf("searched %v; want [te test]", searched)
	}
}

func TestChannelFinder_EmptyQueryIssuesNoSearch(t *testing.T) {
	// edge.ChannelsSearch returns early on an empty query, but the
	// caller must not queue one either: backspacing to empty is how a
	// finder session normally ends, and it is exactly when a pending
	// tick for the last non-empty query would fire.
	var searched []string
	a := newFinderApp(t, &searched, nil)

	emitted := typeIntoFinder(a, "t")
	a.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	stale := emitted[0]

	if _, cmd := a.Update(stale); cmd != nil {
		for _, out := range drainBatch(cmd) {
			_ = out
		}
	}
	if _, cmd := a.Update(channelSearchDebounceMsg{query: "", gen: a.pendingChannelSearchGen}); cmd != nil {
		for _, out := range drainBatch(cmd) {
			_ = out
		}
	}

	if len(searched) != 0 {
		t.Errorf("empty query issued %d searches (%v); want none", len(searched), searched)
	}
}

func TestChannelFinder_LocalMatchesShowBeforeAnySearchCompletes(t *testing.T) {
	// Typing must stay responsive. The cache answers on the keystroke;
	// the server's answer merges when it arrives.
	var searched []string
	a := newFinderApp(t, &searched, nil)

	typeIntoFinder(a, "test")

	items := a.channelFinder.FilteredItems()
	if len(items) == 0 {
		t.Fatal("no local matches rendered while the search was still pending; the finder would look frozen for the length of the debounce plus a round trip")
	}
	if items[0].ID != "C1" {
		t.Errorf("first local match = %+v; want the cached testing-local channel", items[0])
	}
}

func TestChannelFinder_StaleSearchResultsAreDropped(t *testing.T) {
	// A slow response for "te" must not replace the results for
	// "test" that the user is already looking at.
	var searched []string
	a := newFinderApp(t, &searched, nil)
	typeIntoFinder(a, "test")

	a.Update(RemoteChannelsFoundMsg{
		TeamID: "T1",
		Query:  "te",
		Gen:    1,
		Items:  []channelfinder.Item{{ID: "CSTALE", Name: "stale-result", Type: "channel"}},
	})
	for _, it := range a.channelFinder.Items() {
		if it.ID == "CSTALE" {
			t.Fatal("a superseded search result reached the finder; the list would flicker back to an older query's answer")
		}
	}

	a.Update(RemoteChannelsFoundMsg{
		TeamID: "T1",
		Query:  "test",
		Gen:    a.pendingChannelSearchGen,
		Items:  []channelfinder.Item{{ID: "CFRESH", Name: "testing-remote", Type: "channel"}},
	})
	var sawFresh bool
	for _, it := range a.channelFinder.Items() {
		if it.ID == "CFRESH" {
			sawFresh = true
		}
	}
	if !sawFresh {
		t.Error("the current query's results never reached the finder")
	}
}

func TestChannelFinder_ResultsForAnotherWorkspaceAreDropped(t *testing.T) {
	var searched []string
	a := newFinderApp(t, &searched, nil)
	typeIntoFinder(a, "test")

	a.Update(RemoteChannelsFoundMsg{
		TeamID: "T_OTHER",
		Query:  "test",
		Gen:    a.pendingChannelSearchGen,
		Items:  []channelfinder.Item{{ID: "COTHER", Name: "other-workspace", Type: "channel"}},
	})
	for _, it := range a.channelFinder.Items() {
		if it.ID == "COTHER" {
			t.Fatal("another workspace's search results reached this finder")
		}
	}
}
