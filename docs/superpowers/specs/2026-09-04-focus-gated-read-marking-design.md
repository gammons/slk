# Focus-Gated Read Marking

Design for [issue #159](https://github.com/gammons/slk/issues/159), plus the
adjacent thread read-state defects uncovered while scoping it.

## Problem

When a message arrives in the channel the user is currently viewing, slk treats
it as read locally but never advances Slack's server-side read cursor.
`conversations.mark` fires only on channel entry, so its timestamp covers
messages that existed when the channel was opened — not messages received
afterward.

Two consequences:

- Cross-client read state diverges. slk shows a message as read while Slack
  still considers it unread.
- Slack may push a mobile notification for a message already visible in slk.

The deeper flaw is the assumption that "this channel is selected" means "the
user can see it." slk may be running in a background terminal, an inactive tmux
window, or an unfocused tmux pane with the same channel still selected.

Two independent gates encode that assumption, and both key on channel ID alone:

- `cmd/slk/main.go:4029` — `activeChIDForRead != channelID` suppresses the
  `has_unread` DB write.
- `internal/ui/reducer_send.go:289` — `m.ChannelID == a.activeChannelID` skips
  the unread bump.

Three supporting defects surfaced during exploration and are in scope because
this work touches all of them:

1. `markChannel` / `markThread` (`internal/slack/client.go:1215`, `:1246`) never
   read the response body. They check neither HTTP status nor the JSON `ok`
   field, so `{"ok":false,"error":"invalid_auth"}` is reported as success.
   `markChannelReadAsync` (`cmd/slk/main.go:3098`) then discards the returned
   error and writes local read state unconditionally.
2. `internal/slack/events.go:384` derives `read := !evt.Subscription.Active`
   from `thread_marked`. That field means *subscribed*, not *unread* — proven
   inside the repo by `wsThreadSubscribedEvent` (`events.go:265`), a
   byte-identical block whose `active` is read as "subscribed" at `events.go:397`.
   The same field cannot carry both meanings.
3. `applyThreadUnreadBoundary` (`internal/ui/app.go:1822`) feeds the thread
   panel's `── new ──` divider the *channel's* `last_read_ts`. Plain thread
   replies never advance the channel watermark, so that value is systematically
   older than the thread's real cursor and the divider lands too early.

### Observed symptom of defect 2

Posting a reply into a thread makes slk show that thread as unread. It clears
only after switching to another channel and back. Intermittent.

Chain: posting a reply auto-subscribes you, so Slack's `thread_marked` carries
`subscription.active = true`. `events.go:384` computes `read = false`.
`OnThreadMarked` emits `ThreadMarkedRemoteMsg{Read: false}`, `applyThreadMark`
calls `threadsView.MarkByThreadTSUnread` (`app.go:3170`), and the thread renders
unread. That flag is in-memory only; it is wiped when `SetSummaries` replaces
the list from `ListSubscribedThreads`, which recomputes from durable state where
the self-send suppression at `internal/cache/threads.go:127` correctly fires.
Switching channels and back forces that refresh.

It is intermittent because it races the 150 ms `ThreadsListDirtyMsg` debounce
that the reply itself schedules. If the refresh lands after the `thread_marked`,
the bogus flag is wiped and nothing is visible. If it lands before, the flag
sticks.

The same conflation causes a second bug: `OnThreadMarked` writes that bit into
the `active` column (`cmd/slk/main.go:4419`), which `ListSubscribedThreads` uses
as the *subscribed* filter (`internal/cache/threads.go:76`). Marking a thread
read tombstones its row, so the thread disappears from the Threads list until
the next 30-minute `getView` sweep. `cmd/slk/event_handler_test.go:259` asserts
this as intended behavior.

## Expected behavior

When a top-level message or broadcast thread reply arrives in the active channel
of the active workspace, slk advances Slack's read cursor only if the terminal
containing slk is focused. When a reply arrives in a thread whose panel is open,
slk advances that thread's cursor under the same focus condition.

If slk is running in an unfocused terminal, tmux window, or tmux pane, the
message stays unread both locally and on Slack. When focus returns, the
accumulated marks are issued.

Plain thread replies continue to use thread-specific read handling and never
advance the parent channel cursor unless broadcast.

## Design decisions

### Initial focus state: assume focused

Terminals report focus *transitions* only. Enabling DECSET 1004 does not elicit
a current-state report, so a user who launches slk and never leaves the terminal
receives no `FocusMsg` — ever.

slk therefore initializes `terminalFocused` to `true`. The user typed a command
to launch slk, so the terminal was focused at that moment. On a terminal with no
focus-event support, no `BlurMsg` ever arrives, the flag stays `true`, and
behavior is identical to today's. No regression and no capability probe.

This deliberately departs from the issue's "if focus state is unknown, do not
mark read." Taken literally, that would leave the feature permanently dead on
terminals without DECSET 1004 and inert for any user who never alt-tabs away.

### Focus-regain catch-up

On `FocusMsg`, pending marks accumulated while blurred are issued immediately.
Without this, a user who alt-tabs back, reads everything, and never switches
channels leaves the channel unread on Slack indefinitely.

### The divider does not move, but only because the echo is suppressed

The `── new ──` divider must stay where the user entered the channel and
recompute on next entry. Advancing the cursor writes `last_read_ts` to SQLite
and to Slack; the local path does not push a new value into the on-screen
message models. `MarkRead` produces `ChannelMarkedReadMsg`, whose arm calls
`notifyReadStateChanged()` and nothing else, and channel entry already installs
the pre-mark cursor — `MessagesLoadedMsg` carries `LastReadTS` read before the
mark.

The local path is not the whole system, and an earlier revision of this section
reasoned only from it. Slack broadcasts `channel_marked` to every connected
client **including the one that issued the mark**. That echo arrives at
`rtmEventHandler.OnChannelMarked`, becomes a `ChannelMarkedRemoteMsg`, and its
arm calls `applyChannelMark`, which does run `SetLastReadTS` on every window
viewing the channel. So without further work the divider moved to the newest
message a round trip after every mark — including the entry mark, which
predates this feature.

Suppressing that echo is therefore part of the design, not a free consequence
of it. `selfMarkDedup` (`internal/ui/app.go`) records the `(channel, ts)` of
every channel mark slk issues, at all three issuing sites: the tier-1 entry mark
in `reduceChannelSelected`, the auto-mark in `flushPendingMarks`, and
`ChannelService.Fetch`'s entry mark — the last reported back on the Update
goroutine as `MessagesLoadedMsg.MarkedTS`, since the fetcher marks from a cmd
goroutine and must not touch `App` state. `applyChannelMarkEcho`, which is what
the `ChannelMarkedRemoteMsg` arm calls, consumes a matching record and skips the
cursor update, while still calling `notifyReadStateChanged()` so the sidebar dot
and workspace rail clear.

The dedup deliberately sits in `applyChannelMarkEcho` rather than in the shared
`applyChannelMark` helper. `applyChannelMark`'s other caller is the local
mark-unread press (`MessageMarkedUnreadMsg`), a deliberate user action that must
move the divider unconditionally — and the two collide on ts routinely, since
slk auto-marks at the newest message and "mark this newest message unread"
targets the same one. Consuming in the shared helper would silently swallow the
press for as long as the record lives: one round trip normally, unbounded if the
echo never arrives.

Records are consumed on match and the set is bounded (oldest evicted first), so
a `channel_marked` slk did not issue — the user reading the channel in another
Slack client — still moves the divider, which is the correct response to that
event.

The thread panel's landmark has the same shape and the same two-sided fix.
`ThreadMarkedLocalMsg` reports slk's own `subscriptions.thread.mark` completing;
its arm applies threads-list state only (`applyThreadMarkListState`) and leaves
the panel boundary at the pre-open snapshot.

The remote side needed the same treatment, because **`ThreadMarkedRemoteMsg` is
not equivalent to "a mark from another client."** Slack broadcasts
`thread_marked` back to the issuing client exactly as it does `channel_marked` —
`TestThreadMarkedRemoteMsg_SelfReplyDoesNotReFlagUnread` documents slk receiving
one after posting its own reply — so slk's own thread marks used to erase the
panel landmark by the remote route. `App.selfThreadMarks`, a second
`selfMarkDedup` instance keyed on `(channel, threadTS, ts)`, closes it: the
`ThreadMarkedLocalMsg` success arm records the mark (that arm runs on the Update
goroutine, whereas `markThreadRead` issues from a cmd goroutine and must not
touch `App` state), and `applyThreadMarkEcho` — what the
`ThreadMarkedRemoteMsg` arm calls — consumes a matching record and skips
`SetUnreadBoundary` while still settling the threads-list flag and the sidebar
badge. Suppression is per-record and consumed on match, never blanket, because a
thread mark genuinely made in another client must still move the landmark.

### The decision lives in the UI reducer

Focus is known on the UI goroutine via `FocusMsg`/`BlurMsg`. The `has_unread` DB
write happens on the RTM goroutine in `OnMessage`. Rather than duplicate focus
state across goroutines behind an atomic, the decision moves to the reducer:

- `OnMessage` drops its active-channel exemption and always writes
  `has_unread = true` for eligible messages.
- `reduceNewMessage`, which only runs for the active workspace, checks focus and
  issues the mark, clearing the flag on success.

Focus never leaves the UI goroutine. Failure naturally leaves the channel
unread. The path is fully testable through the existing `ChannelServiceFuncs`
closure seam.

Cost: one extra DB write per message, and a sub-second window in which the
active channel is `has_unread = true`. Nothing on this path repaints the
sidebar, so no dot appears; an unrelated event repainting within that window
would briefly show a dot that self-corrects on flush. Accepted.

## Architecture

### Terminology

**Channel-eligible** message: a top-level message (`thread_ts` empty or equal to
`ts`) or a `thread_broadcast`. Only these advance a channel's read cursor, which
is the rule already encoded at `cmd/slk/main.go:4022-4024` and duplicated at
`reducer_send.go:309-310`.

**Thread-eligible** message: any message with a `thread_ts` that differs from
its `ts`. A `thread_broadcast` is both channel- and thread-eligible.

### Focus tracking

`App.View` already returns `tea.View` and sets `AltScreen` / `MouseMode` at
`internal/ui/app.go:2767`. Add `v.ReportFocus = true` beside them. Add
`App.terminalFocused bool`, initialized `true`.

A new `internal/ui/reducer_focus.go` joins the dispatch chain and owns
`tea.FocusMsg`, `tea.BlurMsg`, and the mark-flush tick. `reduceIO` owns terminal
leftovers such as `tea.PasteMsg`, but focus here drives read-state marking — a
cohesive family, matching the repo's reducer doctrine.

### Pending-mark slots

Two single-slot pending marks on `App`:

```
pendingChannelMark {channelID, ts}
pendingThreadMark  {channelID, threadTS, ts}
```

Rules:

- An eligible message arrives: overwrite the slot with the newer ts
  (newest-wins). A slot can never issue a request for a ts older than one it
  already holds.
- Focused: also schedule a flush tick via `tea.Tick` if one is not already
  pending — the `scheduleThreadsDirty` idiom (`app.go:1831`).
- Blurred: record only, no tick.
- `FocusMsg`: if a slot is non-empty, flush immediately.
- Flush tick: flush if focused. If blurred, leave the slot pending rather than
  dropping it.
- Flushing clears the slot, then issues the request. A failed request is
  therefore not retried by this mechanism; it is reconciled by the next arrival,
  the next channel entry, or reconnect sync. This is deliberate — retrying a
  mark that Slack rejected risks a hammering loop against a rate-limited
  endpoint, the same hazard `threadSubsGate` guards against by recording its
  timestamp at admission rather than completion.
- Channel switch: clear the channel slot. The entry-mark supersedes it.
- `BlurMsg`: no flush, and any already-scheduled tick becomes a no-op on
  arrival per the rule above.

Focus-regain catch-up falls out of this mechanism rather than needing a separate
path, and a burst coalesces to one request per interval per target.

Debounce default is 1 s, in a configurable `App` field. `threadsDirtyDebounce`
is 150 ms because it re-queries local SQLite; this is a network write, so a full
second is the right order of magnitude. Tests set it to zero or drive the flush
message directly.

### Channel path

`cmd/slk/main.go:4028` drops the `activeChIDForRead != channelID` condition.
`has_unread = true` is written for every eligible message — top-level or
`thread_broadcast` — regardless of which channel is active. The
`activeChannelID` closure remains; notifications still use it.

`reduceNewMessage`'s active-channel branch (`reducer_send.go:289`), currently a
bare debug log, becomes:

- Focused: record the pending channel mark. Do not call
  `notifyReadStateChanged`, so this event triggers no sidebar repaint.
- Blurred: call `notifyReadStateChanged()` so the unread dot appears. This is
  the new "remain unread locally" behavior.

Flush calls the existing `ChannelService.MarkRead` → `markChannelReadAsync` →
`ChannelMarkedReadMsg`.

### Thread path

In `reduceNewMessage`'s reply branch (`reducer_send.go:198`), if the terminal is
focused and the thread panel is open on that thread, record the pending thread
mark. A `thread_broadcast` records both slots.

The visibility gate is "terminal focused and thread panel open on that thread" —
not `focusedPanel == PanelThread`. This mirrors the channel rule, which is "this
channel is active," not "the messages pane holds slk-internal focus."
`reduceNewMessage` already routes replies on exactly this predicate.

`ThreadService.Mark` is today `void` and remote-only (`main.go:1741`), so
`thread_subscriptions.last_read` never advances locally. It changes to return a
`tea.Cmd` yielding `ThreadMarkedLocalMsg{ChannelID, ThreadTS, TS, Err}` —
convention (b) in `services.go`. The closure writes `last_read` only after a
successful `MarkThread`. One existing call site updates,
`reducer_threads.go:130`.

### Thread `active`/unread conflation fix

`OnThreadMarked` becomes `(channelID, threadTS, lastRead string, subscribed
bool)`. The derived `read` bool is deleted at its source in `events.go`: nothing
downstream may infer read/unread from the subscription block. `subscribed` is
that block's `active` flag forwarded under its true meaning, and it does exactly
one job — choose the cursor writer, both of which leave the durable `active`
column alone. Ownership of `active` belongs solely to `thread_subscribed`,
`thread_unsubscribed`, and `getView`.

The writer choice matters because slk's own `subscriptions.thread.mark` echoes
back as `thread_marked`, so this handler sees marks for threads the user merely
opened and never subscribed to. `markThreadRead` refuses to create a row for
those (`UpdateThreadLastReadIfExists`); without the same check here, the echo
would insert the `active=1` row it refused to create and put a phantom entry in
the Threads list. So: `subscribed` → `cache.UpdateThreadLastRead` (insert is a
legitimate cache repair), otherwise → `cache.UpdateThreadLastReadIfExists`.

Read/unread is decided by comparing `last_read` against the newest known reply —
logic that already exists at `internal/cache/threads.go:105-130`.

`ThreadMarkedRemoteMsg` loses its `TS` and `Read` fields and gains `LastRead`.
`applyThreadMark` correspondingly stops trusting `active`: it sets the thread
panel's unread boundary from `LastRead`, and updates the threads-view flag as
`Unread = LastReplyTS > LastRead` via a new
`threadsview.MarkByThreadTSReadAt(channelID, threadTS, lastRead)` replacing the
`MarkByThreadTSRead` / `MarkByThreadTSUnread` pair on this path. A
`ThreadsListDirtyMsg` still follows for authoritative reconcile from the cache.

This also cleans up mark-unread. `MessageService.MarkUnread`'s thread branch
currently depends on the echo arriving with `active = true`. Under a `last_read`
comparison, Slack rolling the cursor backward naturally yields unread. One rule
serves both directions.

`cmd/slk/event_handler_test.go:245-264` is rewritten; it currently asserts the
vanishing-thread behavior as correct.

### Thread divider fix

`applyThreadUnreadBoundary` (`app.go:1822`) switches from the channel cursor to
the thread cursor, using the `thread_subscriptions.last_read` accessor added
above.

### Slack client error handling

`markChannel` and `markThread` are rewritten as thin `postForm` callers that then
parse `{ok, error}` and return an error when `ok` is false. `postForm`
(`client.go:1489`) already supplies the `token` field, checks HTTP status, maps
429 to `rateLimitError`, and truncates bodies for logs. The helpers lose more
code than they gain.

`apiHTTPClient()` returns `c.httpClient` when set (`client.go:211`), which is
what `newTestClient` populates, and `postForm` uses `c.apiBaseURL` — so this
preserves both the existing test wiring and Enterprise Grid host discovery.

Four public methods begin returning real errors: `MarkChannel`, `MarkThread`,
`MarkChannelUnread`, `MarkThreadUnread`. `MessageService.MarkUnread`'s thread
branch already surfaces `Err` into a toast (`main.go:1670`), so its "Marked
unread" toast stops lying on failure as a side effect.

### `markChannelReadAsync` and its test seam

On error: log, skip the `UpdateChannelReadState` write, skip the
`ChannelMarkedReadMsg`. The channel stays unread locally, which is the honest
state, and it self-heals on the next channel entry or reconnect sync. This
applies to every caller, including channel entry.

`WorkspaceContext.Client` is a concrete `*slackclient.Client`, the sole reason
`TestMarkChannelReadAsync_UpdatesReadState` is skipped
(`event_handler_marked_test.go:138`). Introduce a one-method interface in
`cmd/slk`:

```go
type channelMarker interface {
	MarkChannel(ctx context.Context, channelID, ts string) error
}
```

`markChannelReadAsync` takes that instead of reaching through `wctx.Client`. The
skip comment's objection — "would require introducing an interface seam we don't
otherwise need" — no longer holds.

## Error handling

| Failure | Behavior |
|---|---|
| `conversations.mark` transport error, non-2xx, or `ok:false` | Log. Local `last_read_ts` unchanged, `has_unread` stays true. No `ChannelMarkedReadMsg`. Self-heals on next channel entry or reconnect sync. |
| `subscriptions.thread.mark` failure | Log. `thread_subscriptions.last_read` unchanged. `ThreadMarkedLocalMsg.Err` set. |
| HTTP 429 | Surfaces as `rateLimitError` from `postForm`. Treated as failure; the pending slot is already cleared, so the mark is retried on the next arrival or channel entry rather than immediately. |
| Focus state unavailable | Treated as focused. See "Initial focus state". |

## Testing

| Layer | Coverage |
|---|---|
| `internal/slack` | Mark helpers against HTTP 500, `{"ok":false,"error":"invalid_auth"}`, and 429. `newTestClient` (`client_test.go:1330`) provides the harness. |
| `internal/slack/events` | `thread_marked` passes `last_read` through and no longer derives a read bool; `active` is forwarded as `subscribed` and the cursor it forwards is unchanged either way. |
| `internal/cache` | `UpdateThreadLastRead` preserves `active`; `ListSubscribedThreads` recomputes unread correctly as the cursor moves in both directions. |
| `cmd/slk` | `OnMessage` sets `has_unread` even for the active channel; `OnThreadMarked` updates the cursor and leaves the row active (rewrite of `event_handler_test.go:245`), creates no row when `subscribed` is false, and leaves a tombstoned row tombstoned when it is true; `markChannelReadAsync` skips the local write on error via the new seam. |
| `internal/ui` | Matrix of focused/blurred × active/inactive channel × top-level/plain reply/broadcast. Plus focus-regain flush, burst coalescing (N arrivals produce one `MarkRead`), blurred arrivals staying pending until `FocusMsg`, and the divider unmoved after an auto-mark *and after the `channel_marked` echo of that mark*, with a foreign `channel_marked` still moving it as the control; and the same pair on the thread side, where slk's own `thread_marked` echo leaves the panel landmark alone while a foreign one moves it. All via the existing `ChannelServiceFuncs` / `ThreadServiceFuncs` closure seams. |

## Documentation

A "Focus reporting" section is added to `wiki/Terminal-Compatibility.md`,
adjacent to "Unread indicator in the window title" (lines 57-89), which already
establishes the doc's tmux-caveat convention. It covers what focus reporting
does for read state, the `set -g focus-events on` requirement for tmux users,
and the assume-focused fallback for terminals without DECSET 1004.

## Sequencing

Bottom-up. Each step is independently testable.

1. `internal/slack` mark helpers route through `postForm` and parse `ok`.
2. `cache.UpdateThreadLastRead`.
3. `events.go` and `OnThreadMarked` conflation fix, `applyThreadMark`
   correction, test rewrite.
4. `channelMarker` seam and `markChannelReadAsync` error honoring.
5. `ThreadService.Mark` returns a result; durable `last_read` write.
6. Thread-divider fix.
7. Focus tracking: `ReportFocus`, `terminalFocused`, `reducer_focus.go`.
8. Pending-mark slots and flush, wired into `reduceNewMessage`; drop the
   `OnMessage` active-channel exemption.
9. Documentation.

Steps 1-6 are the thread-correctness half and are independently shippable.
Steps 7-8 are the issue's headline feature.

## Out of scope

- Opening the Threads view or pressing `G` silently marks the top thread read on
  Slack (`reducer_threads.go:162` → `:130`), and a warm cache fires
  `subscriptions.thread.mark` twice per open, the first at a stale ts. Separable
  behavioral change; worth its own issue.
- `WorkspaceContext.ThreadsHasUnreads` is written at `main.go:2597` and read
  nowhere, with struct and API docs still describing a suppression mechanism
  deleted in `6133ba3`. Dead-but-documented state.
- `ThreadService.ListFetch` uses `router.Active()` for the self-user ID while
  querying an arbitrary team (`main.go:1789`), which misfires the self-send
  suppression on Enterprise Grid.
- A dwell delay before marking read on focus regain.
