# Cache-first message loading — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Channel re-entry renders instantly from SQLite cache instead of waiting for a network round-trip; eliminate the "No messages yet" race on workspace switch / cold start; reuse the braille spinner for "Loading messages..." consistency.

**Architecture:** Persist the full upstream `slack.Message` JSON in the existing `messages.raw_json` column on every cache write. Add `loadCachedMessages` / `loadCachedThreadReplies` helpers that read from `cache.DB` and reconstruct `messages.MessageItem` (incl. files/blocks/reactions). New `App.channelCacheReader` and `App.threadCacheReader` callbacks invoked synchronously in `ChannelSelectedMsg` / `ThreadOpenedMsg` before the existing network fetcher fires. Spinner runes move to `internal/ui/styles` for cross-package use; `messages.Model` gets a `spinnerFrame`. Race fix is two `SetLoading(true)` insertions.

**Tech Stack:** Go 1.22+, SQLite (`internal/cache`), Bubbletea/Lipgloss UI, `internal/slack` (Slack web API client).

**Spec:** `docs/superpowers/specs/2026-05-03-cache-first-message-loading-design.md`

---

## File Structure

| File | Role |
|---|---|
| `internal/cache/messages.go` | (no schema change — `raw_json` column already exists) |
| `internal/cache/messages_test.go` | New round-trip test for `raw_json` |
| `cmd/slk/main.go` | Populate `RawJSON` in 4 `UpsertMessage` call sites; add `loadCachedMessages` and `loadCachedThreadReplies`; wire setters in `wireCallbacks` |
| `cmd/slk/cache_render_test.go` (new) | Tests for the two cache-read enrichment helpers |
| `internal/ui/styles/spinner.go` (new) | Exported `SpinnerChars` rune slice |
| `internal/ui/app.go` | Add `channelCacheReader` / `threadCacheReader` fields + setters + types; cache-first branch in `ChannelSelectedMsg` and `ThreadOpenedMsg` debounce path; race fix in `WorkspaceSwitchedMsg` and `WorkspaceReadyMsg`; consume `styles.SpinnerChars`; widen spinner tick gate |
| `internal/ui/messages/model.go` | Add `spinnerFrame int`, `SetSpinnerFrame`, `IsLoading`; render spinner glyph in empty-state and `cacheLoadingHint` |
| `internal/ui/messages/model_test.go` | Spinner render assertion + IsLoading test |
| `internal/ui/app_test.go` | Race fix + cache-first behavior tests |

---

## Important conventions for the engineer

- **Run from the worktree root** `.worktrees/cache-first-messages/`, never the parent repo.
- **Test runner:** `go test ./<pkg>/... -run <Name>` for targeted runs; `go test ./...` for the full suite.
- **Build check:** `go build ./...` after every code change before running tests.
- **Line numbers drift.** When a step says "around line N", confirm with `grep -n` first; the surrounding code shown in the step is authoritative.
- **TDD:** every code-producing task starts with a failing test.
- **Commit after each task** (small, focused commits).

---

## Task 1: Round-trip `raw_json` through the cache

**Files:**
- Modify: `internal/cache/messages_test.go`
- Read-only reference: `internal/cache/messages.go` (UpsertMessage already accepts `RawJSON`; GetMessages already SELECTs it but the existing scan stores into `m.RawJSON`)

The schema and code already support `raw_json` round-trip — but no test currently asserts it. Adding the test first nails down the contract before the producer call sites are changed in Task 2.

- [ ] **Step 1: Write the failing test**

Add to `internal/cache/messages_test.go`:

```go
func TestUpsertMessageRoundTripsRawJSON(t *testing.T) {
	db := newTestDB(t)
	payload := `{"type":"message","ts":"1.0","text":"hi","files":[{"id":"F1"}]}`
	if err := db.UpsertMessage(Message{
		TS:          "1.0",
		ChannelID:   "C1",
		WorkspaceID: "T1",
		UserID:      "U1",
		Text:        "hi",
		RawJSON:     payload,
		CreatedAt:   1,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := db.GetMessages("C1", 50, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].RawJSON != payload {
		t.Fatalf("raw_json round-trip mismatch:\nwant: %s\ngot:  %s", payload, got[0].RawJSON)
	}
}
```

If `newTestDB` is not the helper name, grep `internal/cache/messages_test.go` for whatever is used (likely `setupTestDB` or similar) and reuse it.

- [ ] **Step 2: Run test — expect PASS (the round-trip should already work)**

```bash
go test ./internal/cache/ -run TestUpsertMessageRoundTripsRawJSON -v
```

Expected: PASS. If it fails, the schema or scan code is missing `raw_json` — fix that first by examining `internal/cache/schema.go` and `internal/cache/messages.go:queryMessages`.

- [ ] **Step 3: Commit**

```bash
git add internal/cache/messages_test.go
git commit -m "test(cache): assert raw_json round-trips through UpsertMessage/GetMessages"
```

---

## Task 2: Populate `raw_json` in all four cache writers

**Files:**
- Modify: `cmd/slk/main.go` (4 sites: `fetchChannelMessages` ~line 1455, `fetchOlderMessages` ~line 1390, `fetchThreadReplies` ~line 1521, `rtmEventHandler.OnMessage` ~line 1704)

Slack history endpoints return `slack.Message` with all fields populated. The WS handler receives the raw fields as args; we synthesize a `slack.Message` for marshalling.

- [ ] **Step 1: Locate the four call sites**

```bash
grep -n "db.UpsertMessage\|h.db.UpsertMessage" cmd/slk/main.go
```

Should produce exactly 4 hits (in main.go — ignore test files).

- [ ] **Step 2: Add `RawJSON` to `fetchChannelMessages`**

In the loop body where `db.UpsertMessage(cache.Message{...})` is built (~line 1455), marshal `m` (the `slack.Message`) and assign:

```go
rawBytes, _ := json.Marshal(m)
db.UpsertMessage(cache.Message{
    TS:          m.Timestamp,
    ChannelID:   channelID,
    WorkspaceID: client.TeamID(),
    UserID:      m.User,
    Text:        m.Text,
    ThreadTS:    m.ThreadTimestamp,
    ReplyCount:  m.ReplyCount,
    Subtype:     m.SubType,
    RawJSON:     string(rawBytes),
    CreatedAt:   time.Now().Unix(),
})
```

Ensure `encoding/json` is in the import block (likely already is — check).

- [ ] **Step 3: Apply the same change to `fetchOlderMessages` (~line 1390) and `fetchThreadReplies` (~line 1521)**

Same `rawBytes, _ := json.Marshal(m)` + `RawJSON: string(rawBytes)` line.

- [ ] **Step 4: Update `rtmEventHandler.OnMessage` (~line 1704) to synthesize and marshal**

The handler receives `(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment)`. Reconstruct a `slack.Message` for marshalling:

```go
synthetic := slack.Message{
    Type:            "message",
    Timestamp:       ts,
    User:            userID,
    Text:            text,
    ThreadTimestamp: threadTS,
    SubType:         subtype,
    Files:           files,
    Blocks:          blocks,
    Attachments:     attachments,
}
rawBytes, _ := json.Marshal(synthetic)
h.db.UpsertMessage(cache.Message{
    TS:          ts,
    ChannelID:   channelID,
    WorkspaceID: h.workspaceID,
    UserID:      userID,
    Text:        text,
    ThreadTS:    threadTS,
    Subtype:     subtype,
    RawJSON:     string(rawBytes),
    CreatedAt:   time.Now().Unix(),
})
```

Confirm by reading the `slack.Message` struct in `internal/slack/types.go` (or wherever it lives — `grep -n "^type Message struct" internal/slack/*.go`) that the field names above match. If `Edited` is a struct in `slack.Message`, set `synthetic.Edited` from the `edited bool` arg using whatever the existing convention is (some clients set `Edited.User` non-empty; if uncertain, leave Edited zero — the `EditedAt` cache column already captures the bool aspect indirectly via existing call sites, and our cache reader doesn't currently rely on `RawJSON.Edited`).

- [ ] **Step 5: Build**

```bash
go build ./...
```

Expected: success. Fix any import / type errors.

- [ ] **Step 6: Run all cache + main tests**

```bash
go test ./internal/cache/... ./cmd/slk/... -v
```

Expected: PASS. If WS handler tests fail, inspect the synthetic `slack.Message` construction.

- [ ] **Step 7: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(cache): persist raw_json on every UpsertMessage call site"
```

---

## Task 3: `loadCachedMessages` reader

**Files:**
- Modify: `cmd/slk/main.go` (add helper next to `fetchChannelMessages`)
- Create: `cmd/slk/cache_render_test.go`

This helper produces `[]messages.MessageItem` from SQLite without any network call. Mirrors `fetchChannelMessages`'s enrichment logic exactly so render output is indistinguishable.

- [ ] **Step 1: Write the failing test**

Create `cmd/slk/cache_render_test.go`:

```go
package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack"
)

func TestLoadCachedMessagesEnrichesFromCache(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := cache.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// One message, with a file attachment in raw_json and one reaction.
	raw, _ := json.Marshal(slack.Message{
		Type:      "message",
		Timestamp: "100.0",
		User:      "U1",
		Text:      "hello",
		Files:     []slack.File{{ID: "F1", Name: "x.png", Mimetype: "image/png"}},
	})
	if err := db.UpsertMessage(cache.Message{
		TS: "100.0", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "hello", RawJSON: string(raw), CreatedAt: 1,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.UpsertReaction("100.0", "C1", "thumbsup", []string{"USELF", "U2"}, 2); err != nil {
		t.Fatalf("upsert reaction: %v", err)
	}

	client := slack.NewTestClient("USELF", "T1") // construct via whatever test helper exists; see note
	userNames := map[string]string{"U1": "alice"}
	avatars := avatar.NewCache(t.TempDir(), 10)

	items := loadCachedMessages(db, client, "C1", userNames, "3:04 PM", avatars)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	got := items[0]
	if got.UserName != "alice" {
		t.Errorf("UserName: want alice, got %q", got.UserName)
	}
	if got.Text != "hello" {
		t.Errorf("Text: want hello, got %q", got.Text)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Name != "x.png" {
		t.Errorf("Attachments: want 1 file x.png, got %+v", got.Attachments)
	}
	if len(got.Reactions) != 1 || !got.Reactions[0].HasReacted || got.Reactions[0].Count != 2 {
		t.Errorf("Reactions: want 1 reaction with HasReacted=true count=2, got %+v", got.Reactions)
	}
}

func TestLoadCachedMessagesReturnsNilOnEmptyChannel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, _ := cache.Open(dbPath)
	t.Cleanup(func() { db.Close() })
	got := loadCachedMessages(db, nil, "C-empty", nil, "3:04 PM", nil)
	if got != nil {
		t.Errorf("want nil for empty channel, got %d items", len(got))
	}
}

func TestLoadCachedMessagesHandlesMissingRawJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, _ := cache.Open(dbPath)
	t.Cleanup(func() { db.Close() })
	if err := db.UpsertMessage(cache.Message{
		TS: "1.0", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "no raw", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	client := slack.NewTestClient("USELF", "T1")
	items := loadCachedMessages(db, client, "C1", nil, "3:04 PM", nil)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Text != "no raw" {
		t.Errorf("Text: want 'no raw', got %q", items[0].Text)
	}
	if len(items[0].Attachments) != 0 {
		t.Errorf("Attachments: want none for missing raw_json, got %+v", items[0].Attachments)
	}
}
```

**Test-helper note:** if `slack.NewTestClient` doesn't exist, grep test files for how a `*slackclient.Client` is constructed in tests:

```bash
grep -rn "slackclient.New\|slack.NewClient\|TestClient" cmd/slk/ internal/slack/ | head
```

If no test helper exists, create a minimal one in `internal/slack/client.go` (exported method `NewTestClient(userID, teamID string) *Client`) that returns a `*Client` with only `userID`/`teamID` populated and no transport. If `Client` has unexported fields making this hard, move the test to `cmd/slk` and construct via the existing public `New` function with a stub HTTP server, or skip the client argument by refactoring `loadCachedMessages` to take `selfUserID string` instead of `client` (cleaner — see Step 2).

- [ ] **Step 2: Run test — expect FAIL (function not defined)**

```bash
go test ./cmd/slk/ -run TestLoadCachedMessages -v
```

Expected: `undefined: loadCachedMessages` compile error.

- [ ] **Step 3: Implement `loadCachedMessages`**

Add to `cmd/slk/main.go` directly above `fetchChannelMessages` (~line 1446):

```go
// loadCachedMessages reads up to 50 cached messages for the channel
// from SQLite and reconstructs []messages.MessageItem with the same
// fidelity as fetchChannelMessages — including reactions and (when
// raw_json is present) files/blocks/legacy attachments.
//
// Returns nil if the channel has no cached rows or on any DB error.
// Callers treat nil as "cache miss" and fall through to the network
// fetch path.
func loadCachedMessages(
	db *cache.DB,
	client *slackclient.Client,
	channelID string,
	userNames map[string]string,
	tsFormat string,
	avatarCache *avatar.Cache,
) []messages.MessageItem {
	if db == nil {
		return nil
	}
	rows, err := db.GetMessages(channelID, 50, "")
	if err != nil || len(rows) == 0 {
		return nil
	}

	selfUserID := ""
	if client != nil {
		selfUserID = client.UserID()
	}

	var out []messages.MessageItem
	for _, row := range rows {
		userName, _ := resolveUser(client, row.UserID, userNames, db, avatarCache)

		// Reactions from the reactions table.
		var reactionItems []messages.ReactionItem
		if reacts, err := db.GetReactions(row.TS, channelID); err == nil {
			for _, r := range reacts {
				hasReacted := false
				for _, uid := range r.UserIDs {
					if uid == selfUserID {
						hasReacted = true
						break
					}
				}
				reactionItems = append(reactionItems, messages.ReactionItem{
					Emoji:      r.Emoji,
					Count:      r.Count,
					HasReacted: hasReacted,
				})
			}
		}

		// Files/Blocks/LegacyAttachments from raw_json (best-effort).
		var attachments []messages.Attachment
		var blocks []messages.Block
		var legacy []messages.LegacyAttachment
		if row.RawJSON != "" {
			var slackMsg slack.Message
			if err := json.Unmarshal([]byte(row.RawJSON), &slackMsg); err == nil {
				attachments = extractAttachments(slackMsg.Files)
				blocks = extractBlocks(slackMsg.Blocks)
				legacy = extractLegacyAttachments(slackMsg.Attachments)
			} else {
				log.Printf("warn: cache raw_json unmarshal failed for ts=%s: %v", row.TS, err)
			}
		}

		out = append(out, messages.MessageItem{
			TS:                row.TS,
			UserID:            row.UserID,
			UserName:          userName,
			Text:              row.Text,
			Timestamp:         formatTimestamp(row.TS, tsFormat),
			ThreadTS:          row.ThreadTS,
			ReplyCount:        row.ReplyCount,
			Subtype:           row.Subtype,
			Reactions:         reactionItems,
			Attachments:       attachments,
			Blocks:            blocks,
			LegacyAttachments: legacy,
		})
	}
	return out
}
```

**Type-name verification:** the actual field names on `messages.MessageItem` may differ slightly. Grep first:

```bash
grep -n "type MessageItem struct" internal/ui/messages/model.go
```

Match the exact field names from that struct. In particular: confirm whether the helper types are `messages.Attachment` / `messages.Block` / `messages.LegacyAttachment` or something else, and whether `extractAttachments`/`extractBlocks`/`extractLegacyAttachments` (already used by `fetchChannelMessages`) accept the same types we have here.

- [ ] **Step 4: Run test — expect PASS**

```bash
go test ./cmd/slk/ -run TestLoadCachedMessages -v
```

Expected: PASS for all three test cases. Iterate on field names if compile fails; iterate on enrichment logic if a test assertion fails.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/cache_render_test.go
git commit -m "feat(cache): loadCachedMessages reader reconstructs MessageItems from SQLite"
```

---

## Task 4: Wire `channelCacheReader` into the App

**Files:**
- Modify: `internal/ui/app.go` (new type + field + setter; cache-first branch in `ChannelSelectedMsg`)
- Modify: `cmd/slk/main.go` (call `SetChannelCacheReader` in `wireCallbacks`)
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/app_test.go`:

```go
func TestChannelSelectedRendersFromCacheWithoutSpinner(t *testing.T) {
	a := newTestApp(t) // use existing helper; check tests for actual name

	cachedItems := []messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "from cache"},
	}
	a.SetChannelCacheReader(func(channelID string) []messages.MessageItem {
		if channelID == "C1" {
			return cachedItems
		}
		return nil
	})
	a.SetChannelFetcher(func(channelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: channelID, Messages: nil}
	})

	a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})

	if a.messagepane.IsLoading() {
		t.Errorf("expected loading=false on cache hit, got true")
	}
	got := a.messagepane.Messages()
	if len(got) != 1 || got[0].Text != "from cache" {
		t.Errorf("expected cached items rendered, got %+v", got)
	}
}

func TestChannelSelectedFallsBackToSpinnerOnCacheMiss(t *testing.T) {
	a := newTestApp(t)
	a.SetChannelCacheReader(func(channelID string) []messages.MessageItem { return nil })
	a.SetChannelFetcher(func(channelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: channelID, Messages: nil}
	})

	a.Update(ChannelSelectedMsg{ID: "C2", Name: "alerts", Type: "channel"})

	if !a.messagepane.IsLoading() {
		t.Errorf("expected loading=true on cache miss, got false")
	}
}
```

If `messagepane.IsLoading()` and `messagepane.Messages()` accessors don't exist, this test will fail at compile — that's expected; we add them in Task 5 and Step 4 below.

If `newTestApp` is not the helper name, look for whatever existing tests use (e.g. `newApp`, `setupApp`); follow the same pattern.

- [ ] **Step 2: Add the type, field, and setter**

In `internal/ui/app.go`, near the existing `ChannelFetchFunc` (~line 379):

```go
// ChannelCacheReadFunc is called synchronously when the user selects a
// channel; it returns cached messages from local storage. Returning a
// non-empty slice causes the messagepane to render immediately without
// the loading spinner. Returning nil falls through to the network
// fetcher.
type ChannelCacheReadFunc func(channelID string) []messages.MessageItem
```

Add the field next to `channelFetcher` (~line 625):

```go
channelCacheReader ChannelCacheReadFunc
```

Add the setter near `SetChannelFetcher` (~line 3595):

```go
// SetChannelCacheReader sets the optional synchronous reader that
// pulls messages from the local cache before the network fetcher
// fires. Pass nil to disable.
func (a *App) SetChannelCacheReader(fn ChannelCacheReadFunc) {
	a.channelCacheReader = fn
}
```

- [ ] **Step 3: Update the `ChannelSelectedMsg` handler**

Replace the block at `case ChannelSelectedMsg:` (~lines 1315-1329) — specifically the `SetLoading(true)` / `SetMessages(nil)` / fetcher dispatch — with:

```go
		a.messagepane.SetChannel(msg.Name, "")
		a.messagepane.SetChannelType(msg.Type)

		var cached []messages.MessageItem
		if a.channelCacheReader != nil {
			cached = a.channelCacheReader(msg.ID)
		}
		if len(cached) > 0 {
			a.messagepane.SetLoading(false)
			a.messagepane.SetMessages(cached)
		} else {
			a.messagepane.SetLoading(true)
			a.messagepane.SetMessages(nil)
		}
		a.compose.SetChannel(msg.Name)
		a.statusbar.SetChannel(msg.Name)
		a.statusbar.SetChannelType(msg.Type)
		// Always fetch fresh from the network in the background; the
		// cached render is best-effort. MessagesLoadedMsg will
		// authoritatively replace whatever cache rendered.
		if a.channelFetcher != nil {
			fetcher := a.channelFetcher
			chID, chName := msg.ID, msg.Name
			cmds = append(cmds, func() tea.Msg {
				return fetcher(chID, chName)
			})
		}
```

(Preserve every line outside the modified slice; in particular do not lose the existing `cancelEdit`, `view`, `lastOpenedChannelID`, `CloseThread`, `clearSelections`, `focusedPanel`, `activeChannelID`, `lastTypingSent`, `sidebar.SetActiveChannelID` block above.)

- [ ] **Step 4: Add minimal accessors on `messages.Model`**

Edit `internal/ui/messages/model.go` near `SetLoading` (~line 940):

```go
func (m *Model) IsLoading() bool { return m.loading }

// Messages returns a defensive copy of the current items slice. Test
// helper; do not mutate the returned slice.
func (m *Model) Messages() []MessageItem {
	out := make([]MessageItem, len(m.messages))
	copy(out, m.messages)
	return out
}
```

If `Messages()` already exists, skip its addition.

- [ ] **Step 5: Wire the cache reader in `cmd/slk/main.go wireCallbacks`**

Find the `app.SetChannelFetcher(...)` call inside `wireCallbacks`. Immediately above or below it, add:

```go
app.SetChannelCacheReader(func(channelID string) []messages.MessageItem {
    return loadCachedMessages(db, client, channelID, userNames, tsFormat, avatarCache)
})
```

(Use the exact local variable names that the closure captures elsewhere in `wireCallbacks` — `db`, `client`, `userNames`, `tsFormat`, `avatarCache` — likely already in scope.)

- [ ] **Step 6: Build + run the targeted tests**

```bash
go build ./...
go test ./internal/ui/ -run "TestChannelSelectedRendersFromCache|TestChannelSelectedFallsBackToSpinner" -v
go test ./cmd/slk/ -run TestLoadCachedMessages -v
```

Expected: all PASS.

- [ ] **Step 7: Run the full UI suite to catch regressions**

```bash
go test ./internal/ui/...
```

Expected: PASS. If any existing channel-selection test breaks because it now sees an instant render, update it to set `SetChannelCacheReader(nil)` (or a stub returning nil) before dispatching `ChannelSelectedMsg`.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go internal/ui/messages/model.go internal/ui/app_test.go cmd/slk/main.go
git commit -m "feat(ui): cache-first ChannelSelectedMsg renders from SQLite before network"
```

---

## Task 5: Race fix — `WorkspaceSwitchedMsg` and `WorkspaceReadyMsg`

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

The handlers call `SetMessages(nil)` without `SetLoading(true)`. Between the current tick and the deferred `ChannelSelectedMsg` cmd, the messagepane renders the empty-state branch with no spinner — "No messages yet" flashes.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/app_test.go`:

```go
func TestWorkspaceSwitchedSetsLoadingBeforeChannelSelect(t *testing.T) {
	a := newTestApp(t)
	a.SetChannelCacheReader(func(channelID string) []messages.MessageItem { return nil })
	a.SetChannelFetcher(func(channelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: channelID, Messages: nil}
	})

	a.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		Channels: []ChannelItem{{ID: "C9", Name: "general", Type: "channel"}},
	})

	// At this point the deferred ChannelSelectedMsg cmd has not yet
	// run. The pane MUST already be in loading state to avoid the
	// "No messages yet" flash.
	if !a.messagepane.IsLoading() {
		t.Fatalf("expected messagepane loading=true between ticks")
	}
	if got := a.messagepane.Messages(); len(got) != 0 {
		t.Fatalf("expected messages cleared, got %d", len(got))
	}
}

func TestWorkspaceReadyFirstChannelSetsLoading(t *testing.T) {
	a := newTestApp(t)
	a.SetChannelCacheReader(func(channelID string) []messages.MessageItem { return nil })

	a.Update(WorkspaceReadyMsg{
		TeamID:   "T1",
		TeamName: "Acme",
		Channels: []ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
	})
	if !a.messagepane.IsLoading() {
		t.Fatalf("expected messagepane loading=true on first-channel auto-select")
	}
}
```

If `WorkspaceSwitchedMsg`/`WorkspaceReadyMsg`/`ChannelItem` field names differ, fix the test from the local declarations (grep `type WorkspaceSwitchedMsg struct`).

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/ui/ -run "TestWorkspaceSwitchedSetsLoading|TestWorkspaceReadyFirstChannelSetsLoading" -v
```

Expected: FAIL — `loading` is false.

- [ ] **Step 3: Insert `SetLoading(true)` in `WorkspaceSwitchedMsg`**

Find the existing `a.messagepane.SetMessages(nil)` line in the `WorkspaceSwitchedMsg` handler (currently around line 1830 — verify by grep). Insert directly before it:

```go
		a.messagepane.SetLoading(true)
		a.messagepane.SetMessages(nil)
```

- [ ] **Step 4: Insert `SetLoading(true)` in `WorkspaceReadyMsg`**

In the `WorkspaceReadyMsg` first-channel branch (`if a.activeChannelID == ""`), find the `if len(msg.Channels) > 0 { ... ChannelSelectedMsg ... }` block (~line 1961). Just before the `cmds = append(...)` that dispatches `ChannelSelectedMsg`, add:

```go
				a.messagepane.SetLoading(true)
				a.messagepane.SetMessages(nil)
```

- [ ] **Step 5: Run targeted tests**

```bash
go test ./internal/ui/ -run "TestWorkspaceSwitchedSetsLoading|TestWorkspaceReadyFirstChannelSetsLoading" -v
```

Expected: PASS.

- [ ] **Step 6: Run full UI suite**

```bash
go test ./internal/ui/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "fix(ui): set loading=true before SetMessages(nil) on workspace switch and ready"
```

---

## Task 6: Move spinner runes to `internal/ui/styles`

**Files:**
- Create: `internal/ui/styles/spinner.go`
- Modify: `internal/ui/app.go` (drop the local `spinnerChars`, switch to `styles.SpinnerChars`; widen mod arithmetic)

- [ ] **Step 1: Create the styles file**

`internal/ui/styles/spinner.go`:

```go
package styles

// SpinnerChars is the braille-dot rotation used by the workspace
// loading overlay and the messagepane "Loading messages..." indicator.
var SpinnerChars = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
```

- [ ] **Step 2: Update `internal/ui/app.go`**

Find the existing local declaration:

```bash
grep -n "spinnerChars\s*=\s*\[\]rune" internal/ui/app.go
```

Delete it. Replace every reference to `spinnerChars[...]` with `styles.SpinnerChars[...]`. Ensure `"github.com/gammons/slk/internal/ui/styles"` is imported (likely already is).

In `SpinnerTickMsg` handler (~line 1907), change:

```go
a.spinnerFrame = (a.spinnerFrame + 1) % 10
```

to:

```go
a.spinnerFrame = (a.spinnerFrame + 1) % len(styles.SpinnerChars)
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: success.

- [ ] **Step 4: Run UI tests**

```bash
go test ./internal/ui/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/styles/spinner.go internal/ui/app.go
git commit -m "refactor(ui): move SpinnerChars to internal/ui/styles for cross-package use"
```

---

## Task 7: Animated spinner in messagepane

**Files:**
- Modify: `internal/ui/messages/model.go`
- Modify: `internal/ui/app.go` (forward spinner frame; widen tick gate; kick off tick on cache-miss)
- Modify: `internal/ui/messages/model_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/messages/model_test.go` (or create if absent):

```go
func TestEmptyStateRendersSpinnerWhenLoading(t *testing.T) {
	m := New(/* whatever args the existing tests use */)
	m.SetSize(80, 20)
	m.SetLoading(true)
	m.SetSpinnerFrame(0)
	out := m.View()
	// First glyph in styles.SpinnerChars is ⠋. The literal must appear
	// somewhere in the rendered output.
	if !strings.Contains(out, "⠋ Loading messages...") {
		t.Errorf("expected spinner-prefixed loading text in output:\n%s", out)
	}
}

func TestEmptyStateRendersPlainTextWhenNotLoading(t *testing.T) {
	m := New(/* same args */)
	m.SetSize(80, 20)
	m.SetLoading(false)
	out := m.View()
	if !strings.Contains(out, "No messages yet") {
		t.Errorf("expected 'No messages yet' fallback in output")
	}
}
```

If the existing model_test.go has a constructor pattern, copy it; otherwise grep an example: `grep -n "messages\.New\|New(" internal/ui/messages/*_test.go`.

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/ui/messages/ -run TestEmptyStateRendersSpinner -v
```

Expected: compile error (`SetSpinnerFrame undefined`) or test FAIL.

- [ ] **Step 3: Add `spinnerFrame` + `SetSpinnerFrame` to `messages.Model`**

Field: append to the struct (around line ~194 next to `loading`):

```go
	spinnerFrame int
```

Setter (next to `SetLoading`):

```go
func (m *Model) SetSpinnerFrame(f int) {
	if m.spinnerFrame != f {
		m.spinnerFrame = f
		m.dirty()
	}
}
```

- [ ] **Step 4: Render the spinner in the empty-state branch**

Edit `internal/ui/messages/model.go:2016-2020`:

```go
	if len(m.messages) == 0 {
		text := "No messages yet"
		if m.loading {
			frame := styles.SpinnerChars[m.spinnerFrame%len(styles.SpinnerChars)]
			text = string(frame) + " Loading messages..."
		}
		empty := lipgloss.NewStyle().
			Width(width).
			Height(msgAreaHeight).
			Foreground(styles.TextMuted).
			Background(styles.Background).
			Render(text)
		return chrome + "\n" + empty
	}
```

Confirm `styles` is already imported in this file; if not, add `"github.com/gammons/slk/internal/ui/styles"`.

- [ ] **Step 5: Render the spinner on the "Loading older messages..." line**

Find `m.cacheLoadingHint = hintStyle.Render("  Loading older messages...")` (~line 1048). The cache hint is built once per cache rebuild, so a static string here means it won't animate. Two options:

(a) **Static hint with a leading spinner glyph that updates only when the cache rebuilds.** Acceptable as a v1 — keeps the cache invalidation surface minimal:

```go
frame := styles.SpinnerChars[m.spinnerFrame%len(styles.SpinnerChars)]
m.cacheLoadingHint = hintStyle.Render("  " + string(frame) + " Loading older messages...")
```

(b) **True animation.** Force a cache rebuild on every spinner-frame change (i.e. `SetSpinnerFrame` calls `m.dirty()` — which it does in Step 3). Then the rebuild uses the latest `m.spinnerFrame`. (a) plus the dirty in Step 3 already gives us this.

Use (a) + the existing `m.dirty()` from Step 3 — that's the simplest path that animates cleanly.

- [ ] **Step 6: Forward the spinner frame from App to messagepane and widen the tick gate**

In `internal/ui/app.go` at the `SpinnerTickMsg` handler (~line 1905), change:

```go
case SpinnerTickMsg:
	if a.loading {
		a.spinnerFrame = (a.spinnerFrame + 1) % 10
		return a, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
			return SpinnerTickMsg{}
		})
	}
```

to:

```go
case SpinnerTickMsg:
	if a.loading || a.messagepane.IsLoading() {
		a.spinnerFrame = (a.spinnerFrame + 1) % len(styles.SpinnerChars)
		a.messagepane.SetSpinnerFrame(a.spinnerFrame)
		return a, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
			return SpinnerTickMsg{}
		})
	}
```

- [ ] **Step 7: Kick off the tick when the messagepane enters loading**

In the `ChannelSelectedMsg` handler, in the cache-miss branch (`else` block where `SetLoading(true)` runs) added in Task 4 Step 3, append a tick command:

```go
		} else {
			a.messagepane.SetLoading(true)
			a.messagepane.SetMessages(nil)
			cmds = append(cmds, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			}))
		}
```

Same in the two race-fix call sites (Task 5 Steps 3 and 4) — append the same `tea.Tick` cmd. Duplicate ticks are filtered by the gate; idempotent.

The on-screen spinner now animates while either the workspace overlay or the messagepane is loading. Once the network fetch lands and `MessagesLoadedMsg` calls `SetLoading(false)`, the next `SpinnerTickMsg` evaluates the gate as false and the tick stops.

- [ ] **Step 8: Build + run targeted tests**

```bash
go build ./...
go test ./internal/ui/messages/ -run TestEmptyStateRendersSpinner -v
```

Expected: PASS.

- [ ] **Step 9: Run full UI suite**

```bash
go test ./internal/ui/...
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/messages/model_test.go internal/ui/app.go
git commit -m "feat(ui): animate Loading messages... with shared braille spinner"
```

---

## Task 8: Cache-first thread replies

**Files:**
- Modify: `cmd/slk/main.go` (add `loadCachedThreadReplies`; wire setter)
- Modify: `cmd/slk/cache_render_test.go` (new test)
- Modify: `internal/ui/app.go` (new type/field/setter; cache-first branch in `threadFetchDebounceMsg` and any direct `ThreadOpenedMsg` paths)

The thread panel has no loading indicator today, so this task is purely about avoiding the network wait when re-opening a thread whose replies are already cached.

- [ ] **Step 1: Write the failing test**

Append to `cmd/slk/cache_render_test.go`:

```go
func TestLoadCachedThreadRepliesEnrichesFromCache(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, _ := cache.Open(dbPath)
	t.Cleanup(func() { db.Close() })

	// Parent + 2 replies, all in the same thread.
	for _, m := range []cache.Message{
		{TS: "100.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "parent", ThreadTS: "100.0", CreatedAt: 1},
		{TS: "101.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "reply 1", ThreadTS: "100.0", CreatedAt: 2},
		{TS: "102.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "reply 2", ThreadTS: "100.0", CreatedAt: 3},
	} {
		if err := db.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}

	client := slack.NewTestClient("USELF", "T1")
	items := loadCachedThreadReplies(db, client, "C1", "100.0", nil, "3:04 PM", nil)
	if len(items) != 3 {
		t.Fatalf("want 3 thread items, got %d", len(items))
	}
	if items[0].Text != "parent" || items[2].Text != "reply 2" {
		t.Errorf("unexpected ordering: %+v", items)
	}
}
```

- [ ] **Step 2: Run — expect compile error (function not defined)**

```bash
go test ./cmd/slk/ -run TestLoadCachedThreadReplies -v
```

- [ ] **Step 3: Implement `loadCachedThreadReplies`**

Add to `cmd/slk/main.go` directly above `fetchThreadReplies`:

```go
// loadCachedThreadReplies reads cached thread replies (parent + all
// replies under thread_ts) from SQLite and reconstructs
// []messages.MessageItem with the same fidelity as fetchThreadReplies.
// Returns nil when nothing is cached or on DB error.
func loadCachedThreadReplies(
	db *cache.DB,
	client *slackclient.Client,
	channelID, threadTS string,
	userNames map[string]string,
	tsFormat string,
	avatarCache *avatar.Cache,
) []messages.MessageItem {
	if db == nil {
		return nil
	}
	rows, err := db.GetThreadReplies(channelID, threadTS)
	if err != nil || len(rows) == 0 {
		return nil
	}

	selfUserID := ""
	if client != nil {
		selfUserID = client.UserID()
	}

	var out []messages.MessageItem
	for _, row := range rows {
		userName, _ := resolveUser(client, row.UserID, userNames, db, avatarCache)
		var reactionItems []messages.ReactionItem
		if reacts, err := db.GetReactions(row.TS, channelID); err == nil {
			for _, r := range reacts {
				hasReacted := false
				for _, uid := range r.UserIDs {
					if uid == selfUserID {
						hasReacted = true
						break
					}
				}
				reactionItems = append(reactionItems, messages.ReactionItem{
					Emoji: r.Emoji, Count: r.Count, HasReacted: hasReacted,
				})
			}
		}
		var attachments []messages.Attachment
		var blocks []messages.Block
		var legacy []messages.LegacyAttachment
		if row.RawJSON != "" {
			var slackMsg slack.Message
			if err := json.Unmarshal([]byte(row.RawJSON), &slackMsg); err == nil {
				attachments = extractAttachments(slackMsg.Files)
				blocks = extractBlocks(slackMsg.Blocks)
				legacy = extractLegacyAttachments(slackMsg.Attachments)
			}
		}
		out = append(out, messages.MessageItem{
			TS:                row.TS,
			UserID:            row.UserID,
			UserName:          userName,
			Text:              row.Text,
			Timestamp:         formatTimestamp(row.TS, tsFormat),
			ThreadTS:          row.ThreadTS,
			ReplyCount:        row.ReplyCount,
			Subtype:           row.Subtype,
			Reactions:         reactionItems,
			Attachments:       attachments,
			Blocks:            blocks,
			LegacyAttachments: legacy,
		})
	}
	return out
}
```

(The duplication with `loadCachedMessages` is acceptable — the inner enrichment is small and the read sources differ. If it grows, factor into a `enrichRow(...)` helper later.)

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./cmd/slk/ -run TestLoadCachedThreadReplies -v
```

- [ ] **Step 5: Add the App-side type, field, and setter**

In `internal/ui/app.go` next to `ThreadFetchFunc` (~line 517):

```go
// ThreadCacheReadFunc is called synchronously when a thread is
// opened; returns cached replies (or nil) so the thread panel can
// populate without waiting for the network.
type ThreadCacheReadFunc func(channelID, threadTS string) []messages.MessageItem
```

Field next to `threadFetcher` (~line 641):

```go
threadCacheReader ThreadCacheReadFunc
```

Setter next to `SetThreadFetcher` (~line 3653):

```go
func (a *App) SetThreadCacheReader(fn ThreadCacheReadFunc) {
	a.threadCacheReader = fn
}
```

- [ ] **Step 6: Use the cache reader where threads open**

Two sites use `threadFetcher`:

1. `case threadFetchDebounceMsg:` (~line 1631): the j/k debounce path that fires when the selection lands on a thread.
2. `app.go:3231-3232` and `app.go:3393-3396`: direct user-action paths (likely the `Enter` / hotkey to open a thread).

For each site, immediately before dispatching the network fetcher's `tea.Cmd`, populate the panel from cache when possible:

```go
if a.threadCacheReader != nil {
    if cached := a.threadCacheReader(chID, threadTS); len(cached) > 0 {
        // Render directly into the thread panel. If the panel hasn't
        // been opened yet, we still want the data warm so the open
        // animation lands on populated content.
        if a.threadVisible && a.threadPanel.ThreadTS() == threadTS {
            a.threadPanel.SetThread(a.threadPanel.ParentMsg(), cached, chID, threadTS)
        }
    }
}
```

The exact open-thread call sequence may differ; check what `ThreadOpenedMsg` / `OpenThread` does in the codebase. The principle: feed the panel cached replies synchronously, let `ThreadRepliesLoadedMsg` (`app.go:1635`) replace them when the network call returns.

If the open path is more complex than this, a cleaner alternative: emit a synthetic `ThreadRepliesLoadedMsg` from the cache hit before dispatching the fetcher, since the existing handler already does the right thing with replies. Pseudocode:

```go
if a.threadCacheReader != nil {
    cached := a.threadCacheReader(chID, threadTS)
    if len(cached) > 0 {
        cmds = append(cmds, func() tea.Msg {
            return ThreadRepliesLoadedMsg{ThreadTS: threadTS, Replies: cached}
        })
    }
}
```

— this lets the existing reducer place the replies, and the subsequent network `ThreadRepliesLoadedMsg` will overwrite with authoritative data on the next tick. Prefer this approach unless the panel needs to be populated *before* the next tick (it doesn't — thread open already involves animation).

- [ ] **Step 7: Wire the cache reader in `wireCallbacks`**

In `cmd/slk/main.go`, next to the existing `app.SetThreadFetcher(...)` call, add:

```go
app.SetThreadCacheReader(func(channelID, threadTS string) []messages.MessageItem {
    return loadCachedThreadReplies(db, client, channelID, threadTS, userNames, tsFormat, avatarCache)
})
```

- [ ] **Step 8: Build + run targeted + full UI tests**

```bash
go build ./...
go test ./cmd/slk/ -run TestLoadCachedThreadReplies -v
go test ./internal/ui/...
```

Expected: PASS.

- [ ] **Step 9: Manual smoke test (optional but recommended)**

If a Slack workspace is configured locally and you have `bin/slk` built, run it, open a thread, switch channel, reopen the thread — the panel should populate instantly, with the network fetch silently topping off any new replies.

- [ ] **Step 10: Commit**

```bash
git add cmd/slk/main.go cmd/slk/cache_render_test.go internal/ui/app.go
git commit -m "feat(ui): cache-first thread replies via ThreadCacheReadFunc"
```

---

## Task 9: Verify end-to-end

- [ ] **Step 1: Full build**

```bash
go build ./...
```

- [ ] **Step 2: Full test suite**

```bash
go test ./...
```

Expected: all green. Report any failures.

- [ ] **Step 3: Spec coverage check**

Re-read `docs/superpowers/specs/2026-05-03-cache-first-message-loading-design.md`. For each goal:

- [ ] Channel re-entry renders from SQLite — Tasks 2-4
- [ ] Background refresh always runs — Task 4 Step 3
- [ ] "No messages yet" flash eliminated on workspace switch / cold start — Task 5
- [ ] Spinner animates "Loading messages..." — Task 7
- [ ] Spinner animates "Loading older messages..." — Task 7 Step 5
- [ ] Thread cache reads — Task 8

---

## Self-Review Notes

Areas to watch when implementing:

1. **`slack.Message` field names.** The plan assumes `Timestamp`, `User`, `Text`, `ThreadTimestamp`, `SubType`, `Files`, `Blocks`, `Attachments`, `ReplyCount`. If they differ (e.g. `Ts` vs `Timestamp`, `SubType` vs `Subtype`), correct in-place — the marshalling step is the only place this matters since unmarshal uses the same shape.

2. **`slackclient.Client` test construction.** If no test helper exists, the cleanest fix is to refactor `loadCachedMessages` to take `selfUserID string` (and similarly for `loadCachedThreadReplies`) instead of a `*slackclient.Client`. Avatars are also stored by user ID, so `avatarCache *avatar.Cache` and `userNames map[string]string` together give us everything we need without dragging the live client into the test.

3. **`messages.Model` `Messages()` accessor.** May already exist. If it does, do not redefine; just use it from the test.

4. **Channel and Thread `cmds` slice mutation.** The handlers use `cmds = append(cmds, ...)` and the function returns `tea.Batch(cmds...)`. Stay inside that idiom.

5. **`MessagesLoadedMsg` semantics.** It always replaces wholesale. The brief moment between "render cached items" and "MessagesLoadedMsg replaces" is where reactions/edits that arrived over WS *after* the cache read but *before* the network response could double-show. In practice the network response is canonical so the replace converges within one round-trip; no extra dedup logic needed.
