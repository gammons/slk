# Features

## Messaging

- Real-time messages, edits, deletes, reactions, and typing indicators over WebSocket
- Edit your own messages (`E`) — reuses the compose box with stash/restore for any in-progress draft
- Delete your own messages (`D`) — centered confirmation overlay with message preview
- Slack markdown rendering (bold, italic, strikethrough, code, blockquotes, links, mentions)
- Emoji shortcodes (`:rocket:` → 🚀)
- Day separators (Today, Yesterday, Monday, full date)
- Infinite scroll backfill into SQLite cache
- New-message landmark (red `── new ──` line at the unread boundary)
- Mark-as-read synced to Slack on channel entry
- Mark-as-unread (`U`) — rolls the read watermark backward to the selected message; thread replies supported. Inbound `channel_marked` / `thread_marked` events from other Slack clients are reflected live.
- Edited / threaded message indicators
- ANSI-aware wrapping and truncation (no broken color codes mid-line)
- Drag-to-copy: drag the mouse across messages to highlight them; release to copy plain text to the system clipboard via OSC 52

## Compose

- Multi-line input, `Shift+Enter` for newlines
- Inline `@mention` autocomplete (resolves to `<@UserID>` on send)
- Special mentions: `@here`, `@channel`, `@everyone`
- Bracketed paste — paste multi-line text from the system clipboard without it being interpreted as keystrokes
- Smart paste (`Ctrl+V`) — pastes a clipboard image as an attachment, or a copied file path as an attached file, or falls through to text. Multiple attachments + caption send together via Slack's V2 file-upload API. Note: use `Ctrl+V` (not your terminal's `Ctrl+Shift+V` paste shortcut) — terminal-initiated paste only delivers text, never image bytes.
- CommonMark in compose: type `**bold**`, `~~strike~~`, `[label](url)`, `- list items`, `1. numbered`, or fenced ```code blocks``` and slk converts them on send to Slack's mrkdwn + rich_text format. Already-mrkdwn syntax (`*bold*`, `_italic_`, `~strike~`) passes through unchanged. Single-asterisk emphasis (`*x*`) is preserved as literal text since it conflicts with Slack mrkdwn bold.

## Images

- Inline image attachments render automatically in the messages pane: kitty graphics protocol on capable terminals (kitty, ghostty, recent WezTerm), sixel on foot/mlterm, half-block (`▀`) fallback everywhere else
- User avatars use the same kitty graphics path on capable terminals for sharper pixels; sixel and other terminals fall back to half-block
- Click any inline image (or press `O` on the selected message) for a full-screen in-app preview
- `Enter` from the preview launches the OS image viewer
- Lazy-loaded: images download only as they scroll into view
- LRU cache at `~/.cache/slk/images/` (default 200 MB cap)
- Inside tmux, slk falls back to half-block to avoid pixel-protocol pass-through pitfalls
- Configurable via `[appearance] image_protocol` (`auto` / `kitty` / `sixel` / `halfblock` / `off`) and `max_image_rows`

See [[Terminal Compatibility|Terminal-Compatibility]] for which protocol your terminal supports.

## Threads

- Side panel (35% width), opened with `Enter`, toggled with `Ctrl+]`
- Live thread reply routing, real-time updates
- Auto-closes on channel switch or narrow terminals
- **Threads view** (`⚑ Threads` at top of sidebar): scrollable list of every
  thread you authored, replied to, or were @-mentioned in for the active
  workspace. Unread first, then newest activity. Selecting a thread opens
  it in the side panel; the list re-ranks live as new replies arrive.
  v1 is computed from the local SQLite cache, so threads from channels
  you have not yet opened in slk will not appear until they are seen.

## Reactions

- Search-first picker overlay (`r`) with frecent emoji
- Quick-toggle nav across existing pills (`R`, then `h/l/Enter`)
- Pill-style display (green = yours, gray = others)
- Optimistic UI, deduped against the WebSocket echo

## Channels & Workspaces

- Three-panel layout: workspace rail, channel sidebar, message pane
- Public (`#`), private (`◆`), DM (`●`/`○` for presence), and group DM channels
- **Slack-native sidebar sections** — slk reads your sections directly from Slack and reflects them live: section names, emoji, linked-list order, and channel/DM membership are kept in sync via the same WebSocket events the official client uses. Reorder, rename, create, or delete sections in any other Slack client; slk catches up within a couple seconds. Read-only: section editing still happens in the official client. Falls back to glob-based config sections when disabled or if the API is unavailable.
- Collapsible sections — `Enter`/`Space` on a section header toggles it. The default Channels section starts collapsed (`▸ Channels •3` shows aggregate unreads); pinned sections and DMs start expanded
- Live unread indicators: bold + blue dot for unread channels, muted text for read ones, aggregate dot+count on collapsed section headers
- Glob-based config sections (`[sections.*]` in `config.toml`) — used when `use_slack_sections = false` or as a fallback when Slack's API is unreachable
- Fuzzy channel finder (`Ctrl+t` / `Ctrl+p`) — auto-expands a collapsed section when you open a channel inside it; ranks 1:1 DMs above group DMs when searching by person name
- Workspace picker (`Ctrl+w`) and direct jump (`1`–`9`)
- All workspaces stay connected in parallel for live unread badges

## Notifications

- OS-level desktop notifications via [beeep](https://github.com/gen2brain/beeep)
- Triggers on DMs, mentions, and configurable keywords
- Suppressed when you're focused on the relevant channel
- Suppressed entirely while you're in DND/snooze

## Status & DND

- Set self presence (Active / Away) and DND/snooze from `Ctrl+S`
- Standard snooze durations (20m / 1h / 2h / 4h / 8h / 24h / until tomorrow morning) plus custom minutes
- Live status segment in the status bar with snooze countdown
- Reflects external state changes — set from the official Slack client or via your own API scripts — in real time over the WebSocket

## Connectivity

- Browser-cookie auth (`xoxc` + `d`) — works as any user, no Slack App required
- Direct connection to Slack's internal browser WebSocket protocol
- Auto-reconnect with exponential backoff (1s → 30s)
- Three-state connection indicator in the status bar

## Customization

- 12 built-in modern-looking themes
- Drop-in custom themes (`~/.config/slk/themes/*.toml`)
- Live theme switcher (`Ctrl+y`)
- TOML config for appearance, animations, notifications, and channel sections

See [[Configuration]] for the full `config.toml` reference and [[Keybindings]] for the key map.
