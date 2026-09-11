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
//
// A leftover [workspaces.<teamID>] block next to a slug-keyed block
// for the same team is not a duplicate workspace — it is how
// saveWorkspaceVersionTS used to persist version_ts when it could not
// find the slug header. Those prefs are merged into the slug block
// and the leftover key is dropped so Load does not refuse to start.
func resolveWorkspaceKeys(ws map[string]Workspace) (map[string]Workspace, error) {
	if len(ws) == 0 {
		return ws, nil
	}
	prepared := make(map[string]Workspace, len(ws))
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
		prepared[key] = w
	}

	slugForTeam := make(map[string]string, len(prepared)) // teamID -> slug key
	for key, w := range prepared {
		if key == w.TeamID {
			continue
		}
		if first, dup := slugForTeam[w.TeamID]; dup {
			return nil, fmt.Errorf(
				"workspaces %q and %q both reference team_id %q",
				first, key, w.TeamID)
		}
		slugForTeam[w.TeamID] = key
	}

	out := make(map[string]Workspace, len(prepared))
	for key, w := range prepared {
		if key != w.TeamID {
			continue
		}
		if slug, ok := slugForTeam[w.TeamID]; ok {
			prepared[slug] = mergeWorkspacePrefs(prepared[slug], w)
			continue
		}
		out[key] = w
	}
	for _, slug := range slugForTeam {
		out[slug] = prepared[slug]
	}
	return out, nil
}

// mergeWorkspacePrefs copies unset preference fields from src into dst.
// Used when a leftover team-ID-keyed block sits beside a slug-keyed
// block for the same team: the slug keeps identity, the leftover
// donates version_ts / theme / width / etc. that were saved under
// the team-ID key. Non-empty dst fields win.
func mergeWorkspacePrefs(dst, src Workspace) Workspace {
	if dst.VersionTS == "" {
		dst.VersionTS = src.VersionTS
	}
	if dst.Theme == "" {
		dst.Theme = src.Theme
	}
	if dst.SidebarWidth == 0 {
		dst.SidebarWidth = src.SidebarWidth
	}
	if dst.Order == 0 {
		dst.Order = src.Order
	}
	if dst.UseSlackSections == nil {
		dst.UseSlackSections = src.UseSlackSections
	}
	if len(dst.Sections) == 0 {
		dst.Sections = src.Sections
	}
	return dst
}
