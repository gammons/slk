// Package mention detects whether Slack message text mentions the
// authenticated user.
//
// It exists so the desktop-notification policy (internal/notify) and the
// sidebar mention badge (channels.mention_count, written from cmd/slk's
// WebSocket handler) share one definition of "does this mention me?"
// rather than each carrying its own copy.
package mention

import "strings"

// InText reports whether text contains a direct mention of selfUserID, or a
// channel-wide broadcast that includes them.
//
// Recognized forms are Slack's wire encodings: <@Uxxxx> for a direct mention
// and <!here>, <!channel>, <!everyone> for broadcasts. The angle brackets are
// required, so a bare user ID in prose is not a mention, and <@U123ABC> does
// not match a selfUserID of "U123".
//
// Pipe-labeled variants (<@Uxxxx|name>, <!here|@here>) are NOT matched.
// internal/ui/messages/flatten.go:11-14 records where those forms are actually
// seen: message events normally carry the bare <@UID>, and it is
// search.messages snippets that can carry the labeled one. InText's callers
// are the notify path and the WebSocket message path, neither of which reads
// search results, so the gap is expected to be cold in practice — it is not
// known to be reachable from either caller. The gap is inherited unchanged
// from internal/notify/notifier.go, where this predicate originated; it is
// recorded here rather than fixed so the extraction stays behavior-preserving.
//
// Usergroup mentions (<!subteam^Sxxxx>) are deliberately NOT detected.
// Resolving one requires knowing the user's own usergroup memberships, which
// slk does not have: boot.Subteams.Self (internal/slack/boot/boot.go) is
// untyped because no capture with a non-empty list has ever been observed.
//
// Both gaps can only undercount, never overcount. The two callers recover
// differently, and only one of them recovers at all: the sidebar badge is
// overwritten by the next client.counts refresh or *_marked event, so a missed
// increment is transient. internal/notify has no such correction — a desktop
// notification that was never raised cannot be raised retroactively, so a
// missed mention there is simply lost. See
// docs/superpowers/specs/2026-09-09-mention-badges-design.md.
//
// An empty selfUserID matches no direct mention; broadcasts still match.
func InText(text, selfUserID string) bool {
	if selfUserID != "" && strings.Contains(text, "<@"+selfUserID+">") {
		return true
	}
	return strings.Contains(text, "<!here>") ||
		strings.Contains(text, "<!channel>") ||
		strings.Contains(text, "<!everyone>")
}
