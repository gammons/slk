// Package notify provides desktop notification support.
package notify

import (
	"os"
	"os/exec"
	"strings"

	"github.com/gammons/slk/internal/slackfmt"

	"github.com/gen2brain/beeep"
)

// Notifier sends OS-level desktop notifications.
type Notifier struct {
	enabled bool
	command string
}

// New creates a Notifier. If enabled is false, Notify is a no-op. When command
// is non-empty, Notify runs it in place of the built-in OS notification, which
// lets you route notifications through your own tooling (a terminal
// multiplexer's notifier, terminal-notifier, mako, etc.).
func New(enabled bool, command string) *Notifier {
	return &Notifier{enabled: enabled, command: command}
}

// Notify delivers a notification with the given title and body. It returns nil
// when notifications are disabled. If a notify_command is configured it runs in
// place of the built-in OS notification; otherwise beeep shows the OS one.
func (n *Notifier) Notify(title, body string) error {
	if !n.enabled {
		return nil
	}
	if n.command != "" {
		return n.runCommand(title, body)
	}
	return beeep.Notify(title, body, "")
}

// runCommand runs the configured notify_command via `sh -c`, exposing the
// notification's title and body as $SLK_TITLE and $SLK_BODY. They are passed
// through the environment rather than interpolated into the command string, so
// arbitrary message text (e.g. a body containing "; rm -rf ~") cannot inject
// shell syntax. Notify is already called from its own goroutine, so a
// synchronous Run — which also reaps the child — is fine.
func (n *Notifier) runCommand(title, body string) error {
	cmd := exec.Command("sh", "-c", n.command)
	cmd.Env = append(os.Environ(), "SLK_TITLE="+title, "SLK_BODY="+body)
	return cmd.Run()
}

// NotifyContext holds the state needed to evaluate notification triggers.
type NotifyContext struct {
	CurrentUserID   string
	ActiveChannelID string
	IsActiveWS      bool
	OnMention       bool
	OnDM            bool
	OnKeyword       []string
	IsDND           bool // when true, ShouldNotify always returns false
	IsMuted         bool // when true (conversation is muted), ShouldNotify always returns false
}

// ShouldNotify returns true if a message should trigger a desktop notification.
func ShouldNotify(ctx NotifyContext, channelID, userID, text, channelType string) bool {
	// Never notify for own messages
	if userID == ctx.CurrentUserID {
		return false
	}

	// Suppress entirely while DND/snoozed.
	if ctx.IsDND {
		return false
	}

	// Suppress notifications from a muted conversation — a muted channel or DM
	// is silent, matching Slack.
	if ctx.IsMuted {
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
	if ctx.OnMention && (strings.Contains(text, "<@"+ctx.CurrentUserID+">") ||
		strings.Contains(text, "<!here>") ||
		strings.Contains(text, "<!channel>") ||
		strings.Contains(text, "<!everyone>")) {
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

// StripSlackMarkup converts Slack-formatted text to plain text suitable for
// OS notification bodies. User mentions are resolved against userNames; if
// a user ID is missing from the map (or the map is nil) the raw user ID is
// used as a fallback. Output is truncated to 100 characters (by rune) with a
// "..." suffix.
func StripSlackMarkup(text string, userNames map[string]string) string {
	return StripSlackMarkupWithUserGroups(text, userNames, nil)
}

// StripSlackMarkupWithUserGroups is StripSlackMarkup with a workspace-scoped
// Slack usergroup map for resolving bare <!subteam^SID> tokens. The markup
// stripping itself lives in internal/slackfmt; this adds the notification
// body's length cap.
func StripSlackMarkupWithUserGroups(text string, userNames, userGroups map[string]string) string {
	text = slackfmt.StripMarkupWithUserGroups(text, userNames, userGroups)

	// Cap by rune, not byte, so a multibyte body (e.g. Japanese) isn't
	// sliced mid-rune into invalid UTF-8.
	if r := []rune(text); len(r) > 100 {
		text = string(r[:100]) + "..."
	}
	return text
}
