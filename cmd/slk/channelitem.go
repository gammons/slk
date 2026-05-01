package main

import (
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/slackfmt"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// buildChannelItem converts a Slack conversation into the sidebar
// ChannelItem + finder Item shape used everywhere in slk. Pure function:
// reads from wctx for name/presence resolution, returns the constructed
// sidebar item plus a parallel finder entry. The caller decides whether
// to append, upsert, or persist.
//
// Extracted from the workspace-bootstrap loop in initWorkspace so that
// mid-session conversation events (mpim_open / im_created / group_joined /
// channel_joined) can produce identical items.
//
// The returned finder Item uses the same display name as the sidebar item
// (e.g. "alice" for a DM, the formatted participant list for a group DM)
// because that's what the bootstrap loop has always passed to the finder
// and what the finder's filter/render code expects.
func buildChannelItem(ch slack.Channel, wctx *WorkspaceContext, cfg config.Config, teamID string) (sidebar.ChannelItem, channelfinder.Item) {
	chType := "channel"
	if ch.IsIM {
		// Slack returns the same is_im=true for human DMs and app DMs;
		// the only differentiator is the peer user's IsBot/IsAppUser
		// flag, which we look up via the cache-seeded BotUserIDs set.
		// Unknown peers default to "dm" and are reclassified later by
		// the resolveUser path.
		if wctx.BotUserIDs[ch.User] {
			chType = "app"
		} else {
			chType = "dm"
		}
	} else if ch.IsMpIM {
		chType = "group_dm"
	} else if ch.IsPrivate {
		chType = "private"
	}

	displayName := ch.Name
	if ch.IsIM {
		if resolved, ok := wctx.UserNames[ch.User]; ok {
			displayName = resolved
		} else {
			displayName = ch.User
		}
	} else if ch.IsMpIM {
		displayName = slackfmt.FormatMPDMName(ch.Name, func(h string) string {
			return wctx.UserNamesByHandle[h]
		})
	}

	section := cfg.MatchSection(teamID, ch.Name)
	var sectionOrder int
	if section != "" {
		sectionOrder = cfg.SectionOrder(teamID, section)
	}

	item := sidebar.ChannelItem{
		ID:           ch.ID,
		Name:         displayName,
		Type:         chType,
		Section:      section,
		SectionOrder: sectionOrder,
	}
	if ch.IsIM {
		item.DMUserID = ch.User
	}

	finderItem := channelfinder.Item{
		ID:       ch.ID,
		Name:     displayName,
		Type:     chType,
		Presence: item.Presence,
		Joined:   true,
	}
	return item, finderItem
}

// upsertChannelInDB writes the channel to the SQLite cache. Separated from
// buildChannelItem so the latter stays a pure function.
func upsertChannelInDB(db *cache.DB, ch slack.Channel, chType string, teamID string) {
	db.UpsertChannel(cache.Channel{
		ID:          ch.ID,
		WorkspaceID: teamID,
		Name:        ch.Name,
		Type:        chType,
		Topic:       ch.Topic.Value,
		IsMember:    ch.IsMember,
	})
}
