# Slack-Native Sidebar Sections — Design

**Status:** design
**Date:** 2026-05-02
**Supersedes (partially):** `2026-04-30-per-workspace-config-sections-design.md` — config-driven sections become a fallback, not the primary path.

## Summary

slk currently builds sidebar sections from TOML glob patterns
(`[sections.*]` / `[workspaces.<slug>.sections.*]`). Users have asked
to instead use the channel sections they already maintain in the
official Slack client, so they don't have to re-organize their channels
in slk.

This design adds a live integration with Slack's undocumented
`users.channelSections.list` REST endpoint and four corresponding
WebSocket events. Slack-side sections become the default; the existing
config-glob behavior is preserved as the fallback for users who opt out
or whose workspace fails to bootstrap sections.

This feature is **read-only** in v1: slk reflects Slack-side state but
never mutates it.

## Background

### Existing architecture

Sections today are entirely config-driven. `cfg.MatchSection(teamID,
channelName)` runs a glob match at workspace bootstrap and on every
conversation-opened WS event (`cmd/slk/channelitem.go:58`), stamping
`Section` and `SectionOrder` onto each `sidebar.ChannelItem`. The
sidebar buckets items by name, sorts custom sections first by
`SectionOrder`, then appends three hardcoded built-in sections (Direct
Messages → Apps → Channels) at the bottom
(`internal/ui/sidebar/model.go:64-124`).

Slack-side section support is partially scaffolded but unused:
`internal/slack/client.go:899-941` defines a `ChannelSection` struct
and `GetChannelSections` method calling
`POST users.channelSections.list`, but the struct's `channel_ids_page`
field is wrong (typed as `[]string` when the API returns an object) and
no production code path calls the method. The endpoint also failed to
authenticate prior to this work — used `http.PostForm` rather than the
cookie-aware HTTP client; fixed during the discovery phase.

### Captured data model

Live capture against two real workspaces confirmed:

**WS event types** (incoming, currently dropped at
`internal/slack/events.go:269`):

| Event | Fields | Trigger |
|---|---|---|
| `channel_section_upserted` | `channel_section_id`, `name`, `emoji`, `channel_section_type`, `next_channel_section_id`, `last_update`, `is_redacted` | create / rename / reorder / emoji change |
| `channel_section_deleted` | `channel_section_id`, `last_update` | delete |
| `channel_sections_channels_upserted` | `channel_section_id`, `channel_ids[]`, `last_update` | channels added (DM IDs accepted) |
| `channel_sections_channels_removed` | `channel_section_id`, `channel_ids[]`, `last_update` | channels removed |

**Collapse/expand is client-local** — Slack does not broadcast it.

**REST endpoint shape** (`POST users.channelSections.list`, Bearer
xoxc + d cookie):

```json
{
  "ok": true,
  "channel_sections": [
    {
      "channel_section_id": "L03DBPBN44S",
      "name": "Devops",
      "type": "standard",
      "emoji": "chopsticks",
      "next_channel_section_id": "L03DE4ARV36",
      "last_updated": 1687350049,
      "channel_ids_page": {
        "channel_ids": ["C0CPNGZT3", "C3H3QF2AU", "C82GM1W06"],
        "count": 3,
        "cursor": "C82GM1W06"
      },
      "is_redacted": false
    },
    ...
  ],
  "last_updated": 1777720341,
  "count": 11,
  "cursor": "L08LXPBL4T0",
  "entities": []
}
```

**Field name mismatches** between REST and WS payloads — the decoder
must handle both:

| What | REST | WS |
|---|---|---|
| Section type | `type` | `channel_section_type` |
| Last update | `last_updated` | `last_update` |

**Section types observed:**

| Type | Render in v1 | Notes |
|---|---|---|
| `standard` | yes | User-created. Render even when empty. |
| `channels` | yes | Default catch-all bucket. |
| `direct_messages` | yes | Default DM bucket. |
| `recent_apps` | conditional | Skip when empty (slk has its own Apps logic). |
| `stars` | no (v2) | Slack's Starred feature. |
| `slack_connect` | no (v2) | External workspaces. |
| `salesforce_records` | no | Always empty in observed dumps; official client hides. |
| `agents` | no | Always empty in observed dumps. |

**Linked-list ordering**: each section points to its successor via
`next_channel_section_id`. The tail's pointer is `null`. The head is
the section with no other section pointing at it. The list defines the
**full sidebar order** including where the default `channels` /
`direct_messages` / `recent_apps` buckets sit relative to custom
sections — slk must honor this verbatim rather than imposing its
current hardcoded order.

**Pagination**: sections paginate at ~10 channels (per-section
`channel_ids_page.cursor`). The exact endpoint shape for fetching
additional pages of channels within a section is not yet known and is
the single remaining unknown for this design (see "Open Questions").
The top-level cursor (paging through sections themselves) is
straightforward — re-call `users.channelSections.list?cursor=X`.

## Goals

- Use Slack-side sections by default, ordered exactly as the user has
  arranged them in the official client.
- Live updates within ~1s when a user reorders / renames / moves
  channels between sections from any other Slack client.
- Zero regression for users who explicitly opt out — config-glob
  behavior preserved verbatim.
- Graceful degradation: if the undocumented endpoint fails or
  disappears, slk continues to work using the existing config-glob
  fallback.

## Non-goals

- Mutating Slack-side sections from inside slk (create / rename /
  delete / move channel between sections).
- Surfacing `stars`, `slack_connect`, `salesforce_records`, `agents`
  section types.
- Inline custom-emoji image rendering in section headers (raw
  shortcode is acceptable when no unicode resolution exists).
- Persisting sidebar collapse state across restarts.

## Configuration

```toml
[general]
default_workspace = "work"
use_slack_sections = true   # NEW; default true

[workspaces.work]
team_id = "T01ABCDEF"
use_slack_sections = false  # NEW; per-workspace override
```

Resolution: per-workspace value wins when set; otherwise the global
`[general] use_slack_sections`; default `true`. Existing
`[sections.*]` and `[workspaces.<slug>.sections.*]` blocks still
parse and remain valid — they are simply ignored when the effective
value is `true` and bootstrap succeeds.

When `use_slack_sections=true` and the user has any glob sections
defined, emit one info-level log line at workspace bootstrap noting
that glob sections are being shadowed and pointing at the config
knob. Helps the rare upgraded user who notices their globs stopped
working.

## Architecture

### New types

`internal/slack/sections.go` (new file):

```go
type SidebarSection struct {
    ID         string
    Name       string  // user-visible; may be "" for system sections
    Type       string  // standard | channels | direct_messages | ...
    Emoji      string  // shortcode like "orange_book", or ""
    Next       string  // next_channel_section_id; "" = tail
    LastUpdate int64   // last_updated unix; for delta ordering
    IsRedacted bool
    ChannelIDs []string
}

type ChannelSectionUpserted struct {
    ID, Name, Type, Emoji, Next string
    LastUpdate int64
    IsRedacted bool
}
```

The existing `ChannelSection` struct at `client.go:899-905` is
**replaced** by `SidebarSection` (the existing one has the wrong
`channel_ids_page` shape and was never used in production).

### Decoder normalization

A single decoder handles both REST and WS payloads. Custom
`UnmarshalJSON` (or two structs sharing a builder) translates:

- REST `type` ↔ WS `channel_section_type` → `Type`
- REST `last_updated` ↔ WS `last_update` → `LastUpdate`
- REST `channel_ids_page.{channel_ids,count,cursor}` → first page of
  `ChannelIDs` plus a `Cursor` field for follow-up pagination
- WS payloads do not carry `channel_ids_page`; that field is left zero

Tests for the decoder use the four real WS payloads captured during
discovery and the two real REST payloads (Rands and Truelist dumps)
as golden fixtures.

### SectionStore

`internal/service/sectionstore.go` (new). One per workspace, owned by
`WorkspaceContext` in `cmd/slk/main.go`.

```go
type SectionStore struct {
    mu               sync.RWMutex
    ready            bool
    sectionsByID     map[string]*SidebarSection
    channelToSection map[string]string // channelID → sectionID
    lastBootstrap    time.Time         // for reconnect debounce
}

func (s *SectionStore) Bootstrap(ctx, client) error
func (s *SectionStore) Ready() bool
func (s *SectionStore) ApplyUpsert(ev ChannelSectionUpserted)
func (s *SectionStore) ApplyDelete(sectionID string)
func (s *SectionStore) ApplyChannelsAdded(sectionID string, channelIDs []string)
func (s *SectionStore) ApplyChannelsRemoved(sectionID string, channelIDs []string)
func (s *SectionStore) OrderedSections() []*SidebarSection // walks linked list, applies filter
func (s *SectionStore) SectionForChannel(channelID string) (sectionID string, ok bool)
func (s *SectionStore) DisplayMeta(sectionID string) (name, emoji string, ok bool)
```

`OrderedSections` walks the linked list head-first, applies the
filter rules (type whitelist, exclude redacted, exclude empty
`recent_apps`), and returns the result. Cycle detection: if the walk
visits a section it's already seen, log a warning and stop.
Multiple-head detection: if the reverse-pointer set yields zero or
multiple candidate heads, log a warning and pick the section with
the highest `LastUpdate` as a heuristic head.

### Bootstrap flow

In `connectWorkspace` (`cmd/slk/main.go:981`), after the existing
`client.Connect`:

1. Read effective `use_slack_sections` for this workspace. If
   `false`, skip; `WorkspaceContext.SectionStore = nil`. Done.
2. Construct `SectionStore`. Call `Bootstrap(ctx, client)`:
   1. `client.GetChannelSections(ctx)` (returning all sections with
      first page of channels). Loop on top-level `cursor` until
      drained.
   2. For each section with non-empty `channel_ids_page.cursor`,
      page through additional channels until `len(ChannelIDs) >=
      count`. *(Endpoint shape TBD — see Open Questions.)*
   3. Build `channelToSection` index.
   4. Set `ready = true`.
3. On error: log a warning to `/tmp/slk-debug.log`, leave
   `SectionStore` in `ready = false` state. Sidebar falls through to
   config-glob behavior automatically.

### Reconnect handling

On every `OnConnect` (the `hello` WS event), re-call `Bootstrap`.
Cheap insurance against missed events during disconnect. The store
swaps `sectionsByID` and `channelToSection` atomically under the
write lock so the sidebar never sees a half-rebuilt state.

Debounce: skip re-bootstrap if the previous successful one finished
< 30 s ago. Prevents thunder during a flapping connection.

### WS event handling

`internal/slack/events.go` extensions:

- `EventHandler` gains four methods:
  - `OnChannelSectionUpserted(ev ChannelSectionUpserted)`
  - `OnChannelSectionDeleted(sectionID string)`
  - `OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string)`
  - `OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string)`
- `dispatchWebSocketEvent` switch grows four cases that decode and
  call the corresponding handler.

`WorkspaceContext` (in `cmd/slk/main.go`) implements each by
forwarding to `SectionStore` and posting a `tea.Cmd` that triggers
the existing sidebar rebuild path (the same one used today on
`mpim_open` etc.).

### Last-write-wins

`SectionStore.ApplyUpsert` compares `ev.LastUpdate` against the
stored section's `LastUpdate`; ignores events strictly older than
the stored value. Channel add/remove events apply order-of-arrival
without monotonicity protection — they self-correct on the next
`Bootstrap` call (next reconnect or a manual re-trigger). Acceptable
for the rare reorder race.

### Sidebar integration

`cmd/slk/channelitem.go:58` — replace the single `cfg.MatchSection`
call with a tiered resolver:

```go
section := ""
if store := wctx.SectionStore; store != nil && store.Ready() {
    if id, ok := store.SectionForChannel(ch.ID); ok {
        section = id
    }
}
if section == "" {
    section = cfg.MatchSection(teamID, ch.Name)
}
// section is either a Slack section ID, a config section name,
// or "" (falls into default DM/Apps/Channels bucketing).
```

`ChannelItem.Section` semantics expand: opaque ID when populated by
the store, name when populated by config-glob match, "" for default
bucketing. The sidebar resolves display metadata via a new
`SectionsProvider` interface injected into `internal/ui/sidebar`.

`internal/ui/sidebar/model.go`:

- New `SectionsProvider` interface: `OrderedSlackSections() []SectionMeta`,
  `DisplayMeta(id) (name, emoji string, ok bool)`. The model gets one
  injected at construction; nil means "no Slack sections, use existing
  behavior".
- `orderedSections(items, filtered)` (lines 64-124) becomes a
  two-mode function:
  - **Slack mode** (provider non-nil and returns non-empty list):
    return the provider's order verbatim, filtering to sections that
    have at least one item assigned (or are `standard`-type).
    The default DM/Apps/Channels buckets render at whatever position
    Slack places them.
  - **Config mode** (existing behavior): unchanged.
- Two collapse-state maps: `collapseByName` (existing) and
  `collapseByID` (new). The Slack-mode header rendering keys by ID;
  the config-mode keys by name. Default collapse-on-startup behavior
  for the `channels`-type section is preserved.
- Header label rendering (`renderSectionHeaderLabel`,
  lines 1120-1139) gets emoji prefix support: `▾ 📙 Books`. Emoji
  resolves via the existing shortcode resolver; falls back to raw
  `:shortcode:` text if unresolved.

### DM and Apps default bucketing

The existing `sectionFor(item)` logic at
`internal/ui/sidebar/model.go:44-55` only fires when `item.Section
== ""`. Because the Slack store may assign a DM to a custom section,
this is already correctly conditional and needs no change.

When a `recent_apps`-type section exists in the Slack list AND it
contains items, the sidebar renders it as-is (named whatever the
user named it). When it's empty, slk's existing Apps section logic
takes over (default "Apps" header for app-type items with empty
section).

## Testing

### Unit tests

- `SectionStore.Bootstrap` — mock client returning paginated section
  list (top-level cursor) and per-section channel pages
  (`channel_ids_page.cursor`); verify final `sectionsByID`,
  `channelToSection`, and `ready` state.
- `SectionStore.OrderedSections` — linked-list walk; correct order;
  cycle protection; orphan tolerance; filter rules (type whitelist,
  redacted, empty `recent_apps`, empty `standard` retained).
- `SectionStore.ApplyUpsert` last-write-wins with stale `LastUpdate`.
- `SectionStore.ApplyChannelsAdded/Removed` — channel moving
  sections via remove-then-add; channel landing in two sections
  simultaneously is impossible because `channelToSection` is 1:1
  (verify add overwrites, mirroring Slack's own model).
- WS event dispatch — golden tests using the four real captured
  payloads.
- REST decode — golden tests using the two real workspace dumps.
- Channel resolver in `channelitem.go` — three branches: store ready
  + match, store ready + miss (falls to glob), store nil (glob).

### Integration tests

- Sidebar render against a fixture matching the Truelist dump:
  verify section order honors linked-list (custom sections above
  Channels, then DMs, then Recent Apps), emoji prefix renders,
  empty `standard` section renders.
- Sidebar render with `SectionsProvider = nil`: verify identical
  output to today's behavior (regression guard).

### Manual verification

- `SLK_DEBUG_WS=1 SLK_DEBUG=1 slk` against a real workspace; reorder /
  rename / move channels in the official client; verify slk's sidebar
  matches within a couple seconds.
- `slk --dump-sections` against the same workspace before and after
  to confirm REST state matches.

## Risks

1. **Channel-pagination endpoint shape unknown.** Implementation
   phase 1 captures the network call. Fallback: lazy mode where
   channels 11+ in a section land in the default `channels` bucket
   until the WS layer migrates them via `_upserted` events.
2. **Undocumented API surface.** Documented in README. Two
   independent failure modes (REST endpoint, WS events). Both fall
   back to config-glob behavior on failure.
3. **Reverse-engineered field naming may shift.** Slack could rename
   `channel_section_type` → `type` in a WS event push, or vice
   versa. The decoder accepts both for forward compatibility; if
   neither matches, the dispatch silently drops the event and the
   `SLK_DEBUG_WS=1` capture will surface the new shape.
4. **Reconnect storms.** 30 s debounce on `Bootstrap`.
5. **Existing config-glob users surprised.** One-line info log at
   bootstrap when both Slack sections enabled AND config globs
   defined. README note in the channel-sections section.
6. **`stars` / `slack_connect` users lose visibility on
   starred/external sections.** Acceptable for v1; v2 follow-up.

## Open Questions

1. **Channel-pagination endpoint.** Likely `users.channelSections.list`
   with `?channel_section_id=X&cursor=Y`, or a separate
   `users.channelSections.listChannels`. Resolved by capturing the
   official client's network call when expanding a section >10
   channels. If neither pattern produces correct results, ship v1
   with lazy fallback.

2. **Does the WS broadcast `channel_section_upserted` events for
   every section on initial `hello`?** Probably not, but worth
   confirming; if yes, we could potentially skip parts of
   `Bootstrap`. Doesn't change the design — `Bootstrap` is the
   authoritative path.

## Out of Scope (v2 candidates)

- Read+write: keybinding to move selected channel to a different
  section, plus the underlying mutation endpoint.
- Surfacing `stars`, `slack_connect`, `salesforce_records`,
  `agents` section types.
- Inline workspace-custom emoji image rendering in section headers.
- Persisting sidebar collapse state across restarts.
- Creating / renaming / deleting sections from inside slk.
