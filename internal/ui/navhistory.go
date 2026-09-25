// internal/ui/navhistory.go
//
// Per-workspace browser-style back/forward history of channel IDs.
//
// Phase 2 of the SOLID refactor of internal/ui/app.go: extracts the
// navHistory map + the pushNavHistory / navigateBack / navigateForward
// / walkNav / dropStaleStackEntries quartet out of App into a
// self-contained information holder.
//
// App keeps thin wrappers (navigateBack / navigateForward) that turn
// this store's pure data-structure result into a tea.Cmd carrying a
// ChannelSelectedMsg{FromHistory:true}. Everything else — the stale-
// entry skip-and-drop FSM, the cap-at-50 eviction, the forward-path
// truncation on new visits — lives here.
//
// Tests that previously did `app.navHistory["T1"]` now use
// `app.navHistory.Stack("T1")`. The returned *navStack pointer is the
// same one the store holds, so mutating cursor/entries through it
// continues to work for white-box tests.
package ui

import (
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
)

// navStack is a per-workspace browser-style back/forward history of
// channel IDs. cursor points at the current entry; len(entries)==0
// is the empty state with cursor==-1.
type navStack struct {
	entries []string
	cursor  int
}

// navStackMax caps the per-workspace history at 50 entries. When a
// push would exceed the cap, the oldest entry is dropped and the
// cursor is shifted accordingly.
const navStackMax = 50

// navHistoryStore holds per-workspace navigation stacks. Lazy-creates
// a stack on first Push for each teamID. Cleared only when slk exits;
// the stacks are session-only by design.
type navHistoryStore struct {
	stacks map[string]*navStack
}

func newNavHistoryStore() *navHistoryStore {
	return &navHistoryStore{stacks: make(map[string]*navStack)}
}

// Stack returns the raw *navStack for teamID, or nil if no entry exists.
// Exposed so white-box tests can inspect/mutate state directly; the
// production code paths go through Push / Walk.
func (s *navHistoryStore) Stack(teamID string) *navStack {
	return s.stacks[teamID]
}

// Push appends channelID onto the team's navigation stack.
// Behavior:
//   - Lazy-creates the stack on first push.
//   - Dedupes consecutive: a no-op if entries[cursor] == channelID.
//   - Truncates the forward path: cursor < len-1 entries beyond cursor
//     are dropped (browser-style "new visit kills forward history").
//   - Caps at navStackMax: drops oldest entries and shifts cursor.
func (s *navHistoryStore) Push(teamID, channelID string) {
	if teamID == "" || channelID == "" {
		return
	}
	stack, ok := s.stacks[teamID]
	if !ok {
		stack = &navStack{cursor: -1}
		s.stacks[teamID] = stack
	}
	if stack.cursor >= 0 && stack.cursor < len(stack.entries) && stack.entries[stack.cursor] == channelID {
		return
	}
	if stack.cursor < len(stack.entries)-1 {
		stack.entries = stack.entries[:stack.cursor+1]
	}
	stack.entries = append(stack.entries, channelID)
	stack.cursor = len(stack.entries) - 1
	if len(stack.entries) > navStackMax {
		drop := len(stack.entries) - navStackMax
		stack.entries = stack.entries[drop:]
		stack.cursor -= drop
	}
}

// Walk advances the cursor on teamID's stack by step (±1), skipping
// any channel IDs for which lookup returns ok=false and dropping them
// from the stack. Returns the resolved channel metadata on success.
// Returns ok=false at the stack boundary, when the team has no stack,
// or when the cursor is at -1 (empty state).
//
// If lookup is nil, all entries are treated as valid (used in tests
// that don't wire a ChannelLookupFunc).
//
// On return, the stack's cursor points at the surviving target entry
// (or stays put if no valid target was found). Stale entries
// discovered during the walk are removed regardless of outcome.
func (s *navHistoryStore) Walk(teamID string, step int, lookup core.ChannelLookupFunc) (id, name, channelType string, ok bool) {
	stack, exists := s.stacks[teamID]
	if !exists || stack.cursor < 0 {
		return "", "", "", false
	}

	// Walk in `step` direction looking for the first valid entry.
	// As we go, accumulate stale indices to drop afterwards.
	var stale []int
	idx := stack.cursor + step
	var (
		foundID    string
		foundName  string
		foundType  string
		foundIndex = -1
	)
	for idx >= 0 && idx < len(stack.entries) {
		entry := stack.entries[idx]
		if lookup != nil {
			n, ctype, valid := lookup(ids.ChannelID(entry))
			if valid {
				foundID, foundName, foundType, foundIndex = entry, n, ctype, idx
				break
			}
			stale = append(stale, idx)
		} else {
			// No lookup wired (tests/early init): treat all as valid.
			foundID, foundName, foundType, foundIndex = entry, entry, "channel", idx
			break
		}
		idx += step
	}

	if foundIndex < 0 {
		// No valid target. Still drop the stale entries we discovered
		// so the stack doesn't keep walking past them next time, and
		// shift the cursor back to compensate for any drops below it.
		droppedBeforeCursor := 0
		for _, idx := range stale {
			if idx < stack.cursor {
				droppedBeforeCursor++
			}
		}
		dropStaleEntries(stack, stale)
		stack.cursor -= droppedBeforeCursor
		return "", "", "", false
	}

	// Compute foundIndex's new position after stale drops:
	// every dropped index < foundIndex shifts foundIndex down by 1.
	newFoundIndex := foundIndex
	for _, idx := range stale {
		if idx < foundIndex {
			newFoundIndex--
		}
	}
	dropStaleEntries(stack, stale)
	stack.cursor = newFoundIndex

	return foundID, foundName, foundType, true
}

// dropStaleEntries rewrites stack.entries to omit the indices in
// stale. Order of indices in stale doesn't matter.
func dropStaleEntries(stack *navStack, stale []int) {
	if len(stale) == 0 {
		return
	}
	drop := make(map[int]struct{}, len(stale))
	for _, idx := range stale {
		drop[idx] = struct{}{}
	}
	out := make([]string, 0, len(stack.entries)-len(stale))
	for i, e := range stack.entries {
		if _, skip := drop[i]; !skip {
			out = append(out, e)
		}
	}
	stack.entries = out
}
