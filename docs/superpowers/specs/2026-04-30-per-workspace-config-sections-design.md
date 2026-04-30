# Per-workspace config sections

## Problem

`config.toml` has a single global `[sections.*]` table whose values are
channel-name globs (`eng-*`, `alerts`, `*-alerts`). Globs are inherently
content-specific: the channel set in a work workspace has nothing to do
with the channel set in a side-project workspace, so a single global
table either matches the wrong channels in one workspace or has to be
reduced to the empty intersection. Today there is no way to express
different sidebar sections per workspace.

A second, pre-existing wart compounds the problem: `[workspaces.<key>]`
blocks (currently only used for per-workspace `theme`) are keyed by
the raw Slack team ID (e.g., `T01ABCDEF`). That is user-hostile in a
hand-edited file, and `general.default_workspace` is read into config
but never resolved at runtime — there is no slug → team ID mapping.

## Goals

- Allow each workspace to define its own `[sections.*]` table.
- Switch `[workspaces.<key>]` to slug-keyed blocks with an explicit
  `team_id` field, making the file readable and finally giving
  `default_workspace` a concrete meaning.
- Keep every existing config working without a destructive rewrite.

## Non-goals

- Per-workspace overrides for `[notifications]`, `[sidebar]`,
  `[animations]`, `[cache]`. The structure designed here makes those
  trivial to add later, but they are out of scope for this pass.
- Migrating token JSON files to embed slugs. The token store remains
  keyed by team ID on disk.
- Auto-rewriting legacy team-ID-keyed blocks into slug form. Users opt
  in by editing their config.

## Config shape

```toml
[general]
default_workspace = "work"     # now actually resolves to a workspace

[appearance]
theme = "nord"                 # global default

[sections.Engineering]         # global sections still allowed (fallback)
channels = ["eng-*"]
order = 1

[workspaces.work]
team_id = "T01ABCDEF"          # explicit slug → team_id link
theme   = "dracula"            # per-workspace theme override (existing)

[workspaces.work.sections.Alerts]
channels = ["alerts", "*-alerts"]
order = 1

[workspaces.work.sections.Engineering]
channels = ["eng-*", "deploys"]
order = 2

[workspaces.side]
team_id = "T02XYZ"
# no sections block → falls back to global [sections.*]
```

Slug is the TOML key; `team_id` is a field inside the block.

## Resolution semantics

- **Sections (replace, not merge):** if `[workspaces.<slug>.sections]`
  contains any entries, it replaces global `[sections.*]` for that
  workspace entirely. If empty/absent, the workspace falls back to
  global `[sections.*]`. Within a workspace the existing order/glob
  rules apply unchanged.
- **Theme:** unchanged. `WorkspaceSettings.Theme` keeps overriding
  `Appearance.Theme` exactly as it does today.
- **Other tables** (`[notifications]`, `[sidebar]`, `[animations]`,
  `[cache]`): stay global for this pass.

### API change

`Config.MatchSection(name)` becomes `Config.MatchSection(teamID, name)`.
All call sites that classify channels for the sidebar pass the active
team ID. When `teamID` is empty or unknown, behavior matches the
current global-only path.

## Slug ↔ team_id resolution

After TOML unmarshal, the loader builds two indices:

- `byTeamID map[string]workspaceEntry` — runtime lookup path (the app
  always knows the active team ID).
- `bySlug map[string]string` — for resolving
  `general.default_workspace` to a team ID.

Each `workspaceEntry` carries the slug, team ID, theme, and an optional
sections map (using the existing `SectionDef`).

### Backward-compatible key detection

For each `[workspaces.<key>]` block at load time:

1. If the block has a `team_id` field set, treat `<key>` as a slug and
   the field value as the team ID.
2. Else if `<key>` matches the Slack team/enterprise ID shape
   (`^[TE][A-Z0-9]{6,}$`), treat `<key>` itself as the team ID and
   synthesize a slug equal to the lowercased key. This is the legacy
   path; existing configs keep working unchanged.
3. Otherwise, return a load error: `workspace "<key>" is missing
   team_id`.

Mixed configs (some legacy, some slug-style) work; users opt in to
slugs by renaming the key and adding `team_id`.

### Conflict rules

- Two slugs pointing at the same `team_id` → load error.
- Duplicate slugs are impossible (TOML enforces unique keys).
- A slug-keyed block whose `team_id` is itself slug-shaped (i.e., not
  matching the team-ID regex) → load error.

## CLI / write-path integration

### `--add-workspace` (`cmd/slk/main.go`)

After auth, prompt for a slug with a default derived from `team_name`:
lowercase, non-alphanumerics collapsed to `-`, leading/trailing `-`
trimmed. `"Acme Inc."` → `acme-inc`. On collision with an existing
slug, suggest `acme-inc-2`, `-3`, etc. Write
`[workspaces.<slug>] team_id = "T..."` to the config. Token storage
stays keyed by team ID on disk.

### `saveWorkspaceTheme` (`cmd/slk/save_theme.go:89`)

Today writes `[workspaces.<teamID>]`. New behavior:

1. Resolve team ID → slug via the load-time index.
2. If a slug exists, rewrite or append under `[workspaces.<slug>]`.
3. If no slug is mapped (e.g., a legacy team-ID-keyed block already in
   the file), keep writing under the team-ID key. This preserves the
   user's existing layout rather than silently restructuring it.

### `--list-workspaces`

Include the slug column alongside team ID and team name.

## Testing

### `internal/config/`

- `MatchSection(teamID, channel)`:
  - Per-workspace override wins when present.
  - Fallback to global `[sections.*]` when the workspace has no
    sections block.
  - Unknown team ID falls back to global.
  - Empty team ID falls back to global.
- `default_workspace` resolves a slug to a team ID; unknown slug yields
  empty + error.
- Load:
  - Slug-keyed block with `team_id` field.
  - Legacy team-ID-keyed block with no `team_id`.
  - Mixed file with both styles.
  - Missing `team_id` on a non-team-ID-shaped key → error.
  - Duplicate `team_id` across two slugs → error.
- `ResolveTheme` keeps working through both the slug-keyed and the
  legacy team-ID-keyed paths.

### `cmd/slk/`

- `save_theme_test.go`:
  - Slug path: writes `[workspaces.work]` when a slug is mapped.
  - Legacy path: keeps writing `[workspaces.T01ABCDEF]` when no slug
    is mapped.
- Slug auto-derivation + collision suffixing in `--add-workspace`.

### Sidebar

Existing sidebar tests adjusted to pass `activeTeamID` into
`MatchSection`. No behavior change expected when no workspace
overrides are configured.
