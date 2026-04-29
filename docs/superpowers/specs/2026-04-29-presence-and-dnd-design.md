# Presence & DND controls

**Status:** design
**Date:** 2026-04-29

## Summary

Add first-class support for setting and observing the current user's
Slack presence (active/away) and Do-Not-Disturb / snooze state. A single
hotkey (`Ctrl+S`) opens a picker overlay listing presence and snooze
actions for the active workspace; the status bar shows a live presence /
DND segment that reflects state changes from any source — slk itself,
the official Slack client, or the user's external script that calls the
Slack API directly. While DND/snooze is active, slk suppresses its own
OS notifications.

## Goals

- Set self presence to `auto` ("Active") or `away` from the TUI.
- Set, end, and override snooze with the standard Slack-web durations
  (20m, 1h, 2h, 4h, 8h, 24h, until tomorrow morning, custom).
- End DND immediately (covers both manual snooze and admin-scheduled DND).
- Show the current presence and DND state in the status bar.
- Reflect state changes in real time even when they are made by an
  external client or script (i.e., not initiated from slk).
- Suppress slk's OS notifications while DND/snoozed.

## Non-goals

- Custom status text and emoji. (Separate, larger feature; tracked
  separately. The same `user_change` plumbing would be reused.)
- DM-peer presence subscriptions (currently unsubscribed; will land
  separately).
- Persisting presence/DND to SQLite. State is transient and refetched
  on each WS connect.
- Quiet-hours config wiring. Reuses the new `IsDND` notify hook later.
- Cross-workspace bulk actions ("set away on all workspaces"). Each
  action targets only the active workspace.

## User-visible surface

### Hotkey and picker

`Ctrl+S` (new binding `PresenceMenu`) opens a picker overlay modeled on
`internal/ui/themeswitcher`. Header reads:

```
Status — <WorkspaceName>
```

Items, in order:

1. `● Active` — calls `users.setPresence(auto)`.
2. `○ Away` — calls `users.setPresence(away)`.
3. Separator.
4. `🌙 Snooze for 20 minutes`
5. `🌙 Snooze for 1 hour`
6. `🌙 Snooze for 2 hours`
7. `🌙 Snooze for 4 hours`
8. `🌙 Snooze for 8 hours`
9. `🌙 Snooze for 24 hours`
10. `🌙 Snooze until tomorrow morning` — until 09:00 local on the next
    weekday (so Friday-evening snooze runs until Monday 09:00).
11. `🌙 Snooze custom…` — switches the overlay to a single-line numeric
    input; `Enter` commits, `Esc` returns to the menu.
12. Separator.
13. `End snooze / DND` — only present when DND is currently active.

Behavior:

- `j`/`k` and `↑`/`↓` and `Ctrl+n`/`Ctrl+p` navigate.
- Typing filters the list (free from the picker template).
- `Enter` commits; the picker closes immediately.
- `Esc` cancels.
- The currently-active state row is rendered in the accent color and
  prefixed with a check-mark glyph.
- Snooze options whose duration would be ≤ the remaining snooze are
  hidden when DND is already active. (Avoids accidental shortening; to
  shorten you must End first.)
- Errors flash a red toast in the status bar; state is unchanged.

### Status bar segment

A new right-side segment is added between the unread badge and the
connection indicator (`internal/ui/statusbar/model.go:142`). Format:

| Server state                       | Rendered                       | Style                       |
|------------------------------------|--------------------------------|-----------------------------|
| Active, no DND                     | `● Active`                     | `styles.PresenceOnline` (green) |
| Manual away, no DND                | `○ Away`                       | `styles.PresenceAway` (muted) |
| DND/snooze active                  | `🌙 DND 1h 23m`                | warning color               |
| DND active, snooze end < 1 minute  | `🌙 DND <1m`                   | warning color               |
| DND active, no end (admin-DND)     | `🌙 DND`                       | warning color               |

The countdown refreshes once per minute via a `tea.Tick`. When DND
expires the segment falls back to the underlying presence value with no
network round-trip (we already know the timestamp).

The status bar reflects only the active workspace. Switching workspaces
(`Ctrl+w` / `1`–`9`) recomputes the segment from the new context.

### Notification suppression

While `DNDEnabled && time.Now().Before(DNDEndTS)` is true for the
active workspace's `WorkspaceContext`, slk suppresses all OS
notifications for that workspace — DMs, mentions, and keywords alike.

## Architecture

### Per-workspace state

New fields on `cmd/slk/main.go:48` `WorkspaceContext`:

```go
Presence   string    // "auto" or "away"; "" until first fetch
DNDEnabled bool      // true if either snooze or admin-DND is active
DNDEndTS   time.Time // unified end timestamp; zero if not in DND
```

`DNDEndTS` is set from whichever of `snooze_endtime` or
`next_dnd_end_ts` is non-zero and in the future on each
`GetDNDInfo`/`dnd_updated` payload. This unifies snooze and
admin-scheduled DND for display and notification suppression.

These are mutated by:

1. The bootstrap goroutine that fetches initial state on connect.
2. The WS event handlers for `manual_presence_change`, `dnd_updated`,
   and `dnd_updated_user`.
3. Successful API calls initiated from the picker (optimistic update,
   reconciled by the WS echo).

### Slack client additions

`internal/slack/client.go` gains pass-through wrappers via slack-go,
added to the `SlackAPI` interface:

```go
SetUserPresence(ctx context.Context, presence string) error
GetUserPresence(ctx context.Context, userID string) (*slack.UserPresence, error)
SetSnooze(ctx context.Context, minutes int) (*slack.DNDStatus, error)
EndSnooze(ctx context.Context) (*slack.DNDStatus, error)
EndDND(ctx context.Context) error
GetDNDInfo(ctx context.Context, userID string) (*slack.DNDStatus, error)
```

Plus a small WS-frame helper:

```go
SubscribePresence(userIDs []string) error
```

which writes a `{"type":"presence_sub","ids":[...]}` frame on the
existing browser-protocol WebSocket. It piggy-backs on the same
`writeMessage` path used today for typing events.

### WebSocket event handling

Three new event types are dispatched in
`internal/slack/events.go:dispatchWebSocketEvent`:

- `manual_presence_change` — fires when *the authenticated user* flips
  presence (from any client). Payload: `{ presence: "active"|"away" }`.
- `dnd_updated` and `dnd_updated_user` — fire when self DND/snooze
  changes. Payload includes `dnd_status: { dnd_enabled, snooze_enabled,
  snooze_endtime, next_dnd_start_ts, next_dnd_end_ts }`.

The `EventHandler` interface gains:

```go
OnSelfPresenceChange(presence string)
OnDNDChange(enabled bool, snoozeEndUnix int64)
```

`rtmEventHandler` (in `cmd/slk/main.go`) implements both: mutates its
`*WorkspaceContext` and sends a new bubbletea message:

```go
type StatusChangeMsg struct {
    TeamID     string
    Presence   string
    DNDEnabled bool
    DNDEndTS   time.Time
}
```

The App in `internal/ui/app.go` handles `StatusChangeMsg` by storing it
on the App's per-workspace cache and, if `TeamID == a.activeTeamID`,
forwarding it to the status bar.

The existing `presence_change` (third-party users') handler is
untouched — it continues to drive sidebar DM presence dots.

The `mockEventHandler` in `events_test.go` is updated to implement the
two new methods so existing tests still compile.

### Initial fetch

`cmd/slk/main.go` runs `bootstrapPresenceAndDND(ctx, wsCtx)` as a
goroutine just after each workspace finishes connecting (the same place
that currently calls `client.counts` and friends, around `main.go:486`).
The goroutine calls:

1. `client.SubscribePresence([]string{wsCtx.UserID})` — so future
   `presence_change` events for self arrive (some Slack accounts also
   need this for `manual_presence_change`; cheap regardless).
2. `client.GetUserPresence(wsCtx.UserID)` to seed `Presence`.
3. `client.GetDNDInfo(wsCtx.UserID)` to seed `DNDEnabled` and
   `SnoozeEndTS`.

A single `StatusChangeMsg` is then sent to the program. Failures are
logged and skipped — the user can still set state from the picker; the
WS events will fill in the rest.

### "Until tomorrow morning" semantics

Computed locally:

- If today is Mon–Thu, target is tomorrow 09:00 local.
- If today is Fri, target is Monday 09:00 local.
- If today is Sat, target is Monday 09:00 local.
- If today is Sun, target is Monday 09:00 local.
- Minutes are `int(math.Round(time.Until(target).Minutes()))`, clamped
  to `>=1`.

The 09:00 anchor is a constant for v1. (A config knob is a possible
follow-up but YAGNI for now.)

### Notification suppression

`internal/notify/notifier.go`:

- `NotifyContext` gains `IsDND bool`.
- `ShouldNotify` returns `false` immediately when `ctx.IsDND` is true,
  before any other check.

Caller in `cmd/slk/main.go` populates `IsDND` from
`wsCtx.DNDEnabled && time.Now().Before(wsCtx.DNDEndTS)`.

### Optimistic UI

When the user picks an action from the menu, slk:

1. Closes the picker.
2. Updates the `WorkspaceContext` fields and the status bar
   immediately, in the assumption the API will succeed.
3. Fires the API call in a goroutine.
4. On success: the WS echo (`manual_presence_change` or `dnd_updated`)
   re-affirms the state — idempotent, no flicker.
5. On error: reverts the context, shows an error toast.

This matches the optimistic pattern already used for reactions.

## Data flow diagram

```
                              ┌─────────────────────────┐
   user                       │  external script        │
   presses Ctrl+S             │  (api.slack.com calls)  │
       │                      └────────────┬────────────┘
       ▼                                   │
  Picker overlay                           │
       │  (selection)                      │
       ▼                                   │
  client.SetSnooze(...)                    │
       │  optimistic UI update             │
       ▼                                   ▼
  Slack HTTP API ◄────────────────────────────►  Slack WS
                                                  │
                                                  │ dnd_updated /
                                                  │ manual_presence_change
                                                  ▼
                                       dispatchWebSocketEvent
                                                  │
                                                  ▼
                                       OnDNDChange / OnSelfPresenceChange
                                                  │
                                                  ▼
                                       rtmEventHandler updates wsCtx
                                                  │
                                                  ▼
                                       program.Send(StatusChangeMsg)
                                                  │
                                                  ▼
                                       App.Update → status bar
```

## File-by-file impact

| File | Change |
|---|---|
| `internal/ui/keys.go` | New `PresenceMenu` binding (`ctrl+s`). |
| `internal/ui/mode.go` | New `ModePresenceMenu`, `ModePresenceCustomSnooze` constants. |
| `internal/ui/presencemenu/model.go` | New package mirroring `themeswitcher`; renders menu, handles keys, returns a `PresenceResult` discriminator. |
| `internal/ui/presencemenu/model_test.go` | Picker unit tests. |
| `internal/ui/app.go` | Wire `Ctrl+S` in `handleNormalMode`; new `handlePresenceMenuMode` and `handlePresenceCustomSnoozeMode`; `StatusChangeMsg` handler; route to status bar; per-workspace status cache. |
| `internal/ui/statusbar/model.go` | New `Presence`, `DNDEnabled`, `DNDEndTS` fields and `SetStatus(...)` setter; new right-side segment. Add `tea.Tick` for countdown refresh. |
| `internal/ui/statusbar/model_test.go` | Formatting tests for each state combination. |
| `internal/slack/client.go` | New `SlackAPI` methods (`SetUserPresence`, `GetUserPresence`, `SetSnooze`, `EndSnooze`, `EndDND`, `GetDNDInfo`); new `SubscribePresence` WS frame helper; corresponding wrappers on `Client`. |
| `internal/slack/client_test.go` | Tests for the new wrappers (mock-based). |
| `internal/slack/events.go` | New `wsManualPresenceEvent`, `wsDNDUpdatedEvent` types; new switch cases; new `OnSelfPresenceChange` and `OnDNDChange` methods on `EventHandler`. |
| `internal/slack/events_test.go` | New dispatch tests; mock implements new methods. |
| `cmd/slk/main.go` | New `WorkspaceContext` fields; `bootstrapPresenceAndDND` goroutine post-connect; `rtmEventHandler` implements new methods, sends `StatusChangeMsg`; populates `notify.NotifyContext.IsDND`. |
| `internal/notify/notifier.go` | `NotifyContext.IsDND` field; early-return in `ShouldNotify`. |
| `internal/notify/notifier_test.go` | New `TestShouldNotify_SuppressedByDND`. |

## Test plan

- **Events dispatch**: raw JSON for each new event type → mock handler
  records the call; assert payload (mirrors
  `TestDispatchWebSocketPresenceChangeEvent`).
- **Slack client wrappers**: each new method calls the right slack-go
  function with the right args; one happy path + one error path.
- **Picker model**: navigation, filtering, dynamic visibility (End
  shown only when DND active; snoozes ≤ remaining hidden when DND
  active), custom-snooze sub-mode, escape behavior.
- **Status bar formatting**: each row of the rendering table above is a
  unit test on `View(width)`.
- **Notifier**: `IsDND=true` always returns `false` regardless of
  mention/DM/keyword flags.
- **App-level**: feeding a `StatusChangeMsg` for the active workspace
  updates the status bar; for an inactive one, only the cache.

## Risks and open questions

- **Token scopes**: xoxc browser tokens are believed to grant
  `users:write`, `dnd:write`, `dnd:read`, `users:read`. Confirmed by
  the fact the official web client calls these endpoints with the same
  token. Smoke-test on first run; if a workspace fails, surface the
  error in a toast and don't crash.
- **Admin-scheduled DND vs. snooze**: `DNDEndTS` unifies both via the
  rule above. `dnd.endDnd` only ends *snooze*, not admin DND; if the
  user picks "End snooze / DND" while admin DND is active and snooze
  is not, the call is a no-op — surface this as an info toast rather
  than an error. Most users will only ever see snooze.
- **`presence_sub` framing**: not formally documented for the
  browser-protocol WS, but slack-go's RTM uses it and it's known to
  work. If a workspace silently rejects, the periodic re-fetch on
  reconnect still keeps state correct (just with reduced freshness for
  external API changes between connects).
- **Clock drift**: countdown is local-clock-based against
  `DNDEndTS`; if the user's clock is wrong, the display will be
  wrong but the server is authoritative — the next `dnd_updated` from
  Slack will correct.

## Out of scope, for the record

- Custom status text/emoji.
- DM-peer presence subscriptions for sidebar dots.
- Quiet-hours config integration.
- A "set on all workspaces" bulk action.
- Persisting state to SQLite.

## Implementation order

A full implementation plan will follow in
`docs/superpowers/plans/2026-04-29-presence-and-dnd.md`. High-level
order:

1. Slack client: add interface methods + wrappers + `SubscribePresence`
   + tests. (Foundation.)
2. Events: extend `EventHandler` + dispatch + tests. (Foundation.)
3. `WorkspaceContext` fields + bootstrap goroutine. (Foundation.)
4. Status bar: fields, setter, formatting, countdown tick, tests.
5. `presencemenu` package + tests.
6. App wiring: keybinding, mode, picker overlay, optimistic update.
7. Notifier: `IsDND` plumbing + tests.
8. End-to-end smoke test against a real workspace.
