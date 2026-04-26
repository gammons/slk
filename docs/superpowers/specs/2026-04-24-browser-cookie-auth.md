# Browser Cookie Auth (xoxc/xoxd) Design

## Problem

The current auth flow requires creating a Slack App with specific OAuth scopes and Socket Mode enabled. This requires workspace admin permissions to install the app. Browser cookie auth allows any user to connect by reusing their existing browser session tokens.

## Solution

Replace the Slack App auth (xoxp + xapp tokens, Socket Mode) with browser cookie auth (xoxc token + d cookie, RTM).

### How It Works

1. User logs into Slack in their browser normally
2. User opens browser devtools, copies two values:
   - The `xoxc-*` token (visible in localStorage, network requests, or the browser console)
   - The `d` cookie value (from Application > Cookies > `d` on `.slack.com`)
3. User runs `slk --add-workspace` and pastes these two values
4. The TUI creates a `slack.Client` using the xoxc token and a custom `http.Client` that attaches the `d` cookie to every request via `slack.OptionHTTPClient()`
5. Real-time events use RTM (WebSocket) instead of Socket Mode

### Token Storage

The `Token` struct changes:

```go
// Before
type Token struct {
    AccessToken  string `json:"access_token"`   // xoxp-...
    RefreshToken string `json:"refresh_token"`
    AppToken     string `json:"app_token"`       // xapp-...
    TeamID       string `json:"team_id"`
    TeamName     string `json:"team_name"`
}

// After
type Token struct {
    AccessToken string `json:"access_token"` // xoxc-...
    Cookie      string `json:"cookie"`       // d cookie value
    TeamID      string `json:"team_id"`
    TeamName    string `json:"team_name"`
}
```

Storage format remains JSON files at `~/.local/share/slk/tokens/{teamID}.json` with `0600` permissions.

### Client Creation

`NewClient` signature changes from `(userToken, appToken string)` to `(xoxcToken, dCookie string)`.

Implementation:
- Create an `http.CookieJar` with the `d` cookie set for `https://*.slack.com`
- Create an `http.Client` with that cookie jar
- Pass it to `slack.New(xoxcToken, slack.OptionHTTPClient(httpClient))`
- Use `api.NewRTM()` instead of `socketmode.New()` for real-time events

### Real-Time Events (RTM)

Replace Socket Mode with RTM. The `Client` struct changes:

```go
// Before
type Client struct {
    api       *slack.Client
    socket    *socketmode.Client
    teamID    string
    userID    string
    userToken string
    appToken  string
}

// After
type Client struct {
    api      *slack.Client
    rtm      *slack.RTM
    teamID   string
    userID   string
    token    string
    cookie   string
}
```

Connection: `rtm.ManageConnection()` replaces `socket.RunContext()`. RTM handles reconnection automatically.

### Event Dispatcher

The `EventDispatcher` changes to consume RTM events instead of Socket Mode events:

| RTM Event Type | Handler Method |
|---|---|
| `*slack.MessageEvent` | `OnMessage` (new/edited) or `OnMessageDeleted` |
| `*slack.ReactionAddedEvent` | `OnReactionAdded` |
| `*slack.ReactionRemovedEvent` | `OnReactionRemoved` |
| `*slack.PresenceChangeEvent` | `OnPresenceChange` |
| `*slack.UserTypingEvent` | `OnUserTyping` |
| `*slack.ConnectedEvent` | Log connection info |
| `*slack.InvalidAuthEvent` | Surface re-auth prompt |

The `EventHandler` interface is unchanged. Only the dispatcher implementation changes.

### Onboarding Flow

The `--add-workspace` flow changes to prompt for:
1. xoxc token (validate prefix `xoxc-`)
2. d cookie value (validate non-empty)

Instructions shown to user explain how to find these values in browser devtools.

### What Stays the Same

- All Web API calls (channels, messages, users, reactions, etc.)
- SQLite cache layer
- Service layer (WorkspaceManager, MessageService)
- UI layer (all components)
- `SlackAPI` interface

### Auth Failure Handling

xoxc tokens and d cookies expire when the user logs out of the browser or when Slack rotates them. On auth failure:
- Detect `InvalidAuthEvent` from RTM or HTTP 401/403 from Web API calls
- Show clear status bar message: "Authentication expired -- run --add-workspace to re-authenticate"
- Gracefully degrade: cached data remains browsable

### Files Changed

- `internal/slack/auth.go` -- Update `Token` struct (remove `AppToken`/`RefreshToken`, add `Cookie`)
- `internal/slack/client.go` -- Replace Socket Mode with RTM, add cookie jar HTTP client
- `internal/slack/events.go` -- Replace Socket Mode event dispatcher with RTM event loop
- `cmd/slk/onboarding.go` -- Update prompts for xoxc token and d cookie
- `cmd/slk/main.go` -- Update client wiring to pass cookie instead of app token
- `internal/slack/auth_test.go` -- Update for new Token struct
- `internal/slack/client_test.go` -- Update for new constructor
- `internal/slack/events_test.go` -- Update for RTM events
