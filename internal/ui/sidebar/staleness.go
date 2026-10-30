package sidebar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gammons/slk/internal/core"
)

// IsStale reports whether a channel should be auto-hidden from the
// sidebar because it has been inactive longer than threshold.
//
// hasUnread and lastReadTS are read state for the item, supplied by
// the caller (the sidebar Model passes them through from its
// readStateReader callback). Keeping these as parameters rather than
// fields on ChannelItem lets the staleness predicate remain pure and
// trivially testable without touching the DB.
//
// "Inactive" means: now - parseSlackTS(lastReadTS) > threshold.
//
// A channel is NEVER stale (returns false) when:
//   - threshold <= 0 (feature disabled)
//   - hasUnread (active conversation)
//   - item.Section != "" (matches a user-curated [sections.*] glob)
//   - lastReadTS is malformed or in the future (defensive defaults)
//
// Empty lastReadTS is type-aware:
//   - For "dm" and "group_dm", empty IS stale. Slack's client.counts
//     endpoint only returns these types when the conversation is
//     currently "open"; absence in counts (which surfaces here as
//     empty lastReadTS) is Slack's canonical "this conversation is
//     closed" signal. Roughly half of the user's DMs and 98% of
//     mpdms in real workspaces fall into this bucket.
//   - For "channel" and "private", empty is unexpected (the API
//     always provides last_read for joined channels) and is most
//     likely a transient brand-new-join; show rather than hide.
//
// Selection-based exceptions ("don't hide the currently active
// channel") are NOT handled here. The sidebar Model layer applies
// that on top of this predicate so this function stays pure and
// trivially testable.
func IsStale(item ChannelItem, hasUnread bool, lastReadTS string, threshold time.Duration, now time.Time) bool {
	if threshold <= 0 {
		return false
	}
	if hasUnread {
		return false
	}
	if item.Section != "" {
		return false
	}
	if lastReadTS == "" {
		// dm/group_dm without a last_read = closed conversation.
		// Other types: defensive, prefer to show.
		return item.Type == "dm" || item.Type == "group_dm"
	}
	lastRead, ok := parseSlackTS(lastReadTS)
	if !ok {
		return false
	}
	if lastRead.After(now) {
		return false
	}
	return now.Sub(lastRead) > threshold
}

// activeOnlyWindow is the inactivity span of Slack's "Active only"
// sidebar filter.
const activeOnlyWindow = 30 * 24 * time.Hour

// isInactive applies Slack's "Active only" rule to any conversation,
// sectioned or not. Slack goes by the newest message; with no unread,
// that is at or before the last read, the closest signal slk keeps, so
// this hides a subset of what Slack hides, never more.
func isInactive(item ChannelItem, state core.ReadState, now time.Time) bool {
	item.Section = ""
	return IsStale(item, state.HasUnread, state.LastReadTS, activeOnlyWindow, now)
}

// slackSortKey is name without punctuation, so "eng_announcements" sorts
// before "eng-gazette" as in Slack.
func slackSortKey(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, name)
}

// parseSlackTS converts a Slack timestamp string ("1700000001.000000")
// to a time.Time. Returns ok=false on empty or malformed input.
//
// Slack uses the sentinel "0000000000.000000" (or bare "0") for the
// last_read field of channels/mpims the user has never opened. We
// preserve that as time.Unix(0,0) (the Unix epoch) so callers can
// treat "never read" as maximally stale rather than as missing data.
// Negative seconds remain invalid.
func parseSlackTS(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || sec < 0 {
		return time.Time{}, false
	}
	var nsec int64
	if len(parts) == 2 && parts[1] != "" {
		us, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || us < 0 {
			return time.Time{}, false
		}
		nsec = us * 1000
	}
	return time.Unix(sec, nsec), true
}

// formatTSPair is the inverse of parseSlackTS, used by tests to build
// fixture timestamps. Kept here (rather than in *_test.go) so it can
// be referenced from the test helper.
func formatTSPair(sec int64, usec int) string {
	return fmt.Sprintf("%d.%06d", sec, usec)
}
