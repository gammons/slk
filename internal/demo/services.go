package demo

import (
	"context"
	"strings"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui"
)

// services are the demo's implementations of the internal/core ports.
// Unset funcs fall back to the adapters' documented no-ops.
type services struct {
	channels  core.ChannelService
	threads   core.ThreadService
	messages  core.MessageService
	reactions core.ReactionService
	search    core.SearchService
	unread    core.UnreadService
	workspace core.WorkspaceService
	avatars   core.AvatarService
	settings  core.SettingsService
	presence  core.PresenceService
	files     core.FileService
	profiles  core.ProfileService
}

// toMrkdwn converts what the user typed, as cmd/slk's real send does, so
// the authoritative message matches the App's optimistic placeholder.
func toMrkdwn(s string) string {
	out, _ := mrkdwn.Convert(s)
	return out
}

var frecentNames = []string{"thumbsup", "rocket", "tada", "eyes", "white_check_mark", "heart", "fire", "pray"}

func frecentEmoji(limit int) []core.EmojiEntry {
	codes := emoji.CodeMap()
	var out []core.EmojiEntry
	for _, n := range frecentNames {
		if len(out) == limit {
			break
		}
		out = append(out, core.EmojiEntry{Name: n, Unicode: codes[":"+n+":"]})
	}
	return out
}

func (d *Demo) services() services {
	w, dir := d.world, d.director
	return services{
		channels: core.NewChannelService(core.ChannelServiceFuncs{
			Fetch: func(channelID ids.ChannelID, _ string) core.Msg {
				ch := string(channelID)
				msgs, lastRead := w.messages(ch)
				marked := ""
				if n := len(msgs); n > 0 {
					marked = msgs[n-1].TS
					w.markRead(ch, marked)
				}
				dir.observe(Event{Kind: EventChannelOpened, TeamID: w.teamOf(ch), ChannelID: ch})
				return ui.MessagesLoadedMsg{ChannelID: ch, Messages: msgs, LastReadTS: lastRead, MarkedTS: marked}
			},
			FetchOlder: func(channelID ids.ChannelID, oldestTS ids.MessageTS) core.Msg {
				return ui.OlderMessagesLoadedMsg{ChannelID: string(channelID), AnchorTS: string(oldestTS)}
			},
			FetchAround: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
				msgs, _ := w.messages(string(channelID))
				return ui.MessagesAroundLoadedMsg{ChannelID: string(channelID), TargetTS: string(ts), Messages: msgs}
			},
			MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
				ch := string(channelID)
				w.markRead(ch, string(ts))
				dir.observe(Event{Kind: EventMarkedRead, TeamID: w.teamOf(ch), ChannelID: ch})
				return ui.ChannelMarkedReadMsg{ChannelID: ch}
			},
			Lookup: func(channelID ids.ChannelID) (string, string, bool) {
				return w.lookup(string(channelID))
			},
		}),
		threads: core.NewThreadService(core.ThreadServiceFuncs{
			Fetch: func(channelID ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
				ch, ts := string(channelID), string(threadTS)
				dir.observe(Event{Kind: EventThreadOpened, TeamID: w.teamOf(ch), ChannelID: ch, ThreadTS: ts})
				return ui.ThreadRepliesLoadedMsg{ThreadTS: ts, Replies: w.replies(ch, ts)}
			},
			Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) core.Cmd {
				return func() core.Msg {
					return ui.ThreadMarkedLocalMsg{ChannelID: string(channelID), ThreadTS: string(threadTS), TS: string(ts)}
				}
			},
			SendReply: func(channelID ids.ChannelID, threadTS ids.ThreadTS, txt string, broadcast bool) core.Msg {
				ch, tts := string(channelID), string(threadTS)
				m, ok := w.post(ch, tts, w.selfIn(ch), toMrkdwn(txt), broadcast)
				if !ok {
					return ui.ThreadReplySendFailedMsg{ChannelID: ch, ThreadTS: tts, Reason: errUnavailable.Error(), Broadcast: broadcast}
				}
				dir.observe(Event{Kind: EventMessageSent, TeamID: w.teamOf(ch), ChannelID: ch, ThreadTS: tts})
				return ui.ThreadReplySentMsg{ChannelID: ch, ThreadTS: tts, Message: m, Broadcast: broadcast}
			},
			ListFetch: func(teamID ids.TeamID) core.Msg {
				return ui.ThreadsListLoadedMsg{TeamID: string(teamID), Summaries: w.threadSummaries(string(teamID)), SubscriptionsAvailable: true}
			},
		}),
		messages: core.NewMessageService(core.MessageServiceFuncs{
			Send: func(channelID ids.ChannelID, txt string) core.Msg {
				ch := string(channelID)
				m, ok := w.post(ch, "", w.selfIn(ch), toMrkdwn(txt), false)
				if !ok {
					return ui.MessageSendFailedMsg{ChannelID: ch, Reason: errUnavailable.Error()}
				}
				dir.observe(Event{Kind: EventMessageSent, TeamID: w.teamOf(ch), ChannelID: ch})
				return ui.MessageSentMsg{ChannelID: ch, Message: m}
			},
			Forward: func(context.Context, string, ids.ChannelID, ids.MessageTS, ids.ChannelID) (core.ForwardResult, error) {
				return core.ForwardResult{}, errUnavailable
			},
			Edit: func(channelID ids.ChannelID, ts ids.MessageTS, _ string) core.Msg {
				return ui.MessageEditedMsg{ChannelID: string(channelID), TS: string(ts), Err: errUnavailable}
			},
			Delete: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
				return ui.MessageDeletedMsg{ChannelID: string(channelID), TS: string(ts), Err: errUnavailable}
			},
			MarkUnread: func(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unread int) core.Msg {
				if threadTS == "" {
					w.setLastRead(string(channelID), string(boundaryTS))
				}
				return ui.MessageMarkedUnreadMsg{ChannelID: string(channelID), ThreadTS: string(threadTS), BoundaryTS: string(boundaryTS), UnreadCount: unread}
			},
			Permalink: func(context.Context, ids.ChannelID, ids.MessageTS) (string, error) {
				return "", errUnavailable
			},
		}),
		reactions: core.NewReactionService(
			func(channelID ids.ChannelID, ts ids.MessageTS, name string) error {
				ch := string(channelID)
				if !w.react(ch, string(ts), w.selfIn(ch), name, false) {
					return errUnavailable
				}
				dir.observe(Event{Kind: EventReactionAdded, TeamID: w.teamOf(ch), ChannelID: ch})
				return nil
			},
			func(channelID ids.ChannelID, ts ids.MessageTS, name string) error {
				ch := string(channelID)
				if !w.react(ch, string(ts), w.selfIn(ch), name, true) {
					return errUnavailable
				}
				return nil
			},
			frecentEmoji,
			nil,
		),
		search: core.NewSearchService(core.SearchServiceFuncs{
			SearchChannel: func(channelID ids.ChannelID, query string) core.Msg {
				return ui.ChannelSearchResultsMsg{
					ChannelID: string(channelID),
					Query:     query,
					Terms:     strings.Fields(text.Fold(query)),
					TSes:      w.search(string(channelID), query),
				}
			},
			SearchWorkspace: func(query string) core.Msg {
				return ui.WorkspaceSearchResultsMsg{Query: query}
			},
		}),
		unread: core.NewUnreadService(w.readStates, w.unreadTeams),
		workspace: core.NewWorkspaceService(func(teamID string) core.Msg {
			s, ok := w.snapshot(teamID)
			if !ok || !w.setActive(teamID) {
				return nil
			}
			dir.observe(Event{Kind: EventWorkspaceSwitched, TeamID: teamID})
			return ui.WorkspaceSwitchedMsg{
				TeamID: s.id, TeamName: s.name, Domain: s.domain, Theme: s.theme,
				Channels: s.channels, FinderItems: s.finder, UserNames: s.userNames,
				UserStatuses: s.statuses, UserID: s.selfID,
			}
		}),
		avatars:  core.NewAvatarService(func(userID string) string { return d.avatars[userID] }),
		settings: core.NewSettingsService(nil, nil),
		presence: core.NewPresenceService(nil, nil),
		profiles: core.NewProfileService(core.ProfileServiceFuncs{
			Profile: func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
				return w.profile(teamID, userID)
			},
		}),
		files: core.NewFileService(
			func(string, string, string, []core.PendingAttachment) core.Cmd {
				return func() core.Msg { return ui.UploadResultMsg{Err: errUnavailable} }
			},
			func(context.Context, string, string) (string, error) { return "", errUnavailable },
		),
	}
}
