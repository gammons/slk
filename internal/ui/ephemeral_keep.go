package ui

import (
	"sort"

	"github.com/gammons/slk/internal/ui/messages"
)

// keepEphemerals returns fetched with the ephemeral messages from shown
// merged in by ts. A fetch from Slack never contains ephemerals, so
// replacing a pane's list with it would erase them while the user is
// looking at it. Callers must pass only what is shown for the same
// conversation the fetch is for. A shown ephemeral whose ts is already
// in fetched is dropped rather than duplicated.
func keepEphemerals(fetched, shown []messages.MessageItem) []messages.MessageItem {
	var keep []messages.MessageItem
	for _, m := range shown {
		if m.Ephemeral {
			keep = append(keep, m)
		}
	}
	if len(keep) == 0 {
		return fetched
	}
	have := make(map[string]bool, len(fetched))
	for _, m := range fetched {
		have[m.TS] = true
	}
	out := make([]messages.MessageItem, 0, len(fetched)+len(keep))
	out = append(out, fetched...)
	for _, m := range keep {
		if !have[m.TS] {
			out = append(out, m)
		}
	}
	// Slack timestamps are decimal strings of equal shape, so string
	// order is time order. Stable keeps fetched order for equal ts.
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out
}
