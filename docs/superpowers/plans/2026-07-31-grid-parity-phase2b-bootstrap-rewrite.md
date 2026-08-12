# Grid Parity Phase 2b: Bootstrap Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace slk's boot-time and reconnect-time enumeration with the conditional-revalidation path Phase 2a built, so slk's call pattern stops looking like a scraper.

**Architecture:** Extract the boot sequence into a testable `internal/bootstrap` package that orchestrates the Phase 2a components (`boot`, `edge`, `cache`). `connectWorkspace` shrinks to client construction plus UI wiring. Deletions land only after their replacements are proven, so no intermediate commit leaves slk without a working boot.

**Tech Stack:** Go 1.26, SQLite (`modernc.org/sqlite`), standard `net/http`, standard `testing`, Bubble Tea (UI).

---

## Read First

You have **no context** from the sessions that produced this plan. Read these, in order:

1. `docs/superpowers/specs/2026-07-31-grid-parity-phase2b-design.md` — the design this implements. Its *Cache column mapping* section is the part most likely to cause silent damage; read it twice.
2. `docs/superpowers/plans/2026-07-30-grid-parity-phase2a-outcomes.md` — what Phase 2a shipped, eleven places the captures overruled its plan, and the "What Phase 2b inherits" section, which is this plan's direct input.
3. `internal/slack/boot/boot.go` and `view.go`, `internal/slack/edge/*.go`, `internal/cache/versions.go` — **the real interfaces**. Do not guess at their signatures; they are listed under *Existing API* below but read the doc comments, which carry the measured evidence.
4. `internal/slack/testdata/phase2-api-contracts.json` — the committed evidence base. Every numeric claim in this plan is checkable there under `measured`.

### The captures

Eight HAR captures of the official Slack web client live in the worktree root as `*.har`. They are **gitignored and must stay that way** — they contain live `xoxc`/`xoxd` credentials and real message content. You may read them to settle a question. Never copy their contents into a file, never quote message text, never `git add` one.

**If a capture contradicts this plan, the capture wins.** That happened eleven times in Phase 2a. Say so and correct the plan.

### How this goes wrong

Phase 1 and 2a lessons, in priority order:

- **Mutation-test your own tests.** In Phase 1, ~⅓ of tests passed against a broken implementation. In Phase 2a a reviewer found 9 surviving mutants because every fixture boolean was `false`. After each task, break what you just wrote and confirm a test fails. Paste the real output.
- **Run `go test` un-piped.** `go test ... | tail` reports *tail's* exit status. Three implementers on this project recorded false mutation results that way. Capture `$?` on the next line.
- **A test can encode the bug.** Phase 2a found three pre-existing tests asserting bugs in passing. If a test blocks a fix, question the test.
- **Removing a struct tag is often not a valid mutation in Go** — `encoding/json` falls back to case-insensitive field-name matching. Use `json:"-"` or a mis-tag when you mean "stops decoding".
- **Escalate instead of guessing.** BLOCKED is fine. Inventing a contract is not.

---

## Existing API (verified — use these exact signatures)

```go
// internal/slack/boot
type PostFunc func(ctx context.Context, method string, form url.Values) ([]byte, error)
func UserBoot(ctx context.Context, post PostFunc) (*Result, error)
func ConversationsView(ctx context.Context, post PostFunc, channelID string) (*ViewResult, error)
// Result:     Channels []Channel; IMs []IM; IsOpen []string; Starred []json.RawMessage;
//             Subteams Subteams; DND DND; Prefs Prefs; Self Self; Team Team;
//             ChannelsPriority map[string]float64; EmojiCacheTS string;
//             ReadOnlyChannels/NonThreadableChannels/ThreadOnlyChannels []string;
//             DefaultWorkspace string; HasMoreMPDMs bool
// ViewResult: History History; Users []User; Bots []json.RawMessage;
//             Channels []ViewChannelEntry; Emojis map[string]string;
//             Channel ViewChannel; ResponseMetadata ViewResponseMetadata

// internal/slack/edge
func New(token, teamID string, httpClient *http.Client) *Client
func (c *Client) ChannelsInfo(ctx context.Context, updatedIDs map[string]int64) (ChannelsInfoResult, error)
func (c *Client) UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]User, error)
func (c *Client) ChannelsSearch(ctx context.Context, query string, topChannels []string) ([]Channel, []string, error)
func (c *Client) UsersSearch(ctx context.Context, query, currentChannel string, topUsers []string) ([]User, error)
func (c *Client) UsersList(ctx context.Context, channelID string, count int) (users []User, truncated bool, err error)
func (c *Client) ChannelsMembership(ctx context.Context, channelID string, userIDs []string) (members, nonMembers []string, err error)
func (c *Client) UsersCounts(ctx context.Context, channelID string) (Counts, error)
// ChannelsInfoResult: Channels []Channel; MemberChannels []string; FailedIDs []string

// internal/cache
func (db *DB) ChannelVersions(workspaceID string) (map[string]int64, error)
func (db *DB) SetChannelVersion(channelID string, version int64) error
func (db *DB) UserVersions(workspaceID string) (map[string]int64, error)
func (db *DB) SetUserVersion(userID string, version int64) error
func (db *DB) MessageVersions(channelID, oldestTS, latestTS string) (map[string]string, error)
func (db *DB) SetMessageVersion(channelID, ts, version string) error

// internal/slack
func (c *Client) HistoryWithVersions(ctx context.Context, channelID string, opts HistoryOpts) (HistoryResult, error)
// HistoryOpts:   Limit int; Latest, Oldest string; CachedVersions map[string]string; IncludeDateJoined bool
// HistoryResult: Messages []slack.Message; UnchangedTS []string; LatestUpdates map[string]string; HasMore bool
```

`internal/slack/boot` imports **only stdlib** and must keep doing so — `internal/slack` will import `boot`, so the reverse cycles. Mute parsing stays at the caller via `slack.ParseMutedFromAllNotificationsPrefs(result.Prefs.AllNotificationsPrefs)`.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/slackhttp/counter.go` (create) | Per-process API request tally, grouped by endpoint |
| `internal/slackhttp/counter_test.go` (create) | Counter tests |
| `internal/slackhttp/transport.go` (modify) | One call to record each request |
| `internal/cache/edge_sync.go` (create) | Partial-update writers that preserve columns their source cannot populate |
| `internal/cache/edge_sync_test.go` (create) | Preserve-column round-trip tests |
| `internal/slack/edge/cache.go` (modify) | Add `User.ImageOriginal` |
| `internal/bootstrap/bootstrap.go` (create) | `Deps`, `Result`, `Run` — the boot orchestration |
| `internal/bootstrap/revalidate.go` (create) | Scoped `edge.ChannelsInfo`/`UsersInfo` step |
| `internal/bootstrap/*_test.go` (create) | Fake-driven tests incl. the enumeration regression guard |
| `cmd/slk/main.go` (modify) | `connectWorkspace` calls `bootstrap.Run`; deletions |
| `cmd/slk/reconnect_backfill.go` (modify) | `BackfillCandidates` ceases to be a runtime path |
| `internal/slack/client.go` (modify) | Delete `GetUsers`, `GetAllChannels` |

**Do not** restructure `cmd/slk/main.go` beyond what each task names. It is 4,320 lines and this plan already touches its riskiest function.

---

## Task 1: Request counter

**Files:**
- Create: `internal/slackhttp/counter.go`, `internal/slackhttp/counter_test.go`
- Modify: `internal/slackhttp/transport.go`

Instrumentation lands first so Tasks 8-12 have a before/after number. Success criteria 1 and 2 are otherwise unmeasurable, and no one is testing this on a real Grid account until all three phases ship.

- [ ] **Step 1: Write the failing test**

Create `internal/slackhttp/counter_test.go`:

```go
package slackhttp

import (
	"sync"
	"testing"
)

func TestCounter_TalliesByEndpoint(t *testing.T) {
	var c Counter
	c.Record("https://slack.com/api/conversations.history")
	c.Record("https://slack.com/api/conversations.history")
	c.Record("https://slack.com/api/client.counts")
	c.Record("https://edgeapi.slack.com/cache/T1/users/info")

	got := c.Snapshot()
	for _, tc := range []struct {
		endpoint string
		want     int
	}{
		{"conversations.history", 2},
		{"client.counts", 1},
		{"edge:users/info", 1},
	} {
		if got[tc.endpoint] != tc.want {
			t.Errorf("Snapshot()[%q] = %d; want %d (full: %v)", tc.endpoint, got[tc.endpoint], tc.want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("Snapshot() has %d endpoints; want 3 (%v)", len(got), got)
	}
}

func TestCounter_IgnoresNonAPIURLs(t *testing.T) {
	// Asset fetches are not API calls and must not inflate the tally
	// the success criteria are measured against.
	var c Counter
	c.Record("https://files.slack.com/files-tmb/T1-F2/image_360.png")
	c.Record("https://ca.slack-edge.com/T1-U2-abc/avatar")
	c.Record("https://emoji.slack-edge.com/T1/party/abc.gif")
	if got := c.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot() = %v; want empty — asset hosts are not API calls", got)
	}
}

func TestCounter_SnapshotIsACopy(t *testing.T) {
	var c Counter
	c.Record("https://slack.com/api/client.counts")
	got := c.Snapshot()
	got["client.counts"] = 999
	if again := c.Snapshot(); again["client.counts"] != 1 {
		t.Errorf("mutating a Snapshot changed the counter: %d", again["client.counts"])
	}
}

func TestCounter_ConcurrentRecordIsSafe(t *testing.T) {
	// RoundTrip runs on many goroutines; a map write race here would
	// crash the process it is meant to be observing.
	var c Counter
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Record("https://slack.com/api/client.counts")
		}()
	}
	wg.Wait()
	if got := c.Snapshot()["client.counts"]; got != 50 {
		t.Errorf("client.counts = %d; want 50", got)
	}
}

func TestCounter_TotalAndZeroValueUsable(t *testing.T) {
	var c Counter // deliberately not constructed
	if c.Total() != 0 {
		t.Errorf("zero-value Total() = %d; want 0", c.Total())
	}
	c.Record("https://slack.com/api/a.b")
	c.Record("https://slack.com/api/c.d")
	if c.Total() != 2 {
		t.Errorf("Total() = %d; want 2", c.Total())
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/slackhttp/ -run TestCounter -v`
Expected: FAIL — `undefined: Counter`.

- [ ] **Step 3: Implement**

Create `internal/slackhttp/counter.go`:

```go
package slackhttp

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Counter tallies API requests by endpoint for one process.
//
// It exists because Phase 2b's success criteria are call counts — "a
// boot issues <= 10 API calls, with zero users.list and zero
// per-channel conversations.history fan-out" — and until this, the
// only way to check them was scrolling debug logs. Nobody is testing
// slk against a real Enterprise Grid account until all three grid-parity
// phases land, so a local count is the only feedback loop there is.
//
// The zero value is ready to use. Safe for concurrent use: RoundTrip
// runs on many goroutines at once.
type Counter struct {
	mu sync.Mutex
	n  map[string]int
}

// Record tallies one request by rawURL. Non-API URLs are ignored:
// asset fetches (files.slack.com, *.slack-edge.com) are a different
// concern — Layer 3 — and counting them here would drown the API
// numbers the criteria are about, since one boot pulls ~337 assets
// against ~70 API calls in the official client.
func (c *Counter) Record(rawURL string) {
	endpoint, ok := endpointName(rawURL)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = make(map[string]int, 32)
	}
	c.n[endpoint]++
}

// Snapshot returns a copy of the tally. Callers may mutate it.
func (c *Counter) Snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

// Total is the sum of all endpoint counts.
func (c *Counter) Total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, v := range c.n {
		total += v
	}
	return total
}

// Report renders the tally highest-count-first, for a debug log.
func (c *Counter) Report() string {
	snap := c.Snapshot()
	type row struct {
		name string
		n    int
	}
	rows := make([]row, 0, len(snap))
	total := 0
	for k, v := range snap {
		rows = append(rows, row{k, v})
		total += v
	}
	// Count desc, then name asc, so the output is stable enough to
	// diff between two runs.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].name < rows[j].name
	})
	var b strings.Builder
	fmt.Fprintf(&b, "API requests: %d total across %d endpoints\n", total, len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %5d  %s\n", r.n, r.name)
	}
	return b.String()
}

// endpointName reduces a URL to the name the tally is keyed by, and
// reports whether it is an API call at all.
//
// Workspace API: everything after /api/. edgeapi: the last two path
// segments with an "edge:" prefix, so channels/info and users/info stay
// distinguishable from the workspace endpoints and from each other.
func endpointName(rawURL string) (string, bool) {
	// Deliberately string surgery rather than url.Parse: this runs on
	// every request and never needs the query, the fragment, or
	// percent-decoding.
	rest := rawURL
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return "", false
	}
	host, path := rest[:slash], rest[slash:]
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}

	if isEdgeAPIHost(host) {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 2 {
			return "", false
		}
		return "edge:" + parts[len(parts)-2] + "/" + parts[len(parts)-1], true
	}
	if i := strings.Index(path, "/api/"); i >= 0 {
		name := path[i+len("/api/"):]
		if name == "" {
			return "", false
		}
		return name, true
	}
	return "", false
}
```

`isEdgeAPIHost` already exists at `internal/slackhttp/transport.go:352`; reuse it, do not write a second one.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/slackhttp/ -run TestCounter -race -v`
Expected: PASS, all five tests.

- [ ] **Step 5: Wire it into the transport**

In `internal/slackhttp/transport.go`, add a `Counter` field to `BrowserTransport` and record at the top of `RoundTrip`, before any decoration:

```go
	if t.Counter != nil {
		t.Counter.Record(req.URL.String())
	}
```

Add a test in `internal/slackhttp/transport_test.go` asserting a request through `BrowserTransport` with a `Counter` attached increments the right endpoint, and that a nil `Counter` does not panic. Follow the existing `httptest`-based patterns in that file.

- [ ] **Step 6: Verify and mutation-test**

Run: `go test ./internal/slackhttp/ -race`
Expected: PASS, and the whole pre-existing slackhttp suite still green.

Then prove the tests can fail. Run each, capture `$?` on the next line, restore with `git checkout --` after each:

1. Make `Record` ignore edgeapi URLs → `TestCounter_TalliesByEndpoint` must fail.
2. Make `endpointName` return the full path instead of the name after `/api/` → must fail.
3. Make `Snapshot` return the internal map instead of a copy → `TestCounter_SnapshotIsACopy` must fail.
4. Remove the mutex from `Record` → `TestCounter_ConcurrentRecordIsSafe` must fail **under `-race`**.
5. Make `endpointName` accept asset hosts → `TestCounter_IgnoresNonAPIURLs` must fail.

Paste the literal output of all five.

- [ ] **Step 7: Commit**

```bash
git add internal/slackhttp/counter.go internal/slackhttp/counter_test.go internal/slackhttp/transport.go internal/slackhttp/transport_test.go
git commit -m "feat(slackhttp): count API requests by endpoint

Phase 2b's success criteria are call counts, and until now the only way
to check them was scrolling debug logs. The transport is already the
single chokepoint under both slack-go and the hand-rolled postForm
path, so there is one place to instrument."
```

---

## Task 2: Cache writers that preserve what they cannot populate

**Files:**
- Create: `internal/cache/edge_sync.go`, `internal/cache/edge_sync_test.go`

**This is the task most likely to cause silent data loss.** Read the design's *Cache column mapping* section before starting.

`UpsertChannel` (`internal/cache/channels.go:19`) and `UpsertUser` (`internal/cache/users.go:24`) use `ON CONFLICT … DO UPDATE SET` over a fixed column list. The Phase 2a sources cover **different subsets** of those columns:

| Column | populated by `edge.ChannelsInfo`? |
|---|---|
| `name`, `type`, `topic`, `version` | yes |
| `is_member` | **no** — 0 of 36 observed results carry `is_member`; membership arrives as the top-level `MemberChannels` array |
| `is_starred` | **no** — no edge endpoint returns it |
| `last_read_ts`, `unread_count`, `has_unread` | **no** — those come from `client.counts` |

| Column | populated by `edge.UsersInfo`? |
|---|---|
| `name`, `display_name`, `is_bot`, `version` | yes |
| `avatar_url` | only after Task 3 adds `ImageOriginal` |
| `presence` | **no** — no edge endpoint returns it |

Feeding these straight into the full upserts blanks avatars, membership, starred state and presence on **every** revalidation — silently, surfacing as UI bugs long afterwards.

- [ ] **Step 1: Write the failing test**

Create `internal/cache/edge_sync_test.go`:

```go
package cache

import "testing"

// seedChannelFull writes a channel with every column non-zero, so any
// column a later partial update wrongly clears is visible.
func seedChannelFull(t *testing.T, db *DB, workspaceID, id string) {
	t.Helper()
	if err := db.UpsertChannel(Channel{
		ID: id, WorkspaceID: workspaceID, Name: "original-name",
		Type: "channel", Topic: "original topic",
		IsMember: true, IsStarred: true, UpdatedAt: 111,
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
}

func getChannelRow(t *testing.T, db *DB, id string) Channel {
	t.Helper()
	ch, err := db.GetChannel(id)
	if err != nil {
		t.Fatalf("GetChannel(%s): %v", id, err)
	}
	if ch == nil {
		t.Fatalf("GetChannel(%s) = nil", id)
	}
	return *ch
}

func TestUpdateChannelFromEdge_PreservesColumnsEdgeCannotSupply(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	mustWorkspace(t, db, "T1")
	seedChannelFull(t, db, "T1", "C1")

	// What channels/info actually returns: name, type, topic, version.
	// NOT is_member (0 of 36 observed results carry it) and NOT
	// is_starred (no edge endpoint returns it).
	if err := db.UpdateChannelFromEdge(EdgeChannelUpdate{
		ID: "C1", Name: "new-name", Type: "private", Topic: "new topic",
		Version: 1783337533019,
	}); err != nil {
		t.Fatalf("UpdateChannelFromEdge: %v", err)
	}

	got := getChannelRow(t, db, "C1")
	if got.Name != "new-name" || got.Topic != "new topic" || got.Type != "private" {
		t.Errorf("edge-owned columns not written: %+v", got)
	}
	// The whole point of this method existing.
	if !got.IsMember {
		t.Error("is_member was cleared; channels/info does not carry it, so it must be preserved — clearing it drops the user out of their own channels")
	}
	if !got.IsStarred {
		t.Error("is_starred was cleared; no edge endpoint returns it, so it must be preserved")
	}

	vers, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if vers["C1"] != 1783337533019 {
		t.Errorf("version = %d; want 1783337533019", vers["C1"])
	}
}

func TestApplyMembership_SetsAndClearsOnlyTheIDsQueried(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	mustWorkspace(t, db, "T1")
	for _, id := range []string{"C1", "C2", "C3"} {
		seedChannelFull(t, db, "T1", id)
	}

	// MemberChannels is a snapshot over the ids SENT, not a delta: an
	// id that was sent and is absent is a non-membership; an id never
	// sent says nothing. C3 was not queried and must be untouched.
	if err := db.ApplyMembership("T1", []string{"C1", "C2"}, []string{"C1"}); err != nil {
		t.Fatalf("ApplyMembership: %v", err)
	}

	if !getChannelRow(t, db, "C1").IsMember {
		t.Error("C1 was in member_channels and must stay a member")
	}
	if getChannelRow(t, db, "C2").IsMember {
		t.Error("C2 was queried and absent from member_channels, so it is a non-membership and must be cleared")
	}
	if !getChannelRow(t, db, "C3").IsMember {
		t.Error("C3 was never queried; ApplyMembership must not touch it — treating unqueried as non-member drops every channel not in the batch")
	}
	if !getChannelRow(t, db, "C2").IsStarred {
		t.Error("ApplyMembership must only write is_member")
	}
}

func TestUpdateUserFromEdge_PreservesColumnsEdgeCannotSupply(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	mustWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{
		ID: "U1", WorkspaceID: "T1", Name: "orig", DisplayName: "Orig",
		AvatarURL: "https://example.invalid/orig.png", Presence: "active",
		IsBot: false, IsExternal: true, UpdatedAt: 222,
	}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// No AvatarURL supplied: users/info carries image_original on 255
	// of 291 observed results, but a user with no custom image has
	// none, and blanking a good URL on that basis is the bug.
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "new", DisplayName: "New", IsBot: true,
		Version: 1612802061,
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge: %v", err)
	}

	u, err := db.GetUser("U1")
	if err != nil || u == nil {
		t.Fatalf("GetUser: %v (nil=%v)", err, u == nil)
	}
	if u.Name != "new" || u.DisplayName != "New" || !u.IsBot {
		t.Errorf("edge-owned columns not written: %+v", u)
	}
	if u.AvatarURL != "https://example.invalid/orig.png" {
		t.Errorf("avatar_url = %q; want the original preserved — an empty AvatarURL means 'this source has none', not 'this user has none'", u.AvatarURL)
	}
	if u.Presence != "active" {
		t.Errorf("presence = %q; want active preserved — no edge endpoint returns presence", u.Presence)
	}
}

func TestUpdateUserFromEdge_WritesAvatarWhenSupplied(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	mustWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{
		ID: "U1", WorkspaceID: "T1", Name: "orig",
		AvatarURL: "https://example.invalid/orig.png", Presence: "active",
	}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "orig", AvatarURL: "https://example.invalid/new.png",
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge: %v", err)
	}
	u, _ := db.GetUser("U1")
	if u.AvatarURL != "https://example.invalid/new.png" {
		t.Errorf("avatar_url = %q; want the new one — preserve must not mean ignore", u.AvatarURL)
	}
}

func TestUpdateFromEdge_UnknownRowIsANoOpNotAnInsert(t *testing.T) {
	// These are revalidation writers. A row we have never seen is
	// hydrated through the normal Upsert path, which knows every
	// column; inserting a half-populated row here would create a user
	// with no avatar and a channel with is_member=false.
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	mustWorkspace(t, db, "T1")

	if err := db.UpdateChannelFromEdge(EdgeChannelUpdate{ID: "CNOPE", Name: "x"}); err != nil {
		t.Fatalf("UpdateChannelFromEdge on a missing row: %v", err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{ID: "UNOPE", Name: "x"}); err != nil {
		t.Fatalf("UpdateUserFromEdge on a missing row: %v", err)
	}
	if ch, _ := db.GetChannel("CNOPE"); ch != nil {
		t.Error("UpdateChannelFromEdge inserted a row; it must only update")
	}
	if u, _ := db.GetUser("UNOPE"); u != nil {
		t.Error("UpdateUserFromEdge inserted a row; it must only update")
	}
}
```

`mustWorkspace` may not exist under that name — check `internal/cache/*_test.go` for the existing workspace-seeding helper and use it. If none exists, write a three-line one in this file. Do **not** invent a shared helper in another file. Likewise confirm `GetChannel` and `GetUser` exist with those signatures before relying on them; if the accessors differ, adapt the assertions rather than adding new accessors.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/cache/ -run 'FromEdge|ApplyMembership' -v`
Expected: FAIL — `undefined: EdgeChannelUpdate`.

- [ ] **Step 3: Implement**

Create `internal/cache/edge_sync.go`:

```go
package cache

import "fmt"

// EdgeChannelUpdate is what edgeapi's channels/info can tell us about a
// channel — and nothing more.
//
// Deliberately not cache.Channel. The full struct has fields no edge
// response carries (IsMember, IsStarred), and a caller filling those
// with zero values and handing them to UpsertChannel is exactly the
// silent data loss this type exists to make impossible. If a field is
// absent here, the column it maps to is preserved.
type EdgeChannelUpdate struct {
	ID      string
	Name    string
	Type    string
	Topic   string
	Version int64
}

// UpdateChannelFromEdge applies a revalidation result, touching only
// the columns channels/info actually populates.
//
// is_member is NOT among them: 0 of 36 observed channels/info results
// carried it, because membership comes back as the response's
// top-level member_channels array instead. Use ApplyMembership for
// that. is_starred, last_read_ts, unread_count and has_unread come
// from other sources entirely and are likewise preserved.
//
// A row that does not exist is left alone rather than inserted: this
// is a revalidation writer, and an unknown channel must go through
// UpsertChannel, which knows every column.
func (db *DB) UpdateChannelFromEdge(u EdgeChannelUpdate) error {
	_, err := db.conn.Exec(`
		UPDATE channels
		SET name = ?, type = ?, topic = ?, version = ?
		WHERE id = ?`,
		u.Name, u.Type, u.Topic, u.Version, u.ID)
	if err != nil {
		return fmt.Errorf("updating channel %s from edge: %w", u.ID, err)
	}
	return nil
}

// ApplyMembership records the membership snapshot from a channels/info
// response.
//
// queriedIDs is every id the request sent; memberIDs is the subset the
// response listed in member_channels. An id in queriedIDs but not
// memberIDs is a genuine non-membership and is cleared. An id in
// NEITHER is untouched — member_channels is a snapshot over what was
// asked, not a workspace-wide list, so treating unqueried ids as
// non-members would drop the user out of every channel outside the
// current batch.
func (db *DB) ApplyMembership(workspaceID string, queriedIDs, memberIDs []string) error {
	if len(queriedIDs) == 0 {
		return nil
	}
	members := make(map[string]bool, len(memberIDs))
	for _, id := range memberIDs {
		members[id] = true
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("applying membership: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`UPDATE channels SET is_member = ? WHERE id = ? AND workspace_id = ?`)
	if err != nil {
		return fmt.Errorf("applying membership: %w", err)
	}
	defer stmt.Close()

	for _, id := range queriedIDs {
		if _, err := stmt.Exec(boolToInt(members[id]), id, workspaceID); err != nil {
			return fmt.Errorf("applying membership for %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// EdgeUserUpdate is what edgeapi's users/info and users/search can tell
// us about a user. Same contract as EdgeChannelUpdate: absent field
// means "preserve the column", never "clear it".
type EdgeUserUpdate struct {
	ID          string
	Name        string
	DisplayName string
	// AvatarURL is empty when the source had none — users/info carries
	// image_original on 255 of 291 observed results, and the users
	// without it are the ones with no custom image. Empty therefore
	// means "this response says nothing", so the column is preserved.
	AvatarURL  string
	IsBot      bool
	IsExternal bool
	Version    int64
}

// UpdateUserFromEdge applies a revalidation result, touching only the
// columns an edge response populates. presence is never among them.
//
// avatar_url is written only when non-empty, for the reason on the
// field: blanking a good URL because this particular user has no
// custom image is the failure this whole file exists to prevent.
func (db *DB) UpdateUserFromEdge(u EdgeUserUpdate) error {
	var err error
	if u.AvatarURL != "" {
		_, err = db.conn.Exec(`
			UPDATE users
			SET name = ?, display_name = ?, avatar_url = ?, is_bot = ?, is_external = ?, version = ?
			WHERE id = ?`,
			u.Name, u.DisplayName, u.AvatarURL, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version, u.ID)
	} else {
		_, err = db.conn.Exec(`
			UPDATE users
			SET name = ?, display_name = ?, is_bot = ?, is_external = ?, version = ?
			WHERE id = ?`,
			u.Name, u.DisplayName, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version, u.ID)
	}
	if err != nil {
		return fmt.Errorf("updating user %s from edge: %w", u.ID, err)
	}
	return nil
}
```

`boolToInt` already exists in this package (used by `UpsertMessage`); reuse it.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cache/ -race -v`
Expected: PASS — the new tests and the whole existing cache suite.

- [ ] **Step 5: Mutation-test**

Run each, `$?` on the next line, restore with `git checkout --` after each. Paste literal output.

1. Add `is_member = 0` to `UpdateChannelFromEdge`'s SET list → the preserve test must fail.
2. Add `is_starred = 0` → must fail.
3. Make `ApplyMembership` clear `is_member` for every channel in the workspace rather than only `queriedIDs` → the C3-untouched assertion must fail.
4. Make `UpdateUserFromEdge` always write `avatar_url`, including empty → the preserve test must fail.
5. Make `UpdateUserFromEdge` never write `avatar_url` → `WritesAvatarWhenSupplied` must fail.
6. Add `presence = 'away'` to `UpdateUserFromEdge` → must fail.
7. Turn `UpdateChannelFromEdge` into an upsert (`INSERT … ON CONFLICT`) → `UnknownRowIsANoOpNotAnInsert` must fail.

Mutations 1-4 and 6 are the exact bugs this task exists to prevent. If any of them survives, the test is decorative and must be fixed before you continue.

- [ ] **Step 6: Commit**

```bash
git add internal/cache/edge_sync.go internal/cache/edge_sync_test.go
git commit -m "feat(cache): revalidation writers that preserve what they cannot see

UpsertChannel and UpsertUser overwrite a fixed column list, and the
edge sources cover different subsets of it: channels/info carries no
is_member (0 of 36 observed results) and no is_starred, users/info no
presence. Wiring revalidation into the full upserts would blank all of
them on every pass, silently, surfacing as UI bugs much later."
```

---

## Task 3: `edge.User` gains `ImageOriginal`

**Files:**
- Modify: `internal/slack/edge/cache.go`, `internal/slack/edge/cache_test.go`

Phase 2a omitted every image field from `edge.User` on the belief that `users/info` profiles carry no image URL. That was wrong, and its outcomes doc records the correction: measured over **all 291** `users/info` result objects, `profile.image_original` is present on **255** and non-empty, `is_custom_image` on 255, while `image_32`/`72`/`192` are 0/291. `users/search` agrees. Dropping `Image32` was right; concluding "no image anywhere" was not.

Adding the field removes the `avatar_url` hazard at its source instead of working around it in Task 2.

- [ ] **Step 1: Write the failing test**

Add to `internal/slack/edge/cache_test.go`, following that file's existing fixture style — a `users/info` response whose profile carries `image_original` and `is_custom_image`, asserting both decode. Give `image_original` a distinct non-empty value; the file's own convention (established after Phase 2a lost 9 mutants to all-zero fixtures) is that every asserted field gets a distinct, non-zero value.

Also assert that a profile **without** `image_original` decodes to the empty string rather than failing — 36 of 291 observed results have none.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/slack/edge/ -run UsersInfo -v`
Expected: FAIL — `res[0].Profile.ImageOriginal undefined`.

- [ ] **Step 3: Implement**

In `internal/slack/edge/cache.go`, add to `User.Profile`:

```go
		// ImageOriginal is the user's avatar URL. Present on 255 of
		// the 291 observed users/info results (and on users/search
		// too — the two endpoints agree). The 36 without it are users
		// with no custom image, which is why an empty value here means
		// "this user has no custom avatar", never "this endpoint
		// cannot tell you".
		//
		// The sized variants are genuinely absent: image_32, image_72
		// and image_192 are 0 of 291.
		ImageOriginal  string `json:"image_original"`
		IsCustomImage  bool   `json:"is_custom_image"`
```

Correct the doc comments on `User` (`cache.go`) and `UsersSearch` (`search.go`) that assert `users/info` has no image URL. Replace the claim with the measured counts above and state that the two endpoints agree.

- [ ] **Step 4: Verify and mutation-test**

Run: `go test ./internal/slack/edge/ -race`
Expected: PASS.

Mutations, literal output each: (1) `ImageOriginal` tagged `json:"-"` → must fail; (2) `ImageOriginal` mis-tagged to `image_32` → must fail; (3) `IsCustomImage` and some other bool swapped → must fail.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/edge/cache.go internal/slack/edge/cache_test.go internal/slack/edge/search.go
git commit -m "fix(edge): users/info does carry an avatar URL

image_original is on 255 of 291 observed results, not zero. Phase 2a
generalised from a single sample -- two of the three committed
users/info fixtures were results:[], and the one that remained held a
user with no custom image. Modelling it removes the avatar-blanking
hazard at its source."
```

---

## Task 4: `internal/bootstrap` skeleton

**Files:**
- Create: `internal/bootstrap/bootstrap.go`, `internal/bootstrap/bootstrap_test.go`

`connectWorkspace` (`cmd/slk/main.go:1889`) constructs a live client and calls `Connect`, which Phase 1's outcomes recorded makes it untestable: "no injectable seam without either a live Slack connection or an interface extraction whose only consumer would be the test."

This task creates that seam, and only that seam. The orchestration moves; the UI wiring does not.

- [ ] **Step 1: Write the failing test**

Create `internal/bootstrap/bootstrap_test.go`:

```go
package bootstrap

import (
	"context"
	"errors"
	"testing"
)

func TestRun_CallsUserBootThenCounts(t *testing.T) {
	f := newFakeDeps()
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Order matters: counts is keyed by the conversations userBoot
	// returns, so calling it first would ask about channels we have
	// not learned about yet.
	want := []string{"client.userBoot", "client.counts"}
	if !prefixEqual(f.calls, want) {
		t.Errorf("call sequence = %v; want it to start %v", f.calls, want)
	}
	if res == nil {
		t.Fatal("Run returned a nil Result with a nil error")
	}
	if res.Self.ID != "U_SELF" {
		t.Errorf("Result.Self.ID = %q; want U_SELF", res.Self.ID)
	}
}

func TestRun_NeverEnumerates(t *testing.T) {
	// The regression guard this whole package exists for. slk's
	// Enterprise Grid accounts get signed out for "data scraping",
	// and across 8 captures the official client issues ZERO
	// users.list, ZERO conversations.list, and zero per-channel
	// conversations.history at boot.
	f := newFakeDeps()
	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, forbidden := range []string{"users.list", "conversations.list", "users.conversations"} {
		if f.called(forbidden) {
			t.Errorf("boot called %s; the official client never does, and it is the signature that gets Grid users signed out (sequence: %v)", forbidden, f.calls)
		}
	}
	if n := f.countPrefix("conversations.history"); n > 1 {
		t.Errorf("boot made %d conversations.history calls; at most one (the opened channel's fallback) is allowed, never a per-channel fan-out (sequence: %v)", n, f.calls)
	}
}

func TestRun_BootCallBudget(t *testing.T) {
	// Success criterion 1: a boot issues <= 10 API calls. The fake
	// counts one per dependency invocation, which is the same unit
	// the slackhttp Counter measures.
	f := newFakeDeps()
	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.calls) > 10 {
		t.Errorf("boot issued %d calls; budget is 10 (sequence: %v)", len(f.calls), f.calls)
	}
}

func TestRun_UserBootFailureIsFatal(t *testing.T) {
	// Everything downstream is keyed by what userBoot returns. There
	// is no degraded mode worth having.
	f := newFakeDeps()
	f.userBootErr = errors.New("invalid_auth")
	if _, err := Run(context.Background(), f.Deps()); err == nil {
		t.Fatal("Run returned nil error when userBoot failed")
	}
}

func TestRun_CountsFailureIsNotFatal(t *testing.T) {
	// Unread badges are cosmetic; a workspace that boots without them
	// is far better than one that does not boot.
	f := newFakeDeps()
	f.countsErr = errors.New("ratelimited")
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: counts failure should not be fatal, got %v", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result")
	}
}
```

Write the `fakeDeps` harness in the same file: it records an ordered `calls []string`, returns canned `*boot.Result` / `*boot.ViewResult` values, and exposes per-dependency error injection (`userBootErr`, `countsErr`, …). Give it `called(name) bool`, `countPrefix(name) int`, and a `prefixEqual` helper. Build the canned `boot.Result` with **distinct, non-zero** values for every field a test asserts — Phase 2a lost 9 mutants to fixtures where everything was the zero value.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/bootstrap/ -v`
Expected: FAIL — `undefined: Run`.

- [ ] **Step 3: Implement**

Create `internal/bootstrap/bootstrap.go`:

```go
// Package bootstrap owns the sequence of API calls slk makes when it
// connects to a workspace.
//
// It exists as a package, rather than as more of cmd/slk/main.go's
// connectWorkspace, for one reason: this sequence is what gets slk's
// Enterprise Grid users signed out for "data scraping", and inside
// connectWorkspace no test could reach it. connectWorkspace builds a
// live *slack.Client and calls Connect, so there is no seam without a
// live Slack connection. Everything here takes an interface.
//
// The call budget is the point. Across 8 captures of the official web
// client, a boot issues ~70 API requests and NEVER enumerates: zero
// users.list, zero conversations.list, zero per-channel
// conversations.history. slk previously issued roughly 400 and did all
// three. TestRun_NeverEnumerates is the regression guard.
package bootstrap

import (
	"context"
	"fmt"

	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/slack/edge"
)

// UserBooter fetches and parses client.userBoot.
type UserBooter interface {
	UserBoot(ctx context.Context) (*boot.Result, error)
}

// Counter fetches client.counts, slk's unread source of truth.
type Counter interface {
	Counts(ctx context.Context) (Counts, error)
}

// Viewer fetches conversations.view for one channel. channelID may be
// "", reproducing the captured request, which sent no channel param
// and got back the last-viewed conversation.
type Viewer interface {
	ConversationsView(ctx context.Context, channelID string) (*boot.ViewResult, error)
}

// Historian is the verified fallback for Viewer: conversations.history
// with limit=28 and cached_latest_updates.
type Historian interface {
	HistoryWithVersions(ctx context.Context, channelID string, cached map[string]string) (History, error)
}

// Revalidator is the edgeapi conditional-revalidation pair. This is
// what replaces enumeration.
type Revalidator interface {
	ChannelsInfo(ctx context.Context, updatedIDs map[string]int64) (edge.ChannelsInfoResult, error)
	UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]edge.User, error)
}

// Store is the cache surface bootstrap writes through. Deliberately
// the narrow revalidation writers from internal/cache/edge_sync.go,
// not the full upserts: a full upsert would blank is_member,
// is_starred, avatar_url and presence, none of which any edge response
// carries.
type Store interface {
	ChannelVersions(workspaceID string) (map[string]int64, error)
	UserVersions(workspaceID string) (map[string]int64, error)
	ApplyMembership(workspaceID string, queriedIDs, memberIDs []string) error
}

// Deps is everything Run needs. Every field is required unless its
// comment says otherwise.
type Deps struct {
	WorkspaceID string

	Boot        UserBooter
	Counts      Counter
	View        Viewer
	History     Historian
	Revalidate  Revalidator
	Store       Store

	// OpenChannelID is the conversation to open — the restored last
	// channel, or the configured default. Empty means "whatever Slack
	// considers last-viewed", which is what the capture did.
	OpenChannelID string

	// Log is optional; nil discards.
	Log func(format string, args ...any)
}

// Run performs the boot sequence and returns everything the UI needs.
func Run(ctx context.Context, deps Deps) (*Result, error) {
	logf := deps.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	bootRes, err := deps.Boot.UserBoot(ctx)
	if err != nil {
		// Fatal: every step below is keyed by what this returned.
		return nil, fmt.Errorf("bootstrap: userBoot: %w", err)
	}

	out := &Result{
		Self:             bootRes.Self,
		Team:             bootRes.Team,
		Channels:         bootRes.Channels,
		IMs:              bootRes.IMs,
		IsOpen:           bootRes.IsOpen,
		DND:              bootRes.DND,
		ChannelsPriority: bootRes.ChannelsPriority,
		EmojiCacheTS:     bootRes.EmojiCacheTS,
		MutePrefsRaw:     bootRes.Prefs.AllNotificationsPrefs,
		LegacyMutedRaw:   bootRes.Prefs.MutedChannels,
	}

	// Unread state. Non-fatal: badges are cosmetic and a workspace
	// that boots without them beats one that does not boot.
	if counts, err := deps.Counts.Counts(ctx); err != nil {
		logf("bootstrap: counts: %v (continuing without unread state)", err)
	} else {
		out.Counts = counts
	}

	return out, nil
}
```

Define `Result`, `Counts` and `History` in the same file. `Result` exposes only what `connectWorkspace` consumes — reuse `boot.Self`, `boot.Team`, `boot.Channel`, `boot.IM`, `boot.DND` rather than redeclaring them.

Note `MutePrefsRaw` is the **raw** `all_notifications_prefs` string: `bootstrap` must not import `internal/slack`, so parsing happens at the call site via `slack.ParseMutedFromAllNotificationsPrefs`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/bootstrap/ -race -v`
Expected: PASS. Note `TestRun_NeverEnumerates` passes trivially at this point — nothing calls anything forbidden yet. That is correct; it becomes load-bearing in Tasks 5-7 and Task 9.

- [ ] **Step 5: Mutation-test**

Literal output for each; restore after each:

1. Swap the userBoot and counts calls → `CallsUserBootThenCounts` must fail.
2. Make a userBoot error non-fatal (log and continue) → `UserBootFailureIsFatal` must fail.
3. Make a counts error fatal → `CountsFailureIsNotFatal` must fail.
4. Add a call to a fake dependency named `users.list` inside `Run` → `NeverEnumerates` must fail. **This one matters most**: it proves the regression guard is wired to something, not merely passing because `Run` is short. If it does not fail, your fake is not recording, and every later task's guarantee is worthless.

- [ ] **Step 6: Commit**

```bash
git add internal/bootstrap/
git commit -m "feat(bootstrap): extract the boot sequence behind interfaces

connectWorkspace builds a live client and calls Connect, so no test
could reach the sequence that decides whether slk looks like a scraper.
Adds userBoot + counts and the enumeration regression guard; the
remaining steps land in the following tasks."
```

---

## Task 5: Open the first channel — `conversations.view` with a verified fallback

**Files:**
- Modify: `internal/bootstrap/bootstrap.go`, `internal/bootstrap/bootstrap_test.go`

`conversations.view` returns history, the users those messages reference, bots, channels and emoji in one response — replacing the initial `conversations.history` plus a per-author `users.info` fan-out plus `emoji.list`.

**The `channel` param is unverified.** No captured request carried one; the client sent none and got the last-viewed conversation back. slk needs a *specific* channel. The response's `Channel.ID` is how a caller detects the param was ignored — a caller that skips that comparison renders the wrong channel's history with no error anywhere.

- [ ] **Step 1: Write the failing test**

Add to `internal/bootstrap/bootstrap_test.go`:

```go
func TestRun_OpensTheRequestedChannel(t *testing.T) {
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_WANT" // server honoured the param

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.viewRequestedChannel != "C_WANT" {
		t.Errorf("conversations.view was sent channel=%q; want C_WANT", f.viewRequestedChannel)
	}
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT", res.OpenedChannelID)
	}
	if f.called("conversations.history") {
		t.Error("fell back to conversations.history even though view honoured the channel param")
	}
	if len(res.Messages) == 0 {
		t.Error("no messages from conversations.view")
	}
}

func TestRun_FallsBackWhenViewIgnoresTheChannelParam(t *testing.T) {
	// The unverified-param failure mode. The server answers 200 with a
	// perfectly good response for the WRONG conversation. Without the
	// id comparison slk renders someone else's channel and nothing
	// anywhere reports an error.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_LASTVIEWED" // param ignored

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.called("conversations.history") {
		t.Error("view returned the wrong channel and Run did not fall back to conversations.history")
	}
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT — the fallback must open the channel that was asked for", res.OpenedChannelID)
	}
}

func TestRun_FallsBackWhenViewErrors(t *testing.T) {
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: view failure must fall back, not fail the boot: %v", err)
	}
	if !f.called("conversations.history") {
		t.Error("view errored and Run did not fall back")
	}
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT", res.OpenedChannelID)
	}
}

func TestRun_FallbackSendsCachedVersions(t *testing.T) {
	// The fallback is conversations.history WITH cached_latest_updates
	// — the incremental-sync primitive. Falling back to a plain
	// history fetch would re-download scrollback slk already holds,
	// which is the behaviour this phase removes.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")
	f.cachedVersions = map[string]string{"1700000001.000100": "1783024685.163100"}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.historyCachedVersions) != 1 {
		t.Errorf("history was sent %d cached versions; want the 1 slk holds", len(f.historyCachedVersions))
	}
}

func TestRun_NoOpenChannelSkipsBothCalls(t *testing.T) {
	// First run on a fresh workspace with nothing restored: there is
	// no channel to open, and inventing one would be an extra request.
	f := newFakeDeps()
	f.deps.OpenChannelID = ""

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.called("conversations.view") || f.called("conversations.history") {
		t.Errorf("opened a channel with none requested (sequence: %v)", f.calls)
	}
}
```

Extend `fakeDeps` with `viewChannelID`, `viewRequestedChannel`, `viewErr`, `cachedVersions`, `historyCachedVersions`.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/bootstrap/ -run 'Opens|FallsBack|NoOpenChannel' -v`
Expected: FAIL — `res.OpenedChannelID undefined`.

- [ ] **Step 3: Implement**

Add to `Run`, after the counts step:

```go
	if deps.OpenChannelID != "" {
		out.OpenedChannelID = deps.OpenChannelID
		if err := openChannel(ctx, deps, out, logf); err != nil {
			return nil, fmt.Errorf("bootstrap: opening %s: %w", deps.OpenChannelID, err)
		}
	}
```

and the helper:

```go
// openChannel loads the first channel's history, preferring
// conversations.view and falling back to conversations.history.
//
// The `channel` param on conversations.view is UNVERIFIED: no captured
// request carried one, and the client got back whatever it had last
// viewed. So the response's Channel.ID is compared to what was asked
// for, and a mismatch is treated exactly like an error. Skipping that
// comparison means rendering another conversation's messages under
// this channel's name, with nothing anywhere reporting a problem.
//
// The fallback is conversations.history with cached_latest_updates,
// which IS fully verified (14 of 14 captured requests) — not a plain
// history fetch, which would re-download scrollback slk already holds.
func openChannel(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) error {
	want := deps.OpenChannelID

	view, err := deps.View.ConversationsView(ctx, want)
	switch {
	case err != nil:
		logf("bootstrap: conversations.view failed (%v); falling back to conversations.history", err)
	case view.Channel.ID != want: // ViewChannel embeds boot.Channel, so .ID resolves through it
		logf("bootstrap: conversations.view ignored the channel param (asked %s, got %s); falling back to conversations.history",
			want, view.Channel.ID)
	default:
		out.Messages = view.History.Messages
		out.Users = view.Users
		out.ViewChannels = view.Channels
		out.Emojis = view.Emojis
		out.HasMore = view.History.HasMore
		return nil
	}

	cached, err := deps.Store.MessageVersions(want)
	if err != nil {
		// Not fatal: an empty map means "we vouch for nothing", which
		// is the shape the client sends when it holds nothing.
		logf("bootstrap: reading cached message versions for %s: %v", want, err)
		cached = nil
	}
	hist, err := deps.History.HistoryWithVersions(ctx, want, cached)
	if err != nil {
		return err
	}
	out.Messages = hist.Messages
	out.HasMore = hist.HasMore
	out.UnchangedTS = hist.UnchangedTS
	out.LatestUpdates = hist.LatestUpdates
	return nil
}
```

Add `MessageVersions(channelID string) (map[string]string, error)` to the `Store` interface. Note `cache.MessageVersions` takes `(channelID, oldestTS, latestTS)`; the adapter in Task 7 supplies the window.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/bootstrap/ -race -v`
Expected: PASS, including the earlier tests.

- [ ] **Step 5: Mutation-test**

Literal output each:

1. Delete the `view.Channel.ID != want` case → `FallsBackWhenViewIgnoresTheChannelParam` must fail. **This is the most important mutation in the task.**
2. Make the fallback call history with a nil `cached` map always → `FallbackSendsCachedVersions` must fail.
3. Make a view error fatal → `FallsBackWhenViewErrors` must fail.
4. Open a channel even when `OpenChannelID` is empty → `NoOpenChannelSkipsBothCalls` must fail.
5. Send `""` as the view channel param while still comparing ids → `OpensTheRequestedChannel` must fail on `viewRequestedChannel`.

- [ ] **Step 6: Commit**

```bash
git add internal/bootstrap/
git commit -m "feat(bootstrap): open the first channel via conversations.view

One call replaces the initial conversations.history plus the per-author
users.info fan-out plus emoji.list. The channel param is unverified --
no captured request carried one -- so the response's channel.id is
compared to what was asked for and a mismatch falls back to
conversations.history with cached_latest_updates, which is verified."
```

---

## Task 6: Scoped conditional revalidation

**Files:**
- Create: `internal/bootstrap/revalidate.go`, `internal/bootstrap/revalidate_test.go`

**This is the step that replaces the `users.list` sweep.** Send `{id: version}` for what slk holds, receive only what changed. A fully current cache costs one sub-KB response per batch instead of ~50 paginated pages.

The scope is a **rule**, not a judgement call (design §Boot sequence):

- `ChannelsInfo` sends the ids in `userBoot`'s `channels` + `ims` — the conversations that will actually render in the sidebar. **Not** every channel in the cache.
- `UsersInfo` sends the authors `conversations.view` returned plus the counterparties of open DMs. **Nothing else at boot.** Users met later are resolved on demand.

Anything outside that set stays stale and is revalidated lazily on first use. This matters beyond politeness: a fixed batch over an unbounded id set produces a long run of identically-sized requests — 125 consecutive exactly-80s on a 10k-user workspace — which is a *cleaner* signature than the official client's ragged 1-80 distribution. Scoping the id set is the fix. Jitter is not: inventing a shape with no evidence is the Phase 1 `sec-ch-ua` mistake.

- [ ] **Step 1: Write the failing test**

Create `internal/bootstrap/revalidate_test.go` covering:

- **Channel ids sent are exactly `userBoot` channels + ims.** Seed the cache with extra channels the boot response does not mention and assert they are **not** sent. Name the test so the intent survives: `TestRevalidate_SendsOnlySidebarChannelsNotTheWholeCache`.
- **Versions come from the cache**, and a channel the cache has never seen is sent with `0` — that is how the protocol asks for a full record, and the captures show the client doing exactly that (`{"C6M7U8DFF":0}`).
- **User ids sent are exactly the view authors plus open-DM counterparties.** Seed the cache with 500 other users and assert none is sent. `TestRevalidate_DoesNotSendEveryCachedUser`.
- **`MemberChannels` is applied via `ApplyMembership` with the queried id set**, not the returned one — assert the `queriedIDs` argument equals what was sent, since passing only the returned members would silently clear membership for everything else in the batch.
- **`FailedIDs` are logged and left stale**, never recorded as fresh. A failed id looks identical to an unchanged one; marking it current keeps a stale record forever because its version never advances. Assert `SetChannelVersion` was *not* called for a failed id.
- **An empty id set makes no request at all** (both endpoints).
- **A revalidation error is non-fatal** — the workspace still boots from cache; assert `Run` returns a usable `Result`.

Use distinct, non-zero values throughout.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/bootstrap/ -run Revalidate -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

Create `internal/bootstrap/revalidate.go` with:

```go
// revalidate refreshes the cache against edgeapi instead of
// enumerating it.
//
// This is the function that replaces slk's ~50-page users.list sweep.
// The official client issues zero users.list and zero
// conversations.list calls across all 8 captures; it sends
// {id: version} for what it holds and gets back only what moved. A
// fully current cache costs one ~290-byte response per batch.
//
// The id sets are deliberately SCOPED rather than "everything cached":
//
//   - channels: userBoot's channels + ims, i.e. what the sidebar will
//     actually render.
//   - users: the authors conversations.view returned, plus open-DM
//     counterparties.
//
// Everything else is left stale and revalidated when first needed. A
// fixed batch size over an unbounded id set emits a long run of
// identically-sized requests, which is a cleaner distributional
// signature than the client's own ragged 1-80 spread. Scoping is the
// fix; jitter would be inventing a shape no capture shows.
//
// Errors are non-fatal. A stale cache renders; a workspace that failed
// to boot does not.
func revalidate(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) {
```

Behaviour, in order:

1. Build `channelIDs` from `out.Channels` + `out.IMs`. Look up `deps.Store.ChannelVersions(deps.WorkspaceID)`; for each id use the cached version, or `0` when absent.
2. Call `deps.Revalidate.ChannelsInfo`. On error, log and return — do not abort the boot.
3. For each returned channel, `deps.Store.UpdateChannelFromEdge(...)` (Task 2), which preserves `is_member`/`is_starred`.
4. Apply membership. **The `Store` interface must take `cache.MembershipSnapshot`, not a `[]string`** — that type exists precisely because `encoding/json` cannot distinguish an absent `member_channels` from a literal `[]`, and those are opposite answers (absent = no information, preserve; empty = everyone queried is a non-member, clear). Putting a bare slice in the interface would push a heuristic into the adapter, which is exactly where the bug would live.

   Use `edge`'s `MembershipQueried` — the ids sent in batches whose response actually carried `member_channels` — as the queried set. That removes the guesswork entirely:

   ```go
   if len(res.MembershipQueried) > 0 {
       err := deps.Store.ApplyMembership(deps.WorkspaceID, res.MembershipQueried,
           cache.MembershipReported(res.MemberChannels, res.FailedIDs))
       // ...
   }
   ```

   When no batch reported, make no call at all. Note `res.FailedIDs` accumulates across *all* batches while `MembershipQueried` covers only reporting ones; that is harmless, since `ApplyMembership` touches nothing outside the queried set.

   Do **not** pass `res.MemberChannels` as the queried set — that silently clears membership for everything not returned.
5. Log `res.FailedIDs` and skip them. Do not advance their versions.
6. Build `userIDs` from `out.Users` plus `out.IMs[].UserID` (the field is `UserID`, tagged `json:"user"` — not `User`). Same version lookup via `UserVersions`.
7. Call `UsersInfo`, then `UpdateUserFromEdge` per result.

Extend `Store` with `UpdateChannelFromEdge(cache.EdgeChannelUpdate) error` and `UpdateUserFromEdge(cache.EdgeUserUpdate) error`. This makes `internal/bootstrap` import `internal/cache`, which is fine — the dependency runs inward and `cache` imports neither `bootstrap` nor `slack`.

Call `revalidate(ctx, deps, out, logf)` at the end of `Run`.

- [ ] **Step 4: Verify**

Run: `go test ./internal/bootstrap/ -race -v`
Expected: PASS, including `TestRun_BootCallBudget` — revalidation adds at most a handful of batched calls and the budget is 10.

- [ ] **Step 5: Mutation-test**

Literal output each:

1. Send every cached channel id instead of the sidebar set → the "not the whole cache" test must fail.
2. Send every cached user id → `DoesNotSendEveryCachedUser` must fail.
3. Pass `res.MemberChannels` as `ApplyMembership`'s `queriedIDs` → the queried-set test must fail. This is the bug that silently drops membership for everything not returned.
4. Advance versions for `FailedIDs` → the failed-id test must fail.
5. Make a revalidation error fatal → the non-fatal test must fail.
6. Use `UpsertChannel` instead of `UpdateChannelFromEdge` → a Task 2 preserve test must fail. If it does not, Task 2's tests are decorative — go back and fix them.

- [ ] **Step 6: Commit**

```bash
git add internal/bootstrap/
git commit -m "feat(bootstrap): revalidate the cache instead of enumerating it

Sends {id: version} for the conversations the sidebar renders and the
users the opened channel references, and gets back only what changed.
Replaces the ~50-page users.list sweep. The id sets are scoped
deliberately: a fixed batch over an unbounded set emits a run of
identically-sized requests, which is a cleaner signature than the
client's own ragged distribution."
```

---

## Task 7: Wire `connectWorkspace` onto `bootstrap.Run`

**Files:**
- Modify: `cmd/slk/main.go` (`connectWorkspace`, from line 1889)
- Create: `cmd/slk/bootstrap_adapters.go`, `cmd/slk/bootstrap_adapters_test.go`

The adapters are thin: each wraps one `*slackclient.Client` or `*cache.DB` method to satisfy a `bootstrap` interface. Keeping them in their own file keeps `main.go` from growing while this lands.

- [ ] **Step 1: Write the failing test**

Create `cmd/slk/bootstrap_adapters_test.go`. Test the adapters directly against an `httptest` server, following the existing patterns in `cmd/slk/*_test.go`. Cover at minimum:

- the `UserBooter` adapter calls `client.userBoot` and returns a parsed `*boot.Result`
- the `Historian` adapter passes `CachedVersions` through and returns `UnchangedTS`
- the `Store` adapter's `MessageVersions` supplies a bounded window rather than the whole channel — assert the `oldestTS`/`latestTS` it passes to `cache.MessageVersions` are not `""`/`"9"`, because an unbounded window puts an arbitrarily large map into a request body

- [ ] **Step 2: Confirm red, then implement**

Create `cmd/slk/bootstrap_adapters.go` with one small type per interface. Example shape:

```go
// bootAdapter satisfies bootstrap.UserBooter. boot.UserBoot takes a
// PostFunc with exactly (*slackclient.Client).PostForm's signature, so
// this is a pass-through rather than a translation layer.
type bootAdapter struct{ c *slackclient.Client }

func (a bootAdapter) UserBoot(ctx context.Context) (*boot.Result, error) {
	return boot.UserBoot(ctx, a.c.PostFormForBoot)
}
```

`postForm` is currently unexported. Export a thin wrapper on `slackclient.Client` (`PostFormForBoot`, or rename `postForm` and keep an unexported alias) rather than moving `bootstrap` into `internal/slack`. Do not change `postForm`'s behaviour.

- [ ] **Step 3: Rewire `connectWorkspace`**

Replace the inline boot work with:

```go
	res, err := bootstrap.Run(ctx, bootstrap.Deps{
		WorkspaceID:   client.TeamID(),
		Boot:          bootAdapter{client},
		Counts:        countsAdapter{client},
		View:          viewAdapter{client},
		History:       historyAdapter{client},
		Revalidate:    edge.New(token.AccessToken, client.TeamID(), client.HTTPClient()),
		Store:         storeAdapter{db, client.TeamID()},
		OpenChannelID: restoredChannelID,
		Log:           debuglog.General,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrapping %s: %w", token.TeamName, err)
	}
```

then wire `res` into the existing structures: `wctx.UserNames` from `res.Users`, muted channels from `slack.ParseMutedFromAllNotificationsPrefs(res.MutePrefsRaw)` merged with `res.LegacyMutedRaw`, channels and IMs into the cache via the **existing full** `UpsertChannel`/`UpsertUser` (these are first-sight hydration, which legitimately owns every column — the partial writers are only for revalidation).

`edge.New` needs the browser-shaped `*http.Client`. Check what `slackclient.Client` exposes; if there is no accessor, add a narrow one. It **must** be the client built with `slackhttp.BrowserTransport` — a plain client would send edgeapi requests with no browser headers and no envelope, which is the divergence this whole project removes.

**Leave `client.GetUsers`, `GetChannels` and `triggerBackfill` in place for now.** They are deleted in Tasks 8-9, after this path is proven. This task must leave slk working.

- [ ] **Step 4: Verify by hand**

```bash
go build ./... && SLK_DEBUG=1 ./slk 2>&1 | tail -40
```

Confirm slk boots, the sidebar populates, and the restored channel opens. Record the counter's report. **Paste the actual output** — this is the first task whose correctness cannot be established by unit tests alone.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/bootstrap_adapters.go cmd/slk/bootstrap_adapters_test.go cmd/slk/main.go internal/slack/client.go
git commit -m "feat(slk): boot through internal/bootstrap

connectWorkspace reduces to client construction, bootstrap.Run, and UI
wiring. The old enumeration paths still run alongside; they are deleted
in the next two tasks, once this one is proven."
```

---

## Task 8: Delete the `users.list` sweep

**Files:**
- Modify: `cmd/slk/main.go` (the background user fetch at ~line 2077), `internal/slack/client.go`

`users.list` is ~50 paginated pages on a 10k-user workspace and appears **zero** times across all 8 captures. It is the clearest single "scraping" signal slk emits.

Users now arrive from: `conversations.view`'s `users` (the opened channel's authors), `edge.UsersInfo` revalidation of the cache, and `resolveUser` on demand for misses. The mention picker moves to server-side search in Task 11.

- [ ] **Step 1: Write the failing test**

Add to `internal/bootstrap/bootstrap_test.go` — or wherever the counter is reachable — a test asserting a boot records **zero** `users.list` calls in a `slackhttp.Counter`. If the wiring makes that awkward from `cmd/slk`, assert it in `bootstrap` via the fake, extending `TestRun_NeverEnumerates`. Either is acceptable; say which you chose.

- [ ] **Step 2: Delete**

Remove the background goroutine at `cmd/slk/main.go:2077` that calls `client.GetUsers(ctx)` and fills `wctx.UserNames` / `wctx.UserNamesByHandle` / `wctx.BotUserIDs`. Those maps must still be populated — from `res.Users` in Task 7's wiring, and from the cache via the existing `db.ListUsers(wctx.TeamID)` load.

Delete `func (c *Client) GetUsers` from `internal/slack/client.go` and any test that covers only it.

**Check every reader of `wctx.UserNames` still works with a partially-populated map.** `resolveUser` (`main.go:1793`) already handles misses; verify it is reached on the paths that previously relied on the sweep having finished.

- [ ] **Step 3: Verify**

```bash
go build ./... && go test ./... -race
SLK_DEBUG=1 ./slk 2>&1 | grep -i 'users.list'   # expect no matches
```

Open a channel with messages from people not in your DM list; confirm names resolve rather than showing raw user IDs. **Paste the counter report and note the delta from Task 7.**

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go internal/slack/client.go
git commit -m "feat(slk): stop sweeping users.list at boot

~50 paginated pages on a 10k-user workspace, and zero occurrences
across 8 captures of the official client. Users now arrive from
conversations.view, edge revalidation, and on-demand resolution."
```

---

## Task 9: Delete `triggerBackfill` and bound the reconnect path

**Files:**
- Modify: `cmd/slk/main.go` (method at ~3786, call sites at ~3756 and ~1823), `cmd/slk/reconnect_backfill.go`

**The single largest fingerprint change in the project.** `triggerBackfill` fetches `conversations.history` for every channel ever visited, and it runs from `OnConnect` — which fires on first connect *and every reconnect*. Every laptop sleep, wifi change and VPN flap replays the whole sweep. That is the scraper signature several times a day, not once at boot.

The comment at `main.go:3752` calls the boot case "harmless — most `GetHistorySince` calls return zero messages quickly." True for bytes, false for request count, and request count is what anomaly detection scores.

Observed: after 90 seconds fully offline the official client issued **zero** HTTP recovery calls. slk will do slightly more, because it cannot yet prove it gets the same WebSocket replay.

- [ ] **Step 1: Write the failing test**

Add to `cmd/slk/reconnect_backfill_test.go` (or a new file in that package) a test asserting the reconnect handler issues a **constant** number of API calls regardless of how many channels have been visited. Drive it with 3 visited channels and then 300, and assert the count is identical and small. Name it `TestReconnect_IsO1NotOChannels`.

This is success criterion 2 and it is the whole point of the task. Use the `slackhttp.Counter` from Task 1 if the handler is reachable with one; otherwise a fake client that records calls.

- [ ] **Step 2: Delete and replace**

- Remove `h.triggerBackfill("reconnect")` from `OnConnect` (`main.go:3756`).
- Remove `wctx.RTMHandler.triggerBackfill("wake")` from the wake-from-sleep detector (`main.go:1823`).
- Delete `func (h *rtmEventHandler) triggerBackfill` (`main.go:3786`).
- In `cmd/slk/reconnect_backfill.go`, `BackfillCandidates` ceases to be a runtime path. Delete the method and its call site if nothing else uses them; if the query is still useful for a future lazy path, keep the cache method and delete only the fan-out driver. Say which you did and why.

Replace `OnConnect`'s behaviour with the bounded handler:

1. `client.counts` — one call, refreshes unread state for everything.
2. The **active channel only**, via the normal open path with `cached_latest_updates`.
3. Mark every other channel stale so it revalidates on next open.

### MEASURED 2026-08-01 — the offline check is done, and it changed the design

Run on a real two-workspace setup (105 and 39 channels): slk started, wifi
dropped, a message posted and marked unread from another client, wifi restored.
`slk-debug.log` captured. Three findings, all of which move this task.

**1. The WebSocket does NOT replay missed messages. `client.counts` stays.**

After the reconnect `hello` at 06:49:05, the socket delivered ~160
`presence_change` events and *nothing else*. The only `message` event in the
whole session arrived at 06:49:53 with an event ts of its own moment — a live
post after reconnect, not a replay. No `channel_marked` replayed either, so the
mark-as-unread was lost too.

The spec inferred from slk's socket params (`sync_desync=1`, `ms_latest=true`,
`flannel=3`, `lazy_channels=1`) that slk "strongly suggests it gets the same
missed-event delivery" as the official client. **That inference is wrong.** Keep
the `client.counts` call; do not drop it.

**2. slk never refreshes `client.counts` on reconnect today — this task fixes a
live bug, not just a fingerprint.**

`OnConnect` (`main.go:3721`) does presence/DND, a section rebootstrap, the
backfill and a membership refresh. It does **not** call `client.counts`, which
is boot-only. That is precisely why the unread never appeared: the count is
stale and the socket did not replay the event. The bounded handler this task
introduces is a user-visible fix.

**3. The backfill is more expensive and less useful than the spec said, and the
cost is not where the plan put it.**

Measured over one ~3-minute session:

```
per-channel conversations.history calls:  288
  returned 0 messages:                    250  (86%)
  returned >0:                             38

reconnect sweep, T04T4TH8W (105 channels):
  channel-phase       total_msgs=0     dur_ms=2711      <- 2.7s, found NOTHING
  subscription-phase  subs=1000        dur_ms=132248    <- 132s
  total_dur_ms=132249

  ListThreadSubscriptions: hit hard cap 1000, stopping   (x4 across the session)
```

Four reconnect sweeps ran (two workspaces x initial + reconnect), totalling
**~6 minutes** of backfill for a 90-second outage that produced no new messages.

The spec's rationalisation — "most `GetHistorySince` calls return zero messages
quickly" — is confirmed at 86%, and its conclusion is confirmed wrong: the
request count is the cost.

**But the channel backfill is not the expensive part. The thread-subscription
phase is, by 50x** (132s vs 2.7s), and it hits a 1000-item hard cap every time.
The plan treats deferring `subscriptions.thread.getView` as a minor Task 10
cleanup. It is the single most expensive thing slk does on reconnect.

**Consequence for this task:** the subscription phase must be removed from the
reconnect path *here*, alongside `triggerBackfill`, not deferred to Task 10.
Task 10 keeps only the boot-time deferral. Add a test asserting the reconnect
handler triggers no thread-subscription enumeration.

- [ ] **Step 3 (superseded — the check above is done; keep this for the method)**

The spec leaves one question open: whether slk needs step 1 at all. The official client does zero.

Run it:

```bash
SLK_DEBUG=1 ./slk
# ... let it settle, note the message counts in two busy channels
# drop the network for 90s (e.g. `nmcli networking off`), have someone
# post in those channels, then restore
```

Then check whether the messages posted during the outage arrive **over the WebSocket**, with no HTTP fetch, by grepping the counter report and the debug log for `conversations.history`.

**Record the result in the plan's outcomes doc.** If the socket delivers them, `client.counts` can be dropped and slk matches the official client exactly. If it does not, the bounded handler is load-bearing and must stay. Do not guess — if you cannot run this, report BLOCKED for this step and leave the `client.counts` call in.

- [ ] **Step 4: Verify**

```bash
go build ./... && go test ./... -race
```

Then sleep the laptop (or toggle the network), wake it, and confirm from the counter report that reconnect issues a small constant number of calls. **Paste the before/after counts.**

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/reconnect_backfill.go cmd/slk/reconnect_backfill_test.go
git commit -m "feat(slk): delete triggerBackfill, bound the reconnect path

It fetched conversations.history for every channel ever visited, from
OnConnect -- so every laptop sleep, wifi change and VPN flap replayed
the whole sweep. Reconnect is now O(1): client.counts plus the active
channel, with everything else marked stale for lazy revalidation."
```

---

## Task 10: Defer boot-time `subscriptions.thread.getView`

**Files:**
- Modify: `cmd/slk/main.go`, `internal/slack/client.go`

Two small cleanups that need no new tests beyond the existing suite staying green.

- [ ] **Step 1:** Move the boot-time `subscriptions.thread.getView` call to the first open of the Threads view. Find it via `rg -n 'subscriptions.thread.getView' cmd/ internal/`. Confirm the Threads view still populates on first open and that reopening does not refetch on every keystroke.

- [ ] **Step 2:** *(moved to Task 11)* An earlier revision of this plan said to delete a dead `conversations.list` wrapper here. **There is no dead wrapper.** The real function is `GetAllPublicChannels` (`client.go:497`), it is live, and it is deleted in Task 11 alongside the finder move that replaces it. See the correction at the top of the design doc.

- [ ] **Step 3: Verify and commit**

```bash
go build ./... && go test ./... -race && golangci-lint run ./...
git add cmd/slk/main.go internal/slack/client.go
git commit -m "feat(slk): defer thread subscriptions to first Threads-view open"
```

---

## Task 11: Channel finder and mention picker on server-side search

**Files:**
- Modify: the channel finder and `internal/ui/compose/model.go`

Both become **local cache first, debounced server query on top**. Local matches render immediately so typing stays responsive; server results merge on arrival.

The capture shows two `channels/search` requests for a four-second typing session — roughly one per input pause, never one per keystroke. A finder that fired per keystroke would be a worse fingerprint than the enumeration it replaces.

- [ ] **Step 1: Write the failing test**

Test the debounce directly, with a fake clock or an injected timer — not `time.Sleep`, which makes the suite slow and flaky. Assert:

- typing `t`, `te`, `tes`, `test` within the debounce window issues **one** request, carrying the final query
- a pause longer than the window issues a second
- an empty query issues **none** (`edge.ChannelsSearch` already returns early, but the caller must not queue one either)
- local cache results are returned before any request completes

- [ ] **Step 2: Implement**

- Channel finder → `edge.ChannelsSearch(ctx, query, topChannels)`, debounced ~300 ms, `topChannels` from `internal/cache/frecent.go`.

  **Then delete the enumeration it replaces — in this task, not before it:**
  `fetchBrowseableChannels` (`cmd/slk/main.go:2238`), its `go` spawn at
  `main.go:1787`, and `Client.GetAllPublicChannels` (`internal/slack/client.go:497`).
  That is the live `conversations.list` walk — 4 calls at `Limit: 1000` per page
  on a measured two-workspace boot — and its only job is showing the finder
  channels the user has not joined. `ChannelsSearch` returns exactly those,
  server-side, so the capability survives. Deleting it any earlier would drop
  unjoined channels from the finder with nothing in their place.

  Add a test asserting a finder query issues **zero** `conversations.list`.
- Mention picker → existing per-channel membership first, then `edge.UsersSearch(ctx, query, currentChannel, topUsers)` debounced, with `currentChannel` set to the open channel.

Cancel the in-flight request when a newer query supersedes it — `edge` already propagates `ctx`, and Phase 2a pinned that with a test, so cancellation works if you pass a per-query context.

- [ ] **Step 3: Verify**

```bash
go build ./... && go test ./... -race
SLK_DEBUG=1 ./slk   # open the finder, type "test" briskly
```

Confirm from the counter that a four-character burst produced one or two `edge:channels/search` calls, not four. **Paste the count.**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(ui): search channels and users server-side, debounced

Local cache matches first for instant feedback; one request per input
pause on top. The official client issues two requests for a four-second
typing session, never one per keystroke."
```

---

## Task 12: Full verification

- [ ] **Step 1: Gate**

```bash
go build ./... && go vet ./... && go test ./... -race && golangci-lint run ./...
```

All must pass.

- [ ] **Step 2: No test touches the network**

```bash
unshare -rn sh -c 'ip link set lo up && go test ./...'
```

Expected: all ok. **Note the `ip link set lo up`** — bare `unshare -rn` leaves loopback DOWN, so every `httptest`-based test fails to dial and the check cannot distinguish a real network call from a down interface. The Phase 2a plan got this wrong.

- [ ] **Step 3: Measure the success criteria**

Boot slk against a real workspace with `SLK_DEBUG=1` and record the counter report.

Assert, from the report:
1. Total API calls at boot **≤ 10**.
2. `users.list` — **zero**.
3. `conversations.list` — **zero**.
4. `conversations.history` — **at most one** (the opened channel, and only if the `conversations.view` probe failed).
5. Reconnect issues a small constant number, unchanged between 3 visited channels and 300.

Then do the same on a **fresh profile** (`--add-workspace` into a temp config/cache dir) and confirm cold and warm boot land within a few calls of each other, as the official client does.

**Paste both reports.** These numbers are the PR's central claim and the only evidence available until someone risks a Grid account.

- [ ] **Step 4: Confirm nothing regressed by hand**

Messages render with real names, unread badges are right, muted channels stay muted, starred channels stay starred, avatars still load, the finder and mention picker work. The starred/avatar checks matter specifically: they are what a cache-mapping bug (Task 2) would break, and it would be silent.

- [ ] **Step 5: Write the outcomes doc**

Create `docs/superpowers/plans/2026-07-31-grid-parity-phase2b-outcomes.md` recording: measured before/after call counts, the 90-second offline result from Task 9 Step 3, anything the captures contradicted, and what remains open. Follow the structure of the Phase 2a outcomes doc.

**Lead with whether a Grid tester should now try it.** Two contributors have already been signed out helping diagnose this; that judgement belongs at the top of the document, not the bottom.

- [ ] **Step 6: Commit and push**

```bash
git add docs/superpowers/plans/2026-07-31-grid-parity-phase2b-outcomes.md
git commit -m "docs: record Phase 2b outcomes and measured call counts"
git push origin feat/grid-parity
```

---

## Self-Review Notes

**Spec coverage.** Design §Architecture → Task 4. §Boot sequence → Tasks 4-6. §Deletions → Tasks 8-10. §Reconnect → Task 9. §Finder and mentions → Task 11. §Cache column mapping → Tasks 2-3. §Verification → Tasks 1 and 12. Every design section has a task.

**Ordering is deliberate.** Instrumentation (1) precedes everything so there is a baseline. Cache writers (2-3) precede revalidation (6) because revalidation calls them. The wiring (7) precedes every deletion (8-10), so no intermediate commit leaves slk without a working boot — Task 7 explicitly leaves the old paths running.

**Known weaknesses:**

- **Tasks 6, 8, 9 and 11 specify tests in prose rather than complete code.** Their fixtures depend on `Result` shapes that only exist once Tasks 4-5 land, and on UI internals this plan should not freeze. Those four need more judgement than the rest; the mutation lists are written to compensate, since they name the specific bugs each test must catch.
- **Task 7 Step 4, Task 9 Step 3, and Task 12 Steps 3-4 cannot be verified by unit tests.** They need a real workspace. If you cannot run slk against one, report BLOCKED rather than marking them done — an unmeasured call count is exactly the kind of unsubstantiated claim this project has been burned by.
- **Task 9 Step 3 may change the design.** If the WebSocket delivers the missed messages, `client.counts` should be dropped from the reconnect path and slk matches the official client exactly. That is a plan-modifying result, and it should be reported, not quietly absorbed.

**Deliberately unresolved:** the `conversations.view` `channel` param (Task 5) is probed, not assumed, and the fallback is likely to be the *common* path on Grid where no capture exists — which is why Task 5 tests the fallback as heavily as the primary.
