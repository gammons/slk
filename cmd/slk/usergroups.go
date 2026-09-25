package main

import (
	"strings"

	"github.com/slack-go/slack"
)

func usergroupHandles(groups []slack.UserGroup) map[string]string {
	byID := make(map[string]string, len(groups))
	for _, g := range groups {
		handle := g.Handle
		if handle == "" {
			// Fall back to the display name, slugified: a name like
			// "Platform Team" would otherwise become a handle with a
			// space in it, which neither the @-token composer
			// autocomplete nor the word-boundary send translation can
			// round-trip.
			handle = slugifyHandle(g.Name)
		}
		if handle != "" {
			byID[g.ID] = handle
		}
	}
	return byID
}

// slugifyHandle turns a usergroup display name into a mention-safe
// handle: lowercased, with every run of characters Slack handles don't
// use collapsed to a single "-" ("Platform Team" -> "platform-team").
// Returns "" when nothing usable survives, so the caller can skip the
// group entirely rather than register an unmentionable handle.
func slugifyHandle(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	return b.String()
}
