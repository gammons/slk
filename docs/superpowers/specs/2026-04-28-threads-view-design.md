# Threads View Design Spec

Date: 2026-04-28

## Overview

Add a "Threads" view to slk that lists all threads the user is involved in
across the active workspace, mirroring Slack's official desktop client. The
view appears as a top-level entry above the channel sidebar. Selecting it
replaces the message pane with a scrollable list of thread parents; the
existing thread side panel auto-opens on the highlighted row so replies are
visible (and replyable) inline.

This spec covers v1, which is **cache-based and per-workspace**. A follow-up
v2 will replace the local query with the undocumented internal endpoint
(`subscriptions.thread.*`) and wire `thread_marked` / `thread_subscribed`
WebSocket events for accurate unread state.

## Goals

- Browse and reply to active threads without navigating to their channels.
- Live updates: new replies re-rank the list and update unread counts.
- No SQLite migration; no reverse engineering of internal Slack endpoints.
- Reuse the existing thread side panel and message rendering pipeline.

## Non-Goals (v1)

- Calling `subscriptions.thread.*` (deferred to v2).
- Handling `thread_marked` / `thread_subscribed` WebSocket events (v2).
- Cross-workspace aggregation.
- "Mark all threads read" action.
- Filters (only-unread, only-mentions, etc.).
- Subscribing to a thread the user has not replied to.

## UX

### Sidebar entry

A synthetic top item is rendered above the first sidebar section, in every
workspace's channel sidebar:

```
⚑ Threads  •3
─────────────
Starred
  # general
Channels
  # eng-platform
  ...
```

The badge `•3` shows the count of unread threads. The entry participates in
normal `j/k` navigation; it is the topmost item in the sidebar list.

### Threads view

Pressing `Enter` on the `Threads` entry activates the view: the message pane
is replaced with a scrollable list of thread parents, ordered
`(unread DESC, last_reply_ts DESC)`. Each row is a compact card:

```
#eng-platform · alice · 2:14 PM    •
  > Anyone seen the deploy stall? I'm seeing 502s on...
  6 replies · last by bob 12m ago

#design · me · yesterday
  > here's the spec for the new onboarding flow
  3 replies · last by carol 4h ago
```

- Channel glyph (`#`, `◆`, `●`, `○`) follows the existing sidebar conventions.
- The trailing `•` on the first row is the unread marker; absent on read rows.
- The preview is the parent message text, ANSI-rendered with the same
  markdown / emoji / mention pipeline used in the message pane, then
  truncated to one line.

The right-side thread panel (existing 35% panel) opens automatically and
follows the highlighted row: as `j/k` moves the cursor, the right panel
updates to show that thread's replies.

### Compose

The bottom compose box is hidden in this view. Replies are sent via the
existing thread panel's compose (focus moves to it on `Enter` from the
list — see Keybindings).

### Returning to channels

Highlighting any channel in the sidebar (via `j/k` or fuzzy finder or
workspace switch) deactivates the threads view and restores the normal
messages pane. The right thread panel closes per its existing rules
(channel-switch behavior is unchanged).

## Component Architecture

### New package: `internal/threads/`

Pure data layer. No UI concerns.

```go
type Involvement uint8
const (
    InvolvementAuthored Involvement = 1 << iota
    InvolvementReplied
    InvolvementMentioned
)

type Summary struct {
    ChannelID    string
    ChannelName  string
    ChannelKind  cache.ChannelKind   // public/private/dm/mpim
    ThreadTS     string
    ParentUserID string
    ParentUserName string
    ParentText   string              // raw, rendered+truncated by UI
    ParentTS     time.Time
    ReplyCount   int
    LastReplyTS  time.Time
    LastReplyBy  string              // user ID; UI resolves to display name
    Unread       bool
    Involvement  Involvement
}

type Service struct { /* db handle, channel-meta accessor */ }

func (s *Service) ListInvolved(ctx context.Context, workspaceID, selfUserID string) ([]Summary, error)
```

`ListInvolved` issues one SQL query against the existing `messages` table,
joins channel metadata in Go, computes `Unread` against the cached
`channels.last_read` column, and returns the slice sorted
`(Unread DESC, LastReplyTS DESC)`.

#### SQL sketch

```sql
WITH involved AS (
  SELECT DISTINCT thread_ts, channel_id
  FROM messages
  WHERE thread_ts != ''
    AND (
          user_id = :self                              -- authored or replied
       OR text LIKE '%<@' || :self || '>%'             -- @-mentioned
    )
)
SELECT
  m.channel_id,
  m.thread_ts,
  -- parent
  (SELECT user_id FROM messages WHERE channel_id = m.channel_id AND ts = m.thread_ts) AS parent_user,
  (SELECT text    FROM messages WHERE channel_id = m.channel_id AND ts = m.thread_ts) AS parent_text,
  -- aggregate over replies (excludes the parent: ts > thread_ts)
  COUNT(*) FILTER (WHERE m.ts > m.thread_ts) AS reply_count,
  MAX(m.ts)                                  AS last_reply_ts,
  -- last replier
  (SELECT user_id FROM messages
     WHERE channel_id = m.channel_id AND thread_ts = m.thread_ts
     ORDER BY ts DESC LIMIT 1)               AS last_reply_by
FROM messages m
JOIN involved i USING (thread_ts, channel_id)
GROUP BY m.channel_id, m.thread_ts;
```

The existing `idx_messages_thread` on `(thread_ts, channel_id)` covers both
the filter and the group-by. For typical mailboxes (hundreds of involved
threads) the query runs in under a millisecond.

#### Unread heuristic

`Unread = (LastReplyTS > channels.last_read[ChannelID]) && (LastReplyBy != selfUserID)`

This is approximate (it conflates "unread thread" with "unread channel
activity in that thread") but is good enough for v1. v2 replaces this with
the authoritative subscription state from Slack.

#### Edge cases

- **Parent not in cache** (we have replies but no parent row): `parent_user`
  and `parent_text` come back NULL. Render as `(parent not loaded)`. The UI
  triggers a lazy `conversations.replies` fetch when the row is highlighted,
  which populates the parent in cache and re-renders on next tick.
- **Mention false positives**: SQL `LIKE` matches `<@USERID>` substring,
  which is the canonical Slack mention syntax. Plain-text occurrences of the
  user ID (without `<@…>`) are not matched. This is intentional and matches
  how the UI renders mentions.

### New package: `internal/ui/threadsview/`

UI layer. Mirrors the existing `internal/ui/sidebar/` and `internal/ui/thread/`
packages: a `Model` struct, no `tea.Model` interface, pointer-receiver
setters, `View(height, width int) string`.

**State:**

| Field | Type | Purpose |
|-------|------|---------|
| `summaries` | `[]threads.Summary` | The list as last computed |
| `selected` | `int` | Highlighted row |
| `scrollOffset` | `int` | Viewport scroll |
| `focused` | `bool` | Keyboard focus |
| `version` | `int64` | Bumped on any state change for cache invalidation |
| `userNames` | `func(userID string) string` | Injected display-name resolver |

**Methods:**

- `SetSummaries(s []threads.Summary)` — replace contents; preserves
  `selected` by `(channel_id, thread_ts)` when possible.
- `MoveUp()` / `MoveDown()` / `Top()` / `Bottom()`
- `Selected() (channelID, threadTS string, ok bool)`
- `View(height, width int) string`
- `Version() int64`
- `SetFocused(bool)`

**Rendering:** Each row uses `messages.RenderSlackMarkdown` and
`messages.WordWrap` to render the parent preview with the same fidelity as
the message pane, then truncates to one line with the existing reflow
helpers. Channel glyph and timestamp formatting follow the conventions
already used in `internal/ui/sidebar/`.

### Sidebar changes

`internal/ui/sidebar/model.go` gains a single synthetic top item rendered
above the first section. It is selectable via `j/k` like a channel item;
its activation (`Enter`) emits a new `tea.Msg` (`ThreadsViewActivatedMsg`)
rather than the existing channel-selection message. Sidebar exposes:

- `ThreadsUnreadCount() int` (read by the renderer for the badge)
- `SetThreadsUnreadCount(int)` (called by App when the threads model updates)

### App wiring (`internal/ui/app.go`)

New view-mode field:

```go
type View int
const (
    ViewChannels View = iota
    ViewThreads
)
```

`(*App).View()` is updated so that when `view == ViewThreads`, the
message-pane region renders the `threadsview.Model` instead of
`messages.Model`, and the bottom compose region is omitted (the right
thread panel's compose is the only entry point).

New tea.Msg types:

- `ThreadsViewActivatedMsg{}` — emitted by sidebar when the Threads entry is
  Entered; App switches `view = ViewThreads` and triggers an initial load.
- `ThreadsListLoadedMsg{summaries []threads.Summary}` — async result from
  `threads.Service.ListInvolved`.
- `ThreadsListDirtyMsg{}` — internal kick to re-run the query (debounced).

Channel selection (any sidebar channel `Enter`) sets `view = ViewChannels`
and proceeds with the existing channel-open flow.

Per-workspace state lives on the existing `workspaceState`-equivalent struct
already used to hold per-workspace UI models. The `threadsview.Model`,
`threads.Service`, and last-loaded summaries are all per-workspace.

### Live updates

The existing `OnMessage(workspaceID, msg)` handler in `app.go` already routes
WebSocket message events to the right workspace. We add: if
`msg.ThreadTS != ""` (regardless of whether the threads view is currently
visible), enqueue a `ThreadsListDirtyMsg` for that workspace. A short
(150 ms) debouncer collapses bursts of replies into one re-query.

The threads view's `Version()` increments on `SetSummaries`, which
invalidates `panelCacheMsgPanel` for the threads-view slot the same way the
sidebar/messages caches already work.

The unread badge on the sidebar entry updates from the same query result —
no separate count maintenance.

## Keybindings

| Key | Context | Action |
|-----|---------|--------|
| `j`/`k` | Sidebar (incl. Threads row) | Move highlight |
| `Enter` | Sidebar, on Threads row | Activate Threads view |
| `Enter` | Sidebar, on a channel | (existing) Open channel — also exits Threads view |
| `j`/`k` | Threads view focused | Move highlight; right panel auto-updates |
| `Enter` | Threads view focused | Move focus to right thread panel (for replying) |
| `Esc` / `h` | Threads view focused | Return focus to sidebar |
| `Ctrl+]` | Any | (existing) Toggle thread panel |
| `gg` / `G` | Threads view focused | Top / bottom of list |

No new global keybindings. Discoverability comes from the visible sidebar
entry.

## Data flow

1. App starts → for each workspace, `threads.Service` is constructed with the
   workspace's DB handle.
2. On workspace selection or initial load, App dispatches a goroutine to call
   `ListInvolved`; result arrives as `ThreadsListLoadedMsg`.
3. Sidebar renders the Threads entry with the current unread count from the
   loaded summaries.
4. WS `message` event with `thread_ts` set → `ThreadsListDirtyMsg` → debounced
   re-query → new `ThreadsListLoadedMsg`.
5. User selects Threads entry → `view = ViewThreads`, message pane renders
   `threadsview.Model`, right thread panel opens to the highlighted row.
6. User selects a channel → `view = ViewChannels`, normal flow resumes.

## Testing strategy

### Unit tests

- `internal/threads/service_test.go` against an in-memory SQLite seeded
  using the same helpers as `internal/cache/*_test.go`:
  - Authored-only thread (user posted parent, others replied)
  - Replied-only thread (user replied, did not author)
  - Mentioned-only thread (user neither authored nor replied)
  - Mixed (multiple involvement reasons)
  - Parent missing from cache (lazy-load placeholder)
  - Multi-channel: same `thread_ts` value in different channels treated as
    distinct threads
  - Unread heuristic boundaries (last reply by self, last reply by other,
    `last_reply_ts == last_read`)
  - Ordering: unread before read; within each group, newest first

- `internal/ui/threadsview/model_test.go`:
  - Selection preservation across `SetSummaries`
  - `j/k`, `gg/G`, scroll behavior
  - Snapshot of rendered output (matches the existing UI snapshot pattern)

### Integration tests

- `internal/ui/app_threads_test.go`:
  - Activating the sidebar Threads entry switches `view` and renders the list
  - WS `message` event with `thread_ts` triggers a re-query and re-rank
  - Channel selection deactivates the threads view
  - Multi-workspace isolation: dirtying threads in workspace A does not
    re-query workspace B

## Open questions / risks

- **Mention LIKE-scan cost**: the `text LIKE '%<@USERID>%'` predicate cannot
  use the existing index. For very large caches (hundreds of thousands of
  rows) this could be slow. Mitigation if it materializes: add a
  `mentions` table populated on insert, indexed on `(user_id, thread_ts,
  channel_id)`. Not in v1 unless profiling shows it's needed.
- **Query frequency**: the 150ms debouncer is a guess. If profiling shows
  noticeable jank in busy workspaces, switch to incremental update of the
  in-memory summaries instead of re-running the query.
- **Heuristic unread accuracy**: a thread you have read but where someone
  else later replied in the same channel (outside the thread) won't
  spuriously show as unread, because we compare `LastReplyTS` (thread-only)
  against `channels.last_read`. But a thread can be marked unread by you
  manually in the official client; we have no way to honor that until v2.
