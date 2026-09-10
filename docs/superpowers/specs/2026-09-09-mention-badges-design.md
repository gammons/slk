# Mention Badges

Design for showing per-channel direct-mention counts in the sidebar, so an
unread channel is distinguishable from one that needs the user's attention.

## Problem

slk renders exactly one unread signal: a blue `●` on any channel whose
`has_unread` flag is set. A channel with two hundred unrelated messages and a
channel where someone typed the user's name look identical.

The official Slack client distinguishes these with a numeric badge counting
direct mentions — an explicit `@user`, or a `@here` / `@channel` / `@everyone`
broadcast. slk shows nothing.

The data to fix this is already arriving and being discarded. `client.counts`
returns `mention_count` per channel and per mpim; `GetUnreadCounts`
(`internal/slack/client.go:1027`) copies it into `UnreadInfo.Count`, which no
caller ever reads. The `channels` table declares an `unread_count` column
(`internal/cache/db.go:108`) that nothing writes and nothing selects.

## Goals

- A numeric badge on sidebar rows with unread direct mentions.
- Badge semantics matching the official client, including its DM special case.
- Badges appear as mentions arrive, not only at startup.

## Non-goals

- A mention badge on the synthetic Threads row. `client.counts` already returns
  `Threads.MentionCount` and `internal/bootstrap/bootstrap.go:162` already
  carries it, so the server half is nearly free — but the Threads row is driven
  by a separate in-memory counter (`internal/ui/sidebar/model.go:275`) rather
  than the read-state DB, so it needs its own increment path and its own tests.
  Deferred to keep this change to one coherent mechanism.
- Mention-aware navigation (a "jump to next mention" key alongside `a` / `A`).
- Mention counts in the window title or workspace rail.

## Semantics

Slack badges DMs by *message* count and channels by *mention* count. This is
not two rules requiring a client-side branch: Slack's server already encodes
both in one field. In `client.counts`, `mention_count` means "@-mentions" for
`channels`, and "all unread messages" for `ims` and `mpims`. Consuming that
single field uniformly reproduces Slack's behaviour exactly.

**This paragraph is the design's load-bearing assumption and it is unverified.**
See Risks below: no capture in this repo proves what `ims` carries. Every
statement of these semantics — in code comments, in doc comments, in tests —
must carry that caveat, because the three sentences above are what implementers
copy.

The local-increment path (below) must therefore branch on conversation type to
stay consistent with the server value it will later be overwritten by.

### Conversation types: Slack's three kinds vs slk's five

Slack's `client.counts` has three blocks: `channels`, `mpims`, `ims`. slk's
`ChannelItem.Type` has five values: `channel`, `private`, `dm`, `group_dm`,
`app`. The mapping is not one-to-one, and the gap is load-bearing:

| slk `Type` | `client.counts` block | Local increment rule |
|---|---|---|
| `channel`, `private` | `channels` | `mention.InText` |
| `group_dm` | `mpims` | every message |
| `dm` | `ims` | every message |
| **`app`** | **`ims`** | **every message** |

`app` is the trap. `buildChannelItem` classifies an `is_im=true` conversation
as `app` when the peer is a bot and `dm` otherwise — a distinction slk invents
for sidebar grouping and Slack does not make. Both land in the `ims` block and
both get all-unread semantics. Any local rule that tests for `dm` or `group_dm`
without also testing `app` will badge app DMs from the server at boot and then
never increment them live.

### Mute

`ChannelMuted` (`internal/ui/styles/styles.go:94-100`) documents that muted
channels "never get the blue `•` indicator, even when their UnreadCount is
non-zero", and `ChannelItem.IsVisiblyUnread`
(`internal/ui/sidebar/model.go:54-61`) enforces it.

Mentions pierce mute. A muted channel stays grey and unbolded for ordinary
traffic but still badges on an explicit mention — that escape hatch is what
makes muting safe to use on a busy channel. The existing invariant is narrowed
to scope its suppression to the dot only; the comment at `styles.go:94-100` is
amended in the same commit so code and comment do not diverge.

## Approach

Server counts are authoritative; local detection fills the gap between
refreshes.

`client.counts` is called only at boot
(`cmd/slk/bootstrap_adapters.go:100`) and on reconnect
(`cmd/slk/reconnect_sync.go:129`). A session that stays connected all day would
never update a badge, so server data alone cannot satisfy the third goal.
Incoming `message` WebSocket events therefore increment the count locally.

The error term of this hybrid is what recommends it. Local detection can only
*undercount*, only on `<!subteam^…>` usergroup mentions, and the error is
erased the moment the user reads the channel or reconnects. The unread dot
still appears regardless, so no message is ever missed outright — the worst
case is a dot where Slack would have shown `1`.

The rejected alternatives:

- **Server-only.** Provably correct, but badges update only at boot and
  reconnect. Fails the liveness goal outright.
- **Local-only.** Re-implements semantics the server already computes, and
  drifts permanently with no correction path.
- **Adding a periodic `client.counts` poll.** Would also correct usergroup
  drift, at one extra API call per interval. Rejected because this codebase
  deliberately avoids chatty endpoints — `internal/slack/client.go:31-35`
  records that `conversations.list` and `users.list` are never called at all.
  Reconsider only if usergroup undercounting proves annoying in practice.

## Data model

### Storage

Add to the `channels` table, via the existing `addColumnIfMissing` migration
helper (`internal/cache/db.go:223`):

```sql
mention_count INTEGER NOT NULL DEFAULT 0
```

Remove the dead `unread_count` column from the `CREATE TABLE` statement.
Nothing reads or writes it, and keeping two similarly-named count columns
invites a future author to pick the wrong one. Existing databases retain the
vestigial column harmlessly; no destructive migration is performed.

### Types

`cache.ReadState` and `cache.ChannelReadStateUpdate`
(`internal/cache/channels_read_state.go:11-23`) each gain a mention field.

`UpdateChannelReadState` keeps its current signature. Mention count is written
through two dedicated methods instead:

```go
func (db *DB) SetChannelMentionCount(channelID string, n int) error
func (db *DB) IncrementChannelMentionCount(channelID string) error
```

Three verbs are needed — set, increment, and leave alone — but only two are
operations. "Leave alone" is declining to call, not a parameter value. Encoding
it as one would have forced a positional argument onto
`UpdateChannelReadState`, whose four production callers are outnumbered by
roughly twenty test callers in `internal/cache/channels_read_state_test.go` and
`cmd/slk/event_handler_marked_test.go`. Those edits would be mechanical, would
change no behaviour, and would bury the real diff.

`ChannelReadStateUpdate` gains a plain `MentionCount int`. Both batch writers
are fed exclusively from `client.counts`, so batch semantics are
unconditionally "set" and need no operation field. `ReplaceWorkspaceReadState`
extends its workspace-wide reset to zero `mention_count` alongside
`has_unread`.

`IncrementChannelMentionCount` is expressed as
`mention_count = mention_count + 1` so it is atomic and cannot lose a
concurrent update.

The cost: `OnMessage` and `markChannelReadAsync` each issue two single-row
UPDATEs rather than one. Both target the same row, SQLite serialises them, and
because `MentionBadge` gates on `HasUnread` a reader landing between them
cannot observe the intermediate state.

### Transport types

`slack.UnreadInfo.Count` (`internal/slack/client.go:934`) and its restatement
`bootstrap.Unread.Count` (`internal/bootstrap/bootstrap.go:148`) currently hold
a value that is not a count of anything: `client.go:1026-1031` sets it to
`mention_count`, then floors it to `1` whenever `has_unreads` is true and
`mention_count` is `0`. Nothing reads it, so the fabrication has never mattered.

Both fields are renamed to `MentionCount` and carry the server's raw value with
no flooring. The `ims` block, which currently hardcodes `Count = 1`
(`client.go:1051-1053`), parses and forwards `mention_count` like the other two
blocks. This is a rename of a dead field rather than a behaviour change, and it
is what makes the boot and reconnect rows of the table below meaningful.

### Writes

| Trigger | Write |
|---|---|
| `client.counts` at boot → `ReplaceWorkspaceReadState` | `MentionCount` per entry; workspace-wide reset zeroes channels absent from the snapshot |
| `client.counts` on reconnect → `BatchUpdateChannelReadState` | `MentionCount` per entry |
| `*_marked` WS event → `OnChannelMarked` | `SetChannelMentionCount` from the event's `mention_count` |
| `markChannelReadAsync` — user reads a channel | `SetChannelMentionCount(id, 0)` |
| `MarkUnread` — the `u` key | `SetChannelMentionCount(id, 0)` |
| Incoming `message`, mention detected | `IncrementChannelMentionCount` |
| Incoming `message`, no mention | no call |

### Reads

Unchanged. `GetWorkspaceReadState` selects one additional column. The sidebar's
existing `readStateReader` closure (`cmd/slk/main.go:1353`) already delivers a
`map[string]cache.ReadState` to the renderer on every build, so the new field
reaches the UI with no new plumbing.

## Mention detection

### Reuse, do not reimplement

`notify.ShouldNotify` (`internal/notify/notifier.go`) already contains the
predicate:

```go
strings.Contains(text, "<@"+ctx.CurrentUserID+">") ||
strings.Contains(text, "<!here>") ||
strings.Contains(text, "<!channel>") ||
strings.Contains(text, "<!everyone>")
```

It is embedded in a larger policy that also weighs DND, mute, keyword triggers
and the active channel. Per AGENTS.md — *extract the substrate, not the widget*
— the uniform part is "does this text mention me?"; the divergent parts are the
policies each consumer wraps around it.

Extract to a new `internal/mention` package:

```go
func InText(text, selfUserID string) bool
```

`notify.ShouldNotify` is refactored to call it, keeping its policy layer
intact. A new package rather than an exported `notify.IsMention` because
read-state code importing a package documented as "desktop notification
support" would misrepresent the dependency, and both consumers depend on the
predicate equally rather than one owning it.

`internal/mention.InText` is added to the AGENTS.md shared-code table in the
same commit, per that file's own convention.

### Increment rule

Evaluated in `rtmEventHandler.OnMessage`, immediately beside the existing
`has_unread` write at `cmd/slk/main.go:4037-4048`, so the two decisions cannot
drift apart:

```
increment when:
      author != self
  AND shouldMarkChannel               (existing thread-reply gate)
  AND activeChannelID != channelID    (existing active-channel gate)
  AND ( conversation is dm/group_dm  →  always
      | otherwise                     →  mention.InText(text, selfUserID) )
```

Three of the four clauses are already local variables at that call site.

Reusing `shouldMarkChannel` means a mention inside a non-broadcast thread reply
does not badge its parent channel. This matches Slack, where thread mentions
surface in the Threads section instead.

### Known gaps

Both self-heal on the next server refresh:

- **Usergroup mentions are not detected.** `<!subteam^S…>` requires knowing the
  user's own group memberships. `boot.Subteams.Self`
  (`internal/slack/boot/boot.go:203-215`) is deliberately typed as
  `[]json.RawMessage` because no capture with a non-empty list has ever been
  observed. Undercount only.
- **Per-channel notification prefs are not modelled.** A channel configured to
  suppress `@channel` / `@here` still increments locally. Overcount only.

## Rendering

### Predicate

A sibling to `IsVisiblyUnread`, preserving that method's documented role as the
single source of truth for its rule:

```go
func (item ChannelItem) MentionBadge(state cache.ReadState) int
```

Returns the count to display, or `0` for no badge. Unlike `IsVisiblyUnread` it
does not consult `IsMuted`. It does require `state.HasUnread`: a channel with
no unreads never badges, so a stale non-zero `mention_count` cannot outlive the
unread flag that justifies it.

The `99+` cap is applied at render time only. The database stores the true
count, so a later refresh that lowers the count below 100 shows the real
number rather than a value already clamped on the way in.

### Layout

The badge replaces the trailing dot in `buildCache`
(`internal/ui/sidebar/model.go:1331-1345`). A row shows either a dot (unread,
no mentions) or a badge (unread with mentions), never both — matching Slack and
costing no additional glyph slot in the row layout.

Counts display as `99+` above 99, as Slack's do, bounding the badge at three
characters.

### Collapsed section headers

A collapsed section header renders an aggregate — the count of
channels-with-unreads inside it, via `IsVisiblyUnread`. That count cannot
distinguish chatter from an @-mention, which is the one distinction this
feature exists to make.

It matters because of a default: `sidebar.New()` starts `defaultChannelsSection`
and `defaultAppsSection` collapsed in config mode, so the channel firehose — the
place a mention is most easily lost — is exactly where the badge is invisible.
Slack-sections mode is unaffected, since `collapseByID` starts nil and nothing
is collapsed; DMs default expanded on both paths.

The header therefore carries **two** independent figures, never merged:

```
 ▸ Channels •5 ▐3▌
```

- `•5` — five channels in this section have unreads. Unchanged meaning,
  unchanged glyph, still `IsVisiblyUnread`.
- `▐3▌` — three unread direct mentions across the section, summed from
  `MentionBadge`. Rendered with `MentionBadgeStyle()`, capped at `99+`, and
  omitted entirely at zero.

Summing mention counts rather than counting mentioned channels is deliberate:
at row level the badge answers "how many times was I named here", and a section
header that answered a different question with the same glyph would teach the
user the wrong thing.

Because `MentionBadge` ignores mute, a muted channel contributes to the
mention figure but not to the unread figure. That asymmetry is the same one
the rows already show, and it is the point: muting silences chatter, not
someone naming you.

### Width budget

`model.go:1375-1383` computes `maxNameLen` from a hardcoded budget of
`cursor(2) + prefix(3) + name + space(1) + dot(2) = name + 8`, assuming
worst-case two-column rendering for ambiguous-width glyphs.

That computation is already inside the per-item loop, so it can account for the
current row's actual badge width rather than a global worst case. Rows without
mentions keep exactly today's name width; only badged rows surrender columns.
The bare `8` becomes a named constant derived from the badge's maximum width —
a magic number that already required a five-line comment to justify.

### Style

The theme's highlight pair is `SelectionBackground` / `SelectionForeground`,
which `Apply()` guarantees is populated and contrast-safe on every theme,
deriving `Primary`-on-`Background` when a theme omits it.

```go
func MentionBadgeStyle() lipgloss.Style {
    return lipgloss.NewStyle().
        Background(SelectionBackground).
        Foreground(SelectionForeground)
}
```

A function, not a package var, because `UnreadBadge` demonstrates the failure
mode: it is defined twice, once as an init-time var (`styles.go:106-109`) and
again inside `buildStyles()` (`:457-458`). Every var-shaped style must be
remembered in both places, and the init-time copy reads `Selection*` while it
is still nil (those two are declared without initializers at `:36-37`).
`Apply()` populates them at `:348-361` before calling `buildStyles()` at
`:395`, so the `buildStyles` copy would be correct — but a function has a
single definition and cannot go stale by omission at all. This is why
`SelectionStyle()` and `SearchHighlightStyle()`
(`internal/ui/styles/styles.go:508-521`) are already functions.

`UnreadBadge` is **kept**. It is live: `internal/ui/statusbar/model.go:250`
renders the status bar's " N unread " badge with it. An earlier draft of this
document called it unused and proposed deleting it; that was wrong, and
deleting it would break the build.

### Recorded, out of scope: UnreadBadge contrast

`UnreadBadge` hardcodes `Foreground(lipgloss.Color("#FFFFFF"))` over
`Background(Error)` (`styles.go:457-458`). On a theme whose `Error` is pale,
white-on-error is unreadable. `WorkspaceActive` (`:443-445`) and `StatusMode`
(`:469-470`) hardcode the same literal white over `Primary`.

This is a pre-existing defect in the status bar and workspace rail, not
something this feature introduces or touches. Per AGENTS.md — *found a bug
while refactoring? Record it, annotate it, raise it separately* — it is noted
here and left alone. The mention badge avoids the whole class by using the
theme's own contrast-guaranteed `Selection*` pair.

### Inline ANSI

The badge emits a background color mid-row, so it must participate in the
existing `ReapplyBgAfterResets` handling at `model.go:1400-1432` exactly as the
styled cursor, prefix and dot glyphs already do.

## Testing

Plain `testing.T`, stdlib only, white-box, per repo convention.

- **`internal/mention`** — table test: self-mention; each broadcast form; a
  `<@OTHERUSER>` non-match; a substring near-miss (`<@U123ABC>` when self is
  `U123`) proving the angle bracket is required; empty self ID.
- **`internal/notify`** — the existing `ShouldNotify` tests must pass unchanged.
  That is the regression proof the extraction preserved behaviour.
- **`internal/cache`** — `SetChannelMentionCount` writes;
  `IncrementChannelMentionCount` accumulates across repeated calls and starts
  from zero on a fresh row; neither disturbs `last_read_ts` or `has_unread`.
  `ReplaceWorkspaceReadState` zeroes `mention_count` for channels absent from
  the snapshot. Migration adds the column to a pre-existing database, mirroring
  the column-type assertion at `internal/cache/db_test.go:51-88`.
- **`cmd/slk`** — `OnMessage` increments for a DM and for a channel mention;
  does *not* increment for a self-authored message, a non-mention, a plain
  thread reply, or the active channel. `OnChannelMarked` sets the count from the
  event payload. Extends the existing `event_handler_marked_test.go` pattern.
- **`internal/ui/sidebar`** — badge replaces the dot; a muted channel badges but
  stays unbolded; the `99+` cap; the per-row width budget shrinks the name only
  on badged rows.

## Risks

**`mention_count` on `*_marked` events is unverified.** `wsChannelMarkedEvent`
(`internal/slack/events.go:193-198`) parses only `unread_count_display`, and the
fixture at `events_test.go:447` carries only that field. Real Slack payloads are
believed to include `mention_count`, but this repo has no capture proving it.

**`mention_count` on the `ims` block of `client.counts` is unverified.** The
current parser (`client.go:994-998`) omits the field for IMs, hardcoding
`Count = 1` instead. The DM semantics described above depend on Slack populating
it.

Both degrade gracefully: if the field is absent it decodes as `0`, and
decrements still work because reading a channel zeroes the count locally via
`markChannelReadAsync`. The implementation plan must capture a real payload for
each before relying on them, and fall back to a documented alternative if
either is missing.

## Out of scope, revisit later

- Threads-row mention badge (see Non-goals).
- A periodic `client.counts` poll to correct usergroup undercounting.
- Typing `boot.Subteams.Self` to enable local usergroup mention detection, which
  requires a capture with a non-empty list.
