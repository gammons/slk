# Configuration

Config lives at `~/.config/slk/config.toml`.

## Full example

```toml
[general]
default_workspace = "work"      # the slug, not the team ID
use_slack_sections = true       # use real Slack sidebar sections (default).
                                # set false to use [sections.*] globs instead.

[appearance]
theme = "dracula"
timestamp_format = "3:04 PM"
image_protocol = "auto"   # auto | kitty | sixel | halfblock | off
max_image_rows = 20       # cap inline image height in terminal rows

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
max_image_cache_mb = 200

# Glob-based channel sections — only consulted when use_slack_sections
# is false (globally or per-workspace), or when Slack's section API is
# unreachable. Otherwise slk reads the user's actual Slack sections.
[sections.Alerts]
channels = ["alerts", "ops", "*-alerts"]
order = 1

# Per-workspace settings: keyed by a slug you choose at --add-workspace
# time. team_id ties the slug to the underlying Slack workspace.
[workspaces.work]
team_id = "T01ABCDEF"
order   = 1                     # rail position; 1-based, used by 1-9 keys
theme   = "dracula"             # overrides [appearance].theme
use_slack_sections = false      # this workspace uses [sections.*] globs;
                                # other workspaces still use Slack sections

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

### v1 limitations of Slack-native sections

v1 is read-only — section editing still happens in the official client; slk
reflects the results. Sections of type `stars`, `slack_connect`,
`salesforce_records`, and `agents` are hidden (matching the official
client's filtering). Sections with more than 10 channels may be returned
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
```

Switch themes live with `Ctrl+y`.

## Data paths (XDG)

| Path | Contents |
|---|---|
| `~/.config/slk/` | Configuration, custom themes |
| `~/.local/share/slk/` | SQLite cache, tokens |
| `~/.cache/slk/` | Avatars, image cache |
