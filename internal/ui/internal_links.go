// internal_links.go
//
// Links to slk itself: the slack:// addresses rendered for #channel
// mentions, and any other Slack deep link a message carries. They never
// reach the OS launcher - a target slk cannot open is refused with a
// toast - so the click behaves like an in-app navigation, not like an
// external link.
package ui

import (
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
)

// openInternalLink handles a slack:// link. handled is false for every
// other scheme and true for slack:// whether or not the target opened,
// so unknown or malformed Slack links are refused here rather than
// handed to the OS.
func (a *App) openInternalLink(raw string) (cmd tea.Cmd, handled bool) {
	// Recognize the scheme before parsing: url.Parse fails on some
	// Slack-shaped strings (a legacy attachment's TitleLink, say), and
	// a parse failure must still refuse in-app instead of falling
	// through to the external-link confirmation.
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "slack:") {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return toastWithClear(a, "slk can't open this Slack link", 2*time.Second), true
	}
	switch strings.ToLower(u.Host) {
	case "channel":
		q := u.Query()
		return a.openChannelLink(q.Get("id"), q.Get("name")), true
	default:
		return toastWithClear(a, "slk can't open this Slack link yet", 2*time.Second), true
	}
}

// internalLinkLabel names an internal link for display. A bare channel
// mention's URL carries no label (extraction has no name map), and a
// resolved name may live only in the renderer's channel-name map.
func (a *App) internalLinkLabel(raw, label string) string {
	if label != "" || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "slack:") {
		return label
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, "channel") {
		return label
	}
	q := u.Query()
	name := q.Get("name")
	if name == "" {
		name = a.channelNames[q.Get("id")]
	}
	if name == "" {
		return label
	}
	return "#" + name
}

// openChannelLink opens channelID in the active workspace, joining it
// when the user is not a member. name is the mention's label, used when
// the channel is not in the local store (joining needs a name to show).
func (a *App) openChannelLink(channelID, name string) tea.Cmd {
	if channelID == "" {
		return toastWithClear(a, "This channel link has no channel", 2*time.Second)
	}
	// The finder knows membership; Lookup alone does not (it also holds
	// browseable channels the user has not joined).
	if it, ok := a.findChannelByID(channelID); ok {
		if it.Name != "" {
			name = it.Name
		}
		chType := it.Type
		if chType == "" {
			chType = "channel"
		}
		return a.openChannel(channelfinder.ChannelResult{
			ID: channelID, Name: name, Type: chType, Joined: it.Joined,
		})
	}
	if local, chType, ok := a.channels.Lookup(ids.ChannelID(channelID)); ok {
		if local != "" {
			name = local
		}
		if chType == "" {
			chType = "channel"
		}
		// Known to the workspace but not in the finder: membership is
		// unknown here, so let the join path decide. conversations.join
		// succeeds for a channel the user is already in.
		return a.openChannel(channelfinder.ChannelResult{
			ID: channelID, Name: name, Type: chType,
		})
	}
	if name == "" || name == "unknown" {
		// A resolved bare mention (<#C9>) carries its name in the
		// renderer's map, not in the extracted URL; the keyboard path
		// still needs it to join.
		if resolved := a.channelNames[channelID]; resolved != "" {
			name = resolved
		}
	}
	if name == "" || name == "unknown" {
		// Without a name there is nothing to show while joining (and
		// the mention itself was never resolved).
		return toastWithClear(a, "That channel isn't in this workspace", 2*time.Second)
	}
	return a.openChannel(channelfinder.ChannelResult{
		ID: channelID, Name: name, Type: "channel",
	})
}
