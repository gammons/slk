// internal/ui/slash_builtins.go
//
// Slash built-ins slk runs itself, because Slack's web client handles
// them locally instead of through chat.command. Each cites the capture
// in docs/superpowers/specs/2026-10-12-slash-commands-captures/ (or the
// spec §2 table) that shows the web client's behaviour.
package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slackfmt"
	"github.com/gammons/slk/internal/ui/channelfinder"
)

// slashAway toggles presence, as the web client's /away did twice in a
// row (presence_set_away.json, then presence.set active).
func slashAway(a *App, _ commandArgs) tea.Cmd {
	if p, _, _, _ := a.presence.Status(a.activeTeamID); p == "away" {
		a.applyPresenceAction(core.PresenceSetActive, 0)
		return nil
	}
	a.applyPresenceAction(core.PresenceSetAway, 0)
	return nil
}

// slashActive sets presence to active.
func slashActive(a *App, _ commandArgs) tea.Cmd {
	a.applyPresenceAction(core.PresenceSetActive, 0)
	return nil
}

// slashStatus handles "/status away" (captured: the web client called
// presence.set) and, by symmetry, "/status active". Bare /status and a
// custom status are not captured, so they are unsupported for now.
func slashStatus(a *App, args commandArgs) tea.Cmd {
	if len(args.Fields) == 1 {
		switch strings.ToLower(args.Fields[0]) {
		case "away":
			a.applyPresenceAction(core.PresenceSetAway, 0)
			return nil
		case "active":
			a.applyPresenceAction(core.PresenceSetActive, 0)
			return nil
		}
	}
	return slashUnsupported("/status")(a, args)
}

// slashUnsupported is a built-in the web client handles locally that slk
// does not do yet: it says so rather than sending it to chat.command.
func slashUnsupported(name string) commandFunc {
	return func(a *App, args commandArgs) tea.Cmd {
		return a.refuseCommand(args.Origin, name+" isn't supported in slk yet")
	}
}

// slashOpen switches to a channel, joining it first if needed, as the
// web client's /open did (navigation only, no chat.command). /join is
// its alias (client_app_commands.json: alias_of "/open").
//
// A mention resolves by ID against the finder's rows; one the finder does
// not hold continues as its label, like a typed name. A name resolves
// against the finder first; a name the user is not in is looked up on the
// server, since the finder holds only the user's own conversations until
// a search runs. The answer comes back as channelLookupMsg. name is the
// command as typed (/open or /join), for the usage line.
func slashOpen(name string) commandFunc {
	return func(a *App, args commandArgs) tea.Cmd {
		if strings.TrimSpace(args.Rest) == "" {
			return a.refuseCommand(args.Origin, "Usage: "+name+" #channel")
		}
		channel := strings.TrimPrefix(args.Fields[0], "#")
		if mentioned := slackfmt.MentionedChannels(args.Rest); len(mentioned) > 0 {
			if item, ok := a.findChannelByID(mentioned[0].ID); ok {
				return a.openChannel(channelResultOf(item))
			}
			if mentioned[0].Label != "" {
				channel = mentioned[0].Label
			}
		}
		if item, ok := matchChannelByName(a.channelFinder.Items(), channel); ok {
			return a.openChannel(channelResultOf(item))
		}
		channels, teamID, origin := a.channels, a.activeTeamID, args.Origin
		return func() tea.Msg {
			item, found := matchChannelByName(channels.SearchRemote(channel), channel)
			return channelLookupMsg{teamID: teamID, origin: origin, name: channel, item: item, found: found}
		}
	}
}

// channelLookupMsg is the server's answer to a /open name the finder did
// not hold. name is as the user typed it, without the "#"; teamID is the
// workspace the lookup ran in, so an answer that arrives after a switch
// is dropped; origin is where /open was typed, to give the text back when
// nothing matches.
type channelLookupMsg struct {
	teamID string
	origin commandOrigin
	name   string
	item   core.ChannelFinderItem
	found  bool
}

// findChannelByID returns the finder's row for a channel ID. Synthetic
// destinations never match.
func (a *App) findChannelByID(id string) (core.ChannelFinderItem, bool) {
	for _, it := range a.channelFinder.Items() {
		if !it.Synthetic && it.ID == id {
			return it, true
		}
	}
	return core.ChannelFinderItem{}, false
}

// matchChannelByName finds the channel named name among items, joined or
// not. A leading "#" is ignored and the match is case- and
// accent-insensitive. Only channels and private channels match: a DM
// named the same is not a channel, and synthetic destinations are never
// one.
func matchChannelByName(items []core.ChannelFinderItem, name string) (core.ChannelFinderItem, bool) {
	matches := core.ChannelNameMatcher(name)
	for _, it := range items {
		if !it.Synthetic && matches(it.Type, it.Name) {
			return it, true
		}
	}
	return core.ChannelFinderItem{}, false
}

// channelResultOf is the finder result openChannel takes for a row.
func channelResultOf(it core.ChannelFinderItem) channelfinder.ChannelResult {
	return channelfinder.ChannelResult{ID: it.ID, Name: it.Name, Type: it.Type, Joined: it.Joined}
}

// slashMsg opens the DM with one user. The web client's /msg resolved
// the user and posted with chat.postMessage (spec §2); slk pre-fills the
// message in the DM's composer instead of sending it (spec §3.3). The
// text is wire form, so a mention inside it shows as <@U…> in the
// composer; it still sends correctly. /dm is its alias. The toasts are
// built only on the error paths: building one sets it.
func slashMsg(name string) commandFunc {
	return func(a *App, args commandArgs) tea.Cmd {
		usage := func() tea.Cmd {
			return a.refuseCommand(args.Origin, "Usage: "+name+" @someone [message]")
		}
		if len(args.Fields) == 0 {
			return usage()
		}
		first := args.Fields[0]
		users := slackfmt.MentionedUserIDs(first)
		if len(users) != 1 || first != "<@"+users[0]+">" {
			if strings.HasPrefix(first, "@") {
				return a.refuseCommand(args.Origin, "No one called "+first)
			}
			return usage()
		}
		// One open at a time: a second would supersede the first and
		// drop its text.
		if a.openingConversation() {
			return a.refuseCommand(args.Origin, "Still opening a conversation; try again in a moment")
		}
		text := strings.TrimLeftFunc(strings.TrimPrefix(args.Rest, first), unicode.IsSpace)
		return a.openConversation([]string{users[0]}, msgPrefill{text: text, origin: args.Origin})
	}
}

// slashStar stars the conversation the command was typed in
// (stars.add, the call behind the web client's "Star channel"). The
// sidebar row moves into the Starred section optimistically; a refusal
// (ChannelStarSetMsg with Err set) moves it back.
func slashStar(a *App, args commandArgs) tea.Cmd {
	return a.setConversationStarred(args, true)
}

// slashUnstar un-stars it (stars.remove).
func slashUnstar(a *App, args commandArgs) tea.Cmd {
	return a.setConversationStarred(args, false)
}

// setConversationStarred is the body of /star and /unstar. The
// conversation is the one the composer belongs to: a thread composer
// stars its parent channel, since Slack stars are conversation-level.
func (a *App) setConversationStarred(args commandArgs, starred bool) tea.Cmd {
	chID := args.Origin.channelID
	if chID == "" {
		chID = a.activeChannelID
	}
	if chID == "" {
		return a.refuseCommand(args.Origin, "No conversation to star")
	}
	cmd := a.setStarredChannel(chID, starred)
	if cmd == nil {
		return a.refuseCommand(args.Origin, "That conversation isn't in the sidebar")
	}
	return cmd
}

// starSnapshot is a sidebar row's star state before an optimistic
// move, restored when Slack refuses the change.
type starSnapshot struct {
	starred bool
	section string
}

// setStarredChannel is the shared body of every path that sets a star:
// /star, /unstar, and a click on the header glyph. It moves the sidebar
// row optimistically, updates the header glyph, and dispatches the
// SetStarred command whose ChannelStarSetMsg arm reverts both on a
// refusal, restoring the row's pre-request state (not merely the
// opposite of the request) and ignoring replies a newer request
// superseded. nil means the channel isn't in the sidebar (or there is
// nothing to send); the caller words its own refusal.
func (a *App) setStarredChannel(channelID string, starred bool) tea.Cmd {
	// Everything below is scoped to the workspace the request runs in:
	// on a grid the same channel ID exists in sibling workspaces, and a
	// reply arriving after a workspace switch must not touch the
	// workspace now on screen.
	teamID := a.activeTeamID
	key := teamID + "\x00" + channelID
	// The row's pre-request state, kept until the request settles; a
	// second request while one is pending keeps the FIRST snapshot, so
	// the restore lands on what Slack actually held before the burst.
	if a.starSnapshots == nil {
		a.starSnapshots = map[string]starSnapshot{}
	}
	if _, pending := a.starSnapshots[key]; !pending {
		if prev, section, ok := a.sidebar.ItemStarState(channelID); ok {
			a.starSnapshots[key] = starSnapshot{starred: prev, section: section}
		}
	}
	if a.starSeqs == nil {
		a.starSeqs = map[string]int{}
	}
	a.starSeqs[key]++
	seq := a.starSeqs[key]

	if !a.sidebar.SetItemStarred(channelID, starred) {
		return nil
	}
	a.syncHeaderStar()
	inner := teaCmd(a.channels.SetStarred(ids.TeamID(teamID), ids.ChannelID(channelID), starred))
	if inner == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := inner().(ChannelStarSetMsg)
		if !ok {
			return nil
		}
		msg.TeamID, msg.Seq = teamID, seq
		return msg
	}
}

// syncHeaderStar pushes each open window's channel star state to that
// window's header glyph - every split, not only the focused pane, so
// two windows showing conversations whose stars differ (or changed)
// never disagree. Called wherever the active channel, a sidebar item
// swap, or a star change can move a glyph.
func (a *App) syncHeaderStar() {
	for _, id := range a.wins.Leaves() {
		winChID := ""
		if id == a.focusedWin {
			winChID = a.activeChannelID
		} else if ch, ok := a.wins.Channel(id); ok {
			winChID = ch.ID
		}
		if winChID == "" {
			continue
		}
		starred, _ := a.sidebar.IsItemStarred(winChID)
		if model := a.winModels[id]; model != nil {
			model.SetChannelStarred(starred)
		}
	}
}
