# Channel Marks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pressing `m{letter}` on a sidebar conversation records a channel mark; `'{letter}` opens that conversation at its latest message.

**Architecture:** A channel mark is an ordinary `Mark` whose `Location` has empty `MessageTS` and `ThreadTS`. Marks gain a `ChannelType` snapshot (stored in a new `marks.channel_type` column) so the overlay can label DMs `@name`. The jump reuses `applyLocation`; only the same-channel in-place path learns to go to the newest message for a channel-only location.

**Tech Stack:** Go, Bubble Tea v2, SQLite (`internal/cache`).

**Spec:** `docs/superpowers/specs/2026-11-27-channel-marks-design.md`

## Global Constraints

- Channel mark = `MessageTS == "" && ThreadTS == ""`; no separate kind flag.
- Sidebar-focused `m` with no conversation row highlighted toasts exactly `No conversation selected to mark` (2s) and records nothing.
- Overlay prefix `@` for channel types `dm`, `group_dm`, `app`; `#` for everything else, including empty (old rows).
- A channel mark must never cause `FetchAround`.
- Message marks from the messages pane, thread panel, Threads view and Drafts view keep today's behaviour.
- Persistence rules unchanged (uppercase always persists; lowercase only with `persist_all`).
- Commit messages: a single `<type>: <description>` line, no body.

## Review Focus

- Overwriting a message mark with a channel mark on the same letter must clear the old `MessageTS` (persisted row too) — Task 1 test `TestUpsertMark_ChannelMarkClearsMessageTS`.
- A DB created before this change (no `channel_type` column) must open and keep its marks — Task 1 test `TestMigrate_AddsMarksChannelType`.
- Marking the conversation that is already open from the sidebar, then jumping, must land on the newest message and move focus to the messages pane — Task 3 test `TestJumpMark_ChannelMarkSameChannelGoesToNewest`.
- Sidebar cursor on the Threads/Drafts rows or a section header must not record a mark — Task 2 test `TestSetMark_SidebarNonConversationToasts`.
- A DM message mark (not just a channel mark) shows `@name` — Task 2 test `TestSetMark_MessageMarkRecordsChannelType` + Task 4 render test.

---

### Task 1: Store `ChannelType` with marks

**Files:**
- Modify: `internal/cache/marks.go` (struct, `UpsertMark`, `ListMarks`)
- Modify: `internal/cache/db.go` (`marks` CREATE TABLE ~line 174; migrations block ~line 229)
- Modify: `internal/ui/marks.go` (`Mark` struct)
- Modify: `cmd/slk/main.go:1524-1555` (persist adapter: map `ChannelType` both ways)
- Test: `internal/cache/marks_test.go`, `internal/cache/db_test.go`

**Interfaces:**
- Produces: `cache.Mark.ChannelType string`; `ui.Mark.ChannelType string` (after `ChannelName`); column `channel_type TEXT NOT NULL DEFAULT ''`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cache/marks_test.go
func TestUpsertMark_RoundTripsChannelType(t *testing.T) {
	// New(":memory:"); m := testMark("A"); m.MessageTS = ""; m.ChannelType = "dm"
	// UpsertMark("T1","A",m); ListMarks("T1")
	// want len 1, [0].ChannelType == "dm", [0].MessageTS == ""
}
func TestUpsertMark_ChannelMarkClearsMessageTS(t *testing.T) {
	// upsert testMark("A") (MessageTS set), then same letter with MessageTS "" and ThreadTS ""
	// want ListMarks[0].MessageTS == "" && ThreadTS == ""
}
// internal/cache/db_test.go
func TestMigrate_AddsMarksChannelType(t *testing.T) {
	// file DB in t.TempDir(): New(path); conn.Exec("ALTER TABLE marks DROP COLUMN channel_type"); Close()
	// reopen New(path); hasColumn("marks","channel_type") == true, err nil
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cache/ -run 'ChannelType|ChannelMarkClears'`
Expected: FAIL (compile error: unknown field `ChannelType`).

- [ ] **Step 3: Implement**

Add the field to both structs, the column to the CREATE TABLE, `db.addColumnIfMissing("marks", "channel_type", "ALTER TABLE marks ADD COLUMN channel_type TEXT NOT NULL DEFAULT ''")` next to the other migrations, include `channel_type` in the upsert's INSERT/UPDATE SET and the SELECT/Scan, and map `ChannelType` in both directions in the `main.go` adapter.

- [ ] **Step 4: Verify**

Run: `go test ./internal/cache/ ./internal/ui/ ./cmd/slk/ && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat: store channel type with marks"`

---

### Task 2: Sidebar `m` sets a channel mark

**Files:**
- Modify: `internal/ui/set_mark.go` (`setMark`)
- Test: `internal/ui/set_mark_test.go`

**Interfaces:**
- Consumes: `ui.Mark.ChannelType` (Task 1); `a.sidebar.SelectedItem() (sidebar.ChannelItem, bool)`; `a.channels.Lookup(id) (name, chType string, found bool)`.
- Produces: channel marks shaped `Mark{Location{TeamID: activeTeamID, ChannelID: item.ID}, Letter, ChannelName: item.Name, ChannelType: item.Type}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSetMark_SidebarRecordsChannelMark(t *testing.T) {
	// app := setMarkTestApp(t); messagepane has a selected message in C1 (proves it is ignored)
	// app.sidebar.SetItems([]sidebar.ChannelItem{{ID: "D9", Name: "alice", Type: "dm"}})
	// move cursor to D9 (sidebar.SelectByID("D9")); app.focusedPanel = PanelSidebar
	// pressMarkChord(app,'a') == nil
	// Load("T1","a"): ChannelID "D9", MessageTS "", ThreadTS "", ChannelName "alice",
	//   ChannelType "dm", AuthorName "", Excerpt ""
}
func TestSetMark_SidebarUppercasePersists(t *testing.T) {
	// same setup, app.SetMarksPersistStore(marksPersistForTest(t)); press 'A'
	// a fresh marksStore over the same persist store Loads "A" with ChannelID "D9", MessageTS ""
}
func TestSetMark_SidebarNonConversationToasts(t *testing.T) {
	// sidebar cursor on the Threads row (default after SetItems); focusedPanel = PanelSidebar
	// cmd != nil, toast text "No conversation selected to mark"; Load("T1","a") ok == false
}
func TestSetMark_MessageMarkRecordsChannelType(t *testing.T) {
	// channel lookup returns ("alice","dm",true); message selected in messages pane; press 'a'
	// Load: MessageTS set, ChannelType "dm"
}
```

Find how existing tests read toast text (`TestSetMark_NoSelectionToastsAndRecordsNothing`) and assert the same way.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestSetMark_'`
Expected: the four new tests FAIL; existing `TestSetMark_*` PASS.

- [ ] **Step 3: Implement**

At the top of `setMark`: if `a.focusedPanel == PanelSidebar`, return `a.setChannelMark(letter)` — a new method in `set_mark.go` that builds the mark from `SelectedItem()`, toasts on `!ok`, and calls `a.marks.Set` with the same failure toast as `setMark`. In the existing path, set `m.ChannelType` from the `Lookup` call that already fills `ChannelName`. Update the comment that says a sidebar-focused mark names the channel pane's selection.

- [ ] **Step 4: Verify**

Run: `go test ./internal/ui/ -run 'Mark'`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "feat: mark a conversation from the sidebar"`

---

### Task 3: Channel-mark jump lands on the newest message

**Files:**
- Modify: `internal/ui/reducer_links.go` (`applyLocation` same-channel branch, ~line 254-283)
- Test: `internal/ui/jump_mark_test.go`

**Interfaces:**
- Consumes: `a.messagepane.GoToBottom()` (`internal/ui/messages/model.go:1149`).

- [ ] **Step 1: Write the failing tests**

```go
func TestJumpMark_ChannelMarkSameChannelGoesToNewest(t *testing.T) {
	// app := jumpMarkTestApp(t); seedMark("a", Location{TeamID:"T1", ChannelID:"C1"})
	// app.Update(ChannelSelectedMsg{ID:"C1",...}); messagepane.SelectByTS("1.0"); focusedPanel = PanelSidebar
	// pressJumpChord(app,'a') == nil (in place)
	// selected TS == "2.0"; focusedPanel == PanelMessages; app.pendingLinkNav == nil
}
func TestJumpMark_ChannelMarkOtherChannelOpensWithoutFetchAround(t *testing.T) {
	// seedMark("a", Location{TeamID:"T1", ChannelID:"C2"}); active C1
	// cmd := pressJumpChord(app,'a'); driveJump(app, cmd)
	// activeChannelID == "C2"; selected TS == "10.0"; pendingLinkNav == nil
	// no FetchAround: install the same fetch-around spy TestJumpMark_OutsideLoadedHistoryFetchesAround uses and assert it was not called
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestJumpMark_ChannelMark'`
Expected: `SameChannelGoesToNewest` FAILS (selection stays on 1.0); the other may already pass and stays as a guard.

- [ ] **Step 3: Implement**

In `applyLocation`'s same-channel branch, after `a.focusedPanel = PanelMessages`, call `a.messagepane.GoToBottom()` when `loc.MessageTS == "" && loc.ThreadTS == ""`. Do not touch `completePendingLinkNav` (history walks also produce channel-only locations).

- [ ] **Step 4: Verify**

Run: `go test ./internal/ui/ -run 'Jump|BackJump|Link|History'`
Expected: PASS.

- [ ] **Step 5: Commit**

`git commit -m "fix: channel mark jump lands on newest message"`

---

### Task 4: Overlay labels and help text

**Files:**
- Modify: `internal/ui/marks/marks.go` (`Row`, `renderBox`)
- Modify: `internal/ui/marks_overlay.go` (`marksRows`)
- Modify: `internal/ui/keys.go:142` (MarkSet help)
- Test: `internal/ui/marks/marks_test.go`

**Interfaces:**
- Consumes: `ui.Mark.ChannelType` (Task 1).
- Produces: `marks.Row.ChannelType string`.

- [ ] **Step 1: Write the failing tests**

```go
func TestView_ChannelMarkRows(t *testing.T) {
	// rows: {Letter:"a", ChannelName:"alice", ChannelType:"dm"},
	//       {Letter:"b", ChannelName:"team", ChannelType:"channel"},
	//       {Letter:"c", ChannelName:"old"} // pre-migration row
	// view contains "@alice", "#team", "#old"; does not contain "#alice" or ": "
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/marks/ -run TestView_ChannelMarkRows`
Expected: FAIL (unknown field / "#alice").

- [ ] **Step 3: Implement**

Add `ChannelType` to `Row`; in `renderBox` pick the prefix per Global Constraints; `marksRows` copies `m.ChannelType`. Change the MarkSet help to `key.WithHelp("m", "set mark (sidebar: conversation)")`.

- [ ] **Step 4: Verify**

Run: `go test ./... && go vet ./...`
Expected: PASS (golden tests included; if a golden help snapshot changes, regenerate it with the repo's golden update flag and check the diff is only the help line).

- [ ] **Step 5: Commit**

`git commit -m "feat: label DM marks with @ in the marks overlay"`
