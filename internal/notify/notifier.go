// Package notify provides desktop notification support.
package notify

import (
	"regexp"
	"strings"

	"github.com/gen2brain/beeep"
)

// Notifier sends OS-level desktop notifications.
type Notifier struct {
	enabled bool
}

// New creates a Notifier. If enabled is false, Notify is a no-op.
func New(enabled bool) *Notifier {
	return &Notifier{enabled: enabled}
}

// Notify sends a desktop notification with the given title and body.
// Returns nil if notifications are disabled.
func (n *Notifier) Notify(title, body string) error {
	if !n.enabled {
		return nil
	}
	return beeep.Notify(title, body, "")
}

// NotifyContext holds the state needed to evaluate notification triggers.
type NotifyContext struct {
	CurrentUserID   string
	ActiveChannelID string
	IsActiveWS      bool
	OnMention       bool
	OnDM            bool
	OnKeyword       []string
}

// ShouldNotify returns true if a message should trigger a desktop notification.
func ShouldNotify(ctx NotifyContext, channelID, userID, text, channelType string) bool {
	// Never notify for own messages
	if userID == ctx.CurrentUserID {
		return false
	}

	// Suppress if viewing this channel on the active workspace
	if ctx.IsActiveWS && channelID == ctx.ActiveChannelID {
		return false
	}

	// Check DM trigger
	if ctx.OnDM && (channelType == "dm" || channelType == "group_dm") {
		return true
	}

	// Check mention trigger
	if ctx.OnMention && strings.Contains(text, "<@"+ctx.CurrentUserID+">") {
		return true
	}

	// Check keyword triggers
	if len(ctx.OnKeyword) > 0 {
		lower := strings.ToLower(text)
		for _, kw := range ctx.OnKeyword {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return true
			}
		}
	}

	return false
}

var (
	userMentionRe    = regexp.MustCompile(`<@[A-Z0-9]+>`)
	channelMentionRe = regexp.MustCompile(`<#[A-Z0-9]+\|([^>]+)>`)
	linkWithLabelRe  = regexp.MustCompile(`<(https?://[^|>]+)\|([^>]+)>`)
	linkBareRe       = regexp.MustCompile(`<(https?://[^>]+)>`)
)

// StripSlackMarkup converts Slack-formatted text to plain text.
// Truncates to 100 characters with "..." suffix.
func StripSlackMarkup(text string) string {
	text = channelMentionRe.ReplaceAllString(text, "#$1")
	text = linkWithLabelRe.ReplaceAllString(text, "$2")
	text = linkBareRe.ReplaceAllString(text, "$1")
	text = userMentionRe.ReplaceAllString(text, "@someone")
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "_", "")
	text = strings.ReplaceAll(text, "~", "")
	text = strings.ReplaceAll(text, "`", "")

	if len(text) > 100 {
		text = text[:100] + "..."
	}

	return text
}
