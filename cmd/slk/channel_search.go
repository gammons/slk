package main

import (
	"context"
	"sort"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui/channelfinder"
)

// channelSearcher is edgeapi's channels/search, the endpoint that
// replaced walking conversations.list.
//
// An interface rather than *edge.Client so the mapping below — which
// is where a wrong finder type or a resurrected archived channel would
// come from — is testable without a server.
type channelSearcher interface {
	ChannelsSearch(ctx context.Context, query string, topChannels []string) ([]edge.Channel, []string, error)
}

// maxTopChannels caps the frecency hint sent with a search.
//
// The captures show 22 ids on channels/search and the endpoint does
// not cap it, so this is slk's own bound: the hint is a ranking
// nicety, and a request whose body grows with the user's history is
// exactly the kind of shape that made the old enumeration
// recognisable.
const maxTopChannels = 25

// searchChannelsRemote asks the server which channels match query and
// converts the answer into finder rows.
//
// This is what the finder shows for channels the user has not joined.
// It used to come from fetchBrowseableChannels: a full
// conversations.list walk at Limit: 1000, run in the background of
// every boot on every workspace, whether or not the finder was ever
// opened — 4 requests on a measured two-workspace start, growing with
// the workspace. Now nothing is fetched until somebody types.
//
// Returns nil on any failure, which leaves the finder showing its
// local matches: the same thing it showed before the server answered,
// and before this function existed at all.
func searchChannelsRemote(ctx context.Context, s channelSearcher, lastVisited map[string]int64, query string) []channelfinder.Item {
	if s == nil || query == "" {
		return nil
	}
	channels, _, err := s.ChannelsSearch(ctx, query, topVisitedChannels(lastVisited, maxTopChannels))
	if err != nil {
		debuglog.General("channel finder: channels/search for %q: %v (showing local matches only)", query, err)
		return nil
	}

	// The response's member_channels array is deliberately ignored:
	// channelfinder.SetBrowseable forces Joined=false on everything it
	// is given and skips ids already present as joined rows, so the
	// finder resolves membership from the sidebar list it already
	// holds. Trusting member_channels here would mean two sources
	// disagreeing about the same flag.
	items := make([]channelfinder.Item, 0, len(channels))
	for _, ch := range channels {
		if ch.IsArchived {
			continue
		}
		items = append(items, channelfinder.Item{
			ID:          ch.ID,
			Name:        ch.Name,
			Type:        finderChannelType(ch),
			LastVisited: lastVisited[ch.ID],
		})
	}
	return items
}

// finderChannelType maps an edge result onto the finder's four type
// strings. edge.Channel carries flags, not a type: see its doc
// comment.
func finderChannelType(ch edge.Channel) string {
	switch {
	case ch.IsIM:
		return "dm"
	case ch.IsMPIM:
		return "group_dm"
	case ch.IsPrivate:
		return "private"
	default:
		return "channel"
	}
}

// topVisitedChannels returns up to limit channel ids, most recently
// visited first — slk's only frecency signal, and the closest thing it
// has to the top_channels list the official client sends.
func topVisitedChannels(lastVisited map[string]int64, limit int) []string {
	if len(lastVisited) == 0 {
		return nil
	}
	ids := make([]string, 0, len(lastVisited))
	for id := range lastVisited {
		ids = append(ids, id)
	}
	// Ties broken by id so the hint is stable between two searches
	// with the same history; an unstable order would make otherwise
	// identical requests look different for no reason.
	sort.Slice(ids, func(i, j int) bool {
		if lastVisited[ids[i]] != lastVisited[ids[j]] {
			return lastVisited[ids[i]] > lastVisited[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}
