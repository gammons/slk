# Mention Badges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a per-channel direct-mention count badge in the slk sidebar, so an unread channel is distinguishable from one where the user was @-mentioned.

**Architecture:** Slack's `client.counts` supplies authoritative per-channel `mention_count`, persisted to a new `channels.mention_count` column and read by the sidebar through the existing `readStateReader` closure. Incoming `message` WebSocket events increment the count locally so badges appear live between server refreshes. The badge replaces the trailing unread dot on the row.

**Tech Stack:** Go, bubbletea, lipgloss v2, modernc.org/sqlite, stdlib `testing`.

**Design doc:** `docs/superpowers/specs/2026-09-09-mention-badges-design.md`

**Workspace:** all work happens in the `feat/mention-badges` worktree at
`/home/dev/local_code/slk/.worktrees/feat-mention-badges`, branched from `main`
at `8337707`. Do not commit to `main`.

## Global Constraints

- Tests are plain `testing.T`, stdlib only. No testify, no gomock, no golden libraries.
- Tests are white-box by convention: `package cache`, not `package cache_test`.
- `internal/ui` must not import networking (`internal/slack`, `slackhttp`, `net/http`, `slack-go`). The badge feature adds no I/O to `internal/ui`.
- Before every commit: `go build ./...`, `go vet ./...`, and `gofmt -l cmd internal` must be empty.
- Full verification (run at least at the end of each task): `go test ./... -race`.
- **`gofmt -l .` is NOT usable in this checkout.** It reports ~58 unformatted
  files inside `.worktrees/`, which holds gitignored sibling git worktrees
  (`.gitignore:8`). Those files are pre-existing, belong to other branches, and
  must not be touched. AGENTS.md documents the bare `gofmt -l .` form; scope it
  to `cmd internal` instead. The main tree is currently clean under that
  scoping — verified before this plan was written.
- `internal/slack/events.go` declares `package slackclient`, not `package slack`. It imports `github.com/slack-go/slack` under the alias `slack`.
- `boolToInt` lives in `internal/cache/channels.go:250`, not `db.go`.
- The sidebar render entry point is `View(height, width int) string`. There is no `Render`.
- Commit messages follow the repo style: lowercase `type: subject`, e.g. `feat: add mention badge to sidebar rows`.

## Task Dependency Order

Tasks 1-2 are standalone. Task 3 precedes 4. Tasks 5, 6, 7, 8 all require 4. Task 10 requires 4 and 9. Task 0 is a blocking investigation that gates Tasks 5 and 6.

---

### Task 0: Verify unverified Slack payload fields

Tasks 5 and 6 write code against two fields this repo has never captured:
`mention_count` on the `ims` block of `client.counts`, and `mention_count` on
`channel_marked` / `im_marked` / `group_marked` / `mpim_marked` events.

This task produces evidence, not code. It exists because discovering the field
is absent *during* Task 6 means reworking Task 6, while discovering it now
costs one run of the app.

**Files:**
- No production changes. Record findings in this plan file under "Findings" below.

- [ ] **Step 1: Capture a live `client.counts` response**

Run slk with WebSocket and cache debug logging enabled:

```bash
SLK_DEBUG=ws,cache go run ./cmd/slk 2>/tmp/slk-debug.log
```

Leave it running long enough to boot fully, then quit. If `client.counts` output
is not in the log, add a temporary one-line dump at the top of
`GetUnreadCounts` in `internal/slack/client.go`, immediately after
`body, err := io.ReadAll(resp.Body)` (currently line 974):

```go
	log.Printf("RAW client.counts: %s", string(body))
```

Re-run, capture, then **revert that line** before proceeding.

- [ ] **Step 2: Record whether `ims[]` carries `mention_count`**

Inspect the captured JSON's `ims` array.

Expected if present: `{"id":"D…","has_unreads":true,"mention_count":3,"last_read":"…"}`

Write the answer into the Findings section at the bottom of this file.

- [ ] **Step 3: Capture a `*_marked` event**

With `SLK_DEBUG=ws` running, open a channel that has unread messages in the
official Slack client (or read one there), which makes Slack push a
`channel_marked` to slk. Find the corresponding raw frame. If the debug line at
`internal/slack/events.go:364-365` does not print the raw payload, add a
temporary dump in `HandleEvent` where `data` is still in scope, then revert it.

- [ ] **Step 4: Record whether `*_marked` carries `mention_count`**

Write the answer into Findings.

- [ ] **Step 5: Decide the fallback if either field is absent**

- If `ims[].mention_count` is **absent**: DMs cannot badge by message count from
  the server. Fall back to `unread_count_display` on the `ims` block if present;
  if that is also absent, DM badges come only from the local increment path
  (Task 8), and Task 5 sets DM `MentionCount` to `0`. Record the choice.
- If `*_marked.mention_count` is **absent**: Task 6 reduces to deriving the
  count from `unread_count_display == 0` — i.e. a read event zeroes the mention
  count, a remote mark-unread leaves it alone. Record the choice and simplify
  Task 6 accordingly.

- [ ] **Step 6: Confirm the working tree is clean**

Any temporary logging must be reverted. This task commits nothing except the
Findings edit.

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
git status --short
```

Expected: only `docs/superpowers/plans/2026-09-09-mention-badges.md` modified.

- [ ] **Step 7: Commit the findings**

```bash
git add docs/superpowers/plans/2026-09-09-mention-badges.md
git commit -m "docs: record Slack mention_count payload findings"
```

---

### Task 1: `internal/mention` package

Extracts the mention predicate currently embedded in `notify.ShouldNotify` so
both the notification policy and the read-state path can share it. Nothing
consumes it yet; Task 2 rewires `notify`, Task 8 rewires `OnMessage`.

**Files:**
- Create: `internal/mention/mention.go`
- Create: `internal/mention/mention_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func mention.InText(text, selfUserID string) bool`

- [ ] **Step 1: Write the failing test**

Create `internal/mention/mention_test.go`:

```go
package mention

import "testing"

func TestInText(t *testing.T) {
	const self = "U123"
	tests := []struct {
		name string
		text string
		self string
		want bool
	}{
		{"direct mention mid-sentence", "hey <@U123> take a look", self, true},
		{"direct mention alone", "<@U123>", self, true},
		{"different user", "hey <@U999> take a look", self, false},
		// The closing ">" is load-bearing: without it a self ID of U123
		// would match every user whose ID starts with U123.
		{"longer id with self as prefix", "hey <@U123ABC> hi", self, false},
		{"bare id in prose is not a mention", "hey U123 look", self, false},
		{"here broadcast", "<!here> deploying now", self, true},
		{"channel broadcast", "<!channel> all hands", self, true},
		{"everyone broadcast", "<!everyone> notice", self, true},
		// Usergroup mentions are deliberately out of scope: resolving
		// them needs the user's own group memberships, which slk lacks.
		{"usergroup labeled is not detected", "<!subteam^S1|@eng> ship it", self, false},
		{"usergroup bare is not detected", "<!subteam^S1> ship it", self, false},
		{"empty self id ignores direct mentions", "hello <@U123>", "", false},
		{"empty self id still sees broadcasts", "<!here> hi", "", true},
		{"empty text", "", self, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InText(tt.text, tt.self); got != tt.want {
				t.Errorf("InText(%q, %q) = %v, want %v", tt.text, tt.self, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/mention/ -run TestInText -v
```

Expected: build failure, `undefined: InText`.

- [ ] **Step 3: Write the implementation**

Create `internal/mention/mention.go`:

```go
// Package mention detects whether Slack message text mentions the
// authenticated user.
//
// It exists so the desktop-notification policy (internal/notify) and the
// sidebar mention badge (channels.mention_count, written from cmd/slk's
// WebSocket handler) share one definition of "does this mention me?"
// rather than each carrying its own copy.
package mention

import "strings"

// InText reports whether text contains a direct mention of selfUserID, or a
// channel-wide broadcast that includes them.
//
// Recognized forms are Slack's wire encodings: <@Uxxxx> for a direct mention
// and <!here>, <!channel>, <!everyone> for broadcasts. The angle brackets are
// required, so a bare user ID in prose is not a mention, and <@U123ABC> does
// not match a selfUserID of "U123".
//
// Usergroup mentions (<!subteam^Sxxxx>) are deliberately NOT detected.
// Resolving one requires knowing the user's own usergroup memberships, which
// slk does not have: boot.Subteams.Self (internal/slack/boot/boot.go:203) is
// untyped because no capture with a non-empty list has ever been observed.
// The consequence is a possible undercount, never an overcount, and it is
// corrected by the next client.counts refresh. See
// docs/superpowers/specs/2026-09-09-mention-badges-design.md.
//
// An empty selfUserID matches no direct mention; broadcasts still match.
func InText(text, selfUserID string) bool {
	if selfUserID != "" && strings.Contains(text, "<@"+selfUserID+">") {
		return true
	}
	return strings.Contains(text, "<!here>") ||
		strings.Contains(text, "<!channel>") ||
		strings.Contains(text, "<!everyone>")
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/mention/ -run TestInText -v
```

Expected: PASS, all 13 subtests.

- [ ] **Step 5: Verify build, vet, and format**

```bash
go build ./... && go vet ./internal/mention/ && gofmt -l internal/mention/
```

Expected: no output from any of the three.

- [ ] **Step 6: Commit**

```bash
git add internal/mention/
git commit -m "feat: add internal/mention shared mention predicate"
```

---

### Task 2: Refactor `notify.ShouldNotify` onto `mention.InText`

Removes the duplicate predicate. The existing `internal/notify` tests are the
regression proof: they must pass with zero modifications.

**Files:**
- Modify: `internal/notify/notifier.go:93-99`

**Interfaces:**
- Consumes: `mention.InText` from Task 1.
- Produces: no signature changes. `ShouldNotify` keeps its exact behaviour.

- [ ] **Step 1: Confirm the existing tests pass before touching anything**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/notify/ -v 2>&1 | tail -30
```

Expected: PASS. Note the count of tests run — the same set must pass in Step 4.

- [ ] **Step 2: Replace the inline predicate**

In `internal/notify/notifier.go`, find this block (currently lines 93-99):

```go
	// Check mention trigger
	if ctx.OnMention && (strings.Contains(text, "<@"+ctx.CurrentUserID+">") ||
		strings.Contains(text, "<!here>") ||
		strings.Contains(text, "<!channel>") ||
		strings.Contains(text, "<!everyone>")) {
		return true
	}
```

Replace it with:

```go
	// Check mention trigger. The predicate lives in internal/mention so
	// the sidebar's mention badge applies the same rule; the policy
	// around it (OnMention, DND, mute, active channel) stays here.
	if ctx.OnMention && mention.InText(text, ctx.CurrentUserID) {
		return true
	}
```

- [ ] **Step 3: Add the import**

The import block at the top of `internal/notify/notifier.go` currently reads:

```go
import (
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/usergroups"
	"github.com/gen2brain/beeep"
)
```

Add `mention` to the local group:

```go
import (
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/mention"
	"github.com/gammons/slk/internal/usergroups"
	"github.com/gen2brain/beeep"
)
```

Note: `strings` is still used by `ShouldNotify`'s keyword loop and by
`StripSlackMarkupWithUserGroups`, so it stays.

- [ ] **Step 4: Run the notify tests unchanged**

```bash
go test ./internal/notify/ -v 2>&1 | tail -30
```

Expected: PASS, the same test set as Step 1. If `TestShouldNotify_Mention` or
`TestShouldNotify_SpecialMentions` fails, the extraction changed behaviour —
stop and reconcile rather than editing the test.

- [ ] **Step 5: Verify build, vet, and format**

```bash
go build ./... && go vet ./internal/notify/ && gofmt -l internal/notify/
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/notify/notifier.go
git commit -m "refactor: use internal/mention in notify.ShouldNotify"
```

---

### Task 3: Add the `mention_count` column, remove dead `unread_count`

Schema only. No reads, no writes, no behaviour change.

**Files:**
- Modify: `internal/cache/db.go:99-112` (CREATE TABLE), `:223-226` (migration block)
- Modify: `internal/cache/db_test.go` (append two tests)

**Interfaces:**
- Consumes: nothing.
- Produces: `channels.mention_count` column, `INTEGER NOT NULL DEFAULT 0`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cache/db_test.go`. These mirror the structure of the
existing `TestMigration_AddsHasUnreadColumn` at line 51:

```go
func TestMigration_AddsMentionCountColumn(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	rows, err := db.conn.Query("PRAGMA table_info(channels)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "mention_count" {
			found = true
			if ctype != "INTEGER" {
				t.Errorf("mention_count type = %q, want INTEGER", ctype)
			}
			if notnull != 1 {
				t.Errorf("mention_count NOT NULL = %d, want 1", notnull)
			}
			if !dflt.Valid || dflt.String != "0" {
				t.Errorf("mention_count default = %v, want 0", dflt)
			}
		}
	}
	if !found {
		t.Fatal("mention_count column not added")
	}
}

// unread_count was declared in the original schema and never read or
// written. It was removed when mention_count landed, because two
// similarly-named count columns invite a future author to pick the wrong
// one. Fresh databases must not have it. Pre-existing databases keep the
// vestigial column harmlessly; no destructive migration is performed.
func TestSchema_FreshDBHasNoUnreadCountColumn(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	rows, err := db.conn.Query("PRAGMA table_info(channels)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "unread_count" {
			t.Error("unread_count column still present in fresh schema")
		}
	}
}
```

`database/sql` and `testing` are already imported by `db_test.go` (lines 3-10),
so no import changes are needed.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/cache/ -run 'TestMigration_AddsMentionCountColumn|TestSchema_FreshDBHasNoUnreadCountColumn' -v
```

Expected: both FAIL. `TestMigration_AddsMentionCountColumn` fails with
"mention_count column not added"; `TestSchema_FreshDBHasNoUnreadCountColumn`
fails with "unread_count column still present in fresh schema".

- [ ] **Step 3: Update the CREATE TABLE statement**

In `internal/cache/db.go`, the `channels` table currently reads (lines 99-112):

```go
	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'channel',
		topic TEXT NOT NULL DEFAULT '',
		is_member INTEGER NOT NULL DEFAULT 0,
		is_starred INTEGER NOT NULL DEFAULT 0,
		last_read_ts TEXT NOT NULL DEFAULT '',
		unread_count INTEGER NOT NULL DEFAULT 0,
		has_unread INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
	);
```

Replace the `unread_count` line with `mention_count`:

```go
	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'channel',
		topic TEXT NOT NULL DEFAULT '',
		is_member INTEGER NOT NULL DEFAULT 0,
		is_starred INTEGER NOT NULL DEFAULT 0,
		last_read_ts TEXT NOT NULL DEFAULT '',
		has_unread INTEGER NOT NULL DEFAULT 0,
		mention_count INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
	);
```

- [ ] **Step 4: Add the migration for pre-existing databases**

In `internal/cache/db.go`, immediately after the `has_unread` migration block
(currently lines 223-226), add:

```go
	if err := db.addColumnIfMissing("channels", "mention_count",
		"ALTER TABLE channels ADD COLUMN mention_count INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
```

The result should read:

```go
	if err := db.addColumnIfMissing("channels", "has_unread",
		"ALTER TABLE channels ADD COLUMN has_unread INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.addColumnIfMissing("channels", "mention_count",
		"ALTER TABLE channels ADD COLUMN mention_count INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/cache/ -run 'TestMigration_AddsMentionCountColumn|TestSchema_FreshDBHasNoUnreadCountColumn' -v
```

Expected: both PASS.

- [ ] **Step 6: Run the whole cache package**

```bash
go test ./internal/cache/ -race
```

Expected: PASS. Nothing selected `unread_count`, so removing it must not break
any existing test. If something fails here, a hidden consumer exists — find it
before proceeding.

- [ ] **Step 7: Verify build, vet, and format**

```bash
go build ./... && go vet ./internal/cache/ && gofmt -l internal/cache/
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add internal/cache/db.go internal/cache/db_test.go
git commit -m "feat: add channels.mention_count, drop dead unread_count"
```

---

### Task 4: Cache read/write API for mention count

Adds the field to both read-state types, two dedicated setters, and mention
handling to the batch writers. No production caller yet — Tasks 5-8 wire them.

`UpdateChannelReadState`'s signature is deliberately untouched so its ~20
existing test callers compile unchanged.

**Files:**
- Modify: `internal/cache/channels_read_state.go` (all of it)
- Modify: `internal/cache/channels_read_state_test.go` (append tests)

**Interfaces:**
- Consumes: `channels.mention_count` from Task 3.
- Produces:
  - `cache.ReadState{LastReadTS string; HasUnread bool; MentionCount int}`
  - `cache.ChannelReadStateUpdate{ChannelID, LastReadTS string; HasUnread bool; MentionCount int}`
  - `func (db *DB) SetChannelMentionCount(channelID string, n int) error`
  - `func (db *DB) IncrementChannelMentionCount(channelID string) error`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cache/channels_read_state_test.go`. The file already has the
`newRSChannel(t, db, id, workspaceID)` helper at line 7 and imports only
`testing`.

```go
func TestSetChannelMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.SetChannelMentionCount("C1", 4); err != nil {
		t.Fatalf("SetChannelMentionCount: %v", err)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 4 {
		t.Errorf("MentionCount = %d, want 4", state.MentionCount)
	}

	// Overwrite, including back down to zero — the read paths rely on
	// this to clear a badge.
	if err := db.SetChannelMentionCount("C1", 0); err != nil {
		t.Fatalf("SetChannelMentionCount(0): %v", err)
	}
	state, err = db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 0 {
		t.Errorf("MentionCount = %d, want 0 after reset", state.MentionCount)
	}
}

func TestIncrementChannelMentionCount_AccumulatesFromZero(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// A freshly upserted channel has mention_count = 0 by column
	// default, so the first increment must yield 1 rather than error or
	// leave NULL.
	for i := 1; i <= 3; i++ {
		if err := db.IncrementChannelMentionCount("C1"); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
		state, err := db.GetChannelReadState("C1")
		if err != nil {
			t.Fatalf("GetChannelReadState: %v", err)
		}
		if state.MentionCount != i {
			t.Errorf("after %d increments MentionCount = %d, want %d", i, state.MentionCount, i)
		}
	}
}

// The mention setters must not disturb the columns that drive the unread
// dot and the "new messages" divider. An earlier design threaded mention
// count through UpdateChannelReadState; this test pins the separation.
func TestMentionSetters_DoNotDisturbReadState(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.UpdateChannelReadState("C1", "1700000000.000009", true); err != nil {
		t.Fatalf("UpdateChannelReadState: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 2); err != nil {
		t.Fatalf("SetChannelMentionCount: %v", err)
	}
	if err := db.IncrementChannelMentionCount("C1"); err != nil {
		t.Fatalf("IncrementChannelMentionCount: %v", err)
	}

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000009" {
		t.Errorf("LastReadTS = %q, want preserved 1700000000.000009", state.LastReadTS)
	}
	if !state.HasUnread {
		t.Error("HasUnread = false, want preserved true")
	}
	if state.MentionCount != 3 {
		t.Errorf("MentionCount = %d, want 3", state.MentionCount)
	}
}

func TestBatchUpdateChannelReadState_WritesMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")

	// C1 exercises the "both columns" statement (non-empty LastReadTS);
	// C2 exercises the flag-only statement (empty LastReadTS).
	updates := []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0001", HasUnread: true, MentionCount: 7},
		{ChannelID: "C2", LastReadTS: "", HasUnread: true, MentionCount: 2},
	}
	if err := db.BatchUpdateChannelReadState(updates); err != nil {
		t.Fatalf("BatchUpdateChannelReadState: %v", err)
	}

	for _, want := range []struct {
		id    string
		count int
	}{{"C1", 7}, {"C2", 2}} {
		state, err := db.GetChannelReadState(want.id)
		if err != nil {
			t.Fatalf("GetChannelReadState(%s): %v", want.id, err)
		}
		if state.MentionCount != want.count {
			t.Errorf("%s MentionCount = %d, want %d", want.id, state.MentionCount, want.count)
		}
	}
}

// Boot applies an authoritative full snapshot. A channel the user read in
// another client while slk was closed is simply absent from
// client.counts, so the workspace-wide reset must clear its mention count
// as well as its unread flag — otherwise a stale badge survives forever.
func TestReplaceWorkspaceReadState_ZeroesAbsentChannelMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")

	if err := db.SetChannelMentionCount("C1", 5); err != nil {
		t.Fatalf("seed C1: %v", err)
	}
	if err := db.SetChannelMentionCount("C2", 9); err != nil {
		t.Fatalf("seed C2: %v", err)
	}

	// Snapshot mentions C1 only. C2 must be reset.
	if err := db.ReplaceWorkspaceReadState("T1", []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0", HasUnread: true, MentionCount: 3},
	}); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}

	c1, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState(C1): %v", err)
	}
	if c1.MentionCount != 3 {
		t.Errorf("C1 MentionCount = %d, want 3", c1.MentionCount)
	}
	c2, err := db.GetChannelReadState("C2")
	if err != nil {
		t.Fatalf("GetChannelReadState(C2): %v", err)
	}
	if c2.MentionCount != 0 {
		t.Errorf("C2 MentionCount = %d, want 0 (absent from snapshot)", c2.MentionCount)
	}
}

func TestGetWorkspaceReadState_IncludesMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.SetChannelMentionCount("C1", 6); err != nil {
		t.Fatalf("SetChannelMentionCount: %v", err)
	}
	m, err := db.GetWorkspaceReadState("T1")
	if err != nil {
		t.Fatalf("GetWorkspaceReadState: %v", err)
	}
	if m["C1"].MentionCount != 6 {
		t.Errorf("C1 MentionCount = %d, want 6", m["C1"].MentionCount)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/cache/ -run 'MentionCount|MentionSetters' -v
```

Expected: build failure — `state.MentionCount` undefined, `db.SetChannelMentionCount` undefined,
`db.IncrementChannelMentionCount` undefined, unknown field `MentionCount` in `ChannelReadStateUpdate`.

- [ ] **Step 3: Add `MentionCount` to both types**

In `internal/cache/channels_read_state.go`, replace the two type declarations
(lines 8-23) with:

```go
// ReadState captures the per-channel read-state values that drive the
// unread dot, the mention badge, and the "new messages" line. It is the
// canonical type for passing read state across package boundaries.
type ReadState struct {
	LastReadTS string
	HasUnread  bool
	// MentionCount is the number of unread direct mentions: an explicit
	// @user or an @here/@channel/@everyone broadcast. For DMs and group
	// DMs, Slack's client.counts reports every unread message here, which
	// is what makes the sidebar badge match the official client without a
	// client-side branch. Rendering gates it on HasUnread.
	MentionCount int
}

// ChannelReadStateUpdate is one entry in a batched read-state write.
// LastReadTS == "" means "preserve the existing last_read_ts" (used by
// events that update has_unread only, e.g. new-message arrivals).
// MentionCount is always applied: both batch writers are fed from
// client.counts, which is authoritative.
type ChannelReadStateUpdate struct {
	ChannelID    string
	LastReadTS   string
	HasUnread    bool
	MentionCount int
}
```

- [ ] **Step 4: Add the two setters**

In `internal/cache/channels_read_state.go`, insert immediately after
`UpdateChannelReadState` (which currently ends at line 42):

```go
// SetChannelMentionCount overwrites the channel's mention count with an
// authoritative value. Callers: the *_marked WS handler (server-supplied
// count) and the read paths that clear it (markChannelReadAsync, the u-key
// mark-unread).
//
// Deliberately separate from UpdateChannelReadState rather than a
// parameter on it: "leave the mention count alone" is the common case, and
// expressing it as a parameter value would have forced ~20 existing test
// call sites to spell out a no-op.
func (db *DB) SetChannelMentionCount(channelID string, n int) error {
	if _, err := db.conn.Exec(
		`UPDATE channels SET mention_count = ? WHERE id = ?`,
		n, channelID,
	); err != nil {
		return fmt.Errorf("setting channel mention count: %w", err)
	}
	return nil
}

// IncrementChannelMentionCount adds one to the channel's mention count.
// Used by the inbound-message path, which detects mentions locally and has
// no server-supplied total to set. The addition happens in SQL so it is
// atomic and cannot lose a concurrent update.
func (db *DB) IncrementChannelMentionCount(channelID string) error {
	if _, err := db.conn.Exec(
		`UPDATE channels SET mention_count = mention_count + 1 WHERE id = ?`,
		channelID,
	); err != nil {
		return fmt.Errorf("incrementing channel mention count: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Add mention count to `BatchUpdateChannelReadState`**

Replace the two `tx.Prepare` calls and the loop body inside
`BatchUpdateChannelReadState` (currently lines 54-79) with:

```go
	stmtBoth, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare both: %w", err)
	}
	defer stmtBoth.Close()
	stmtFlag, err := tx.Prepare(`UPDATE channels SET has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare flag: %w", err)
	}
	defer stmtFlag.Close()

	for _, u := range updates {
		if u.LastReadTS == "" {
			if _, err := stmtFlag.Exec(boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				tx.Rollback()
				return fmt.Errorf("batch flag for %s: %w", u.ChannelID, err)
			}
		} else {
			if _, err := stmtBoth.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				tx.Rollback()
				return fmt.Errorf("batch both for %s: %w", u.ChannelID, err)
			}
		}
	}
```

- [ ] **Step 6: Add mention count to `ReplaceWorkspaceReadState`**

Two changes inside `ReplaceWorkspaceReadState`.

First, extend the workspace-wide reset (currently lines 115-120):

```go
	if _, err := tx.Exec(
		`UPDATE channels SET has_unread = 0, mention_count = 0 WHERE workspace_id = ?`,
		workspaceID,
	); err != nil {
		return fmt.Errorf("reset workspace unread: %w", err)
	}
```

Second, extend the prepared statements and loop (currently lines 122-143):

```go
	stmtBoth, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare both: %w", err)
	}
	defer stmtBoth.Close()
	stmtFlag, err := tx.Prepare(`UPDATE channels SET has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare flag: %w", err)
	}
	defer stmtFlag.Close()

	for _, u := range updates {
		if u.LastReadTS == "" {
			if _, err := stmtFlag.Exec(boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				return fmt.Errorf("replace flag for %s: %w", u.ChannelID, err)
			}
		} else {
			if _, err := stmtBoth.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				return fmt.Errorf("replace both for %s: %w", u.ChannelID, err)
			}
		}
	}
```

Also update the doc comment above the function: the sentence reading
"it first resets has_unread=0 for EVERY channel in the workspace" becomes
"it first resets has_unread=0 and mention_count=0 for EVERY channel in the
workspace".

- [ ] **Step 7: Add mention count to both readers**

Replace `GetChannelReadState` (currently lines 150-166) with:

```go
// GetChannelReadState returns the read state for a single channel.
// A missing row yields a zero-valued ReadState and a nil error.
func (db *DB) GetChannelReadState(channelID string) (ReadState, error) {
	var lastReadTS string
	var hasUnread, mentionCount int
	err := db.conn.QueryRow(
		`SELECT last_read_ts, has_unread, mention_count FROM channels WHERE id = ?`,
		channelID,
	).Scan(&lastReadTS, &hasUnread, &mentionCount)
	if err == sql.ErrNoRows {
		return ReadState{}, nil
	}
	if err != nil {
		return ReadState{}, fmt.Errorf("getting channel read state: %w", err)
	}
	return ReadState{
		LastReadTS:   lastReadTS,
		HasUnread:    hasUnread == 1,
		MentionCount: mentionCount,
	}, nil
}
```

Replace `GetWorkspaceReadState` (currently lines 168-190) with:

```go
// GetWorkspaceReadState returns channelID -> ReadState for every
// channel in the workspace. Single batched query. Called by the
// sidebar View() at render time.
func (db *DB) GetWorkspaceReadState(workspaceID string) (map[string]ReadState, error) {
	rows, err := db.conn.Query(
		`SELECT id, last_read_ts, has_unread, mention_count FROM channels WHERE workspace_id = ?`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query workspace read state: %w", err)
	}
	defer rows.Close()
	out := make(map[string]ReadState)
	for rows.Next() {
		var id, lastRead string
		var hasUnread, mentionCount int
		if err := rows.Scan(&id, &lastRead, &hasUnread, &mentionCount); err != nil {
			return nil, fmt.Errorf("scan workspace read state: %w", err)
		}
		out[id] = ReadState{
			LastReadTS:   lastRead,
			HasUnread:    hasUnread == 1,
			MentionCount: mentionCount,
		}
	}
	return out, rows.Err()
}
```

- [ ] **Step 8: Run the new tests to verify they pass**

```bash
go test ./internal/cache/ -run 'MentionCount|MentionSetters' -v
```

Expected: all PASS.

- [ ] **Step 9: Run the whole cache package with race detection**

```bash
go test ./internal/cache/ -race
```

Expected: PASS. All pre-existing read-state tests must pass untouched — that is
the proof `UpdateChannelReadState`'s contract did not change.

- [ ] **Step 10: Verify build, vet, and format**

```bash
go build ./... && go vet ./internal/cache/ && gofmt -l internal/cache/
```

Expected: no output.

- [ ] **Step 11: Commit**

```bash
git add internal/cache/channels_read_state.go internal/cache/channels_read_state_test.go
git commit -m "feat: mention count read/write API in cache read state"
```

---

### Task 5: Carry server mention counts from `client.counts` to the DB

Renames the dead `Count` field to `MentionCount` through the transport types,
parses `mention_count` for the `ims` block, and applies it on both the boot and
reconnect paths.

**Prerequisite:** Task 0 Step 2 must have determined whether `ims[]` carries
`mention_count`. If it does not, follow the fallback recorded in Task 0 Step 5.

**Files:**
- Modify: `internal/slack/client.go:931-937` (`UnreadInfo`), `:994-998` (ims parse), `:1019-1055` (assembly)
- Modify: `internal/bootstrap/bootstrap.go:146-151` (`Unread`)
- Modify: `cmd/slk/bootstrap_adapters.go:116-123` (adapter loop)
- Modify: `cmd/slk/main.go:2609-2616` (boot snapshot)
- Modify: `cmd/slk/reconnect_sync.go:137-144` (reconnect batch)
- Modify: `internal/slack/client_test.go` (append a parse test)

**Interfaces:**
- Consumes: `cache.ChannelReadStateUpdate.MentionCount` from Task 4.
- Produces:
  - `slack.UnreadInfo{ChannelID string; MentionCount int; HasUnread bool; LastRead string}`
  - `bootstrap.Unread{ChannelID string; MentionCount int; HasUnread bool; LastRead string}`

- [ ] **Step 1: Write the failing parse test**

Append to `internal/slack/client_test.go`. Match the file's existing fake-server
helper names — if `newFakeSlack` / `newTestClient` are not present in this file,
mirror whichever helper the neighbouring `GetUnreadCounts`-adjacent tests use.

```go
// client.counts is the only source of authoritative mention counts.
// mention_count means "@-mentions" for channels and mpims, and "every
// unread message" for ims — Slack's server encodes the DM special case
// for us, so slk consumes one field uniformly.
func TestGetUnreadCounts_ParsesMentionCounts(t *testing.T) {
	const body = `{
	  "ok": true,
	  "channels": [
	    {"id":"C1","has_unreads":true,"mention_count":2,"unread_count_display":9,"last_read":"1.0"},
	    {"id":"C2","has_unreads":true,"mention_count":0,"last_read":"2.0"},
	    {"id":"C3","has_unreads":false,"mention_count":0,"last_read":"3.0"}
	  ],
	  "mpims": [{"id":"G1","has_unreads":true,"mention_count":4,"last_read":"4.0"}],
	  "ims": [{"id":"D1","has_unreads":true,"mention_count":6,"last_read":"5.0"}],
	  "threads": {"has_unreads":false,"unread_count":0,"mention_count":0}
	}`
	srv := newFakeSlack(t, map[string]string{"/api/client.counts": body})
	c := newTestClient(t, srv.Server)

	unreads, _, err := c.GetUnreadCounts()
	if err != nil {
		t.Fatalf("GetUnreadCounts: %v", err)
	}

	got := map[string]int{}
	for _, u := range unreads {
		got[u.ChannelID] = u.MentionCount
	}
	want := map[string]int{
		"C1": 2,
		// An unread channel with no mentions must report 0, not a
		// floor of 1. The old code fabricated a 1 here, which would
		// have painted a "1" badge on every unread channel.
		"C2": 0,
		"C3": 0,
		"G1": 4,
		"D1": 6,
	}
	for id, wantCount := range want {
		if got[id] != wantCount {
			t.Errorf("%s MentionCount = %d, want %d", id, got[id], wantCount)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/slack/ -run TestGetUnreadCounts_ParsesMentionCounts -v
```

Expected: build failure, `u.MentionCount` undefined.

- [ ] **Step 3: Rename the field on `UnreadInfo`**

In `internal/slack/client.go`, replace the `UnreadInfo` declaration (lines 931-937):

```go
// UnreadInfo holds the unread state for a single channel.
type UnreadInfo struct {
	ChannelID string
	// MentionCount is Slack's per-conversation mention count. For
	// channels and mpims it counts @-mentions; for ims Slack reports
	// every unread message, which is exactly the DM badge semantics the
	// official client shows. Zero is meaningful: an unread channel with
	// no mentions reports 0 and must render a dot, not a "1".
	MentionCount int
	HasUnread    bool
	LastRead     string // Slack message timestamp
}
```

- [ ] **Step 4: Parse `mention_count` on the `ims` block**

In the anonymous response struct inside `GetUnreadCounts`, the `Ims` block
currently reads (lines 994-998):

```go
		Ims []struct {
			ID         string `json:"id"`
			HasUnreads bool   `json:"has_unreads"`
			LastRead   string `json:"last_read"`
		} `json:"ims"`
```

Replace with:

```go
		Ims []struct {
			ID           string `json:"id"`
			HasUnreads   bool   `json:"has_unreads"`
			MentionCount int    `json:"mention_count"`
			LastRead     string `json:"last_read"`
		} `json:"ims"`
```

- [ ] **Step 5: Replace the three assembly loops**

Replace the whole assembly block (lines 1019-1055) with:

```go
	var unreads []UnreadInfo
	// All three conversation kinds carry mention_count and are handled
	// identically. The previous code floored the value to 1 whenever a
	// conversation had unreads but no mentions, and hardcoded 1 for
	// every im — a fabrication that was invisible only because nothing
	// read the field.
	for _, ch := range result.Channels {
		unreads = append(unreads, UnreadInfo{
			ChannelID:    ch.ID,
			MentionCount: ch.MentionCount,
			HasUnread:    ch.HasUnreads,
			LastRead:     ch.LastRead,
		})
	}
	for _, ch := range result.Mpims {
		unreads = append(unreads, UnreadInfo{
			ChannelID:    ch.ID,
			MentionCount: ch.MentionCount,
			HasUnread:    ch.HasUnreads,
			LastRead:     ch.LastRead,
		})
	}
	for _, ch := range result.Ims {
		unreads = append(unreads, UnreadInfo{
			ChannelID:    ch.ID,
			MentionCount: ch.MentionCount,
			HasUnread:    ch.HasUnreads,
			LastRead:     ch.LastRead,
		})
	}
```

Note: `UnreadCountDisplay` on the `Channels` block is now parsed and unused. Leave
it in place — it documents the field's existence and costs nothing. If `go vet`
or the linter objects to an unused struct field (it does not, for anonymous
struct fields populated by `encoding/json`), leave it and record the objection.

- [ ] **Step 6: Rename the field on `bootstrap.Unread`**

In `internal/bootstrap/bootstrap.go`, replace the `Unread` declaration (lines 146-151):

```go
type Unread struct {
	ChannelID    string
	MentionCount int
	HasUnread    bool
	LastRead     string
}
```

Leave the package comment above it about import direction unchanged.

- [ ] **Step 7: Update the counts adapter**

In `cmd/slk/bootstrap_adapters.go`, the loop at lines 116-123 currently reads:

```go
	for _, u := range unreads {
		out.Unreads = append(out.Unreads, bootstrap.Unread{
			ChannelID: u.ChannelID,
			Count:     u.Count,
			HasUnread: u.HasUnread,
			LastRead:  u.LastRead,
		})
	}
```

Replace with:

```go
	for _, u := range unreads {
		out.Unreads = append(out.Unreads, bootstrap.Unread{
			ChannelID:    u.ChannelID,
			MentionCount: u.MentionCount,
			HasUnread:    u.HasUnread,
			LastRead:     u.LastRead,
		})
	}
```

- [ ] **Step 8: Apply mention counts on the boot path**

In `cmd/slk/main.go`, the boot snapshot loop at lines 2609-2616 currently reads:

```go
		updates := make([]cache.ChannelReadStateUpdate, 0, len(unreadCounts))
		for _, u := range unreadCounts {
			updates = append(updates, cache.ChannelReadStateUpdate{
				ChannelID:  u.ChannelID,
				LastReadTS: u.LastRead, // may be ""; ReplaceWorkspaceReadState preserves existing in that case
				HasUnread:  u.HasUnread,
			})
		}
```

Replace with:

```go
		updates := make([]cache.ChannelReadStateUpdate, 0, len(unreadCounts))
		for _, u := range unreadCounts {
			updates = append(updates, cache.ChannelReadStateUpdate{
				ChannelID:  u.ChannelID,
				LastReadTS: u.LastRead, // may be ""; ReplaceWorkspaceReadState preserves existing in that case
				HasUnread:  u.HasUnread,
				// Boot is the authoritative snapshot: channels absent
				// from client.counts get mention_count reset to 0 by
				// ReplaceWorkspaceReadState's workspace-wide reset.
				MentionCount: u.MentionCount,
			})
		}
```

- [ ] **Step 9: Apply mention counts on the reconnect path**

In `cmd/slk/reconnect_sync.go`, the loop at lines 137-144 currently reads:

```go
	updates := make([]cache.ChannelReadStateUpdate, 0, len(unreads))
	for _, u := range unreads {
		updates = append(updates, cache.ChannelReadStateUpdate{
			ChannelID:  u.ChannelID,
			LastReadTS: u.LastRead,
			HasUnread:  u.HasUnread,
		})
	}
```

Replace with:

```go
	updates := make([]cache.ChannelReadStateUpdate, 0, len(unreads))
	for _, u := range unreads {
		updates = append(updates, cache.ChannelReadStateUpdate{
			ChannelID:  u.ChannelID,
			LastReadTS: u.LastRead,
			HasUnread:  u.HasUnread,
			// Reconnect is additive, not a snapshot: only channels
			// client.counts names are corrected. A channel read
			// elsewhere during the outage and omitted here keeps its
			// stale badge until the next boot, matching how
			// has_unread already behaves on this path.
			MentionCount: u.MentionCount,
		})
	}
```

- [ ] **Step 10: Run the affected package tests**

```bash
go test ./internal/slack/ ./internal/bootstrap/ ./cmd/slk/ -run 'Unread|Counts|Mention' -v 2>&1 | tail -40
```

Expected: PASS, including the pre-existing
`TestCountsAdapter_CarriesUnreadsAndTheThreadRollup`, which asserts only
`ChannelID`, `HasUnread` and the threads rollup and therefore needs no edit.

- [ ] **Step 11: Verify build, vet, format, and the full suite**

```bash
go build ./... && go vet ./... && gofmt -l cmd internal && go test ./... -race 2>&1 | grep -v '^ok' | head -20
```

Expected: no output from build/vet/gofmt; no FAIL lines from the test run.

- [ ] **Step 12: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go \
        internal/bootstrap/bootstrap.go cmd/slk/bootstrap_adapters.go \
        cmd/slk/main.go cmd/slk/reconnect_sync.go
git commit -m "feat: carry server mention counts from client.counts to cache"
```

---

### Task 6: Carry `mention_count` on `*_marked` WebSocket events

`*_marked` fires in both directions — when the user reads a channel and when
they mark one unread — so it is the live correction channel for mention counts.

**Prerequisite:** Task 0 Step 4 must have determined whether these payloads
carry `mention_count`. If they do not, follow the fallback in Task 0 Step 5:
skip Steps 3-4 below, and in Step 7 derive `mentionCount` as `0` when
`unreadCount == 0` while leaving the stored count untouched otherwise.

**Files:**
- Modify: `internal/slack/events.go:193-198` (payload), `:30-36` (interface doc + signature), `:359-366` (dispatch)
- Modify: `internal/slack/events_test.go:62-66` (record struct), `:112-114` (mock method), append one test
- Modify: `cmd/slk/main.go:4372-4407` (`OnChannelMarked`)
- Modify: `cmd/slk/event_handler_marked_test.go` (4 call sites, append two tests)

**Interfaces:**
- Consumes: `cache.SetChannelMentionCount` from Task 4.
- Produces: `EventHandler.OnChannelMarked(channelID, ts string, unreadCount, mentionCount int)`

- [ ] **Step 1: Write the failing dispatch test**

Append to `internal/slack/events_test.go`:

```go
// *_marked payloads carry mention_count alongside unread_count_display.
// It is the live correction channel for the sidebar mention badge: reading
// a channel elsewhere zeroes it, a remote mark-unread restores it.
func TestDispatch_ChannelMarked_CarriesMentionCount(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"channel_marked","channel":"C123","ts":"1700000000.000100","unread_count_display":9,"mention_count":4}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	got := handler.channelMarks[0]
	if got.mentionCount != 4 {
		t.Errorf("mentionCount = %d, want 4", got.mentionCount)
	}
	if got.unreadCount != 9 {
		t.Errorf("unreadCount = %d, want 9", got.unreadCount)
	}
}

// A payload without mention_count must decode to 0 rather than fail, so an
// older or narrower Slack response degrades to "no badge" instead of
// dropping the event.
func TestDispatch_ChannelMarked_AbsentMentionCountIsZero(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"channel_marked","channel":"C123","ts":"1.0","unread_count_display":2}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	if got := handler.channelMarks[0].mentionCount; got != 0 {
		t.Errorf("mentionCount = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/slack/ -run 'TestDispatch_ChannelMarked_(CarriesMentionCount|AbsentMentionCountIsZero)' -v
```

Expected: build failure, `got.mentionCount` undefined.

- [ ] **Step 3: Add the field to the payload struct**

In `internal/slack/events.go`, replace `wsChannelMarkedEvent` (lines 190-198):

```go
// wsChannelMarkedEvent represents a channel_marked / im_marked /
// group_marked / mpim_marked event. Slack uses the same payload
// shape across all four — the type field disambiguates.
type wsChannelMarkedEvent struct {
	Type               string `json:"type"`
	Channel            string `json:"channel"`
	TS                 string `json:"ts"`
	UnreadCountDisplay int    `json:"unread_count_display"`
	// MentionCount drives the sidebar mention badge. Absent on payloads
	// that omit it, which decodes to 0 and clears the badge rather than
	// dropping the event.
	MentionCount int `json:"mention_count"`
}
```

- [ ] **Step 4: Widen the interface method and its doc**

In `internal/slack/events.go`, replace the `OnChannelMarked` declaration and its
doc comment inside the `EventHandler` interface (lines 30-36):

```go
	// OnChannelMarked is delivered when Slack pushes a channel_marked /
	// im_marked / group_marked / mpim_marked event (read state changed
	// in another client, or via slk's own MarkChannel/MarkChannelUnread
	// echoing back). ts is the new last_read watermark; unreadCount is
	// the canonical workspace-side unread count for the channel;
	// mentionCount is the canonical unread direct-mention count that
	// drives the sidebar badge.
	OnChannelMarked(channelID, ts string, unreadCount, mentionCount int)
```

- [ ] **Step 5: Pass it through the dispatcher**

In `internal/slack/events.go`, replace the dispatch case (lines 359-366):

```go
	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var evt wsChannelMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("%s: channel=%s ts=%s unread_count=%d mention_count=%d",
			evt.Type, evt.Channel, evt.TS, evt.UnreadCountDisplay, evt.MentionCount)
		handler.OnChannelMarked(evt.Channel, evt.TS, evt.UnreadCountDisplay, evt.MentionCount)
```

- [ ] **Step 6: Update the mock handler**

In `internal/slack/events_test.go`, replace `channelMarkRecord` (lines 62-66):

```go
type channelMarkRecord struct {
	channelID    string
	ts           string
	unreadCount  int
	mentionCount int
}
```

and replace the mock method (lines 112-114):

```go
func (m *mockEventHandler) OnChannelMarked(channelID, ts string, unreadCount, mentionCount int) {
	m.channelMarks = append(m.channelMarks, channelMarkRecord{channelID, ts, unreadCount, mentionCount})
}
```

The four pre-existing dispatch tests read `got.unreadCount` and `got.channelID`
only, so they need no edits.

- [ ] **Step 7: Update the real handler**

In `cmd/slk/main.go`, replace `OnChannelMarked` (lines 4372-4407):

```go
func (h *rtmEventHandler) OnChannelMarked(channelID, ts string, unreadCount, mentionCount int) {
	// Slack's *_marked events fire in BOTH directions: when the user
	// reads a channel (unreadCount=0) AND when the user marks one
	// unread (unreadCount>0). The event payload's
	// `unread_count_display` tells us which case we're in. We must
	// use it instead of always clearing the unread flag — the
	// original spec hardcoded false here, which meant a remote
	// mark-unread (via another client) silently cleared slk's dot.
	hasUnread := unreadCount > 0
	// Persist regardless of active workspace so the cache stays
	// authoritative across workspace switches.
	if err := h.db.UpdateChannelReadState(channelID, ts, hasUnread); err != nil {
		log.Printf("Warning: failed to update read state on channel_marked %s/%s: %v", channelID, ts, err)
	}
	// The event's mention_count is authoritative and replaces whatever
	// the local increment path accumulated, which is how @usergroup
	// undercounting gets corrected. A read event carries 0 and clears
	// the badge.
	if err := h.db.SetChannelMentionCount(channelID, mentionCount); err != nil {
		log.Printf("Warning: failed to set mention count on channel_marked %s: %v", channelID, err)
	}
	if h.program != nil {
		// Always notify so the workspace rail can refresh, regardless
		// of whether this workspace is active. The active-workspace
		// sidebar refresh and toast come from ChannelMarkedRemoteMsg
		// below; the rail refresh comes from ReadStateChangedMsg's
		// App.Update handler.
		h.program.Send(ui.ReadStateChangedMsg{WorkspaceID: h.workspaceID, ChannelID: channelID})
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace: persistence + rail refresh above are
		// the only visible effects; no sidebar/toast to update.
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ChannelMarkedRemoteMsg{
		ChannelID:   channelID,
		TS:          ts,
		UnreadCount: unreadCount,
	})
}
```

`ui.ChannelMarkedRemoteMsg` is deliberately unchanged: the sidebar reads mention
count from the DB via `readStateReader`, so the message needs only to trigger a
refresh, not to carry the value.

- [ ] **Step 8: Update the four existing handler-test call sites**

In `cmd/slk/event_handler_marked_test.go`, each `h.OnChannelMarked(...)` call
gains a fourth argument. Add `0` to each, matching the read/unread intent of the
surrounding test:

- `TestOnChannelMarked_WritesReadState` — `h.OnChannelMarked("C1", "1.0050", 0, 0)`
- `TestOnChannelMarked_InactiveWorkspace_StillWritesDB` — append `, 0` to its call
- `TestOnChannelMarked_RemoteMarkUnread_SetsHasUnread` — append `, 0` to its call
- `TestOnChannelMarked_ZeroUnreadCount_ClearsHasUnread` — append `, 0` to its call

Locate them with:

```bash
grep -n 'h.OnChannelMarked(' cmd/slk/event_handler_marked_test.go
```

- [ ] **Step 9: Write the failing handler tests**

Append to `cmd/slk/event_handler_marked_test.go`:

```go
// The event's mention_count is authoritative: it replaces whatever the
// local increment path accumulated. This is the mechanism that corrects
// @usergroup undercounting without a poll.
func TestOnChannelMarked_SetsMentionCountFromEvent(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	// Local detection had accumulated 5; the server says 2.
	if err := db.SetChannelMentionCount("C1", 5); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	h := &rtmEventHandler{
		db:       db,
		wsCtx:    &WorkspaceContext{},
		isActive: func() bool { return true },
		program:  nil,
	}

	h.OnChannelMarked("C1", "1.0050", 9, 2)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 2 {
		t.Errorf("MentionCount = %d, want 2 (server value replaces local)", state.MentionCount)
	}
}

// Reading a channel in another client pushes unread_count_display=0 and
// mention_count=0, which must clear the badge as well as the dot.
func TestOnChannelMarked_ReadClearsMentionCount(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 4); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	h := &rtmEventHandler{
		db:       db,
		wsCtx:    &WorkspaceContext{},
		isActive: func() bool { return true },
		program:  nil,
	}

	h.OnChannelMarked("C1", "1.0050", 0, 0)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.HasUnread {
		t.Error("HasUnread = true, want false after read")
	}
	if state.MentionCount != 0 {
		t.Errorf("MentionCount = %d, want 0 after read", state.MentionCount)
	}
}
```

- [ ] **Step 10: Run the tests to verify they pass**

```bash
go test ./internal/slack/ -run 'TestDispatch_ChannelMarked' -v
go test ./cmd/slk/ -run 'TestOnChannelMarked' -v
```

Expected: all PASS, including the four pre-existing `TestOnChannelMarked_*` tests.

- [ ] **Step 11: Verify build, vet, format, and the full suite**

```bash
go build ./... && go vet ./... && gofmt -l cmd internal && go test ./... -race 2>&1 | grep -v '^ok' | head -20
```

Expected: no output from build/vet/gofmt; no FAIL lines.

- [ ] **Step 12: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go \
        cmd/slk/main.go cmd/slk/event_handler_marked_test.go
git commit -m "feat: carry mention_count on channel_marked events"
```

---

### Task 7: Clear the mention count when a channel is read

Two write paths zero the badge: entering a channel (`markChannelReadAsync`) and
the `u` key's mark-unread, which moves the read boundary and so has no mentions
below it.

**Files:**
- Modify: `cmd/slk/main.go:3101-3121` (`markChannelReadAsync`)
- Modify: `cmd/slk/main.go:1630-1672` (the `MarkUnread` service closure)
- Create: `cmd/slk/mark_read_mention_test.go` (one behaviour test driving the
  real `markChannelReadAsync` against a fake Slack, plus two guard tests)

**Interfaces:**
- Consumes: `cache.SetChannelMentionCount` from Task 4.
- Produces: no new exported surface.

- [ ] **Step 1: Write the failing test**

Create `cmd/slk/mark_read_mention_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// waitForMentionCount polls until the channel's mention count reaches
// want, or fails. markChannelReadAsync does its work in a goroutine and,
// with a nil *tea.Program, emits no completion signal — so poll rather
// than sleeping a fixed duration.
func waitForMentionCount(t *testing.T, db *cache.DB, channelID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	last := -1
	for time.Now().Before(deadline) {
		state, err := db.GetChannelReadState(channelID)
		if err != nil {
			t.Fatalf("GetChannelReadState: %v", err)
		}
		last = state.MentionCount
		if last == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mention count for %s = %d after 2s, want %d", channelID, last, want)
}

// Entering a channel clears its mention badge. This drives the real
// markChannelReadAsync against a fake Slack so the production path is
// what gets exercised, not a hand-rolled pair of DB writes.
func TestMarkChannelRead_ClearsMentionCount(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	srv := newFakeSlack(t, map[string]string{"/api/conversations.mark": `{"ok":true}`})
	wctx := &WorkspaceContext{Client: newTestClient(t, srv.Server)}

	markChannelReadAsync(context.Background(), wctx, db, nil, "C1", "1.0050")
	waitForMentionCount(t, db, "C1", 0)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.HasUnread {
		t.Error("HasUnread = true, want false after read")
	}
	if state.LastReadTS != "1.0050" {
		t.Errorf("LastReadTS = %q, want 1.0050", state.LastReadTS)
	}
	// The DB write is unconditional (markChannelReadAsync discards
	// MarkChannel's error), so assert the API call really happened —
	// otherwise this test would pass with no network path at all.
	if got := srv.requestTo(t, "/api/conversations.mark").form.Get("channel"); got != "C1" {
		t.Errorf("conversations.mark channel = %q, want C1", got)
	}
}

// The nil-workspace guard must short-circuit before any write, so a
// workspace that failed to construct cannot silently clear badges.
// No goroutine starts in this path, so the check is immediate.
func TestMarkChannelRead_NilWorkspaceDoesNotWrite(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	markChannelReadAsync(context.Background(), nil, db, nil, "C1", "1.0050")

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 3 {
		t.Errorf("nil wctx wrote to the DB; MentionCount = %d, want 3 untouched", state.MentionCount)
	}
}

// An empty ts is the other guard: there is no watermark to advance, so
// nothing should be cleared.
func TestMarkChannelRead_EmptyTSDoesNotWrite(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	srv := newFakeSlack(t, map[string]string{"/api/conversations.mark": `{"ok":true}`})
	wctx := &WorkspaceContext{Client: newTestClient(t, srv.Server)}

	markChannelReadAsync(context.Background(), wctx, db, nil, "C1", "")

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 3 {
		t.Errorf("empty ts wrote to the DB; MentionCount = %d, want 3 untouched", state.MentionCount)
	}
}
```

Imports needed: `context`, `testing`, `time`, and
`github.com/gammons/slk/internal/cache`. The helpers `newTestDB`
(`cmd/slk/reconnect_sync_test.go:33`), `newFakeSlack`
(`cmd/slk/bootstrap_adapters_test.go:75`), `newTestClient` (`:41`) and
`fakeSlack.requestTo` all already exist in `package main`.

- [ ] **Step 2: Run the tests to verify the first one fails**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./cmd/slk/ -run TestMarkChannelRead -v 2>&1 | tail -25
```

Expected: `TestMarkChannelRead_ClearsMentionCount` FAILS with
"mention count for C1 = 3 after 2s, want 0" — the production path does not
clear the count yet. The two guard tests PASS already, which is correct: they
pin behaviour that must not change.

Expected: PASS. This test pins the DB contract and the nil-guard; Steps 3-4 add
the production wiring that makes the real path match it.

- [ ] **Step 3: Clear the count in `markChannelReadAsync`**

In `cmd/slk/main.go`, the goroutine body inside `markChannelReadAsync`
(lines 3112-3120) currently reads:

```go
	go func() {
		_ = client.MarkChannel(ctx, channelID, ts)
		if err := db.UpdateChannelReadState(channelID, ts, false); err != nil {
			log.Printf("Warning: failed to update read state in markChannelReadAsync %s/%s: %v", channelID, ts, err)
		}
		if p != nil {
			p.Send(ui.ChannelMarkedReadMsg{ChannelID: channelID})
		}
	}()
```

Replace with:

```go
	go func() {
		_ = client.MarkChannel(ctx, channelID, ts)
		if err := db.UpdateChannelReadState(channelID, ts, false); err != nil {
			log.Printf("Warning: failed to update read state in markChannelReadAsync %s/%s: %v", channelID, ts, err)
		}
		// Reading the channel clears its mention badge. Slack echoes a
		// *_marked event with mention_count=0 shortly after, which
		// would do this anyway — but doing it here means the badge
		// clears on the same render as the dot instead of one round
		// trip later.
		if err := db.SetChannelMentionCount(channelID, 0); err != nil {
			log.Printf("Warning: failed to clear mention count in markChannelReadAsync %s: %v", channelID, err)
		}
		if p != nil {
			p.Send(ui.ChannelMarkedReadMsg{ChannelID: channelID})
		}
	}()
```

- [ ] **Step 4: Clear the count in the `MarkUnread` closure**

In `cmd/slk/main.go`, the channel-level branch inside the `MarkUnread` closure
(lines 1643-1651) currently reads:

```go
				if threadTSStr == "" {
					err = client.MarkChannelUnread(ctx, chIDStr, boundaryTSStr)
					if err == nil {
						if dbErr := db.UpdateChannelReadState(chIDStr, boundaryTSStr, true); dbErr != nil {
							log.Printf("Warning: failed to update read state on mark-unread %s/%s: %v", chIDStr, boundaryTSStr, dbErr)
						}
					} else {
						log.Printf("Warning: failed to mark channel %s as unread (boundary %s): %v", chIDStr, boundaryTSStr, err)
					}
				} else {
```

Replace with:

```go
				if threadTSStr == "" {
					err = client.MarkChannelUnread(ctx, chIDStr, boundaryTSStr)
					if err == nil {
						if dbErr := db.UpdateChannelReadState(chIDStr, boundaryTSStr, true); dbErr != nil {
							log.Printf("Warning: failed to update read state on mark-unread %s/%s: %v", chIDStr, boundaryTSStr, dbErr)
						}
						// Mark-unread moves the read boundary to the
						// selected message; slk has not evaluated
						// whether anything below it mentions the user,
						// so claiming a count would be inventing one.
						// Zero shows the dot and lets Slack's echoed
						// *_marked event supply the real number.
						if dbErr := db.SetChannelMentionCount(chIDStr, 0); dbErr != nil {
							log.Printf("Warning: failed to clear mention count on mark-unread %s: %v", chIDStr, dbErr)
						}
					} else {
						log.Printf("Warning: failed to mark channel %s as unread (boundary %s): %v", chIDStr, boundaryTSStr, err)
					}
				} else {
```

- [ ] **Step 5: Run the cmd/slk tests**

```bash
go test ./cmd/slk/ -race 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 6: Verify build, vet, and format**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/
```

Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add cmd/slk/main.go cmd/slk/mark_read_mention_test.go
git commit -m "feat: clear mention count when a channel is read or marked unread"
```

---

### Task 8: Increment the mention count on inbound messages

The liveness half of the design. Sits immediately beside the existing
`has_unread` write so the two decisions cannot drift apart.

**Files:**
- Modify: `cmd/slk/main.go:4037-4048` (the read-state block inside `OnMessage`)
- Create: `cmd/slk/on_message_mention_test.go`

**Interfaces:**
- Consumes: `mention.InText` (Task 1), `cache.IncrementChannelMentionCount` (Task 4).
- Produces: no new exported surface.

- [ ] **Step 1: Write the failing tests**

Create `cmd/slk/on_message_mention_test.go`:

```go
package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// onMessageMentionFixture builds the minimal handler OnMessage needs to
// reach its read-state block: a db, a channel-type map, the self user ID,
// and an active-channel getter. notifier is nil so the notification
// branch is skipped, and program is nil so no tea.Program is required.
func onMessageMentionFixture(t *testing.T, chType, activeChannelID string) (*rtmEventHandler, *cache.DB) {
	t.Helper()
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	h := &rtmEventHandler{
		db:              db,
		workspaceID:     "T1",
		currentUserID:   "USELF",
		channelTypes:    map[string]string{"C1": chType},
		userNames:       map[string]string{},
		channelNames:    map[string]string{"C1": "general"},
		isActive:        func() bool { return false }, // stop before the UI dispatch
		activeChannelID: func() string { return activeChannelID },
	}
	return h, db
}

func mentionCount(t *testing.T, db *cache.DB, channelID string) int {
	t.Helper()
	state, err := db.GetChannelReadState(channelID)
	if err != nil {
		t.Fatalf("GetChannelReadState(%s): %v", channelID, err)
	}
	return state.MentionCount
}

func TestOnMessage_MentionIncrementsCount(t *testing.T) {
	tests := []struct {
		name     string
		chType   string
		author   string
		text     string
		threadTS string
		subtype  string
		active   string
		want     int
	}{
		{
			name: "explicit self mention in channel",
			chType: "channel", author: "UOTHER",
			text: "hey <@USELF> ship it", want: 1,
		},
		{
			name: "here broadcast in channel",
			chType: "channel", author: "UOTHER",
			text: "<!here> standup", want: 1,
		},
		{
			name: "plain channel message does not count",
			chType: "channel", author: "UOTHER",
			text: "unrelated chatter", want: 0,
		},
		{
			// Slack badges every DM message, and client.counts reports
			// them in mention_count, so local detection must agree.
			name: "any dm message counts",
			chType: "dm", author: "UOTHER",
			text: "no markup here", want: 1,
		},
		{
			name: "any group dm message counts",
			chType: "group_dm", author: "UOTHER",
			text: "no markup here", want: 1,
		},
		{
			name: "self-authored mention does not count",
			chType: "channel", author: "USELF",
			text: "note to <@USELF>", want: 0,
		},
		{
			// Mirrors the has_unread gate: a non-broadcast thread reply
			// does not touch the parent channel. Thread mentions surface
			// in the Threads section instead.
			name: "plain thread reply does not count",
			chType: "channel", author: "UOTHER",
			text: "<@USELF> in a thread", threadTS: "1.0000", want: 0,
		},
		{
			name: "thread broadcast counts",
			chType: "channel", author: "UOTHER",
			text: "<@USELF> broadcasting", threadTS: "1.0000", subtype: "thread_broadcast", want: 1,
		},
		{
			name: "mention in the active channel does not count",
			chType: "channel", author: "UOTHER",
			text: "<@USELF> hi", active: "C1", want: 0,
		},
		{
			name: "usergroup mention is not detected",
			chType: "channel", author: "UOTHER",
			text: "<!subteam^S1|@eng> ship it", want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, db := onMessageMentionFixture(t, tt.chType, tt.active)
			h.OnMessage("C1", tt.author, "1.0001", tt.text, tt.threadTS, tt.subtype,
				false, nil, nil, nil, "", "")
			if got := mentionCount(t, db, "C1"); got != tt.want {
				t.Errorf("MentionCount = %d, want %d", got, tt.want)
			}
		})
	}
}

// Two mentions arriving before the channel is read must both count. The
// increment happens in SQL, so this also exercises that path rather than a
// read-modify-write.
func TestOnMessage_MentionsAccumulate(t *testing.T) {
	h, db := onMessageMentionFixture(t, "channel", "")
	h.OnMessage("C1", "UOTHER", "1.0001", "<@USELF> first", "", "", false, nil, nil, nil, "", "")
	h.OnMessage("C1", "UOTHER", "1.0002", "<!channel> second", "", "", false, nil, nil, nil, "", "")
	if got := mentionCount(t, db, "C1"); got != 2 {
		t.Errorf("MentionCount = %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./cmd/slk/ -run 'TestOnMessage_Mention' -v 2>&1 | tail -30
```

Expected: FAIL. The mention subtests report `MentionCount = 0, want 1`; the
non-mention subtests already pass because nothing increments yet.

- [ ] **Step 3: Add the increment**

In `cmd/slk/main.go`, the read-state block (lines 4037-4048) currently reads:

```go
	isThreadReply := threadTS != "" && threadTS != ts
	isBroadcast := subtype == "thread_broadcast"
	shouldMarkChannel := !isThreadReply || isBroadcast
	activeChIDForRead := ""
	if h.activeChannelID != nil {
		activeChIDForRead = h.activeChannelID()
	}
	if h.db != nil && shouldMarkChannel && activeChIDForRead != channelID {
		if err := h.db.UpdateChannelReadState(channelID, "", true); err != nil {
			log.Printf("Warning: failed to set has_unread for %s: %v", channelID, err)
		}
	}
```

Replace with:

```go
	isThreadReply := threadTS != "" && threadTS != ts
	isBroadcast := subtype == "thread_broadcast"
	shouldMarkChannel := !isThreadReply || isBroadcast
	activeChIDForRead := ""
	if h.activeChannelID != nil {
		activeChIDForRead = h.activeChannelID()
	}
	if h.db != nil && shouldMarkChannel && activeChIDForRead != channelID {
		if err := h.db.UpdateChannelReadState(channelID, "", true); err != nil {
			log.Printf("Warning: failed to set has_unread for %s: %v", channelID, err)
		}
		// Mention badge: bump the count when this message mentions the
		// user. Deliberately gated on the same three conditions as the
		// has_unread write above so the dot and the badge can never
		// disagree about whether a message "arrived unread".
		//
		// Conversation type decides what counts. Slack reports every
		// unread message in mention_count for ims and mpims, and only
		// @-mentions for channels; matching that split here keeps local
		// increments consistent with the server value that will later
		// overwrite them.
		//
		// userID, not authorID: a bot message has userID == "" and can
		// never be "you", and mention.InText's empty-self guard makes
		// the direct-mention check a no-op in that case while still
		// honouring @here/@channel from bots.
		chTypeForMention := h.channelTypes[channelID]
		isDMLike := chTypeForMention == "dm" || chTypeForMention == "group_dm"
		mentionsSelf := isDMLike || mention.InText(text, h.currentUserID)
		if userID != h.currentUserID && mentionsSelf {
			if err := h.db.IncrementChannelMentionCount(channelID); err != nil {
				log.Printf("Warning: failed to increment mention count for %s: %v", channelID, err)
			}
		}
	}
```

- [ ] **Step 4: Add the import**

Add `"github.com/gammons/slk/internal/mention"` to the import block in
`cmd/slk/main.go`, in the local-package group, keeping the group sorted.
Verify placement with `gofmt`.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./cmd/slk/ -run 'TestOnMessage_Mention' -v 2>&1 | tail -30
```

Expected: PASS, all 10 subtests plus `TestOnMessage_MentionsAccumulate`.

- [ ] **Step 6: Run the full cmd/slk suite**

```bash
go test ./cmd/slk/ -race 2>&1 | tail -20
```

Expected: PASS. In particular no pre-existing `OnMessage` test regresses — the
new code runs strictly inside the existing `shouldMarkChannel` guard.

- [ ] **Step 7: Verify build, vet, and format**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add cmd/slk/main.go cmd/slk/on_message_mention_test.go
git commit -m "feat: increment mention count on inbound mentions"
```

---

### Task 9: `MentionBadgeStyle()` in the styles package

Adds a theme-aware badge style using the palette's contrast-guaranteed
highlight pair.

**`styles.UnreadBadge` is NOT deleted.** It is live at
`internal/ui/statusbar/model.go:250`, which renders the status bar's
" N unread " badge with it. Its hardcoded white-on-`Error` foreground is a
pre-existing contrast defect recorded in the design doc's "Recorded, out of
scope" section — leave it alone.

**Files:**
- Modify: `internal/ui/styles/styles.go` (append one function near `SelectionStyle`)
- Modify: `internal/ui/styles/styles_test.go` (append one test)

**Interfaces:**
- Consumes: `SelectionBackground`, `SelectionForeground` (existing package vars).
- Produces: `func styles.MentionBadgeStyle() lipgloss.Style`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/styles/styles_test.go`. The file already provides the
`colorEqual` helper at line 12 and imports `config`, `lipgloss`, `color`, `fmt`,
`testing`.

```go
// The mention badge uses the theme's highlight pair rather than a
// hardcoded color, so it stays legible on every theme. Apply() derives
// Primary-on-Background when a theme omits the pair, so the style is
// never blank.
func TestMentionBadgeStyle_UsesThemeSelectionColors(t *testing.T) {
	Apply("dark", config.Theme{})
	s := MentionBadgeStyle()
	if !colorEqual(s.GetBackground(), SelectionBackground) {
		t.Errorf("background = %v, want SelectionBackground %v", s.GetBackground(), SelectionBackground)
	}
	if !colorEqual(s.GetForeground(), SelectionForeground) {
		t.Errorf("foreground = %v, want SelectionForeground %v", s.GetForeground(), SelectionForeground)
	}
}

// Switching themes must change the badge. A package-level var composed at
// init time would not, which is why this is a function.
func TestMentionBadgeStyle_TracksThemeChange(t *testing.T) {
	Apply("dark", config.Theme{})
	darkBg := MentionBadgeStyle().GetBackground()

	Apply("light", config.Theme{})
	lightBg := MentionBadgeStyle().GetBackground()

	if colorEqual(darkBg, lightBg) {
		t.Errorf("badge background did not change between dark and light themes (both %v)", darkBg)
	}

	// Restore the default so later tests in this package see a known theme.
	Apply("dark", config.Theme{})
}

// An explicit theme override for the selection pair must reach the badge.
func TestMentionBadgeStyle_HonorsSelectionOverride(t *testing.T) {
	Apply("dark", config.Theme{})
	defer Apply("dark", config.Theme{})

	SelectionBackground = lipgloss.Color("#123456")
	SelectionForeground = lipgloss.Color("#ABCDEF")
	s := MentionBadgeStyle()
	if !colorEqual(s.GetBackground(), lipgloss.Color("#123456")) {
		t.Errorf("background = %v, want #123456", s.GetBackground())
	}
	if !colorEqual(s.GetForeground(), lipgloss.Color("#ABCDEF")) {
		t.Errorf("foreground = %v, want #ABCDEF", s.GetForeground())
	}
}
```

Verified before writing this plan: `Apply("dark", …)` yields
`SelectionBackground` = `#4A9EFF` (RGBA `{74 158 255 255}`) and
`Apply("light", …)` yields `#0366D6` (`{3 102 214 255}`), so
`TestMentionBadgeStyle_TracksThemeChange` has a real difference to observe.
`lipgloss.Style.GetBackground()` / `GetForeground()` were likewise confirmed to
exist in `charm.land/lipgloss/v2` and to round-trip through the package's
`colorEqual` helper.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/ui/styles/ -run TestMentionBadgeStyle -v
```

Expected: build failure, `undefined: MentionBadgeStyle`.

- [ ] **Step 3: Add the function**

In `internal/ui/styles/styles.go`, insert immediately after `SelectionStyle()`
(which currently ends at line 512):

```go
// MentionBadgeStyle returns the style used for the sidebar's unread
// direct-mention badge. It borrows the theme's selection highlight pair,
// which Apply() always populates and which is contrast-safe by
// construction (defaulting to Primary-on-Background).
//
// A function rather than a package var: var-shaped styles in this file
// must be declared twice — once in the top-level var block and again in
// buildStyles() — and the top-level copy would read SelectionBackground
// while it is still nil, since that var has no initializer. SelectionStyle
// and SearchHighlightStyle are functions for the same reason.
//
// Deliberately not UnreadBadge: that style hardcodes white over Error and
// belongs to the status bar. See
// docs/superpowers/specs/2026-09-09-mention-badges-design.md.
func MentionBadgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(SelectionBackground).
		Foreground(SelectionForeground).
		Padding(0, 1)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/ui/styles/ -run TestMentionBadgeStyle -v
```

Expected: PASS, all three tests.

- [ ] **Step 5: Run the whole styles package**

```bash
go test ./internal/ui/styles/ -race
```

Expected: PASS. The theme-mutating tests restore `"dark"`, so no ordering
dependency is introduced.

- [ ] **Step 6: Verify build, vet, and format**

```bash
go build ./... && go vet ./internal/ui/styles/ && gofmt -l internal/ui/styles/
```

Expected: no output. `go build ./...` in particular confirms
`statusbar/model.go:250` still compiles — proof `UnreadBadge` was left in place.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/styles/styles.go internal/ui/styles/styles_test.go
git commit -m "feat: add theme-aware MentionBadgeStyle"
```

---

### Task 10: Render the badge in the sidebar

The user-visible task. Adds the predicate, replaces the trailing dot with a
badge when mentions exist, and makes the name-truncation budget per-row.

**Files:**
- Modify: `internal/ui/sidebar/model.go:54-61` (predicate + `IsVisiblyUnread` doc), `:1331-1345` (badge selection), `:1375-1389` (width budget), `:1396-1398` (label assembly)
- Create: `internal/ui/sidebar/mention_badge_test.go`

**Interfaces:**
- Consumes: `cache.ReadState.MentionCount` (Task 4), `styles.MentionBadgeStyle` (Task 9).
- Produces: `func (item ChannelItem) MentionBadge(state cache.ReadState) int`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/sidebar/mention_badge_test.go`:

```go
package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// rowFor returns the rendered sidebar line containing name, failing the
// test if no such line exists. Mirrors the line-scanning approach in
// muted_test.go — sidebar tests never use golden files.
func rowFor(t *testing.T, view, name string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("row %q not rendered:\n%s", name, view)
	return ""
}

func TestMentionBadge_Predicate(t *testing.T) {
	tests := []struct {
		name  string
		item  ChannelItem
		state cache.ReadState
		want  int
	}{
		{
			name:  "unread with mentions",
			item:  ChannelItem{ID: "C1"},
			state: cache.ReadState{HasUnread: true, MentionCount: 3},
			want:  3,
		},
		{
			name:  "unread without mentions",
			item:  ChannelItem{ID: "C1"},
			state: cache.ReadState{HasUnread: true, MentionCount: 0},
			want:  0,
		},
		{
			// Gating on HasUnread means a stale mention_count cannot
			// outlive the unread flag that justifies it.
			name:  "read but stale mention count",
			item:  ChannelItem{ID: "C1"},
			state: cache.ReadState{HasUnread: false, MentionCount: 4},
			want:  0,
		},
		{
			// Mentions pierce mute. Unlike IsVisiblyUnread, this
			// predicate ignores IsMuted: muting a busy channel must not
			// hide a direct @-mention.
			name:  "muted with mentions still badges",
			item:  ChannelItem{ID: "C1", IsMuted: true},
			state: cache.ReadState{HasUnread: true, MentionCount: 2},
			want:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.MentionBadge(tt.state); got != tt.want {
				t.Errorf("MentionBadge() = %d, want %d", got, tt.want)
			}
		})
	}
}

// A channel with mentions shows a count instead of the dot, never both.
func TestMentionBadge_ReplacesUnreadDot(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 3},
			"C2": {HasUnread: true, MentionCount: 0},
		}
	})
	m.ToggleCollapse("Channels")
	view := m.View(10, 30)

	badged := rowFor(t, view, "deploys")
	if !strings.Contains(badged, "3") {
		t.Errorf("mentioned row lacks its count:\n%q", badged)
	}
	if strings.Contains(badged, "●") {
		t.Errorf("mentioned row still shows the unread dot:\n%q", badged)
	}

	plain := rowFor(t, view, "general")
	if !strings.Contains(plain, "●") {
		t.Errorf("unread row without mentions lost its dot:\n%q", plain)
	}
}

// Muting silences the dot and the bold, but not the badge.
func TestMentionBadge_MutedChannelStillBadges(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", IsMuted: true},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 2}}
	})
	m.ToggleCollapse("Channels")
	view := m.View(10, 30)

	line := rowFor(t, view, "noisy")
	if !strings.Contains(line, "2") {
		t.Errorf("muted channel with mentions lost its badge:\n%q", line)
	}
	// The dot suppression that muted_test.go pins must still hold.
	if strings.Contains(line, "●") {
		t.Errorf("muted channel rendered an unread dot:\n%q", line)
	}
}

// Slack caps its badges at 99+. The cap is a render concern only: the DB
// keeps the true count so a later refresh below 100 shows the real number.
func TestMentionBadge_CapsAt99Plus(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "firehose", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 250}}
	})
	m.ToggleCollapse("Channels")
	line := rowFor(t, m.View(10, 30), "firehose")

	if !strings.Contains(line, "99+") {
		t.Errorf("expected 99+ cap:\n%q", line)
	}
	if strings.Contains(line, "250") {
		t.Errorf("raw count leaked into the badge:\n%q", line)
	}
}

func TestMentionBadge_ExactlyNinetyNineIsNotCapped(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "busy", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 99}}
	})
	m.ToggleCollapse("Channels")
	line := rowFor(t, m.View(10, 30), "busy")

	if strings.Contains(line, "99+") {
		t.Errorf("99 should render bare, not capped:\n%q", line)
	}
	if !strings.Contains(line, "99") {
		t.Errorf("expected 99 in the badge:\n%q", line)
	}
}

// ansiRe strips SGR escape sequences so a rendered row can be measured as
// the user sees it. The sidebar injects inline styling for the cursor,
// type prefix, unread dot and mention badge, so raw View() output cannot
// be measured directly.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// The name budget is computed per row, so a channel without a badge keeps
// exactly the width it had before badges existed. Only badged rows pay for
// the badge.
//
// Widths are chosen so both rows truncate deterministically. At width 24:
//
//	unbadged maxNameLen = (24-2) - 6 - 2 = 14
//	badged   maxNameLen = (24-2) - 6 - 5 = 11
//
// The 36-character name exceeds both, so the ellipsis lands three columns
// earlier on the badged row.
func TestMentionBadge_WidthBudgetIsPerRow(t *testing.T) {
	const longName = "engineering-deployments-and-releases"

	render := func(t *testing.T, mentions int) string {
		t.Helper()
		m := New([]ChannelItem{{ID: "C1", Name: longName, Type: "channel"}})
		m.SetReadStateReader(func() map[string]cache.ReadState {
			return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: mentions}}
		})
		m.ToggleCollapse("Channels")
		return rowFor(t, m.View(10, 24), longName[:10])
	}

	plain := ansiRe.ReplaceAllString(render(t, 0), "")
	badged := ansiRe.ReplaceAllString(render(t, 42), "")

	plainCut := strings.Index(plain, "…")
	badgedCut := strings.Index(badged, "…")
	if plainCut < 0 {
		t.Fatalf("unbadged row was not truncated, so the test cannot compare budgets:\n%q", plain)
	}
	if badgedCut < 0 {
		t.Fatalf("badged row was not truncated:\n%q", badged)
	}
	if badgedCut >= plainCut {
		t.Errorf("badged name cut at %d, unbadged at %d; badged must be shorter,"+
			" otherwise the width budget is not per-row\nplain:  %q\nbadged: %q",
			badgedCut, plainCut, plain, badged)
	}
	if !strings.Contains(badged, "42") {
		t.Errorf("badged row lost its count:\n%q", badged)
	}
}

// The converse of the test above: an unbadged row must be byte-identical
// to what it rendered before mention badges existed. rowChromeExcludingTrailer
// plus the dot's 2 cells must still equal the original hardcoded 8.
func TestMentionBadge_UnbadgedRowKeepsOriginalNameWidth(t *testing.T) {
	const longName = "engineering-deployments-and-releases"
	m := New([]ChannelItem{{ID: "C1", Name: longName, Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 0}}
	})
	m.ToggleCollapse("Channels")
	plain := ansiRe.ReplaceAllString(rowFor(t, m.View(10, 24), longName[:10]), "")

	// maxNameLen = (24-2) - 6 - 2 = 14, unchanged from the original
	// hardcoded budget of 8 (rowChromeExcludingTrailer 6 + dot 2).
	//
	// Bracket the budget rather than asserting an exact cut, so the test
	// does not depend on whether truncate.StringWithTail counts the
	// ellipsis inside or outside the limit: 12 characters must survive,
	// 15 must not.
	if !strings.Contains(plain, "engineering-") {
		t.Errorf("unbadged row truncated below the 14-column budget: %q", plain)
	}
	if strings.Contains(plain, "engineering-dep") {
		t.Errorf("unbadged row exceeded the 14-column budget: %q", plain)
	}
}

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
go test ./internal/ui/sidebar/ -run TestMentionBadge -v 2>&1 | tail -30
```

Expected: build failure, `item.MentionBadge` undefined.

- [ ] **Step 3: Add the predicate and narrow the `IsVisiblyUnread` doc**

In `internal/ui/sidebar/model.go`, replace `IsVisiblyUnread` and its doc
comment (lines 54-61) with:

```go
// IsVisiblyUnread reports whether this channel should render the unread
// DOT -- DB-level HasUnread AND the user hasn't muted it. This is the
// single source of truth for the "unread dot" predicate; the sidebar
// View, section aggregates, and the App's tab-title counter MUST consult
// this helper rather than re-deriving the rule.
//
// Scoped to the dot deliberately: mentions pierce mute. See MentionBadge.
func (item ChannelItem) IsVisiblyUnread(state cache.ReadState) bool {
	return state.HasUnread && !item.IsMuted
}

// MentionBadge reports the direct-mention count to render on this row, or
// 0 for no badge. It is the single source of truth for the badge
// predicate, the counterpart to IsVisiblyUnread.
//
// Two differences from IsVisiblyUnread:
//
//   - It ignores IsMuted. Slack keeps a muted channel grey and unbolded
//     for ordinary traffic but still badges an explicit @-mention; that
//     escape hatch is what makes muting safe on a busy channel.
//   - It requires HasUnread, so a stale non-zero mention_count cannot
//     outlive the unread flag that justifies it.
//
// The 99+ cap is applied by the renderer, not here: the DB keeps the true
// count so a later refresh below 100 shows the real number.
func (item ChannelItem) MentionBadge(state cache.ReadState) int {
	if !state.HasUnread {
		return 0
	}
	if state.MentionCount < 0 {
		return 0
	}
	return state.MentionCount
}
```

- [ ] **Step 4: Add the badge constants and formatter**

In `internal/ui/sidebar/model.go`, add to the `const` block near the top
(alongside `defaultChannelsSection` at lines 21-28):

```go
// Mention badge sizing. The badge replaces the trailing unread dot, so
// the row's width budget must account for whichever is present.
//
// mentionBadgeCap matches Slack: counts above it render as "99+", which
// bounds the digit run at three characters. mentionBadgeMaxCells is the
// worst-case column cost of the rendered badge -- three digits plus the
// single space of Padding(0, 1) on each side.
const (
	mentionBadgeCap      = 99
	mentionBadgeMaxCells = 5
)
```

Add this helper next to the other row helpers in the same file:

```go
// formatMentionBadge renders n as Slack does: the bare number up to
// mentionBadgeCap, then "99+". Returns "" for n <= 0.
func formatMentionBadge(n int) string {
	switch {
	case n <= 0:
		return ""
	case n > mentionBadgeCap:
		return fmt.Sprintf("%d+", mentionBadgeCap)
	default:
		return fmt.Sprintf("%d", n)
	}
}
```

No import change needed: `fmt` is already imported by `model.go` (line 4), and
`fmt.Sprintf("%d", ...)` is what the Threads-row badge at line 1270 already
uses. `strconv` is deliberately not introduced for one call site.

- [ ] **Step 5: Select badge-or-dot in `buildCache`**

In `internal/ui/sidebar/model.go`, replace the dot-selection block
(lines 1341-1345):

```go
		// Unread dot indicator (same regardless of selection state).
		unreadDot := " "
		if hasUnread {
			unreadDot = unreadDotStr
		}
```

with:

```go
		// Trailing indicator: a mention badge when the channel has
		// unread direct mentions, otherwise the unread dot, otherwise
		// blank. Never both -- the badge subsumes the dot, matching
		// Slack and costing no extra glyph slot.
		//
		// mentionBadge is computed from item.MentionBadge rather than
		// hasUnread because the two predicates disagree on muted rows
		// by design: a muted channel suppresses the dot but keeps the
		// badge.
		//
		// The badge is rendered as ONE styled span so no ANSI reset
		// lands between the digits, which is what lets tests find the
		// literal substring in View() output. The Threads row badge at
		// the top of this function does the same for the same reason.
		unreadDot := " "
		trailerCells := 2 // worst-case cost of the dot glyph
		if badgeText := formatMentionBadge(item.MentionBadge(readState[item.ID])); badgeText != "" {
			unreadDot = styles.MentionBadgeStyle().Render(badgeText)
			trailerCells = mentionBadgeMaxCells
		} else if hasUnread {
			unreadDot = unreadDotStr
		}
```

- [ ] **Step 6: Make the width budget per-row**

Replace the truncation block (lines 1375-1389):

```go
		// Truncate name to fit sidebar width.
		// Unicode chars like ● (U+25CF), ○, ◆, ▌ have East Asian Width
		// "Ambiguous" — terminals may render them as 2 columns wide, but
		// lipgloss.Width() reports them as 1. We can't trust lipgloss
		// measurements for these chars, so use a conservative fixed budget:
		//   cursor(2) + prefix(3) + name + space(1) + dot(2) = name + 8
		// This assumes worst-case 2-col rendering for every ambiguous char.
		name := item.Name
		maxNameLen := (width - 2) - 8
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if lipgloss.Width(name) > maxNameLen {
			name = truncate.StringWithTail(name, uint(maxNameLen), "…")
		}
```

with:

```go
		// Truncate name to fit sidebar width.
		// Unicode chars like ● (U+25CF), ○, ◆, ▌ have East Asian Width
		// "Ambiguous" — terminals may render them as 2 columns wide, but
		// lipgloss.Width() reports them as 1. We can't trust lipgloss
		// measurements for these chars, so use a conservative budget
		// assuming worst-case 2-col rendering for every ambiguous char:
		//
		//   cursor(2) + prefix(3) + space(1) + trailer = rowChromeCells
		//
		// trailer is 2 for the dot and mentionBadgeMaxCells for a
		// badge, computed per row above. Charging every row the badge's
		// width would truncate names on rows that have no badge, so
		// this stays inside the loop.
		const rowChromeExcludingTrailer = 6 // cursor(2) + prefix(3) + space(1)
		name := item.Name
		maxNameLen := (width - 2) - rowChromeExcludingTrailer - trailerCells
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if lipgloss.Width(name) > maxNameLen {
			name = truncate.StringWithTail(name, uint(maxNameLen), "…")
		}
```

Note: `rowChromeExcludingTrailer + 2 == 8`, so an unbadged row's budget is
byte-for-byte what it was before this change. Verify by running the existing
sidebar tests in Step 8 — any width regression shows up there.

- [ ] **Step 7: Confirm the label assembly and ANSI reapply need no change**

The three label variants at lines 1396-1398 already interpolate `unreadDot`:

```go
		labelNormal := " " + prefix + name + " " + unreadDot
		labelSelected := cursorSelected + prefix + name + " " + unreadDot
		labelActive := activeBorder + prefix + name + " " + unreadDot
```

No edit needed. The `ReapplyBgAfterResets` calls that follow (lines 1428-1432)
already re-inject the row's background and foreground after inline ANSI resets,
and the badge is just another inline styled span — the same treatment the dot
and the private-channel prefix already receive.

Read those lines and confirm before moving on. If the badge's own background is
being overwritten by the reapply payload, that is the failure this step is
looking for; fix it by rendering the badge after the reapply rather than before.

- [ ] **Step 8: Run the new tests, then the whole sidebar package**

```bash
go test ./internal/ui/sidebar/ -run TestMentionBadge -v 2>&1 | tail -40
go test ./internal/ui/sidebar/ -race
```

Expected: new tests PASS; every pre-existing sidebar test PASSES unchanged. Pay
particular attention to `TestMutedChannel_SuppressesUnreadDot`
(`muted_test.go:14`), `TestThreadsItem_UnreadBadgeRenders` (`model_test.go:115`)
and `TestCollapsedHeader_ShowsAggregateUnreadBadge` (`collapse_test.go:98`) —
they assert on the same rendered rows.

- [ ] **Step 9: Run the UI package too**

```bash
go test ./internal/ui/ -race 2>&1 | tail -20
```

Expected: PASS. `internal/ui/jump_unread_test.go` and `app_test.go` construct
`cache.ReadState` values; the new field is additive and zero-valued, so they
should be unaffected.

- [ ] **Step 10: Verify build, vet, format, and the full suite**

```bash
go build ./... && go vet ./... && gofmt -l cmd internal && go test ./... -race 2>&1 | grep -v '^ok' | head -20
```

Expected: no output from build/vet/gofmt; no FAIL lines.

- [ ] **Step 11: Look at it**

Tests cannot judge whether the badge reads well against the row background at a
realistic sidebar width. Run the app:

```bash
go run ./cmd/slk
```

Confirm by eye:
- A channel with mentions shows a legible pill, not a smear of background color.
- The badge does not bleed its background into the rest of the row (that would
  be an ANSI reapply ordering bug from Step 7).
- A muted channel with a mention shows the badge while staying grey.
- Narrowing the terminal truncates badged names without wrapping the row.

- [ ] **Step 12: Commit**

```bash
git add internal/ui/sidebar/model.go internal/ui/sidebar/mention_badge_test.go
git commit -m "feat: render mention badges in the sidebar"
```

---

### Task 11: Document the shared helper in AGENTS.md

AGENTS.md's own convention: *"Adding a reusable helper? Add it to the tables
above in the same commit. An unlisted helper gets re-implemented."* Task 1
created one; this closes that loop.

**Files:**
- Modify: `AGENTS.md` (the "Text and rendering" table)

**Interfaces:**
- Consumes: nothing.
- Produces: documentation only.

- [ ] **Step 1: Add the row**

In `AGENTS.md`, find the "Text and rendering" table row for search highlighting:

```
| Search-term highlighting (ANSI/OSC-safe) | `messages.HighlightSearchTerms`, `messages.SearchHighlightSGR` |
```

Add immediately after it:

```
| Does this message text mention me? | `mention.InText(text, selfUserID)` |
```

- [ ] **Step 2: Verify the table still renders**

```bash
cd /home/dev/local_code/slk/.worktrees/feat-mention-badges
grep -n 'mention.InText' AGENTS.md
```

Expected: one match, inside the table.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: list mention.InText in the shared-code table"
```

---

## Findings

Filled in by Task 0. Until then these are open questions, and Tasks 5 and 6
must not be started.

**Does `client.counts` return `mention_count` on the `ims` block?**

> UNANSWERED — see Task 0 Step 2.

**Do `*_marked` events carry `mention_count`?**

> UNANSWERED — see Task 0 Step 4.

**Fallback chosen, if either is absent:**

> UNANSWERED — see Task 0 Step 5.

---

## Deferred

Recorded here so they are not silently lost:

- **Threads-row mention badge.** `client.counts` already returns
  `Threads.MentionCount` and `bootstrap.Threads` already carries it, but the
  Threads row is driven by an in-memory counter
  (`internal/ui/sidebar/model.go:275`), not the read-state DB.
- **`UnreadBadge` contrast bug.** `styles.go:457-458` hardcodes `#FFFFFF` over
  `Error`; `WorkspaceActive` and `StatusMode` do the same over `Primary`.
  Affects the status bar and workspace rail on pale-`Error` themes. Pre-existing
  and untouched by this work.
- **A periodic `client.counts` poll** to correct `@usergroup` undercounting
  without waiting for a reconnect.
- **Typing `boot.Subteams.Self`** to enable local usergroup mention detection.
  Needs a capture with a non-empty list.
