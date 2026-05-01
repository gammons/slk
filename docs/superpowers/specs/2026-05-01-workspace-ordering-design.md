# Workspace ordering

## Problem

The number keys `1`–`9` jump to a workspace by index in the rail, but the
rail's order is currently determined by whichever workspace's connection
goroutine finishes first. Two related issues:

1. **No user-expressed preference.** A user with three workspaces has no way
   to say "this is workspace 1, this is 2, this is 3."
2. **Non-deterministic order even without preference.** Tokens are loaded
   via `os.ReadDir` (alphabetical by team ID filename) and connected in
   parallel goroutines, so the rail order changes from run to run depending
   on network latency.

## Goals

- Let users specify a stable order for their workspaces in `config.toml`.
- Make `1`–`9` map to the same workspace every run.
- Make `1`–`9` work from the moment the TUI is drawn, even before all
  workspaces have finished connecting.

## Non-goals

- An in-app UI for reordering (drag-in-rail, picker reorder, etc.).
- Showing the `1`–`9` digit as an overlay on rail items.
- Changing how tokens are stored on disk.

## Config schema

Add one field to the existing `[workspaces.<slug>]` block:

```toml
[workspaces.work]
team_id = "T01ABCDEF"
order   = 1            # NEW: 1-based; lower = earlier in rail

[workspaces.side]
team_id = "T02XYZ"
order   = 2

[workspaces.oss]
team_id = "T03QQQ"
# no `order` -> unordered bucket
```

```go
type Workspace struct {
    TeamID   string                `toml:"team_id"`
    Theme    string                `toml:"theme"`
    Order    int                   `toml:"order"`   // NEW. 0/missing = unordered.
    Sections map[string]SectionDef `toml:"sections"`
}
```

`order` is treated as 1-based: positive values are explicit positions
ascending. `0` or missing means "unordered" — the field's zero value
deliberately maps to "no preference," so legacy configs continue to
behave the way they do today (modulo determinism — see below).

## Sort algorithm

Given the list of tokens on disk and the loaded config, produce a
deterministic ordered slice:

1. For each token, look up its config block by `team_id`
   (existing `Config.WorkspaceByTeamID`). Record the slug used as the map
   key.
2. **Bucket A — configured with `order > 0`:** sort ascending by `order`.
   Ties (two workspaces with the same `order`) are broken alphabetically
   by slug.
3. **Bucket B — configured but no `order` (or `order <= 0`):** sort
   alphabetically by slug.
4. **Bucket C — no config block at all:** sort alphabetically by team ID.
5. The final order is `A ++ B ++ C`. Index `i` (0-based) maps to the
   `i+1`-th number key (`1`..`9`); rail items beyond the 9th are still
   reachable via the workspace picker (`Ctrl+w`) but not the digit keys.

This algorithm is implemented as a pure helper that takes
`([]slack.Token, config.Config)` and returns a slice of "ordered token"
records. It is unit-tested in isolation.

## Rail population timing

The rail is **pre-allocated at startup**, before any WebSocket connects:

1. After loading config and tokens, compute the ordered slice.
2. Seed `WorkspaceManager` with one entry per token, using a placeholder
   display name (the configured slug if known, otherwise the team ID).
3. Emit a startup message (`ui.WorkspacesSeededMsg`) to the UI carrying
   the ordered list, so the rail renders all slots immediately. Slots for
   not-yet-connected workspaces render in a "connecting" visual style
   (e.g., dimmed text).
4. Connection goroutines are launched as today, in parallel.
5. When a `WorkspaceReadyMsg` arrives, the rail and `WorkspaceManager`
   **update the existing entry in place** (real Slack name, domain) —
   they do not append. The slot index does not change.
6. `WorkspaceFailedMsg` continues to fire on connect failure; its rail
   entry can render as failed but stays in the same slot.

This means `1`–`9` works from the first frame and remains stable through
the connect phase. Pressing `3` for a not-yet-ready workspace shows the
existing "loading" state until its `WorkspaceReadyMsg` lands.

The existing `general.default_workspace` resolution and "first to
connect claims active" fallback are unchanged. `default_workspace`
still wins; otherwise the first ready workspace claims active. Slot
order and "active" are independent.

## Code touch points

- `internal/config/config.go` — add `Order int` to `Workspace`. No
  migration required: the zero value preserves "unordered" semantics
  for existing configs.
- `internal/config/ordering.go` (new) — exported helper, e.g.:

  ```go
  type OrderedToken struct {
      Token slack.Token
      Slug  string // "" if no config block
      Order int
  }

  func OrderTokens(tokens []slack.Token, cfg Config) []OrderedToken
  ```

  Pure function; no I/O. Unit tested in `ordering_test.go`.
- `internal/service/workspace.go` — split `AddWorkspace` into:
  - `SeedWorkspace(id, placeholderName, domain string)` — append a
    placeholder slot at startup.
  - `UpdateWorkspace(id, name, domain string)` — update in place; no-op
    if `id` is unknown (defensive).

  The connect goroutine calls `UpdateWorkspace` on success instead of
  `AddWorkspace`. Existing callers of `AddWorkspace` are migrated.
- `cmd/slk/main.go` — replace
  `for _, token := range tokens { go connectWorkspace(...) }` with:
  compute ordered slice → seed `wsMgr` → send `ui.WorkspacesSeededMsg`
  → launch goroutines. The `default_workspace` resolution stays
  unchanged.
- `internal/ui/app.go` — handle the new `WorkspacesSeededMsg`; change
  the `WorkspaceReadyMsg` handler to update the existing rail item
  rather than appending.
- `internal/ui/rail/...` (whatever the rail package is) — accept a
  seeded list at startup; render unconnected entries dimmed; update on
  ready/failed messages without resorting.
- `README.md` — document `order` under the `[workspaces.<slug>]`
  example block.

## Tests

- `config_test.go` — `Order` round-trips through TOML load/save;
  legacy configs without `order` still load and produce zero values.
- `internal/config/ordering_test.go` (new) — table-driven:
  - all workspaces ordered, distinct values
  - all workspaces ordered, ties (alphabetical tiebreak by slug)
  - mix of ordered + unordered configured + unconfigured
  - explicit `order = 0` treated as unordered
  - empty token list returns empty slice
  - tokens whose `team_id` matches no config block fall into bucket C
- `internal/service/workspace_test.go` — `SeedWorkspace` then
  `UpdateWorkspace` keeps index stable; `UpdateWorkspace` for unknown
  id is a no-op.
- Integration-style assertion in `cmd/slk/main_test.go` (if present)
  or a focused test on the seeding glue: given fake tokens + config,
  the seeded `wsMgr.Workspaces()` returns entries in the expected
  order with placeholder names.

## Migration

None required. Configs without `order` continue to load. The behavior
change for unordered configs is that the rail order becomes
deterministic (alphabetical by slug, then team ID) instead of
race-dependent — strictly an improvement. Documented in the README.
