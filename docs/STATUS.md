# slk Implementation Status

Last updated: 2026-04-28

## What's Working

### Core
- [x] Project scaffolding (Go modules, Makefile, build)
- [x] TOML configuration with defaults and XDG paths
- [x] SQLite cache layer (messages, channels, users, workspaces, reactions, frecent emoji)
- [x] Slack API client (Web API via slack-go)
- [x] OAuth token storage (JSON files, per-workspace)
- [x] Interactive onboarding (`--add-workspace` with huh forms)
- [x] Multi-workspace runtime switching (1-9 number keys + Ctrl+w picker)
- [x] All workspaces maintain live WebSocket connections with real-time unread badges
- [x] Parallel workspace connection at startup with loading overlay
- [x] Browser cookie auth (xoxc/xoxd) -- connect using browser session tokens, no Slack App needed
- [x] Real-time WebSocket events -- direct connection using Slack's browser protocol (not RTM or Socket Mode)
- [x] Automatic WebSocket reconnection with exponential backoff (1s-30s)

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
- [x] Customizable themes (12 built-in themes, custom theme files, Ctrl+y theme switcher)

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
- [x] @mention autocomplete in compose (inline picker, translates to <@UserID> on send)
- [x] Real-time incoming messages via WebSocket (auto-scroll, cached to SQLite)
- [x] Render cache for scroll performance
- [x] ANSI-aware text wrapping (muesli/reflow/wordwrap)
- [x] ANSI-safe string truncation (muesli/reflow/truncate)
- [x] Spacing between messages (margin below each)
- [x] New message landmark (red "── new ──" separator marking unread boundary)
- [x] Mark-as-read synced to Slack via conversations.mark API on channel entry
- [x] Typing indicators (show who's typing, broadcast your own typing)
- [x] Desktop notifications (mentions, DMs, keywords via beeep)

### Threads
- [x] Thread panel -- side panel (35% width) for viewing and replying to threads
- [x] Enter on message opens thread, Escape closes
- [x] Ctrl+] toggles thread panel
- [x] Thread replies with green left-border selection
- [x] Thread reply compose with Shift+Enter for newlines
- [x] Real-time thread reply routing via WebSocket
- [x] Thread reply sending via Slack API
- [x] Channel switch closes thread panel
- [x] Threads view (top-of-sidebar `⚑ Threads` entry): list of threads the
  user authored, replied to, or was @-mentioned in for the active workspace,
  unread first, live re-rank on new replies (cache-based v1)

### Users & Avatars
- [x] User display name resolution (Profile.DisplayName > RealName > Name)
- [x] DM channels show user names instead of IDs
- [x] Half-block pixel art avatars (downloaded, cached, rendered as Unicode art)

### Reactions
- [x] Reaction picker overlay (press `r` -- search-first with frecent emoji)
- [x] Quick-toggle reaction nav (press `R` -- h/l to navigate, Enter to toggle)
- [x] Pill-style reaction display (green = your reaction, gray = others)
- [x] Real-time reaction sync via WebSocket (deduped against optimistic updates)
- [x] Frecent emoji tracking (most-used emoji shown first)
- [x] Optimistic UI updates
- [x] Safe emoji rendering (single-codepoint Unicode displayed, multi-codepoint falls back to :name:)

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
- [ ] Message editing (`e` on own message)
- [ ] Message deletion (`dd` on own message)
- [ ] Search (`:search <query>` or `Ctrl+/`)
- [ ] File uploads and downloads
- [ ] User presence tracking (online/away/DND updates)
- [ ] Inline image rendering (Kitty graphics > Sixel > fallback)
- [ ] OSC 52 clipboard integration (yank message text)
### Low Priority

- [ ] Quiet hours for notifications
- [ ] Custom keybinding overrides in config
- [ ] Message link previews / unfurling

## Architecture Overview

```
slk/
├── cmd/slk/
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
│       ├── mode.go          # Vim mode enum (NORMAL, INSERT, COMMAND, FIND, REACT)
│       ├── keys.go          # Key binding definitions
│       ├── styles/          # Lipgloss style definitions
│       ├── workspace/       # Workspace rail component
│       ├── sidebar/         # Channel sidebar with sections + scrolling
│       ├── messages/        # Message pane with viewport + markdown rendering
│       ├── thread/          # Thread panel with viewport + reply compose
│       ├── channelfinder/   # Ctrl+t/Ctrl+p fuzzy channel finder overlay
│       ├── reactionpicker/  # Reaction picker overlay with emoji search
│       ├── workspacefinder/ # Ctrl+w workspace picker overlay
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

- 31 source files, 24 test files
- ~9,300 lines of Go
- 14 test packages, all tests passing
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
