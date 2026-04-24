# slack-tui Implementation Status

Last updated: 2026-04-24

## What's Working

### Core
- [x] Project scaffolding (Go modules, Makefile, build)
- [x] TOML configuration with defaults and XDG paths
- [x] SQLite cache layer (messages, channels, users, workspaces)
- [x] Slack API client (Socket Mode + Web API via slack-go)
- [x] OAuth token storage (JSON files, per-workspace)
- [x] Interactive onboarding (`--add-workspace` with huh forms)
- [x] Multi-workspace support in data layer (single workspace connected at runtime)

### UI
- [x] Three-panel layout: workspace rail, channel sidebar, message pane
- [x] Vim-inspired modal editing (NORMAL, INSERT, COMMAND modes)
- [x] j/k navigation in channel list and messages
- [x] h/l and Tab to switch focus between panels
- [x] Ctrl+b to toggle sidebar
- [x] Channel sidebar with sections, scrolling, and selection cursor
- [x] Message pane with viewport scrolling (keeps selected message visible)
- [x] Message compose box with INSERT mode
- [x] Status bar showing current mode, channel, workspace
- [x] Bordered panels with focus highlighting (blue = focused, gray = unfocused)
- [x] Lipgloss styling throughout with dark theme

### Messages
- [x] Fetch and display channel messages
- [x] Load messages on channel switch
- [x] Infinite scroll -- load older messages when scrolling to top
- [x] Day separators (Today, Yesterday, Monday, full date)
- [x] Slack markdown rendering (bold, italic, strikethrough, code, code blocks, blockquotes)
- [x] Emoji shortcode rendering (:emoji: -> actual emoji)
- [x] Link rendering (Slack's `<url|label>` format)
- [x] User and channel mention rendering
- [x] Thread reply count indicators
- [x] Edited message indicators
- [x] Message sending via Slack API
- [x] Render cache for scroll performance

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

## Not Yet Implemented

### High Priority (next iteration)
- [ ] **Browser cookie auth (xoxc/xoxd)** -- allows any user to connect without admin permissions. Current Slack App flow requires workspace admin. RTM API via slack-go would replace Socket Mode.
- [ ] **Real-time event handling** -- Socket Mode connection runs but events aren't wired to the UI yet. New messages from other users don't appear until channel is re-selected.
- [ ] **Ctrl+t/Ctrl+p fuzzy channel finder** -- the floating overlay for quick channel switching
- [ ] **Thread panel** -- side panel for viewing and replying to threads (spec'd in design, not built)

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
- [ ] Unread count badges on channels
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
│   ├── slack/               # Slack API client, token storage, event handling
│   └── ui/
│       ├── app.go           # Root bubbletea model, layout, focus management
│       ├── mode.go          # Vim mode enum
│       ├── keys.go          # Key binding definitions
│       ├── styles/          # Lipgloss style definitions
│       ├── workspace/       # Workspace rail component
│       ├── sidebar/         # Channel sidebar with sections + scrolling
│       ├── messages/        # Message pane with viewport + markdown rendering
│       ├── compose/         # Message input box
│       └── statusbar/       # Bottom status bar
├── docs/
│   ├── STATUS.md            # This file
│   └── superpowers/
│       ├── specs/           # Design specification
│       └── plans/           # Implementation plan
├── Makefile
└── go.mod
```

## Stats

- 22 source files, 17 test files
- ~4,700 lines of Go
- 10 packages, all tests passing
- Single binary, no runtime dependencies beyond the terminal

## Key Design Decisions

1. **Service-oriented layers** -- UI, Service, Client, Data layers with clear interfaces
2. **SQLite as cache, not source of truth** -- Slack API is authoritative, SQLite enables fast startup
3. **Render caching** -- messages rendered once, cached until content changes
4. **Actual height measurement** -- lipgloss height summing is unreliable; viewport uses `lipgloss.Height(strings.Join(...))` on actual content
5. **Config-based channel sections** -- undocumented Slack API for sections requires xoxc tokens; config-based approach is reliable and user-controllable
