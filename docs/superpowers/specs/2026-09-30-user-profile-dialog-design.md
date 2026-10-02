# User profile dialog

## Purpose

Answer "who is this person?" without leaving the terminal. In normal mode,
`K` opens a read-only dialog describing the author of the selected message:
who they are, whether they're around, and what time it is where they are.

## UX

`K` (vim's "info about the thing under the cursor") acts on the selected
message in whichever pane has focus: a channel message in the messages
pane, or a reply in the thread panel. `K`, `esc` or `q` closes the dialog
and returns to normal mode. It changes nothing else: channel, selection,
focus, thread and compose draft stay as they were.

No selection, a message with no user ID (a bot message carrying only a
`bot_id`), or a message whose `UserID` starts with `B` (the bot-ID
substitute `cmd/slk/history.go:279` uses when a message has no human
author), shows the toast `No profile for this message` and opens nothing.

`K` is bound as `KeyMap.UserProfile` with help text "show author's
profile", so the `?` overlay lists it.

## Layout

A centred box over `overlay.DimmedOverlay`, width `min(56, termWidth-4)`,
with only as many rows as it needs:

```
╭─ Profile ──────────────────────────────────────╮
│ ▟██▙  Priya Raman                   ● active   │
│ ▜██▛  @priya · she/her                         │
│       Staff Engineer, Platform                 │
│                                                │
│ 🌴 Vacation until Fri                          │
│ 🔕 Do not disturb until 5:00 PM                │
│                                                │
│ Local time  3:42 PM (PDT, −3h from you)        │
│ Email       priya@example.com 📋               │
│ Phone       +1 555 0100                        │
│                                                │
│               e copy email · K / esc / q close │
╰────────────────────────────────────────────────╯
```

- **Avatar:** 4×2 cells to the left of the name block, from the existing
  `core.AvatarService.Avatar(userID)`. That service returns kitty
  placeholders or half-block text; avatars never use sixel, so #265's
  sixel suppression does not affect them, and `DimmedOverlay` already
  keeps kitty placeholders that sit on box rows. Before the avatar has
  loaded, the name block starts at column 0 with no empty gutter. An
  `AvatarReadyMsg` for the displayed user makes the next frame draw it.
- **Header:** display name, then real name, then handle, whichever is
  first non-empty. Right-aligned presence dot and word (`active` / `away`)
  **only when presence is known**. slk tracks presence per user only for
  DM peers (`sidebar.presenceByUser`), and `users.info` does not return
  it, so for other users the indicator is left out rather than guessed.
- **Line 2:** `@handle`, plus ` · pronouns` when set. Badges: `APP` for
  bots and app users, `external` for Slack Connect users, and a dimmed
  `deactivated` when `users.info` reports `deleted: true`. Handle, real
  name and the `APP` badge come from the fetch — the UI's cached seed
  carries only display names — so before the fetch returns, line 2 shows
  only `external` when the user is already known to be external, and is
  otherwise empty.
- **Title:** shown when non-empty.
- **Status block:** custom status, DND and huddle state, from the
  presence controller's `peers[userID]` `peerstatus.Status`, rendered
  with `peerstatus` helpers so they read as they do elsewhere. Lines with
  nothing to show are left out, and so is the whole block if all are empty.
- **Details:** local time, email and phone, from the fetch. While loading
  they appear as one `Loading profile…` line, and after a failure as
  one error line (see Errors). Empty fields are left out, not shown as `—`.
- **Copy email:** a loaded, non-empty email ends in `📋`, and the footer
  reads `e copy email · K / esc / q close`. `e`, or a click on the icon,
  copies the address through `App.clipboardWrite`, toasts `Copied email`
  and leaves the dialog open. With no email (still loading, failed, or
  none on the profile) there is no icon, the footer is the plain one, and
  `e` toasts `No email to copy`. A long address is truncated before the
  icon so the icon always fits; a click anywhere else inside the box is a
  no-op, and a click outside closes it.
- **Local time:** the author's wall clock from `users.info`'s `tz_offset`,
  with the zone abbreviation (`PDT`) when there is one — derived in
  `cmd/slk` from the IANA `tz` name via `time.LoadLocation`; numeric
  abbreviations such as `-03` are dropped, not shown — and the
  difference from **this machine's** local offset (`time.Local`; slk
  does not know the signed-in user's Slack timezone). Formats: `−3h`,
  `+5h30m`, `same time as you`. It uses an injectable clock so goldens
  are deterministic.
- **Narrow or short terminals:** values are truncated with `…`
  (`emoji.Width`-aware). When the box is taller than the screen allows,
  rows are dropped from the bottom up: details first, then status, then
  title. Name and handle are always kept.

## Architecture

### Port

A new port in `internal/core/ports.go`, since none of the existing ones
covers looking up a person:

```go
// ProfileService fetches other users' full profiles.
type ProfileService interface {
    // Profile fetches one user's profile in the given workspace.
    // Blocking; call it from a tea.Cmd with a bounded context.
    Profile(ctx context.Context, teamID, userID string) (UserProfile, error)
}
```

`core.UserProfile` (in `types.go`) holds `UserID, TeamID, Handle, RealName,
DisplayName, Title, Pronouns, Email, Phone, TZ, TZAbbrev string; TZOffset
int; IsBot, Deleted bool`. As with the other ports, a
`NewProfileService(ProfileServiceFuncs{...})` adapter returns
`errors.ErrUnsupported` when its func is nil.

### Composition root (`cmd/slk`)

- `slack.Client` gains `GetUserProfileContext(ctx, userID)` wrapping
  `GetUserInfoContext`. The shared API HTTP client sets no timeout, so the
  context carries the bound: **10 s**, set by the UI's command.
- The adapter resolves the workspace client **by `teamID`**, never by
  whichever workspace is active when the command runs. It maps
  `slack.User` to `core.UserProfile` in a small pure function.
- It is wrapped in an in-memory TTL cache keyed by `teamID/userID`:
  5 minutes, injectable clock, errors not cached. Nothing is written to
  SQLite.
- `internal/demo` provides a fake with fixed profiles for the demo cast.

### UI (`internal/ui`)

- **`internal/ui/userprofile/`:** a self-contained sub-model. `Open(seed)`
  takes the cached identity (name, handle, bot/external flags). It also
  has `SetProfile(teamID, userID, core.UserProfile)`, `SetError(teamID,
  userID, err)`, `Close`, `IsVisible` and `Target() (teamID, userID)`.
  `View(width, height, live)` receives the render-time inputs (avatar
  string, presence, `peerstatus.Status`, now), so live status, presence
  and avatar updates show up without the model copying them. The box
  comes from `lipgloss` + `overlay`. The Phase 4 shared modal-chrome
  package does not exist yet; this modal moves onto it when it lands
  and adds no further `renderBox`/`visibleWindow` copy.
- **Mode:** `ModeUserProfile` is added to `Mode`, `String()`,
  `IsModalOverlay()` (so #265's sixel suppression applies) and
  `modeHandlers`, with `mode_user_profile.go` handling `K`/`esc`/`q`.
- **Opening:** the normal-mode handler for `KeyMap.UserProfile` resolves
  the focused pane's selected message and opens the model. It sets the
  mode and returns a `tea.Cmd` that calls `Profile` with a 10 s context
  and returns `UserProfileLoadedMsg{TeamID, UserID, Profile, Err}`.
- **`reducer_userprofile.go`:** applies `UserProfileLoadedMsg` only when
  the dialog is visible and its `Target()` matches, and drops it
  otherwise. It is added to the reducer chain; `Update` is not modified.
- **`AvatarReadyMsg`:** no new handling is needed beyond a re-render,
  since `View` reads the avatar at render time. A test confirms it.
- `internal/ui` still does no I/O; `boundary_test.go` must pass unchanged.

## Errors

In every case the cached rows stay on screen; only the details area changes.

- Network/API error: `Couldn't load full profile: <short reason>`. `K`
  again retries (errors aren't cached).
- `slack.RateLimitedError`: `Rate limited — try again in Ns`.
- Timeout (10 s context): the network-error line, reason `timed out`.
- Closed, or re-targeted, before the fetch returns: the result is dropped.
- Deactivated: `deactivated` badge; whatever Slack returned is still shown.

## Tests

Stdlib only, written first, tests inside the package they test.

- **`internal/ui/userprofile`:** renders the cached-only, loading, loaded
  and error states. Also: empty fields left out; badges (`APP`,
  `external`, `deactivated`); presence shown only when known; avatar
  present and absent (no empty gutter); local time with a fixed clock
  for negative, positive, half-hour and same-offset cases; truncation and
  the order rows drop in on narrow and short terminals.
- **`internal/ui`:**
  - `runKeyCases`: `K` in the messages pane and in the thread panel; no
    selection and a bot-only message (toast, no mode change); `K`, `esc`
    and `q` each close the dialog.
  - Reducer: matching result applied; mismatched team/user dropped; result
    after close dropped.
  - `IsModalOverlay(ModeUserProfile)`; `AvatarReadyMsg` and
    `UserStatusChangeMsg` for the displayed user show up in the next
    frame; kitty placeholders on box rows survive `DimmedOverlay`
    (reuse the existing overlay test if it already covers this).
  - A golden frame via `newGoldenApp` + `compareGolden`; `boundary_test.go`.
- **`cmd/slk`:** `slack.User` → `core.UserProfile` mapping; workspace
  resolved by ID; TTL cache hit, miss, expiry (fake clock), errors not
  cached.
- **`internal/core`:** unsupported error from a nil adapter func.
- **`internal/demo`:** the fake returns profiles for the demo cast.

## Out of scope

Workspace custom profile fields (`team.profile.get`), a larger avatar
(needs a second `avatar.Cache` render size and kitty upload per user),
opening profiles from the sidebar or DM header, fetching presence for
non-DM users, and actions such as "message this person".
