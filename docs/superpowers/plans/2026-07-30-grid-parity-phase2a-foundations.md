# Grid Parity Phase 2a: Bootstrap Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and test the three components the new bootstrap needs — cache version tracking, an edgeapi client, and a `client.userBoot`/`conversations.view` parser — without changing any runtime behaviour.

**Architecture:** Three independent units. `internal/cache` gains version columns and query helpers so slk can participate in Slack's conditional-revalidation protocol. `internal/slack/edge` is a new package wrapping `edgeapi.slack.com`, which is a genuinely different protocol from the workspace API (JSON body, `text/plain` content type, different host, `updated_ids` semantics). `internal/slack/boot` parses the two combined boot endpoints into one struct. Nothing calls any of it until Phase 2b.

**Tech Stack:** Go, SQLite (`internal/cache`), standard `net/http`, standard `testing`.

---

## Read First

You have **no context** from the sessions that produced this plan. Read these, in order:

1. `docs/superpowers/specs/2026-07-30-enterprise-grid-bootstrap-design.md` — the design. Layer 2 is what this plan implements.
2. `internal/slack/testdata/phase2-api-contracts.json` — **the committed evidence base.** Verbatim request params (tokens redacted) and response shapes for all 12 endpoints, plus the ordered boot sequence from two captures. Start here; do not guess at contracts it covers.

3. **The raw HAR captures at `/tmp/*.har`** — 8 files, ~270 MB, from the machine that recorded them. These are the ground truth behind every claim in this plan and the spec, and you can query them directly to derive facts the committed fixtures don't cover.

   **They must never be committed.** They contain live `xoxc`/`xoxd` credentials and real message content. Anything you extract must be sanitized (redact `xoxc-…`/`xoxd-…`, summarize response bodies to types/shapes rather than values) before it goes in `testdata/`. `/tmp/opencode/phase2_fixtures.py` is the script that produced the committed contracts file and shows the pattern, including its assert-no-token-leak check.

   Helper scripts, all taking HAR paths as arguments: `/tmp/opencode/har_analyze.py` (request counts by kind/host, concurrency, per-second rate), `har_detail.py` (splits static bundles from data/asset fetches), `har_endpoint.py <har> <url-substring>` (request params, headers, response shape for one endpoint).

   If a claim in this plan disagrees with the captures, **the captures win** — say so and correct the plan.
4. `internal/slackhttp/testdata/capture-evidence.json` — aggregate measurements from Phase 1.
5. `docs/superpowers/plans/2026-07-30-grid-parity-phase1-outcomes.md` — what Phase 1 shipped and its known gaps.

## Why This Is Split From Phase 2b

Phase 2 as designed is: build three new components, then rewire `connectWorkspace` onto them and delete the old path. That is too large for one plan, and the two halves have different risk profiles.

**Phase 2a (this plan)** is purely additive. Every task adds code that nothing calls. It cannot regress runtime behaviour, and it is independently mergeable and reviewable.

**Phase 2b (separate plan, written after 2a lands)** flips the switch: rewires `connectWorkspace`, deletes `triggerBackfill` from all call sites, moves the channel finder onto server-side search. That plan should be written *against 2a's real interfaces* rather than speculated now.

## Background: what problem Layer 2 solves

Slack signs Enterprise Grid users out with a notice citing "data scraping." Across 8 captures of the official web client there are **zero** `users.list` calls, **zero** `conversations.list` calls, and **zero** per-channel `conversations.history` at boot. slk does all three on every start:

- `users.list` — ~50 paginated pages on a 10k-user workspace
- `conversations.list` — every public channel including unjoined
- `conversations.history` for **every channel ever visited**, plus `conversations.replies` per thread found

The official client instead keeps a local cache and revalidates it conditionally:

```
POST edgeapi.slack.com/cache/T…/channels/info
{"check_membership":true,"updated_ids":{"CL0AET1L0":1783337533019, …}}
→ 290 bytes, results=0            (nothing changed)
```

Hundreds of channels validated in one sub-KB round trip. That is the mechanism this plan makes possible.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/cache/db.go` (modify) | Add three version columns via the existing `addColumnIfMissing` pattern |
| `internal/cache/versions.go` (create) | Version read/write helpers for channels, users, messages |
| `internal/cache/versions_test.go` (create) | Version helper tests |
| `internal/slack/edge/client.go` (create) | edgeapi transport: JSON body, `text/plain` content type, shared `*http.Client` |
| `internal/slack/edge/cache.go` (create) | `ChannelsInfo`, `UsersInfo` — conditional revalidation |
| `internal/slack/edge/search.go` (create) | `ChannelsSearch`, `UsersSearch` |
| `internal/slack/edge/members.go` (create) | `UsersList`, `ChannelsMembership`, `UsersCounts` |
| `internal/slack/edge/*_test.go` (create) | Per-file tests, fixture-driven |
| `internal/slack/boot/boot.go` (create) | `client.userBoot` request + response types |
| `internal/slack/boot/view.go` (create) | `conversations.view` request + response types |
| `internal/slack/boot/*_test.go` (create) | Fixture-driven parser tests |

**Do not modify** `cmd/slk/main.go`, `internal/slack/client.go`'s existing methods, or `cmd/slk/reconnect_backfill.go` in this plan. Those belong to Phase 2b.

---

## Task 1: Cache version columns

**Files:**
- Modify: `internal/cache/db.go` (the idempotent column-migration block, around lines 205-235)
- Test: `internal/cache/db_test.go`

Conditional revalidation requires per-row version stamps slk does not store. Observed formats: channels use millisecond ints (`1783337533019`), users second ints (`1612802061`), messages an opaque string from `latest_updates`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cache/db_test.go`:

```go
func TestMigrate_AddsVersionColumns(t *testing.T) {
	db := newTestDB(t) // use whatever helper this file already uses
	for _, tc := range []struct{ table, column string }{
		{"channels", "version"},
		{"users", "version"},
		{"messages", "version"},
	} {
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			var count int
			err := db.conn.QueryRow(
				`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
				tc.table, tc.column,
			).Scan(&count)
			if err != nil {
				t.Fatalf("pragma_table_info(%s): %v", tc.table, err)
			}
			if count != 1 {
				t.Errorf("column %s.%s missing after migrate()", tc.table, tc.column)
			}
		})
	}
}

func TestMigrate_VersionColumnsAreIdempotent(t *testing.T) {
	// migrate() runs on every Open. A second run must not error.
	db := newTestDB(t)
	if err := db.migrate(); err != nil {
		t.Fatalf("second migrate(): %v", err)
	}
}
```

Check the top of `internal/cache/db_test.go` for the actual test-DB helper name and use it; do not invent one.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/cache/ -run TestMigrate_AddsVersionColumns -v`
Expected: FAIL — `column channels.version missing after migrate()` (×3).

- [ ] **Step 3: Implement**

In `internal/cache/db.go`, in the idempotent column-migration block alongside the existing `addColumnIfMissing` calls, add:

```go
	// Version stamps for Slack's conditional cache-revalidation
	// protocol (edgeapi updated_ids). channels.version is a
	// millisecond int, users.version a second int, messages.version an
	// opaque string from conversations.history's latest_updates. Zero
	// or empty means "never seen", which is what we send to ask for a
	// full record.
	if err := db.addColumnIfMissing("channels", "version",
		"ALTER TABLE channels ADD COLUMN version INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("users", "version",
		"ALTER TABLE users ADD COLUMN version INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("messages", "version",
		"ALTER TABLE messages ADD COLUMN version TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
```

- [ ] **Step 4: Verify**

Run: `go test ./internal/cache/ -v`
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add internal/cache/db.go internal/cache/db_test.go
git commit -m "feat(cache): add version columns for conditional revalidation"
```

---

## Task 2: Version query helpers

**Files:**
- Create: `internal/cache/versions.go`, `internal/cache/versions_test.go`

The edgeapi client needs `{id: version}` maps to send as `updated_ids`, and a way to write versions back.

- [ ] **Step 1: Write the failing test**

Create `internal/cache/versions_test.go`:

```go
package cache

import "testing"

func TestChannelVersions_ReturnsZeroForUnknown(t *testing.T) {
	db := newTestDB(t)
	mustUpsertWorkspace(t, db, "T1")

	// A channel row with no version yet must appear with version 0 —
	// that is how we ask Slack for the full record.
	mustUpsertChannel(t, db, "T1", "C1")

	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if v, ok := got["C1"]; !ok || v != 0 {
		t.Errorf("ChannelVersions()[C1] = %v, ok=%v; want 0, true", v, ok)
	}
}

func TestChannelVersions_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	mustUpsertWorkspace(t, db, "T1")
	mustUpsertChannel(t, db, "T1", "C1")

	if err := db.SetChannelVersion("C1", 1783337533019); err != nil {
		t.Fatalf("SetChannelVersion: %v", err)
	}
	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if got["C1"] != 1783337533019 {
		t.Errorf("version = %d; want 1783337533019", got["C1"])
	}
}

func TestChannelVersions_ScopedToWorkspace(t *testing.T) {
	db := newTestDB(t)
	mustUpsertWorkspace(t, db, "T1")
	mustUpsertWorkspace(t, db, "T2")
	mustUpsertChannel(t, db, "T1", "C1")
	mustUpsertChannel(t, db, "T2", "C2")

	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if _, leaked := got["C2"]; leaked {
		t.Error("ChannelVersions leaked a channel from another workspace")
	}
}

func TestUserVersions_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	mustUpsertWorkspace(t, db, "T1")
	mustUpsertUser(t, db, "T1", "U1")

	if err := db.SetUserVersion("U1", 1612802061); err != nil {
		t.Fatalf("SetUserVersion: %v", err)
	}
	got, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if got["U1"] != 1612802061 {
		t.Errorf("version = %d; want 1612802061", got["U1"])
	}
}

func TestMessageVersions_RoundTripAndScope(t *testing.T) {
	db := newTestDB(t)
	mustUpsertWorkspace(t, db, "T1")
	mustUpsertChannel(t, db, "T1", "C1")
	mustInsertMessage(t, db, "T1", "C1", "1700000001.000100")
	mustInsertMessage(t, db, "T1", "C1", "1700000002.000200")

	if err := db.SetMessageVersion("C1", "1700000001.000100", "1783024685.163100"); err != nil {
		t.Fatalf("SetMessageVersion: %v", err)
	}
	got, err := db.MessageVersions("C1", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions: %v", err)
	}
	if got["1700000001.000100"] != "1783024685.163100" {
		t.Errorf("version = %q; want 1783024685.163100", got["1700000001.000100"])
	}
	// Messages with no version must be omitted, not sent as empty —
	// cached_latest_updates only carries messages we can vouch for.
	if _, present := got["1700000002.000200"]; present {
		t.Error("MessageVersions included a message with no version")
	}
}
```

`newTestDB`, `mustUpsertWorkspace`, `mustUpsertChannel`, `mustUpsertUser`, `mustInsertMessage` — **check which of these already exist** in `internal/cache/*_test.go`. Reuse the existing ones. Only write helpers that are genuinely missing, and put new ones in `versions_test.go`.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/cache/ -run 'TestChannelVersions|TestUserVersions|TestMessageVersions' -v`
Expected: FAIL — `db.ChannelVersions undefined`.

- [ ] **Step 3: Implement**

Create `internal/cache/versions.go`:

```go
package cache

import "fmt"

// ChannelVersions returns {channelID: version} for every cached channel
// in the workspace, for use as edgeapi's updated_ids. A channel with no
// recorded version appears with 0, which is how the official client
// asks for a full record.
func (db *DB) ChannelVersions(workspaceID string) (map[string]int64, error) {
	rows, err := db.conn.Query(
		`SELECT id, version FROM channels WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing channel versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64, 256)
	for rows.Next() {
		var id string
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("scanning channel version: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// SetChannelVersion records the version stamp Slack returned for a
// channel.
func (db *DB) SetChannelVersion(channelID string, version int64) error {
	_, err := db.conn.Exec(
		`UPDATE channels SET version = ? WHERE id = ?`, version, channelID)
	if err != nil {
		return fmt.Errorf("setting channel version: %w", err)
	}
	return nil
}

// UserVersions returns {userID: version} for every cached user in the
// workspace. Same semantics as ChannelVersions.
func (db *DB) UserVersions(workspaceID string) (map[string]int64, error) {
	rows, err := db.conn.Query(
		`SELECT id, version FROM users WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing user versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64, 1024)
	for rows.Next() {
		var id string
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("scanning user version: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// SetUserVersion records the version stamp Slack returned for a user.
func (db *DB) SetUserVersion(userID string, version int64) error {
	_, err := db.conn.Exec(
		`UPDATE users SET version = ? WHERE id = ?`, version, userID)
	if err != nil {
		return fmt.Errorf("setting user version: %w", err)
	}
	return nil
}

// MessageVersions returns {ts: version} for cached messages in the
// channel within [oldestTS, latestTS], for use as
// conversations.history's cached_latest_updates. Messages with no
// recorded version are omitted: that parameter is an assertion about
// what we hold, and we can only vouch for messages Slack has
// versioned.
func (db *DB) MessageVersions(channelID, oldestTS, latestTS string) (map[string]string, error) {
	rows, err := db.conn.Query(
		`SELECT ts, version FROM messages
		 WHERE channel_id = ? AND ts >= ? AND ts <= ? AND version != ''`,
		channelID, oldestTS, latestTS)
	if err != nil {
		return nil, fmt.Errorf("listing message versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, 64)
	for rows.Next() {
		var ts, v string
		if err := rows.Scan(&ts, &v); err != nil {
			return nil, fmt.Errorf("scanning message version: %w", err)
		}
		out[ts] = v
	}
	return out, rows.Err()
}

// SetMessageVersion records the version stamp from
// conversations.history's latest_updates.
func (db *DB) SetMessageVersion(channelID, ts, version string) error {
	_, err := db.conn.Exec(
		`UPDATE messages SET version = ? WHERE channel_id = ? AND ts = ?`,
		version, channelID, ts)
	if err != nil {
		return fmt.Errorf("setting message version: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Verify**

Run: `go test ./internal/cache/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cache/versions.go internal/cache/versions_test.go
git commit -m "feat(cache): add version query helpers for updated_ids"
```

---

## Task 3: edgeapi client skeleton

**Files:**
- Create: `internal/slack/edge/client.go`, `internal/slack/edge/client_test.go`

edgeapi is a different protocol from the workspace API: JSON request bodies with `Content-Type: text/plain;charset=UTF-8` (a CORS simple-request trick), a different host, and no `_x_id`/`slack_route`/`_x_version_ts` — Phase 1's `BrowserTransport` already knows this and applies the smaller edgeapi envelope automatically. That transport is the *only* thing that should decorate requests; this package must not add headers itself.

Contract, from `internal/slack/testdata/phase2-api-contracts.json`:
- URL: `https://edgeapi.slack.com/cache/<teamID>/<endpoint>`
- Method: POST
- Content-Type: `text/plain;charset=UTF-8`
- Body: JSON, always including `"token": "<xoxc>"`

- [ ] **Step 1: Write the failing test**

Create `internal/slack/edge/client_test.go`:

```go
package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_PostsJSONAsTextPlain(t *testing.T) {
	var gotCT, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T04T4TH8W", srv.Client())
	c.baseURL = srv.URL

	var out struct {
		OK bool `json:"ok"`
	}
	err := c.call(context.Background(), "users/info",
		map[string]any{"check_interaction": true}, &out)
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	// edgeapi takes JSON with a text/plain content type — this is how
	// the official client avoids a CORS preflight, and matching it is
	// the point of this package.
	if gotCT != "text/plain;charset=UTF-8" {
		t.Errorf("Content-Type = %q; want text/plain;charset=UTF-8", gotCT)
	}
	if gotPath != "/cache/T04T4TH8W/users/info" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/users/info", gotPath)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, gotBody)
	}
	if body["token"] != "xoxc-test" {
		t.Errorf("body token = %v; want xoxc-test", body["token"])
	}
	if body["check_interaction"] != true {
		t.Errorf("caller payload not merged: %v", body)
	}
	if !out.OK {
		t.Error("response not decoded")
	}
}

func TestClient_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	var out struct{}
	if err := c.call(context.Background(), "users/info", map[string]any{}, &out); err == nil {
		t.Error("call returned nil error on HTTP 500")
	}
}

func TestClient_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	var out struct{}
	err := c.call(context.Background(), "users/info", map[string]any{}, &out)
	if err == nil {
		t.Fatal("call returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %v; want it to mention invalid_auth", err)
	}
}

func TestClient_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out struct{}
	if err := c.call(ctx, "users/info", map[string]any{}, &out); err == nil {
		t.Error("call ignored a cancelled context")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/slack/edge/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Implement**

Create `internal/slack/edge/client.go`:

```go
// Package edge wraps edgeapi.slack.com, Slack's edge cache API.
//
// This is a different protocol from the workspace Web API and is
// deliberately a separate package rather than more surface on
// slack.Client: JSON request bodies with a text/plain content type
// (the official client's way of avoiding a CORS preflight), a
// different host, and conditional-revalidation semantics via
// updated_ids.
//
// Request decoration — browser headers and the edgeapi query envelope
// (_x_app_name, fp, _x_num_retries) — is handled entirely by
// slackhttp.BrowserTransport, which already distinguishes edgeapi from
// the workspace API. This package must not set headers itself; doing
// so would reintroduce the divergence Phase 1 removed.
//
// Contracts verified against internal/slack/testdata/phase2-api-contracts.json.
package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultBaseURL = "https://edgeapi.slack.com"

// Client calls edgeapi.slack.com for one workspace.
type Client struct {
	token   string
	teamID  string
	http    *http.Client
	baseURL string
}

// New returns a Client. httpClient must be one built with
// slackhttp.BrowserTransport so requests carry the browser headers and
// edgeapi envelope; pass the same client slack.Client uses.
func New(token, teamID string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		token:   token,
		teamID:  teamID,
		http:    httpClient,
		baseURL: defaultBaseURL,
	}
}

// call POSTs payload (with the token merged in) to
// /cache/<teamID>/<endpoint> and decodes the response into out.
func (c *Client) call(ctx context.Context, endpoint string, payload map[string]any, out any) error {
	body := make(map[string]any, len(payload)+1)
	body["token"] = c.token
	for k, v := range payload {
		body[k] = v
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("edge %s: encoding request: %w", endpoint, err)
	}

	url := fmt.Sprintf("%s/cache/%s/%s", c.baseURL, c.teamID, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("edge %s: building request: %w", endpoint, err)
	}
	// text/plain, not application/json: the official client uses a
	// CORS "simple request" so the browser skips the preflight. The
	// server accepts JSON regardless.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("edge %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("edge %s: reading response: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("edge %s: HTTP %d: %s", endpoint, resp.StatusCode, truncate(raw))
	}

	var probe struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("edge %s: decoding %s: %w", endpoint, truncate(raw), err)
	}
	if !probe.OK {
		return fmt.Errorf("edge %s: %s", endpoint, probe.Error)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("edge %s: decoding result: %w", endpoint, err)
		}
	}
	return nil
}

func truncate(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
```

- [ ] **Step 4: Verify**

Run: `go test ./internal/slack/edge/ -race -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/edge/
git commit -m "feat(edge): add edgeapi client with text/plain JSON transport"
```

---

## Task 4: Conditional revalidation — `channels/info` and `users/info`

**Files:**
- Create: `internal/slack/edge/cache.go`, `internal/slack/edge/cache_test.go`

This is the mechanism that replaces enumeration.

Observed contracts (`phase2-api-contracts.json`):

```
POST /cache/<team>/channels/info
{"token":…,"check_membership":true,"updated_ids":{"CL0AET1L0":1783337533019, …}}
→ {"ok":true,"results":[ …only changed/unknown channels… ]}

POST /cache/<team>/users/info
{"token":…,"check_interaction":true,"include_profile_only_users":true,
 "updated_ids":{"UAMBHRNE6":1612802061, …}}
→ {"ok":true,"results":[ …only changed/unknown users… ]}
```

Observed batch sizes: `channels/info` up to 63 ids per call; `users/info` 14-34. A request with all-current versions returns `results: []` in ~290 bytes.

- [ ] **Step 1: Write the failing test**

Create `internal/slack/edge/cache_test.go`:

```go
package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChannelsInfo_SendsUpdatedIDsAndDecodesResults(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Write([]byte(`{"ok":true,"results":[
			{"id":"C1","name":"general","updated":1783337533019,"is_channel":true,"is_member":true}
		]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL

	res, err := c.ChannelsInfo(context.Background(), map[string]int64{
		"C1": 1749647756152,
		"C2": 0,
	})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if got["check_membership"] != true {
		t.Errorf("check_membership not sent: %v", got)
	}
	ids, _ := got["updated_ids"].(map[string]any)
	if len(ids) != 2 {
		t.Errorf("updated_ids = %v; want 2 entries", ids)
	}
	if len(res) != 1 || res[0].ID != "C1" {
		t.Fatalf("results = %+v; want one channel C1", res)
	}
	if res[0].Version != 1783337533019 {
		t.Errorf("Version = %d; want 1783337533019 (from the `updated` field)", res[0].Version)
	}
}

func TestChannelsInfo_EmptyResultsMeansNothingChanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	res, err := c.ChannelsInfo(context.Background(), map[string]int64{"C1": 1783337533019})
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("results = %+v; want empty", res)
	}
}

func TestChannelsInfo_NoIDsMakesNoRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	res, err := c.ChannelsInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if called {
		t.Error("made an HTTP request for an empty id set")
	}
	if len(res) != 0 {
		t.Errorf("results = %+v; want empty", res)
	}
}

func TestUsersInfo_SendsExpectedFlags(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Write([]byte(`{"ok":true,"results":[
			{"id":"U1","name":"grant","updated":1612802061,
			 "profile":{"display_name":"Grant","image_32":"https://x/32.png"}}
		]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL

	res, err := c.UsersInfo(context.Background(), map[string]int64{"U1": 0})
	if err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}
	// Both flags are on every captured users/info request.
	if got["check_interaction"] != true {
		t.Errorf("check_interaction not sent: %v", got)
	}
	if got["include_profile_only_users"] != true {
		t.Errorf("include_profile_only_users not sent: %v", got)
	}
	if len(res) != 1 || res[0].ID != "U1" {
		t.Fatalf("results = %+v; want one user U1", res)
	}
	if res[0].Version != 1612802061 {
		t.Errorf("Version = %d; want 1612802061", res[0].Version)
	}
	if res[0].Profile.Image32 != "https://x/32.png" {
		t.Errorf("Image32 = %q; want the profile image url", res[0].Profile.Image32)
	}
}

func TestBatched_SplitsLargeIDSets(t *testing.T) {
	// Observed batch sizes: channels/info up to 63 ids, users/info
	// 14-34. Sending 5000 ids in one request would be a shape no real
	// client emits.
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			UpdatedIDs map[string]int64 `json:"updated_ids"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		batches = append(batches, len(body.UpdatedIDs))
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL

	ids := make(map[string]int64, 150)
	for i := 0; i < 150; i++ {
		ids[string(rune('A'+i%26))+string(rune('a'+i/26))] = int64(i)
	}
	if _, err := c.ChannelsInfo(context.Background(), ids); err != nil {
		t.Fatalf("ChannelsInfo: %v", err)
	}
	if len(batches) < 2 {
		t.Fatalf("batches = %v; want the id set split across multiple requests", batches)
	}
	for _, n := range batches {
		if n > channelsInfoBatchSize {
			t.Errorf("batch of %d exceeds channelsInfoBatchSize %d", n, channelsInfoBatchSize)
		}
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/slack/edge/ -run 'TestChannelsInfo|TestUsersInfo|TestBatched' -v`
Expected: FAIL — `c.ChannelsInfo undefined`.

- [ ] **Step 3: Implement**

Create `internal/slack/edge/cache.go`:

```go
package edge

import "context"

// Batch sizes. The official client sends up to 63 channel ids and
// 14-34 user ids per request; a single request carrying thousands
// would be a shape no real client emits.
const (
	channelsInfoBatchSize = 60
	usersInfoBatchSize    = 30
)

// Channel is one entry in a channels/info response.
type Channel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     int64  `json:"updated"`
	IsChannel   bool   `json:"is_channel"`
	IsGroup     bool   `json:"is_group"`
	IsIM        bool   `json:"is_im"`
	IsMPIM      bool   `json:"is_mpim"`
	IsPrivate   bool   `json:"is_private"`
	IsArchived  bool   `json:"is_archived"`
	IsMember    bool   `json:"is_member"`
	ContextTeam string `json:"context_team_id"`
	Topic       struct {
		Value string `json:"value"`
	} `json:"topic"`
}

// User is one entry in a users/info response.
type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"updated"`
	Deleted bool   `json:"deleted"`
	IsBot   bool   `json:"is_bot"`
	TeamID  string `json:"team_id"`
	Profile struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Image32     string `json:"image_32"`
	} `json:"profile"`
}

// ChannelsInfo revalidates cached channels. updatedIDs maps channel id
// to the version slk holds; send 0 for a channel whose record is
// unknown. Only channels that changed (or were unknown) come back, so
// a fully-current cache costs one sub-KB round trip per batch.
//
// This is what replaces conversations.list enumeration.
func (c *Client) ChannelsInfo(ctx context.Context, updatedIDs map[string]int64) ([]Channel, error) {
	var out []Channel
	err := c.batched(ctx, "channels/info", updatedIDs, channelsInfoBatchSize,
		map[string]any{"check_membership": true},
		func(raw *batchResponse) { out = append(out, raw.Channels...) })
	return out, err
}

// UsersInfo revalidates cached users. Same semantics as ChannelsInfo.
// This is what replaces users.list enumeration.
func (c *Client) UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]User, error) {
	var out []User
	err := c.batched(ctx, "users/info", updatedIDs, usersInfoBatchSize,
		map[string]any{
			"check_interaction":          true,
			"include_profile_only_users": true,
		},
		func(raw *batchResponse) { out = append(out, raw.Users...) })
	return out, err
}

// batchResponse decodes either shape; only one of the slices is
// populated depending on the endpoint.
type batchResponse struct {
	Channels []Channel `json:"-"`
	Users    []User    `json:"-"`
	Raw      []byte    `json:"-"`
}

// batched splits updatedIDs into batches of at most size, issuing one
// request each, and invokes collect for every response. Makes no
// request at all for an empty id set.
func (c *Client) batched(
	ctx context.Context,
	endpoint string,
	updatedIDs map[string]int64,
	size int,
	extra map[string]any,
	collect func(*batchResponse),
) error {
	if len(updatedIDs) == 0 {
		return nil
	}
	batch := make(map[string]int64, size)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		payload := make(map[string]any, len(extra)+1)
		for k, v := range extra {
			payload[k] = v
		}
		payload["updated_ids"] = batch

		var resp struct {
			Channels []Channel `json:"results"`
		}
		var uresp struct {
			Users []User `json:"results"`
		}
		// Decode into whichever shape the caller wants by decoding
		// twice against the same body; both are cheap and only one
		// will carry meaningful fields.
		if endpoint == "channels/info" {
			if err := c.call(ctx, endpoint, payload, &resp); err != nil {
				return err
			}
			collect(&batchResponse{Channels: resp.Channels})
		} else {
			if err := c.call(ctx, endpoint, payload, &uresp); err != nil {
				return err
			}
			collect(&batchResponse{Users: uresp.Users})
		}
		batch = make(map[string]int64, size)
		return nil
	}

	for id, v := range updatedIDs {
		batch[id] = v
		if len(batch) >= size {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}
```

**Note to the implementer:** the `batched` helper above uses an `endpoint == "channels/info"` string comparison to pick the decode shape, which is ugly. If you can express this more cleanly — generics, or two separate small functions rather than one shared helper — do so and say what you chose. Do not leave the string comparison if a clearer form exists; two near-duplicate 20-line functions may well be better than one clever one.

- [ ] **Step 4: Verify**

Run: `go test ./internal/slack/edge/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/edge/
git commit -m "feat(edge): add channels/info and users/info conditional revalidation

This is the mechanism that replaces enumeration: send {id: version}
for what we hold, receive only what changed. A fully-current cache
costs one sub-KB round trip per batch instead of ~55 paginated
conversations.list / users.list calls."
```

---

## Task 5: Server-side search — `channels/search` and `users/search`

**Files:**
- Create: `internal/slack/edge/search.go`, `internal/slack/edge/search_test.go`

Replaces slk's `conversations.list` enumeration for the fuzzy finder. Observed contract:

```
POST /cache/<team>/channels/search
{"token":…,"query":"test","count":30,"fuzz":1,"include_record_channels":true,
 "top_channels":[…22 frecent ids…],"default_workspace":"T04T4TH8W",
 "check_membership":true}
→ {"ok":true,"results":[…30 full channel objects…],"member_channels":[…]}

POST /cache/<team>/users/search
{"token":…,"include_profile_only_users":true,"query":"t","count":30,"fuzz":1,
 "enable_workspace_ranking":true,"search_email":true,"top_users":[…]}
→ {"ok":true,"results":[…30 users…]}
```

`top_channels`/`top_users` are the client's frecency list, sent as a ranking hint. slk already has one in `internal/cache/frecent.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/slack/edge/search_test.go` with tests asserting:
- `ChannelsSearch` sends `query`, `count=30`, `fuzz=1`, `include_record_channels=true`, `check_membership=true`, `default_workspace=<teamID>`, and the supplied `top_channels`
- it decodes `results` into `[]Channel` and `member_channels` into `[]string`
- `UsersSearch` sends `query`, `count=30`, `fuzz=1`, `enable_workspace_ranking=true`, `search_email=true`, `include_profile_only_users=true`, and `top_users`
- both return an empty slice and make **no** HTTP request for an empty query
- an API error propagates

Write these in the same style as `cache_test.go` — an `httptest` server capturing the decoded request body, then assertions on individual fields. Reuse the `New(...)` + `c.baseURL = srv.URL` setup.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/slack/edge/ -run Search -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

Create `internal/slack/edge/search.go` with:

```go
package edge

import "context"

// searchCount matches the official client: every captured search
// request asks for 30 results.
const searchCount = 30

// ChannelsSearch runs Slack's server-side fuzzy channel search.
// topChannels is a frecency-ordered id list sent as a ranking hint —
// pass slk's frecent channels.
//
// This replaces enumerating every public channel via
// conversations.list: the official client never fetches a full channel
// list in any capture, it searches server-side as the user types.
// Callers must debounce; the official client issues roughly one request
// per input pause, not per keystroke.
func (c *Client) ChannelsSearch(ctx context.Context, query string, topChannels []string) ([]Channel, []string, error) {
	if query == "" {
		return nil, nil, nil
	}
	payload := map[string]any{
		"query":                   query,
		"count":                   searchCount,
		"fuzz":                    1,
		"include_record_channels": true,
		"check_membership":        true,
		"default_workspace":       c.teamID,
	}
	if len(topChannels) > 0 {
		payload["top_channels"] = topChannels
	}
	var resp struct {
		Results        []Channel `json:"results"`
		MemberChannels []string  `json:"member_channels"`
	}
	if err := c.call(ctx, "channels/search", payload, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Results, resp.MemberChannels, nil
}

// UsersSearch runs Slack's server-side fuzzy user search. topUsers is a
// frecency-ordered id list sent as a ranking hint.
func (c *Client) UsersSearch(ctx context.Context, query string, topUsers []string) ([]User, error) {
	if query == "" {
		return nil, nil
	}
	payload := map[string]any{
		"query":                      query,
		"count":                      searchCount,
		"fuzz":                       1,
		"enable_workspace_ranking":   true,
		"search_email":               true,
		"include_profile_only_users": true,
	}
	if len(topUsers) > 0 {
		payload["top_users"] = topUsers
	}
	var resp struct {
		Results []User `json:"results"`
	}
	if err := c.call(ctx, "users/search", payload, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}
```

- [ ] **Step 4: Verify and commit**

```bash
go test ./internal/slack/edge/ -race -v
git add internal/slack/edge/
git commit -m "feat(edge): add server-side channel and user search

Replaces conversations.list enumeration for the fuzzy finder. The
official client never fetches a full channel list; it searches
server-side, debounced, with a frecency ranking hint."
```

---

## Task 6: Membership endpoints

**Files:**
- Create: `internal/slack/edge/members.go`, `internal/slack/edge/members_test.go`

Three small endpoints, all channel-scoped rather than workspace-wide:

```
POST /cache/<team>/users/list
{"token":…,"channels":["C06FR0Q00"],"present_first":true,
 "filter":"everyone AND NOT bots AND NOT apps","count":30}
→ {"ok":true,"results":[…30 users…]}

POST /cache/<team>/channels/membership
{"token":…,"channel":"C…","users":["U1","U2",…],"as_admin":false}
→ {"ok":true,"channel":"C…","members":[…],"non_members":[…]}

POST /cache/<team>/users/counts
{"token":…,"channel":"C…","as_admin":false}
→ {"ok":true,"channel":"C…","counts":…}
```

- [ ] **Step 1-4:** Same pattern as Tasks 4 and 5. Write `members_test.go` first with an `httptest` server per endpoint asserting the exact request fields above and decoding the responses; confirm red; implement `UsersList(ctx, channelID string, count int) ([]User, error)`, `ChannelsMembership(ctx, channelID string, userIDs []string) (members, nonMembers []string, err error)`, and `UsersCounts(ctx, channelID string) (int, error)`; confirm green.

Check the response shape for `users/counts` in `phase2-api-contracts.json` before defining its return type — the `counts` field's shape is recorded there and you should match it rather than assuming an int.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/edge/
git commit -m "feat(edge): add channel-scoped membership endpoints"
```

---

## Task 7: `client.userBoot` parser

**Files:**
- Create: `internal/slack/boot/boot.go`, `internal/slack/boot/boot_test.go`

One call replaces five: `users.conversations`, `users.prefs.get`, `stars.list`, `usergroups.list`, `dnd.info`.

Observed request (multipart form in the real client; slk sends urlencoded — a documented Phase 1 residual):

```
token=…, _x_reason=initial-data, version_all_channels=false,
return_all_relevant_mpdms=true,
omit_extras=feature_usage_data,plan_info,salesforce_features,
_x_sonic=true, _x_app_name=client
```

Response top-level keys (verbatim from the capture):

```
ok, app_commands_cache_ts, cache_ts_version, cache_version, emoji_cache_ts,
translations_cache_ts, is_content_reporting_enabled, dnd, is_europe,
account_types, can_access_client_v2, channels_priority, channels,
default_workspace, has_more_mpdms, ims, is_open, non_threadable_channels,
prefs, prefs_version, read_only_channels, self, slack_route, starred, team,
thread_only_channels, workspaces, is_slack_first_crm,
is_eligible_invited_user_glow_up, mobile_app_requires_upgrade, subteams,
accept_tos_url, links
```

Observed sizes on a ~10k-user workspace: 55 `channels`, 11 `ims`, **702** `prefs` keys, 139 KB total.

- [ ] **Step 1: Write the failing test**

Create `internal/slack/boot/boot_test.go`. Build a fixture JSON body covering the fields slk needs — `ok`, `self`, `team`, `channels`, `ims`, `is_open`, `prefs`, `starred`, `subteams`, `dnd`, `channels_priority`, `emoji_cache_ts` — and assert:
- request params exactly match the observed set above (including `_x_reason=initial-data`)
- each field decodes into the right Go type
- `channels` entries carry their `updated` version stamp (needed for Task 2's `SetChannelVersion`)
- an `ok:false` response is an error
- unknown/extra response fields do not break decoding (Slack adds fields without notice)

- [ ] **Step 2-4:** Confirm red; implement `boot.go` with a `Result` struct and `func UserBoot(ctx context.Context, post PostFunc) (*Result, error)`, where `PostFunc` is a small interface/func type so the parser is testable without an HTTP server. Confirm green.

Define `Result` to expose only what slk actually consumes. **Do not** model all 702 pref keys — pull out the handful slk needs (muted channels at minimum; check `internal/slack/client.go`'s `GetMutedChannels` for which pref that is) and keep the rest as `json.RawMessage`.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/boot/
git commit -m "feat(boot): parse client.userBoot

One call replaces users.conversations, users.prefs.get, stars.list,
usergroups.list and dnd.info."
```

---

## Task 8: `conversations.view` parser

**Files:**
- Create: `internal/slack/boot/view.go`, `internal/slack/boot/view_test.go`

Returns history + users + bots + channels + emoji in one response, replacing the initial `conversations.history` plus the per-author `users.info` fan-out plus `emoji.list`.

Observed request params:

```
token, canonical_avatars=true, no_user_profile=true, ignore_replies=true,
no_self=true, include_full_users=true, include_use_case=true,
include_stories=true, no_members=true, include_mutation_timestamps=true,
count=28, include_free_team_extra_messages=true, _x_sonic=true,
_x_app_name=client
```

Response top-level: `ok`, `history` (`messages`, `has_more`, `mutation_timestamps`, `channel_actions_ts`, `channel_actions_count`, `next_ts`), `users`, `bots`, `channels`, `emojis`.

**Unverified assumption, flagged in the spec:** the captured request carries **no `channel` param** — it returned the last-viewed conversation. slk needs a *specific* channel. A `channel` param probably works but is unverified.

**Handle this explicitly:** implement `ConversationsView(ctx, post, channelID string)` such that it sends `channel` when non-empty, and make the caller able to detect failure and fall back. Add a test for the no-channel form too. Document the uncertainty in the doc comment. The verified fallback is `conversations.history` with `limit=28` + `cached_latest_updates` (Task 9), which Phase 2b will use if the probe fails.

- [ ] **Steps:** Same TDD shape as Task 7. Commit:

```bash
git add internal/slack/boot/
git commit -m "feat(boot): parse conversations.view

Combines history, users, bots, channels and emoji into one response.
The channel param is unverified -- the captured request carried none
and returned the last-viewed conversation -- so callers must be able
to fall back to conversations.history."
```

---

## Task 9: `conversations.history` with `cached_latest_updates`

**Files:**
- Modify: `internal/slack/client.go` (add a new method; do not change existing ones)
- Test: `internal/slack/client_test.go`

Slack's incremental-sync primitive. Lets slk validate cached scrollback **without re-downloading it**.

Observed request:

```
channel, limit=28, inclusive=true, ignore_replies=true, no_user_profile=true,
include_pin_count=true, include_stories=true,
include_free_team_extra_messages=true, include_date_joined=<bool>,
latest=<ts> | oldest=<ts>,
cached_latest_updates={"<ts>":"<version>", …}
_x_reason=message-pane/requestHistory
```

Response adds `latest_updates` (`{ts: version}`) and `unchanged_messages` (`[ts]`) alongside `messages`.

Verified working in the scroll capture: the client sent one cached `{ts: version}` and the server replied `unchanged_messages=1, messages=27`.

- [ ] **Steps:** TDD as usual. Add `HistoryWithVersions(ctx, channelID string, opts HistoryOpts) (HistoryResult, error)` where `HistoryOpts` carries `Limit`, `Latest`, `Oldest`, `CachedVersions map[string]string`, and `HistoryResult` carries `Messages`, `UnchangedTS []string`, `LatestUpdates map[string]string`, `HasMore`.

Use `slackhttp.WithReason(ctx, "message-pane/requestHistory")`. Use `limit=28`, not 50 — every captured request uses 28, and slk's existing 50/200/500 page sizes are shapes the client never emits.

Test that: the request carries `cached_latest_updates` as a JSON-encoded map; `unchanged_messages` decodes; an empty `CachedVersions` sends `{}` (which is what the client sends when it holds nothing, per the capture) rather than omitting the param.

- [ ] **Commit:**

```bash
git add internal/slack/
git commit -m "feat(slack): add conversations.history with cached_latest_updates

Slack's incremental-sync primitive: send {ts: version} for cached
messages, receive unchanged_messages plus only the bodies that
changed. Lets slk validate scrollback without re-downloading it."
```

---

## Task 10: Full verification

- [ ] **Step 1: Build, vet, test, lint**

```bash
go build ./... && go vet ./... && go test ./... -race && golangci-lint run ./...
```
All must pass.

- [ ] **Step 2: Confirm nothing is wired up yet**

```bash
grep -rn 'slack/edge\|slack/boot' --include='*.go' cmd/ internal/ | grep -v '_test.go' | grep -v '^internal/slack/edge' | grep -v '^internal/slack/boot'
```
Expected: **no matches.** This plan is purely additive; if anything in `cmd/` imports the new packages, that belongs to Phase 2b.

- [ ] **Step 3: Confirm the network is not touched by tests**

```bash
unshare -rn go test ./... 2>&1 | grep -E '^(FAIL|---)'
```
Expected: no failures. (If `unshare` is unavailable, say so rather than skipping silently.) Phase 1 found three endpoints making live calls to slack.com during tests; do not reintroduce that.

- [ ] **Step 4: Commit any fixes**

---

## Phase 2b Preview (separate plan, written after 2a merges)

Not in scope here. Recorded so the shape is clear:

1. **Rewire `connectWorkspace`** onto: `auth.test` (retained for Grid host discovery) → `client.userBoot` → `client.counts` → `conversations.view` → `edge.ChannelsInfo` + `edge.UsersInfo`. Six calls replacing ~400.
2. **Delete `triggerBackfill` from every call site** — `main.go` `OnConnect` and the wake-from-sleep detector, plus the method itself. This is the single largest fingerprint change. Replace the reconnect path with a bounded O(1) handler: `client.counts` + the active channel only, others marked stale for lazy revalidation.
3. **Move the channel finder** onto `edge.ChannelsSearch`, debounced ~300 ms, local cache matched first for instant feedback.
4. **Defer `subscriptions.thread.getView`** to first open of the Threads view.
5. **Delete `conversations.list`** enumeration and the `users.list` full sync.

**Verification task for 2b, cheap and local:** run slk with `SLK_DEBUG=1`, drop the network for 90 s, restore, and check whether messages sent during the outage arrive over the WebSocket without any HTTP fetch. The official client does **zero** HTTP catch-up on reconnect; if slk's socket delivers the same replay, 2b's `client.counts` call can be dropped too.

---

## Self-Review Notes

**Spec coverage.** Layer 2 of the spec requires: `client.userBoot` (Task 7), `conversations.view` (Task 8), edgeapi conditional revalidation (Task 4), `channels/search` finder (Task 5), `cached_latest_updates` incremental sync (Task 9), version columns (Tasks 1-2), and the `internal/slack/edge` + `internal/slack/boot` module split (Tasks 3-8). The deletions and rewiring are explicitly Phase 2b.

**Known weakness in this plan:** Tasks 6, 7 and 8 specify tests in prose rather than complete code, because their fixtures depend on response shapes too large to inline here. The implementer must read `phase2-api-contracts.json`, and can query `/tmp/*.har` directly for anything it doesn't cover. This is a deliberate trade-off against a plan long enough to time out — but it does mean those three tasks need more judgement than Tasks 1-5.

**Deliberately unresolved:** the `conversations.view` `channel` param (Task 8) is unverified and the plan says so rather than guessing. Task 4's `batched` helper is given with an explicit note that its shape is poor and the implementer should improve it.
