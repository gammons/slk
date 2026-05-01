# Mark Unread

**Status:** Design  
**Date:** 2026-05-01  
**Owner:** slk  

## Problem

slk currently auto-marks-as-read on channel entry (`cmd/slk/main.go:544-567` →
`client.MarkChannel`) and offers no way to mark a message as unread again.
This breaks the common Slack workflow of "I'll come back to this message
later" — once a channel is opened, every message in it is read, and the
`── new ──` landmark vanishes until a new message arrives.

We want to let the user press a key on a selected message (or thread reply)
and roll the read-state watermark backward, so the selected message and
everything newer becomes unread again.

## Goals

- Mark-from-here unread on the selected message in the channel pane and on
  the selected reply in the thread side panel.
- Slack-server-authoritative: write through `conversations.mark` /
  `subscriptions.thread.mark` so other Slack clients (web, mobile,
  another slk) see the change.
- Inbound WS reconciliation: handle `channel_marked` / `im_marked` /
  `group_marked` / `mpim_marked` / `thread_marked` events so slk picks up
  read-state changes made elsewhere without needing a channel switch.
- Visible feedback: `── new ──` landmark moves, sidebar shows accurate
  unread count, brief status-bar toast.

## Non-Goals

- Per-thread durable last-read column in SQLite. Thread unread surfacing
  remains heuristic-driven (`thread.last_reply_ts > channel.last_read_ts
  AND last_reply_by != self`); presentation flip via the existing
  `MarkByThreadTSRead` is good enough for v1.
- Single-message-only flagging. Slack's read-state model is a single
  watermark per channel/thread; we follow that.
- Preventing the existing channel-entry auto-mark-as-read from re-marking
  a manually-unread channel as read on next entry. Matches official Slack
  behavior; "comes back to it" works because the channel stays in the
  unread state until you actually open it again.
- Backfill a missing prior message when the user marks unread on the
  oldest loaded message — fall back to `ts="0"` (Slack's "everything
  unread" sentinel).
- No ownership check (you can mark anyone's message unread, including
  your own). Slack allows this.

## User Experience

### Keybinding

| Key | Mode | Action |
|---|---|---|
| `U` | Normal (message) | Mark selected message and everything newer in this channel/thread as unread |

`U` is currently free in normal mode. `Ctrl+U` (half-page-up) and `Ctrl+u`
(insert-mode clear-compose) are unaffected. Lowercase `u` is intentionally
left free for a possible future "undo" binding.

### Channel pane

When the user presses `U` on selected message X:

- The `── new ──` landmark moves to sit immediately above X.
- The sidebar shows the channel as unread, with badge count =
  `len(loadedBuffer) - indexOfX`.
- A 2-second status-bar toast: `Marked unread`.
- No confirmation prompt (non-destructive).

### Thread side panel

When the user presses `U` on selected reply X inside the thread side panel:

- The thread pane's `── new ──` landmark moves above X (the existing
  `thread.SetUnreadBoundary` already supports this).
- The `⚑ Threads` view's row for this thread flips to `Unread=true` via
  the existing presentation-only `MarkByThreadTSRead(threadTS, false)`
  helper.
- Channel-level read state is **not** touched.
- Same 2-second `Marked unread` toast.

### Edge cases

- **Oldest loaded message:** `boundaryTS = "0"`. Channel becomes
  fully unread.
- **Parent message in channel pane that has thread replies:**
  channel-level mark-unread only. The thread itself is not touched.
- **Selection unavailable:** silent no-op (matches `Edit` / `Delete`).
- **Already-unread message:** still re-issues the call, no harm done.
  Boundary doesn't move backward of where it already is on the server,
  but `conversations.mark` accepts it.

### Failure feedback

- HTTP error from `MarkChannelUnread` / `MarkThreadUnread` → toast
  `Mark unread failed` (mirrors the existing `DeleteFailedMsg`
  pattern). No local state change is committed.

## Architecture

Mirrors the existing Edit/Delete action pattern almost line-for-line.
All file/line references are relative to the current tree.

### A. Slack client (`internal/slack/client.go`)

Add two HTTP wrappers alongside the existing `MarkChannel` / `MarkThread`
(both of which bypass slack-go and POST to internal endpoints):

```go
// MarkChannelUnread rolls the channel's read watermark backward to ts.
// Pass ts == "" or "0" to mark the entire channel unread.
func (c *Client) MarkChannelUnread(ctx context.Context, channelID, ts string) error

// MarkThreadUnread marks a thread as unread starting at ts using
// subscriptions.thread.mark with read=0.
func (c *Client) MarkThreadUnread(ctx context.Context, channelID, threadTS, ts string) error
```

Refactor: `MarkChannel` / `MarkChannelUnread` share an internal helper;
`MarkThread` / `MarkThreadUnread` share an internal helper that takes a
`read bool` and flips the `read` form value to `0` or `1`.

For testability, introduce a private `httpClient *http.Client` field on
`*Client`, defaulting to `newCookieHTTPClient(c.cookie)` but settable in
tests (this also backfills missing test coverage for `MarkChannel` /
`MarkThread`).

Empirical risk: we assume `conversations.mark` accepts a `ts` earlier
than the current `last_read_ts`. The official Slack web client does this
exact thing, so it's expected to work, but the implementation plan
includes a manual smoke step to confirm before merging.

### B. UI message types & callbacks (`internal/ui/app.go`)

```go
// MarkUnreadMsg requests the App to mark the given message as unread.
type MarkUnreadMsg struct {
    ChannelID   string
    ThreadTS    string // "" for channel-level, parent ts for thread-level
    BoundaryTS  string // ts to set as last-read (i.e., ts of msg before selected)
    UnreadCount int    // computed from the loaded buffer at press time; 0 for thread-level
}

// MessageMarkedUnreadMsg is the result of a MarkUnreadFunc call.
type MessageMarkedUnreadMsg struct {
    ChannelID  string
    ThreadTS   string
    BoundaryTS string
    UnreadCount int   // for sidebar; 0 for thread-level
    Err        error
}

// MarkUnreadFunc is the application-level callback that performs the
// HTTP call and updates SQLite + in-memory caches.
type MarkUnreadFunc func(channelID, threadTS, boundaryTS string, unreadCount int) tea.Msg
```

Setter: `SetMessageMarkUnreader(MarkUnreadFunc)`, mirroring
`SetMessageDeleter` / `SetMessageEditor` (current home: `app.go:3381-3389`).

### C. Action helper (`internal/ui/app.go`)

```go
// markUnreadOfSelected marks the selected message (in either pane) as
// unread, rolling the local watermark backward and asking Slack to do
// the same. Non-destructive — no confirmation prompt.
func (a *App) markUnreadOfSelected() tea.Cmd
```

Uses the existing `selectedMessageContext()` helper to resolve
`(channelID, ts, _, _, panel, ok)`. Walks the loaded message buffer to
find the message immediately *before* the selected one and computes
`boundaryTS`:

- Channel pane: walks `messagepane.Messages()` for the selected index;
  uses `messages[i-1].TS` or `"0"` if `i == 0`.
- Thread pane: walks `threadPanel.Replies()` similarly. The parent
  message itself is at the top — selecting it is a no-op (you can't
  "mark the whole thread including the root unread"; parent is always
  considered seen if you've opened the thread). Document this; emit
  no-op cmd if the parent is selected.

For the channel pane, also computes `unreadCount = len(messages) -
selectedIndex`, passed through so the sidebar gets an accurate count.

### D. Keymap (`internal/ui/keys.go`)

Add to the `KeyMap` struct (around line 33-42):

```go
MarkUnread key.Binding
```

Add to `DefaultKeyMap()` (around line 70-75):

```go
MarkUnread: key.NewBinding(
    key.WithKeys("U"),
    key.WithHelp("U", "mark unread"),
),
```

Add a dispatch arm in `handleNormalMode` (after the `OpenPreview` arm
near `app.go:2076`):

```go
case key.Matches(msg, a.keys.MarkUnread):
    return a.markUnreadOfSelected()
```

### E. Sidebar (`internal/ui/sidebar/model.go`)

Add a setter alongside the existing `MarkUnread` (`+1`) and
`ClearUnread`:

```go
// SetUnreadCount sets the unread count for a channel to an exact value
// (rather than incrementing). Re-runs the staleness filter so the
// channel becomes visible if it had been hidden.
func (m *Model) SetUnreadCount(channelID string, n int)
```

The staleness filter (`internal/ui/sidebar/staleness.go:40`) already
exempts `UnreadCount > 0`, so a marked-unread channel won't be hidden.

### F. Cache (`internal/cache/channels.go`)

Reuse the existing `UpdateLastReadTS(channelID, ts string) error`. No
schema change. The `WorkspaceContext.LastReadMap` in-memory cache
(`cmd/slk/main.go:82`) must also be updated; this happens in the
wiring layer below.

### G. Status bar (`internal/ui/statusbar/model.go`)

Add new toast message types alongside the existing `PermalinkCopiedMsg`
etc. (around line 283-320):

```go
type MarkedUnreadMsg struct{}
type MarkUnreadFailedMsg struct{ Err error }
```

App handles them by calling `statusbar.SetToast(...)` and scheduling a
`CopiedClearMsg` after 2 seconds (mirrors the existing permalink-copied
toast at `app.go:1104-1108`).

### H. Wiring (`cmd/slk/main.go`)

Register the `MarkUnreadFunc` on the App after `SetMessageDeleter`
(current home: `cmd/slk/main.go:602`):

```go
app.SetMessageMarkUnreader(func(channelID, threadTS, boundaryTS string, unreadCount int) tea.Msg {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    var err error
    if threadTS == "" {
        err = client.MarkChannelUnread(ctx, channelID, boundaryTS)
        if err == nil {
            _ = db.UpdateLastReadTS(channelID, boundaryTS)
            wctx.LastReadMap[channelID] = boundaryTS
        }
    } else {
        err = client.MarkThreadUnread(ctx, channelID, threadTS, boundaryTS)
        // No SQLite write for thread-level (no per-thread column in v1).
    }
    return ui.MessageMarkedUnreadMsg{
        ChannelID: channelID, ThreadTS: threadTS,
        BoundaryTS: boundaryTS, UnreadCount: unreadCount, Err: err,
    }
})
```

The App's `Update` arm for `MessageMarkedUnreadMsg` then:

- On error: emits `statusbar.MarkUnreadFailedMsg{Err}` toast; no state mutation.
- On success (channel-level):
  - `messagepane.SetLastReadTS(boundaryTS)` (moves the `── new ──` line).
  - `sidebar.SetUnreadCount(channelID, unreadCount)`.
  - Emits `statusbar.MarkedUnreadMsg{}` toast.
- On success (thread-level):
  - `threadPanel.SetUnreadBoundary(boundaryTS)`.
  - `threadsview.MarkByThreadTSRead(threadTS, false)`.
  - Emits `statusbar.MarkedUnreadMsg{}` toast.

### I. WebSocket events (`internal/slack/events.go`)

Add cases to `dispatchWebSocketEvent` (current dispatch table:
`internal/slack/events.go:113-194`):

```go
case "channel_marked", "im_marked", "group_marked", "mpim_marked":
    // payload has "channel", "ts", "unread_count_display"
    h.OnChannelMarked(channelID, ts, unreadCount)
case "thread_marked":
    // payload has "channel", "thread_ts", "ts" within "subscription"
    h.OnThreadMarked(channelID, threadTS, ts)
```

Extend the `EventHandler` interface (same file) with:

```go
OnChannelMarked(channelID, ts string, unreadCount int)
OnThreadMarked(channelID, threadTS, ts string)
```

Wire the handlers in `cmd/slk/main.go` to push App-level messages:

```go
type ChannelMarkedRemoteMsg struct {
    ChannelID   string
    TS          string
    UnreadCount int
}
type ThreadMarkedRemoteMsg struct {
    ChannelID, ThreadTS, TS string
}
```

App's `Update` arms for these messages:

- `ChannelMarkedRemoteMsg`: update
  `sidebar.SetUnreadCount(channelID, unreadCount)`, and if this is the
  currently active channel, also update `messagepane.SetLastReadTS(ts)`.
  **No toast** — silent reconciliation.
- `ThreadMarkedRemoteMsg`: if this is the currently open thread, call
  `threadPanel.SetUnreadBoundary(ts)`. Always call
  `threadsview.MarkByThreadTSRead(threadTS, ts == "")` (any non-empty
  `ts` from a remote thread_marked is interpreted as "thread is now
  unread"; an empty/missing `ts` means "thread is now read"). The next
  threads-view fetch will re-rank from cache. **No toast**.

> **Implementation note:** SQLite + `LastReadMap` persistence happens in
> the `rtmEventHandler.OnChannelMarked` body (i.e. at the network-edge),
> not in the App's `Update` arm. The cache must stay authoritative even
> when the receiving workspace isn't currently active (e.g. background
> workspace), and the WS handler runs regardless of active state. Doing
> persistence there means the App-side arm is pure UI and can be
> short-circuited when the workspace isn't visible. See
> `cmd/slk/main.go:OnChannelMarked` for the live code.

The local-press code path and the remote-event code path are factored
into shared App methods (e.g. `applyChannelMark(channelID, ts string,
unreadCount int)`) so the two flows don't drift.

## Data Flow

### Local press of `U` (channel pane)

```
keys.go (KeyMap.MarkUnread "U")
   -> app.handleNormalMode -> markUnreadOfSelected()
       -> selectedMessageContext()
       -> compute boundaryTS, unreadCount
       -> emit MarkUnreadMsg{ChannelID, ThreadTS:"", BoundaryTS, UnreadCount}
   -> app.Update(MarkUnreadMsg) -> calls MarkUnreadFunc
       -> client.MarkChannelUnread(ctx, channelID, boundaryTS)   [HTTP]
       -> on success: db.UpdateLastReadTS, wctx.LastReadMap update
       -> returns MessageMarkedUnreadMsg{...}
   -> app.Update(MessageMarkedUnreadMsg)
       -> applyChannelMark(channelID, boundaryTS, unreadCount)
            -> messagepane.SetLastReadTS
            -> sidebar.SetUnreadCount
       -> statusbar.MarkedUnreadMsg toast
       -> 2s tick -> CopiedClearMsg
```

### Local press of `U` (thread pane)

```
... same dispatch ...
   -> markUnreadOfSelected() detects PanelThread, sets ThreadTS
   -> client.MarkThreadUnread(ctx, channelID, threadTS, boundaryTS)
   -> on success: NO db write, just in-memory
   -> MessageMarkedUnreadMsg
       -> applyThreadMark(channelID, threadTS, boundaryTS)
            -> threadPanel.SetUnreadBoundary
            -> threadsview.MarkByThreadTSRead(threadTS, false)
       -> statusbar.MarkedUnreadMsg toast
```

### Inbound WS event (other client marked unread)

```
events.go: case "channel_marked"
   -> EventHandler.OnChannelMarked(channelID, ts, unreadCount)
   -> connection layer -> ChannelMarkedRemoteMsg
   -> app.Update(ChannelMarkedRemoteMsg)
       -> applyChannelMark(channelID, ts, unreadCount)
            (same shared method as local path)
       -> NO toast

events.go: case "thread_marked"
   -> EventHandler.OnThreadMarked(channelID, threadTS, ts)
   -> tea.Msg ThreadMarkedRemoteMsg
   -> app.Update(ThreadMarkedRemoteMsg)
       -> applyThreadMark(...)
       -> NO toast
```

## Failure Modes

| Failure | Behavior |
|---|---|
| HTTP error from `MarkChannelUnread` / `MarkThreadUnread` | `MessageMarkedUnreadMsg.Err` populated → `MarkUnreadFailedMsg` toast. No local state change. |
| Selection unavailable (no message focused) | Silent no-op (matches Edit/Delete). |
| Boundary computation when selected is the oldest loaded | `boundaryTS = "0"`, channel becomes fully unread. Logged at debug level. |
| User presses `U` on the parent message inside the thread pane | Silent no-op; document. |
| `conversations.mark` rejects rolled-back `ts` (unexpected) | Surfaces as HTTP error; user sees `Mark unread failed` toast. The plan's smoke test catches this before merge. |
| WS event arrives for unknown channel (channel not in cache) | Update SQLite + LastReadMap, skip sidebar update (channel isn't shown). Same pattern as today's WS message handlers. |

## Concurrency & Ordering

- The existing `channelFetcher` re-marks-as-read on channel **entry**
  (not on every WS message arrival). So a user who marks unread, switches
  away, and switches back will see the channel re-mark as read — same as
  official Slack. Documented as expected behavior.
- The bulk `client.counts` writer (`cmd/slk/main.go:1080-1081`) on
  workspace load could overwrite a freshly-marked-unread state if it
  fires after a press of `U`. In practice `client.counts` only fires on
  workspace-context bootstrap (once per session per workspace), so this
  is not a real conflict. Note in the plan; do not gate this work on it.
- Local press and remote WS event for the same channel could race.
  Whichever arrives last wins; both call the same `applyChannelMark`
  shared method, which is idempotent.

## Testing Strategy

### App-level tests (`internal/ui/app_test.go`)

- `TestMarkUnreadOfSelected_ChannelPane_EmitsMarkUnreadMsg` — happy
  path; assert `BoundaryTS == ts[i-1]` and `UnreadCount` matches.
- `TestMarkUnreadOfSelected_OldestMessage_BoundaryIsZero`.
- `TestMarkUnreadOfSelected_NoSelection_NoOp`.
- `TestMarkUnreadOfSelected_ThreadPane_EmitsThreadMarkUnread` — assert
  `ThreadTS` populated.
- `TestMarkUnreadOfSelected_ThreadParent_NoOp`.
- `TestMarkUnreadOfSelected_OwnMessage_AllowedNoConfirm` — explicitly
  assert no confirm prompt is shown (unlike Delete).
- `TestMessageMarkedUnreadMsg_UpdatesPaneAndSidebar` — inject a
  `MarkUnreadFunc` returning success; assert `messagepane.lastReadTS`
  and `sidebar` count are updated and a `MarkedUnreadMsg` toast is
  queued.
- `TestMessageMarkedUnreadMsg_Error_ShowsFailedToast` — inject a func
  that returns `Err`; assert `MarkUnreadFailedMsg` and no state
  mutation.
- `TestChannelMarkedRemoteMsg_UpdatesStateSilently` — feed synthetic
  remote-mark msg; assert SQLite + sidebar updated; **no toast**.
- `TestThreadMarkedRemoteMsg_UpdatesThreadviewRow`.
- `TestApplyChannelMark_Idempotent` — calling twice with same args
  produces the same final state.

### Slack client tests (new in `internal/slack/client_test.go`)

Introduce a private `httpClient` field for DI and use
`httptest.NewServer` to assert request body shape:

- `TestMarkChannelUnread_PostsCorrectForm` — assert `channel`, `ts`
  values in posted form-encoded body and target URL.
- `TestMarkThreadUnread_PostsReadZero` — assert `read=0` in the form.
- Also backfill: `TestMarkChannel_PostsCorrectForm` and
  `TestMarkThread_PostsReadOne`, since they currently have no test
  coverage.

### WebSocket event tests (`internal/slack/events_test.go`)

- `TestDispatch_ChannelMarked_CallsHandler` — feed synthetic JSON,
  assert handler invoked with correct args.
- Same for `im_marked`, `group_marked`, `mpim_marked`.
- `TestDispatch_ThreadMarked_CallsHandler`.

### Cache tests (`internal/cache/channels_test.go`)

- Add `TestUpdateLastReadTS_RoundTrip` if not already present (the
  exploration agent flagged this as missing coverage).

### Sidebar tests (`internal/ui/sidebar/model_test.go`)

- `TestSetUnreadCount_UpdatesBadgeAndUnstale` — verify the new setter
  sets the count exactly, re-runs the filter, and exempts from
  staleness.

### Manual / smoke verification

Documented in the plan, run before merge:

1. Press `U` on a message in a channel → `── new ──` moves up,
   sidebar bolds, badge shows correct count, official Slack web client
   reflects the unread within ~1s.
2. Press `U` on a reply in a thread → thread `── new ──` moves,
   threads view bolds the row.
3. Mark unread in another Slack client → slk picks it up via WS
   `channel_marked` event without needing a channel switch.
4. Press `U` on the oldest visible message → channel becomes fully
   unread (boundary = "0").

## Future Work

- Per-thread `last_read_ts` column in SQLite, replacing the heuristic
  for thread unread surfacing. Would also unlock pre-merge cleaner
  semantics for thread mark-unread. Tracked separately.
- "Mark all in channel as unread" / "Mark all in workspace as read"
  bulk commands.
- Vim `u` for undo (e.g., undo the last mark-unread or the last
  message-send). Not in scope; key intentionally reserved.

## Open Questions

None at design time. The one empirical risk (does
`conversations.mark` accept a rolled-back `ts`?) is covered by the
manual smoke check in the test plan.

## References

- `internal/slack/client.go:728-787` — existing `MarkChannel` /
  `MarkThread` to mirror.
- `internal/ui/app.go:367-425` — message-action types (Edit/Delete) to
  mirror.
- `internal/ui/app.go:4646-4673` — `beginDeleteOfSelected`, the closest
  template (minus the ownership check and confirmation prompt).
- `internal/ui/app.go:4449-4470` — `selectedMessageContext`, used as-is.
- `internal/ui/messages/model.go:961-970, 1036-1044` — channel-pane
  unread boundary + landmark.
- `internal/ui/thread/model.go:117, 174` — thread-pane unread boundary.
- `internal/cache/threads.go:33-95` — thread unread heuristic
  (unchanged in this design).
- `internal/ui/sidebar/model.go:529-559` — sidebar `MarkUnread` /
  `ClearUnread`; new `SetUnreadCount` will live alongside.
- `internal/slack/events.go:113-194` — WS dispatch table; new event
  cases land here.
- `cmd/slk/main.go:544-567` — existing `channelFetcher` mark-as-read
  path (informational; not modified except for in-memory map updates
  in adjacent wiring).
