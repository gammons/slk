package config

import (
	"fmt"
	"reflect"
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
// A team-id-keyed block that only holds state slk writes itself
// (version_ts, theme, sidebar_width) and shares its team id with a
// slug block is folded into that slug. First setup used to write
// exactly that pair (#499). A team-id block that also carries
// something a person configures still conflicts.
func resolveWorkspaceKeys(ws map[string]Workspace) (map[string]Workspace, error) {
	if len(ws) == 0 {
		return ws, nil
	}

	type resolved struct {
		key       string
		w         Workspace
		slug      bool
		legacy    bool
		stateOnly bool
	}
	list := make([]resolved, 0, len(ws))
	for key, orig := range ws {
		w := orig
		var slug, legacy bool
		switch {
		case orig.TeamID != "":
			// team_id must look like a real ID, slug key or not.
			if !isTeamIDKey(orig.TeamID) {
				return nil, fmt.Errorf(
					"workspace %q: team_id %q does not look like a Slack team ID",
					key, orig.TeamID)
			}
			slug = !isTeamIDKey(key)
			legacy = isTeamIDKey(key) && orig.TeamID == key
		case isTeamIDKey(key):
			// Legacy team-ID-keyed block; synthesize TeamID from key.
			w.TeamID = key
			legacy = true
		default:
			return nil, fmt.Errorf(
				"workspace %q is missing team_id (the TOML key is a slug, "+
					"so the block must set team_id explicitly)", key)
		}
		list = append(list, resolved{
			key: key, w: w, slug: slug, legacy: legacy,
			stateOnly: workspaceStateOnly(key, orig),
		})
	}

	slugByTeam := make(map[string]int, len(list))
	for i, e := range list {
		if !e.slug {
			continue
		}
		if prev, dup := slugByTeam[e.w.TeamID]; dup {
			return nil, fmt.Errorf(
				"workspaces %q and %q both reference team_id %q",
				list[prev].key, e.key, e.w.TeamID)
		}
		slugByTeam[e.w.TeamID] = i
	}

	// Fold a state-only team-id block into the slug for the same team
	// before the duplicate check, so the pair first setup wrote loads.
	drop := make(map[int]bool, len(list))
	for i, e := range list {
		if !e.legacy {
			continue
		}
		slugIdx, ok := slugByTeam[e.w.TeamID]
		if !ok {
			continue
		}
		if !e.stateOnly {
			return nil, fmt.Errorf(
				"workspaces %q and %q both reference team_id %q",
				list[slugIdx].key, e.key, e.w.TeamID)
		}
		slug := list[slugIdx]
		slug.w = mergeWorkspaceState(slug.w, e.w)
		list[slugIdx] = slug
		drop[i] = true
	}

	out := make(map[string]Workspace, len(list))
	seenTeamID := make(map[string]string, len(list))
	for i, e := range list {
		if drop[i] {
			continue
		}
		if first, dup := seenTeamID[e.w.TeamID]; dup {
			return nil, fmt.Errorf(
				"workspaces %q and %q both reference team_id %q",
				first, e.key, e.w.TeamID)
		}
		seenTeamID[e.w.TeamID] = e.key
		out[e.key] = e.w
	}
	return out, nil
}

// workspaceStateOnly reports whether a team-id-keyed block carries
// only the fields slk writes for itself: version_ts, theme, and
// sidebar_width, plus a team_id that restates the key. orig is the
// block before TeamID is synthesized from the key.
//
// Those three are cleared and the rest is compared to the zero
// Workspace, so this is an allow list. A field added to Workspace
// later is configuration until someone clears it here: a set value
// refuses the fold instead of being dropped. TestWorkspaceStateOnlyAllowList
// fails when the struct gains a field, which is the prompt to classify it.
func workspaceStateOnly(key string, orig Workspace) bool {
	if !isTeamIDKey(key) {
		return false
	}
	if orig.TeamID != "" && orig.TeamID != key {
		return false
	}
	cleared := orig
	cleared.TeamID = ""
	cleared.VersionTS = ""
	cleared.Theme = ""
	cleared.SidebarWidth = 0
	return reflect.DeepEqual(cleared, Workspace{})
}

// mergeWorkspaceState copies state the slug block left empty off the
// team-id block. A value the slug already has wins: that block is the
// one the person configured.
func mergeWorkspaceState(dst, src Workspace) Workspace {
	if dst.VersionTS == "" {
		dst.VersionTS = src.VersionTS
	}
	if dst.Theme == "" {
		dst.Theme = src.Theme
	}
	if dst.SidebarWidth == 0 {
		dst.SidebarWidth = src.SidebarWidth
	}
	return dst
}
