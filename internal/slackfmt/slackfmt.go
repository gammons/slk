package slackfmt

// StripMarkup (below) converts Slack-formatted message text ("mrkdwn") to
// plain text: it resolves user/channel/subteam mentions and broadcast
// tokens, unwraps links, and drops emphasis punctuation, applying no length
// limit (callers clip to whatever width they render at). It lives here so
// OS-notification bodies (internal/notify) and in-app previews (the Activity
// view) share one implementation instead of the view depending on the
// notification formatter.

import (
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/linkscheme"
	"github.com/gammons/slk/internal/usergroups"
)

var (
	userMentionRe = regexp.MustCompile(`<@([A-Z0-9]+)>`)
	// Group 1 is the channel ID, group 2 its label.
	channelMentionRe = regexp.MustCompile(`<#([A-Z0-9]+)\|([^>]+)>`)
	// Group 1 is the usergroup ID; group 2 the optional embedded label.
	// Labeled forms normalize to "@label"; bare forms resolve through the
	// caller's workspace-scoped usergroup map.
	subteamMentionRe = regexp.MustCompile(`<!subteam\^([A-Z0-9]+)(?:\|([^>]+))?>`)
	broadcastRe      = regexp.MustCompile(`<!(here|channel|everyone)>`)
	// Match URLs with an allowed scheme (linkscheme) including mailto:
	// addresses; Slack auto-linkifies typed emails into <mailto:X|X>.
	// Bare-link substitution keeps the URL as-is but strips the mailto:
	// prefix so the text reads as just the address.
	linkWithLabelRe = regexp.MustCompile(`<((?:` + linkscheme.Alternation + `)[^|>]+)\|([^>]+)>`)
	linkBareRe      = regexp.MustCompile(`<((?:` + linkscheme.Alternation + `)[^>]+)>`)
)

// MentionedUserIDs returns the user IDs of every <@U…> mention in text,
// in order, duplicates included.
func MentionedUserIDs(text string) []string {
	matches := userMentionRe.FindAllStringSubmatch(text, -1)
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m[1])
	}
	return ids
}

// MentionedChannelIDs returns the channel IDs of every <#C…|name>
// mention in text, in order, duplicates included. This is the form the
// composer sends (compose.TranslateMentionsForSend).
func MentionedChannelIDs(text string) []string {
	mentions := MentionedChannels(text)
	ids := make([]string, 0, len(mentions))
	for _, m := range mentions {
		ids = append(ids, m.ID)
	}
	return ids
}

// ChannelMention is one <#C…|label> mention: the channel's ID and the
// name the text carries for it.
type ChannelMention struct {
	ID, Label string
}

// MentionedChannels returns every <#C…|label> mention in text, in order,
// duplicates included.
func MentionedChannels(text string) []ChannelMention {
	matches := channelMentionRe.FindAllStringSubmatch(text, -1)
	mentions := make([]ChannelMention, 0, len(matches))
	for _, m := range matches {
		mentions = append(mentions, ChannelMention{ID: m[1], Label: m[2]})
	}
	return mentions
}

// StripMarkup converts Slack mrkdwn to plain text, resolving user mentions
// against userNames. Bare <!subteam^ID> tokens can't be resolved without a
// usergroup map, so they fall back to "@group"; use StripMarkupWithUserGroups
// when a map is available. No truncation is applied.
func StripMarkup(text string, userNames map[string]string) string {
	return StripMarkupWithUserGroups(text, userNames, nil)
}

// StripMarkupWithUserGroups is StripMarkup with a workspace-scoped Slack
// usergroup map (ID -> handle) for resolving bare <!subteam^ID> tokens.
func StripMarkupWithUserGroups(text string, userNames, userGroups map[string]string) string {
	text = channelMentionRe.ReplaceAllString(text, "#$2")
	text = linkWithLabelRe.ReplaceAllString(text, "$2")
	text = linkBareRe.ReplaceAllStringFunc(text, func(match string) string {
		url := linkBareRe.FindStringSubmatch(match)[1]
		return strings.TrimPrefix(url, "mailto:")
	})
	text = subteamMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		groups := subteamMentionRe.FindStringSubmatch(match)
		return usergroups.Display(userGroups, groups[1], groups[2])
	})
	text = broadcastRe.ReplaceAllString(text, "@$1")
	text = userMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		userID := userMentionRe.FindStringSubmatch(match)[1]
		if name, ok := userNames[userID]; ok {
			return "@" + name
		}
		return "@" + userID
	})
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "_", "")
	text = strings.ReplaceAll(text, "~", "")
	text = strings.ReplaceAll(text, "`", "")
	return text
}
