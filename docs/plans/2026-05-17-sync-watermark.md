# Sync Watermark Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the reconnect-backfill algorithm so messages that arrive while slk is offline (laptop suspend, dead WS, etc.) are reliably caught up, including in channels that have never been opened in slk.

**Architecture:** Replace the wall-clock `channels.synced_at` cursor with a per-channel Slack-message-ts watermark (`channels.latest_synced_ts`). Advance the watermark only when we have proof the channel is fully caught up — either a real-time WS message landed (Slack delivers in order on a live WS), or a backfill batch completed with `has_more=false` and no cap hit. Broaden the backfill candidate set to include any channel `client.counts` says has unreads, not just channels we have cached messages for. Keep the existing wall-clock `synced_at` for UI cache-freshness display (different concept; both columns coexist).

**Tech Stack:** Go 1.22+, `modernc.org/sqlite`, `github.com/slack-go/slack v0.23.0`, the in-tree `internal/slack` client (which wraps the library and adds hand-rolled `client.counts` support).

**Background:** This plan supersedes the cursor logic introduced by `docs/plans/2026-05-11-reconnect-backfill-and-threads-sort.md`. The May 11 plan introduced `channels.synced_at` as a wall-clock timestamp and the `ChannelsWithMessages` filter; both decisions need to change.

---

## File Structure

**New files:**
- None.

**Modified files (in order of first edit):**
- `internal/cache/db.go` — add `latest_synced_ts` column migration.
- `internal/cache/channels.go` — add `Set/GetChannelLatestSyncedTS`, `AdvanceChannelLatestSyncedTS`, `MaxMessageTSForChannel` helpers.
- `internal/cache/channels_test.go` — unit tests for the new helpers.
- `internal/cache/channels_sync.go` — add `BackfillCandidates`; deprecate `ChannelsWithMessages` (keep until callers migrate).
- `internal/cache/channels_sync_test.go` — new file, tests for `BackfillCandidates`.
- `cmd/slk/main.go` — update `OnMessage` to advance the new watermark; update `newBackfiller` call site.
- `cmd/slk/reconnect_backfill.go` — switch to ts-based watermark, conditional advancement, broadened candidate set.
- `cmd/slk/reconnect_backfill_test.go` — extend `fakeHistory` with `GetUnreadCounts`; add new tests for cap-hit, new-DM, and watermark semantics.
- `internal/slack/client.go` — minor: update the `GetHistorySince` docstring to describe newest-first pagination.

**Touched but not behavior-changed:**
- `internal/ui/app.go` — no change. The existing `synced_at`-based freshness display is preserved.

---

## Task 1: Add `latest_synced_ts` column + read/write helpers

**Why:** We need a per-channel Slack-message-ts watermark separate from the wall-clock `synced_at`. This task adds the storage and the read/write primitives, with **no behavior change** to the rest of the app. Later tasks will start using it.

**Files:**
- Modify: `internal/cache/db.go` (append one `addColumnIfMissing` call inside `migrate()`).
- Modify: `internal/cache/channels.go` (append four new functions).
- Create: tests in `internal/cache/channels_test.go` (the file already exists; append new `Test*` functions).

### Steps

- [ ] **Step 1.1: Write the failing test for `SetChannelLatestSyncedTS` / `GetChannelLatestSyncedTS`**

Append to `internal/cache/channels_test.go`:

```go
func TestSetAndGetChannelLatestSyncedTS(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// Default: empty string.
	if got := db.GetChannelLatestSyncedTS("C1"); got != "" {
		t.Errorf("default GetChannelLatestSyncedTS = %q, want empty", got)
	}

	// After Set, returns what we set.
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000123"); err != nil {
		t.Fatalf("SetChannelLatestSyncedTS: %v", err)
	}
	if got := db.GetChannelLatestSyncedTS("C1"); got != "1700000000.000123" {
		t.Errorf("GetChannelLatestSyncedTS = %q, want %q", got, "1700000000.000123")
	}

	// Unknown channel: returns empty without error.
	if got := db.GetChannelLatestSyncedTS("CDOESNOTEXIST"); got != "" {
		t.Errorf("GetChannelLatestSyncedTS unknown = %q, want empty", got)
	}
}
```

- [ ] **Step 1.2: Run test, verify it fails with "undefined: db.SetChannelLatestSyncedTS"**

Run: `go test ./internal/cache/ -run TestSetAndGetChannelLatestSyncedTS -v`
Expected: compile error like `db.SetChannelLatestSyncedTS undefined`.

- [ ] **Step 1.3: Add the schema migration**

In `internal/cache/db.go`, inside `migrate()`, add a new `addColumnIfMissing` call after the existing `synced_at` one (around line 186):

```go
	if err := db.addColumnIfMissing("channels", "latest_synced_ts",
		"ALTER TABLE channels ADD COLUMN latest_synced_ts TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
```

- [ ] **Step 1.4: Add the helper functions to `internal/cache/channels.go`**

Append to the bottom of `internal/cache/channels.go`:

```go
// SetChannelLatestSyncedTS stores a Slack message timestamp (string
// form, e.g. "1700000000.000123") that represents the watermark of
// "we have no gaps in this channel above this ts." Unlike synced_at,
// this is a Slack-domain value, not a wall-clock timestamp.
//
// Implemented as an UPDATE so it only touches existing rows; callers
// must have UpsertChannel'd the channel first. Rows-affected is not
// checked — a missing row is a silent no-op, matching the established
// pattern in SetChannelSyncedAt.
func (db *DB) SetChannelLatestSyncedTS(channelID, ts string) error {
	_, err := db.conn.Exec(
		`UPDATE channels SET latest_synced_ts = ? WHERE id = ?`,
		ts, channelID,
	)
	if err != nil {
		return fmt.Errorf("setting channel latest_synced_ts: %w", err)
	}
	return nil
}

// GetChannelLatestSyncedTS returns the watermark set by
// SetChannelLatestSyncedTS, or "" if the channel row is missing or the
// column was never written. The empty return doubles as the
// "no prior sync" signal that backfill uses to decide between
// pagination from a known cursor vs. a one-shot "fetch latest page"
// request.
func (db *DB) GetChannelLatestSyncedTS(channelID string) string {
	var ts string
	err := db.conn.QueryRow(
		`SELECT latest_synced_ts FROM channels WHERE id = ?`,
		channelID,
	).Scan(&ts)
	if err != nil {
		return ""
	}
	return ts
}

// AdvanceChannelLatestSyncedTS sets latest_synced_ts to ts only if ts
// is lexicographically greater than the current value. Slack ts
// strings (e.g., "1700000000.123456") sort lexicographically the same
// way they sort numerically when both are normalized to the same
// decimal width, which Slack always emits, so string compare is safe.
//
// This is the operation real-time WS message handlers should use: a
// WS-delivered message is always newer than any prior WS-delivered
// message on the same connection, but during reconnect or a race we
// may briefly process an event with ts older than our recorded
// watermark (e.g., a delayed thread reply that we already backfilled).
// In that case we must NOT regress the watermark.
//
// Returns the resulting watermark (either the new ts or the
// pre-existing value) and any error from the database. A missing row
// behaves as if the watermark was empty: the new ts is written.
func (db *DB) AdvanceChannelLatestSyncedTS(channelID, ts string) (string, error) {
	if ts == "" {
		return db.GetChannelLatestSyncedTS(channelID), nil
	}
	// UPDATE ... WHERE ts > existing returns rows-affected=0 if the
	// proposed ts is not strictly greater; that is the no-regress case.
	_, err := db.conn.Exec(
		`UPDATE channels
		 SET latest_synced_ts = ?
		 WHERE id = ? AND ? > latest_synced_ts`,
		ts, channelID, ts,
	)
	if err != nil {
		return "", fmt.Errorf("advancing channel latest_synced_ts: %w", err)
	}
	return db.GetChannelLatestSyncedTS(channelID), nil
}

// MaxMessageTSForChannel returns the highest message ts stored for
// the given channel, or "" if the channel has no cached messages.
// Used by GetChannelWatermark as the fallback when latest_synced_ts
// has never been written (i.e., this is a pre-migration channel whose
// cache predates the latest_synced_ts column).
//
// The query uses idx_messages_channel(channel_id, ts) and is a
// straight index lookup — it does not scan the table.
func (db *DB) MaxMessageTSForChannel(channelID string) (string, error) {
	var ts sql.NullString
	err := db.conn.QueryRow(
		`SELECT MAX(ts) FROM messages WHERE channel_id = ?`,
		channelID,
	).Scan(&ts)
	if err != nil {
		return "", fmt.Errorf("max message ts: %w", err)
	}
	if !ts.Valid {
		return "", nil
	}
	return ts.String, nil
}

// GetChannelWatermark returns the per-channel sync watermark used by
// the reconnect backfill. It prefers the explicit latest_synced_ts
// column (set by real-time WS handlers and completed backfill batches)
// and falls back to MAX(ts) FROM messages for channels whose cache
// predates the latest_synced_ts migration. Returns "" only if both
// sources are empty, which means "no prior sync — fetch the latest
// page only."
func (db *DB) GetChannelWatermark(channelID string) (string, error) {
	if v := db.GetChannelLatestSyncedTS(channelID); v != "" {
		return v, nil
	}
	return db.MaxMessageTSForChannel(channelID)
}
```

The current `internal/cache/channels.go` only imports `"fmt"`. Update the import block at the top of the file to:

```go
import (
	"database/sql"
	"fmt"
)
```

The `database/sql` import is required for `sql.NullString` inside `MaxMessageTSForChannel`.

- [ ] **Step 1.5: Re-run the test, verify it passes**

Run: `go test ./internal/cache/ -run TestSetAndGetChannelLatestSyncedTS -v`
Expected: PASS.

- [ ] **Step 1.6: Add tests for `AdvanceChannelLatestSyncedTS` and `MaxMessageTSForChannel` and `GetChannelWatermark`**

Append to `internal/cache/channels_test.go`:

```go
func TestAdvanceChannelLatestSyncedTS_NoRegress(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// First advance from empty: writes the value.
	got, err := db.AdvanceChannelLatestSyncedTS("C1", "1700000000.000010")
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got != "1700000000.000010" {
		t.Errorf("first advance: got %q want %q", got, "1700000000.000010")
	}

	// Advance to a newer ts: writes.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "1700000001.000000")
	if err != nil {
		t.Fatalf("advance newer: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance newer: got %q want %q", got, "1700000001.000000")
	}

	// Advance to an older ts: NO regress; current value preserved.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "1699999999.000000")
	if err != nil {
		t.Fatalf("advance older: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance older should not regress: got %q want %q", got, "1700000001.000000")
	}

	// Advance to equal ts: no change.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "1700000001.000000")
	if err != nil {
		t.Fatalf("advance equal: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance equal: got %q want %q", got, "1700000001.000000")
	}

	// Empty ts: no-op, returns current value.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "")
	if err != nil {
		t.Fatalf("advance empty: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance empty: got %q want %q", got, "1700000001.000000")
	}
}

func TestMaxMessageTSForChannel(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	// Empty channel: empty result.
	got, err := db.MaxMessageTSForChannel("C1")
	if err != nil {
		t.Fatalf("max empty: %v", err)
	}
	if got != "" {
		t.Errorf("max empty: got %q want empty", got)
	}

	// Insert a few messages out of order.
	for _, ts := range []string{"1700000000.000010", "1700000005.000000", "1700000002.000050"} {
		if err := db.UpsertMessage(cache.Message{
			TS: ts, ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "x",
			CreatedAt: 1700000000,
		}); err != nil {
			t.Fatalf("upsert message %s: %v", ts, err)
		}
	}

	got, err = db.MaxMessageTSForChannel("C1")
	if err != nil {
		t.Fatalf("max populated: %v", err)
	}
	if got != "1700000005.000000" {
		t.Errorf("max: got %q want %q", got, "1700000005.000000")
	}
}

func TestGetChannelWatermark_PrefersExplicitThenMaxTS(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// No watermark, no messages → empty.
	if got, err := db.GetChannelWatermark("C1"); err != nil || got != "" {
		t.Errorf("empty: got %q err %v, want empty/nil", got, err)
	}

	// No explicit watermark, but cached messages → MAX(ts).
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000100.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "msg", CreatedAt: 1700000100,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got, err := db.GetChannelWatermark("C1"); err != nil || got != "1700000100.000000" {
		t.Errorf("fallback: got %q err %v, want %q", got, err, "1700000100.000000")
	}

	// Explicit watermark takes precedence, even if behind MAX(ts).
	// (This models the "we have a gap above the watermark" scenario.)
	if err := db.SetChannelLatestSyncedTS("C1", "1700000050.000000"); err != nil {
		t.Fatalf("set explicit: %v", err)
	}
	if got, err := db.GetChannelWatermark("C1"); err != nil || got != "1700000050.000000" {
		t.Errorf("explicit precedence: got %q err %v, want %q", got, err, "1700000050.000000")
	}
}
```

- [ ] **Step 1.7: Run all the new tests**

Run: `go test ./internal/cache/ -run 'TestSetAndGetChannelLatestSyncedTS|TestAdvanceChannelLatestSyncedTS_NoRegress|TestMaxMessageTSForChannel|TestGetChannelWatermark_PrefersExplicitThenMaxTS' -v`
Expected: 4 tests PASS.

- [ ] **Step 1.8: Run the full cache package suite to make sure the migration didn't break anything**

Run: `go test ./internal/cache/...`
Expected: PASS (all existing tests still green).

- [ ] **Step 1.9: Commit**

```bash
git add internal/cache/db.go internal/cache/channels.go internal/cache/channels_test.go
git commit -m "feat(cache): add latest_synced_ts column and watermark helpers

Introduces a per-channel Slack-message-ts watermark separate from the
existing wall-clock synced_at. Provides Set/Get/Advance helpers plus a
GetChannelWatermark accessor that falls back to MAX(ts) for channels
predating the new column. No behavior change yet; later commits wire
this into OnMessage and the reconnect backfiller."
```

---

## Task 2: Have `OnMessage` advance the watermark

**Why:** Real-time WS messages are the cheapest, most accurate source of "this channel is up to date through ts X." We piggyback on every `OnMessage` to advance the watermark, so a slk that has been running normally always has a current watermark for every channel that's had activity.

**Files:**
- Modify: `cmd/slk/main.go` (inside `OnMessage`, around line 2570-2572).

### Steps

- [ ] **Step 2.1: Locate the existing SetChannelSyncedAt call**

Open `cmd/slk/main.go`, find lines 2570-2572:

```go
		if err := h.db.SetChannelSyncedAt(channelID, time.Now().Unix()); err != nil {
			debuglog.Cache("OnMessage: SetChannelSyncedAt %s: %v", channelID, err)
		}
```

This stays — `synced_at` retains its wall-clock semantics for UI freshness. We are **adding** an `AdvanceChannelLatestSyncedTS` call alongside it.

- [ ] **Step 2.2: Add the watermark advance call**

Edit `cmd/slk/main.go` lines 2570-2572 to read:

```go
		if err := h.db.SetChannelSyncedAt(channelID, time.Now().Unix()); err != nil {
			debuglog.Cache("OnMessage: SetChannelSyncedAt %s: %v", channelID, err)
		}
		// Advance the per-channel ts watermark used by reconnect
		// backfill. Slack delivers WS messages in order, so receipt
		// of a message with ts=X implies we have no missing messages
		// with ts <= X on this channel — that is exactly the
		// invariant latest_synced_ts encodes. AdvanceChannelLatestSyncedTS
		// is no-regress, so out-of-order replay (e.g., a delayed
		// duplicate after reconnect) won't move the cursor backward.
		if _, err := h.db.AdvanceChannelLatestSyncedTS(channelID, ts); err != nil {
			debuglog.Cache("OnMessage: AdvanceChannelLatestSyncedTS %s ts=%s: %v", channelID, ts, err)
		}
```

- [ ] **Step 2.3: Build to verify no compile errors**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 2.4: Run the full test suite — nothing observable should have changed yet, all tests still pass**

Run: `make test`
Expected: PASS.

- [ ] **Step 2.5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(sync): advance latest_synced_ts watermark on every WS message

OnMessage now records the incoming message's ts as the per-channel
watermark in addition to bumping the wall-clock synced_at. Uses
AdvanceChannelLatestSyncedTS so the watermark cannot regress on
out-of-order delivery. No effect yet — the backfiller still reads
synced_at; that switch happens in the next commit."
```

---

## Task 3: Switch backfill to ts-based watermark; only advance on completion

**Why:** This is the core fix for the "messages silently dropped when the cap is hit" bug. Two related changes:
1. `oldest` parameter to `GetHistorySince` is derived from `GetChannelWatermark` (a real Slack ts) instead of `synced_at` (wall-clock seconds).
2. The watermark is **only** advanced after a batch when (a) we know we got everything (the API said `has_more=false` and we didn't hit the cap), and we advance it to the **highest ts in the batch**. If the cap was hit, the watermark stays put so the next reconnect re-fetches the missed range.

Note: we still bump the wall-clock `synced_at` after every batch for UI freshness — that's unrelated and harmless.

**Files:**
- Modify: `internal/slack/client.go` (docstring update on `GetHistorySince` + add a result type that exposes `HasMore`).
- Modify: `cmd/slk/reconnect_backfill.go` (use new return type, new advancement rule).
- Modify: `cmd/slk/reconnect_backfill_test.go` (extend `fakeHistory` to return the new type, add cap-hit test).

### 3a. Expose `has_more` from `GetHistorySince`

The current `GetHistorySince` returns `([]slack.Message, error)`. The caller cannot distinguish "we got everything" from "we hit the cap and there's more." We change the signature to return a small result struct.

- [ ] **Step 3.1: Define the result struct in `internal/slack/client.go`**

Above the `GetHistorySince` function (around line 456), add:

```go
// HistorySinceResult bundles the messages fetched by GetHistorySince
// with a "did we get everything?" flag. Capped == true means the
// caller hit maxTotal before the API ran out of pages, so there are
// still older-than-cap messages between (oldest, latest-fetched-ts)
// the caller didn't get. Callers that advance a sync watermark MUST
// gate the advance on Capped == false.
type HistorySinceResult struct {
	Messages []slack.Message
	Capped   bool
}
```

- [ ] **Step 3.2: Update `GetHistorySince` to return the new type**

Replace the function body (currently `internal/slack/client.go:471-528`) with:

```go
// GetHistorySince fetches all messages newer than `oldest` for the
// given channel, paginating through next_cursor up to a hard ceiling
// of maxTotal messages. Slack returns messages newest-first per page;
// pagination via next_cursor walks toward older pages within the
// (oldest, latest] window. When maxTotal is hit, the result's Capped
// field is set to true so callers can decide whether to advance a
// watermark (don't) or record a gap.
//
// If oldest == "", behaves like a single GetHistory call (no
// pagination) and returns at most maxTotal messages from the latest
// page, with Capped reflecting whether HasMore was true. This matches
// the "first-sync channel: just give me the latest page" pattern.
func (c *Client) GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (HistorySinceResult, error) {
	if maxTotal <= 0 {
		maxTotal = 500
	}

	// No prior sync — fetch latest page only.
	if oldest == "" {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Limit:     200,
		}
		resp, err := c.api.GetConversationHistory(params)
		if err != nil {
			return HistorySinceResult{}, fmt.Errorf("get history (no oldest): %w", err)
		}
		out := resp.Messages
		capped := resp.HasMore
		if len(out) > maxTotal {
			out = out[:maxTotal]
			capped = true
		}
		return HistorySinceResult{Messages: out, Capped: capped}, nil
	}

	var all []slack.Message
	cursor := ""
	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    oldest,
			Limit:     200,
			Cursor:    cursor,
		}
		resp, err := c.api.GetConversationHistory(params)
		if err != nil {
			if rlErr, ok := err.(*slack.RateLimitedError); ok {
				wait := rlErr.RetryAfter
				if wait == 0 {
					wait = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return HistorySinceResult{Messages: all, Capped: true}, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return HistorySinceResult{Messages: all, Capped: true}, fmt.Errorf("get history since %s: %w", oldest, err)
		}

		all = append(all, resp.Messages...)
		if len(all) >= maxTotal {
			return HistorySinceResult{Messages: all[:maxTotal], Capped: true}, nil
		}
		if !resp.HasMore || resp.ResponseMetaData.NextCursor == "" {
			return HistorySinceResult{Messages: all, Capped: false}, nil
		}
		cursor = resp.ResponseMetaData.NextCursor
	}
}
```

Key changes:
- Returns `HistorySinceResult` instead of `[]slack.Message`.
- The "no oldest" branch now sets `Capped = resp.HasMore` so callers know there's older history available even on first sync.
- Mid-loop errors return `Capped: true` defensively (we don't know if we got everything).
- The "hit the cap" path explicitly returns `Capped: true`.
- The "natural completion" path (`!HasMore || NextCursor == ""`) returns `Capped: false`.

### 3b. Update the test fake and write a failing cap-hit test

- [ ] **Step 3.3: Locate the `fakeHistory` and `historyFetcher` interface in `cmd/slk/reconnect_backfill.go`**

Lines 24-28 currently read:

```go
type historyFetcher interface {
	GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) ([]slack.Message, error)
	GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error)
	ListThreadSubscriptions(ctx context.Context) ([]slack.ThreadSubscriptionView, error)
}
```

Change the `GetHistorySince` line to:

```go
	GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (slackclient.HistorySinceResult, error)
```

(Adjust the import alias at the top of the file: `slackclient "github.com/gammons/slk/internal/slack"` is already imported elsewhere; reuse the existing alias.)

- [ ] **Step 3.4: Update `fakeHistory` in `cmd/slk/reconnect_backfill_test.go` to return the new type**

In the existing test file, locate the `fakeHistory` struct and its `GetHistorySince` method. Replace its return-value construction. The struct fields stay the same; only the method signature and return shape change:

```go
func (f *fakeHistory) GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (slackclient.HistorySinceResult, error) {
	msgs := f.history[channelID]
	capped := false
	if maxTotal > 0 && len(msgs) > maxTotal {
		msgs = msgs[:maxTotal]
		capped = true
	}
	return slackclient.HistorySinceResult{Messages: msgs, Capped: capped}, nil
}
```

If the existing `fakeHistory` exposes a per-channel "force capped=true" toggle for testing, add a `forceCapped map[string]bool` field and OR it in. Otherwise, the `len > maxTotal` rule is sufficient for the new tests.

- [ ] **Step 3.5: Write the failing test for "cap hit → watermark NOT advanced"**

Append to `cmd/slk/reconnect_backfill_test.go`:

```go
func TestBackfill_CapHit_DoesNotAdvanceWatermark(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "busy", Type: "channel", IsMember: true}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	// Pre-existing watermark so backfill takes the "since" branch.
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000000"); err != nil {
		t.Fatalf("set watermark: %v", err)
	}
	// Pre-existing message to ensure the channel is in
	// BackfillCandidates' "cached" branch.
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000000.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "anchor", CreatedAt: 1700000000,
	}); err != nil {
		t.Fatalf("upsert anchor: %v", err)
	}

	// Fake returns 10 messages but cap is 5 → Capped == true.
	fh := &fakeHistory{
		history: map[string][]slack.Message{
			"C1": makeFakeMessages("1700001000", 10), // helper below
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 1 /*conc*/, 5 /*cap*/, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Watermark must NOT have advanced past the pre-existing value
	// because the batch was capped.
	got := db.GetChannelLatestSyncedTS("C1")
	if got != "1700000000.000000" {
		t.Errorf("watermark advanced despite cap: got %q want %q", got, "1700000000.000000")
	}
}

// makeFakeMessages returns n messages with monotonically increasing ts
// starting from base (a string like "1700001000"). Each message's ts
// is base + ".0000NN" so they sort correctly as Slack ts strings.
func makeFakeMessages(base string, n int) []slack.Message {
	out := make([]slack.Message, n)
	for i := 0; i < n; i++ {
		out[i] = slack.Message{Msg: slack.Msg{
			Type:      "message",
			Timestamp: fmt.Sprintf("%s.%06d", base, i),
			User:      "U1",
			Text:      fmt.Sprintf("msg %d", i),
		}}
	}
	return out
}
```

- [ ] **Step 3.6: Run the new test, verify it fails**

Run: `go test ./cmd/slk/ -run TestBackfill_CapHit_DoesNotAdvanceWatermark -v`
Expected: FAIL. The current `backfillOneChannel` will advance `synced_at` (which we don't check) but it will not write `latest_synced_ts` at all, so the test will fail on `got != "1700000000.000000"` — actually the test as written expects the **pre-set** watermark to remain. The current code doesn't touch `latest_synced_ts`, so the assertion would pass trivially. Reframe the test to also write a sentinel "advanced" value if the buggy code ran:

Update the test's final assertion block to also exercise the **non-capped** branch first, then the capped branch, to prove the advancement rule. Replace the final assertion with:

```go
	// Watermark must NOT have advanced past the pre-existing value
	// because the batch was capped. The current implementation
	// (pre-fix) does not write latest_synced_ts at all, so this
	// assertion will pass; the more important assertion lives in
	// the next test (TestBackfill_FullFetch_AdvancesWatermark) which
	// the current code WILL fail.
	got := db.GetChannelLatestSyncedTS("C1")
	if got != "1700000000.000000" {
		t.Errorf("watermark advanced despite cap: got %q want %q", got, "1700000000.000000")
	}
```

And immediately add the complementary positive test:

```go
func TestBackfill_FullFetch_AdvancesWatermarkToMaxTS(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "quiet", Type: "channel", IsMember: true}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000000"); err != nil {
		t.Fatalf("set watermark: %v", err)
	}
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000000.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "anchor", CreatedAt: 1700000000,
	}); err != nil {
		t.Fatalf("upsert anchor: %v", err)
	}

	// 3 messages, cap is 10 → Capped == false. Highest ts is .000002.
	fh := &fakeHistory{
		history: map[string][]slack.Message{
			"C1": makeFakeMessages("1700001000", 3),
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 1, 10, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := db.GetChannelLatestSyncedTS("C1")
	want := "1700001000.000002"
	if got != want {
		t.Errorf("watermark not advanced to MAX(ts): got %q want %q", got, want)
	}
}
```

- [ ] **Step 3.7: Run both new tests, verify the **positive** one fails**

Run: `go test ./cmd/slk/ -run 'TestBackfill_CapHit_DoesNotAdvanceWatermark|TestBackfill_FullFetch_AdvancesWatermarkToMaxTS' -v`
Expected: cap-hit test PASSES vacuously (no code path touches `latest_synced_ts` yet); full-fetch test FAILS with `want "1700001000.000002" got ""`. That's the failure we drive the implementation against.

### 3c. Implement the new advancement rule in `backfillOneChannel`

- [ ] **Step 3.8: Replace `backfillOneChannel` in `cmd/slk/reconnect_backfill.go`**

Replace the body of `backfillOneChannel` (currently `cmd/slk/reconnect_backfill.go:147-192`) with:

```go
// backfillOneChannel fetches missed history for a single channel and
// upserts every returned message. Returns the count of upserted
// messages. Records thread_ts of any returned thread-reply messages
// into b.discoveredThreads.
//
// Watermark advancement rule: latest_synced_ts is advanced to the
// highest ts in the fetched batch ONLY when the batch was not capped
// (i.e., we know we got every message in (oldest, now]). If the cap
// was hit, the watermark is left untouched so the next reconnect
// re-fetches the missed range. The wall-clock synced_at is bumped
// either way for UI cache-freshness display.
func (b *backfiller) backfillOneChannel(ctx context.Context, row cache.ChannelSyncRow) (int, error) {
	// Resolve the watermark: explicit latest_synced_ts wins over
	// MAX(ts) fallback. Empty string means "first sync — just fetch
	// the latest page."
	oldest, err := b.db.GetChannelWatermark(row.ChannelID)
	if err != nil {
		return 0, fmt.Errorf("watermark for %s: %w", row.ChannelID, err)
	}
	start := time.Now()

	res, err := b.client.GetHistorySince(ctx, row.ChannelID, oldest, b.perChannelCap)
	if err != nil {
		return 0, err
	}

	var maxTS string
	for _, m := range res.Messages {
		raw, _ := json.Marshal(m)
		b.db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   row.ChannelID,
			WorkspaceID: b.workspaceID,
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(raw),
			CreatedAt:   time.Now().Unix(),
		})
		if m.Timestamp > maxTS {
			maxTS = m.Timestamp
		}
		if m.ThreadTimestamp != "" {
			b.mu.Lock()
			b.discoveredThreads[threadKey{ChannelID: row.ChannelID, ThreadTS: m.ThreadTimestamp}] = struct{}{}
			b.mu.Unlock()
		}
	}

	// Wall-clock freshness bump (unchanged behavior). The UI uses
	// this to decide whether to spin or show the cache.
	b.db.SetChannelSyncedAt(row.ChannelID, time.Now().Unix())

	// Watermark advance: only when we know we got everything. A
	// capped batch means there are still messages in (maxTS, now)
	// the API didn't give us; advancing past oldest would skip them
	// on the next reconnect.
	if !res.Capped && maxTS != "" {
		if _, err := b.db.AdvanceChannelLatestSyncedTS(row.ChannelID, maxTS); err != nil {
			debuglog.Backfill("team=%s channel=%s advance watermark err=%v", b.workspaceID, row.ChannelID, err)
		}
	}

	capStr := ""
	if res.Capped {
		capStr = " capped=true"
	}
	debuglog.Backfill("team=%s channel=%s oldest=%s count=%d max_ts=%s dur_ms=%d%s",
		b.workspaceID, row.ChannelID, oldest, len(res.Messages), maxTS, time.Since(start).Milliseconds(), capStr)
	return len(res.Messages), nil
}
```

Add `"fmt"` to the imports if it's not already present (it likely is via other code in the file; verify).

The `cache.ChannelSyncRow.SyncedAt` field is no longer read by this function (we go through `GetChannelWatermark` instead). The field is still populated by `ChannelsWithMessages`/`BackfillCandidates` and might be useful for future telemetry; leave it on the struct for now.

- [ ] **Step 3.9: Run the two new tests again, verify both pass**

Run: `go test ./cmd/slk/ -run 'TestBackfill_CapHit_DoesNotAdvanceWatermark|TestBackfill_FullFetch_AdvancesWatermarkToMaxTS' -v`
Expected: both PASS.

- [ ] **Step 3.10: Run the full backfill test suite to verify no regressions**

Run: `go test ./cmd/slk/ -v -run TestBackfill`
Expected: all PASS. If a pre-existing test depended on the old `[]slack.Message` return shape, update it to read from `result.Messages`.

- [ ] **Step 3.11: Run the full test suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 3.12: Commit**

```bash
git add internal/slack/client.go cmd/slk/reconnect_backfill.go cmd/slk/reconnect_backfill_test.go
git commit -m "fix(sync): only advance backfill watermark on complete batches

GetHistorySince now returns HistorySinceResult with a Capped flag so
callers can tell 'got everything' from 'hit the cap, more remains.'
backfillOneChannel uses the new latest_synced_ts watermark via
GetChannelWatermark and only advances it when Capped == false. The
wall-clock synced_at is still bumped on every batch for UI freshness.

Fixes the silent-message-drop bug where a capped backfill would
advance the cursor as if everything had been fetched, permanently
hiding the un-fetched window."
```

---

## Task 4: Broaden the backfill candidate set via `client.counts`

**Why:** This is the second half of the user's reported bug. `ChannelsWithMessages` excludes channels with zero cached messages, so a brand-new DM that arrived during the overnight outage is invisible to the backfill. We replace it with a candidate set that's the **union** of (channels we have cached messages for) and (channels Slack's `client.counts` says have unreads). The existing `GetUnreadCounts` method on `*slack.Client` is the perfect source.

**Files:**
- Modify: `internal/cache/channels_sync.go` (add `BackfillCandidates`).
- Create: `internal/cache/channels_sync_test.go`.
- Modify: `cmd/slk/reconnect_backfill.go` (interface, candidate-set construction).
- Modify: `cmd/slk/reconnect_backfill_test.go` (extend fake, new test).

### Steps

- [ ] **Step 4.1: Add `BackfillCandidates` to `internal/cache/channels_sync.go`**

Append to the existing file:

```go
// BackfillCandidates returns the union of (channels with at least one
// cached message) and (channels listed in `unreadChannelIDs`),
// de-duplicated and stable-sorted by channel ID. SyncedAt on the
// returned rows is the channel's wall-clock synced_at (0 for channels
// not in the channels table). The caller resolves the ts watermark
// per-row via GetChannelWatermark.
//
// This is the reconnect-backfill driver's source of truth: the cached
// branch covers the steady-state "catch up on rooms I read regularly,"
// the unread branch covers "I was offline and got a DM in a room I've
// never opened."
func (db *DB) BackfillCandidates(workspaceID string, unreadChannelIDs []string) ([]ChannelSyncRow, error) {
	seen := make(map[string]int64, 32) // channelID -> synced_at (0 if not in channels table)

	cached, err := db.ChannelsWithMessages(workspaceID)
	if err != nil {
		return nil, err
	}
	for _, r := range cached {
		seen[r.ChannelID] = r.SyncedAt
	}

	for _, id := range unreadChannelIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		// Look up synced_at if the channel row exists. A miss leaves
		// SyncedAt at 0, which GetChannelWatermark handles correctly.
		var sa int64
		_ = db.conn.QueryRow(
			`SELECT synced_at FROM channels WHERE id = ?`, id,
		).Scan(&sa)
		seen[id] = sa
	}

	out := make([]ChannelSyncRow, 0, len(seen))
	for id, sa := range seen {
		out = append(out, ChannelSyncRow{ChannelID: id, SyncedAt: sa})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out, nil
}
```

Add `"sort"` to the imports.

- [ ] **Step 4.2: Write the test for `BackfillCandidates`**

Create `internal/cache/channels_sync_test.go`:

```go
package cache_test

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

func TestBackfillCandidates_UnionOfCachedAndUnread(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	// C1: cached, no unread. Will appear via ChannelsWithMessages.
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert C1: %v", err)
	}
	if err := db.SetChannelSyncedAt("C1", 1700000050); err != nil {
		t.Fatalf("synced_at C1: %v", err)
	}
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000010.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "x", CreatedAt: 1700000010,
	}); err != nil {
		t.Fatalf("upsert message C1: %v", err)
	}

	// D1: unread DM, never opened in slk. Will appear via unread set.
	// We do NOT pre-insert the channel row to simulate the
	// "completely new" case.

	// C2: cached AND unread (no double-count expected).
	if err := db.UpsertChannel(cache.Channel{ID: "C2", WorkspaceID: "T1", Name: "random", Type: "channel"}); err != nil {
		t.Fatalf("upsert C2: %v", err)
	}
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000020.000000", ChannelID: "C2", WorkspaceID: "T1",
		UserID: "U1", Text: "x", CreatedAt: 1700000020,
	}); err != nil {
		t.Fatalf("upsert message C2: %v", err)
	}

	got, err := db.BackfillCandidates("T1", []string{"D1", "C2"})
	if err != nil {
		t.Fatalf("BackfillCandidates: %v", err)
	}

	want := map[string]bool{"C1": true, "C2": true, "D1": true}
	if len(got) != 3 {
		t.Fatalf("len got = %d, want 3 (rows=%+v)", len(got), got)
	}
	for _, r := range got {
		if !want[r.ChannelID] {
			t.Errorf("unexpected channel %q in candidates", r.ChannelID)
		}
		delete(want, r.ChannelID)
	}
	for missing := range want {
		t.Errorf("expected channel %q in candidates, missing", missing)
	}
}

func TestBackfillCandidates_EmptyUnreadList(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000000.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "x", CreatedAt: 1700000000,
	}); err != nil {
		t.Fatalf("upsert msg: %v", err)
	}

	got, err := db.BackfillCandidates("T1", nil)
	if err != nil {
		t.Fatalf("BackfillCandidates: %v", err)
	}
	if len(got) != 1 || got[0].ChannelID != "C1" {
		t.Errorf("unexpected: %+v", got)
	}
}
```

- [ ] **Step 4.3: Run the new tests**

Run: `go test ./internal/cache/ -run TestBackfillCandidates -v`
Expected: PASS.

### 4b. Wire `GetUnreadCounts` into the backfiller

- [ ] **Step 4.4: Extend the `historyFetcher` interface to include `GetUnreadCounts`**

In `cmd/slk/reconnect_backfill.go`, update the interface (currently lines 24-28) to:

```go
type historyFetcher interface {
	GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (slackclient.HistorySinceResult, error)
	GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error)
	ListThreadSubscriptions(ctx context.Context) ([]slack.ThreadSubscriptionView, error)
	GetUnreadCounts() ([]slackclient.UnreadInfo, slackclient.ThreadsAggregate, error)
}
```

- [ ] **Step 4.5: Update `runChannelPhase` to use `BackfillCandidates`**

Replace lines 106-111 in `cmd/slk/reconnect_backfill.go`:

```go
func (b *backfiller) runChannelPhase(ctx context.Context) error {
	channels, err := b.db.ChannelsWithMessages(b.workspaceID)
	if err != nil {
		return err
	}
	debuglog.Backfill("team=%s trigger=reconnect channels=%d start", b.workspaceID, len(channels))
```

With:

```go
func (b *backfiller) runChannelPhase(ctx context.Context) error {
	// Fetch the server's unread map first so we can include channels
	// the user has never opened in slk (e.g., a brand-new DM that
	// arrived during an offline window). Failures are non-fatal: we
	// fall back to the cached-channels-only set.
	var unreadIDs []string
	if unreads, _, err := b.client.GetUnreadCounts(); err != nil {
		debuglog.Backfill("team=%s GetUnreadCounts err=%v (falling back to cached-only)", b.workspaceID, err)
	} else {
		for _, u := range unreads {
			if u.HasUnread {
				unreadIDs = append(unreadIDs, u.ChannelID)
			}
		}
	}

	channels, err := b.db.BackfillCandidates(b.workspaceID, unreadIDs)
	if err != nil {
		return err
	}
	debuglog.Backfill("team=%s trigger=reconnect channels=%d unread_only=%d start",
		b.workspaceID, len(channels), len(unreadIDs))
```

The rest of `runChannelPhase` is unchanged.

### 4c. Test the new-DM case

- [ ] **Step 4.6: Extend `fakeHistory` to support `GetUnreadCounts`**

In `cmd/slk/reconnect_backfill_test.go`, find the existing `fakeHistory` definition. Add a field:

```go
type fakeHistory struct {
	// ... existing fields ...
	unreads    []slackclient.UnreadInfo
	threadsAgg slackclient.ThreadsAggregate
}
```

And add the method:

```go
func (f *fakeHistory) GetUnreadCounts() ([]slackclient.UnreadInfo, slackclient.ThreadsAggregate, error) {
	return f.unreads, f.threadsAgg, nil
}
```

- [ ] **Step 4.7: Write the failing test for "new DM during offline window is backfilled"**

Append to `cmd/slk/reconnect_backfill_test.go`:

```go
func TestBackfill_NewDM_NoCachedMessages_IsCaughtUp(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// D1 is a DM that has never been opened in slk: no UpsertChannel,
	// no cached messages. Slack tells us via client.counts that it
	// has unreads.

	// The fake responds to GetHistorySince("D1", "", ...) with one
	// message (the new DM that arrived during the outage).
	fh := &fakeHistory{
		history: map[string][]slack.Message{
			"D1": {{Msg: slack.Msg{
				Type:      "message",
				Timestamp: "1700009999.000001",
				User:      "U_FRIEND",
				Text:      "hey, you up?",
			}}},
		},
		unreads: []slackclient.UnreadInfo{
			{ChannelID: "D1", HasUnread: true, Count: 1, LastRead: "0"},
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 1, 10, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, err := db.GetMessages("D1", 10, "")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 cached message for D1, got %d", len(msgs))
	}
	if msgs[0].TS != "1700009999.000001" {
		t.Errorf("wrong ts: got %q want %q", msgs[0].TS, "1700009999.000001")
	}
}
```

(Adjust to match the exact signature of `db.GetMessages` — if it returns `[]cache.Message`, this is correct; if it uses a different name like `ListMessages`, swap accordingly.)

- [ ] **Step 4.8: Run the new test, verify it fails**

Run: `go test ./cmd/slk/ -run TestBackfill_NewDM_NoCachedMessages_IsCaughtUp -v`
Expected: FAIL — `len(msgs) == 0`, because the current code excludes D1 from the candidate set.

- [ ] **Step 4.9: With step 4.5 in place, re-run the test**

Steps 4.5 should already make this pass. If the build is broken because `slackclient.HistorySinceResult` isn't an exported type yet, double-check step 3.1 was applied.

Run: `go test ./cmd/slk/ -run TestBackfill_NewDM_NoCachedMessages_IsCaughtUp -v`
Expected: PASS.

- [ ] **Step 4.10: Run the full backfill suite**

Run: `go test ./cmd/slk/ -v -run TestBackfill`
Expected: PASS.

- [ ] **Step 4.11: Run the full test suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 4.12: Commit**

```bash
git add internal/cache/channels_sync.go internal/cache/channels_sync_test.go cmd/slk/reconnect_backfill.go cmd/slk/reconnect_backfill_test.go
git commit -m "fix(sync): include unread-but-uncached channels in reconnect backfill

runChannelPhase now calls client.counts first and unions the result
with the cached-channels list before backfilling. This fixes the case
where a brand-new DM arrives during an offline window: the channel
has no cached messages, so the old ChannelsWithMessages query
excluded it from backfill entirely, and the missed message would only
ever surface if a subsequent message in the same DM happened to land
during a live WS session.

Existing GetUnreadCounts wrapper is reused — no new endpoint."
```

---

## Task 5: Integration test for the overnight-suspend scenario

**Why:** Lock in the fix with a single end-to-end test that mirrors the user's reported failure mode. This test should fail on any future regression that re-introduces the bug.

**Files:**
- Modify: `cmd/slk/reconnect_backfill_test.go` (one new test).

### Steps

- [ ] **Step 5.1: Write the integration test**

Append to `cmd/slk/reconnect_backfill_test.go`:

```go
// TestBackfill_OvernightSuspendScenario reproduces the user-reported
// "left slk open, laptop suspended for 8 hours, sync was wrong"
// failure. It exercises three different channel categories
// simultaneously to catch any regression in the new watermark +
// candidate-set logic.
func TestBackfill_OvernightSuspendScenario(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// --- Setup: pre-suspend state ---

	// A: an active channel with cached history and a recent watermark.
	if err := db.UpsertChannel(cache.Channel{ID: "A", WorkspaceID: "T1", Name: "team-eng", Type: "channel", IsMember: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(cache.Message{TS: "1700000100.000000", ChannelID: "A", WorkspaceID: "T1", UserID: "U1", Text: "before suspend", CreatedAt: 1700000100}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetChannelLatestSyncedTS("A", "1700000100.000000"); err != nil {
		t.Fatal(err)
	}

	// B: a brand-new DM (no UpsertChannel, no messages). Arrived
	// during the offline window.
	// (Nothing to set up here — the absence IS the setup.)

	// C: a quiet channel cached weeks ago, no activity overnight.
	if err := db.UpsertChannel(cache.Channel{ID: "C", WorkspaceID: "T1", Name: "off-topic", Type: "channel", IsMember: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(cache.Message{TS: "1690000000.000000", ChannelID: "C", WorkspaceID: "T1", UserID: "U2", Text: "old chatter", CreatedAt: 1690000000}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetChannelLatestSyncedTS("C", "1690000000.000000"); err != nil {
		t.Fatal(err)
	}

	// --- Server state at wake-up ---

	fh := &fakeHistory{
		history: map[string][]slack.Message{
			// A got 5 new messages during the offline window. The
			// API will return them all (cap is 100), so the watermark
			// must advance to the highest ts.
			"A": makeFakeMessages("1700008000", 5),

			// B got 1 message — the new DM the user must not miss.
			"B": {{Msg: slack.Msg{
				Type: "message", Timestamp: "1700008500.000000",
				User: "U_FRIEND", Text: "first time DM",
			}}},

			// C had no new activity. The fake returns empty for "C".
		},
		unreads: []slackclient.UnreadInfo{
			{ChannelID: "A", HasUnread: true, Count: 5, LastRead: "1700000100.000000"},
			{ChannelID: "B", HasUnread: true, Count: 1, LastRead: "0"},
			// C is not in unreads → must still be backfilled because
			// it's in the cached-channels set, even though the result
			// will be empty.
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 4, 100, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	// --- Post-conditions ---

	// A: all 5 new messages cached, watermark advanced to the highest.
	if msgs, _ := db.GetMessages("A", 100, ""); len(msgs) != 6 { // 1 pre + 5 new
		t.Errorf("A: expected 6 messages, got %d", len(msgs))
	}
	if got := db.GetChannelLatestSyncedTS("A"); got != "1700008000.000004" {
		t.Errorf("A watermark: got %q want %q", got, "1700008000.000004")
	}

	// B: the new DM is in the cache. Watermark advanced to its ts.
	if msgs, _ := db.GetMessages("B", 10, ""); len(msgs) != 1 || msgs[0].TS != "1700008500.000000" {
		t.Errorf("B: missing or wrong message; got %+v", msgs)
	}

	// C: untouched cache, watermark NOT regressed.
	if msgs, _ := db.GetMessages("C", 10, ""); len(msgs) != 1 {
		t.Errorf("C: cache disturbed; got %d msgs", len(msgs))
	}
	if got := db.GetChannelLatestSyncedTS("C"); got != "1690000000.000000" {
		t.Errorf("C watermark regressed: got %q want %q", got, "1690000000.000000")
	}
}
```

- [ ] **Step 5.2: Run the test, verify it passes**

Run: `go test ./cmd/slk/ -run TestBackfill_OvernightSuspendScenario -v`
Expected: PASS.

- [ ] **Step 5.3: Run the full suite once more**

Run: `make test`
Expected: PASS.

- [ ] **Step 5.4: Run the linter**

Run: `make lint`
Expected: PASS (or pre-existing warnings unrelated to this change). Fix any new warnings introduced by this plan.

- [ ] **Step 5.5: Build and smoke-run**

Run: `make build`
Expected: clean build of `bin/slk`.

Then run `./bin/slk` against a real workspace, leave it for a few minutes, kill your wifi briefly, reconnect, and grep the debug log:

```bash
SLK_DEBUG=1 ./bin/slk
# In another shell after a wifi-toggle:
grep '\[backfill\]' slk-debug.log | tail -50
```

Expected log lines: `channels=N unread_only=M` (M > 0 if you had unreads); per-channel lines with `max_ts=...` populated and (when not capped) no `capped=true` suffix.

- [ ] **Step 5.6: Commit**

```bash
git add cmd/slk/reconnect_backfill_test.go
git commit -m "test(sync): pin down the overnight-suspend scenario end-to-end

Regression test that exercises three channel categories at once
(active+cached, brand-new-DM, quiet+cached). Future changes to the
watermark or candidate-set logic that re-introduce silent drops or
the 'never-opened channels are skipped' bug will fail this test."
```

---

## Task 6 (Optional, deferred): Gap markers for the UI

**Why:** When a backfill batch is capped, we know there are missing messages but the UI has no way to surface that. A `channel_gaps` table + a "load earlier" affordance would close the loop.

**Status:** Deferred. The bug-fix work above (Tasks 1–5) is sufficient to fix the reported issue and the most likely follow-on cases. Gap-marker UI is a separate feature with its own design surface (where to render the marker, how to manually trigger a fill, whether gaps survive cache compaction, etc.) and belongs in a follow-up plan.

If/when you do this:
- Add table `channel_gaps(workspace_id, channel_id, gap_top_ts, gap_bottom_ts, created_at)` keyed on `(channel_id, gap_top_ts)`.
- In `backfillOneChannel`, when `res.Capped`, insert a row with `gap_top_ts = maxTS` and `gap_bottom_ts = oldest`.
- UI: a horizontal rule between the cached message at `gap_bottom_ts` and the next-newer cached message, labeled "Earlier messages unavailable — press <key> to load."
- Foreground "load more" issues a `GetHistorySince(channelID, gap.gap_bottom_ts, ...)` call; on completion delete the gap row.

---

## Verification checklist (run after Task 5 commit)

- [ ] `make test` — green.
- [ ] `make lint` — green (or only pre-existing warnings).
- [ ] `make build` — clean.
- [ ] Manual: kill wifi for 5+ minutes, restore, check `[backfill]` debug lines show `unread_only=` count > 0 if you had unreads.
- [ ] Manual: confirm the messages that arrived during the wifi-off window are present in their respective channels.
- [ ] Manual: confirm `bin/slk` does NOT show `capped=true` for any normal-volume channel.

---

## Non-goals / explicit deferrals

- **Replacing `synced_at` with `latest_synced_ts` everywhere.** The wall-clock `synced_at` keeps its current role as a UI cache-freshness indicator (`internal/ui/app.go:1497`). It conveys "how recently did anything happen here," which is genuinely different from the new ts-based sync watermark.
- **Gap-marker UI.** See Task 6.
- **Clock-jump / suspend detection.** Not needed if WS disconnect-on-suspend works (it does on the user's setup — they confirmed wifi is off during suspend, so TCP fails and reconnect fires).
- **Removing the per-channel 500 cap.** With the new advancement rule, a capped batch is recoverable: on the next reconnect the cursor hasn't moved, so the missed range is re-fetched. Removing the cap would simplify reasoning but increases the cost of large catch-ups on first reconnect; keep it for now.
- **Rate-limit-aware concurrency tuning.** The existing 4-wide semaphore is unchanged. If `make lint` or production logs show rate-limit retries dominating, a follow-up plan can reduce concurrency or add exponential backoff per-channel.
