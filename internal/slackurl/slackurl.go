// Package slackurl parses Slack archive permalinks
// (https://<subdomain>.slack.com/archives/<CHANNEL>/p<digits>) into
// their components so the UI can navigate to the referenced
// conversation in-app instead of opening a browser, and builds the
// huddle deep link for a conversation.
//
// Pure functions only: no I/O, no dependencies beyond net/url and the
// ids types.
package slackurl

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/ids"
)

// Permalink is the parsed form of a Slack archive permalink.
type Permalink struct {
	// Subdomain is the workspace subdomain, e.g. "truelist-workspace"
	// for truelist-workspace.slack.com.
	Subdomain string
	ChannelID ids.ChannelID
	// MessageTS is the target message timestamp ("1779284733.270139"),
	// decoded from the path's p-value by inserting a dot before the
	// last six digits.
	MessageTS ids.MessageTS
	// ThreadTS is the thread parent ts from the thread_ts query
	// parameter; empty when the link targets a channel-level message.
	ThreadTS ids.ThreadTS
}

// archivePathRe matches "/archives/<CHANNEL>/p<digits>". The p-value
// must be long enough to split into seconds + 6 digits of microseconds
// (Slack always emits 16 digits; we accept >= 11 to be lenient about
// future second-counter widths but reject obviously-wrong values).
var archivePathRe = regexp.MustCompile(`^/archives/([A-Z0-9]+)/p(\d{11,})$`)

// Parse decodes a Slack archive permalink. ok is false for anything
// that is not an https://<subdomain>.slack.com/archives/... message
// link. The cid query parameter is accepted but ignored — the channel
// ID in the path wins.
func Parse(rawURL string) (Permalink, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return Permalink{}, false
	}
	host := u.Host
	if !strings.HasSuffix(host, ".slack.com") {
		return Permalink{}, false
	}
	sub := strings.TrimSuffix(host, ".slack.com")
	if sub == "" {
		return Permalink{}, false
	}
	m := archivePathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return Permalink{}, false
	}
	digits := m[2]
	ts := digits[:len(digits)-6] + "." + digits[len(digits)-6:]
	return Permalink{
		Subdomain: sub,
		ChannelID: ids.ChannelID(m[1]),
		MessageTS: ids.MessageTS(ts),
		ThreadTS:  ids.ThreadTS(u.Query().Get("thread_ts")),
	}, true
}

// HuddleURL builds the deep link Slack's own "Copy huddle link"
// produces for a conversation: https://app.slack.com/huddle/<TEAM>/<CHANNEL>.
// The channel may be a regular channel (C), a group DM (G) or a DM (D);
// opening it joins the live huddle or shows Slack's huddle panel for
// that conversation when none is active.
func HuddleURL(teamID, channelID string) string {
	return "https://app.slack.com/huddle/" + teamID + "/" + channelID
}
