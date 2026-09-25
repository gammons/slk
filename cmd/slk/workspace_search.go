package main

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/slack-go/slack"
)

// searchWorkspaceFunc builds the SearchService.SearchWorkspace
// closure: a server-side search.messages query against the active
// workspace. Always returns a WorkspaceSearchResultsMsg — a nil msg
// would leave the ctrl+f modal spinner stuck (the reducer only exits
// the loading state on a results msg).
func searchWorkspaceFunc(router *workspaceRouter, db *cache.DB, tsFormat string) func(query string) core.Msg {
	return func(query string) core.Msg {
		wctx := router.Active()
		if wctx == nil {
			return ui.WorkspaceSearchResultsMsg{Query: query, Err: errors.New("no active workspace")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := wctx.Client.SearchMessages(ctx, query, 50)
		if err != nil {
			return ui.WorkspaceSearchResultsMsg{Query: query, Err: err}
		}
		// Read-only lookup: this closure runs in a bubbletea cmd
		// goroutine, so it must not write wctx.UserNames (shared with
		// the UI goroutine; see userResolver.Request).
		resolveUser := func(id string) (string, bool) {
			return lookupUserCached(id, wctx.UserNames, db)
		}
		resolveChannel := func(id string) (string, bool) {
			if db == nil {
				return "", false
			}
			if ch, err := db.GetChannel(id); err == nil && ch.Name != "" {
				return ch.Name, true
			}
			return "", false
		}
		items := searchResultItems(res.Matches, tsFormat, time.Now(), resolveUser, resolveChannel, wctx.UserGroups())
		return ui.WorkspaceSearchResultsMsg{Query: query, Items: items, Total: res.Total}
	}
}

// userIDShapeRe matches a string shaped like a Slack user ID. DM hits
// from search.messages carry the counterpart's raw user ID as the
// channel "name"; this is the detection heuristic for that case.
var userIDShapeRe = regexp.MustCompile(`^[UW][A-Z0-9]{5,}$`)

// searchResultItems converts search.messages matches into the modal's
// row items: snippets have mrkdwn entities flattened to plain text, DM
// channel names (raw user IDs on the wire) are resolved to the
// counterpart's display name, and thread TSes are recovered from
// permalinks. Pure: all lookups go through the supplied resolvers.
func searchResultItems(matches []slack.SearchMessage, tsFormat string, now time.Time, resolveUser, resolveChannel func(id string) (string, bool), userGroups map[string]string) []searchresults.Item {
	items := make([]searchresults.Item, 0, len(matches))
	for _, match := range matches {
		// ThreadTS comes from the hit's permalink. Known v1
		// limitation: a thread-reply hit with an unparseable
		// permalink degrades to plain-message nav, which may
		// toast "Message not found in loaded history" (replies
		// aren't in channel history).
		threadTS := ""
		if pl, ok := slackurl.Parse(match.Permalink); ok {
			threadTS = string(pl.ThreadTS)
		}

		// DM detection: an IM channel ID (D...) is authoritative; a
		// user-ID-shaped channel name counts only when it actually
		// resolves as a user (slack-go's CtxChannel has no IsIM flag).
		channelName := match.Channel.Name
		isDM := strings.HasPrefix(match.Channel.ID, "D")
		if userIDShapeRe.MatchString(channelName) {
			if name, ok := resolveUser(channelName); ok && name != "" {
				channelName = name
				isDM = true
			}
		}

		items = append(items, searchresults.Item{
			ChannelID:   match.Channel.ID,
			ChannelName: channelName,
			UserName:    match.Username,
			TS:          match.Timestamp,
			ThreadTS:    threadTS,
			Text:        messages.FlattenMrkdwnWithUserGroups(match.Text, resolveUser, resolveChannel, userGroups),
			Timestamp:   formatSearchTimestamp(match.Timestamp, tsFormat, now),
			IsDM:        isDM,
		})
	}
	return items
}

// formatSearchTimestamp formats a search-result timestamp for the
// modal's metadata line. Results span months, so non-today hits get a
// date prefix: "May 19, 8:01 PM" this year, "May 19 2025, 8:01 PM" for
// prior years. Today's hits show just the time (like Slack, and
// mirroring the message pane's "Today" separator). Unparseable
// timestamps fall back to the raw value, matching formatTimestamp.
func formatSearchTimestamp(ts, timeFormat string, now time.Time) string {
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ts
	}
	t := time.Unix(sec, 0)
	ny, nm, nd := now.Date()
	ty, tm, td := t.Date()
	switch {
	case ty == ny && tm == nm && td == nd:
		return t.Format(timeFormat)
	case ty == ny:
		return t.Format("Jan 2, ") + t.Format(timeFormat)
	default:
		return t.Format("Jan 2 2006, ") + t.Format(timeFormat)
	}
}
