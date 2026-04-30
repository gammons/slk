package config

import (
	"regexp"
	"strings"
)

// teamIDKeyRe matches the shape of a raw Slack team ID
// (workspace IDs start with T, enterprise IDs with E). Used to
// recognize legacy [workspaces.T01ABCDEF] blocks that predate
// slug-keyed entries with explicit team_id fields.
var teamIDKeyRe = regexp.MustCompile(`^[TE][A-Z0-9]{6,}$`)

// isTeamIDKey reports whether s looks like a raw Slack team or
// enterprise ID. Used by Load to decide whether a [workspaces.<key>]
// TOML key whose block has no team_id field should be treated as a
// legacy team-ID key.
func isTeamIDKey(s string) bool {
	return teamIDKeyRe.MatchString(s)
}

// Slugify produces a lowercase, hyphen-separated slug from a
// human-readable name. Non-alphanumeric runes become hyphens; runs
// of hyphens are collapsed; leading/trailing hyphens are trimmed.
// Returns an empty string if the input has no alphanumeric content.
func Slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true // suppress leading hyphens
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
