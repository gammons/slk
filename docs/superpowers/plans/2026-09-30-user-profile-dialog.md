# User Profile Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `K` in normal mode opens a read-only modal showing the selected message author's profile. Cached identity, avatar and status appear immediately; title, pronouns, local time, email and phone arrive from `users.info`.

**Architecture:** A new `core.ProfileService` port. A self-contained `internal/ui/userprofile` sub-model renders the box. `ModeUserProfile`, a normal-mode key and `reduceUserProfile` connect it to the App through the existing mode table and reducer chain. `cmd/slk` wires the port to a context-aware `users.info` behind a 5-minute TTL cache; `internal/demo` fakes it.

**Tech Stack:** Go, bubbletea v2, lipgloss v2, slack-go v0.23.0, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-09-30-user-profile-dialog-design.md`

## Global Constraints

- `internal/ui` does no I/O; `internal/ui/boundary_test.go` passes unchanged. Timezone lookups (`time.LoadLocation` reads tzdata) happen in `cmd/slk`, not the UI.
- Tests: stdlib `testing` only, white-box (same package as the code).
- Add behavior via a new `reducer_*.go` and a `modeHandlers` entry; never extend `App.Update`.
- Key `K`, help text `show author's profile`. `K`, `esc`, `q` close.
- No-target toast: `No profile for this message`.
- Box width `min(56, termWidth-4)`; title `Profile`; footer `K / esc / q close`.
- Copy: `Loading profile…`; `Couldn't load full profile: <reason>`; `Rate limited — try again in Ns`; timeout reason `timed out`.
- Fetch context 10 s. TTL cache 5 min, keyed `teamID/userID`, errors not cached, nothing in SQLite.
- Avatar 4×2 from the existing `core.AvatarService`; no empty gutter when absent.
- Presence shown only when known (DM peers).
- Local-time delta: `−3h` (U+2212), `+5h30m`, `same time as you`; "you" is the zone offset of the App's clock.
- Every commit: `gofmt -l .` empty, touched packages' tests green.

**Spec corrections (applied in Task 1):**
1. Handle, real name and the `APP` badge come from the fetch (the UI caches only display names). Before it returns, line 2 shows only `external` when the user is known external.
2. A `UserID` starting with `B` (a bot ID that `cmd/slk/history.go:279` substitutes) is treated as "no profile" (toast).
3. The zone abbreviation (`PDT`) is derived in `cmd/slk` from `tz` via `time.LoadLocation`; numeric abbreviations such as `-03` are dropped.

## Review Focus

1. **Bot-only messages** (`UserID` `B…`): toast, no fetch. Covered by Task 3, `TestUserProfileKey_BotIDToast`.
2. **Workspace switched mid-fetch**: the dialog closes and the late result is dropped. Covered by Task 3, `TestReduceUserProfile_DroppedAfterWorkspaceSwitch`.
3. **Close, reselect a different author, reopen before the first fetch lands**: only the second result applies. Covered by Task 3, `TestReduceUserProfile_StaleUserDropped`.
4. **Huge or emoji-heavy values**: no line exceeds the box width. Covered by Task 2, `TestView_TruncatesToBoxWidth`.
5. **Missing timezone** (`TZ == ""`, common for bots and deactivated users): the local-time row is omitted, not shown as UTC. Covered by Task 2, `TestView_NoTZOmitsLocalTime`.

---

### Task 1: `core` port and types

**Files:**
- Modify: `internal/core/types.go`, `internal/core/ports.go` (after `UnreadService`), `internal/core/adapters.go`
- Test: `internal/core/profile_test.go`
- Modify: the spec (the three corrections above)

**Interfaces:**
- Produces:
  ```go
  type UserProfile struct {
      UserID, TeamID, Handle, RealName, DisplayName string
      Title, Pronouns, Email, Phone                 string
      TZ       string // IANA name; "" = unknown
      TZAbbrev string // "PDT"; "" when none or numeric
      TZOffset int    // seconds east of UTC
      IsBot, Deleted bool
  }
  // RateLimitedError is how adapters report Slack rate limiting, so the
  // UI never imports slack-go.
  type RateLimitedError struct{ RetryAfter time.Duration }
  func (e *RateLimitedError) Error() string // "rate limited, retry after 30s"
  type ProfileService interface {
      Profile(ctx context.Context, teamID, userID string) (UserProfile, error)
  }
  type ProfileServiceFuncs struct {
      Profile func(ctx context.Context, teamID, userID string) (UserProfile, error)
  }
  func NewProfileService(f ProfileServiceFuncs) ProfileService
  ```

- [ ] **Step 1: Write failing tests:** `TestProfileService_NilFuncUnsupported` (`errors.Is(err, errors.ErrUnsupported)`, zero profile); `TestProfileService_Delegates` (ctx, team and user are passed through; the result is returned verbatim); `TestRateLimitedError_As` (`errors.As` finds it through `fmt.Errorf("%w")`).
- [ ] **Step 2: Run** `go test ./internal/core -run 'TestProfileService|TestRateLimitedError'`. Expected: FAIL, undefined.
- [ ] **Step 3: Implement** the code above. A nil func returns `errors.ErrUnsupported`, like the editor adapter (`adapters.go:479`).
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Apply the spec corrections**, then commit `feat(core): ProfileService port` with this plan and the spec edit.

---

### Task 2: `internal/ui/userprofile` sub-model

**Files:**
- Create: `internal/ui/userprofile/model.go`, `view.go`, `localtime.go`
- Test: `internal/ui/userprofile/model_test.go`, `view_test.go`, `localtime_test.go`

**Interfaces:**
- Consumes: Task 1's `core.UserProfile` and `core.RateLimitedError`; `peerstatus.Status`; `overlay.DimmedOverlay`; `emoji.Width`; `styles`.
- Produces:
  ```go
  type Seed struct{ TeamID, UserID, DisplayName string; IsExternal bool }
  type Live struct {
      Avatar   string // "" = not loaded
      Presence string // "active" | "away" | "" (unknown → omitted)
      Status   peerstatus.Status
      Now      time.Time // its zone offset is "you"
  }
  func New() *Model
  func (m *Model) Open(s Seed)
  func (m *Model) SetProfile(teamID, userID string, p core.UserProfile) bool // false on mismatch or when closed
  func (m *Model) SetError(teamID, userID string, err error) bool
  func (m *Model) Close()
  func (m *Model) IsVisible() bool
  func (m *Model) Target() (teamID, userID string)
  func (m *Model) ViewOverlay(termW, termH int, background string, live Live) string
  func formatLocalTime(now time.Time, tzOffset int, abbrev string) string
  func errorLine(err error) string
  ```

- [ ] **Step 1: Write failing tests** (box assertions compare ANSI-stripped text):
  - `TestFormatLocalTime` (table; `now := time.Date(2026, 9, 30, 18, 42, 0, 0, time.FixedZone("EDT", -4*3600))`):
    - `(-7*3600, "PDT")` → `3:42 PM (PDT, −3h from you)`
    - `(19800, "IST")` → `4:12 AM (IST, +9h30m from you)`
    - `(-4*3600, "EDT")` → `6:42 PM (EDT, same time as you)`
    - `(-3*3600, "")` → `7:42 PM (+1h from you)`
  - `TestErrorLine`: `context.DeadlineExceeded` (wrapped) → `Couldn't load full profile: timed out`; `&core.RateLimitedError{RetryAfter: 30*time.Second}` → `Rate limited — try again in 30s`; `errors.New("boom")` → `Couldn't load full profile: boom`.
  - `TestView_CachedOnly`: the name, `external`, `Loading profile…`, the footer; no `Email`/`Phone`/`Local time`.
  - `TestView_Loaded`: `@priya · she/her`, then the title, then `Local time`, `Email`, `Phone` in order; `APP` when `IsBot`; `deactivated` when `Deleted`.
  - `TestView_Error`: the name is still present; the error line is present; `Loading profile…` is absent.
  - `TestView_EmptyFieldsOmitted`: with Email/Phone/Title/Pronouns empty, no `Email`, no `Phone`, no ` · `, no `—`.
  - `TestView_NoTZOmitsLocalTime`: `TZ == ""` → no `Local time`.
  - `TestView_PresenceOnlyWhenKnown`: `""` → neither `active` nor `away`; `"away"` → `away`.
  - `TestView_StatusBlock`: `Live.Status` with text and DND → both lines present; a zero status → no blank block (no two consecutive empty inner rows).
  - `TestView_AvatarGutter`: `Avatar: "AAAA\nAAAA"` → the name row starts `AAAA  Priya`; `Avatar: ""` → it starts `Priya`.
  - `TestView_TruncatesToBoxWidth`: for termW 40 and 120, every box line's `emoji.Width` ≤ `min(56, termW-4)`, with a 200-rune title and a name of 30 `🎉`.
  - `TestView_ShortTerminalDropsRowsBottomUp`: as termH decreases, `Phone`/`Email`/`Local time` vanish before the status, the status before the title; name and handle remain at the smallest height that fits them.
  - `TestSetProfile_TargetMismatch`: the wrong user → false and the view still says `Loading profile…`; after `Close` → false.
- [ ] **Step 2: Run** `go test ./internal/ui/userprofile`. Expected: FAIL.
- [ ] **Step 3: Implement.** States are `loading → loaded | failed`, reset by `Open`. `errorLine` uses `errors.Is(err, context.DeadlineExceeded)` and `errors.As(err, **core.RateLimitedError)`, and rounds `RetryAfter` to seconds. Status lines come from `peerstatus.Status` (`Glyph`, `Summary`) at `Live.Now`. Lines are truncated with `…` using `emoji.Width`. Height priority is name/handle > title > status > details. The package doc says it moves onto the Phase 4 shared chrome when that lands.
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(ui): userprofile modal sub-model`.

---

### Task 3: App integration

**Files:**
- Modify:
  - `internal/ui/mode.go`: `ModeUserProfile` after `ModeWorkspaceSearch`; `String()` `"PROFILE"`; `IsModalOverlay`.
  - `internal/ui/keys.go`: `UserProfile` binding.
  - `internal/ui/mode_handlers.go`: table entry.
  - `internal/ui/mode_normal.go`: case beside `ListReactions` (~line 298).
  - `internal/ui/msgs.go`: message type.
  - `internal/ui/app.go`:
    - fields `userProfile *userprofile.Model`, `profileSvc core.ProfileService`, `now func() time.Time` (defaults to `time.Now`);
    - `SetProfileService`;
    - `reduceUserProfile` added to the `dispatchReducers` list (~line 939), before `reduceIO`.
  - `internal/ui/view_overlays.go`: render next to `reactionsView`; add to `overlayActive`.
  - `internal/ui/reducer_workspace.go`: close on `WorkspaceSwitchedMsg`.
  - `internal/ui/sidebar/model.go`: getter.
  - `internal/ui/golden_test.go`: set `a.now = goldenClock` in `newGoldenApp`.
- Create: `internal/ui/mode_user_profile.go`, `internal/ui/reducer_userprofile.go`
- Test: `internal/ui/mode_user_profile_test.go`, `internal/ui/reducer_userprofile_test.go`, `internal/ui/sidebar/model_test.go`, `internal/ui/sixelpaint_test.go`, `internal/ui/overlay/overlay_test.go`, `internal/ui/golden_test.go` + `testdata/golden/user_profile.ansi`

**Interfaces:**
- Consumes: Task 2's API; Task 1's port.
- Produces:
  ```go
  func (a *App) SetProfileService(s core.ProfileService)
  type UserProfileLoadedMsg struct{ TeamID, UserID string; Profile core.UserProfile; Err error }
  func (m *Model) PresenceByUser(userID string) (string, bool) // sidebar
  func (a *App) openUserProfile() tea.Cmd
  func handleUserProfileMode(a *App, msg tea.KeyMsg) tea.Cmd
  var reduceUserProfile reducerFunc
  ```

- [ ] **Step 1: Write failing tests:**
  - `TestUserProfileKey_OpensFromMessages` / `…FromThread` (`focusMessages` / `focusThreadPanel`, then `K` through `updateAndRender`): the mode is `ModeUserProfile`, `Target()` is `(activeTeam, selected.UserID)`, and the cmd is non-nil.
  - `TestUserProfileKey_NoSelectionToast`, `TestUserProfileKey_BotIDToast` (`UserID: "B123"`), `TestUserProfileKey_EmptyUserIDToast`: `statusbarText(a)` contains `No profile for this message`, the mode stays `ModeNormal`, and the cmd is nil.
  - `runKeyCases(t, ModeUserProfile, …)`: `K`, `esc`, `q` → `ModeNormal` and `!a.userProfile.IsVisible()`.
  - `TestUserProfileCmd_TenSecondDeadline`: a fake service records `ctx.Deadline()`; it is within 10 s ± 1 s of now; the msg carries the seeded team and user.
  - `TestReduceUserProfile_Applied`; `…_StaleUserDropped`; `…_DroppedAfterClose`; `…_DroppedAfterWorkspaceSwitch` (the switch closes the dialog and the mode becomes Normal; a late T1 msg changes nothing).
  - `TestModeUserProfile_IsModalOverlay`.
  - Sixel: add a `"user profile"` case to `TestCollectSixelPlacements_WithheldWhileAModalIsOpen`.
  - `TestUserProfile_LiveStatusUpdate`: while open, a `UserStatusChangeMsg` for the user → the frame contains the new status text.
  - `TestUserProfile_AvatarAppears`: the avatar service returns `""` and then `"AV"`; after `messages.AvatarReadyMsg{UserID}`, the frame contains `AV`.
  - `TestSidebar_PresenceByUser`: unknown → `("", false)`; after `UpdatePresenceByUser("U1","away")` → `("away", true)`.
  - `TestDimmedOverlayKeepsBoxKittyPlaceholders`: a placeholder-rune cell inside `box` survives byte-for-byte. The existing `TestDimmedOverlayDropsKittyPlaceholders` covers only the background.
  - `TestGolden_UserProfile`: `newGoldenApp`, select the first golden message, `K`, deliver a fixed loaded msg (with `TZOffset`, `TZAbbrev`, title, email), then `compareGolden(t, "user_profile", frame)`.
- [ ] **Step 2: Run** `go test ./internal/ui/... -run 'UserProfile|PresenceByUser|KittyPlaceholders|WithheldWhileAModal'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `openUserProfile` branches on `a.focusedPanel` like `openReactionsView` (`app.go:1210`).
  - It rejects an empty `UserID` or one with the `B` prefix with the toast.
  - It seeds from `a.userNames[userID]` and `a.externalUsers[userID]`, then calls `Open` and `SetMode(ModeUserProfile)`.
  - The cmd wraps `a.profileSvc.Profile` in `context.WithTimeout(context.Background(), 10*time.Second)`. With a nil `profileSvc`, the adapter's `ErrUnsupported` flows to `SetError`.
  - At render time, `Live` is built from `a.avatarFn`, `a.sidebar.PresenceByUser`, `a.presence.peers[userID]` and `a.now()`.
- [ ] **Step 4: Bless and run:** `go test ./internal/ui -run TestGolden_UserProfile -update`, inspect `testdata/golden/user_profile.ansi` (`cat` it in a terminal), then `go test ./internal/ui/...`. Expected: PASS, with no other golden changed (`git status testdata` shows only the new file).
- [ ] **Step 5: Commit** `feat(ui): K opens the author profile dialog`.

---

### Task 4: Production wiring in `cmd/slk`

**Files:**
- Modify: `internal/slack/client.go` (beside `GetUserProfile`, ~line 622), `cmd/slk/main.go` (beside `SetMessageService`, ~line 934)
- Create: `cmd/slk/profile_service.go`, `cmd/slk/profile_service_test.go`

**Interfaces:**
- Consumes: Task 1's types; Task 3's `App.SetProfileService`.
- Produces:
  ```go
  func (c *Client) GetUserProfileContext(ctx context.Context, userID string) (*slack.User, error) // api.GetUserInfoContext
  func toCoreProfile(u *slack.User, at time.Time) core.UserProfile
  func zoneAbbrev(tz string, at time.Time) string // "" on load failure or numeric ("-03")
  func newProfileCache(ttl time.Duration, now func() time.Time,
      fetch func(context.Context, string, string) (core.UserProfile, error)) *profileCache
  func (c *profileCache) Profile(ctx context.Context, teamID, userID string) (core.UserProfile, error)
  ```

- [ ] **Step 1: Write failing tests:**
  - `TestToCoreProfile_MapsFields`: `Name→Handle`, `RealName`, `Profile.DisplayName`, `Profile.Title`, `Profile.Pronouns`, `Profile.Email`, `Profile.Phone`, `TZ`, `TZOffset`, `TeamID`, `IsBot || IsAppUser → IsBot`, `Deleted`.
  - `TestZoneAbbrev`: `("America/Los_Angeles", 2026-09-30)` → `PDT`; `"America/Sao_Paulo"` → `""`; `"Not/AZone"` → `""`; `""` → `""`.
  - `TestProfileCache_HitWithinTTL` (1 fetch for 2 calls); `…_ExpiresAfterTTL` (fake clock +5m1s → 2 fetches); `…_ErrorsNotCached`; `…_KeyedByTeam` (U1 in T1 and T2 → 2 fetches).
  - `TestProfileFetch_RateLimitWrapped`: `*slack.RateLimitedError{RetryAfter: 30s}` from the client → `errors.As` yields `*core.RateLimitedError` with 30s.
  - `TestProfileFetch_UnknownWorkspace`: `router.ByID` nil → error contains `workspace "T9" is unavailable`.
- [ ] **Step 2: Run** `go test ./cmd/slk -run 'TestToCoreProfile|TestZoneAbbrev|TestProfileCache|TestProfileFetch'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Cache key `teamID+"/"+userID`, guarded by a mutex. The fetch resolves `router.ByID(teamID)` (the pattern `Forward` uses at `main.go:937`), calls `GetUserProfileContext`, and maps the error. Register it with `app.SetProfileService(core.NewProfileService(core.ProfileServiceFuncs{Profile: newProfileCache(5*time.Minute, time.Now, fetch).Profile}))`.
- [ ] **Step 4: Run.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(slk): users.info-backed ProfileService with 5m TTL cache`.

---

### Task 5: Demo fake

**Files:**
- Modify: `internal/demo/world.go` (the `user` struct gains `title, pronouns, tz, tzAbbrev string; tzOffset int`, and the cast is filled in), `internal/demo/services.go` (a `profiles core.ProfileService` field), `internal/demo/demo.go` (after `SetPresenceService`, ~line 92: `app.SetProfileService(s.profiles)`)
- Test: `internal/demo/services_test.go`

**Interfaces:**
- Consumes: Task 1's port; Task 3's setter.

- [ ] **Step 1: Write failing test** `TestDemoProfiles_ReturnCast`: every user in every demo team resolves with a non-empty `Title` and `TZ`, and at least one has a half-hour `TZOffset` (`%3600 != 0`). An unknown ID → error.
- [ ] **Step 2: Run** `go test ./internal/demo -run TestDemoProfiles`. Expected: FAIL.
- [ ] **Step 3: Implement** the fake over `world`, with fixed titles, pronouns and zones (include `Asia/Kolkata`, `IST`, 19800).
- [ ] **Step 4: Run** `go test ./internal/demo`. Expected: PASS, including its `boundary_test.go`.
- [ ] **Step 5: Commit** `feat(demo): fake profiles for the demo cast`.

---

### Task 6: Docs and full verification

**Files:**
- Modify: `AGENTS.md`: "13 modal packages" → "14" at both mentions (Architecture block and Known duplication).

- [ ] **Step 1: Edit AGENTS.md** as above.
- [ ] **Step 2: Run** `go build ./... && go vet ./... && test -z "$(gofmt -l .)" && golangci-lint run && go test ./... -race`. Expected: exit 0, every package `ok`.
- [ ] **Step 3: Smoke test:** `go run ./cmd/slk --demo`, select a message, `K`. Expected: the dialog with avatar, title and local time; `K` closes it.
- [ ] **Step 4: Commit** `docs: count userprofile among modal packages`.
