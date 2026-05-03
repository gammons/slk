# slk

> **A blazingly fast Slack TUI.**
> Keyboard-driven, beautifully themed, and under 20MB. One static binary. No Electron required.
>
> Marketing site: [getslk.sh](https://getslk.sh) · Docs: [Wiki](https://github.com/gammons/slk/wiki)

![slk screenshot](docs/assets/screenshot.png)

`slk` is a daily-driver replacement for the official Slack desktop client, built in Go with [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss).

## Why slk?

- **Fast.** Cold start in milliseconds. Render-cached messages. SQLite-backed scrollback. Real-time over WebSocket.
- **Tiny.** ~19 MB on disk. ~60 MB RSS for a live multi-workspace session vs. 500 MB–1.5 GB for the official client. No node_modules, no Chromium, no 1Gb RAM tax.
- **Keyboard-first.** Vim-style modal editing. `j/k`, `h/l`, `i`, `Esc`.
- **Pretty.** 12 built-in themes, lipgloss-styled panels, true-pixel avatars on kitty (half-block fallback elsewhere), emoji shortcodes, day separators, and pill-style reactions.
- **Multi-workspace.** All your workspaces stay connected in parallel. `1`–`9` to instantly jump between them, with live unread badges in the rail.
- **Yours.** TOML config, custom themes, custom channel sections via glob, XDG-compliant paths.

## Highlights

- Real-time messages, edits, deletes, reactions, typing indicators
- Inline images (kitty graphics / sixel / half-block fallback) with full-screen preview
- Threads side panel + a workspace-wide threads view
- Smart paste: clipboard images, file paths, or text — multiple attachments + caption in one send
- Slack-native sidebar sections, kept live; or glob-based config sections
- Browser-cookie auth (`xoxc` + `d`) — no Slack App required
- Vim-style modal keybindings, fuzzy channel finder, workspace picker
- 12 themes + drop-in custom themes, live theme switcher
- OS desktop notifications on DMs, mentions, and configurable keywords

Full feature breakdown: **[[Features|https://github.com/gammons/slk/wiki/Features]]**

## Quick install

**Linux/macOS** (auto-resolves the latest version):

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
# Linux x86_64
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_x86_64.tar.gz" | tar xz
# macOS Apple Silicon
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_darwin_arm64.tar.gz" | tar xz
sudo mv slk /usr/local/bin/
```

**Go:**

```bash
go install github.com/gammons/slk/cmd/slk@latest
```

For `.deb` / `.rpm` / `.apk` packages, Windows, build-from-source, and checksums, see the [Installation wiki page](https://github.com/gammons/slk/wiki/Installation).

## Setup

slk uses your existing browser session. Grab your `xoxc` token and `d`
cookie from DevTools, then:

```bash
slk --add-workspace
```

Full walkthrough: [Setup wiki page](https://github.com/gammons/slk/wiki/Setup).

## Documentation

Everything lives in the [**wiki**](https://github.com/gammons/slk/wiki):

- [Installation](https://github.com/gammons/slk/wiki/Installation) — prebuilt binaries, Go install, build from source
- [Setup](https://github.com/gammons/slk/wiki/Setup) — token extraction, adding workspaces
- [Features](https://github.com/gammons/slk/wiki/Features) — full feature breakdown
- [Keybindings](https://github.com/gammons/slk/wiki/Keybindings) — every key, every mode
- [Configuration](https://github.com/gammons/slk/wiki/Configuration) — `config.toml`, custom themes, XDG paths
- [Terminal Compatibility](https://github.com/gammons/slk/wiki/Terminal-Compatibility) — what each terminal supports
- [Clipboard and OSC 52](https://github.com/gammons/slk/wiki/Clipboard-and-OSC-52) — copy/paste setup notes
- [Tradeoffs and Non-Goals](https://github.com/gammons/slk/wiki/Tradeoffs-and-Non-Goals) — roadmap, caveats, TOS notice
- [Architecture](https://github.com/gammons/slk/wiki/Architecture) — service layout, data layer

## Disclaimer

`slk` is an independent, unofficial project. It is not affiliated with, endorsed by, or sponsored by Slack Technologies, LLC or Salesforce, Inc. "Slack" is a trademark of Slack Technologies, LLC; it is used here only to describe the service this client interoperates with.

slk talks to Slack via the same internal browser protocol the official web client uses. This is unofficial and not sanctioned by Slack — see [Tradeoffs and Non-Goals](https://github.com/gammons/slk/wiki/Tradeoffs-and-Non-Goals#unofficial--tos-caveat) for details.

## License

[MIT](LICENSE) © Grant Ammons
