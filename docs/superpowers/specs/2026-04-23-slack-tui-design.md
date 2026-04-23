# slack-tui Design Specification

A terminal-based Slack client built in Go using bubbletea and lipgloss, designed as a personal daily-driver replacement for the official Slack desktop client.

## Goals

- Replace or supplement the official Slack desktop client for a terminal power user
- Vim-inspired keyboard-driven interface with modal editing
- Multi-workspace support with concurrent connections
- Polished, functional TUI with subtle, configurable animations
- Inline image rendering where the terminal supports it
- Desktop notifications for mentions, DMs, and keyword matches

## Non-Goals (v1)

- Huddles / audio/video calls
- Slack Connect (cross-org channels)
- Workflow Builder / apps marketplace
- Bot/app management
- Custom emoji management
- Slash commands

## v1 Feature Set

- Channels (public/private), DMs, group DMs
- Send, receive, edit, delete messages
- Rich text rendering (bold, italic, strikethrough, code, code blocks, links, lists, blockquotes)
- Threaded conversations (side panel, Slack-style)
- Emoji reactions (add/remove/view)
- File uploads and downloads
- Inline image rendering (Kitty > Sixel > external viewer fallback)
- User presence (online/away/DND)
- Search (messages, files, channels)
- Desktop notifications via OS notification APIs
- OSC 52 clipboard integration
- Multi-workspace with workspace switcher

---

## Architecture

Service-oriented layered architecture with four distinct layers:

```
+-----------------------------------------------------------+
|                    UI Layer (bubbletea)                    |
|  +----------+-----------+------------+---------------+    |
|  |Workspace | Channel   | Message    | Thread        |    |
|  |Rail      | Sidebar   | Pane       | Panel         |    |
|  +----------+-----------+------------+---------------+    |
|  | CommandBar | SearchOverlay | NotificationToast     |    |
|  +-----------+---------------+-----------------------+    |
+-----------------------------------------------------------+
|                   Service Layer                           |
|  +-----------------+  +--------------+  +-----------+     |
|  |WorkspaceManager |  |MessageService|  |SearchSvc  |     |
|  | - connections[] |  | - send/recv  |  | - query   |     |
|  | - active switch |  | - edit/del   |  | - index   |     |
|  | - health monitor|  | - reactions  |  |           |     |
|  +-----------------+  +--------------+  +-----------+     |
|  +-----------------+  +--------------+  +-----------+     |
|  |NotificationSvc  |  |FileSvc       |  |PresenceSvc|     |
|  | - OS notify     |  | - upload/dl  |  | - track   |     |
|  | - rules/filters |  | - image rend |  | - update  |     |
|  +-----------------+  +--------------+  +-----------+     |
+-----------------------------------------------------------+
|                   Client Layer                            |
|  +-------------------------+  +----------------------+    |
|  | SlackClient (per wksp)  |  | OAuthManager         |    |
|  | - Socket Mode WebSocket |  | - token exchange     |    |
|  | - Web API calls         |  | - token refresh      |    |
|  | - rate limiting         |  | - secure storage     |    |
|  +-------------------------+  +----------------------+    |
+-----------------------------------------------------------+
|                   Data Layer                              |
|  +-------------------------+  +----------------------+    |
|  | SQLite (cache)          |  | Config (TOML)        |    |
|  | - messages              |  | - keybindings        |    |
|  | - channels              |  | - appearance         |    |
|  | - users                 |  | - workspace creds    |    |
|  | - files metadata        |  | - notification rules |    |
|  +-------------------------+  +----------------------+    |
+-----------------------------------------------------------+
```

### Key Design Decisions

- **One `SlackClient` per workspace.** Each maintains its own Socket Mode WebSocket connection and handles rate limiting independently. The `WorkspaceManager` orchestrates them.
- **Services are interfaces.** This enables mocking for tests and allows swapping implementations (e.g., a mock Slack client for development without a real workspace).
- **UI components receive read-only state snapshots.** Services own the mutable state. UI dispatches actions (as `tea.Msg`), services process them and push state updates back as `tea.Msg` via subscriptions.
- **SQLite is a cache, not source of truth.** Slack's API is the source of truth. SQLite enables fast startup, offline browsing of recent history, and efficient search.

### Module Structure

```
slack-tui/
  cmd/slack-tui/       # main entry point
  internal/
    ui/                # bubbletea models & views
      app.go           # root model, layout orchestration
      workspace/       # workspace rail component
      sidebar/         # channel list component
      messages/        # message pane component
      thread/          # thread panel component
      compose/         # message composition component
      search/          # search overlay
      command/         # command bar (:commands)
      styles/          # lipgloss style definitions
    service/           # business logic services
    slack/             # Slack API client wrapper
    cache/             # SQLite cache layer
    config/            # configuration management
    notify/            # OS notification abstraction
    image/             # terminal image rendering
    clipboard/         # OSC 52 clipboard
  docs/
  go.mod
```

---

## UI Layout & Interaction Model

### Panel Layout

```
+--+----------+--------------------------+---------------+
|  |          | # general        @ topic |  Thread x     |
|W1| Channels | -------------------------| ------------- |
|  | # general|                          |               |
|W2| # eng    |  alice  10:32 AM         |  carol:       |
|  | # random |  Hey team, the deploy    |  API changes? |
|W3|          |  looks good              |               |
|  | DMs      |                          |  > alice:     |
|  | * alice  |  bob    10:35 AM         |  > Handled    |
|  | o bob    |  Nice work!              |               |
|  |          |                          |  > bob:       |
|  |          |  carol  10:41 AM         |  > +1         |
|  |          |  What about the API      |               |
|  |          |  changes? [4 replies ->] |  [Reply...]   |
|  |          |                          |               |
|  |          | [Message #general    ]   |               |
+--+----------+--------------------------+---------------+
| NORMAL | #general | W1: Acme Corp        3 unread    * |
+---------------------------------------------------------+
```

### Panel Behavior

- **Workspace Rail (leftmost):** Always visible. Shows workspace icons/initials. Unread indicators. `1-9` or `gt`+number to switch.
- **Channel Sidebar:** Collapsible with `Ctrl+b`. Sections: Starred, Channels, DMs. Unread counts and bold for unread channels. Fuzzy filter with `/` while focused.
- **Message Pane:** Primary content area. Takes remaining width. Shows messages with timestamps, reactions, thread reply counts.
- **Thread Panel:** Opens to the right when entering a thread. Collapsible with `Ctrl+]`. Takes ~35% of the message pane width when open.
- **Status Bar (bottom):** Current mode (NORMAL/INSERT/COMMAND), active channel, workspace name, unread count, presence indicator.

### Focus Model

One panel has focus at a time. The focused panel receives keyboard input. Visual border highlight indicates focus.

- `Tab` / `Shift+Tab` -- cycle focus between panels
- `h/l` -- move focus left/right between adjacent panels (when not in a text input)
- `Ctrl+b` -- toggle sidebar
- `Ctrl+]` -- toggle thread panel

### Responsive Behavior

Terminal too narrow for three-panel layout: thread panel auto-hides, then sidebar auto-hides. Minimum viable width is just the message pane.

### Vim Modes

| Mode | Behavior |
|------|----------|
| NORMAL | Navigate messages, channels, panels. hjkl movement. Keybinds active. |
| INSERT | Typing in the compose box or thread reply. `i` to enter, `Esc` to exit. |
| COMMAND | `:` prefix commands (`:quit`, `:search`, `:workspace`). `Esc` to cancel. |
| SEARCH | `/` activates search within the focused panel. `Enter` to execute, `Esc` to cancel. |

### Key Bindings

| Key | Context | Action |
|-----|---------|--------|
| `j/k` | Channel sidebar | Move selection up/down |
| `j/k` | Message pane | Scroll through messages |
| `j/k` | Thread panel | Scroll through replies |
| `h/l` | NORMAL, not in text input | Move focus left/right between panels |
| `Ctrl+t` / `Ctrl+p` | Global | Open fuzzy finder for all channels + DMs across workspaces |
| `Tab` / `Shift+Tab` | Global | Cycle focus between panels |
| `Ctrl+b` | Global | Toggle sidebar |
| `Ctrl+]` | Global | Toggle thread panel |
| `i` | NORMAL | Enter INSERT mode (focus compose box) |
| `Esc` | INSERT/COMMAND/SEARCH | Return to NORMAL |
| `:` | NORMAL | Enter COMMAND mode |
| `/` | NORMAL, sidebar focused | Filter channels in sidebar |
| `/` | NORMAL, messages focused | Search messages |
| `Enter` | Message selected | Open thread |
| `r` | Message selected | Add reaction |
| `e` | Own message selected | Edit message |
| `dd` | Own message selected | Delete message (with confirmation) |
| `yy` | Message selected | Yank message text to clipboard (OSC 52) |
| `gg` / `G` | Message/channel list | Jump to top/bottom |
| `1-9` | Workspace rail | Switch to workspace N |

### Animations

All animations are configurable, with a global `animations.enabled` master switch and individual toggles per animation type.

- Smooth scrolling when navigating messages
- Typing indicator animation (pulsing dots)
- Loading spinner when fetching history
- Toast notifications slide in from top-right, auto-dismiss after 3 seconds
- Fade-in for new messages arriving in real-time

---

## Slack API Integration

### Authentication

OAuth 2.0 flow with PKCE:

1. Start a temporary local HTTP server on a random port
2. Open the browser to Slack's OAuth authorization URL
3. Receive the callback with the auth code
4. Exchange for access token + refresh token
5. Store tokens encrypted at rest in the XDG data directory

Token refresh is handled automatically by the `OAuthManager`. If a Web API call returns a token expiry error, it refreshes transparently and retries.

### Required Slack App Scopes (User Token)

- `channels:read`, `channels:history` -- browse and read channels
- `groups:read`, `groups:history` -- private channels
- `im:read`, `im:history`, `im:write` -- direct messages
- `mpim:read`, `mpim:history`, `mpim:write` -- group DMs
- `chat:write` -- send messages
- `reactions:read`, `reactions:write` -- emoji reactions
- `files:read`, `files:write` -- file upload/download
- `users:read` -- user profiles and presence
- `search:read` -- search messages and files
- `team:read` -- workspace info

### Socket Mode

Each workspace maintains a persistent WebSocket connection via Socket Mode. Subscribed events:

- `message` -- new messages, edits, deletions
- `reaction_added` / `reaction_removed`
- `channel_created` / `channel_renamed` / `channel_deleted`
- `member_joined_channel` / `member_left_channel`
- `user_typing`
- `presence_change`
- `file_shared`

### Rate Limiting

The `SlackClient` implements:

- Per-method rate limit tracking via `Retry-After` headers
- Request queuing with priority (user-initiated actions > background sync)
- Exponential backoff on 429 responses

### Reconnection

- Automatic reconnection with exponential backoff (1s, 2s, 4s, 8s, max 30s)
- Connection health monitoring via ping/pong
- Graceful degradation: cached data remains browsable, status bar shows "reconnecting..." indicator
- Message send queue: messages composed while disconnected are queued and sent on reconnect

---

## Data Layer

### SQLite Schema (Cache)

```sql
workspaces (
  id TEXT PRIMARY KEY,
  name TEXT,
  domain TEXT,
  icon_url TEXT,
  last_synced_at INTEGER
)

users (
  id TEXT PRIMARY KEY,
  workspace_id TEXT FK,
  name TEXT,
  display_name TEXT,
  avatar_url TEXT,
  presence TEXT,          -- active/away/dnd
  updated_at INTEGER
)

channels (
  id TEXT PRIMARY KEY,
  workspace_id TEXT FK,
  name TEXT,
  type TEXT,              -- channel/dm/group_dm/private
  topic TEXT,
  is_member BOOLEAN,
  is_starred BOOLEAN,
  last_read_ts TEXT,
  unread_count INTEGER,
  updated_at INTEGER
)

messages (
  ts TEXT,                -- Slack's timestamp (unique per channel)
  channel_id TEXT FK,
  workspace_id TEXT FK,
  user_id TEXT FK,
  text TEXT,
  thread_ts TEXT,         -- NULL if top-level, parent ts if reply
  reply_count INTEGER,
  edited_at TEXT,
  is_deleted BOOLEAN,
  raw_json TEXT,          -- full Slack payload for rich rendering
  created_at INTEGER,
  PRIMARY KEY (ts, channel_id)
)

reactions (
  message_ts TEXT FK,
  channel_id TEXT FK,
  emoji TEXT,
  user_ids TEXT,          -- JSON array of user IDs
  count INTEGER,
  PRIMARY KEY (message_ts, channel_id, emoji)
)

files (
  id TEXT PRIMARY KEY,
  message_ts TEXT FK,
  channel_id TEXT FK,
  name TEXT,
  mimetype TEXT,
  size INTEGER,
  url_private TEXT,
  local_path TEXT,        -- path if downloaded/cached locally
  thumbnail_path TEXT
)
```

### Cache Strategy

- **On connect:** Fetch channel list, user list, and last 50 messages per channel the user is a member of. Store in SQLite.
- **Real-time:** Socket Mode events update the cache incrementally (new messages, edits, reactions, presence changes).
- **History loading:** When scrolling up past cached messages, fetch older history from the API and append to cache.
- **Eviction:** Messages older than 30 days (configurable) are pruned periodically. Starred/pinned messages are exempt.
- **Startup:** Show cached data immediately while syncing with the API in the background. A subtle indicator shows sync progress.

### Sync & Reconciliation

Slack does not have a delta sync API. The sync strategy accounts for gaps when the app is offline:

- **While connected:** Socket Mode events keep the cache perfectly in sync (new messages, edits, deletions, reactions).
- **On reconnect after a gap:** Immediately re-fetch recent history for the active channel and reconcile against the cache (insert new, update edited, remove deleted). Background-sync other channels, prioritized by unread count and recent activity.
- **Deleted message detection:** If a cached message does not appear in the re-fetched API response for the same time range, mark it as deleted locally.
- **Stale history tradeoff:** Very old edits or deletions (beyond the re-fetch window) are not detected until the user scrolls back to that point, at which time history is fetched on demand and reconciled. This matches the behavior of the official Slack client.

### Configuration (TOML)

Stored at `~/.config/slack-tui/config.toml`:

```toml
[general]
default_workspace = "acme"

[appearance]
theme = "dark"              # dark | light | custom
timestamp_format = "3:04 PM"
show_avatars = false

[animations]
enabled = true
smooth_scrolling = true
typing_indicators = true
toast_transitions = true
message_fade_in = true

[notifications]
enabled = true
on_mention = true
on_dm = true
on_keyword = ["deploy", "incident"]
quiet_hours = "22:00-08:00"

[keybindings]
# Override defaults
# channel_fuzzy = "ctrl+t"

[cache]
message_retention_days = 30
max_db_size_mb = 500
```

---

## Image Rendering & File Handling

### Image Protocol Detection

On startup, detect terminal capabilities:

1. Check `TERM_PROGRAM` env var (identifies Kitty, iTerm2, WezTerm, etc.)
2. Send terminal query sequences to detect Kitty graphics protocol support
3. Send DECSIXEL query for Sixel support
4. Fall back to text placeholder if nothing is supported

### Rendering Tiers

| Tier | Protocol | Terminals | Behavior |
|------|----------|-----------|----------|
| 1 | Kitty graphics | Kitty, Ghostty | Full inline image rendering, resized to fit message pane. |
| 2 | Sixel | foot, WezTerm, mlterm | Inline rendering. Quality varies by terminal. |
| 3 | Fallback | Alacritty, others | File metadata + colored placeholder. `Enter` opens in system viewer. |

### Image Display in Messages

- Images render inline within the message flow, constrained to a max height (configurable, default ~10 terminal rows).
- Caption below shows filename and dimensions.
- `Enter` on a focused image opens it full-size in the system default viewer.
- `yy` on a focused image copies the Slack URL to clipboard.
- Images are downloaded and cached locally in `~/.cache/slack-tui/images/`. Cache evicted by LRU when exceeding configured size.

### File Handling (Non-Image)

- Files show as a styled block with icon (text representation), filename, size, and type.
- `Enter` downloads (if not cached) and opens with `xdg-open` / `open`.
- `d` downloads to a configured download directory (default `~/Downloads`).
- Upload via `:upload <path>` command in COMMAND mode.

---

## Notifications

### Desktop Notifications

OS-level notifications via a platform abstraction layer:

| Platform | Method |
|----------|--------|
| Linux | `notify-send` (libnotify) via exec, with fallback to D-Bus direct |
| macOS | `osascript` to trigger `display notification` |

### Notification Rules

- **Mentions:** @-mentions, @-here, @-channel in channels you're in
- **DMs:** Any new direct message or group DM message
- **Keywords:** Configurable keyword list (e.g., `["deploy", "incident"]`)
- **Quiet hours:** Suppress notifications during configured time window
- **Per-channel mute:** Mute specific channels (mirrors Slack's mute setting)

### Notification Content

- Title: `Workspace: #channel` or `Workspace: username`
- Body: First ~100 characters of the message
- Configurable "no sensitive content" mode: title only, body shows "New message"

### Focus Suppression

If the TUI is in the foreground and the relevant channel is active (visible), desktop notifications are suppressed. If the TUI is in the foreground but a different channel is active, notifications still fire.

### In-TUI Indicators

Always on, independent of animation settings:

- Unread count badges on channels in the sidebar
- Bold channel name for unread channels
- Workspace rail dot indicator for workspaces with unread activity
- Status bar total unread count

---

## Search

### Local Filter (`/` in Sidebar)

Fuzzy-match filter on the channel/DM list. Instant, client-side, filters as you type. `Esc` clears the filter.

### Slack Search (`:search <query>` or `Ctrl+/`)

Full Slack API search using `search.messages` and `search.files`. Opens a search results overlay:

```
+-------------------------------------------+
| Search: deploy incident                   |
| ----------------------------------------- |
| Messages (23)  |  Files (4)              |
| ----------------------------------------- |
| > #ops  alice  2h ago                     |
|   Deploy incident resolved, root cause    |
|   was the config change in...             |
|                                           |
|   #eng  bob    yesterday                  |
|   Post-incident: deploy pipeline fix      |
|                                           |
|   @carol (DM)  3 days ago                 |
|   Can you check the deploy logs?          |
|                                           |
| j/k navigate  Enter: jump to message     |
| Tab: switch Messages/Files  Esc: close    |
+-------------------------------------------+
```

- Results show channel, author, relative time, snippet with highlighted matches
- `Enter` navigates to the message in context
- `Tab` switches between Messages and Files tabs
- Supports Slack query modifiers (`from:`, `in:#`, `before:`, etc.)

### Global Fuzzy Finder (`Ctrl+t` / `Ctrl+p`)

Fast channel/DM switcher overlay:

```
+-----------------------------------------+
| > gen_                                  |
| --------------------------------------- |
| > W1: #general           142 members   |
|   W2: #general-ops        28 members   |
|   W1: @gene (DM)             online    |
|                                         |
| j/k navigate  Enter: switch            |
+-----------------------------------------+
```

- Searches across all workspaces
- Fuzzy matches on channel name, workspace name, user display name
- Results ranked by frequency of use
- Client-side only, operates on cached data

---

## Error Handling & Resilience

### Connection Errors

- **WebSocket disconnect:** Status bar shows `DISCONNECTED` indicator. Automatic reconnection with exponential backoff (1s, 2s, 4s, 8s, max 30s). Cached data remains browsable. Compose box shows "offline -- message will be sent when reconnected" and queues the message.
- **Auth token expired:** Automatic refresh via `OAuthManager`. If refresh fails, toast prompts re-authentication. Other workspaces remain functional.
- **Rate limited:** Requests delayed per `Retry-After` header. User-initiated actions get priority. Subtle indicator when rate-limited.

### Message Send Failures

- Failed sends show a red error indicator next to the message with option to retry (`r`) or discard (`dd`).
- Messages kept in local send queue until confirmed by the server.

### Startup Errors

- Corrupted SQLite: recreate database and log warning.
- No workspaces configured: launch directly into OAuth add-workspace flow.
- Workspace connection failure: other workspaces still connect. Failed workspace shows error state in workspace rail.

### Graceful Degradation

- Image rendering failure falls back to next tier silently.
- Missing `notify-send` / `osascript`: desktop notifications disabled with one-time warning.
- Terminal too narrow: thread panel auto-hides, then sidebar auto-hides.

---

## Testing Strategy

- **Unit tests:** Each service layer component tested in isolation with mock interfaces.
- **UI component tests:** bubbletea models tested via `teatest` -- send key sequences, assert on rendered output.
- **Integration tests:** Mock Slack server (local HTTP + WebSocket) simulating Socket Mode events. Full-flow tests: connect, receive, send, edit, delete, react.
- **Cache tests:** SQLite layer tested with in-memory databases. Verify sync, eviction, and reconciliation.

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | Styling and layout |
| `github.com/charmbracelet/bubbles` | Standard components (text input, viewport, list, spinner) |
| `github.com/slack-go/slack` | Slack API client (Web API + Socket Mode) |
| `modernc.org/sqlite` | Pure Go SQLite driver (no CGo, cross-compiles) |
| `github.com/pelletier/go-toml/v2` | TOML config parsing |
| `github.com/sahilm/fuzzy` | Fuzzy matching for channel/DM finder |
| `github.com/gen2brain/beeep` | Cross-platform desktop notifications |

## Build

- Go 1.22+
- `go build ./cmd/slack-tui`
- Single binary, no runtime dependencies beyond the terminal
- Makefile with targets: `build`, `test`, `lint`, `run`
