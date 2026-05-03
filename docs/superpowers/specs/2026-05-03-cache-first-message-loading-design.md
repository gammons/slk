# Cache-first message loading

## Problem

slk has an SQLite cache (`internal/cache/messages.go`) that all incoming
messages — both `client.GetHistory` results and live WebSocket events —
are written to. The schema has `GetMessages` and `GetThreadReplies` read
methods, and `internal/service/messages.go` exposes them via
`MessageService`. But in production, `MessageService` is instantiated
and immediately discarded:

```go
// cmd/slk/main.go:338
msgSvc := service.NewMessageService(db)
_ = msgSvc // will wire for send/receive
```

No production code path ever reads messages from SQLite. Every channel
entry — first time, second time, after a workspace switch — calls
`client.GetHistory` over the network. The cache is dead weight on the
read side.

This causes three observable problems:

1. **"Loading messages..." tax on every workspace switch.** Switching
   away from a workspace and back forces another network round-trip
   for the auto-restored channel, even though the cache is fresh
   (live WS messages have been writing through the whole time).
2. **"No messages yet" race.** `WorkspaceSwitchedMsg`
   (`internal/ui/app.go:1822`) calls `messagepane.SetMessages(nil)`
   without `SetLoading(true)`. The follow-up `ChannelSelectedMsg` is
   dispatched as a `tea.Cmd` and lands on a later Bubbletea tick.
   Between ticks the pane renders with `messages == nil &&
   loading == false` → the empty-state branch flashes. Same pattern
   in `WorkspaceReadyMsg`'s first-channel selection on cold start.
3. **No spinner.** "Loading messages..." renders as static text.
   The workspace loading overlay uses a braille spinner
   (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`) that we should reuse for visual consistency.

## Goals

- Channel re-entry — including across workspace switches — renders
  instantly from SQLite when the cache has rows for that channel.
- Authoritative data still comes from Slack: every channel open also
  fires a background `client.GetHistory` to catch any gap (WS reconnect
  blip, missed events). The network response replaces the cached view
  when it arrives.
- The "No messages yet" flash is eliminated for workspace
  switch / cold start.
- "Loading messages..." (channel cold load and "Loading older
  messages..." backfill) animates with the same braille spinner used
  by the workspace loading overlay.
- Same treatment applies to thread reply loading.

## Non-goals

- Smarter merge that preserves scroll position across the network
  refresh. The first-50 anchor is consistent, so wholesale replace
  is acceptable for v1.
- Cache size enforcement, `message_retention_days`, or any other
  cache lifecycle work.
- Reading other entities (channel lists, user directory, sections,
  etc.) from cache — this spec is about messages and thread replies
  only.

## Design

### 1. Persist `raw_json`

The schema already has a `raw_json TEXT` column on `messages`, but the
four call sites that write messages don't populate it. `cache.Message`
captures `Text`, `ThreadTS`, `ReplyCount`, `Subtype`, `EditedAt`, etc.,
but **not** `Files` (image attachments), `Blocks`, or
`LegacyAttachments`. To round-trip messages to cache without losing
inline images and link previews, we serialize the full upstream
`slack.Message` to JSON and store it in `raw_json`.

Call sites to update:

| File:Line | Caller | Source object |
|---|---|---|
| `cmd/slk/main.go:1455` | `fetchChannelMessages` | `slack.Message` from `client.GetHistory` |
| `cmd/slk/main.go:1390` | `fetchOlderMessages` | same |
| `cmd/slk/main.go:1521` | `fetchThreadReplies` | `slack.Message` from `client.GetReplies` |
| `cmd/slk/main.go:1704` | `rtmEventHandler.OnMessage` | reconstructed from WS event fields |

For the WS handler we don't have the raw `slack.Message`. We construct
a synthetic one from the available fields (`channelID`, `userID`, `ts`,
`text`, `threadTS`, `subtype`, `edited`, `files`, `blocks`,
`attachments`) and marshal that. This gives us full fidelity for
WS-delivered messages.

Backwards compatibility: rows already in the DB without `raw_json`
populated render text-only on cache read. They auto-upgrade as soon as
the next history fetch (or a WS edit) writes `raw_json` for them.

### 2. Cache-aware reader: `loadCachedMessages`

A new helper alongside `fetchChannelMessages` in `cmd/slk/main.go`:

```go
func loadCachedMessages(
    db *cache.DB,
    client *slackclient.Client,
    channelID string,
    userNames map[string]string,
    tsFormat string,
    avatarCache *avatar.Cache,
) []messages.MessageItem
```

Signature note: `client` is here only for `client.UserID()` (used to
compute `HasReacted` per reaction) and `client.TeamID()` if needed —
not for any network call.

Implementation:

1. `rows, err := db.GetMessages(channelID, 50, "")` — returns ASC by ts.
2. For each row:
   - `userName, _ := resolveUser(client, row.UserID, userNames, db, avatarCache)`
   - `formatTimestamp(row.TS, tsFormat)` for the display timestamp.
   - `reactions := loadReactionsForMessage(db, channelID, row.TS)` —
     a thin wrapper around the existing
     `db.GetReactions(messageTS, channelID)` (returns
     `[]ReactionRow`) that converts to `[]messages.ReactionItem`
     with `HasReacted` computed against `client.UserID()`.
   - If `row.RawJSON != ""`: `json.Unmarshal` into a local
     `slack.Message` shape and pull `Files`, `Blocks`, `Attachments`
     out via the existing `extractAttachments`, `extractBlocks`,
     `extractLegacyAttachments` helpers.
   - Otherwise: leave those fields nil — text-only render.
3. Build a `messages.MessageItem` per row. Same field mapping as
   `fetchChannelMessages` (including `Edited`, `IsDeleted`,
   `ReplyCount`, `Subtype`, `ThreadTS`).

The helper returns `nil` on any DB error or when no rows match; the
caller treats both as "cache miss".

### 3. Cache-first channel-open flow

Add a new fetcher type and App field:

```go
type ChannelCacheReadFunc func(channelID string) []messages.MessageItem

// in App:
channelCacheReader ChannelCacheReadFunc

func (a *App) SetChannelCacheReader(fn ChannelCacheReadFunc) { ... }
```

Wired in `cmd/slk/main.go wireCallbacks` next to `SetChannelFetcher`,
closing over the active workspace's `db`, `client`, `userNames`,
`tsFormat`, `avatarCache`.

Updated `ChannelSelectedMsg` handler (`internal/ui/app.go:1284`):

```go
case ChannelSelectedMsg:
    a.activeChannelID = msg.ID
    a.messagepane.SetChannel(msg.Name, "")
    a.messagepane.SetChannelType(msg.Type)

    var cached []messages.MessageItem
    if a.channelCacheReader != nil {
        cached = a.channelCacheReader(msg.ID)
    }

    if len(cached) > 0 {
        a.messagepane.SetLoading(false)
        a.messagepane.SetMessages(cached)
    } else {
        a.messagepane.SetLoading(true)
        a.messagepane.SetMessages(nil)
    }

    if a.channelFetcher != nil {
        fetcher := a.channelFetcher
        chID, chName := msg.ID, msg.Name
        cmds = append(cmds, func() tea.Msg {
            return fetcher(chID, chName)
        })
    }
    // ... existing mark-as-read, last-read-ts, etc. ...
```

`MessagesLoadedMsg` handler stays as-is — it always replaces with the
authoritative network response and clears `loading`.

### 4. Race fix on workspace switch / startup

Two-line addition:

- `internal/ui/app.go:1822` (`WorkspaceSwitchedMsg`): insert
  `a.messagepane.SetLoading(true)` immediately before the existing
  `a.messagepane.SetMessages(nil)`. The cache-first lookup will
  happen on the subsequent `ChannelSelectedMsg` tick and may flip
  `loading` back to false; in the meantime the pane renders the
  spinner instead of "No messages yet".
- `internal/ui/app.go:~1953` (`WorkspaceReadyMsg` first-channel
  branch): same pair before dispatching the initial
  `ChannelSelectedMsg`.

Note: in the cache-hit case, the brief `loading=true` state will be
overwritten on the next tick when `ChannelSelectedMsg` runs and finds
cached rows. That's fine — one spinner frame at most, no flash of
"No messages yet".

### 5. Reusable spinner

Move the rune set out of the `internal/ui` package's private scope
into a shared location:

```go
// internal/ui/styles/spinner.go
package styles

var SpinnerChars = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
```

Update `internal/ui/app.go:3503` and `app.go:3505+` to use
`styles.SpinnerChars`.

Add to `messages.Model`:

```go
spinnerFrame int

func (m *Model) SetSpinnerFrame(f int) { m.spinnerFrame = f }
```

Update the render at `messages/model.go:2017-2020`:

```go
if len(m.messages) == 0 {
    text := "No messages yet"
    if m.loading {
        spinner := string(styles.SpinnerChars[m.spinnerFrame % len(styles.SpinnerChars)])
        text = spinner + " Loading messages..."
    }
    ...
}
```

Same change to the "Loading older messages..." line at
`model.go:1048` / render path at `model.go:2276`.

In `App`'s `SpinnerTickMsg` handler (`app.go:1897-1903`), extend the
gate so the tick keeps firing while the messagepane is loading:

```go
if a.loading || a.messagepane.IsLoading() {
    a.spinnerFrame = (a.spinnerFrame + 1) % len(styles.SpinnerChars)
    a.messagepane.SetSpinnerFrame(a.spinnerFrame)
    cmds = append(cmds, spinnerTick())
}
```

We also need to start the tick when `messagepane.SetLoading(true)` is
first called from a path that didn't already have it running. The
cleanest hook is in the `ChannelSelectedMsg` cache-miss branch and the
race-fix call sites: emit a `spinnerTick()` cmd alongside the existing
work. Idempotent enough — duplicate ticks are filtered by the gate.

Add `messages.Model.IsLoading() bool` if it doesn't already exist.

### 6. Thread reply cache reads

Mirror the channel pattern. New helper:

```go
func loadCachedThreadReplies(
    db *cache.DB,
    client *slackclient.Client,
    channelID, threadTS string,
    userNames map[string]string,
    tsFormat string,
    avatarCache *avatar.Cache,
) []messages.MessageItem
```

Backed by `db.GetThreadReplies(channelID, threadTS)`, same
enrichment as `loadCachedMessages`.

New App field `threadCacheReader ThreadCacheReadFunc` and setter,
wired in `wireCallbacks`. Updated `ThreadOpenedMsg` handler
(`internal/ui/app.go:1620`) follows the same cache-first pattern as
`ChannelSelectedMsg`. The thread panel's loading indicator
(`thread.Model`) gets the same `SpinnerFrame` plumbing as the main
messagepane so its "Loading thread..." text animates consistently.

## Error handling

- DB errors in `loadCachedMessages` / `loadCachedThreadReplies` are
  logged at debug level and treated as cache miss (return nil). The
  caller falls through to the existing spinner+network path.
- `json.Unmarshal` failures on a single `raw_json` blob: log once per
  affected message TS, render that one row text-only. Don't fail the
  whole load.
- Background `MessagesLoadedMsg` always replaces wholesale. Any
  divergence between cached view and network truth is resolved
  within a single round-trip.

## Testing

New tests:

- `internal/cache/messages_test.go`: round-trip a `raw_json` payload
  through `UpsertMessage` and verify it survives `GetMessages`.
- New `cmd/slk/cache_render_test.go` (or similar) for
  `loadCachedMessages` enrichment: insert a message + reactions +
  raw_json with files → produced `MessageItem` has correct
  attachments, reactions with `HasReacted` flagged for the calling
  user, formatted timestamp, resolved username.
- `internal/ui/app_test.go`: test asserting `WorkspaceSwitchedMsg`
  leaves the messagepane in `loading=true` between ticks (closes the
  "No messages yet" race). Same for `WorkspaceReadyMsg`.
- `internal/ui/app_test.go`: test asserting cache-hit on
  `ChannelSelectedMsg` skips `SetLoading(true)` and renders the
  cached items synchronously, then replaces with the network
  response on `MessagesLoadedMsg`.
- Spinner: assertion that `SpinnerTickMsg` keeps firing while
  `messagepane.IsLoading()` is true.
- Thread cache: parallel test for `ThreadOpenedMsg` cache-hit flow.

Existing tests to update:

- Tests that mock `channelFetcher` may need to also mock
  `channelCacheReader` (or accept its absence).
- `app_threads_debounce_test.go` test fixture grows a
  `threadCacheReader` mock.

## File-by-file summary

| File | Change |
|---|---|
| `internal/cache/messages.go` | (no change — schema already has `raw_json`) |
| `cmd/slk/main.go` | Populate `RawJSON` in 4 `UpsertMessage` call sites; add `loadCachedMessages`, `loadCachedThreadReplies`; wire `SetChannelCacheReader` and `SetThreadCacheReader` in `wireCallbacks` |
| `internal/ui/styles/spinner.go` | New file: export `SpinnerChars` |
| `internal/ui/app.go` | New `channelCacheReader` / `threadCacheReader` fields + setters; cache-first branch in `ChannelSelectedMsg` and `ThreadOpenedMsg`; race fix in `WorkspaceSwitchedMsg` and `WorkspaceReadyMsg`; spinner gate extension; use `styles.SpinnerChars` |
| `internal/ui/messages/model.go` | `spinnerFrame int` + `SetSpinnerFrame`; `IsLoading()` if missing; render spinner in empty-state and "Loading older messages..." paths |
| `internal/ui/thread/model.go` | Same spinner plumbing for thread panel loading indicator |
| Tests | New tests as above; mock-fixture updates in existing tests |
