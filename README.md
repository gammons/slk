# slack-tui

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

The binary is output to `bin/slack-tui`.

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
  JSON.parse(localStorage.localConfig_v2).teams[Object.keys(JSON.parse(localStorage.localConfig_v2).teams)[0]].token
  ```
- Copy the `xoxc-...` token

### 3. Add Workspace

```bash
./bin/slack-tui --add-workspace
```

This launches an interactive onboarding that prompts for your `xoxc` token and `d` cookie.

Alternatively, just run `./bin/slack-tui` -- it will launch onboarding automatically if no workspaces are configured.

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
| `Ctrl+c` | Any | Quit |

### Configuration

Config file: `~/.config/slack-tui/config.toml`

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
```

### Data Storage

Follows the XDG Base Directory specification:

| Path | Contents |
|------|----------|
| `~/.config/slack-tui/` | Configuration (config.toml) |
| `~/.local/share/slack-tui/` | Data (SQLite cache, tokens) |
| `~/.cache/slack-tui/` | Cache (avatars, images) |

## Architecture

Service-oriented layered architecture:

```
UI Layer (bubbletea)     -- workspace rail, sidebar, messages, compose, status bar
Service Layer            -- WorkspaceManager, MessageService
Client Layer             -- Slack API wrapper (Socket Mode + Web API)
Data Layer               -- SQLite cache, TOML config
```

See [design spec](docs/superpowers/specs/2026-04-23-slack-tui-design.md) for full architecture details.

## License

TBD
