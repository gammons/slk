# Team-Scoped Edge Revalidation (Grid) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `channels/info` revalidation work on Enterprise Grid by scoping each request to the team that owns the conversations, per `docs/superpowers/specs/2026-08-05-grid-edge-team-scoping-design.md`.

**Architecture:** `edge.Client.call` takes a per-request team id; `ChannelsInfo` gains a mandatory team parameter; `bootstrap.revalidateChannels` partitions `updated_ids` by userBoot's `context_team_id` (empty → the workspace's own team) and processes one call per team. Nothing else changes scope: `users/info`, search, and the members endpoints keep the workspace team.

**Tech Stack:** Go, stdlib only (`maps`, `slices` already in use).

**Key fixture fact that shapes everything below:**
`cannedBootResult` (internal/bootstrap/bootstrap_test.go:49) already mixes context teams: C_GENERAL + D_ALICE are `T_HOME`; C_PRIVATE + D_BOB are `T_OTHER`. The workspace id is `T_HOME`. So the moment partitioning lands, the existing suite makes TWO channels/info calls and exercises the split for free. The fake must become team-aware (record per-call, filter its canned response to the ids asked), and three existing assertions change: the budget test (1 → 2 calls), the workspace-id test (1 → 2 membership calls), and the membership queried-set test (split across two calls).

---

### Task 1: edge.Client — per-request team id

**Files:**
- Modify: `internal/slack/edge/client.go:53-56` (call signature + URL)
- Modify: `internal/slack/edge/cache.go` (fetchInfo + ChannelsInfo signatures, UsersInfo passes `c.teamID`, wrong Grid comment)
- Modify: `internal/slack/edge/members.go:125,178,235` and `internal/slack/edge/search.go:91,164` (pass `c.teamID`)
- Test: `internal/slack/edge/cache_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/slack/edge/cache_test.go`, next to the other ChannelsInfo tests:

```go
func TestChannelsInfo_ScopesThePathToTheGivenTeam(t *testing.T) {
	// On Enterprise Grid a user's conversations are owned by many
	// teams within the org, and the edge cache keys them under the
	// owning team. The team in the request path is therefore a
	// per-call decision, not a client property: scoping every request
	// to the auth.test team is what resolved zero of raff's 217
	// conversations (gammons/slk#5).
	rec := newRecorder(t, alwaysEmpty)
	c := rec.client() // constructed with team T04T4TH8W

	if _, err := c.ChannelsInfo(context.Background(), "T_OTHER_TEAM", map[string]int64{"C1": 0}); err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d; want 1", len(reqs))
	}
	if want := "/cache/T_OTHER_TEAM/channels/info"; reqs[0].path != want {
		t.Errorf("path = %q; want %q — the call's team, not the client's construction team", reqs[0].path, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/edge/ -run TestChannelsInfo_ScopesThePathToTheGivenTeam 2>&1 | tail -3`
Expected: build failure — `too many arguments in call to c.ChannelsInfo`.

- [ ] **Step 3: Change call, fetchInfo and ChannelsInfo to carry the team**

`internal/slack/edge/client.go` — `call` gains `teamID` as its second parameter, and the URL uses it:

```go
// call POSTs payload (with the token merged in) to
// /cache/<teamID>/<endpoint> and decodes the response into out.
//
// teamID is a per-request argument, not read from the client: on
// Enterprise Grid the conversations one call asks about are owned by
// different teams within the org, and the edge cache keys records
// under the owning team. Callers whose scope is the workspace pass
// the client's team; ChannelsInfo passes the owning team.
func (c *Client) call(ctx context.Context, teamID, endpoint string, payload map[string]any, out any) error {
```

and in the body:

```go
	url := fmt.Sprintf("%s/cache/%s/%s", c.baseURL, teamID, endpoint)
```

`internal/slack/edge/cache.go` — `fetchInfo` gains the team right after the client:

```go
func fetchInfo[Resp any](
	ctx context.Context,
	c *Client,
	teamID string,
	endpoint string,
	flags map[string]any,
	updatedIDs map[string]int64,
	batchSize int,
	merge func(Resp, []string),
) error {
```

and its inner call becomes:

```go
		if err := c.call(ctx, teamID, endpoint, payload, &resp); err != nil {
```

`ChannelsInfo` gains the team as its second parameter (doc comment extended):

```go
// ChannelsInfo revalidates channels against the edge cache, scoped to
// teamID: the request path is /cache/<teamID>/channels/info.
//
// teamID is mandatory and per-call because of Enterprise Grid. A Grid
// user's conversations are owned by different teams within the org,
// and the edge cache keys records under the owning team; a request
// scoped to the auth.test team resolves only the conversations that
// team owns and fails the rest (gammons/slk#5: 217 of 217 failed).
// The caller partitions by userBoot's context_team_id; non-Grid
// callers pass the workspace's own team id, which is every
// conversation's context team there.
func (c *Client) ChannelsInfo(ctx context.Context, teamID string, updatedIDs map[string]int64) (ChannelsInfoResult, error) {
	var out ChannelsInfoResult
	err := fetchInfo(ctx, c, teamID, "channels/info", map[string]any{
```

`UsersInfo` keeps its signature and passes the client team — Grid
scoping for users is deliberately unchanged until a log implicates it:

```go
	err := fetchInfo(ctx, c, c.teamID, "users/info", map[string]any{
```

The five other call sites pass `c.teamID`:
- `internal/slack/edge/members.go:125` → `c.call(ctx, c.teamID, "users/list", payload, &resp)`
- `internal/slack/edge/members.go:178` → `c.call(ctx, c.teamID, "channels/membership", map[string]any{`
- `internal/slack/edge/members.go:235` → `c.call(ctx, c.teamID, "users/counts", map[string]any{`
- `internal/slack/edge/search.go:91` → `c.call(ctx, c.teamID, "channels/search", payload, &resp)`
- `internal/slack/edge/search.go:164` → `c.call(ctx, c.teamID, "users/search", payload, &resp)`

Fix the wrong provenance comment in `internal/slack/edge/cache.go`
(the "These are not documented limits" block): the captures are from a
non-Grid workspace, and claiming otherwise is how "Grid is covered"
nearly got believed. Change

```
// under the largest batch the official web client has been observed
// sending, measured across 8 HAR captures of a live Grid workspace:
```

to

```
// under the largest batch the official web client has been observed
// sending, measured across 8 HAR captures of a live NON-Grid
// workspace (Rands, T04T4TH8W). No Grid capture of edgeapi exists;
// behaviour there is inferred from a user's log, not observed:
```

- [ ] **Step 4: Update the existing edge tests mechanically, with an assertion**

All ~21 `ChannelsInfo` call sites in `internal/slack/edge/cache_test.go`
and the 2 direct `fetchInfo` calls gain the team. Run:

```bash
cd ~/local_code/slk
before=$(grep -c '\.ChannelsInfo(context\.Background(), ' internal/slack/edge/cache_test.go)
sed -i 's/\.ChannelsInfo(context\.Background(), /.ChannelsInfo(context.Background(), "T04T4TH8W", /g' internal/slack/edge/cache_test.go
after=$(grep -c '\.ChannelsInfo(context\.Background(), "T04T4TH8W", ' internal/slack/edge/cache_test.go)
[ "$before" -eq "$after" ] && [ "$before" -gt 20 ] || { echo "sed miscounted: before=$before after=$after"; exit 1; }
sed -i 's/fetchInfo(context\.Background(), c, "channels\/info",/fetchInfo(context.Background(), c, "T04T4TH8W", "channels\/info",/g' internal/slack/edge/cache_test.go
grep -c 'fetchInfo(context.Background(), c, "T04T4TH8W"' internal/slack/edge/cache_test.go  # must be 2
```

(The count guard exists because a scripted edit against a pattern that
silently matches nothing has already cost this project a debugging
session. Assert the pattern matched.)

- [ ] **Step 5: Run the edge package tests**

Run: `go test ./internal/slack/edge/ 2>&1 | tail -3`
Expected: PASS, including the new test.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/edge/
git commit -m "feat(edge): per-request team scoping, so channels/info can target the owning team"
```

---

### Task 2: bootstrap — partition revalidation by context team

**Files:**
- Modify: `internal/bootstrap/bootstrap.go:79-81` (Revalidator interface)
- Modify: `internal/bootstrap/revalidate.go` (revalidateChannels rewrite; new imports)
- Test: `internal/bootstrap/revalidate_test.go` (fake becomes team-aware; new tests)
- Test: `internal/bootstrap/bootstrap_test.go` (budget assertion)
- Modify: `cmd/slk/bootstrap_adapters_test.go:533` (call-site signature)

- [ ] **Step 1: Update the interface, and keep everything compiling at today's behaviour**

`internal/bootstrap/bootstrap.go:79-81`:

```go
type Revalidator interface {
	// ChannelsInfo revalidates conversations against the edge cache,
	// scoped to teamID — on Enterprise Grid the owning team, which is
	// not necessarily the workspace's own. See edge.ChannelsInfo.
	ChannelsInfo(ctx context.Context, teamID string, updatedIDs map[string]int64) (edge.ChannelsInfoResult, error)
	UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]edge.User, error)
}
```

Immediately adapt the two call sites so the tree still builds, with NO
behaviour change — one call, scoped to the workspace team, exactly as
today:

- `internal/bootstrap/revalidate.go`: the existing
  `res, err := deps.Revalidate.ChannelsInfo(ctx, updated)` becomes
  `res, err := deps.Revalidate.ChannelsInfo(ctx, deps.WorkspaceID, updated)`.
- `cmd/slk/bootstrap_adapters_test.go:533` becomes
  `if _, err := deps.Revalidate.ChannelsInfo(context.Background(), "T1", map[string]int64{"C1": 0}); err != nil {`.

Run: `go build ./... 2>&1 | head -5`
Expected: one compile error remains — the fake in revalidate_test.go
no longer satisfies the interface. Fixed in Step 2.

- [ ] **Step 2: Make the fake team-aware (suite still green — behaviour is unchanged)**

In `internal/bootstrap/revalidate_test.go`:

Add the call record type and fields to `revalidateFake` (next to the
existing `channelsInfoSent` field):

```go
// channelsInfoCall is one team-scoped channels/info request.
type channelsInfoCall struct {
	team string
	sent map[string]int64
}
```

Add to the `revalidateFake` struct:

```go
	channelsInfoCalls  []channelsInfoCall
	channelsInfoErrFor map[string]error // per-team error injection
```

Replace the fake `ChannelsInfo` method. The filtered response is the
honest part: a real server answers about the ids it was asked, so the
canned result is intersected with the request. `channelsInfoSent` keeps
accumulating a merged map so the existing single-map assertions survive
unchanged:

```go
func (f *fakeDeps) ChannelsInfo(_ context.Context, teamID string, updatedIDs map[string]int64) (edge.ChannelsInfoResult, error) {
	f.record(callChannelsInfo)
	f.mu.Lock()
	sent := make(map[string]int64, len(updatedIDs))
	for k, v := range updatedIDs {
		sent[k] = v
	}
	f.channelsInfoCalls = append(f.channelsInfoCalls, channelsInfoCall{team: teamID, sent: sent})
	if f.channelsInfoSent == nil {
		f.channelsInfoSent = map[string]int64{}
	}
	for k, v := range sent {
		f.channelsInfoSent[k] = v
	}
	err := f.channelsInfoErr
	if e, ok := f.channelsInfoErrFor[teamID]; ok {
		err = e
	}
	f.mu.Unlock()
	if err != nil {
		return poisonedChannelsInfo(), err
	}
	return filterChannelsInfo(f.channelsInfoRes, sent), nil
}

// filterChannelsInfo intersects a canned result with the ids one
// request actually asked about, the way the server scopes its answer
// to the request. A canned membership report that names no asked id
// decays to "this batch said nothing" (empty MembershipQueried), which
// is what the real absent-member_channels case already models.
func filterChannelsInfo(res edge.ChannelsInfoResult, sent map[string]int64) edge.ChannelsInfoResult {
	var out edge.ChannelsInfoResult
	for _, ch := range res.Channels {
		if _, ok := sent[ch.ID]; ok {
			out.Channels = append(out.Channels, ch)
		}
	}
	keep := func(ids []string) []string {
		var kept []string
		for _, id := range ids {
			if _, ok := sent[id]; ok {
				kept = append(kept, id)
			}
		}
		return kept
	}
	out.MemberChannels = keep(res.MemberChannels)
	out.MembershipQueried = keep(res.MembershipQueried)
	out.FailedIDs = keep(res.FailedIDs)
	return out
}
```

(`maps`/`slices` are NOT needed in the test file for this; plain loops
match the file's existing style.)

Run: `go test ./internal/bootstrap/ 2>&1 | tail -3`
Expected: PASS, unchanged. One channels/info call still happens
(production passes `deps.WorkspaceID`), the filter is a no-op against
the full id set, and every existing assertion still holds. If anything
fails here, the fake change is wrong — fix it before going on.

- [ ] **Step 3: Write the new failing tests, and update the three assertions the partition will change**

Add to `internal/bootstrap/revalidate_test.go`:

```go
// --- team partitioning ------------------------------------------------

// TestRevalidate_PartitionsChannelsInfoByContextTeam is the Grid fix.
// The fixture spans two context teams (C_GENERAL/D_ALICE on T_HOME,
// C_PRIVATE/D_BOB on T_OTHER), so a correct run issues one
// channels/info per team, each carrying exactly that team's ids.
func TestRevalidate_PartitionsChannelsInfoByContextTeam(t *testing.T) {
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.channelsInfoCalls) != 2 {
		t.Fatalf("channels/info calls = %d; want 2, one per context team (calls: %+v)", len(f.channelsInfoCalls), f.channelsInfoCalls)
	}
	byTeam := map[string]map[string]int64{}
	for _, c := range f.channelsInfoCalls {
		byTeam[c.team] = c.sent
	}
	if want := map[string]int64{"C_GENERAL": 1783337533019, "D_ALICE": 0}; !reflect.DeepEqual(byTeam["T_HOME"], want) {
		t.Errorf("T_HOME was sent %v; want %v", byTeam["T_HOME"], want)
	}
	if want := map[string]int64{"C_PRIVATE": 0, "D_BOB": 1783337533022}; !reflect.DeepEqual(byTeam["T_OTHER"], want) {
		t.Errorf("T_OTHER was sent %v; want %v", byTeam["T_OTHER"], want)
	}
}

// TestRevalidate_EmptyContextTeamUsesTheWorkspaceTeam: a conversation
// userBoot gave no context_team_id for must behave exactly as
// pre-Grid code behaved — scoped to the workspace's own team.
//
// NOTE: this is a characterization test. It passes both before and
// after the partition lands, because the pre-partition code scopes
// everything to the workspace team anyway. Its job is to pin the
// fallback rule so a future refactor can't drop it.
func TestRevalidate_EmptyContextTeamUsesTheWorkspaceTeam(t *testing.T) {
	f := openedFake()
	f.bootRes.Channels = append(f.bootRes.Channels, boot.Channel{ID: "C_NO_TEAM", Name: "no-team"})

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range f.channelsInfoCalls {
		if _, ok := c.sent["C_NO_TEAM"]; ok {
			if c.team != "T_HOME" {
				t.Errorf("C_NO_TEAM was scoped to %q; want the workspace team T_HOME", c.team)
			}
			return
		}
	}
	t.Errorf("C_NO_TEAM was never sent (calls: %+v)", f.channelsInfoCalls)
}

// TestRevalidate_TeamFailureDoesNotSkipOtherTeams mirrors the existing
// channels/users independence guarantee one level down: one team's
// failure must not strand the other team's revalidation.
func TestRevalidate_TeamFailureDoesNotSkipOtherTeams(t *testing.T) {
	f := openedFake()
	f.channelsInfoErrFor = map[string]error{"T_OTHER": errors.New("ratelimited")}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.channelsInfoCalls) != 2 {
		t.Fatalf("channels/info calls = %d; want 2 — a failed team must not suppress the other team's call", len(f.channelsInfoCalls))
	}
	// T_HOME's results still land.
	if !reflect.DeepEqual(f.channelUpdates, wantChannelUpdates()) {
		t.Errorf("channel updates = %#v; want %#v from the healthy team", f.channelUpdates, wantChannelUpdates())
	}
	if !f.loggedMatching("team T_OTHER") {
		t.Errorf("the failed team was not named in the log; on Grid this line is the only diagnostic (logged: %v)", f.logged())
	}
}
```

Then update the three existing assertions whose expectations the
partition changes (they encode the old single-call behaviour; updating
them before the production change keeps this step honestly RED):

In `TestRevalidate_StaysInsideTheBootCallBudget` (revalidate_test.go,
the `countPrefix(callChannelsInfo)` assertion):

```go
	if n := f.countPrefix(callChannelsInfo); n != 2 {
		t.Errorf("channels/info called %d times; want exactly 2 — the fixture spans two context teams (T_HOME, T_OTHER), and each gets one batched conditional request (sequence: %v)", n, f.calls)
	}
```

In `TestRevalidate_UsesTheWorkspaceIDForEveryCacheCall`, replace the
`len(f.membershipCalls) != 1` block with:

```go
	if len(f.membershipCalls) != 2 {
		t.Fatalf("ApplyMembership called %d times; want 2, one per context team (%#v)", len(f.membershipCalls), f.membershipCalls)
	}
	for _, c := range f.membershipCalls {
		if c.workspaceID != "T_HOME" {
			t.Errorf("ApplyMembership workspace = %q; want T_HOME", c.workspaceID)
		}
	}
```

In `TestRevalidate_AppliesMembershipToTheQueriedIDsNotTheReturnedOnes`,
keep the original comment block (the hazard it names is unchanged) and
replace the single-call assertions with the per-team split. The
failed-id half moves to the T_OTHER call because D_BOB is T_OTHER.
Team order is sorted (T_HOME first), and membership is applied inside
the same per-team iteration, so `channelsInfoCalls[i]` and
`membershipCalls[i]` align:

```go
	if len(f.membershipCalls) != 2 {
		t.Fatalf("ApplyMembership called %d times; want 2, one per context team (%#v)", len(f.membershipCalls), f.membershipCalls)
	}
	byTeam := map[string]membershipCall{}
	for i, c := range f.channelsInfoCalls {
		byTeam[c.team] = f.membershipCalls[i]
	}
	home := byTeam["T_HOME"]
	if want := []string{"C_GENERAL", "D_ALICE"}; !reflect.DeepEqual(home.queriedIDs, want) {
		t.Errorf("T_HOME queried set = %v; want %v (MembershipQueried, never MemberChannels %v)", home.queriedIDs, want, cannedChannelsInfo().MemberChannels)
	}
	if want := cache.MembershipReported([]string{"C_GENERAL"}, nil); !reflect.DeepEqual(home.snap, want) {
		t.Errorf("T_HOME snapshot = %#v; want %#v", home.snap, want)
	}
	other := byTeam["T_OTHER"]
	if want := []string{"C_PRIVATE"}; !reflect.DeepEqual(other.queriedIDs, want) {
		t.Errorf("T_OTHER queried set = %v; want %v", other.queriedIDs, want)
	}
	// The failed id rides in its own team's snapshot: omitting it
	// would clear is_member for an id the server explicitly could not
	// answer about.
	if want := cache.MembershipReported(nil, failedChannelIDs); !reflect.DeepEqual(other.snap, want) {
		t.Errorf("T_OTHER snapshot = %#v; want %#v", other.snap, want)
	}
```

Run: `go test ./internal/bootstrap/ 2>&1 | tail -8`
Expected: FAIL — the three updated tests plus the three new ones fail,
because production still makes one workspace-scoped call. The rest of
the suite passes. If an UNRELATED test fails, the fake's filter is
wrong — fix it, not the test.

- [ ] **Step 4: Rewrite revalidateChannels with partitioning (GREEN)**

Replace `revalidateChannels` in `internal/bootstrap/revalidate.go`.
The per-team body is the existing call/write/membership/failed-ids
logic, moved under the loop and given team-named log lines. New imports
for the file: `"maps"` and `"slices"`.

```go
// revalidateChannels conditionally refreshes the conversations the
// sidebar will render: userBoot's channels plus its ims, and nothing
// else in the cache.
//
// The id set is partitioned by each conversation's context_team_id,
// and each team gets its own channels/info call. On Enterprise Grid a
// user's conversations are owned by many teams within the org and the
// edge cache keys records under the owning team: a single call scoped
// to the auth.test team resolved zero of one Grid user's 217
// conversations (gammons/slk#5). On a non-Grid workspace every
// context team IS the workspace team, so the partition is a single
// group and the request shape is identical to before. An empty
// context team groups under the workspace team, preserving the old
// behaviour for anything the field is missing on.
//
// The ims are included on purpose even though channels/info cannot
// resolve them. Measured across the captures: of 193 ids the official
// client sent to this endpoint, 22 were IM ids, and **all 22 came back
// in failed_ids** — none appeared in results, none in member_channels.
// So the official client sends them and they always fail, and matching
// that is the point of this package.
//
// Two consequences worth knowing before anyone "optimises" ims out of
// this set, which would be a divergence from the client:
//
//   - No IM is ever written by UpdateChannelFromEdge, because IMs never
//     come back as results. A DM's cached name and type cannot be
//     corrupted from here.
//   - Every IM lands in FailedIDs, and ApplyMembership preserves failed
//     ids rather than clearing them. That is the only reason DMs keep
//     is_member across a boot. Removing the failed-id exclusion would
//     mark every DM a non-membership on the next revalidation.
func revalidateChannels(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) {
	teamOf := make(map[string]string, len(out.Channels)+len(out.IMs))
	ids := make([]string, 0, len(out.Channels)+len(out.IMs))
	for _, ch := range out.Channels {
		ids = append(ids, ch.ID)
		teamOf[ch.ID] = ch.ContextTeamID
	}
	for _, im := range out.IMs {
		ids = append(ids, im.ID)
		teamOf[im.ID] = im.ContextTeamID
	}

	cached, err := deps.Store.ChannelVersions(deps.WorkspaceID)
	if err != nil {
		// Degrade to sending 0 for everything, which asks for full
		// records: more bytes back, but correct. The map returned
		// beside the error is discarded — a version slk cannot vouch
		// for makes the server withhold a record slk does not have.
		logf("bootstrap: reading cached channel versions: %v (revalidating everything from scratch)", err)
		cached = nil
	}

	updated := conditionalVersions(ids, cached)
	if len(updated) == 0 {
		// No request at all. An updated_ids-less revalidation is a
		// round trip that can only return nothing, and a stream of
		// them is a shape the official client never produces.
		return
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
	// Sorted for determinism: request order must not depend on map
	// iteration, both for the debug log's readability and so a test
	// can rely on call order.
	teams := slices.Collect(maps.Keys(groups))
	slices.Sort(teams)
	for _, team := range teams {
		revalidateChannelTeam(ctx, deps, team, groups[team], logf)
	}
}

// revalidateChannelTeam runs the channels/info call and its
// write-through for one owning team. Team failures are independent:
// one team's error is logged and its ids are left stale, and the
// remaining teams still run — the same independence the channels and
// users passes have from each other, one level down.
func revalidateChannelTeam(ctx context.Context, deps Deps, team string, updated map[string]int64, logf func(string, ...any)) {
	res, err := deps.Revalidate.ChannelsInfo(ctx, team, updated)
	if err != nil {
		// Everything below is discarded along with it. Slack answers
		// ok:false with a populated body, so "err != nil and the
		// value looks fine" is the normal shape of a failure here.
		logf("bootstrap: channels/info for team %s: %d conversations: %v (leaving them stale)", team, len(updated), err)
		return
	}

	for _, ch := range res.Channels {
		if err := deps.Store.UpdateChannelFromEdge(cache.EdgeChannelUpdate{
			ID:      ch.ID,
			Name:    ch.Name,
			Type:    channelType(ch),
			Topic:   ch.Topic.Value,
			Version: ch.Version,
		}); err != nil {
			// One bad row must not cost the rest of the batch: the
			// call is already spent, and abandoning the remaining
			// updates would leave versions unadvanced for channels the
			// server did answer about.
			logf("bootstrap: caching revalidated channel %s: %v", ch.ID, err)
		}
	}

	// Membership, and the queried set is res.MembershipQueried — the
	// ids sent in batches whose response actually carried
	// member_channels — never res.MemberChannels.
	//
	// The difference is silent data loss. member_channels is a
	// snapshot over the ids ASKED about: one absent from it is a
	// genuine non-member, so passing the returned members as the
	// queried set would tell ApplyMembership that every id it was not
	// handed is a non-member, and clear is_member for the whole rest
	// of the batch.
	//
	// Empty means no batch reported, which is the COMMON case —
	// member_channels is absent from 13 of 18 observed responses, all
	// of which requested it — and it means "no information", so
	// nothing is written and no call is made. Note res.FailedIDs
	// accumulates across all batches while MembershipQueried covers
	// only the reporting ones; that is harmless, since ApplyMembership
	// touches nothing outside the queried set.
	//
	// Applying per team is safe because the queried set is exactly
	// this team's ids: membership answers never cross the partition.
	if len(res.MembershipQueried) > 0 {
		if err := deps.Store.ApplyMembership(deps.WorkspaceID, res.MembershipQueried,
			cache.MembershipReported(res.MemberChannels, res.FailedIDs)); err != nil {
			logf("bootstrap: applying membership for %d conversations: %v", len(res.MembershipQueried), err)
		}
	}

	// Failed ids are left exactly as they were, versions included.
	//
	// This is a correctness hazard rather than a lost nicety. Absence
	// from res.Channels otherwise means "unchanged, still fresh", so
	// stamping a failed id as current would keep its stale record
	// forever — its version never advances, so it never comes back.
	// Leaving the version where it is means the next boot asks again.
	//
	// The team is named: on Grid this line is the whole diagnosis.
	if len(res.FailedIDs) > 0 {
		logf("bootstrap: channels/info for team %s could not resolve %d ids (%v); leaving them stale to be retried", team, len(res.FailedIDs), res.FailedIDs)
	}
}
```

Note: the old single-call log line `channels/info for %d conversations`
is replaced by the team-named one. The existing failure-mode test
asserts `loggedMatching("channels/info for")`, which the new line still
satisfies.

- [ ] **Step 5: Run the full affected suites**

Run: `go build ./... && go test ./internal/bootstrap/ ./internal/slack/edge/ ./cmd/slk/ 2>&1 | tail -6`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/bootstrap/ cmd/slk/bootstrap_adapters_test.go
git commit -m "feat(bootstrap): partition channels/info revalidation by context_team_id

On Enterprise Grid a user's conversations are owned by many teams and
the edge cache keys records under the owning team, so a single call
scoped to the auth.test team resolved zero of one Grid user's 217
conversations (gammons/slk#5). Each context team now gets its own
batched call; an empty context team groups under the workspace team,
and on non-Grid workspaces the partition is a single group whose
request shape is identical to before. Failures are per-team
independent and the failure logs name the team, since that line is
the whole diagnosis on Grid."
```

---

### Task 3: Live verification (non-Grid no-op proof)

**Files:** none modified; produces the evidence the spec's merge gate needs.

- [ ] **Step 1: Build and run a 25-second session**

```bash
cd ~/local_code/slk
go build -o /tmp/slk-gridfix ./cmd/slk
cd /tmp/opencode/slk-repro   # or any scratch dir; the log lands in $PWD
SLK_DEBUG=1 script -qec 'timeout -s INT 25 /tmp/slk-gridfix' /dev/null >/dev/null 2>&1
grep -A25 'shutdown API request tally' slk-debug.log
```

- [ ] **Step 2: Assert the tally is unchanged**

Expected: `edge:channels/info` count is **3** (unchanged from the
pre-change runs), total still **37 across 18 endpoints**, and no new
endpoints appear. A different count means some conversation on the
local workspaces carries a foreign or empty context team — investigate
with `grep 'channels/info for team' slk-debug.log` before proceeding;
the per-team log line should name only the workspaces' own teams.

- [ ] **Step 3: Commit nothing; report**

Report the tally diff (expected: none) to the user. The change is then
ready for the raff-facing PR together with `fix/sidebar-from-userboot`.

---

## Self-review notes

- Spec coverage: items 1–5 of the design map to Tasks 1–2; the local
  verifiability gate is Task 3; the cache.go comment fix is Task 1
  Step 3; out-of-scope items (host derivation, users scoping, cache
  column) are untouched.
- Type consistency: `ChannelsInfo(ctx, teamID string, updatedIDs)` is
  the same shape in the interface (Task 2 Step 1), the fake (Step 2),
  the production call (Step 4), and the cmd/slk test (Step 6).
- The fake's merged `channelsInfoSent` map keeps every existing
  single-map assertion valid; only the three assertions named in Task 2
  Step 5 change.
