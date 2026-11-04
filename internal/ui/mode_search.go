// internal/ui/mode_search.go
//
// Search-mode key handler: the in-channel `/` prompt.
//
// The prompt is an input buffer rendered in the status line's search
// segment. Enter executes the FTS query for the active channel (via
// the SearchService) and returns to Normal mode; the results land as
// a ChannelSearchResultsMsg (see reducer_search.go). Esc cancels.
package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

func handleSearchMode(a *App, msg tea.KeyMsg) tea.Cmd {
	// Match on the key code, as the other prompt handlers do, so a held
	// modifier (shift+enter, shift+esc) still submits or cancels (#186).
	switch msg.Key().Code {
	case tea.KeyEscape:
		a.searchInput = ""
		// If a search is still active (the user re-entered `/` on
		// top of existing results and bailed), restore its i/N
		// indicator instead of blanking the segment.
		if s := a.search; s != nil {
			a.statusbar.SetSearch(fmt.Sprintf("/%s  %d/%d", s.query, s.idx+1, len(s.matches)))
		} else {
			a.statusbar.SetSearch("")
		}
		a.SetMode(ModeNormal)
		return nil
	case tea.KeyEnter:
		query := strings.TrimSpace(a.searchInput)
		a.searchInput = ""
		a.SetMode(ModeNormal)
		if query == "" {
			a.clearActiveSearch()
			a.statusbar.SetSearch("")
			return nil
		}
		a.statusbar.SetSearch("/" + query + "  …")
		// New dispatch supersedes any in-flight query; stamp the
		// result with this dispatch's generation so the reducer can
		// drop late results from older queries (or ones the user
		// cleared while pending). The Gen is attached here, UI-side,
		// so the SearchService stays unaware of UI bookkeeping.
		a.searchGen++
		gen := a.searchGen
		search := a.searchSvc
		chID := a.activeChannelID
		return func() tea.Msg {
			msg := search.SearchChannel(ids.ChannelID(chID), query)
			if r, ok := msg.(ChannelSearchResultsMsg); ok {
				r.Gen = gen
				return r
			}
			return msg
		}
	}
	// Text editing: append printable runes, backspace deletes.
	switch s := normalizeFinderKey(msg); s {
	case "backspace":
		if a.searchInput != "" {
			r := []rune(a.searchInput)
			a.searchInput = string(r[:len(r)-1])
		}
	case "space":
		// Key.String() renders a literal space as "space"; queries
		// can be multi-term, so map it back.
		a.searchInput += " "
	default:
		// Single-rune filter: accepts ordinary printable input but
		// drops multi-rune graphemes (emoji ZWJ sequences, some IME
		// commits arrive as multi-rune strings). Known v1 limitation.
		if len([]rune(s)) == 1 {
			a.searchInput += s
		}
	}
	a.statusbar.SetSearch("/" + a.searchInput)
	return nil
}
