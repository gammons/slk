# Reaction Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add full emoji reaction support — search-first picker overlay, quick-toggle on existing reactions, pill-style display, and real-time sync.

**Architecture:** New `reactionpicker` package follows the channel finder overlay pattern. Reaction-nav is a sub-state on `messages.Model` and `thread.Model`. Cache CRUD for reactions and frecent tracking. RTM event handlers wired to update messages in place with optimistic UI.

**Tech Stack:** Go, bubbletea, lipgloss, kyokomi/emoji/v2, SQLite (modernc.org/sqlite)

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `internal/cache/reactions.go` | CRUD for reactions table |
| `internal/cache/reactions_test.go` | Tests for reactions CRUD |
| `internal/cache/frecent.go` | Frecent emoji tracking |
| `internal/cache/frecent_test.go` | Tests for frecent tracking |
| `internal/ui/reactionpicker/model.go` | Reaction picker overlay component |
| `internal/ui/reactionpicker/model_test.go` | Tests for picker logic |

### Modified Files
| File | Changes |
|------|---------|
| `internal/cache/db.go` | Add `frecent_emoji` table to schema |
| `internal/ui/messages/model.go` | Enhanced `ReactionItem`, pill rendering, reaction-nav state |
| `internal/ui/messages/model_test.go` | Tests for reaction-nav and pill rendering |
| `internal/ui/thread/model.go` | Reaction rendering + reaction-nav state |
| `internal/ui/mode.go` | Add `ModeReactionPicker` |
| `internal/ui/keys.go` | Add `ReactionNav` key binding |
| `internal/ui/app.go` | Picker integration, mode handler, reaction message types, callbacks |
| `internal/ui/styles/styles.go` | Reaction pill styles |
| `cmd/slack-tui/main.go` | Populate reactions from API, wire RTM handlers, wire callbacks |

---

### Task 1: Cache CRUD for Reactions

**Files:**
- Create: `internal/cache/reactions.go`
- Create: `internal/cache/reactions_test.go`

- [ ] **Step 1: Write the failing tests for reactions CRUD**

Create `internal/cache/reactions_test.go`:

```go
package cache

import (
	"testing"
)

func TestUpsertAndGetReactions(t *testing.T) {
	db := setupTestDB(t)

	// Insert a reaction
	err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001", "U002"}, 2)
	if err != nil {
		t.Fatalf("UpsertReaction: %v", err)
	}

	// Get reactions
	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(rows))
	}
	if rows[0].Emoji != "thumbsup" {
		t.Errorf("expected emoji thumbsup, got %s", rows[0].Emoji)
	}
	if rows[0].Count != 2 {
		t.Errorf("expected count 2, got %d", rows[0].Count)
	}
	if len(rows[0].UserIDs) != 2 || rows[0].UserIDs[0] != "U001" {
		t.Errorf("unexpected userIDs: %v", rows[0].UserIDs)
	}
}

func TestUpsertReactionUpdatesExisting(t *testing.T) {
	db := setupTestDB(t)

	err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001"}, 1)
	if err != nil {
		t.Fatalf("UpsertReaction: %v", err)
	}

	// Upsert with updated data
	err = db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001", "U002"}, 2)
	if err != nil {
		t.Fatalf("UpsertReaction update: %v", err)
	}

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(rows))
	}
	if rows[0].Count != 2 {
		t.Errorf("expected count 2, got %d", rows[0].Count)
	}
}

func TestDeleteReaction(t *testing.T) {
	db := setupTestDB(t)

	err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001"}, 1)
	if err != nil {
		t.Fatalf("UpsertReaction: %v", err)
	}

	err = db.DeleteReaction("1234.5678", "C123", "thumbsup")
	if err != nil {
		t.Fatalf("DeleteReaction: %v", err)
	}

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 reactions, got %d", len(rows))
	}
}

func TestGetReactionsEmpty(t *testing.T) {
	db := setupTestDB(t)

	rows, err := db.GetReactions("nonexistent", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 reactions, got %d", len(rows))
	}
}

func TestMultipleReactionsOnMessage(t *testing.T) {
	db := setupTestDB(t)

	_ = db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001"}, 1)
	_ = db.UpsertReaction("1234.5678", "C123", "rocket", []string{"U002", "U003"}, 2)
	_ = db.UpsertReaction("1234.5678", "C123", "heart", []string{"U001"}, 1)

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 reactions, got %d", len(rows))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cache/ -run TestUpsertAndGetReactions -v`
Expected: FAIL — `UpsertReaction` not defined

- [ ] **Step 3: Implement reactions CRUD**

Create `internal/cache/reactions.go`:

```go
package cache

import (
	"encoding/json"
	"fmt"
)

// ReactionRow represents a reaction stored in the cache.
type ReactionRow struct {
	Emoji   string
	UserIDs []string
	Count   int
}

// UpsertReaction inserts or updates a reaction for a message.
func (db *DB) UpsertReaction(messageTS, channelID, emoji string, userIDs []string, count int) error {
	userIDsJSON, err := json.Marshal(userIDs)
	if err != nil {
		return fmt.Errorf("marshal user_ids: %w", err)
	}

	_, err = db.conn.Exec(`
		INSERT INTO reactions (message_ts, channel_id, emoji, user_ids, count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(message_ts, channel_id, emoji)
		DO UPDATE SET user_ids = excluded.user_ids, count = excluded.count`,
		messageTS, channelID, emoji, string(userIDsJSON), count,
	)
	return err
}

// GetReactions returns all reactions for a message.
func (db *DB) GetReactions(messageTS, channelID string) ([]ReactionRow, error) {
	rows, err := db.conn.Query(`
		SELECT emoji, user_ids, count
		FROM reactions
		WHERE message_ts = ? AND channel_id = ?`,
		messageTS, channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ReactionRow
	for rows.Next() {
		var r ReactionRow
		var userIDsJSON string
		if err := rows.Scan(&r.Emoji, &userIDsJSON, &r.Count); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(userIDsJSON), &r.UserIDs); err != nil {
			return nil, fmt.Errorf("unmarshal user_ids: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// DeleteReaction removes a reaction from the cache.
func (db *DB) DeleteReaction(messageTS, channelID, emoji string) error {
	_, err := db.conn.Exec(`
		DELETE FROM reactions
		WHERE message_ts = ? AND channel_id = ? AND emoji = ?`,
		messageTS, channelID, emoji,
	)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/ -run "TestUpsert|TestDelete|TestGetReactions|TestMultiple" -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cache/reactions.go internal/cache/reactions_test.go
git commit -m "feat: add reactions cache CRUD"
```

---

### Task 2: Frecent Emoji Tracking

**Files:**
- Modify: `internal/cache/db.go` (add frecent_emoji table)
- Create: `internal/cache/frecent.go`
- Create: `internal/cache/frecent_test.go`

- [ ] **Step 1: Write the failing tests for frecent tracking**

Create `internal/cache/frecent_test.go`:

```go
package cache

import (
	"testing"
)

func TestRecordAndGetFrecentEmoji(t *testing.T) {
	db := setupTestDB(t)

	// Record some emoji usage
	if err := db.RecordEmojiUse("thumbsup"); err != nil {
		t.Fatalf("RecordEmojiUse: %v", err)
	}
	if err := db.RecordEmojiUse("thumbsup"); err != nil {
		t.Fatalf("RecordEmojiUse: %v", err)
	}
	if err := db.RecordEmojiUse("rocket"); err != nil {
		t.Fatalf("RecordEmojiUse: %v", err)
	}

	// thumbsup used twice, should rank higher
	results, err := db.GetFrecentEmoji(10)
	if err != nil {
		t.Fatalf("GetFrecentEmoji: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 frecent emoji, got %d", len(results))
	}
	if results[0] != "thumbsup" {
		t.Errorf("expected thumbsup first, got %s", results[0])
	}
	if results[1] != "rocket" {
		t.Errorf("expected rocket second, got %s", results[1])
	}
}

func TestGetFrecentEmojiLimit(t *testing.T) {
	db := setupTestDB(t)

	emojis := []string{"a", "b", "c", "d", "e"}
	for _, e := range emojis {
		_ = db.RecordEmojiUse(e)
	}

	results, err := db.GetFrecentEmoji(3)
	if err != nil {
		t.Fatalf("GetFrecentEmoji: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestGetFrecentEmojiEmpty(t *testing.T) {
	db := setupTestDB(t)

	results, err := db.GetFrecentEmoji(10)
	if err != nil {
		t.Fatalf("GetFrecentEmoji: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cache/ -run TestRecordAndGetFrecent -v`
Expected: FAIL — `frecent_emoji` table doesn't exist

- [ ] **Step 3: Add frecent_emoji table to schema**

Modify `internal/cache/db.go` — add the table creation after the existing `reactions` table block (after the `CREATE TABLE IF NOT EXISTS reactions` statement, before the index creation):

```go
	CREATE TABLE IF NOT EXISTS frecent_emoji (
		emoji TEXT PRIMARY KEY,
		use_count INTEGER NOT NULL DEFAULT 0,
		last_used INTEGER NOT NULL DEFAULT 0
	);
```

- [ ] **Step 4: Implement frecent tracking**

Create `internal/cache/frecent.go`:

```go
package cache

import (
	"time"
)

// RecordEmojiUse records usage of an emoji for frecent tracking.
func (db *DB) RecordEmojiUse(emoji string) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(`
		INSERT INTO frecent_emoji (emoji, use_count, last_used)
		VALUES (?, 1, ?)
		ON CONFLICT(emoji)
		DO UPDATE SET use_count = use_count + 1, last_used = excluded.last_used`,
		emoji, now,
	)
	return err
}

// GetFrecentEmoji returns the top N frequently/recently used emoji.
// Scored by: use_count / (1 + days_since_last_use).
func (db *DB) GetFrecentEmoji(limit int) ([]string, error) {
	now := time.Now().Unix()
	rows, err := db.conn.Query(`
		SELECT emoji
		FROM frecent_emoji
		ORDER BY CAST(use_count AS REAL) / (1.0 + (? - last_used) / 86400.0) DESC
		LIMIT ?`,
		now, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var emoji string
		if err := rows.Scan(&emoji); err != nil {
			return nil, err
		}
		result = append(result, emoji)
	}
	return result, rows.Err()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cache/ -run "TestRecordAndGetFrecent|TestGetFrecentEmoji" -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cache/db.go internal/cache/frecent.go internal/cache/frecent_test.go
git commit -m "feat: add frecent emoji tracking"
```

---

### Task 3: Reaction Pill Styles and Enhanced ReactionItem

**Files:**
- Modify: `internal/ui/styles/styles.go`
- Modify: `internal/ui/messages/model.go`

- [ ] **Step 1: Add reaction pill styles**

Add to `internal/ui/styles/styles.go` after the existing style definitions (e.g., after the compose styles):

```go
	// Reaction pill styles
	ReactionPillOwn = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a2e1a")).
			Foreground(lipgloss.Color("#50C878")).
			Padding(0, 1)

	ReactionPillOther = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(lipgloss.Color("#888888")).
			Padding(0, 1)

	ReactionPillSelected = lipgloss.NewStyle().
			Background(lipgloss.Color("#252540")).
			Foreground(lipgloss.Color("#4A9EFF")).
			Padding(0, 1)

	ReactionPillPlus = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(lipgloss.Color("#4A9EFF")).
			Padding(0, 1)
```

- [ ] **Step 2: Enhance ReactionItem struct**

In `internal/ui/messages/model.go`, update the `ReactionItem` struct (currently lines 36-39):

Replace:
```go
type ReactionItem struct {
	Emoji string
	Count int
}
```

With:
```go
type ReactionItem struct {
	Emoji      string // emoji name without colons, e.g. "thumbsup"
	Count      int
	HasReacted bool // whether the current user has reacted with this emoji
}
```

- [ ] **Step 3: Add reaction-nav state to messages Model**

In `internal/ui/messages/model.go`, add fields to the `Model` struct (after the existing fields):

```go
	reactionNavActive bool
	reactionNavIndex  int
```

Add the following methods after the existing navigation methods:

```go
// EnterReactionNav activates reaction navigation on the selected message.
func (m *Model) EnterReactionNav() {
	if msg := m.SelectedMessage(); msg != nil && len(msg.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.cache = nil // invalidate render cache
	}
}

// ExitReactionNav deactivates reaction navigation.
func (m *Model) ExitReactionNav() {
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.cache = nil // invalidate render cache
}

// ReactionNavActive returns whether reaction navigation is active.
func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

// ReactionNavLeft moves the reaction selection left with wrapping.
func (m *Model) ReactionNavLeft() {
	msg := m.SelectedMessage()
	if msg == nil {
		return
	}
	total := len(msg.Reactions) + 1 // +1 for [+] pill
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
	m.cache = nil
}

// ReactionNavRight moves the reaction selection right with wrapping.
func (m *Model) ReactionNavRight() {
	msg := m.SelectedMessage()
	if msg == nil {
		return
	}
	total := len(msg.Reactions) + 1 // +1 for [+] pill
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.cache = nil
}

// SelectedReaction returns the emoji at the current nav index.
// If isPlus is true, the [+] button is selected.
func (m *Model) SelectedReaction() (emoji string, isPlus bool) {
	msg := m.SelectedMessage()
	if msg == nil {
		return "", false
	}
	if m.reactionNavIndex >= len(msg.Reactions) {
		return "", true
	}
	return msg.Reactions[m.reactionNavIndex].Emoji, false
}

// ClampReactionNav adjusts the reaction nav index after reactions change.
func (m *Model) ClampReactionNav() {
	msg := m.SelectedMessage()
	if msg == nil || len(msg.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(msg.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
	m.cache = nil
}
```

- [ ] **Step 4: Replace reaction rendering with pill style**

In `internal/ui/messages/model.go`, replace the reaction rendering in `renderMessagePlain`. Find the current reaction code block (approximately lines 236-243):

Replace:
```go
	var reactionLine string
	if len(msg.Reactions) > 0 {
		var parts []string
		for _, r := range msg.Reactions {
			parts = append(parts, fmt.Sprintf("%s %d", r.Emoji, r.Count))
		}
		reactionLine = "\n" + lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
			strings.Join(parts, "  "))
	}
```

With:
```go
	var reactionLine string
	if len(msg.Reactions) > 0 {
		var pills []string
		for i, r := range msg.Reactions {
			emojiStr := emoji.Sprint(":" + r.Emoji + ":")
			pillText := fmt.Sprintf("%s %d", emojiStr, r.Count)
			var style lipgloss.Style
			if isSelected && m.reactionNavActive && i == m.reactionNavIndex {
				style = styles.ReactionPillSelected
			} else if r.HasReacted {
				style = styles.ReactionPillOwn
			} else {
				style = styles.ReactionPillOther
			}
			pills = append(pills, style.Render(pillText))
		}
		if isSelected && m.reactionNavActive {
			plusStyle := styles.ReactionPillPlus
			if m.reactionNavIndex >= len(msg.Reactions) {
				plusStyle = styles.ReactionPillSelected
			}
			pills = append(pills, plusStyle.Render("+"))
		}
		reactionLine = "\n" + strings.Join(pills, " ")
	}
```

Note: The `renderMessagePlain` function signature needs to accept whether this message is the selected one. Currently it does not — it renders all messages identically and selection is applied later. We need to change the approach: pass `isSelected` and `m` to the render function so it can conditionally render reaction-nav state.

Update the `renderMessagePlain` signature from:
```go
func renderMessagePlain(msg MessageItem, width int, avatarFn func(userID string) string, userNames map[string]string) string {
```

To:
```go
func (m *Model) renderMessagePlain(msg MessageItem, width int, avatarFn func(userID string) string, userNames map[string]string, isSelected bool) string {
```

Update all call sites of `renderMessagePlain` in `buildCache` to pass `m` as receiver and `isSelected`:

In `buildCache`, the call is inside a loop. Change from:
```go
rendered := renderMessagePlain(msg, width, m.avatarFn, m.userNames)
```
To:
```go
rendered := m.renderMessagePlain(msg, width, m.avatarFn, m.userNames, i == m.selected)
```

Also add the emoji import at the top of `model.go`:
```go
import (
	emoji "github.com/kyokomi/emoji/v2"
)
```

- [ ] **Step 5: Handle reaction-nav cache invalidation for selection changes**

In `MoveUp` and `MoveDown` methods, if reaction-nav is active, exit it:

Add to the top of both `MoveUp()` and `MoveDown()`:
```go
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
```

- [ ] **Step 6: Run tests and build**

Run: `go build ./...`
Expected: PASS (compiles)

Run: `go test ./internal/ui/messages/ -v`
Expected: Existing tests still pass

- [ ] **Step 7: Commit**

```bash
git add internal/ui/styles/styles.go internal/ui/messages/model.go
git commit -m "feat: pill-style reactions with reaction-nav state"
```

---

### Task 4: Thread Panel Reaction Support

**Files:**
- Modify: `internal/ui/thread/model.go`

- [ ] **Step 1: Add reaction-nav state and pill rendering to thread model**

The thread panel's `renderThreadMessage` (line 254) currently doesn't render reactions at all. The thread `Model` also doesn't store `MessageItem` — it stores its own inline struct. Check the exact types used.

In `internal/ui/thread/model.go`, the replies are stored as `[]messages.MessageItem` (imported from the messages package). Add reaction-nav state fields to the thread `Model` struct:

```go
	reactionNavActive bool
	reactionNavIndex  int
```

Add a `SelectedReply` method if one doesn't exist:
```go
func (m *Model) SelectedReply() *messages.MessageItem {
	if m.selected < 0 || m.selected >= len(m.replies) {
		return nil
	}
	return &m.replies[m.selected]
}
```

Add reaction-nav methods (same logic as messages.Model but operating on `m.replies[m.selected]`):

```go
func (m *Model) EnterReactionNav() {
	if reply := m.SelectedReply(); reply != nil && len(reply.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
	}
}

func (m *Model) ExitReactionNav() {
	m.reactionNavActive = false
	m.reactionNavIndex = 0
}

func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

func (m *Model) ReactionNavLeft() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
}

func (m *Model) ReactionNavRight() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
}

func (m *Model) SelectedReaction() (emoji string, isPlus bool) {
	reply := m.SelectedReply()
	if reply == nil {
		return "", false
	}
	if m.reactionNavIndex >= len(reply.Reactions) {
		return "", true
	}
	return reply.Reactions[m.reactionNavIndex].Emoji, false
}

func (m *Model) ClampReactionNav() {
	reply := m.SelectedReply()
	if reply == nil || len(reply.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(reply.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
}

func (m *Model) UpdateReaction(messageTS, emoji, userID string, remove bool) {
	for i, reply := range m.replies {
		if reply.TS == messageTS {
			if remove {
				for j, r := range reply.Reactions {
					if r.Emoji == emoji {
						r.Count--
						if r.Count <= 0 {
							m.replies[i].Reactions = append(reply.Reactions[:j], reply.Reactions[j+1:]...)
						} else {
							r.HasReacted = false
							m.replies[i].Reactions[j] = r
						}
						break
					}
				}
			} else {
				found := false
				for j, r := range reply.Reactions {
					if r.Emoji == emoji {
						r.Count++
						r.HasReacted = true
						m.replies[i].Reactions[j] = r
						found = true
						break
					}
				}
				if !found {
					m.replies[i].Reactions = append(m.replies[i].Reactions, messages.ReactionItem{
						Emoji:      emoji,
						Count:      1,
						HasReacted: true,
					})
				}
			}
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			return
		}
	}
}
```

Update `renderThreadMessage` to include pill-style reaction rendering. Add the same emoji import and pill rendering logic as messages model — render `reply.Reactions` as styled pills after the message text, using `styles.ReactionPillOwn`/`ReactionPillOther`/`ReactionPillSelected` and `emoji.Sprint(":" + r.Emoji + ":")`.

Exit reaction-nav in MoveUp/MoveDown by adding at the top of each:
```go
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
```

- [ ] **Step 2: Run tests and build**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./internal/ui/thread/ -v`
Expected: Existing tests pass

- [ ] **Step 3: Commit**

```bash
git add internal/ui/thread/model.go
git commit -m "feat: add reaction rendering and nav to thread panel"
```

---

### Task 5: Reaction Picker Overlay Component

**Files:**
- Create: `internal/ui/reactionpicker/model.go`
- Create: `internal/ui/reactionpicker/model_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/reactionpicker/model_test.go`:

```go
package reactionpicker

import (
	"testing"
)

func TestNewModel(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected picker to start hidden")
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", []string{"thumbsup"})
	if !m.IsVisible() {
		t.Error("expected picker to be visible after Open")
	}
	if m.channelID != "C123" {
		t.Errorf("expected channelID C123, got %s", m.channelID)
	}
	m.Close()
	if m.IsVisible() {
		t.Error("expected picker to be hidden after Close")
	}
}

func TestFilterByQuery(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)

	// Type "rock" to filter
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")

	if m.query != "rock" {
		t.Errorf("expected query 'rock', got '%s'", m.query)
	}
	// Should have filtered results containing "rock" (e.g., "rocket")
	if len(m.filtered) == 0 {
		t.Error("expected filtered results for 'rock'")
	}
	for _, e := range m.filtered {
		if !containsSubstring(e.Name, "rock") {
			t.Errorf("filtered entry %s doesn't match query 'rock'", e.Name)
		}
	}
}

func TestNavigationUpDown(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	// Type something to get results
	m.HandleKey("h")
	m.HandleKey("e")
	m.HandleKey("a")
	m.HandleKey("r")
	m.HandleKey("t")

	if len(m.filtered) < 2 {
		t.Skip("not enough filtered results for navigation test")
	}

	if m.selected != 0 {
		t.Errorf("expected selected 0, got %d", m.selected)
	}

	m.HandleKey("down")
	if m.selected != 1 {
		t.Errorf("expected selected 1 after down, got %d", m.selected)
	}

	m.HandleKey("up")
	if m.selected != 0 {
		t.Errorf("expected selected 0 after up, got %d", m.selected)
	}
}

func TestSelectEmoji(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")
	m.HandleKey("e")
	m.HandleKey("t")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on enter")
	}
	if result.Emoji != "rocket" && result.Emoji == "" {
		t.Error("expected non-empty emoji in result")
	}
	if result.Remove {
		t.Error("expected Remove=false for new reaction")
	}
}

func TestSelectExistingReactionTogglesRemove(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", []string{"rocket"})
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")
	m.HandleKey("e")
	m.HandleKey("t")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on enter")
	}
	if !result.Remove {
		t.Error("expected Remove=true for existing reaction")
	}
}

func TestEscapeCloses(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	result := m.HandleKey("esc")
	if result != nil {
		t.Error("expected nil result on esc")
	}
	if m.IsVisible() {
		t.Error("expected picker to be hidden after esc")
	}
}

func TestBackspace(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	if m.query != "roc" {
		t.Errorf("expected query 'roc', got '%s'", m.query)
	}
	m.HandleKey("backspace")
	if m.query != "ro" {
		t.Errorf("expected query 'ro' after backspace, got '%s'", m.query)
	}
}

func TestFrecentShownWhenQueryEmpty(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]EmojiEntry{
		{Name: "thumbsup", Unicode: "👍"},
		{Name: "rocket", Unicode: "🚀"},
	})
	m.Open("C123", "1234.5678", nil)

	// With empty query, displayed list should be the frecent list
	displayed := m.displayedList()
	if len(displayed) < 2 {
		t.Fatalf("expected at least 2 frecent entries, got %d", len(displayed))
	}
	if displayed[0].Name != "thumbsup" {
		t.Errorf("expected first frecent entry thumbsup, got %s", displayed[0].Name)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/reactionpicker/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement the reaction picker model**

Create `internal/ui/reactionpicker/model.go`:

```go
package reactionpicker

import (
	"sort"
	"strings"

	emoji "github.com/kyokomi/emoji/v2"
	"github.com/charmbracelet/lipgloss"

	"slack-tui/internal/ui/styles"
)

// EmojiEntry represents an emoji with its name and Unicode character.
type EmojiEntry struct {
	Name    string // e.g. "thumbsup"
	Unicode string // e.g. "👍"
}

// ReactionResult is returned when the user selects an emoji.
type ReactionResult struct {
	Emoji  string // emoji name without colons
	Remove bool   // true if toggling off an existing reaction
}

// Model is the reaction picker overlay.
type Model struct {
	allEmoji           []EmojiEntry
	frecent            []EmojiEntry
	filtered           []EmojiEntry
	query              string
	selected           int
	visible            bool
	messageTS          string
	channelID          string
	existingReactions  []string
}

// New creates a new reaction picker with the full emoji list.
func New() *Model {
	m := &Model{}
	m.buildEmojiList()
	return m
}

func (m *Model) buildEmojiList() {
	codeMap := emoji.CodeMap()
	seen := make(map[string]bool)
	m.allEmoji = make([]EmojiEntry, 0, len(codeMap))

	for code, unicode := range codeMap {
		// Strip surrounding colons
		name := strings.Trim(code, ":")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		m.allEmoji = append(m.allEmoji, EmojiEntry{Name: name, Unicode: unicode})
	}

	sort.Slice(m.allEmoji, func(i, j int) bool {
		return m.allEmoji[i].Name < m.allEmoji[j].Name
	})
}

// Open shows the picker for a specific message.
func (m *Model) Open(channelID, messageTS string, existingReactions []string) {
	m.channelID = channelID
	m.messageTS = messageTS
	m.existingReactions = existingReactions
	m.query = ""
	m.selected = 0
	m.filtered = nil
	m.visible = true
}

// Close hides the picker and resets state.
func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

// IsVisible returns whether the picker is showing.
func (m *Model) IsVisible() bool {
	return m.visible
}

// SetFrecentEmoji sets the frequently/recently used emoji list.
func (m *Model) SetFrecentEmoji(emoji []EmojiEntry) {
	m.frecent = emoji
}

// ChannelID returns the target channel.
func (m *Model) ChannelID() string {
	return m.channelID
}

// MessageTS returns the target message timestamp.
func (m *Model) MessageTS() string {
	return m.messageTS
}

// displayedList returns the list currently shown (frecent or filtered).
func (m *Model) displayedList() []EmojiEntry {
	if m.query == "" {
		return m.frecent
	}
	return m.filtered
}

func (m *Model) filter() {
	if m.query == "" {
		m.filtered = nil
		m.selected = 0
		return
	}

	q := strings.ToLower(m.query)
	m.filtered = m.filtered[:0]

	// Prefix matches first, then substring matches
	var substringMatches []EmojiEntry
	for _, e := range m.allEmoji {
		if strings.HasPrefix(e.Name, q) {
			m.filtered = append(m.filtered, e)
		} else if strings.Contains(e.Name, q) {
			substringMatches = append(substringMatches, e)
		}
		// Cap total results
		if len(m.filtered)+len(substringMatches) >= 50 {
			break
		}
	}
	m.filtered = append(m.filtered, substringMatches...)
	m.selected = 0
}

func (m *Model) isExistingReaction(emoji string) bool {
	for _, r := range m.existingReactions {
		if r == emoji {
			return true
		}
	}
	return false
}

// HandleKey processes a key event and returns a result if an emoji was selected.
func (m *Model) HandleKey(keyStr string) *ReactionResult {
	switch keyStr {
	case "esc", "escape":
		m.Close()
		return nil

	case "enter":
		list := m.displayedList()
		if len(list) == 0 || m.selected >= len(list) {
			return nil
		}
		selected := list[m.selected]
		return &ReactionResult{
			Emoji:  selected.Name,
			Remove: m.isExistingReaction(selected.Name),
		}

	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "down", "j":
		list := m.displayedList()
		if m.selected < len(list)-1 {
			m.selected++
		}
		return nil

	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.filter()
		}
		return nil

	default:
		// Printable character — append to query
		if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
			m.query += keyStr
			m.filter()
		}
		return nil
	}
}

// View renders the picker box content.
func (m *Model) View(termWidth int) string {
	boxWidth := termWidth * 30 / 100
	if boxWidth < 35 {
		boxWidth = 35
	}
	if boxWidth > 50 {
		boxWidth = 50
	}
	innerWidth := boxWidth - 4 // account for border + padding

	var b strings.Builder

	// Title
	title := lipgloss.NewStyle().
		Foreground(styles.Primary).
		Bold(true).
		Render("Add Reaction")
	b.WriteString(title)
	b.WriteString("\n")

	// Query input
	cursor := "|"
	queryDisplay := m.query + cursor
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(styles.Primary).
		PaddingLeft(1)
	b.WriteString(inputStyle.Render(queryDisplay))
	b.WriteString("\n")

	// Separator
	sep := lipgloss.NewStyle().
		Foreground(styles.Border).
		Render(strings.Repeat("─", innerWidth))
	b.WriteString(sep)
	b.WriteString("\n")

	// Results list
	list := m.displayedList()
	maxVisible := 10
	if len(list) < maxVisible {
		maxVisible = len(list)
	}

	// Calculate scroll window
	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(list) {
		end = len(list)
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}

	if len(list) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Render("No matching emoji")
		b.WriteString(noResults)
	}

	for i := start; i < end; i++ {
		entry := list[i]
		prefix := "  "
		if i == m.selected {
			prefix = lipgloss.NewStyle().
				Foreground(styles.Accent).
				Render("▌ ")
		}

		emojiDisplay := entry.Unicode + " " + entry.Name

		suffix := ""
		if m.isExistingReaction(entry.Name) {
			suffix = lipgloss.NewStyle().
				Foreground(styles.Accent).
				Render(" ✓")
		}

		line := prefix + emojiDisplay + suffix
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// ViewOverlay composites the picker on top of the background.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	boxWidth := termWidth * 30 / 100
	if boxWidth < 35 {
		boxWidth = 35
	}
	if boxWidth > 50 {
		boxWidth = 50
	}

	content := m.View(termWidth)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(
		termWidth, termHeight,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0F0F1A")),
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/reactionpicker/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/reactionpicker/
git commit -m "feat: reaction picker overlay component"
```

---

### Task 6: Mode and Key Binding Updates

**Files:**
- Modify: `internal/ui/mode.go`
- Modify: `internal/ui/keys.go`

- [ ] **Step 1: Add ModeReactionPicker**

In `internal/ui/mode.go`, add `ModeReactionPicker` to the const block after `ModeChannelFinder`:

```go
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeSearch
	ModeChannelFinder
	ModeReactionPicker
)
```

Update the `String()` method to include the new mode:

Add a case before the default:
```go
	case ModeReactionPicker:
		return "REACT"
```

- [ ] **Step 2: Add ReactionNav key binding**

In `internal/ui/keys.go`, add `ReactionNav` to the `KeyMap` struct after `Reaction`:

```go
	ReactionNav    key.Binding
```

In `DefaultKeyMap()`, add the binding after the Reaction binding:

```go
	ReactionNav: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "navigate reactions")),
```

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: Compiles

- [ ] **Step 4: Commit**

```bash
git add internal/ui/mode.go internal/ui/keys.go
git commit -m "feat: add ModeReactionPicker and ReactionNav key binding"
```

---

### Task 7: App Integration — Picker Wiring and Mode Handler

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add reaction picker field, message types, and callback types**

In `internal/ui/app.go`, add the import for the reactionpicker package:

```go
	"slack-tui/internal/ui/reactionpicker"
```

Add the new message types after the existing ones (e.g., after `ConnectionStateMsg`):

```go
// ReactionAddedMsg is sent when a reaction is added via WebSocket.
type ReactionAddedMsg struct {
	ChannelID string
	MessageTS string
	UserID    string
	Emoji     string
}

// ReactionRemovedMsg is sent when a reaction is removed via WebSocket.
type ReactionRemovedMsg struct {
	ChannelID string
	MessageTS string
	UserID    string
	Emoji     string
}

// ReactionSentMsg is sent after the API call completes (or fails).
type ReactionSentMsg struct {
	Err error
}
```

Add callback types after the existing ones:

```go
type ReactionAddFunc func(channelID, messageTS, emoji string) error
type ReactionRemoveFunc func(channelID, messageTS, emoji string) error
```

Add fields to the `App` struct:

```go
	reactionPicker    *reactionpicker.Model
	reactionAddFn     ReactionAddFunc
	reactionRemoveFn  ReactionRemoveFunc
	currentUserID     string
```

- [ ] **Step 2: Add setter methods and initialize picker**

Add setter methods:

```go
func (a *App) SetReactionSender(add ReactionAddFunc, remove ReactionRemoveFunc) {
	a.reactionAddFn = add
	a.reactionRemoveFn = remove
}

func (a *App) SetCurrentUserID(userID string) {
	a.currentUserID = userID
}
```

In `NewApp()` (or wherever sub-models are initialized), add:

```go
	reactionPicker: reactionpicker.New(),
```

- [ ] **Step 3: Add handleReactionPickerMode**

Add the new mode handler:

```go
func (a *App) handleReactionPickerMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()

	// Map tea key names to what the picker expects
	switch msg.Type {
	case tea.KeyEscape, tea.KeyEsc:
		keyStr = "esc"
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	}

	result := a.reactionPicker.HandleKey(keyStr)

	if !a.reactionPicker.IsVisible() {
		// Esc was pressed
		a.mode = ModeNormal
		return nil
	}

	if result != nil {
		// Capture values before Close resets them
		channelID := a.reactionPicker.ChannelID()
		messageTS := a.reactionPicker.MessageTS()
		emojiName := result.Emoji

		a.reactionPicker.Close()
		a.mode = ModeNormal

		// Optimistic update
		a.updateReactionOnMessage(channelID, messageTS, emojiName, a.currentUserID, result.Remove)

		// Fire API call
		if result.Remove {
			if a.reactionRemoveFn != nil {
				return func() tea.Msg {
					err := a.reactionRemoveFn(channelID, messageTS, emojiName)
					return ReactionSentMsg{Err: err}
				}
			}
		} else {
			if a.reactionAddFn != nil {
				return func() tea.Msg {
					err := a.reactionAddFn(channelID, messageTS, emojiName)
					return ReactionSentMsg{Err: err}
				}
			}
		}
	}

	return nil
}
```

- [ ] **Step 4: Add updateReactionOnMessage helper**

This updates the message's Reactions slice for optimistic UI:

```go
func (a *App) updateReactionOnMessage(channelID, messageTS, emojiName, userID string, remove bool) {
	// Update in messages model
	a.messages.UpdateReaction(messageTS, emojiName, userID, remove)
	// Update in thread model if applicable
	a.threadPanel.UpdateReaction(messageTS, emojiName, userID, remove)
}
```

Add `UpdateReaction` to both `messages.Model` and `thread.Model`:

In `internal/ui/messages/model.go`, add:

```go
// UpdateReaction adds or removes a reaction on a message.
func (m *Model) UpdateReaction(messageTS, emoji, userID string, remove bool) {
	for i, msg := range m.messages {
		if msg.TS == messageTS {
			if remove {
				for j, r := range msg.Reactions {
					if r.Emoji == emoji {
						r.Count--
						if r.Count <= 0 {
							m.messages[i].Reactions = append(msg.Reactions[:j], msg.Reactions[j+1:]...)
						} else {
							r.HasReacted = false
							m.messages[i].Reactions[j] = r
						}
						break
					}
				}
			} else {
				found := false
				for j, r := range msg.Reactions {
					if r.Emoji == emoji {
						r.Count++
						r.HasReacted = true
						m.messages[i].Reactions[j] = r
						found = true
						break
					}
				}
				if !found {
					m.messages[i].Reactions = append(m.messages[i].Reactions, ReactionItem{
						Emoji:      emoji,
						Count:      1,
						HasReacted: true,
					})
				}
			}
			m.cache = nil // invalidate render cache
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			return
		}
	}
}
```

Add the equivalent `UpdateReaction` to `internal/ui/thread/model.go` operating on `m.replies`.

- [ ] **Step 5: Wire reaction-nav key handling in handleNormalMode**

In `handleNormalMode`, add reaction-nav interception at the top (before other key checks):

```go
	// Reaction-nav sub-state (intercept before normal keys)
	if a.focusedPanel == PanelMessages && a.messages.ReactionNavActive() {
		return a.handleReactionNav(msg)
	}
	if a.focusedPanel == PanelThread && a.threadPanel.ReactionNavActive() {
		return a.handleThreadReactionNav(msg)
	}
```

Add after the existing key handlers for `r` and `R`:

```go
	case key.Matches(msg, a.keys.Reaction):
		// Open reaction picker on selected message
		if a.focusedPanel == PanelMessages {
			if sel := a.messages.SelectedMessage(); sel != nil {
				var existing []string
				for _, r := range sel.Reactions {
					if r.HasReacted {
						existing = append(existing, r.Emoji)
					}
				}
				a.reactionPicker.Open(a.activeChannelID, sel.TS, existing)
				a.mode = ModeReactionPicker
			}
		} else if a.focusedPanel == PanelThread {
			if sel := a.threadPanel.SelectedReply(); sel != nil {
				var existing []string
				for _, r := range sel.Reactions {
					if r.HasReacted {
						existing = append(existing, r.Emoji)
					}
				}
				a.reactionPicker.Open(a.threadPanel.ChannelID(), sel.TS, existing)
				a.mode = ModeReactionPicker
			}
		}
		return nil

	case key.Matches(msg, a.keys.ReactionNav):
		if a.focusedPanel == PanelMessages {
			a.messages.EnterReactionNav()
		} else if a.focusedPanel == PanelThread {
			a.threadPanel.EnterReactionNav()
		}
		return nil
```

- [ ] **Step 6: Add handleReactionNav helper**

```go
func (a *App) handleReactionNav(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.Left):
		a.messages.ReactionNavLeft()
	case key.Matches(msg, a.keys.Right):
		a.messages.ReactionNavRight()
	case key.Matches(msg, a.keys.Enter):
		emoji, isPlus := a.messages.SelectedReaction()
		if isPlus {
			// Open full picker
			if sel := a.messages.SelectedMessage(); sel != nil {
				var existing []string
				for _, r := range sel.Reactions {
					if r.HasReacted {
						existing = append(existing, r.Emoji)
					}
				}
				a.messages.ExitReactionNav()
				a.reactionPicker.Open(a.activeChannelID, sel.TS, existing)
				a.mode = ModeReactionPicker
			}
		} else {
			// Toggle the selected reaction
			sel := a.messages.SelectedMessage()
			if sel != nil {
				remove := false
				for _, r := range sel.Reactions {
					if r.Emoji == emoji && r.HasReacted {
						remove = true
						break
					}
				}
				a.updateReactionOnMessage(a.activeChannelID, sel.TS, emoji, a.currentUserID, remove)
				if remove {
					if a.reactionRemoveFn != nil {
						channelID := a.activeChannelID
						ts := sel.TS
						e := emoji
						return func() tea.Msg {
							err := a.reactionRemoveFn(channelID, ts, e)
							return ReactionSentMsg{Err: err}
						}
					}
				} else {
					if a.reactionAddFn != nil {
						channelID := a.activeChannelID
						ts := sel.TS
						e := emoji
						return func() tea.Msg {
							err := a.reactionAddFn(channelID, ts, e)
							return ReactionSentMsg{Err: err}
						}
					}
				}
			}
		}
	case key.Matches(msg, a.keys.Reaction):
		// r opens full picker from reaction-nav
		if sel := a.messages.SelectedMessage(); sel != nil {
			var existing []string
			for _, r := range sel.Reactions {
				if r.HasReacted {
					existing = append(existing, r.Emoji)
				}
			}
			a.messages.ExitReactionNav()
			a.reactionPicker.Open(a.activeChannelID, sel.TS, existing)
			a.mode = ModeReactionPicker
		}
	case key.Matches(msg, a.keys.Escape):
		a.messages.ExitReactionNav()
	}
	return nil
}
```

Add a similar `handleThreadReactionNav` method that operates on `a.threadPanel` instead of `a.messages`.

- [ ] **Step 7: Wire mode dispatch and Update handler**

In `handleKey`, add the reaction picker mode to the switch:

```go
	case ModeReactionPicker:
		return a.handleReactionPickerMode(msg)
```

In `Update`, add handlers for the new message types:

```go
	case ReactionAddedMsg:
		a.updateReactionOnMessage(msg.ChannelID, msg.MessageTS, msg.Emoji, msg.UserID, false)
		return a, nil

	case ReactionRemovedMsg:
		a.updateReactionOnMessage(msg.ChannelID, msg.MessageTS, msg.Emoji, msg.UserID, true)
		return a, nil

	case ReactionSentMsg:
		if msg.Err != nil {
			// TODO: show error in status bar
			// For now, silently ignore — the optimistic update stays
		}
		return a, nil
```

- [ ] **Step 8: Wire overlay in View**

In `View()`, after the channel finder overlay check, add:

```go
	if a.reactionPicker.IsVisible() {
		screen = a.reactionPicker.ViewOverlay(a.width, a.height, screen)
	}
```

- [ ] **Step 9: Build and run tests**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 10: Commit**

```bash
git add internal/ui/app.go internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "feat: wire reaction picker into app with mode handling and reaction-nav"
```

---

### Task 8: Populate Reactions from API and Wire RTM Events

**Files:**
- Modify: `cmd/slack-tui/main.go`

- [ ] **Step 1: Populate reactions when fetching channel messages**

In `cmd/slack-tui/main.go`, find the `fetchChannelMessages` function. In the loop where `slack.Message` is converted to `messages.MessageItem`, populate the `Reactions` field.

Find the message conversion block (approximately line 418 where `MessageItem` is constructed) and add reactions conversion after the existing fields:

```go
			// Convert reactions
			var reactions []messages.ReactionItem
			for _, r := range m.Reactions {
				hasReacted := false
				for _, uid := range r.Users {
					if uid == currentUserID {
						hasReacted = true
						break
					}
				}
				reactions = append(reactions, messages.ReactionItem{
					Emoji:      r.Name,
					Count:      r.Count,
					HasReacted: hasReacted,
				})
				// Cache to SQLite
				_ = cacheDB.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
			}
```

Then set `Reactions: reactions` on the `MessageItem`.

Do the same in `fetchOlderMessages` and `fetchThreadReplies`.

Note: `currentUserID` needs to be available in these functions. It's stored on the Slack client or can be passed as a parameter. Check how the existing code gets the user ID — likely from the auth info. Pass it through or capture it in the closure.

- [ ] **Step 2: Wire RTM event handlers**

Replace the TODO stubs for `OnReactionAdded` and `OnReactionRemoved`:

```go
func (h *rtmEventHandler) OnReactionAdded(channelID, ts, userID, emoji string) {
	// Update cache
	rows, err := h.cacheDB.GetReactions(ts, channelID)
	if err == nil {
		found := false
		for _, r := range rows {
			if r.Emoji == emoji {
				userIDs := append(r.UserIDs, userID)
				_ = h.cacheDB.UpsertReaction(ts, channelID, emoji, userIDs, r.Count+1)
				found = true
				break
			}
		}
		if !found {
			_ = h.cacheDB.UpsertReaction(ts, channelID, emoji, []string{userID}, 1)
		}
	}

	h.program.Send(ui.ReactionAddedMsg{
		ChannelID: channelID,
		MessageTS: ts,
		UserID:    userID,
		Emoji:     emoji,
	})
}

func (h *rtmEventHandler) OnReactionRemoved(channelID, ts, userID, emoji string) {
	// Update cache
	rows, err := h.cacheDB.GetReactions(ts, channelID)
	if err == nil {
		for _, r := range rows {
			if r.Emoji == emoji {
				var newUserIDs []string
				for _, uid := range r.UserIDs {
					if uid != userID {
						newUserIDs = append(newUserIDs, uid)
					}
				}
				if len(newUserIDs) == 0 {
					_ = h.cacheDB.DeleteReaction(ts, channelID, emoji)
				} else {
					_ = h.cacheDB.UpsertReaction(ts, channelID, emoji, newUserIDs, r.Count-1)
				}
				break
			}
		}
	}

	h.program.Send(ui.ReactionRemovedMsg{
		ChannelID: channelID,
		MessageTS: ts,
		UserID:    userID,
		Emoji:     emoji,
	})
}
```

Note: The `rtmEventHandler` struct needs access to `cacheDB`. Check if it already has it — if not, add a `cacheDB *cache.DB` field and pass it during construction.

- [ ] **Step 3: Wire reaction sender callbacks**

In the main function where other callbacks are wired (after `SetMessageSender`, etc.), add:

```go
	app.SetReactionSender(
		func(channelID, messageTS, emoji string) error {
			return slackClient.AddReaction(ctx, channelID, messageTS, emoji)
		},
		func(channelID, messageTS, emoji string) error {
			return slackClient.RemoveReaction(ctx, channelID, messageTS, emoji)
		},
	)
	app.SetCurrentUserID(currentUserID)
```

- [ ] **Step 4: Wire frecent loading and recording**

Add two new callback types and fields to `internal/ui/app.go`:

```go
type FrecentLoadFunc func(limit int) []reactionpicker.EmojiEntry
type FrecentRecordFunc func(emoji string)
```

Add fields to `App` struct:

```go
	frecentLoadFn   FrecentLoadFunc
	frecentRecordFn FrecentRecordFunc
```

Add setter:

```go
func (a *App) SetFrecentFuncs(load FrecentLoadFunc, record FrecentRecordFunc) {
	a.frecentLoadFn = load
	a.frecentRecordFn = record
}
```

In `handleNormalMode`, in the `r` key handler, before calling `a.reactionPicker.Open(...)`, load frecent:

```go
	if a.frecentLoadFn != nil {
		a.reactionPicker.SetFrecentEmoji(a.frecentLoadFn(10))
	}
```

In `handleReactionPickerMode`, after a successful add (not remove), record usage:

```go
	if result != nil && !result.Remove {
		if a.frecentRecordFn != nil {
			a.frecentRecordFn(result.Emoji)
		}
	}
```

Wire in `main.go` after the other callback setups:

```go
	app.SetFrecentFuncs(
		func(limit int) []reactionpicker.EmojiEntry {
			names, err := cacheDB.GetFrecentEmoji(limit)
			if err != nil {
				return nil
			}
			codeMap := emoji.CodeMap()
			var entries []reactionpicker.EmojiEntry
			for _, name := range names {
				unicode := codeMap[":"+name+":"]
				entries = append(entries, reactionpicker.EmojiEntry{
					Name:    name,
					Unicode: unicode,
				})
			}
			return entries
		},
		func(emojiName string) {
			_ = cacheDB.RecordEmojiUse(emojiName)
		},
	)
```

- [ ] **Step 6: Build and test**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add cmd/slack-tui/main.go internal/ui/app.go
git commit -m "feat: wire reaction data flow - API population, RTM events, callbacks"
```

---

### Task 9: Final Integration Testing and Polish

**Files:**
- Various — fix any remaining compilation issues

- [ ] **Step 1: Full build verification**

Run: `go build ./...`
Expected: Clean compile

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass

- [ ] **Step 3: Verify import cycles**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 4: Test manually**

Run: `./bin/slack-tui`
Verify:
1. Messages with reactions show pill-style display
2. Press `r` on a message to open the reaction picker
3. Type to filter emoji, select with Enter
4. Reaction appears on the message
5. Press `R` on a message with reactions to enter reaction-nav
6. Navigate with h/l, toggle with Enter
7. Real-time reactions from other users update in place

- [ ] **Step 5: Update STATUS.md**

In `docs/STATUS.md`, move the reaction picker line from "Not Yet Implemented" to the appropriate "What's Working" section:

Change:
```
- [ ] Reaction picker (press `r` to add emoji reaction)
```

To add under a new "### Reactions" section in "What's Working":
```
### Reactions
- [x] Reaction picker overlay (press `r` — search-first with frecent emoji)
- [x] Quick-toggle reaction nav (press `R` — h/l to navigate, Enter to toggle)
- [x] Pill-style reaction display (green = your reaction, gray = others)
- [x] Real-time reaction sync via WebSocket
- [x] Frecent emoji tracking (most-used emoji shown first)
- [x] Optimistic UI updates
```

- [ ] **Step 6: Final commit**

```bash
git add -A
git commit -m "feat: complete reaction picker implementation"
```
