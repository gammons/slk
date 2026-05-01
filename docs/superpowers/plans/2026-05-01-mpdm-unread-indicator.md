# DM/MPDM Unread Indicator Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make sidebar unread indicators reliable for DMs, mpdms, and inactive-workspace activity. Three related bugs, fixed together so the sidebar's "this conversation has new messages" signal always renders correctly.

**Architecture:** Three independent fixes in one plan because they all surface as "the unread indicator is missing or wrong":

1. **Conversation-opened events** — Listen for Slack's `mpim_open`, `im_created`, `group_joined`, `channel_joined` WS events so the sidebar's `m.items` slice stays in sync with Slack's reality. Once an item exists, the existing `MarkUnread` + render path Just Works (it's already type-agnostic).
2. **Cross-workspace unread persistence** — When a `message` event arrives for an inactive workspace, also bump that workspace's `WorkspaceContext.Channels[i].UnreadCount` so switching in shows the correct state. Today only the rail dot is updated.
3. **Bold name on unread DMs** — The styled DM/private/app/group_dm prefix glyph emits a mid-label `\x1b[m` reset that wipes the outer `ChannelUnread` style's bold attribute for everything to its right. Re-apply the bold attribute alongside the bg/fg in the sidebar's `ReapplyBgAfterResets` invocations for unread rows.

**Tech Stack:** Go, bubbletea, slack-go, lipgloss. No new dependencies.

---

## Background

### Bug 1: New conversations missing from the sidebar

The render code in `internal/ui/sidebar/model.go:993-998` already styles unread mpdms identically to other channels (`ChannelUnread` when `UnreadCount > 0`). The bug surfaces when:

1. A new mpdm/dm/group is created or first-opened mid-session and isn't in `users.conversations` at startup.
2. An inbound `message` event arrives for that channel ID.
3. `app.go:1422` calls `a.sidebar.MarkUnread(channelID)`.
4. `sidebar.MarkUnread` (model.go:534-545) silently no-ops because the channel isn't in `m.items`.

Result: no indicator, no bold name. The fix is to surface conversation-creation events so `m.items` stays in sync with Slack's reality. Tasks 1-5.

### Bug 2: Inactive-workspace activity doesn't persist per-channel unread

Reproducer: workspace A and B both connected; B is focused; a DM arrives in A. The desktop notification fires, the workspace rail shows A's unread dot. Switch to A: the DM channel does not show as unread.

In `cmd/slk/main.go:1720-1727` (`rtmEventHandler.OnMessage`), the inactive-workspace branch only sends `WorkspaceUnreadMsg` to the program (which sets the rail dot in `internal/ui/app.go:1788-1789`). It does NOT mutate `wctx.Channels[i].UnreadCount`. When the user later switches to that workspace, `WorkspaceSwitchedMsg` ships `wctx.Channels` (`cmd/slk/main.go:818`) to `App.Update`, which rebuilds the sidebar from stale data. Per-channel bold/dot are absent. Task 7.

### Bug 3: DM names not bolded on unread

Mechanism: the DM prefix `● ` is rendered through lipgloss with a presence color (`internal/ui/sidebar/model.go:858-870`), so it emits `\x1b[38;...m● \x1b[m` — a mid-label reset. The outer `styles.ChannelUnread.Render(label)` wraps the whole label with `\x1b[1;38;...m` at the start and `\x1b[m` at the end. After the prefix's mid-label reset, the bold attribute is gone for everything to the right (the channel name, trailing space, and unread dot).

`messages.ReapplyBgAfterResets` (`internal/ui/messages/render.go:248-255`) re-injects `bgAnsi+fgAnsi` after each reset, restoring colors — but not `\x1b[1m`. Public `#` channels look right because they have no inline-styled prefix → no mid-label reset → bold survives until the outer style's closing reset. Task 8.

---

## File Structure

**Modified files:**
- `internal/slack/events.go` — add 4 event types + dispatch cases, add `OnConversationOpened` to `EventHandler` interface (~50 LOC)
- `internal/slack/events_test.go` — dispatch tests for each new event (~80 LOC)
- `internal/ui/messages.go` (or wherever bubbletea messages live) — add `ConversationOpenedMsg` (~15 LOC)
- `internal/ui/sidebar/model.go` — add `UpsertItem(ChannelItem)` method (~25 LOC)
- `internal/ui/sidebar/model_test.go` — tests for upsert + interaction with MarkUnread (~70 LOC)
- `internal/ui/app.go` — handle `ConversationOpenedMsg` in `Update` (~30 LOC)
- `internal/ui/app_test.go` — end-to-end test: receive event → message arrives → sidebar shows unread (~50 LOC)
- `cmd/slk/channelitem.go` (new) — extract `buildChannelItem` helper from main.go's existing inline logic (~70 LOC)
- `cmd/slk/main.go` — refactor existing channel construction loop to call `buildChannelItem`; implement `OnConversationOpened` on `rtmEventHandler` (~40 LOC delta)

Each file has one clear responsibility; the new helper lives next to its primary caller.

---

## Pre-flight check

- [ ] **Step 0: Confirm baseline tests pass on current `main`**

Run: `go test ./...`
Expected: PASS across all packages.

If anything fails before this work starts, stop and investigate — don't paper over an unrelated regression.

---

## Task 1: Extract `buildChannelItem` helper

**Files:**
- Create: `cmd/slk/channelitem.go`
- Modify: `cmd/slk/main.go:1058-1122`
- Test: `cmd/slk/channelitem_test.go` (new)

This extracts the existing channel→`ChannelItem` logic so both the initial workspace-bootstrap loop and the new event handler can share one implementation. Pure refactor, no behavior change.

- [ ] **Step 1: Write failing test for the extracted helper**

Create `cmd/slk/channelitem_test.go`:

```go
package main

import (
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/slack-go/slack"
)

func TestBuildChannelItem_DM(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{"U123": "alice"},
		UserNamesByHandle: map[string]string{"alice": "alice"},
	}
	cfg := &config.Config{}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID:     "D1",
				IsIM:   true,
				User:   "U123",
			},
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.ID != "D1" {
		t.Errorf("ID = %q, want D1", item.ID)
	}
	if item.Type != "dm" {
		t.Errorf("Type = %q, want dm", item.Type)
	}
	if item.Name != "alice" {
		t.Errorf("Name = %q, want alice", item.Name)
	}
	if item.DMUserID != "U123" {
		t.Errorf("DMUserID = %q, want U123", item.DMUserID)
	}
}

func TestBuildChannelItem_GroupDM(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{"alice": "Alice", "bob": "Bob"},
	}
	cfg := &config.Config{}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID:     "G1",
				IsMpIM: true,
			},
			Name: "mpdm-alice--bob-1",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Type != "group_dm" {
		t.Errorf("Type = %q, want group_dm", item.Type)
	}
	if item.Name == "mpdm-alice--bob-1" {
		t.Errorf("Name not formatted: %q", item.Name)
	}
}

func TestBuildChannelItem_Channel(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
	}
	cfg := &config.Config{}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1"},
			Name:         "general",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Type != "channel" {
		t.Errorf("Type = %q, want channel", item.Type)
	}
	if item.Name != "general" {
		t.Errorf("Name = %q, want general", item.Name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestBuildChannelItem -v`
Expected: FAIL with "undefined: buildChannelItem".

- [ ] **Step 3: Create `cmd/slk/channelitem.go` with the extracted helper**

```go
package main

import (
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	slackfmt "github.com/gammons/slk/internal/slack/format"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// buildChannelItem converts a Slack conversation into the sidebar
// ChannelItem + finder Item shape used everywhere in slk. Pure function:
// reads from wctx for name/presence resolution, returns the constructed
// sidebar item plus a parallel finder entry. The caller decides whether
// to append, upsert, or persist.
//
// Extracted from the workspace-bootstrap loop in initWorkspace so that
// mid-session conversation events (mpim_open / im_created / group_joined /
// channel_joined) can produce identical items.
func buildChannelItem(ch slack.Channel, wctx *WorkspaceContext, cfg *config.Config, teamID string) (sidebar.ChannelItem, channelfinder.Item) {
	chType := "channel"
	if ch.IsIM {
		if wctx.BotUserIDs[ch.User] {
			chType = "app"
		} else {
			chType = "dm"
		}
	} else if ch.IsMpIM {
		chType = "group_dm"
	} else if ch.IsPrivate {
		chType = "private"
	}

	displayName := ch.Name
	if ch.IsIM {
		if resolved, ok := wctx.UserNames[ch.User]; ok {
			displayName = resolved
		} else {
			displayName = ch.User
		}
	} else if ch.IsMpIM {
		displayName = slackfmt.FormatMPDMName(ch.Name, func(h string) string {
			return wctx.UserNamesByHandle[h]
		})
	}

	section := cfg.MatchSection(teamID, ch.Name)
	var sectionOrder int
	if section != "" {
		sectionOrder = cfg.SectionOrder(teamID, section)
	}

	item := sidebar.ChannelItem{
		ID:           ch.ID,
		Name:         displayName,
		Type:         chType,
		Section:      section,
		SectionOrder: sectionOrder,
	}
	if ch.IsIM {
		item.DMUserID = ch.User
	}

	finderItem := channelfinder.Item{
		ID:       ch.ID,
		Name:     ch.Name,
		Type:     chType,
		Presence: item.Presence,
		Joined:   true,
	}
	return item, finderItem
}

// upsertChannelInDB writes the channel to the SQLite cache. Separated from
// buildChannelItem so the latter stays a pure function.
func upsertChannelInDB(db *cache.Database, ch slack.Channel, chType string, teamID string) {
	db.UpsertChannel(cache.Channel{
		ID:          ch.ID,
		WorkspaceID: teamID,
		Name:        ch.Name,
		Type:        chType,
		Topic:       ch.Topic.Value,
		IsMember:    ch.IsMember,
	})
}
```

- [ ] **Step 4: Refactor main.go bootstrap loop to use the helper**

In `cmd/slk/main.go`, replace the body of the `for _, ch := range channels` loop (currently lines 1058-1122) with:

```go
	for _, ch := range channels {
		item, finderItem := buildChannelItem(ch, wctx, cfg, client.TeamID())
		upsertChannelInDB(db, ch, item.Type, client.TeamID())

		if ch.IsIM {
			if _, ok := wctx.UserNames[ch.User]; !ok {
				wctx.UnresolvedDMs = append(wctx.UnresolvedDMs, UnresolvedDM{
					ChannelID: ch.ID,
					UserID:    ch.User,
				})
			}
			if cachedUser, err := db.GetUser(ch.User); err == nil && cachedUser.Presence != "" {
				item.Presence = cachedUser.Presence
				finderItem.Presence = cachedUser.Presence
			}
		}
		wctx.Channels = append(wctx.Channels, item)
		wctx.FinderItems = append(wctx.FinderItems, finderItem)
	}
```

Then delete the now-redundant separate `for _, ch := range wctx.Channels { wctx.FinderItems = append(...) }` block at lines 1150-1158.

- [ ] **Step 5: Run tests + build**

Run: `go test ./cmd/slk/... ./internal/...`
Expected: PASS. The new `TestBuildChannelItem_*` tests pass; existing tests still pass because the refactor preserves behavior.

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/channelitem.go cmd/slk/channelitem_test.go cmd/slk/main.go
git commit -m "refactor: extract buildChannelItem helper from workspace bootstrap"
```

---

## Task 2: Wire new WS events through `events.go`

**Files:**
- Modify: `internal/slack/events.go`
- Test: `internal/slack/events_test.go`

Slack's browser-protocol WS emits four events for new conversations:
- `mpim_open` — user opened a group DM (own action or auto-open from a message)
- `im_created` / `im_open` — new direct message conversation
- `group_joined` — added to a private channel or mpim
- `channel_joined` — joined a public channel (rare during normal use, but include for completeness)

All four payloads carry a top-level `channel` field with the full conversation object.

- [ ] **Step 1: Write failing dispatch test**

Append to `internal/slack/events_test.go`:

```go
func TestDispatch_MPIMOpen(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"mpim_open","channel":{"id":"G1","is_mpim":true,"name":"mpdm-alice--bob-1"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "G1" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_IMCreated(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"im_created","channel":{"id":"D1","is_im":true,"user":"U1"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "D1" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_GroupJoined(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"group_joined","channel":{"id":"G2","is_group":true,"name":"private-room"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "G2" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}

func TestDispatch_ChannelJoined(t *testing.T) {
	h := &mockEventHandler{}
	payload := []byte(`{"type":"channel_joined","channel":{"id":"C1","is_channel":true,"name":"general"}}`)
	dispatchWebSocketEvent(payload, h)
	if h.lastConversationOpenedID != "C1" {
		t.Errorf("OnConversationOpened not called or wrong ID; got %q", h.lastConversationOpenedID)
	}
}
```

Then add the necessary fields and method stub to the mock:

```go
// in the mockEventHandler struct definition
lastConversationOpenedID string
lastConversationOpenedCh slack.Channel

// new method on mockEventHandler
func (m *mockEventHandler) OnConversationOpened(ch slack.Channel) {
	m.lastConversationOpenedID = ch.ID
	m.lastConversationOpenedCh = ch
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestDispatch_ -v`
Expected: FAIL — undefined `OnConversationOpened` and the `mockEventHandler` doesn't satisfy the interface yet.

- [ ] **Step 3: Add the handler method to the interface and implement dispatch**

Edit `internal/slack/events.go`:

1. Add the handler method to the `EventHandler` interface (after `OnThreadMarked`):

```go
	// OnConversationOpened is delivered when a new or previously-closed
	// conversation becomes visible to the user mid-session: mpim_open,
	// im_created, group_joined, or channel_joined. The full slack.Channel
	// payload is forwarded so the receiver can construct a sidebar item
	// without an extra conversations.info round-trip.
	OnConversationOpened(channel slack.Channel)
```

2. Add the event payload struct (near the other ws*Event types):

```go
// wsConversationOpenedEvent is the shared shape for mpim_open, im_created,
// group_joined, and channel_joined events. All four carry a top-level
// `channel` field with the full conversation object.
type wsConversationOpenedEvent struct {
	Type    string        `json:"type"`
	Channel slack.Channel `json:"channel"`
}
```

3. Add a case to `dispatchWebSocketEvent` (alongside the existing `channel_marked` case):

```go
	case "mpim_open", "im_created", "im_open", "group_joined", "channel_joined":
		var evt wsConversationOpenedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		if evt.Channel.ID != "" {
			handler.OnConversationOpened(evt.Channel)
		}
```

- [ ] **Step 4: Run dispatch tests**

Run: `go test ./internal/slack/ -run TestDispatch_ -v`
Expected: all four new dispatch tests PASS.

Run: `go test ./internal/slack/...`
Expected: full slack package PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go
git commit -m "feat(slack): dispatch mpim_open/im_created/group_joined/channel_joined events"
```

---

## Task 3: Sidebar `UpsertItem` method

**Files:**
- Modify: `internal/ui/sidebar/model.go`
- Test: `internal/ui/sidebar/model_test.go`

`SetItems` replaces the whole list, which is too coarse: it would clobber `UnreadCount` set elsewhere. We need an idempotent upsert keyed on `ID` that preserves existing item state and re-runs the filter so the new row becomes visible.

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/sidebar/model_test.go`:

```go
func TestUpsertItem_AddsNewChannel(t *testing.T) {
	m := New()
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})

	m.UpsertItem(ChannelItem{ID: "G1", Name: "alice, bob", Type: "group_dm"})

	items := m.AllItems()
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	found := false
	for _, it := range items {
		if it.ID == "G1" && it.Type == "group_dm" {
			found = true
		}
	}
	if !found {
		t.Errorf("G1 not present after upsert: %+v", items)
	}
}

func TestUpsertItem_UpdatesExistingChannel(t *testing.T) {
	m := New()
	m.SetItems([]ChannelItem{{ID: "G1", Name: "old name", Type: "group_dm", UnreadCount: 3}})

	m.UpsertItem(ChannelItem{ID: "G1", Name: "new name", Type: "group_dm"})

	items := m.AllItems()
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Name != "new name" {
		t.Errorf("Name = %q, want %q", items[0].Name, "new name")
	}
	// UnreadCount must be preserved on update — Slack's mpim_open does
	// not carry unread state, and clobbering a live count to 0 would
	// erase the indicator we're trying to fix.
	if items[0].UnreadCount != 3 {
		t.Errorf("UnreadCount = %d, want 3 (preserved)", items[0].UnreadCount)
	}
}

func TestUpsertItem_ThenMarkUnread(t *testing.T) {
	m := New()
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	// Simulate the bug scenario: a new mpdm shows up via mpim_open,
	// then a message arrives. MarkUnread must successfully bump the
	// count on the freshly-upserted item.
	m.UpsertItem(ChannelItem{ID: "G1", Name: "alice, bob", Type: "group_dm"})
	m.MarkUnread("G1")

	items := m.AllItems()
	for _, it := range items {
		if it.ID == "G1" && it.UnreadCount != 1 {
			t.Errorf("G1 UnreadCount = %d, want 1", it.UnreadCount)
		}
	}
}
```

If `AllItems()` doesn't exist on the model, use `VisibleItems()` plus pre-staleness inspection (or expose a test-only accessor). Check `model.go` first; there's already `VisibleItems()`. Adjust the test to use whichever accessor is available and meaningful.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/sidebar/ -run TestUpsertItem -v`
Expected: FAIL — undefined `UpsertItem` (and possibly `AllItems`).

- [ ] **Step 3: Implement `UpsertItem`**

Add to `internal/ui/sidebar/model.go` (near `SetItems`):

```go
// UpsertItem inserts a new ChannelItem keyed by ID, or updates the existing
// item's Name / Type / Section / SectionOrder / DMUserID / Presence in
// place if the ID is already present. Existing UnreadCount and LastReadTS
// are PRESERVED on update — Slack's mpim_open / im_created / group_joined
// payloads do not carry unread state, so clobbering would erase live
// indicators we want to keep visible.
//
// Re-runs the staleness filter so a freshly-added item (or one whose
// staleness state may have changed) is reflected in the visible list
// immediately.
func (m *Model) UpsertItem(item ChannelItem) {
	for i := range m.items {
		if m.items[i].ID == item.ID {
			// Preserve unread/last-read; overwrite descriptive fields.
			preservedUnread := m.items[i].UnreadCount
			preservedLastRead := m.items[i].LastReadTS
			m.items[i] = item
			m.items[i].UnreadCount = preservedUnread
			m.items[i].LastReadTS = preservedLastRead
			m.rebuildFilter()
			m.rebuildNavPreserveCursor()
			m.cacheValid = false
			m.dirty()
			return
		}
	}
	m.items = append(m.items, item)
	m.rebuildFilter()
	m.rebuildNavPreserveCursor()
	m.cacheValid = false
	m.dirty()
}

// AllItems returns the full unfiltered item slice. Test helper; callers
// that want only what's currently rendered should use VisibleItems.
func (m *Model) AllItems() []ChannelItem {
	out := make([]ChannelItem, len(m.items))
	copy(out, m.items)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/sidebar/ -run TestUpsertItem -v`
Expected: PASS.

Run: `go test ./internal/ui/sidebar/...`
Expected: full sidebar package PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/sidebar/model.go internal/ui/sidebar/model_test.go
git commit -m "feat(sidebar): UpsertItem for mid-session conversation additions"
```

---

## Task 4: Define `ConversationOpenedMsg` and handle it in `App.Update`

**Files:**
- Modify: `internal/ui/messages.go` (or wherever `tea.Msg` types live — search for `WorkspaceUnreadMsg` to find the right file)
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Locate the messages file**

Run: `rg -l "type WorkspaceUnreadMsg" internal/ui`
Expected: one file. Use that file in subsequent steps.

- [ ] **Step 2: Write failing end-to-end test**

Append to `internal/ui/app_test.go`:

```go
func TestConversationOpenedMsg_SidebarReceivesItemAndUnread(t *testing.T) {
	app := newTestApp(t) // use whatever test constructor exists; mirror nearby tests
	app.sidebar.SetItems([]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})

	// Simulate Slack pushing an mpim_open for a previously-unknown mpdm.
	app.Update(ConversationOpenedMsg{
		TeamID: app.activeTeamID(), // or whatever accessor the app exposes
		Item: sidebar.ChannelItem{
			ID:   "G1",
			Name: "alice, bob",
			Type: "group_dm",
		},
	})

	// Then a message arrives for that mpdm while the user is elsewhere.
	app.Update(NewMessageMsg{
		ChannelID: "G1",
		Message:   messages.MessageItem{TS: "1700000001.000000", UserID: "U2", Text: "hi"},
	})

	// The sidebar should now show G1 with UnreadCount > 0.
	found := false
	for _, it := range app.sidebar.AllItems() {
		if it.ID == "G1" {
			found = true
			if it.UnreadCount < 1 {
				t.Errorf("G1 UnreadCount = %d, want >= 1", it.UnreadCount)
			}
		}
	}
	if !found {
		t.Errorf("G1 not in sidebar after ConversationOpenedMsg")
	}
}
```

If a test constructor like `newTestApp` doesn't exist, mirror the setup pattern from the nearest existing `Test*` in `app_test.go`. Don't invent new test infrastructure.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestConversationOpenedMsg -v`
Expected: FAIL — undefined `ConversationOpenedMsg`.

- [ ] **Step 4: Define the message**

Add to the messages file located in step 1:

```go
// ConversationOpenedMsg is sent when Slack delivers an mpim_open,
// im_created, group_joined, or channel_joined event. The TeamID
// disambiguates events for inactive workspaces; only events whose
// TeamID matches the currently-active workspace mutate the live
// sidebar — others are persisted in the workspace's WorkspaceContext
// for when the user switches in.
type ConversationOpenedMsg struct {
	TeamID string
	Item   sidebar.ChannelItem
}
```

(Add the `"github.com/gammons/slk/internal/ui/sidebar"` import if it's not already in the messages file. If a circular import results, define the message with the raw fields instead and reconstruct the `sidebar.ChannelItem` in `app.go` — but try the cleaner version first.)

- [ ] **Step 5: Handle the message in `App.Update`**

In `internal/ui/app.go`, add a case alongside the other workspace-scoped messages (e.g. near `WorkspaceUnreadMsg`):

```go
	case ConversationOpenedMsg:
		if msg.TeamID == a.activeTeamID() {
			a.sidebar.UpsertItem(msg.Item)
		}
		// Even for inactive workspaces, we want the next workspace
		// switch to show the conversation — but persistence in
		// WorkspaceContext.Channels is owned by the rtmEventHandler
		// in cmd/slk/main.go, not here. App.Update only mutates the
		// active sidebar.
```

Use whatever accessor the app already has for the active team ID; search nearby for `activeTeamID` / `currentWorkspaceID` to match style.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run TestConversationOpenedMsg -v`
Expected: PASS.

Run: `go test ./internal/ui/...`
Expected: full package PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/<messages-file>.go internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(ui): handle ConversationOpenedMsg to upsert sidebar item"
```

---

## Task 5: Implement `OnConversationOpened` on `rtmEventHandler`

**Files:**
- Modify: `cmd/slk/main.go`

This is the glue that links the WS dispatch (Task 2) to the UI message (Task 4). The handler builds a `ChannelItem` via the helper from Task 1, persists it in the workspace context (so a later workspace switch sees it), upserts it into the SQLite cache, and forwards a `ConversationOpenedMsg` to the program for the active-workspace UI update.

- [ ] **Step 1: Implement the method**

Add near the other `rtmEventHandler` methods (e.g. after `OnDNDChange`):

```go
func (h *rtmEventHandler) OnConversationOpened(ch slack.Channel) {
	if h.wsCtx == nil {
		return
	}

	item, finderItem := buildChannelItem(ch, h.wsCtx, h.cfg, h.workspaceID)
	upsertChannelInDB(h.db, ch, item.Type, h.workspaceID)

	// Persist in the workspace context so a workspace switch later
	// shows the new conversation. De-dupe on ID — the same event can
	// arrive twice (e.g. im_open followed by im_created on first DM).
	replaced := false
	for i := range h.wsCtx.Channels {
		if h.wsCtx.Channels[i].ID == item.ID {
			// Preserve unread/last-read from the live context.
			item.UnreadCount = h.wsCtx.Channels[i].UnreadCount
			item.LastReadTS = h.wsCtx.Channels[i].LastReadTS
			h.wsCtx.Channels[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		h.wsCtx.Channels = append(h.wsCtx.Channels, item)
		h.wsCtx.FinderItems = append(h.wsCtx.FinderItems, finderItem)
	}

	// Mirror channelTypes / channelNames maps used by the notifier so
	// follow-up messages on this channel get notified correctly.
	if h.channelNames != nil {
		h.channelNames[ch.ID] = item.Name
	}
	if h.channelTypes != nil {
		h.channelTypes[ch.ID] = item.Type
	}

	if h.program == nil {
		return
	}
	h.program.Send(ui.ConversationOpenedMsg{
		TeamID: h.workspaceID,
		Item:   item,
	})
}
```

If `rtmEventHandler` doesn't already have a `cfg *config.Config` field, add it, and populate it where the struct is constructed (search for `&rtmEventHandler{`). Same for any other field referenced above that doesn't exist yet — verify each by grep before assuming it's there.

- [ ] **Step 2: Build to confirm interface satisfaction**

Run: `go build ./...`
Expected: clean build. If you get "rtmEventHandler does not implement EventHandler" — that means the interface change in Task 2 isn't wired here yet; the new method satisfies it.

- [ ] **Step 3: Add a focused test for the handler**

Append to `cmd/slk/` tests (find `rtmEventHandler` test file; if none exists, create `event_handler_test.go`):

```go
func TestOnConversationOpened_AppendsAndSends(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		Channels:          []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		FinderItems:       []channelfinder.Item{{ID: "C1", Name: "general", Type: "channel", Joined: true}},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		cfg:          &config.Config{},
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
		// db, program left nil — handler must guard.
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "G1", IsMpIM: true},
			Name:         "mpdm-alice--bob-1",
		},
	}
	h.OnConversationOpened(ch)

	if len(wctx.Channels) != 2 {
		t.Errorf("len(Channels) = %d, want 2", len(wctx.Channels))
	}
	if h.channelTypes["G1"] != "group_dm" {
		t.Errorf("channelTypes[G1] = %q, want group_dm", h.channelTypes["G1"])
	}
}
```

The handler must guard against `nil` `db` and `program` for this test to pass. Add nil checks in the implementation if missing.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/slk/...`
Expected: PASS.

Run: `go test ./...`
Expected: full repo PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/event_handler_test.go
git commit -m "feat: forward conversation-opened events to sidebar via tea program"
```

---

## Task 6: Inactive-workspace conversation-opened persistence

**Files:**
- Modify: `cmd/slk/main.go` (the `OnConversationOpened` method added in Task 5)
- Test: `cmd/slk/event_handler_test.go` (or wherever Task 5's test landed)

Tasks 1-5 wired the active-workspace path. Now make sure inactive-workspace `OnConversationOpened` events still update `wctx.Channels` so a workspace switch later picks the new conversation up. The implementation in Task 5 already mutates `wctx.Channels` regardless of `isActive`, but only sends the UI message when `program != nil`. Confirm with a test, and add an `isActive` guard around the program send to mirror the rest of `rtmEventHandler` (which gates UI messages on `isActive`).

- [ ] **Step 1: Write failing test for inactive-workspace conversation open**

Append to the test file:

```go
func TestOnConversationOpened_InactiveWorkspace_PersistsButDoesNotSend(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
	}
	sent := false
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		cfg:          &config.Config{},
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
		isActive:     func() bool { return false },
		// Replace program with a fake that flips `sent`. If the
		// existing handler uses *tea.Program directly, factor out a
		// minimal interface (Send(tea.Msg)) and inject it via the
		// struct so this test can observe the call without spinning
		// up a real program.
	}
	_ = sent // wire to fake
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "G1", IsMpIM: true},
			Name:         "mpdm-alice--bob-1",
		},
	}
	h.OnConversationOpened(ch)

	if len(wctx.Channels) != 1 || wctx.Channels[0].ID != "G1" {
		t.Errorf("inactive workspace context not updated: %+v", wctx.Channels)
	}
	if sent {
		t.Errorf("inactive workspace should not send ConversationOpenedMsg to program")
	}
}
```

If injecting a fake sender requires a refactor wider than this task, instead test the `isActive` gating by checking that `h.OnConversationOpened` does not panic when `program == nil` and the persisted state is correct. The persistence half of the assertion is the load-bearing one.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestOnConversationOpened_Inactive -v`
Expected: FAIL — either the test compiles and finds `sent=true` (handler sends regardless of active state), or it points to whatever scaffolding it needs.

- [ ] **Step 3: Add `isActive` guard around the program send in `OnConversationOpened`**

Edit the method added in Task 5: wrap the `h.program.Send(...)` call in a check:

```go
	if h.program == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		// Persistence above already updated wctx.Channels; defer the
		// UI message until the user switches into this workspace.
		return
	}
	h.program.Send(ui.ConversationOpenedMsg{
		TeamID: h.workspaceID,
		Item:   item,
	})
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/slk/ -run TestOnConversationOpened -v`
Expected: PASS for both active and inactive scenarios.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/event_handler_test.go
git commit -m "fix: persist conversation-opened state for inactive workspaces"
```

---

## Task 7: Persist per-channel unread for inactive-workspace messages

**Files:**
- Modify: `cmd/slk/main.go:1720-1727` (the inactive-workspace branch of `rtmEventHandler.OnMessage`)
- Test: `cmd/slk/event_handler_test.go`

This is the fix for the user-reported bug: "Workspace B focused, DM arrives in A → notification fires but switching to A shows no per-channel unread."

The current branch only ships a `WorkspaceUnreadMsg` (rail dot). It must also bump `wctx.Channels[i].UnreadCount` so `WorkspaceSwitchedMsg.Channels` carries fresh data when the user switches in.

Use the same thread-reply guard the active branch uses (only top-level messages and `thread_broadcast` count). For unknown channel IDs (no match in `wctx.Channels`), fall through to the existing send — the inactive `OnConversationOpened` path (Task 6) handles the new-mpdm case, but if a `message` arrives without a preceding conversation-opened event for an unknown channel, we don't want to crash or silently drop the rail dot.

- [ ] **Step 1: Write failing test**

Append to `cmd/slk/event_handler_test.go`:

```go
func TestOnMessage_InactiveWorkspace_BumpsChannelUnreadCount(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:  map[string]bool{},
		UserNames:   map[string]string{},
		Channels: []sidebar.ChannelItem{
			{ID: "D1", Name: "alice", Type: "dm", UnreadCount: 0},
			{ID: "C1", Name: "general", Type: "channel"},
		},
	}
	h := &rtmEventHandler{
		wsCtx:         wctx,
		workspaceID:   "T1",
		channelNames:  map[string]string{"D1": "alice", "C1": "general"},
		channelTypes:  map[string]string{"D1": "dm", "C1": "channel"},
		isActive:      func() bool { return false },
		// db, program, notifier may be nil — the inactive branch
		// must guard.
	}
	h.OnMessage("D1", "U2", "1700000001.000000", "hi", "", "", false, nil, slack.Blocks{}, nil)

	for _, ch := range wctx.Channels {
		if ch.ID == "D1" && ch.UnreadCount != 1 {
			t.Errorf("D1 UnreadCount = %d, want 1", ch.UnreadCount)
		}
	}
}

func TestOnMessage_InactiveWorkspace_ThreadReplyDoesNotBumpChannel(t *testing.T) {
	wctx := &WorkspaceContext{
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		channelNames: map[string]string{"C1": "general"},
		channelTypes: map[string]string{"C1": "channel"},
		isActive:     func() bool { return false },
	}
	// thread_ts != ts and subtype != "thread_broadcast" → reply, must not bump.
	h.OnMessage("C1", "U2", "1700000002.000000", "reply", "1700000001.000000", "", false, nil, slack.Blocks{}, nil)

	if wctx.Channels[0].UnreadCount != 0 {
		t.Errorf("thread reply bumped channel unread; got %d", wctx.Channels[0].UnreadCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestOnMessage_InactiveWorkspace -v`
Expected: FAIL — `D1.UnreadCount` is 0, expected 1.

- [ ] **Step 3: Implement the bump in the inactive-workspace branch**

In `cmd/slk/main.go`, replace the inactive-workspace branch in `OnMessage` (currently lines ~1720-1727):

```go
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — persist per-channel unread so a
		// later workspace switch reflects the activity, then notify
		// the rail.
		//
		// Skip thread replies that aren't broadcasts: per Slack's
		// channel-unread semantics they don't mark the parent
		// channel as unread (only top-level messages and
		// thread_broadcast subtypes do). Mirrors the active-branch
		// guard at internal/ui/app.go:1420-1423.
		isThreadReply := threadTS != "" && threadTS != ts
		isBroadcast := subtype == "thread_broadcast"
		if !isThreadReply || isBroadcast {
			if h.wsCtx != nil {
				for i := range h.wsCtx.Channels {
					if h.wsCtx.Channels[i].ID == channelID {
						h.wsCtx.Channels[i].UnreadCount++
						break
					}
				}
			}
		}
		if h.program != nil {
			h.program.Send(ui.WorkspaceUnreadMsg{
				TeamID:    h.workspaceID,
				ChannelID: channelID,
			})
		}
		return
	}
```

Note: this leaves the unknown-channel case (channel not in `wctx.Channels`) handled gracefully — the rail dot still ships. The Task 5/6 path adds the missing item via `OnConversationOpened` so subsequent messages will bump correctly.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/slk/ -run TestOnMessage_InactiveWorkspace -v`
Expected: PASS for both bump and thread-reply-skip cases.

Run: `go test ./cmd/slk/...`
Expected: full package PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/event_handler_test.go
git commit -m "fix: persist per-channel unread for inactive-workspace messages"
```

---

## Task 8: Restore bold attribute on unread DM/private/app/group_dm rows

**Files:**
- Modify: `internal/ui/sidebar/model.go` (the per-row render block around lines 985-998)
- Test: `internal/ui/sidebar/model_test.go`

The styled DM/private/app/group_dm prefix glyph emits a mid-label `\x1b[m` reset. `ReapplyBgAfterResets` (`internal/ui/messages/render.go:248`) re-injects bg+fg after each reset but does NOT re-emit `\x1b[1m`, so the outer `ChannelUnread.Bold(true)` is dropped for everything to the right of the prefix.

The fix: when rendering an unread row, append `\x1b[1m` to the re-applied attribute string. We do this at the call site rather than changing `ReapplyBgAfterResets`'s shared signature — the helper is intentionally generic; the bold attribute is a sidebar-specific concern only relevant for unread rows.

- [ ] **Step 1: Write failing test**

Append to `internal/ui/sidebar/model_test.go`:

```go
func TestRender_UnreadDMRow_KeepsBoldAfterPrefixReset(t *testing.T) {
	m := New()
	m.SetSize(40, 20)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active", UnreadCount: 1},
	})

	out := m.View()

	// The DM prefix emits an inline ANSI reset (\x1b[m) before the
	// channel name. After that reset, the bold attribute (\x1b[1m)
	// must be re-emitted so "alice" still renders bold. Search for
	// the substring "\x1b[m" followed (eventually, after color
	// re-injection) by "\x1b[1m" before "alice".
	if !strings.Contains(out, "alice") {
		t.Fatalf("output missing channel name; got: %q", out)
	}
	// Find the position of "alice" and the last \x1b[m before it.
	aliceIdx := strings.Index(out, "alice")
	prefix := out[:aliceIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		// No reset before the name — outer style alone provides bold.
		// In that case the test premise (mid-label reset wipes bold)
		// no longer applies; this test is a guard against regression
		// in the prefix rendering. Skip rather than fail.
		t.Skip("no mid-label reset found before name; render path changed")
	}
	afterReset := prefix[lastResetIdx:]
	if !strings.Contains(afterReset, "\x1b[1m") {
		t.Errorf("bold attribute not re-emitted after prefix reset for unread DM row\nafterReset=%q", afterReset)
	}
}

func TestRender_ReadDMRow_DoesNotEmitBoldAfterReset(t *testing.T) {
	m := New()
	m.SetSize(40, 20)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active", UnreadCount: 0},
	})

	out := m.View()
	aliceIdx := strings.Index(out, "alice")
	if aliceIdx < 0 {
		t.Fatalf("output missing channel name")
	}
	prefix := out[:aliceIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		return // nothing to check
	}
	afterReset := prefix[lastResetIdx:]
	if strings.Contains(afterReset, "\x1b[1m") {
		t.Errorf("read DM row unexpectedly emitted bold after reset; afterReset=%q", afterReset)
	}
}
```

- [ ] **Step 2: Run test to verify the unread one fails**

Run: `go test ./internal/ui/sidebar/ -run TestRender_.*DMRow -v`
Expected: `TestRender_UnreadDMRow_KeepsBoldAfterPrefixReset` FAILS (no `\x1b[1m` after the reset). `TestRender_ReadDMRow_DoesNotEmitBoldAfterReset` PASSES.

- [ ] **Step 3: Implement: pass a per-row attribute string to ReapplyBgAfterResets**

Edit `internal/ui/sidebar/model.go` around lines 985-998 (the per-channel-row block). Replace:

```go
		labelNormal = messages.ReapplyBgAfterResets(labelNormal, bgAnsi)
		labelSelected = messages.ReapplyBgAfterResets(labelSelected, bgAnsi)
		labelActive = messages.ReapplyBgAfterResets(labelActive, bgAnsi)

		// Pick base style for non-selected state.
		var baseStyle lipgloss.Style
		if item.UnreadCount > 0 {
			baseStyle = styles.ChannelUnread
		} else {
			baseStyle = styles.ChannelNormal
		}
```

with:

```go
		// Unread rows must re-emit the bold attribute after every
		// inline-prefix ANSI reset, otherwise lipgloss's outer
		// ChannelUnread bold is wiped for the channel name + dot
		// span that follows the styled prefix glyph.
		rowAttrs := bgAnsi
		if item.UnreadCount > 0 {
			rowAttrs += "\x1b[1m"
		}
		labelNormal = messages.ReapplyBgAfterResets(labelNormal, rowAttrs)
		labelSelected = messages.ReapplyBgAfterResets(labelSelected, rowAttrs)
		labelActive = messages.ReapplyBgAfterResets(labelActive, rowAttrs)

		// Pick base style for non-selected state.
		var baseStyle lipgloss.Style
		if item.UnreadCount > 0 {
			baseStyle = styles.ChannelUnread
		} else {
			baseStyle = styles.ChannelNormal
		}
```

Apply the same change to the synthetic Threads-row block (the three `messages.ReapplyBgAfterResets(threads*, bgAnsi)` calls, around lines 887-889) — when `m.threadsUnread > 0`, pass `bgAnsi + "\x1b[1m"` so the Threads row also keeps bold past its `⚑` glyph's reset:

```go
	threadsAttrs := bgAnsi
	if m.threadsUnread > 0 {
		threadsAttrs += "\x1b[1m"
	}
	threadsLabel = messages.ReapplyBgAfterResets(threadsLabel, threadsAttrs)
	threadsCursor = messages.ReapplyBgAfterResets(threadsCursor, threadsAttrs)
	threadsActiveLabel = messages.ReapplyBgAfterResets(threadsActiveLabel, threadsAttrs)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/sidebar/ -run TestRender_.*DMRow -v`
Expected: both PASS.

Run: `go test ./internal/ui/sidebar/...`
Expected: full sidebar package PASS. If existing render-output tests now find unexpected `\x1b[1m` sequences, they need their fixture strings updated — that's a legitimate snapshot change.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/sidebar/model.go internal/ui/sidebar/model_test.go
git commit -m "fix(sidebar): keep bold on unread rows past inline prefix reset"
```

---

## Task 9: End-to-end smoke test + manual verification

- [ ] **Step 1: Run the full test suite one more time**

Run: `go test ./...`
Expected: PASS across all packages.

Run: `go vet ./...`
Expected: no warnings.

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 2: Manual verification (real Slack)**

1. Build: `make build`
2. Start slk against two workspaces (A + B) with another teammate available.

**Bug 1 — new mpdm appears with unread:**
3. With slk focused on A, ask the teammate to open a fresh group DM with you and one other person, then send a message.
4. Verify slk's sidebar:
   - The new mpdm row appears in the Direct Messages section.
   - The row name is **bold + bright text** with a `•1` unread dot.
   - Click into the mpdm to confirm `MarkChannel` clears the indicator.

**Bug 2 — cross-workspace unread persists:**
5. Switch to workspace B. Have the teammate send a DM in workspace A.
6. Confirm: desktop notification fires, workspace rail shows A's unread dot.
7. Switch to workspace A.
8. Verify: the DM channel for that teammate shows **bold name + blue dot** in the sidebar (not just the rail dot).

**Bug 3 — DM/private bold name on unread:**
9. From workspace A's view, find a 1:1 DM with unread messages.
10. Verify the channel name renders **bold and bright**, not just with a trailing blue dot.
11. Same check for an unread mpdm (group DM) and an unread private channel (`◆` prefix).

**Issue 1 + 3 (already in working tree) regression check:**
12. In compose, type `:rocket` — verify the emoji picker opens. Press `Up`/`Down` arrows; selection moves. `Ctrl+P`/`Ctrl+N` still work; `Enter`/`Tab` still selects.
13. Verify Threads row at the top of the sidebar is bold + bright when there are unread threads, matching unread channels.

- [ ] **Step 3: Final commit (if any cleanup or doc tweaks needed)**

```bash
git status
# if anything outstanding:
git add -A
git commit -m "chore: cleanup after sidebar unread fixes"
```

---

## Self-Review Checklist

- [ ] All four target events (`mpim_open`, `im_created`, `group_joined`, `channel_joined`) are dispatched and tested.
- [ ] `buildChannelItem` is the single source of truth for `slack.Channel → sidebar.ChannelItem` mapping; the bootstrap loop and the WS handler both call it.
- [ ] `UpsertItem` preserves `UnreadCount` and `LastReadTS` on update — verified by `TestUpsertItem_UpdatesExistingChannel`.
- [ ] `OnConversationOpened` updates `wctx.Channels` even for inactive workspaces; only the program-send is gated on `isActive`.
- [ ] Inactive-workspace `OnMessage` bumps `wctx.Channels[i].UnreadCount` for the matching channel ID, with the same thread-reply guard as the active-workspace path.
- [ ] Unread sidebar rows re-emit `\x1b[1m` after every inline-prefix ANSI reset; read rows do NOT (so muted text stays muted).
- [ ] No new placeholders, TODOs, or "implement later" markers in plan text.
- [ ] All file paths are exact; line ranges are checked against current `main`.

## Out of Scope

Deliberately not addressed in this plan:
- Lazy `conversations.info` fetch when `MarkUnread` hits an unknown channel ID. If the four WS events above turn out to miss edge cases in production, add this as a follow-up belt-and-suspenders fix.
- Adding an unread/read prefix glyph variant for `group_dm` (cosmetic; the primary bold-name + unread-dot indicators will work after this plan).
- Refactoring `ReapplyBgAfterResets` to take structured attribute parameters. The current call-site approach is sufficient and keeps the helper generic.
