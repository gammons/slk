package config

import (
	"fmt"
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

// resolveWorkspaceKeys walks ws and, for each block, fills in TeamID
// when it can be inferred from the TOML key (legacy team-ID-keyed
// blocks). Returns an error if a slug-keyed block lacks team_id, if
// two slugs map to the same team_id, or if a slug-keyed block's
// team_id field is itself not a Slack-team-ID-shaped string.
func resolveWorkspaceKeys(ws map[string]Workspace) (map[string]Workspace, error) {
	if len(ws) == 0 {
		return ws, nil
	}
	out := make(map[string]Workspace, len(ws))
	seenTeamID := make(map[string]string, len(ws)) // teamID -> first slug we saw
	for key, w := range ws {
		switch {
		case w.TeamID != "":
			// Slug-keyed block. team_id must look like a real ID.
			if !isTeamIDKey(w.TeamID) {
				return nil, fmt.Errorf(
					"workspace %q: team_id %q does not look like a Slack team ID",
					key, w.TeamID)
			}
		case isTeamIDKey(key):
			// Legacy team-ID-keyed block; synthesize TeamID from key.
			w.TeamID = key
		default:
			return nil, fmt.Errorf(
				"workspace %q is missing team_id (the TOML key is a slug, "+
					"so the block must set team_id explicitly)", key)
		}
		if first, dup := seenTeamID[w.TeamID]; dup {
			return nil, fmt.Errorf(
				"workspaces %q and %q both reference team_id %q",
				first, key, w.TeamID)
		}
		seenTeamID[w.TeamID] = key
		out[key] = w
	}
	return out, nil
}
