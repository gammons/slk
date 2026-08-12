# Edge Health + Batched User Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop paying full price for broken edge resolution on Grid (23 wasted `edge:channels/info` per boot) and coalesce per-miss `users.info` fan-out (measured 282 in one Grid session) into batched `edge.UsersInfo` calls — per `docs/superpowers/specs/2026-08-05-edge-health-and-batched-resolver-design.md`.

**Architecture:** A session-scoped `edge.Health` tracker per workspace; bootstrap marks it degraded when the largest context-team group fails wholesale (≥50% of ids) and aborts the remaining groups. `userResolver` queues misses for 200ms and flushes them as one `edge.UsersInfo` batch, falling back to today's per-user path for anything edge doesn't return, on edge error, or when the workspace is degraded.

**Tech Stack:** Go, stdlib. New cache upsert method (SQLite).

**Landmine the plan is designed around (verified by reading the code):**
`cache.UpdateUserFromEdge` (internal/cache/edge_sync.go:207) is UPDATE-only — it writes nothing for a row that does not exist. Bootstrap gets away with it because `hydrateFirstSight` inserts placeholder rows first. The resolver's misses are by definition rows that do NOT exist, so the resolver's edge path must use a NEW upsert method (`UpsertUserFromEdge`, Task 3), never `UpdateUserFromEdge`.

**Fixture facts:**
- `cannedBootResult` teams: C_GENERAL+D_ALICE are T_HOME, C_PRIVATE+D_BOB are T_OTHER, 2 ids each — equal sizes, so the new size-descending/alpha-tiebreak order keeps T_HOME first and existing tests are unaffected.
- The bootstrap fake already supports per-team error injection (`channelsInfoErrFor`) and filters canned `FailedIDs` to the asked ids.
- Existing resolver tests (cmd/slk/user_resolver_test.go) construct with `newUserResolver("T1", newTestClient(t, srv), db, nil, nil)` — they get two new nil params and must behave identically (nil batcher → per-user path).
- `avatar.Cache.Preload(userID, "")` is safe on a nil cache; a non-empty URL with a nil cache is not. Tests pass empty avatar URLs or a real cache, matching the existing convention.

> **Amendment record (post-implementation):** two code-review rounds changed Task 2's exact code after this plan was written: the majority base is NON-IM ids on both sides (not `len(groups[team])*2 >= len(updated)`), the wholesale comment reads "can only fail wholesale via callErr", and a 50/50 tie is documented as intentionally not-a-majority (commit 9a69d04). Task 6's ResolveNow race comment and the empty-name fall-through in both `resolveDMNames` and `flush` were added in commits 26a6ac6 and 6a584d7. The snippets below are the plan of record at write time; the code and its comments are authoritative.

---

### Task 1: edge.Health

**Files:**
- Create: `internal/slack/edge/health.go`
- Test: `internal/slack/edge/health_test.go`

- [ ] **Step 1: Write the failing test**

```go
package edge

import "testing"

func TestHealth(t *testing.T) {
	h := NewHealth()
	if h.Degraded() {
		t.Error("a new Health is degraded; degradation must be earned by an observed wholesale failure")
	}
	h.MarkDegraded()
	h.MarkDegraded() // idempotent
	if !h.Degraded() {
		t.Error("MarkDegraded did not stick")
	}
}
```

- [ ] **Step 2: Run it, watch it fail to build**

Run: `go test ./internal/slack/edge/ -run TestHealth 2>&1 | tail -2`
Expected: undefined NewHealth.

- [ ] **Step 3: Implement**

```go
package edge

import "sync/atomic"

// Health records whether edge resolution is working for one workspace
// this session. Two states — unknown and degraded — and deliberately
// nothing else: it exists so that a workspace where edge resolution
// fails wholesale (Enterprise Grid today: the enterprise-id group
// resolves nothing, the foreign teams are Unauthenticated) pays for
// that discovery once per boot instead of once per call site.
//
// Session-scoped by construction. Persisting pessimism would risk
// suppressing the real Grid-scoping fix when it lands, and a cold
// boot re-discovers degradation for the cost of a handful of calls.
type Health struct {
	degraded atomic.Bool
}

// NewHealth returns a Health in the unknown (not degraded) state.
func NewHealth() *Health { return &Health{} }

// MarkDegraded latches the degraded state. Idempotent; there is no
// path back within a session.
func (h *Health) MarkDegraded() { h.degraded.Store(true) }

// Degraded reports whether edge resolution has failed wholesale this
// session. Nil-safe: a nil *Health reads as not degraded, so callers
// wired optionally need no guard.
func (h *Health) Degraded() bool { return h != nil && h.degraded.Load() }
```

- [ ] **Step 4: Run the package tests**

Run: `go test -count=1 ./internal/slack/edge/ 2>&1 | tail -2`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/edge/health.go internal/slack/edge/health_test.go
git commit -m "feat(edge): session-scoped per-workspace health signal"
```

---

### Task 2: bootstrap — mark degraded on wholesale failure, abort remaining groups

**Files:**
- Modify: `internal/bootstrap/bootstrap.go` (Deps gains Health)
- Modify: `internal/bootstrap/revalidate.go` (ordering + wholesale check + abort; revalidateChannelTeam returns an outcome)
- Test: `internal/bootstrap/revalidate_test.go`

- [ ] **Step 1: Deps.Health**

In `internal/bootstrap/bootstrap.go`, add to the Deps struct after `Revalidate Revalidator`:

```go
	// Health, when non-nil, is marked degraded when the largest
	// context-team group fails wholesale (every non-IM id unresolved)
	// and holds at least half of all revalidated ids; the remaining
	// groups are then aborted. Nil disables the check. See
	// revalidateChannels for the reasoning.
	Health *edge.Health
```

Run: `go build ./internal/bootstrap/`
Expected: builds (the field is inert until Step 4).

- [ ] **Step 2: Write the failing tests**

Add to `internal/bootstrap/revalidate_test.go`:

```go
// --- edge health ------------------------------------------------------

// bigTeamFixture builds a boot whose conversations partition into one
// 6-id T_BIG group and one 2-id T_SMALL group, all channels (no IMs
// unless added by the test). 6 of 8 ids is over the majority
// threshold; T_BIG sorts first by size.
func bigTeamFixture(f *fakeDeps) {
	f.bootRes.Channels = []boot.Channel{
		{ID: "C_BIG1", Name: "big1", ContextTeamID: "T_BIG"},
		{ID: "C_BIG2", Name: "big2", ContextTeamID: "T_BIG"},
		{ID: "C_BIG3", Name: "big3", ContextTeamID: "T_BIG"},
		{ID: "C_BIG4", Name: "big4", ContextTeamID: "T_BIG"},
		{ID: "C_BIG5", Name: "big5", ContextTeamID: "T_BIG"},
		{ID: "C_BIG6", Name: "big6", ContextTeamID: "T_BIG"},
		{ID: "C_SMALL1", Name: "small1", ContextTeamID: "T_SMALL"},
		{ID: "C_SMALL2", Name: "small2", ContextTeamID: "T_SMALL"},
	}
	f.bootRes.IMs = nil
}

func TestRevalidate_WholesaleFailureMarksDegradedAndAborts(t *testing.T) {
	// Measured on the first working Grid session (2026-08-05): one
	// enterprise-id group holding 79% of the user's conversations
	// resolved none of them, and the 16 foreign-team groups behind it
	// were all Unauthenticated — 23 wasted edge calls per boot. The
	// largest group failing wholesale IS the diagnosis; the rest of
	// the partition is not worth spending requests on.
	f := openedFake()
	bigTeamFixture(f)
	f.deps.Health = edge.NewHealth()
	f.channelsInfoRes.FailedIDs = []string{"C_BIG1", "C_BIG2", "C_BIG3", "C_BIG4", "C_BIG5", "C_BIG6"}
	// The canned response's Channels/queried sets name fixture ids
	// this boot does not have, so filterChannelsInfo reduces them to
	// nothing for these ids — only the failed ids apply.

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.deps.Health.Degraded() {
		t.Error("the majority group failed wholesale and edge was not marked degraded; the resolver will keep spending edge calls on a workspace where they resolve nothing")
	}
	if len(f.channelsInfoCalls) != 1 {
		t.Errorf("channels/info calls = %d; want 1 — after a wholesale failure of the majority group the remaining teams are aborted (calls: %+v)", len(f.channelsInfoCalls), f.channelsInfoCalls)
	}
	if f.channelsInfoCalls[0].team != "T_BIG" {
		t.Errorf("first call went to %q; want T_BIG — groups are processed largest-first so the diagnosis comes before the spend", f.channelsInfoCalls[0].team)
	}
	if !f.loggedMatching("degraded") {
		t.Errorf("a wholesale edge failure must say what it decided (logged: %v)", f.logged())
	}
}

func TestRevalidate_CallErrorOfTheMajorityGroupAlsoMarksDegraded(t *testing.T) {
	// The other wholesale shape: not failed_ids but an error —
	// ratelimited, or Grid's Unauthenticated.
	f := openedFake()
	bigTeamFixture(f)
	f.deps.Health = edge.NewHealth()
	f.channelsInfoErrFor = map[string]error{"T_BIG": errors.New("Unauthenticated")}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.deps.Health.Degraded() {
		t.Error("an errored majority group did not mark edge degraded")
	}
	if len(f.channelsInfoCalls) != 1 {
		t.Errorf("channels/info calls = %d; want 1", len(f.channelsInfoCalls))
	}
}

func TestRevalidate_IMOnlyFailureDoesNotMarkDegraded(t *testing.T) {
	// IMs ALWAYS land in failed_ids — 22 of 22 across the captures,
	// on healthy workspaces. A group whose only failures are IMs is
	// the normal case, and marking it degraded would disable edge
	// batching everywhere.
	f := openedFake()
	f.bootRes.Channels = nil
	f.bootRes.IMs = []boot.IM{
		{ID: "D_A", UserID: "U_ALICE", IsIM: true, IsOpen: true, ContextTeamID: "T_HOME"},
		{ID: "D_B", UserID: "U_BOB", IsIM: true, IsOpen: true, ContextTeamID: "T_HOME"},
	}
	f.deps.Health = edge.NewHealth()
	f.channelsInfoRes.FailedIDs = []string{"D_A", "D_B"}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.deps.Health.Degraded() {
		t.Error("IM-only failures marked edge degraded; IMs land in failed_ids on every healthy workspace")
	}
}

func TestRevalidate_HealthyRunDoesNotMarkDegraded(t *testing.T) {
	f := openedFake()
	f.deps.Health = edge.NewHealth()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.deps.Health.Degraded() {
		t.Error("a healthy revalidation marked edge degraded")
	}
}

func TestRevalidate_MinorityWholesaleFailureDoesNotAbort(t *testing.T) {
	// The threshold is the majority: a failing group that holds less
	// than half the ids is one team's problem, not a broken edge.
	// Here T_BIG holds 3 of 7 — wholesale-failed, but a minority —
	// so all three teams are still called and nothing is marked.
	f := openedFake()
	f.bootRes.Channels = []boot.Channel{
		{ID: "C_BIG1", Name: "big1", ContextTeamID: "T_BIG"},
		{ID: "C_BIG2", Name: "big2", ContextTeamID: "T_BIG"},
		{ID: "C_BIG3", Name: "big3", ContextTeamID: "T_BIG"},
		{ID: "C_MID1", Name: "mid1", ContextTeamID: "T_MID"},
		{ID: "C_MID2", Name: "mid2", ContextTeamID: "T_MID"},
		{ID: "C_SMALL1", Name: "small1", ContextTeamID: "T_SMALL"},
		{ID: "C_SMALL2", Name: "small2", ContextTeamID: "T_SMALL"},
	}
	f.bootRes.IMs = nil
	f.deps.Health = edge.NewHealth()
	f.channelsInfoErrFor = map[string]error{"T_BIG": errors.New("ratelimited")}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.deps.Health.Degraded() {
		t.Error("a minority group's failure marked edge degraded; the threshold is the majority of ids")
	}
	if len(f.channelsInfoCalls) != 3 {
		t.Errorf("channels/info calls = %d; want 3 — a minority failure aborts nothing", len(f.channelsInfoCalls))
	}
}
```

Run: `go test ./internal/bootstrap/ -run 'TestRevalidate_Wholesale|TestRevalidate_CallError|TestRevalidate_IMOnly|TestRevalidate_HealthyRun|TestRevalidate_Minority' 2>&1 | tail -6`
Expected: FAIL — Degraded is never set, calls are never aborted (4 of 5 fail); `TestRevalidate_HealthyRunDoesNotMarkDegraded` and possibly `TestRevalidate_IMOnlyFailureDoesNotMarkDegraded` may pass already (nothing marks anything yet). If a PRE-EXISTING test fails, the fixture helpers are wrong — fix them, not the old tests.

- [ ] **Step 3: Implement the wholesale check and abort**

In `internal/bootstrap/revalidate.go`:

Add `"strings"` to imports (for the tiebreak).

In `revalidateChannels`, replace the group-building-and-iteration tail (from `groups := make(...)` through the `for _, team := range teams` loop) with:

```go
	// noteIM tracks which ids are IMs: they ALWAYS land in failed_ids
	// (22 of 22 across the captures, healthy workspaces included), so
	// the wholesale-failure check below must not count them, or every
	// healthy workspace would trip it.
	noteIM := make(map[string]bool, len(out.IMs))
	for _, im := range out.IMs {
		noteIM[im.ID] = true
	}

	groups := make(map[string]map[string]int64)
	for id, version := range updated {
		team := teamOf[id]
		if team == "" {
			team = deps.WorkspaceID
		}
		if groups[team] == nil {
			groups[team] = make(map[string]int64)
		}
		groups[team][id] = version
	}
	// Largest group first, alphabetical on ties. On a Grid org the
	// enterprise-id group holds the overwhelming majority of a user's
	// conversations (measured: 218 of 277), and judging it first means
	// a wholesale edge failure is diagnosed before the remaining
	// groups spend their requests. Deterministic order also keeps the
	// debug log readable and tests able to rely on call order.
	teams := slices.Collect(maps.Keys(groups))
	slices.SortFunc(teams, func(a, b string) int {
		if d := len(groups[b]) - len(groups[a]); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	for i, team := range teams {
		outcome := revalidateChannelTeam(ctx, deps, team, groups[team], logf)
		// Only the first (largest) group can trip the wholesale
		// check: later groups are smaller, so if the largest holds
		// under half the ids no group reaches the majority threshold.
		if i == 0 && deps.Health != nil && len(groups[team])*2 >= len(updated) && wholesaleFailure(outcome, groups[team], noteIM) {
			deps.Health.MarkDegraded()
			logf("bootstrap: channels/info for team %s failed wholesale (%d of %d ids); marking edge degraded for this session and skipping the remaining %d team group(s)",
				team, len(groups[team]), len(updated), len(teams)-1)
			return
		}
	}
```

Change `revalidateChannelTeam` to RETURN an outcome. Add above it:

```go
// teamOutcome is what one team's revalidation learned, for the
// wholesale-failure check in revalidateChannels. callErr covers both
// transport and ok:false failures (the body's ids are all
// unresolved either way); failed is the failed_ids set.
type teamOutcome struct {
	callErr bool
	failed  map[string]struct{}
}

// wholesaleFailure reports whether a team's call resolved NOTHING it
// was asked about: an errored call, or every non-IM id in failed_ids.
// IMs are excluded — they always fail (see revalidateChannels), so
// counting them would trip the check on healthy workspaces. A group
// whose response is empty because everything was already current is
// NOT a wholesale failure: nothing in failed_ids means nothing
// failed. A group with no non-IM ids can never fail wholesale.
func wholesaleFailure(oc teamOutcome, group map[string]int64, noteIM map[string]bool) bool {
	if oc.callErr {
		return true
	}
	nonIM := 0
	for id := range group {
		if noteIM[id] {
			continue
		}
		nonIM++
		if _, failed := oc.failed[id]; !failed {
			return false
		}
	}
	return nonIM > 0
}
```

And in `revalidateChannelTeam` itself: signature becomes `func revalidateChannelTeam(ctx context.Context, deps Deps, team string, updated map[string]int64, logf func(string, ...any)) teamOutcome`. The error branch:

```go
	res, err := deps.Revalidate.ChannelsInfo(ctx, team, updated)
	if err != nil {
		// Everything below is discarded along with it. Slack answers
		// ok:false with a populated body, so "err != nil and the
		// value looks fine" is the normal shape of a failure here.
		logf("bootstrap: channels/info for team %s: %d conversations: %v (leaving them stale)", team, len(updated), err)
		return teamOutcome{callErr: true}
	}
```

and the end of the function:

```go
	failed := make(map[string]struct{}, len(res.FailedIDs))
	for _, id := range res.FailedIDs {
		failed[id] = struct{}{}
	}
	return teamOutcome{failed: failed}
}
```

(The failed-ids log line and membership logic stay exactly as they are, above this new tail.)

- [ ] **Step 4: Run the bootstrap suite**

Run: `go test -count=1 ./internal/bootstrap/ 2>&1 | tail -3`
Expected: PASS, all of it — the five new tests and every pre-existing one (the fixture's groups are equal-sized, so the tiebreak keeps T_HOME first and old order-dependent tests are unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/
git commit -m "feat(bootstrap): mark edge degraded and abort remaining teams on wholesale failure

Measured on the first working Grid session: one enterprise-id group
holding 79% of the user's conversations resolved none of them, and
the 16 foreign-team groups behind it were all Unauthenticated — 23
wasted edge calls per boot. The largest group is now processed
first; if it fails wholesale (every non-IM id unresolved) and holds
at least half the ids, edge is marked degraded for the session and
the rest of the partition is skipped. IMs are excluded from the
check because they always land in failed_ids, healthy workspaces
included."
```

---

### Task 3: cache upsert + batched userResolver

**Files:**
- Modify: `internal/cache/edge_sync.go` (new UpsertUserFromEdge)
- Test: `internal/cache/edge_sync_test.go`
- Modify: `cmd/slk/main.go` (userResolver batching; newUserResolver signature)
- Test: `cmd/slk/user_resolver_test.go`

- [ ] **Step 1: Write the failing cache test**

Add to `internal/cache/edge_sync_test.go`:

```go
func TestUpsertUserFromEdge(t *testing.T) {
	db, err := cache.New(":memory:") // adjust to this file's construction convention
	...
}
```

Write it to match the file's existing construction style (read the file first; several tests there build a db and seed users). It must assert ALL of:

1. A user that does not exist is INSERTED with name, display name, version, is_bot, is_external set.
2. For an existing row with an avatar, an update with EMPTY AvatarURL preserves the stored avatar (the UpdateUserFromEdge contract, kept for the upsert).
3. An update with a non-empty AvatarURL replaces it.
4. `version` is written (assert via `db.UserVersions`).

- [ ] **Step 2: Run it, watch it fail**

Run: `go test ./internal/cache/ -run TestUpsertUserFromEdge 2>&1 | tail -2`
Expected: undefined UpsertUserFromEdge.

- [ ] **Step 3: Implement UpsertUserFromEdge**

In `internal/cache/edge_sync.go`, after `UpdateUserFromEdge`:

```go
// UpsertUserFromEdge is UpdateUserFromEdge with row-creating
// semantics, for callers whose whole case is that the row does not
// exist yet — the batched user resolver's cache misses. (Bootstrap
// uses UpdateUserFromEdge because hydrateFirstSight has already
// inserted placeholder rows; UPDATE-only there is a feature: it
// cannot invent rows.) workspaceID is taken explicitly because
// EdgeUserUpdate does not carry it.
//
// The preserve-on-empty avatar contract is identical to
// UpdateUserFromEdge, and for the same reason. presence and
// updated_at are never touched: on insert they take the column
// defaults, on conflict they keep what they had.
func (db *DB) UpsertUserFromEdge(workspaceID string, u EdgeUserUpdate) error {
	var err error
	if u.AvatarURL != "" {
		_, err = db.conn.Exec(`
			INSERT INTO users (id, workspace_id, name, display_name, avatar_url, is_bot, is_external, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name=excluded.name,
				display_name=excluded.display_name,
				avatar_url=excluded.avatar_url,
				is_bot=excluded.is_bot,
				is_external=excluded.is_external,
				version=excluded.version
		`, u.ID, workspaceID, u.Name, u.DisplayName, u.AvatarURL, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version)
	} else {
		_, err = db.conn.Exec(`
			INSERT INTO users (id, workspace_id, name, display_name, is_bot, is_external, version)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name=excluded.name,
				display_name=excluded.display_name,
				is_bot=excluded.is_bot,
				is_external=excluded.is_external,
				version=excluded.version
		`, u.ID, workspaceID, u.Name, u.DisplayName, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version)
	}
	if err != nil {
		return fmt.Errorf("upserting user %s from edge: %w", u.ID, err)
	}
	return nil
}
```

- [ ] **Step 4: Run cache tests**

Run: `go test -count=1 ./internal/cache/ 2>&1 | tail -2`
Expected: PASS.

- [ ] **Step 5: Commit the cache half**

```bash
git add internal/cache/
git commit -m "feat(cache): UpsertUserFromEdge — row-creating edge user write for the resolver"
```

- [ ] **Step 6: Write the failing resolver tests**

Add to `cmd/slk/user_resolver_test.go`. First the helpers:

```go
// fakeBatcher implements userBatcher and records each batch it was
// asked for.
type fakeBatcher struct {
	mu   sync.Mutex
	sent []map[string]int64
	res  []edge.User
	err  error
}

func (f *fakeBatcher) UsersInfo(_ context.Context, updatedIDs map[string]int64) ([]edge.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make(map[string]int64, len(updatedIDs))
	for k, v := range updatedIDs {
		cp[k] = v
	}
	f.sent = append(f.sent, cp)
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func (f *fakeBatcher) calls() []map[string]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]int64(nil), f.sent...)
}

// edgeUserRecord builds an edge.User, whose Profile is an anonymous
// struct with no spellable type at a call site.
func edgeUserRecord(id, name, display, real, teamID string, version int64, isBot bool) edge.User {
	u := edge.User{ID: id, Name: name, Version: version, IsBot: isBot, TeamID: teamID}
	u.Profile.DisplayName = display
	u.Profile.RealName = real
	return u
}
```

Then the tests:

```go
func TestUserResolver_BatchesMissesThroughEdge(t *testing.T) {
	// Measured on the first working Grid session: 282 users.info
	// calls, one per miss, with no coalescing. edge users/info takes
	// 80 ids a request and returns full records inline — the same
	// misses are ~4 requests.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1783337599010, false),
		edgeUserRecord("U002", "bob", "", "Bob Real", "T1", 1783337599011, false),
	}}
	var sentMu sync.Mutex
	var sent []tea.Msg
	r := newUserResolver("T1", nil, db, nil, func(m tea.Msg) {
		sentMu.Lock()
		sent = append(sent, m)
		sentMu.Unlock()
	}, batcher, nil)

	r.Request("U001")
	r.Request("U002")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(batcher.calls()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	calls := batcher.calls()
	if len(calls) != 1 {
		t.Fatalf("edge batches = %d; want 1 — misses inside the window coalesce (sent: %v)", len(calls), calls)
	}
	want := map[string]int64{"U001": 0, "U002": 0}
	if !reflect.DeepEqual(calls[0], want) {
		t.Errorf("batch = %v; want %v — 0 is the protocol's 'never seen, send the full record'", calls[0], want)
	}

	// Cached, so a second Request is a no-op.
	for _, id := range []string{"U001", "U002"} {
		u, err := db.GetUser(id)
		if err != nil {
			t.Fatalf("%s was not cached from the edge batch: %v", id, err)
		}
		if u.Version == 0 {
			t.Errorf("%s cached with version 0; the batch returned one and conditional revalidation reads it", id)
		}
	}
	// Display-name chain: display when set, real otherwise.
	u1, _ := db.GetUser("U001")
	if u1.DisplayName != "Alice" {
		t.Errorf("U001 display = %q; want Alice", u1.DisplayName)
	}
	u2, _ := db.GetUser("U002")
	if u2.DisplayName != "Bob Real" {
		t.Errorf("U002 display = %q; want the real-name fallback", u2.DisplayName)
	}
	// One UserResolvedMsg per resolved user, same as the per-user path.
	sentMu.Lock()
	resolved := 0
	for _, m := range sent {
		if _, ok := m.(ui.UserResolvedMsg); ok {
			resolved++
		}
	}
	sentMu.Unlock()
	if resolved != 2 {
		t.Errorf("UserResolvedMsg count = %d; want 2 — the UI patches display names live from these", resolved)
	}
	// Dedup end-to-end: a repeat Request resolves nothing further.
	r.Request("U001")
	time.Sleep(userResolverBatchWindow + 300*time.Millisecond)
	if n := len(batcher.calls()); n != 1 {
		t.Errorf("a repeat Request produced batch %d; the cache check must make it a no-op", n)
	}
}

func TestUserResolver_EdgeMissFallsBackToPerUser(t *testing.T) {
	// ids edge does not return are resolved the old way — absence
	// from the batch means "could not resolve", and a raw user id on
	// screen is the failure this whole path exists to avoid.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1, false),
	}}
	var profiles atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profiles.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U002","name":"bob","team_id":"T1","profile":{"display_name":"Bob"}}}`))
	}))
	defer srv.Close()

	r := newUserResolver("T1", newTestClient(t, srv), db, nil, nil, batcher, nil)
	r.Request("U001")
	r.Request("U002")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := db.GetUser("U002"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := db.GetUser("U002"); err != nil {
		t.Fatal("U002 was absent from the edge batch and was never resolved per-user")
	}
	if got := profiles.Load(); got != 1 {
		t.Errorf("per-user users.info calls = %d; want 1 — only the id edge missed", got)
	}
}

func TestUserResolver_EdgeErrorFallsBackToPerUser(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{err: errors.New("ratelimited")}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U001","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
	}))
	defer srv.Close()

	r := newUserResolver("T1", newTestClient(t, srv), db, nil, nil, batcher, nil)
	r.Request("U001")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := db.GetUser("U001"); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the edge call failed and the per-user fallback never ran")
}

func TestUserResolver_DegradedWorkspaceSkipsEdge(t *testing.T) {
	// Once boot has marked a workspace's edge broken, the resolver
	// must not spend even one call discovering it again.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1, false),
	}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U001","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
	}))
	defer srv.Close()

	r := newUserResolver("T1", newTestClient(t, srv), db, nil, nil, batcher, func() bool { return true })
	r.Request("U001")
	time.Sleep(userResolverBatchWindow + 300*time.Millisecond)

	if n := len(batcher.calls()); n != 0 {
		t.Errorf("a degraded workspace made %d edge calls; want 0", n)
	}
	if _, err := db.GetUser("U001"); err != nil {
		t.Error("U001 was not resolved per-user on the degraded path")
	}
}
```

Add imports as needed: `context`, `errors`, `reflect`, `sync`, `tea "charm.land/bubbletea/v2"`, `"github.com/gammons/slk/internal/slack/edge"`, `"github.com/gammons/slk/internal/ui"`.

- [ ] **Step 7: Run them, watch them fail**

Run: `go test ./cmd/slk/ -run 'TestUserResolver_' 2>&1 | tail -4`
Expected: build failure — newUserResolver takes 5 args today.

- [ ] **Step 8: Implement the batching**

In `cmd/slk/main.go`:

Add near `userResolverConcurrency`:

```go
// userResolverBatchWindow is how long Request waits for more misses
// to coalesce before flushing them as one edge users/info call. The
// render path resolves a channel's unknown authors in a single
// burst, so a short window turns a channel open from N requests into
// one; 200 ms is below what a person perceives as resolution lag.
const userResolverBatchWindow = 200 * time.Millisecond
```

Add the interface (above the userResolver struct):

```go
// userBatcher is the edge.UsersInfo subset the resolver batches
// misses through. *edge.Client satisfies it structurally; nil means
// no batching and every miss takes the per-user users.info path.
type userBatcher interface {
	UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]edge.User, error)
}
```

Extend the struct:

```go
	batcher  userBatcher
	degraded func() bool // nil: never degraded

	pendingMu  sync.Mutex
	pending    map[string]struct{}
	flushTimer *time.Timer
```

Extend newUserResolver's signature with `batcher userBatcher, degraded func() bool` (append them) and initialise `pending: map[string]struct{}{}` in the literal.

Rewrite Request's tail — replace the `go func() {...}()` block and everything after the cache check with:

```go
	if r.batcher == nil || (r.degraded != nil && r.degraded()) {
		go r.resolveOne(userID)
		return
	}
	r.pendingMu.Lock()
	r.pending[userID] = struct{}{}
	if r.flushTimer == nil {
		r.flushTimer = time.AfterFunc(userResolverBatchWindow, r.flush)
	}
	r.pendingMu.Unlock()
}
```

Turn the old goroutine body into a method (its content unchanged):

```go
// resolveOne resolves a single user through the Web API users.info
// path: the pre-batch behaviour, now the fallback for ids edge did
// not return, for a failed edge call, and for workspaces whose edge
// is degraded. Callers run it on its own goroutine.
func (r *userResolver) resolveOne(userID string) {
	defer r.inflight.Delete(userID)
	if r.sem != nil {
		r.sem <- struct{}{}
		defer func() { <-r.sem }()
	}
	u, err := r.client.GetUserProfile(userID)
	... // the rest of the old body, verbatim
}
```

Add the flush and the edge-record application:

```go
// flush resolves everything queued since the window opened, as one
// edge users/info batch. Anything the batch does not return falls
// back to the per-user path: absence from the batch means "could not
// resolve", and an errored batch resolves nothing at all.
func (r *userResolver) flush() {
	r.pendingMu.Lock()
	ids := make([]string, 0, len(r.pending))
	for id := range r.pending {
		ids = append(ids, id)
	}
	clear(r.pending)
	r.flushTimer = nil
	r.pendingMu.Unlock()
	if len(ids) == 0 {
		return
	}
	// Re-check at flush time: boot may have marked the workspace
	// degraded while the window was open.
	if r.degraded != nil && r.degraded() {
		for _, id := range ids {
			go r.resolveOne(id)
		}
		return
	}
	updated := make(map[string]int64, len(ids))
	for _, id := range ids {
		// 0 is the conditional protocol's "never seen, send the full
		// record" — the resolver only ever queues cache misses.
		updated[id] = 0
	}
	users, err := r.batcher.UsersInfo(context.Background(), updated)
	if err != nil {
		debuglog.Cache("userResolver: edge users/info for %d users team=%s: %v (falling back to per-user users.info)", len(ids), r.teamID, err)
		for _, id := range ids {
			go r.resolveOne(id)
		}
		return
	}
	returned := make(map[string]struct{}, len(users))
	for _, u := range users {
		returned[u.ID] = struct{}{}
		r.applyEdgeUser(u)
	}
	for _, id := range ids {
		if _, ok := returned[id]; !ok {
			go r.resolveOne(id)
		}
	}
}

// applyEdgeUser records one user the edge batch returned: cache row
// (created — these are misses), avatar preload, and the same
// UserResolvedMsg/UserExternalMsg pair the per-user path emits, so
// the UI cannot tell the two paths apart.
func (r *userResolver) applyEdgeUser(u edge.User) {
	defer r.inflight.Delete(u.ID)
	name := u.Profile.DisplayName
	if name == "" {
		name = u.Profile.RealName
	}
	if name == "" {
		name = u.Name
	}
	isExternal := u.TeamID != "" && u.TeamID != r.teamID
	r.avatars.Preload(u.ID, u.Profile.ImageOriginal)
	_ = r.db.UpsertUserFromEdge(r.teamID, cache.EdgeUserUpdate{
		ID:          u.ID,
		Name:        u.Name,
		DisplayName: name,
		AvatarURL:   u.Profile.ImageOriginal,
		IsBot:       u.IsBot,
		IsExternal:  isExternal,
		Version:     u.Version,
	})
	if r.send != nil {
		r.send(ui.UserResolvedMsg{
			TeamID:      r.teamID,
			UserID:      u.ID,
			DisplayName: name,
			IsBot:       u.IsBot,
		})
	}
	if isExternal && r.send != nil {
		r.send(ui.UserExternalMsg{UserID: u.ID, IsExternal: true})
	}
}
```

Note: `avatars.Preload(u.ID, u.Profile.ImageOriginal)` — edge returns `image_original` (absolute URL), not the sized `image_32` the per-user path uses. Preload takes any URL. If the receiver is nil and the URL is non-empty it panics, same as the existing path's contract; production always wires a real avatar cache.

- [ ] **Step 9: Update the existing resolver tests' constructor calls**

The two pre-existing tests pass `newUserResolver("T1", newTestClient(t, srv), db, nil, nil)` — append `, nil, nil` (nil batcher, nil degraded → per-user path, behaviour unchanged). Any other newUserResolver call sites in tests get the same treatment EXCEPT the production call, which is Task 4.

Run: `go test -count=1 ./cmd/slk/ -run 'TestUserResolver' -v 2>&1 | tail -12`
Expected: all PASS, old and new.

- [ ] **Step 10: Commit**

```bash
git add cmd/slk/main.go cmd/slk/user_resolver_test.go
git commit -m "feat(slk): batch resolver misses through edge users/info

Measured on the first working Grid session: 282 users.info calls in
one session, one per cache miss. Misses now queue for a 200ms window
and flush as one edge users/info call ({id: 0} — 'never seen, send
the full record'). Ids edge does not return, a failed edge call, and
workspaces marked degraded all fall back to the per-user users.info
path, which is unchanged."
```

---

### Task 4: wiring

**Files:**
- Modify: `cmd/slk/main.go` (WorkspaceContext.EdgeHealth field; construction; newUserResolver call)
- Modify: `cmd/slk/bootstrap_adapters.go` (newBootstrapDeps gains health)
- Test: `cmd/slk/bootstrap_adapters_test.go` (TestBootstrapDeps_PopulatesEveryDependency)

- [ ] **Step 1: WorkspaceContext field**

In `cmd/slk/main.go`, add to the WorkspaceContext struct next to `Edge`:

```go
	// EdgeHealth records whether edge resolution is working for this
	// workspace this session. bootstrap marks it degraded on a
	// wholesale failure; the user resolver reads it to skip batch
	// attempts that would resolve nothing.
	EdgeHealth *edge.Health
```

and to the wctx literal (next to the `Edge:` line, ~main.go:2046):

```go
		EdgeHealth:           edge.NewHealth(),
```

- [ ] **Step 2: newBootstrapDeps**

In `cmd/slk/bootstrap_adapters.go`, change the signature and add the field:

```go
func newBootstrapDeps(c *slackclient.Client, db *cache.DB, accessToken, openChannelID string, health *edge.Health) bootstrap.Deps {
	return bootstrap.Deps{
		...
		Revalidate:    edge.New(accessToken, c.TeamID(), c.HTTPClient()),
		Health:        health,
		Store:         storeAdapter{db},
		...
	}
}
```

Update the production call in `cmd/slk/main.go` to pass `wctx.EdgeHealth` (find it with `rg -n 'newBootstrapDeps(' cmd/slk/main.go`; it runs after the wctx literal exists).

- [ ] **Step 3: newUserResolver call**

At `cmd/slk/main.go:2101` (the `wctx.UserResolver = newUserResolver(` call), append the two arguments: `wctx.Edge, wctx.EdgeHealth.Degraded`. Note the method value: `wctx.EdgeHealth.Degraded` is nil-safe because Health.Degraded has a nil receiver guard, and EdgeHealth is always constructed. If `wctx.Edge` is nil (construction failure), the batcher is nil and the resolver takes the per-user path — which is the pre-existing behaviour for that case.

- [ ] **Step 4: Fix the dependency-population test**

`cmd/slk/bootstrap_adapters_test.go` — `TestBootstrapDeps_PopulatesEveryDependency` asserts every Deps field is populated. Update its `newBootstrapDeps(...)` call to pass `edge.NewHealth()` and, if the assertion enumerates fields, add Health. That test exists precisely to catch a forgotten wiring; make it enforce Health too.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go vet ./... && go test -count=1 ./... 2>&1 | grep -v '^ok\|no test files' | head -10`
Expected: no output after the build.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/
git commit -m "feat(slk): wire edge health from bootstrap through to the user resolver"
```

---

### Task 6: batch the unresolved-DM sweep (added after Task 5's measurement)

**Files:**
- Modify: `cmd/slk/main.go` (userResolver.ResolveNow; extract the DM sweep into a testable resolveDMNames)
- Test: `cmd/slk/user_resolver_test.go`

**Why this task exists (the measurement that falsified Task 3's
assumption):** Task 5's cold-boot A/B showed the batcher absorbing
almost nothing — 116 users.info with it vs 99 without. Reading the
code showed why: the dominant cold-boot users.info source is not
`UserResolver.Request` at all. The unresolved-DM sweep
(cmd/slk/main.go, the `if len(wctx.UnresolvedDMs) > 0` block in
connectWorkspace) loops every DM whose counterparty the cache does not
know and calls `resolveUser`, which issues one synchronous
`GetUserProfile` per DM. The sweep also cannot simply call `Request`:
it needs results mapped to channel ids — `DMNameResolvedMsg` renames
sidebar DM rows and re-buckets app DMs (internal/ui/reducer_workspace.go:92),
while `UserResolvedMsg` only patches in-history names
(reducer_workspace.go:107).

- [ ] **Step 1: Write the failing tests**

Add to `cmd/slk/user_resolver_test.go` (the fakeBatcher and
edgeUserRecord helpers from Task 3 are reused):

```go
func TestUserResolver_ResolveNowBatchesAndApplies(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U001", "alice", "Alice", "", "T1", 1783337599010, false),
		edgeUserRecord("U002", "bob", "", "Bob Real", "T1", 1783337599011, false),
	}}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, nil)

	got := r.ResolveNow([]string{"U001", "U002"})

	if len(got) != 2 {
		t.Fatalf("ResolveNow returned %d records; want 2", len(got))
	}
	calls := batcher.calls()
	if len(calls) != 1 {
		t.Fatalf("edge batches = %d; want 1", len(calls))
	}
	if want := map[string]int64{"U001": 0, "U002": 0}; !reflect.DeepEqual(calls[0], want) {
		t.Errorf("batch = %v; want %v", calls[0], want)
	}
	// Applied: cache rows exist and carry versions, so the sweep's
	// callers and conditional revalidation both read them.
	for _, id := range []string{"U001", "U002"} {
		if _, err := db.GetUser(id); err != nil {
			t.Errorf("%s was not cached by ResolveNow: %v", id, err)
		}
	}
	versions, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if versions["U001"] != 1783337599010 {
		t.Errorf("U001 version = %d; want 1783337599010", versions["U001"])
	}
}

func TestUserResolver_ResolveNowReturnsNilWhenDegraded(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, func() bool { return true })

	if got := r.ResolveNow([]string{"U001"}); got != nil {
		t.Errorf("a degraded workspace resolved %v through edge; want nil so the caller falls back per-user", got)
	}
	if n := len(batcher.calls()); n != 0 {
		t.Errorf("a degraded workspace made %d edge calls; want 0", n)
	}
}

func TestUserResolver_ResolveNowReturnsNilOnError(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{err: errors.New("ratelimited")}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, nil)

	if got := r.ResolveNow([]string{"U001"}); got != nil {
		t.Errorf("a failed edge call returned %v; want nil", got)
	}
	if _, err := db.GetUser("U001"); err == nil {
		t.Error("U001 was cached from a response the resolver rejected")
	}
}

func TestUserResolver_ResolveNowSkipsEmptyInput(t *testing.T) {
	db := newTestDB(t)
	batcher := &fakeBatcher{}
	r := newUserResolver("T1", nil, db, nil, nil, batcher, nil)

	if got := r.ResolveNow(nil); got != nil {
		t.Errorf("ResolveNow(nil) = %v; want nil", got)
	}
	if got := r.ResolveNow([]string{""}); got != nil {
		t.Errorf("ResolveNow([\"\"]) = %v; want nil — an updated_ids map containing \"\" is a request shape nothing observed produces", got)
	}
	if n := len(batcher.calls()); n != 0 {
		t.Errorf("empty input produced %d edge calls; want 0", n)
	}
}

func TestResolveDMNames(t *testing.T) {
	// The sweep maps resolutions to CHANNEL ids: DMNameResolvedMsg
	// renames the sidebar row and re-buckets app DMs, which
	// UserResolvedMsg (history patching) cannot do.
	db := newTestDB(t)
	batcher := &fakeBatcher{res: []edge.User{
		edgeUserRecord("U_ALICE", "alice", "Alice A", "", "T1", 1, false),
		edgeUserRecord("U_APP", "someapp", "Some App", "", "T1", 1, true),
	}}
	wctx := &WorkspaceContext{
		TeamID:        "T1",
		UserNames:     map[string]string{},
		BotUserIDs:    map[string]bool{},
		UserResolver:  newUserResolver("T1", nil, db, nil, nil, batcher, nil),
		UnresolvedDMs: []UnresolvedDM{
			{ChannelID: "D_ALICE", UserID: "U_ALICE"},
			{ChannelID: "D_APP", UserID: "U_APP"},
		},
	}
	var mu sync.Mutex
	var sent []tea.Msg
	resolveDMNames(wctx, db, nil, func(m tea.Msg) {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()
	var alice, app *ui.DMNameResolvedMsg
	for _, m := range sent {
		if dm, ok := m.(ui.DMNameResolvedMsg); ok {
			switch dm.ChannelID {
			case "D_ALICE":
				d := dm
				alice = &d
			case "D_APP":
				d := dm
				app = &d
			}
		}
	}
	if alice == nil || alice.DisplayName != "Alice A" {
		t.Errorf("D_ALICE got %+v; want a DMNameResolvedMsg naming Alice A", alice)
	}
	if app == nil || app.DisplayName != "Some App" || !app.IsBot {
		t.Errorf("D_APP got %+v; want a DMNameResolvedMsg naming Some App with IsBot true — that flag re-buckets the row into the Apps section", app)
	}
	if !wctx.BotUserIDs["U_APP"] {
		t.Error("U_APP was not recorded in BotUserIDs")
	}
	if n := len(batcher.calls()); n != 1 {
		t.Errorf("the sweep made %d edge calls; want 1 for any number of DMs", n)
	}
}
```

Check the `UnresolvedDM` struct's field names before writing the test
(`rg -n 'type UnresolvedDM' cmd/slk/main.go`) — adjust the literal if
they differ.

Run: `go test ./cmd/slk/ -run 'TestUserResolver_ResolveNow|TestResolveDMNames' 2>&1 | tail -3`
Expected: build failure — ResolveNow/resolveDMNames undefined.

- [ ] **Step 2: Implement ResolveNow**

In `cmd/slk/main.go`, after `flush`:

```go
// ResolveNow resolves ids immediately through one edge users/info
// batch and returns the records edge resolved. Unlike Request it
// blocks the caller, so it is for background goroutines that need the
// results — the unresolved-DM sweep maps them to channel ids and
// cannot use the fire-and-forget queue. Ids edge does not resolve are
// simply absent from the result; the caller falls back per-user.
// Nil means "resolve everything per-user": no edge client, a degraded
// workspace, a failed call, or nothing worth sending.
//
// Note applyEdgeUser's inflight.Delete is a no-op for these ids —
// they were never queued. A Request racing a ResolveNow for the same
// id can produce one duplicate users.info; the upserts are idempotent
// and the window is a cache miss wide, so no guard is taken.
func (r *userResolver) ResolveNow(ids []string) []edge.User {
	if r == nil || r.batcher == nil || len(ids) == 0 {
		return nil
	}
	if r.degraded != nil && r.degraded() {
		return nil
	}
	updated := make(map[string]int64, len(ids))
	for _, id := range ids {
		if id != "" {
			updated[id] = 0
		}
	}
	if len(updated) == 0 {
		return nil
	}
	users, err := r.batcher.UsersInfo(context.Background(), updated)
	if err != nil {
		debuglog.Cache("userResolver: ResolveNow edge users/info for %d users team=%s: %v (caller falls back per-user)", len(updated), r.teamID, err)
		return nil
	}
	for _, u := range users {
		r.applyEdgeUser(u)
	}
	return users
}
```

- [ ] **Step 3: Extract resolveDMNames and batch the sweep**

Replace the `// Resolve unknown DM user names in background` block in
connectWorkspace (the `if len(wctx.UnresolvedDMs) > 0 { go func() {...}() }`
statement) with:

```go
		// Resolve unknown DM user names in background
		if len(wctx.UnresolvedDMs) > 0 {
			go resolveDMNames(wctx, db, avatarCache, func(msg tea.Msg) {
				if p != nil {
					p.Send(msg)
				}
			})
		}
```

and add the function (near resolveUser):

```go
// resolveDMNames resolves the display names of unresolved DM
// counterparties, one edge users/info batch for the whole sweep, with
// the per-user resolveUser loop as the fallback for ids edge did not
// return. Batched because the sweep is the dominant cold-boot
// users.info source: one synchronous GetUserProfile per unresolved DM,
// measured at ~100 calls on a two-workspace cold boot and 282 in a
// full Grid session. The mapping to channel ids is why this cannot go
// through UserResolver.Request: DMNameResolvedMsg renames the sidebar
// row and re-buckets app DMs, while UserResolvedMsg only patches
// in-history names.
func resolveDMNames(wctx *WorkspaceContext, db *cache.DB, avatarCache *avatar.Cache, send func(tea.Msg)) {
	dmIDs := make([]string, 0, len(wctx.UnresolvedDMs))
	for _, dm := range wctx.UnresolvedDMs {
		dmIDs = append(dmIDs, dm.UserID)
	}
	byEdge := make(map[string]edge.User)
	for _, u := range wctx.UserResolver.ResolveNow(dmIDs) {
		byEdge[u.ID] = u
	}
	for _, dm := range wctx.UnresolvedDMs {
		if u, ok := byEdge[dm.UserID]; ok {
			name := u.Profile.DisplayName
			if name == "" {
				name = u.Profile.RealName
			}
			if name == "" {
				name = u.Name
			}
			// edge users/info carries no is_app_user: a Slack app's DM
			// resolved here may bucket as "dm" rather than "app" until
			// something else classifies it. No capture shows that
			// field on this endpoint, so none is invented; the
			// per-user fallback below classifies the ids edge missed.
			if u.IsBot {
				wctx.BotUserIDs[dm.UserID] = true
			}
			if name != "" && send != nil {
				send(ui.DMNameResolvedMsg{
					ChannelID:   dm.ChannelID,
					DisplayName: name,
					IsBot:       u.IsBot,
				})
			}
			continue
		}
		resolved, isBot := resolveUser(wctx.Client, dm.UserID, wctx.UserNames, db, avatarCache)
		if isBot {
			wctx.BotUserIDs[dm.UserID] = true
		}
		if resolved != dm.UserID && send != nil {
			send(ui.DMNameResolvedMsg{
				ChannelID:   dm.ChannelID,
				DisplayName: resolved,
				IsBot:       isBot,
			})
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./cmd/slk/ -run 'TestUserResolver|TestResolveDMNames' 2>&1 | tail -3`
Expected: PASS. Then `go build ./... && go vet ./... && go test -count=1 ./... 2>&1 | grep -v '^ok\|no test files' | head -5` — no output.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/
git commit -m "feat(slk): batch the unresolved-DM sweep through edge users/info

The sweep — one synchronous GetUserProfile per unresolved DM — is the
dominant cold-boot users.info source, measured at ~100 calls on a
two-workspace boot and 282 in a full Grid session. Task 5's A/B
showed the Request queue absorbing almost none of it. The sweep now
resolves its whole counterparty list in one edge users/info call via
ResolveNow and falls back per-user only for ids edge did not return."
```

---

### Task 7: live verification (cold-cache A/B)

**Files:** none modified; produces the merge-gate evidence.

- [ ] **Step 1: Cold-cache protocol (from the handoff's trap list)**

A warm cache hides the resolver fan-out. Measure cold: point XDG_DATA_HOME at a temp dir containing ONLY a copy of the tokens:

```bash
cd ~/local_code/slk
go build -o /tmp/slk-batched ./cmd/slk
export COLDXDG=$(mktemp -d)
mkdir -p $COLDXDG/slk
cp -r ~/.local/share/slk/tokens $COLDXDG/slk/
```

- [ ] **Step 2: Cold boot with the new binary (and a main-built baseline)**

Build the baseline from `main` first (`git archive main | tar -x -C <dir>`, build there) so the comparison is exact. Run both cold, in either order:

```bash
cd /tmp/opencode/slk-repro
XDG_DATA_HOME=$COLDXDG SLK_DEBUG=1 script -qec 'timeout -s INT 25 /tmp/slk-mainline' /dev/null >/dev/null 2>&1
mv slk-debug.log cold-main.log
# fresh temp XDG with the same tokens for the new binary
XDG_DATA_HOME=$COLDXDG2 SLK_DEBUG=1 script -qec 'timeout -s INT 25 /tmp/slk-batched' /dev/null >/dev/null 2>&1
mv slk-debug.log cold-batched.log
```

Expected after Task 6: the batched binary's `users.info` count is near
zero (only ids edge genuinely cannot resolve), `edge:users/info` is a
small number (boot revalidation plus one batch per workspace needing
resolution), and the total drops correspondingly against the main
baseline.

- [ ] **Step 3: Warm boot regression check**

```bash
SLK_DEBUG=1 script -qec 'timeout -s INT 25 /tmp/slk-batched' /dev/null >/dev/null 2>&1
grep -A25 'shutdown API request tally' slk-debug.log
```

Expected: the tally matches the pre-change warm-boot baseline (37-38 total, same endpoints). No new endpoints, no extra edge calls — a fully resolved cache produces no misses, hence no batches.

- [ ] **Step 4: Report, clean up**

`rm -rf $COLDXDG`. Report both tallies to the user.

---

## Self-review notes

- Spec coverage: health signal (Tasks 1+2), batching (Task 3), foreign-team noise via abort (Task 2), no persistence (guard: Task 3's new method is the only cache change, no columns), wiring (Task 4), verification (Task 5).
- The UPDATE-only landmine is handled by the new `UpsertUserFromEdge` in Task 3; bootstrap's `UpdateUserFromEdge` is untouched.
- Type consistency: `userBatcher` matches `(*edge.Client).UsersInfo` exactly; `Deps.Health` is `*edge.Health` in bootstrap.go, revalidate.go, the adapter, and the test.
- Existing-behaviour preservation: nil batcher/degraded → byte-identical resolver path; equal-size fixture groups keep T_HOME first under the new ordering; Health nil in old bootstrap tests disables the check entirely.
