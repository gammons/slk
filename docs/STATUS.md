# slack-tui Implementation Status

Last updated: 2026-04-26

## What's Working

### Core
- [x] Project scaffolding (Go modules, Makefile, build)
- [x] TOML configuration with defaults and XDG paths
- [x] SQLite cache layer (messages, channels, users, workspaces)
- [x] Slack API client (Web API via slack-go)
- [x] OAuth token storage (JSON files, per-workspace)
- [x] Interactive onboarding (`--add-workspace` with huh forms)
- [x] Multi-workspace support in data layer (single workspace connected at runtime)
- [x] Browser cookie auth (xoxc/xoxd) -- connect using browser session tokens, no Slack App needed
- [x] Real-time WebSocket events -- direct connection using Slack's browser protocol (not RTM or Socket Mode)

### UI
- [x] Three-panel layout: workspace rail, channel sidebar, message pane
- [x] Vim-inspired modal editing (NORMAL, INSERT, COMMAND modes)
- [x] j/k navigation in channel list and messages
- [x] h/l and Tab to switch focus between panels
- [x] Ctrl+b to toggle sidebar
- [x] Channel sidebar with sections, scrolling, and green left-border selection
- [x] Message pane with viewport scrolling (bubbles/viewport)
- [x] Multi-line message compose (bubbles/textarea, Shift+Enter for newline)
- [x] Compose box with thick left border and dark background
- [x] Status bar showing current mode, channel, workspace, connection state
- [x] Three-state connection indicator (green Connected / yellow Connecting / red Disconnected)
- [x] Thick border on focused panel, rounded border on unfocused
- [x] Insert mode indicated by compose box highlight only (panel borders stay gray)
- [x] Lipgloss styling throughout with dark theme
- [x] Ctrl+t/Ctrl+p fuzzy channel finder overlay with blue input border
- [x] Green left-border selection indicator (messages, threads, channels, channel finder)
- [x] Workspace rail -- borderless dark background strip

### Messages
- [x] Fetch and display channel messages
- [x] Load messages on channel switch
- [x] Infinite scroll -- load older messages when scrolling to top
- [x] Day separators (Today, Yesterday, Monday, full date)
- [x] Slack markdown rendering (bold, italic, strikethrough, code, code blocks, blockquotes)
- [x] Emoji shortcode rendering (:emoji: -> actual emoji)
- [x] Link rendering (Slack's `<url|label>` format)
- [x] User and channel mention rendering (resolved to display names)
- [x] Thread reply count indicators
- [x] Edited message indicators
- [x] Message sending via Slack API
- [x] Real-time incoming messages via WebSocket (auto-scroll, cached to SQLite)
- [x] Render cache for scroll performance
- [x] ANSI-aware text wrapping (muesli/reflow/wordwrap)
- [x] ANSI-safe string truncation (muesli/reflow/truncate)
- [x] Spacing between messages (margin below each)

### Threads
- [x] Thread panel -- side panel (35% width) for viewing and replying to threads
- [x] Enter on message opens thread, Escape closes
- [x] Ctrl+] toggles thread panel
- [x] Thread replies with green left-border selection
- [x] Thread reply compose with Shift+Enter for newlines
- [x] Real-time thread reply routing via WebSocket
- [x] Thread reply sending via Slack API
- [x] Channel switch closes thread panel

### Users & Avatars
- [x] User display name resolution (Profile.DisplayName > RealName > Name)
- [x] DM channels show user names instead of IDs
- [x] Half-block pixel art avatars (downloaded, cached, rendered as Unicode art)

### Channels
- [x] Public channels (# prefix)
- [x] Private channels (◆ prefix)
- [x] DMs with presence indicators (● online, ○ offline)
- [x] Group DMs
- [x] Config-based channel sections with glob pattern matching
- [x] Channel name truncation for long names
- [x] Sidebar scrolling with selected item always visible
- [x] Unread channel indicators (blue dot + bold text)
- [x] Unread counts fetched via Slack's client.counts API

## Not Yet Implemented

### Medium Priority
- [ ] Reaction picker (press `r` to add emoji reaction)
- [ ] Message editing (`e` on own message)
- [ ] Message deletion (`dd` on own message)
- [ ] Search (`:search <query>` or `Ctrl+/`)
- [ ] File uploads and downloads
- [ ] Desktop notifications (OS-level via notify-send/osascript)
- [ ] User presence tracking (online/away/DND updates)
- [ ] Inline image rendering (Kitty graphics > Sixel > fallback)
- [ ] OSC 52 clipboard integration (yank message text)

### Low Priority
- [ ] Multi-workspace switching at runtime (workspace rail click)
- [ ] Typing indicators
- [ ] Quiet hours for notifications
- [ ] Custom keybinding overrides in config
- [ ] Message link previews / unfurling
- [ ] Custom themes (light mode, custom colors)

## Architecture Overview

```
slack-tui/
├── cmd/slack-tui/
│   ├── main.go              # Entry point, dependency wiring
│   └── onboarding.go        # --add-workspace interactive setup
├── internal/
│   ├── avatar/              # Download, cache, half-block render avatars
│   ├── cache/               # SQLite cache (6 tables, full CRUD)
│   ├── config/              # TOML config with defaults
│   ├── service/             # WorkspaceManager, MessageService
│   ├── slack/               # Slack API client, token storage, WebSocket events
│   └── ui/
│       ├── app.go           # Root bubbletea model, layout, focus management
│       ├── mode.go          # Vim mode enum (NORMAL, INSERT, COMMAND, FIND)
│       ├── keys.go          # Key binding definitions
│       ├── styles/          # Lipgloss style definitions
│       ├── workspace/       # Workspace rail component
│       ├── sidebar/         # Channel sidebar with sections + scrolling
│       ├── messages/        # Message pane with viewport + markdown rendering
│       ├── thread/          # Thread panel with viewport + reply compose
│       ├── channelfinder/   # Ctrl+t/Ctrl+p fuzzy channel finder overlay
│       ├── compose/         # Multi-line message input (textarea)
│       └── statusbar/       # Bottom status bar with connection state
├── docs/
│   ├── STATUS.md            # This file
│   └── superpowers/
│       ├── specs/           # Design specifications
│       └── plans/           # Implementation plans
├── Makefile
└── go.mod
```

## Stats

- 26 source files, 19 test files
- ~6,600 lines of Go
- 13 packages, all tests passing
- Single binary, no runtime dependencies beyond the terminal

## Key Design Decisions

1. **Service-oriented layers** -- UI, Service, Client, Data layers with clear interfaces
2. **SQLite as cache, not source of truth** -- Slack API is authoritative, SQLite enables fast startup
3. **Render caching** -- messages rendered once, cached until content changes
4. **bubbles/viewport scrolling** -- all scrollable panels use bubbles/viewport with item-level selection
5. **Direct WebSocket** -- connects to Slack's internal browser WebSocket protocol (not RTM or Socket Mode) for real-time events with xoxc tokens
6. **Config-based channel sections** -- undocumented Slack API for sections requires xoxc tokens; config-based approach is reliable and user-controllable
7. **muesli/reflow** -- ANSI-aware text wrapping, padding, and truncation for correct rendering with styled text
8. **Green left-border selection** -- consistent `▌` indicator across messages, threads, channels, and channel finder
9. **Thick left-border compose** -- compose boxes use `▌` border with dark background, matching opencode's input style
