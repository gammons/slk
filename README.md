# slk

A terminal-based Slack client built in Go using [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss). Designed as a keyboard-driven daily-driver replacement for the official Slack desktop client.

## Features

- **Three-panel layout** -- workspace rail, channel sidebar, message pane
- **Vim-inspired navigation** -- `j/k` to scroll, `h/l` to switch panels, `i` for insert mode, `Esc` to return to normal mode
- **Slack markdown rendering** -- bold, italic, strikethrough, code blocks, links, mentions
- **Emoji shortcodes** -- `:rocket:` renders as the actual emoji
- **Half-block pixel art avatars** -- tiny user avatars rendered next to messages using Unicode half-block characters
- **Day separators** -- messages grouped by date (Today, Yesterday, Monday, etc.)
- **Infinite scroll** -- scroll up to load older message history
- **Message sending** -- compose and send messages in insert mode
- **Channel sections** -- organize channels into custom groups via config
- **Private channel indicators** -- visual distinction between public, private, and DM channels
- **Configurable** -- TOML config for appearance, animations, notifications, keybindings, and channel sections

## Prerequisites

- Go 1.22+
- A Slack workspace (log in via browser to get auth tokens)

## Build

```bash
make build
```

The binary is output to `bin/slk`.

## Setup

### 1. Log Into Slack in Your Browser

Open [https://app.slack.com](https://app.slack.com) and log into your workspace.

### 2. Get Your Browser Tokens

**Get the `d` cookie:**
- Open DevTools (F12 or Cmd+Option+I)
- Go to Application > Cookies > `https://app.slack.com`
- Find the cookie named `d` and copy its value

**Get the `xoxc` token:**
- Go to the Console tab in DevTools and run:
  ```javascript
  Object.entries(JSON.parse(localStorage.localConfig_v2).teams).forEach(([id,t]) => console.log(t.name, t.token))
  ```
- This prints the name and token for each workspace. Copy the `xoxc-...` token for the workspace you want to add.

### 3. Add Workspace

```bash
./bin/slk --add-workspace
```

This launches an interactive onboarding that prompts for your `xoxc` token and `d` cookie.

Alternatively, just run `./bin/slk` -- it will launch onboarding automatically if no workspaces are configured.

## Usage

### Key Bindings

| Key | Mode | Action |
|-----|------|--------|
| `j` / `k` | Normal | Move up/down in channel list or messages |
| `h` / `l` | Normal | Switch focus between panels |
| `Tab` / `Shift+Tab` | Normal | Cycle focus between panels |
| `Enter` | Normal (sidebar) | Open selected channel |
| `i` | Normal | Enter insert mode (compose message) |
| `Enter` | Insert | Send message |
| `Esc` | Insert/Command | Return to normal mode |
| `Ctrl+b` | Any | Toggle sidebar |
| `gg` / `G` | Normal | Jump to top/bottom |
| `Ctrl+y` | Normal | Switch theme |
| `Ctrl+c` | Any | Quit |

### Configuration

Config file: `~/.config/slk/config.toml`

```toml
[general]
default_workspace = "myteam"

[appearance]
theme = "dark"
timestamp_format = "3:04 PM"

[animations]
enabled = true
smooth_scrolling = true
typing_indicators = true

[notifications]
enabled = true
on_mention = true
on_dm = true
on_keyword = ["deploy", "incident"]
quiet_hours = "22:00-08:00"

[cache]
message_retention_days = 30
max_db_size_mb = 500

# Custom channel sections
[sections.Alerts]
channels = ["alerts", "ops", "*-alerts"]
order = 1

[sections.Engineering]
channels = ["eng-*", "deploys", "bugs"]
order = 2

# Custom theme colors (override active theme)
[theme]
primary = "#4A9EFF"
accent = "#50C878"
background = "#1A1A2E"
text = "#E0E0E0"
```

### Custom Themes

Place `.toml` theme files in `~/.config/slk/themes/`:

```toml
name = "My Theme"

[colors]
primary = "#BD93F9"
accent = "#50FA7B"
warning = "#FFB86C"
error = "#FF5555"
background = "#282A36"
surface = "#343746"
surface_dark = "#21222C"
text = "#F8F8F2"
text_muted = "#6272A4"
border = "#44475A"
```

Built-in themes: Dark, Light, Dracula, Solarized Dark, Solarized Light, Gruvbox Dark, Gruvbox Light, Nord, Tokyo Night, Catppuccin Mocha, One Dark, Rosé Pine.

### Data Storage

Follows the XDG Base Directory specification:

| Path | Contents |
|------|----------|
| `~/.config/slk/` | Configuration (config.toml) |
| `~/.local/share/slk/` | Data (SQLite cache, tokens) |
| `~/.cache/slk/` | Cache (avatars, images) |

## Architecture

Service-oriented layered architecture:

```
UI Layer (bubbletea)     -- workspace rail, sidebar, messages, compose, status bar
Service Layer            -- WorkspaceManager, MessageService
Client Layer             -- Slack API wrapper (Socket Mode + Web API)
Data Layer               -- SQLite cache, TOML config
```

See [design spec](docs/superpowers/specs/2026-04-23-slk-design.md) for full architecture details.

## License

TBD
