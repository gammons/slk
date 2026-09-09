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
// Usergroup mentions (<!subteam^Sxxxx>) are deliberately NOT detected.
// Resolving one requires knowing the user's own usergroup memberships, which
// slk does not have: boot.Subteams.Self (internal/slack/boot/boot.go:203) is
// untyped because no capture with a non-empty list has ever been observed.
// The consequence is a possible undercount, never an overcount, and it is
// corrected by the next client.counts refresh. See
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
