# slk

> **A blazingly fast Slack TUI.**
> Keyboard-driven, beautifully themed, and under 20MB. One static binary. No Electron required.

![slk screenshot](docs/assets/screenshot.png)

`slk` is a daily-driver replacement for the official Slack desktop client, built in Go with [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss). 

## Why slk?

- **Fast.** Cold start in milliseconds. Render-cached messages. SQLite-backed scrollback. Real-time over WebSocket.
- **Tiny.** ~19 MB on disk. ~60 MB RSS for a live multi-workspace session vs. 500 MB–1.5 GB for the official client. No node_modules, no Chromium, no 1Gb RAM tax.
- **Keyboard-first.** Vim-style modal editing. `j/k`, `h/l`, `i`, `Esc`
- **Pretty.** 12 built-in themes, lipgloss-styled panels, half-block pixel-art avatars, emoji shortcodes, day separators, and pill-style reactions.
- **Multi-workspace.** All your workspaces stay connected in parallel. `1`–`9` to instantly jump between them, with live unread badges in the rail.
- **Yours.** TOML config, custom themes, custom channel sections via glob, XDG-compliant paths.

## Features

### Messaging
- Real-time messages, edits, deletes, reactions, and typing indicators over WebSocket
- Slack markdown rendering (bold, italic, strikethrough, code, blockquotes, links, mentions)
- Emoji shortcodes (`:rocket:` → 🚀)
- Day separators (Today, Yesterday, Monday, full date)
- Infinite scroll backfill into SQLite cache
- New-message landmark (red `── new ──` line at the unread boundary)
- Mark-as-read synced to Slack on channel entry
- Edited / threaded message indicators
- ANSI-aware wrapping and truncation (no broken color codes mid-line)
- Drag-to-copy: drag the mouse across messages to highlight them; release to copy plain text to the system clipboard via OSC 52

### Compose
- Multi-line input, `Shift+Enter` for newlines
- Inline `@mention` autocomplete (resolves to `<@UserID>` on send)
- Special mentions: `@here`, `@channel`, `@everyone`
- Bracketed paste — paste multi-line text from the system clipboard without it being interpreted as keystrokes

### Threads
- Side panel (35% width), opened with `Enter`, toggled with `Ctrl+]`
- Live thread reply routing, real-time updates
- Auto-closes on channel switch or narrow terminals
- **Threads view** (`⚑ Threads` at top of sidebar): scrollable list of every
  thread you authored, replied to, or were @-mentioned in for the active
  workspace. Unread first, then newest activity. Selecting a thread opens
  it in the side panel; the list re-ranks live as new replies arrive.
  v1 is computed from the local SQLite cache, so threads from channels
  you have not yet opened in slk will not appear until they are seen.

### Reactions
- Search-first picker overlay (`r`) with frecent emoji
- Quick-toggle nav across existing pills (`R`, then `h/l/Enter`)
- Pill-style display (green = yours, gray = others)
- Optimistic UI, deduped against the WebSocket echo

### Channels & Workspaces
- Three-panel layout: workspace rail, channel sidebar, message pane
- Public (`#`), private (`◆`), DM (`●`/`○` for presence), and group DM channels
- Custom channel sections via glob patterns in config
- Unread dots and counts (via Slack's `client.counts` API)
- Fuzzy channel finder (`Ctrl+t` / `Ctrl+p`)
- Workspace picker (`Ctrl+w`) and direct jump (`1`–`9`)
- All workspaces stay connected in parallel for live unread badges

### Notifications
- OS-level desktop notifications via [beeep](https://github.com/gen2brain/beeep)
- Triggers on DMs, mentions, and configurable keywords
- Suppressed when you're focused on the relevant channel

### Connectivity
- Browser-cookie auth (`xoxc` + `d`) — works as any user, no Slack App required
- Direct connection to Slack's internal browser WebSocket protocol
- Auto-reconnect with exponential backoff (1s → 30s)
- Three-state connection indicator in the status bar

### Customization
- 12 built-in modern-looking themes
- Drop-in custom themes (`~/.config/slk/themes/*.toml`)
- Live theme switcher (`Ctrl+y`)
- TOML config for appearance, animations, notifications, and channel sections

## Tradeoffs & Non-Goals

slk is intentionally not a 1:1 port of the desktop client. Some Slack features are deferred or out of scope:

**On the roadmap:**
- Message editing (`e`) and deletion (`dd`)
- Slack-side search (`Ctrl+/` / `:search`)
- File uploads and downloads
- Inline image rendering (Kitty graphics → Sixel → fallback)
- OSC 52 clipboard yank (`yy`)
- Presence change events (online/away/DND)
- Quiet hours and per-channel mute
- Custom keybinding overrides

**Not planned:**
- Huddles, Slack Connect, Workflow Builder
- Bot/app management, slash commands, custom emoji management
- Animated reactions, link unfurls, in-app toasts

**Auth caveat:** browser-cookie auth means tokens expire when you log out of the browser or Slack rotates them. Re-run `--add-workspace` and you're back in business.

## Install

Grab a prebuilt binary from the [latest release](https://github.com/gammons/slk/releases/latest), or use one of the methods below.

The shell snippets resolve the latest version automatically:

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
```

### Linux

**Debian / Ubuntu:**
```bash
curl -fsSLO "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_amd64.deb"
sudo dpkg -i "slk_${VERSION}_linux_amd64.deb"
```

**Fedora / RHEL:**
```bash
sudo rpm -i "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_amd64.rpm"
```

**Alpine:**
```bash
curl -fsSLO "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_amd64.apk"
sudo apk add --allow-untrusted "slk_${VERSION}_linux_amd64.apk"
```

**Tarball (any distro, swap `x86_64` for `arm64` on ARM):**
```bash
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_x86_64.tar.gz" | tar xz
sudo mv slk /usr/local/bin/
```

### macOS

```bash
# Apple Silicon
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_darwin_arm64.tar.gz" | tar xz
# Intel
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_darwin_x86_64.tar.gz" | tar xz

sudo mv slk /usr/local/bin/
```

### Windows

Download the `windows_x86_64.zip` from the [latest release](https://github.com/gammons/slk/releases/latest), extract `slk.exe`, and add it to your `PATH`.

### Go

```bash
go install github.com/gammons/slk/cmd/slk@latest
```

### Build from source

Requires Go 1.22+.

```bash
git clone https://github.com/gammons/slk.git
cd slk
make build       # binary at bin/slk
```

### Verify your download

```bash
curl -fsSLO https://github.com/gammons/slk/releases/latest/download/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

## Setup

### 1. Log into Slack in your browser
Open [https://app.slack.com](https://app.slack.com) and sign into your workspace. Open the browser version of your slack workspace.

### 2. Grab your browser tokens

**The `d` cookie:**
- DevTools (F12 / Cmd+Option+I) → Application → Cookies → `https://app.slack.com`
- Copy the value of the cookie named `d`

**The `xoxc` token:** in the DevTools Console, run:
```javascript
Object.entries(JSON.parse(localStorage.localConfig_v2).teams).forEach(([id,t]) => console.log(t.name, t.token))
```
Copy the `xoxc-…` token for the workspace you want.

### 3. Add the workspace
```bash
./bin/slk --add-workspace
```
Or just run `./bin/slk`. Onboarding launches automatically when no workspaces are configured.

## Keybindings

| Key | Mode | Action |
|---|---|---|
| `j` / `k` | Normal | Move down/up in channel list or messages |
| `h` / `l` | Normal | Switch focus between panels |
| `Tab` / `Shift+Tab` | Normal | Cycle focus |
| `Enter` | Normal (sidebar) | Open selected channel |
| `Enter` | Normal (message) | Open thread |
| `i` | Normal | Enter insert mode |
| `Esc` | Insert / Command | Return to normal mode |
| `Enter` | Insert | Send message |
| `Shift+Enter` | Insert | Newline |
| `gg` / `G` | Normal | Jump to top / bottom |
| `Ctrl+b` | Any | Toggle sidebar |
| `Ctrl+]` | Any | Toggle thread panel |
| `Ctrl+t` / `Ctrl+p` | Any | Fuzzy channel finder |
| `Ctrl+w` | Any | Workspace picker |
| `1`–`9` | Normal | Jump to workspace N |
| `r` | Normal (message) | Open reaction picker |
| `R` | Normal (message) | Quick-toggle existing reactions |
| `Y` / `C` | Normal (message) | Copy message permalink |
| `Ctrl+y` | Any | Switch theme |
| `Ctrl+c` | Any | Quit |

## Configuration

Config lives at `~/.config/slk/config.toml`:

```toml
[general]
default_workspace = "myteam"

[appearance]
theme = "dracula"
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
quiet_hours = "22:00-08:00"   # planned

[cache]
message_retention_days = 30
max_db_size_mb = 500

# Custom channel sections (glob patterns)
[sections.Alerts]
channels = ["alerts", "ops", "*-alerts"]
order = 1

[sections.Engineering]
channels = ["eng-*", "deploys", "bugs"]
order = 2

# Inline color overrides on top of the active theme
[theme]
primary = "#4A9EFF"
accent = "#50C878"
background = "#1A1A2E"
text = "#E0E0E0"
```

### Custom themes

Drop `.toml` files into `~/.config/slk/themes/`:

```toml
name = "My Theme"

[colors]
primary      = "#BD93F9"
accent       = "#50FA7B"
warning      = "#FFB86C"
error        = "#FF5555"
background   = "#282A36"
surface      = "#343746"
surface_dark = "#21222C"
text         = "#F8F8F2"
text_muted   = "#6272A4"
border       = "#44475A"

# Optional sidebar/rail overrides — lets you have a darker sidebar with a
# lighter message pane (Slack's default look). Fall back to
# background/text/text_muted/surface_dark when omitted.
sidebar_background = "#19171D"
sidebar_text       = "#D1D2D3"
sidebar_text_muted = "#9A9B9E"
rail_background    = "#19171D"
```

### Data paths (XDG)

| Path | Contents |
|---|---|
| `~/.config/slk/` | Configuration, custom themes |
| `~/.local/share/slk/` | SQLite cache, tokens |
| `~/.cache/slk/` | Avatars, image cache |

## Architecture

Service-oriented, four layers:

```
UI Layer (bubbletea)   workspace rail · sidebar · messages · thread · compose · status bar
Service Layer          WorkspaceManager · MessageService · ConnectionManager
Client Layer           Slack Web API + browser-protocol WebSocket
Data Layer             SQLite cache · TOML config · token storage
```

- ~9,300 lines of Go across 31 source files and 24 test files
- SQLite is a cache, not the source of truth — Slack remains authoritative
- Render cache + bubbles/viewport for snappy scrolling
- muesli/reflow everywhere for ANSI-correct wrapping and truncation

See [`docs/superpowers/specs/`](docs/superpowers/specs/) for design specs and [`docs/STATUS.md`](docs/STATUS.md) for the live implementation status.

## Clipboard / OSC 52 caveats

slk writes the system clipboard via the OSC 52 escape. Most modern
terminal emulators (alacritty, kitty, wezterm, foot, iterm2, recent
gnome-terminal) accept these writes by default. A few need explicit
opt-in:

- **tmux:** `set -g set-clipboard on` in your tmux config.
- **screen:** has no working OSC 52 path; consider switching to tmux.
- **kitty (older versions):** `clipboard_control write-clipboard` in
  `kitty.conf`.

If `Copied N chars` shows in the status bar but nothing lands in your
clipboard, your terminal is silently dropping the OSC 52 write. There
is no reliable round-trip to detect this from inside slk.

## License

[MIT](LICENSE) © Grant Ammons
