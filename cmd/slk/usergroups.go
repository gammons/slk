package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/slack/boot"
)

// gridMemberTeams returns the other teams that own the user's
// conversations, from userBoot's context_team_id: on Enterprise Grid
// the member workspaces of the org (gammons/slk#214), elsewhere none,
// since every conversation belongs to the workspace itself.
func gridMemberTeams(channels []boot.Channel, ims []boot.IM, workspaceID string) []string {
	var teams []string
	add := func(id string) {
		if id != "" && id != workspaceID && !slices.Contains(teams, id) {
			teams = append(teams, id)
		}
	}
	for _, ch := range channels {
		add(ch.ContextTeamID)
	}
	for _, im := range ims {
		add(im.ContextTeamID)
	}
	slices.Sort(teams)
	return teams
}

const userGroupsFetchTimeout = 30 * time.Second

// fetchUserGroups lists the workspace's usergroups, then each Grid
// member team's, since an org-level session only sees a member
// workspace's groups when it names the team. A failed call is logged
// and skipped so one team can't hide the others' groups; the error is
// returned only when every call failed.
func fetchUserGroups(ctx context.Context, list func(context.Context, string) ([]slack.UserGroup, error), memberTeams []string, logf func(string, ...any)) ([]slack.UserGroup, error) {
	var all []slack.UserGroup
	var lastErr error
	failed := 0
	for _, team := range append([]string{""}, memberTeams...) {
		// One deadline per team: the HTTP client has none, and a
		// stalled team would otherwise hold back the groups already
		// fetched for the whole session.
		callCtx, cancel := context.WithTimeout(ctx, userGroupsFetchTimeout)
		groups, err := list(callCtx, team)
		cancel()
		if err != nil {
			logf("usergroups fetch (team %q) failed: %v", team, err)
			lastErr = err
			failed++
			continue
		}
		all = append(all, groups...)
	}
	if failed == len(memberTeams)+1 {
		return nil, lastErr
	}
	return all, nil
}

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

// selfUserGroups returns the IDs of the groups userID is a member of.
func selfUserGroups(groups []slack.UserGroup, userID string) map[string]bool {
	self := map[string]bool{}
	for _, g := range groups {
		if slices.Contains(g.Users, userID) {
			self[g.ID] = true
		}
	}
	return self
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
