# Configuration

Config lives at `~/.config/slk/config.toml`.

## Full example

```toml
[general]
default_workspace = "work"      # the slug, not the team ID
use_slack_sections = true       # use real Slack sidebar sections (default).
                                # set false to use [sections.*] globs instead.
download_dir = "~/Downloads"    # where file attachments (`d` keybinding) are
                                # saved. "~" is expanded to your home directory.
hide_deactivated_users = true   # hide deactivated people, as the Slack desktop
                                # client does: their DMs leave the sidebar and
                                # they are not offered in the channel finder,
                                # the new-message picker or @ / # autocomplete.
                                # Set false to show them again. See below.
workspace_picker_vim_keys = false  # true: :ws opens navigating: j/k move,
                                   # / starts filtering (esc clears it)
image_loading = "trusted" # which message images load without asking:
                          #   trusted: Slack-hosted (files.slack.com, *.slack-edge.com)
                          #            and trusted_image_hosts load; others wait for I
                          #   confirm: every message image waits for I
                          #   off:     never load message images; they stay links
                          # Avatars and custom emoji are not affected.
trusted_image_hosts = []  # extra hosts (and their subdomains) to load in "trusted"
                          # mode, https only, e.g. ["githubassets.com"]

[appearance]
theme = "dracula"
transparent_background = false  # true: panes, sidebar, rail, status bar and the
                                # startup screen use the terminal's own background,
                                # so its opacity and blur show through. Set the
                                # opacity in the terminal (WezTerm
                                # window_background_opacity, Ghostty
                                # background-opacity, kitty background_opacity,
                                # Alacritty window.opacity). Selection,
                                # badges, the composer and dialogs keep their colours.
                                # Text keeps the theme's colours, so pick a theme
                                # whose text reads on your terminal's background
                                # (a light theme on a dark terminal won't).
timestamp_format = "3:04 PM"
show_avatars = true      # false hides the avatars beside messages
thread_avatars = true    # false hides them in the thread pane (needs show_avatars)
show_workspace_rail = true  # false starts with the workspace rail hidden (Ctrl+g
                            # toggles it); never drawn with one workspace
image_protocol = "auto"   # auto | kitty | sixel | halfblock | off
kitty_placement = "auto"  # auto | native | emulated: who places kitty images.
                          # auto = emulated on WezTerm (no unicode
                          # placeholders), native elsewhere. Native under
                          # tmux and zellij. See Terminal Compatibility.
max_image_rows = 20       # cap inline image height in terminal rows
reply_avatars = false     # true: Slack-style reply row, repliers' avatars + "N replies"
                          # (avatars need show_avatars and kitty graphics)
colored_usernames = false # color each user's name by ID hash (Slack-style)
click_opens_thread = true # false: clicking a message only selects it (Enter
                          # still opens the thread)
compact = false           # compact, IRC-like style (less padding, username on same line as messages, no avatars except repliers)
                          # Similar to Slack's "Compact Theme"

[animations]
enabled = true
smooth_scrolling = true
typing_indicators = true  # also apps' "working" status (e.g. "Atlas is working...")

[notifications]
enabled = true
on_mention = true
on_dm = true
on_keyword = ["deploy", "incident"]
on_thread_reply = false    # true: notify for replies in threads you're subscribed to
on_active_channel = false  # true: still notify for the channel you're viewing
quiet_hours = "22:00-08:00"   # planned

# notify_command (optional): run INSTEAD of the built-in OS notification for any
# message that would notify (DM / mention / keyword / thread reply). Executed via `sh -c` with
# $SLK_TITLE and $SLK_BODY set, so you can route notifications through your own
# tooling (terminal-notifier, a multiplexer's notifier, mako, ...). Values are
# passed via the environment, so message text can't inject shell syntax.
# notify_command = 'terminal-notifier -title "$SLK_TITLE" -message "$SLK_BODY"'

# status_command (optional): run on every unread-state change (a message arrives
# or a channel is read) so an external surface can mirror slk's unread state.
# Because it fires on reads too, it can clear a status as well as set one.
# Runs are serialized and coalesced: states never run concurrently or out of
# order, and under a burst intermediate states may be skipped — the newest
# state always runs last, so the surface converges on the current state.
# Executed via `sh -c` with:
#   $SLK_UNREAD        unread channels in the active workspace (mute-filtered)
#   $SLK_OTHER_UNREAD  other workspaces with unread channels (mute-filtered) or threads
#   $SLK_WORKSPACE     active workspace name
#   $SLK_TITLE         the window-title string, e.g. "slk SW (3) +1"
# status_command = 'my-statusbar --slack-unread "$SLK_UNREAD"'

# Both hooks require a POSIX `sh` on $PATH and are unavailable on Windows
# (the built-in OS notification still works there). Hook failures are silent
# in the UI; run slk with SLK_DEBUG=1 and check slk-debug.log ([notify] lines)
# to diagnose a misbehaving command.

# Muted channels and DMs never notify — including on mentions and keywords —
# matching Slack. (This is a behavior change: previously a mention or keyword
# in a muted channel would still notify.)

[sidebar]
right_align_badges = false  # true: mention badges at the right end of the row (Slack),
                            # and the Threads, Drafts and Activity counts as red badges there
channel_indent = 0          # cells to indent the rows under each section header (0-6)
hide_inactive_after_days = 30  # auto-hide conversations not read for this many days;
                               # 0 shows them all (the active channel and unread ones stay)
sort_dms_by_recency = true  # true: order DMs and group DMs by recent activity (unread
                            # first), as Slack does; the Direct Messages header's ⇅/A-Z
                            # control toggles it for the session
slack_style = false         # true: Slack's look: no ▾ on open sections, plain headers,
                            # no unread dot (bold only), names sorted ignoring punctuation
private_glyph = "◆"         # glyph before private channels, e.g. a lock your font has
                            # (sidebar only: the finder and pane header keep ◆)
active_only = false         # true: hide conversations with no activity in 30 days, in
                            # every section (Slack's "Active only"; unread ones stay)
mouse_resize = false        # true: drag the sidebar's right border to resize it
                            # ([ and ] also resize; the width is saved per workspace)
start_collapsed = false     # true: every section starts collapsed (Space/Enter
                            # on a header expands it until slk restarts)
show_unread_in_collapsed = false  # true: a collapsed section still lists its
                                  # unread channels (and the open one)
enter_keeps_focus = false   # true: Enter opens a conversation (or Threads,
                            # Drafts, Activity, or a workspace in the rail) but keeps
                            # focus in the list, to skim with j/k; l moves on

[compose]
editor = "nvim"   # Ctrl+E editor, used when $VISUAL and $EDITOR are unset
# Keep typing after Enter sends (default false: return to normal mode like
# vim). While it's on, app Ctrl shortcuts (Ctrl+P, Ctrl+N, Ctrl+A, ...) also
# work from an empty composer; Ctrl+W, Ctrl+H and Ctrl+K keep deleting text.
stay_in_insert_after_send = true

# Make the composer's unsent text Slack's own draft for the conversation:
# saved while you type, restored when you open the conversation (on any
# device), marked sent when you send. Also enables :drafts. Default true.
sync_drafts = true

[sidebar]
mention_pills = false   # true: "↑/↓ Unread mentions" pills while a mentioned
                        # channel is scrolled out of view (click to reach it)

[cache]
message_retention_days = 30
max_db_size_mb = 500
max_image_cache_mb = 200

[marks]
# Vim-style marks. Lowercase marks (ma..mz) are session-only and
# uppercase marks (mA..mZ) survive a restart, matching vim's
# buffer-local / global split.

# Make lowercase marks persist too. Turning this back off is not
# destructive: previously saved lowercase marks stop loading, and
# reappear if you turn it on again.
persist_all = false

# Pressing ' opens the marks overlay so you can see what is set
# without memorising letters. Set false for plain vim behaviour: '
# waits silently for a letter. This never affects `:marks`, which
# always shows the overlay.
show_jump_overlay = true

# Glob-based channel sections — only consulted when use_slack_sections
# is false (globally or per-workspace), or when Slack's section API is
# unreachable. Otherwise slk reads the user's actual Slack sections.
[sections.Alerts]
channels = ["alerts", "ops", "*-alerts"]
order = 1

# Channels can carry an optional ":<N>" suffix to pin their order
# within the section. Lower numbers sort higher. Entries without a
# suffix fall after annotated ones, alphabetically.
# This syntax is only honored when use_slack_sections = false;
# in Slack-native mode, channels sort alphabetically within each section.
[sections.Engineering]
channels = ["eng-general:1", "eng-alerts:2", "eng-*"]
order = 2

# Per-workspace settings: keyed by a slug you choose at --add-workspace
# time. team_id ties the slug to the underlying Slack workspace.
[workspaces.work]
team_id = "T01ABCDEF"
order   = 1                     # rail position; 1-based, used by 1-9 keys
theme   = "dracula"             # overrides [appearance].theme
use_slack_sections = false      # this workspace uses [sections.*] globs;
                                # other workspaces still use Slack sections
default_channel = "general"     # see "Default channel" below

[workspaces.work.sections.Alerts]
channels = ["alerts", "*-alerts"]
order = 1

[workspaces.work.sections.Engineering]
channels = ["eng-*", "deploys"]
order = 2

# A second workspace with no per-workspace sections — falls back to
# the global [sections.*] above.
[workspaces.side]
team_id = "T02XYZ"
order   = 2

# Inline color overrides on top of the active theme
[theme]
primary = "#4A9EFF"
accent = "#50C878"
background = "#1A1A2E"
text = "#E0E0E0"
```

## Default channel

`[workspaces.<key>] default_channel` names the channel slk opens the first
time that workspace is shown in a session:

- at startup, in place of the channel you last visited there;
- on the first switch to the workspace, in place of the first channel in the
  sidebar.

Switching back later returns to the channel you were last viewing in that
workspace, or to the default if that channel is gone. A Slack link opened from
the channel finder still goes to its own channel.

The name is matched as `/open` matches it: a leading `#`, case and accents are
ignored, and only channels and private channels you are a member of match,
never a DM. A name that matches nothing is ignored, and slk behaves as if the
key were not set.

## Deactivated users

`[general] hide_deactivated_users` (default `true`) hides people whose Slack
account is deactivated, matching the Slack desktop client. It applies to:

- DM and app rows in the sidebar (including starred ones and ones with unread
  messages; the conversation you have open is never hidden, so a permalink or
  history jump to it still works)
- the channel finder's list before you type (`Ctrl+t` / `Ctrl+p`) and the
  forward picker. Type part of the person's name to find the old DM on
  purpose: it shows dimmed, tagged `deactivated`, below every other match,
  and opens to its history
- the new-message picker (`Ctrl+n`)
- `@` mention autocomplete and `#` autocomplete in both composers

Group DMs that include a deactivated person are kept, as in Slack, and their
messages still show who wrote them. slk learns who is deactivated from the
conversation list and from each user's profile. On startup it refreshes the
people in your open DMs and the authors in the channel it opens first, so
someone else may only be hidden from `@` autocomplete once their profile is
next fetched. Set the option to `false` to show everyone again.

You can't send messages to a deactivated account, so the composer of such a DM
is disabled and the forward picker never offers it, whether the option is on
or off.

## Section resolution

When `use_slack_sections = true` (the default) and Slack's section endpoint
is reachable, slk reads the user's actual sidebar sections — names, emoji,
linked-list order, and channel membership — directly from Slack and keeps
them live via WebSocket events. Any `[sections.*]` or
`[workspaces.<slug>.sections.*]` blocks in `config.toml` are ignored in this
mode (a one-line info note is emitted to the debug log on first connect so
the shadowing isn't silent). Set `use_slack_sections = false` globally, or
per-workspace, to opt into glob-based sections instead.

Per-workspace `[workspaces.<slug>.sections.*]` blocks fully replace the
global `[sections.*]` for that workspace. Workspaces that define no
sections of their own fall back to the global table.

### Ordering channels within a section

Channels sort alphabetically within each section, regardless of unread status.
Slack-native section order is preserved, so Starred stays where you placed it.
Within Starred, channels stay ahead of people and apps, with each group sorted
alphabetically.

Each entry in a section's `channels` list may carry an optional `:<N>`
suffix where `N` is a non-negative integer. Channels matched by an
annotated pattern sort ahead of channels matched by un-annotated
patterns; among annotated channels, lower `N` wins; un-annotated
channels sort alphabetically.

```toml
[sections.Engineering]
channels = ["eng-general:1", "eng-alerts:2", "eng-*"]
order = 2
```

In the example above, `#eng-general` is pinned to the top of the
Engineering section, followed by `#eng-alerts`, followed by every
other `eng-*` channel in alphabetical order.

This syntax is only honored when `use_slack_sections = false` (or
when Slack's section endpoint is unreachable and slk falls back to
glob mode). In Slack-native mode, channels sort alphabetically within each
section and the `:<N>` suffix is ignored along with the rest of the
`[sections.*]` block.

### Limitations of Slack-native sections

Slack-native sections are read-only — section editing still happens in the official client; slk
reflects the results. Starred conversation membership comes from `client.userBoot`,
including on Enterprise Grid where the `stars.list` endpoint can be restricted.
The initial boot list is reused at startup and refreshed on section rebootstrap
after reconnect. The `stars` section is shown when non-empty, with the header
`Starred`. Sections of type `slack_connect`, `salesforce_records`, and `agents`
are hidden. Sections with more than 10 channels may be returned
only partially by Slack's API on initial load; the missing channels
temporarily fall into the catch-all bucket and migrate into their correct
section as WebSocket events fire or the workspace reconnects. A debug-log
warning identifies which sections were truncated.

## Workspace order

The `order` field controls workspace position in the rail and the mapping
for the `1`–`9` digit keys. Positive values sort ascending (lowest first);
workspaces without an `order` (or with `order = 0`) sort after explicitly
ordered ones, alphabetically by slug. Tokens on disk that have no
`[workspaces.<slug>]` block at all sort last, alphabetically by team ID.
The order is stable across runs. Previously the rail order depended on
which workspace's WebSocket connected first; it is now deterministic
regardless of network timing, even without an explicit `order` set.

Legacy configs that key the block by raw team ID
(`[workspaces.T01ABCDEF]`) keep working unchanged.

## Terminal-palette themes (`ANSI Dark`, `ANSI Light`)

Two built-in themes use ANSI 16 color codes exclusively rather than
fixed RGB values. They inherit the user's terminal color palette, so
changing your terminal colorscheme (light/dark, solarized,
accessibility palettes, etc.) immediately changes slk's UI colors to
match.

```toml
[appearance]
theme = "ANSI Dark"   # or "ANSI Light"
```

Pick the variant whose background matches your terminal's background.

**Trade-off:** selection-row highlights and compose-input tints are
still computed as RGB approximations, so the tint regions of those
elements use truecolor rather than your palette. The rest of the UI
honors the palette.

## Custom themes

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

# Optional, for a Slack-like look. Unset keeps slk's defaults.
username            = "#1D1C1D" # author names (default: primary)
sidebar_selected_bg = "#333133" # selected sidebar row as a filled pill, no ▌
sidebar_selected_fg = "#F8F8F8" # its text (default: sidebar_text)
mention_badge_bg    = "#C01343" # mention-count badge (default: selection colours)
mention_badge_fg    = "#FFFFFF"

# A message that mentions you (directly, @here/@channel/@everyone, or a
# usergroup you're in). Unset derives them from warning.
self_mention_bg       = "#ECB22E" # the @mention itself (default: warning)
self_mention_fg       = "#1D1C1D" # (default: background)
mention_highlight_bg  = "#FEF6DE" # the whole message (default: warning mixed into background)
mention_highlight_bar = "#ECB22E" # its left bar (default: warning)
```

Every built-in theme now sets a channels-panel (sidebar) background that is
perceptibly distinct from the message pane. When writing a custom theme,
set `sidebar_background` to a clearly darker (or, on near-black themes, a
slightly lighter) shade than `background` for the same effect.

Switch themes live with `Ctrl+y`, or cycle through them with `Alt+y` /
`Alt+Shift+y`.

slk reads the theme files at startup. To have a running slk read them again
(say a palette generator such as Noctalia or matugen rewrote one), send it
`SIGUSR1`: `pkill -USR1 -x slk`. It picks up new and changed themes and
reapplies your selected theme. Not on Windows.

## Marks

`m` followed by a letter records the selected message; `'` followed by
that letter jumps back to it. A mark records the channel, the message,
and — when you are reading inside a thread — the thread and the reply,
so `'a` can put you back on one message in a 200-reply thread.

Case decides lifetime, as in vim:

| | |
|---|---|
| `ma` … `mz` | session marks, gone when slk exits |
| `mA` … `mZ` | persistent, stored in the local SQLite cache |

`persist_all = true` makes lowercase marks persist as well. Turning it
off later does not delete them — they stop loading, and come back if you
turn it on again.

`''` is the back-jump: it returns you to wherever you were before the
last jump, whether or not the jump changed channel. Pressing it again
returns you to the mark, so `''` toggles between two places.

Marks belong to the workspace they were set in. The same letter in two
workspaces is two independent marks, and jumping to a mark from another
workspace is refused rather than switching workspaces.

A mark whose message has since been deleted still opens its channel and
tells you the message was not found (a deleted thread reply opens its
thread with nothing selected); the mark is kept either way. Marks
are only removed when you remove them, with `:delmarks` or `Backspace`
in the `:marks` overlay.

## Hiding authors

`:suppress` on a message hides its author's messages, in that
conversation or in the whole workspace; `:suppressed` lists who is
hidden and `Backspace` there stops hiding them. Both edit
`config.toml`, where the rules are kept, one block each in the
workspace's section. You can write them by hand too:

```toml
[workspaces.acme]
team_id = "T01ACME"

[[workspaces.acme.suppress]]
author = "B01DEPLOYBOT"     # required: a user ID (U…/W…) or a bot ID (B…)
note = "DeployBot"          # optional, for you; slk ignores it

[[workspaces.acme.suppress]]
author = "U02ALICE"
channel = "C03RANDOM"       # omit to hide them everywhere in the workspace
```

IDs, not names, because names change: `:suppress` writes the right ID
(and a `note`) for you. An author is the ID slk shows the message as
from: a person's user ID, a bot that posts as a user by that user ID,
and a bot that posts without one by its bot ID.

A hidden author's messages and thread replies are not drawn, do not
notify (even when they mention you), and do not mark their channel
unread. They are still cached, and opening a channel still marks it read
up to its newest message, hidden ones included. Search, the Activity
view and reply counts are not filtered. Your own messages are never
hidden.

A channel that Slack already reported unread when slk connected stays
unread until you open it, even if its only new messages are hidden.

slk adds a rule by appending a block to the end of the file and removes
one by deleting its block, leaving everything else as written. A rule
written another way (an inline table) can only be removed by hand, and
while a workspace's rules are an inline list (`suppress = [...]`) new
ones have to be added to that list by hand too.

## Data paths (XDG)

| Path | Contents |
|---|---|
| `~/.config/slk/` | Configuration, custom themes |
| `~/.local/share/slk/` | SQLite cache, tokens |
| `~/.cache/slk/` | Avatars, image cache |
