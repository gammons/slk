# Desktop Notifications Design

Date: 2026-04-26

## Overview

Send OS-level desktop notifications when messages arrive that match configured triggers (mentions, DMs, keywords). Uses the `beeep` library for cross-platform support. Focus suppression prevents notifications for the channel currently being viewed.

## Existing Infrastructure

- **Config:** `config.Notifications` struct with `Enabled`, `OnMention`, `OnDM`, `OnKeyword` fields, defaults to enabled (`internal/config/config.go:47-53,73-77`)
- **WebSocket events:** `rtmEventHandler.OnMessage()` receives all incoming messages with `channelID`, `userID`, `ts`, `text`, `threadTS` (`cmd/slk/main.go:766-803`)
- **User names:** `h.userNames map[string]string` available on the handler for resolving sender display names
- **Channel metadata:** `sidebar.ChannelItem` has `Type` field (`"dm"`, `"channel"`, `"private"`, `"group_dm"`) and `Name`
- **Unread tracking:** Fully implemented for sidebar badges and workspace rail dots

## Notification Package

New package: `internal/notify/`

### Notifier

```go
type Notifier struct {
    enabled bool
}

func New(enabled bool) *Notifier

func (n *Notifier) Notify(title, body string) error
```

The `Notify` method checks `enabled` and calls `beeep.Notify(title, body, "")`. The empty string means no icon. Returns nil if disabled or on success.

The notifier is intentionally thin -- it only handles the OS notification mechanism. All trigger logic lives in the caller.

### Testing

The notifier is tested with `enabled=false` to verify suppression (calling `beeep.Notify` in tests would pop real desktop notifications). The trigger logic in `main.go` is tested indirectly through integration.

## Trigger Logic

Located in `rtmEventHandler.OnMessage()` in `cmd/slk/main.go`. After the existing message caching and UI dispatch, check notification triggers.

### Trigger Rules

A message triggers a notification if **any** of these conditions match:

1. **DM:** Channel type is `"dm"` or `"group_dm"` and `cfg.Notifications.OnDM` is true
2. **Mention:** Message text contains `<@CURRENT_USER_ID>` and `cfg.Notifications.OnMention` is true
3. **Keyword:** Message text contains any string from `cfg.Notifications.OnKeyword` (case-insensitive substring match)

### Suppression Rules

- **Self-messages:** Never notify when sender == current user ID
- **Active channel:** Never notify when the message is for the currently-viewed channel on the active workspace. The handler's `isActive()` check already distinguishes active vs inactive workspace. For the active workspace, compare `channelID` against the active channel ID (accessed via a callback/closure).
- **Disabled:** Skip all checks when `cfg.Notifications.Enabled` is false

### Trigger Check Function

A pure function for testability:

```go
type NotifyContext struct {
    CurrentUserID   string
    ActiveChannelID string
    IsActiveWS      bool
    OnMention       bool
    OnDM            bool
    OnKeyword       []string
}

func ShouldNotify(ctx NotifyContext, channelID, userID, text, channelType string) bool
```

This lives in the `notify` package so it can be unit tested without the full app context.

## Notification Content

- **Title:** `workspaceName: #channelName` (for channels/private) or `workspaceName: senderName` (for DMs)
- **Body:** First 100 characters of message text, plain text (Slack markup like `<@U123>`, `<url|label>`, `*bold*` stripped to plain text)

### Text Stripping

A simple function to strip Slack markup to plain text:

```go
func StripSlackMarkup(text string) string
```

Handles:
- `<@U123>` -> `@username` (or just removes if no resolution available)
- `<#C123|channel>` -> `#channel`
- `<url|label>` -> `label`
- `<url>` -> `url`
- `*bold*` -> `bold`, `_italic_` -> `italic`, `~strike~` -> `strike`
- `` `code` `` -> `code`

Truncate to 100 characters with `...` suffix if longer.

## Data Wiring

### rtmEventHandler additions

The handler needs:
- `notifier *notify.Notifier` -- the notification sender
- `currentUserID string` -- to filter self-messages and detect @mentions
- `channelNames map[string]string` -- channel ID -> display name
- `channelTypes map[string]string` -- channel ID -> type ("dm", "channel", etc.)
- `activeChannelID func() string` -- callback to get the currently viewed channel
- `workspaceName string` -- for notification title
- `notifyCfg config.Notifications` -- trigger configuration

### Wiring in main.go

The notifier is created once in `run()` and shared across all workspace handlers:

```go
notifier := notify.New(cfg.Notifications.Enabled)
```

Channel names/types are built from the `[]sidebar.ChannelItem` slice that's already constructed per workspace. The `activeChannelID` callback reads from the app's active channel state.

## Files

| File | Changes |
|------|---------|
| `internal/notify/notifier.go` | New: `Notifier` struct, `New()`, `Notify()`, `ShouldNotify()`, `StripSlackMarkup()` |
| `internal/notify/notifier_test.go` | New: tests for `ShouldNotify` and `StripSlackMarkup` |
| `cmd/slk/main.go` | Add notifier to `rtmEventHandler`, add trigger check in `OnMessage`, wire at construction |
| `go.mod` / `go.sum` | Add `github.com/gen2brain/beeep` dependency |
| `docs/STATUS.md` | Move notifications from "Not Yet Implemented" to "What's Working" |

## Out of Scope

- Quiet hours (separate follow-up)
- Per-channel mute
- Sound/custom notification icons
- In-app toast notifications
