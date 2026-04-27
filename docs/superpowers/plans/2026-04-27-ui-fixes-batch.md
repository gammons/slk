# UI Fixes Batch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix seven UI issues: workspace rail dot, sidebar width/truncation, status bar alignment, external guest DM names, presence wiring, and mouse click support.

**Architecture:** Each fix is independent and modifies a small set of files. The workspace rail, sidebar, and status bar are self-contained components under `internal/ui/`. Mouse support requires changes to the main app view and update loop. Presence wiring connects the existing RTM event handler stub to the existing sidebar rendering code.

**Tech Stack:** Go, bubbletea v2, lipgloss v2, muesli/reflow/truncate

---

### Task 1: Widen workspace rail to 6 columns, use `●` dot on same line

**Files:**
- Modify: `internal/ui/workspace/model.go`

- [ ] **Step 1: Update `Width()` to return 6**

In `internal/ui/workspace/model.go`, change line 108-110:

```go
func (m Model) Width() int {
	return 6 // 6 content, no border
}
```

- [ ] **Step 2: Update `View()` to render dot on same line and use `●` instead of `*`**

Replace lines 83-86 in the `View()` function. Change:

```go
		label := style.Render(item.Initials)
		if item.HasUnread && i != m.selected {
			label += "\n" + styles.PresenceOnline.Render("*")
		}
```

To:

```go
		initials := item.Initials
		if item.HasUnread && i != m.selected {
			initials = initials + styles.PresenceOnline.Render("●")
		}
		label := style.Render(initials)
```

- [ ] **Step 3: Update the rail width in the style wrapper**

In the same `View()` function, change `Width(5)` to `Width(6)` on line 97:

```go
	rail := lipgloss.NewStyle().
		Width(6).
```

- [ ] **Step 4: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/workspace/model.go
git commit -m "fix: workspace rail uses dot on same line, widen to 6 cols"
```

---

### Task 2: Widen sidebar to 30 columns, fix unread dot truncation

**Files:**
- Modify: `internal/ui/sidebar/model.go`

- [ ] **Step 1: Update `Width()` to return 30**

In `internal/ui/sidebar/model.go`, change lines 287-289:

```go
func (m Model) Width() int {
	return 30
}
```

- [ ] **Step 2: Fix name truncation to account for unread dot**

The line layout is: `cursor(1) + prefix(2) + name + " "(1) + unreadDot(1)`. The line is rendered with `style.Width(width - 2)` (accounting for borders). So available name width = `(width - 2) - 1 - 2 - 2 = width - 7`.

Replace lines 185-195 in `View()`:

```go
		// Truncate name to fit sidebar width.
		// Layout: cursor(1) + prefix(2) + name + " "(1) + unreadDot(1) = 5 fixed chars
		// Rendered with style.Width(width-2), so available = width - 2 - 5 = width - 7
		name := item.Name
		maxNameLen := width - 7
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if len(name) > maxNameLen {
			name = truncate.StringWithTail(name, uint(maxNameLen), "…")
		}

		label := cursor + prefix + name + " " + unreadDot
```

- [ ] **Step 3: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/sidebar/model.go
git commit -m "fix: widen sidebar to 30 cols, truncate names to fit unread dot on same line"
```

---

### Task 3: Right-align "Connected" indicator with inner panel edge

**Files:**
- Modify: `internal/ui/app.go` (View method, line 1674)

- [ ] **Step 1: Offset the status bar to start after the workspace rail**

In `internal/ui/app.go`, replace line 1674:

```go
	status := a.statusbar.View(a.width)
```

With:

```go
	statusWidth := a.width - railWidth
	railSpacer := lipgloss.NewStyle().
		Width(railWidth).
		Background(styles.SurfaceDark).
		Render("")
	status := lipgloss.JoinHorizontal(lipgloss.Center, railSpacer, a.statusbar.View(statusWidth))
```

- [ ] **Step 2: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/app.go
git commit -m "fix: right-align Connected indicator with inner panel edge"
```

---

### Task 4: Fix external guest DM name resolution

**Files:**
- Modify: `cmd/slk/main.go` (WorkspaceContext struct, connectWorkspace, caller goroutine)
- Modify: `internal/ui/app.go` (new `DMNameResolvedMsg`)

- [ ] **Step 1: Add `UnresolvedDM` type and field to `WorkspaceContext`**

In `cmd/slk/main.go`, add the type and update the struct (around line 32):

```go
type UnresolvedDM struct {
	ChannelID string
	UserID    string
}

type WorkspaceContext struct {
	Client        *slackclient.Client
	ConnMgr       *slackclient.ConnectionManager
	RTMHandler    *rtmEventHandler
	UserNames     map[string]string
	LastReadMap   map[string]string
	Channels      []sidebar.ChannelItem
	FinderItems   []channelfinder.Item
	TeamID        string
	TeamName      string
	UserID        string
	UnresolvedDMs []UnresolvedDM
}
```

- [ ] **Step 2: Collect unresolved DMs during channel loading**

In `cmd/slk/main.go`, change the DM name resolution block (lines 493-500) to also track unresolved DMs:

```go
		displayName := ch.Name
		if ch.IsIM {
			if resolved, ok := wctx.UserNames[ch.User]; ok {
				displayName = resolved
			} else {
				displayName = ch.User
				wctx.UnresolvedDMs = append(wctx.UnresolvedDMs, UnresolvedDM{
					ChannelID: ch.ID,
					UserID:    ch.User,
				})
			}
		}
```

- [ ] **Step 3: Add `DMNameResolvedMsg` type to `app.go`**

In `internal/ui/app.go`, add in the message type block (around line 36):

```go
	DMNameResolvedMsg struct {
		ChannelID   string
		DisplayName string
	}
```

- [ ] **Step 4: Handle `DMNameResolvedMsg` in `Update()`**

In `internal/ui/app.go`, add a case in `switch msg := msg.(type)` (after `ChannelMarkedReadMsg` case):

```go
	case DMNameResolvedMsg:
		items := a.sidebar.Items()
		for i := range items {
			if items[i].ID == msg.ChannelID {
				items[i].Name = msg.DisplayName
				break
			}
		}
		a.sidebar.SetItems(items)
```

- [ ] **Step 5: Launch async resolution in caller goroutine**

In `cmd/slk/main.go`, inside the goroutine at line 348, after `p.Send(ui.WorkspaceReadyMsg{...})` (line 393-400), add:

```go
			// Resolve unknown DM user names in background
			if len(wctx.UnresolvedDMs) > 0 {
				go func() {
					for _, dm := range wctx.UnresolvedDMs {
						resolved := resolveUser(wctx.Client, dm.UserID, wctx.UserNames, db, avatarCache)
						if resolved != dm.UserID {
							p.Send(ui.DMNameResolvedMsg{
								ChannelID:   dm.ChannelID,
								DisplayName: resolved,
							})
						}
					}
				}()
			}
```

- [ ] **Step 6: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go cmd/slk/main.go
git commit -m "fix: resolve external guest DM names asynchronously"
```

---

### Task 5: Wire up presence / online status

**Files:**
- Modify: `internal/ui/sidebar/model.go` (add `DMUserID` field, `UpdatePresenceByUser` method)
- Modify: `internal/ui/app.go` (new `PresenceChangeMsg`, handler)
- Modify: `cmd/slk/main.go` (`OnPresenceChange` handler, initial presence on DM items)

- [ ] **Step 1: Add `DMUserID` field to `ChannelItem`**

In `internal/ui/sidebar/model.go`, update the struct (line 13-21):

```go
type ChannelItem struct {
	ID          string
	Name        string
	Type        string // channel, dm, group_dm, private
	Section     string // section name for grouping
	UnreadCount int
	IsStarred   bool
	Presence    string // for DMs: active, away, dnd
	DMUserID    string // for DMs: the user ID of the other party
}
```

- [ ] **Step 2: Add `UpdatePresenceByUser` method to sidebar**

In `internal/ui/sidebar/model.go`, add after `ClearUnread`:

```go
// UpdatePresenceByUser updates the presence for any DM item whose DMUserID matches.
func (m *Model) UpdatePresenceByUser(userID, presence string) {
	for i := range m.items {
		if m.items[i].DMUserID == userID {
			m.items[i].Presence = presence
			return
		}
	}
}
```

- [ ] **Step 3: Set `DMUserID` and initial presence during channel loading**

In `cmd/slk/main.go`, replace the `ChannelItem` construction (lines 503-508):

Change:
```go
		wctx.Channels = append(wctx.Channels, sidebar.ChannelItem{
			ID:      ch.ID,
			Name:    displayName,
			Type:    chType,
			Section: section,
		})
```

To:
```go
		item := sidebar.ChannelItem{
			ID:      ch.ID,
			Name:    displayName,
			Type:    chType,
			Section: section,
		}
		if ch.IsIM {
			item.DMUserID = ch.User
			if cachedUser, err := db.GetUser(ch.User); err == nil && cachedUser.Presence != "" {
				item.Presence = cachedUser.Presence
			}
		}
		wctx.Channels = append(wctx.Channels, item)
```

- [ ] **Step 4: Add `PresenceChangeMsg` to `app.go`**

In `internal/ui/app.go`, add to the message type block:

```go
	PresenceChangeMsg struct {
		UserID   string
		Presence string
	}
```

- [ ] **Step 5: Handle `PresenceChangeMsg` in `Update()`**

In `internal/ui/app.go`, add a case:

```go
	case PresenceChangeMsg:
		a.sidebar.UpdatePresenceByUser(msg.UserID, msg.Presence)
```

- [ ] **Step 6: Implement `OnPresenceChange` in RTM handler**

In `cmd/slk/main.go`, replace lines 966-968:

```go
func (h *rtmEventHandler) OnPresenceChange(userID, presence string) {
	_ = h.db.UpdatePresence(userID, presence)
	if h.program == nil {
		return
	}
	h.program.Send(ui.PresenceChangeMsg{
		UserID:   userID,
		Presence: presence,
	})
}
```

- [ ] **Step 7: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go internal/ui/sidebar/model.go cmd/slk/main.go
git commit -m "feat: wire up presence change events to sidebar DM indicators"
```

---

### Task 6: Enable mouse support and panel focus on click

**Files:**
- Modify: `internal/ui/app.go` (View MouseMode, layout widths, MouseClickMsg handler)
- Modify: `internal/ui/sidebar/model.go` (new `ClickAt` method)
- Modify: `internal/ui/messages/model.go` (new `ClickAt` method)
- Modify: `internal/ui/thread/model.go` (new `ClickAt` method)

- [ ] **Step 1: Enable mouse mode in View()**

In bubbletea v2, mouse is enabled on the `View` struct, not as a program option. In `internal/ui/app.go`, in the `View()` method, update the view construction near the end (around line 1709):

Change:
```go
	v := tea.NewView(finalScreen)
	v.AltScreen = true
	return v
```

To:
```go
	v := tea.NewView(finalScreen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
```

- [ ] **Step 2: Add layout width fields to `App` struct**

In `internal/ui/app.go`, add fields to the `App` struct (after `height int` around line 187):

```go
	// Cached layout widths for mouse hit-testing
	layoutRailWidth    int
	layoutSidebarEnd   int // railWidth + sidebarWidth + sidebarBorder
	layoutMsgEnd       int // layoutSidebarEnd + msgWidth + msgBorder
	layoutThreadEnd    int // layoutMsgEnd + threadWidth + threadBorder
```

- [ ] **Step 3: Store layout widths in `View()`**

In `internal/ui/app.go`, in the `View()` method, after `msgWidth` is calculated (after line 1577), add:

```go
	// Store layout widths for mouse hit-testing in Update()
	a.layoutRailWidth = railWidth
	a.layoutSidebarEnd = railWidth + sidebarWidth + sidebarBorder
	a.layoutMsgEnd = a.layoutSidebarEnd + msgWidth + msgBorder
	if a.threadVisible && threadWidth > 0 {
		a.layoutThreadEnd = a.layoutMsgEnd + threadWidth + threadBorder
	} else {
		a.layoutThreadEnd = a.layoutMsgEnd
	}
```

- [ ] **Step 4: Handle `tea.MouseClickMsg` in `Update()`**

In `internal/ui/app.go`, add a case in `switch msg := msg.(type)` after the `tea.KeyMsg` case (around line 298):

```go
	case tea.MouseClickMsg:
		if a.loading {
			break
		}
		if msg.Button != tea.MouseLeft {
			break
		}
		x := msg.X
		statusHeight := 1
		if msg.Y >= a.height-statusHeight {
			break // click on status bar, ignore
		}

		// Determine which panel was clicked
		if x < a.layoutRailWidth {
			// Workspace rail — ignore for now
		} else if a.sidebarVisible && x < a.layoutSidebarEnd {
			a.focusedPanel = PanelSidebar
			sidebarY := msg.Y - 1 // account for top border
			if sidebarY >= 0 {
				if item, ok := a.sidebar.ClickAt(sidebarY); ok {
					return a, func() tea.Msg {
						return ChannelSelectedMsg{ID: item.ID, Name: item.Name}
					}
				}
			}
		} else if x < a.layoutMsgEnd {
			a.focusedPanel = PanelMessages
			msgY := msg.Y - 1 // account for top border
			if msgY >= 0 {
				a.messagepane.ClickAt(msgY)
			}
		} else if a.threadVisible && x < a.layoutThreadEnd {
			a.focusedPanel = PanelThread
			threadY := msg.Y - 1
			if threadY >= 0 {
				a.threadPanel.ClickAt(threadY)
			}
		}
```

- [ ] **Step 5: Add `ClickAt` method to sidebar**

In `internal/ui/sidebar/model.go`, add:

```go
// ClickAt handles a mouse click at the given y-coordinate (relative to sidebar content top).
// Selects the item at that position. Returns the item and true if a selectable item was clicked.
func (m *Model) ClickAt(y int) (ChannelItem, bool) {
	absoluteY := y + m.vp.YOffset()

	// Rebuild the section structure (same logic as View) to map y to filterIdx.
	// Each channel item = 1 line, each section header = 1 line, blank line between sections.
	sectionOrder := []string{}
	sectionMap := map[string][]int{} // section name -> list of filter indices

	for fi, idx := range m.filtered {
		item := m.items[idx]
		sectionName := item.Section
		if sectionName == "" {
			if item.Type == "dm" || item.Type == "group_dm" {
				sectionName = "Direct Messages"
			} else {
				sectionName = "Channels"
			}
		}
		if _, ok := sectionMap[sectionName]; !ok {
			sectionOrder = append(sectionOrder, sectionName)
		}
		sectionMap[sectionName] = append(sectionMap[sectionName], fi)
	}

	currentLine := 0
	for i, name := range sectionOrder {
		if i > 0 {
			currentLine++ // blank line between sections
		}
		currentLine++ // section header line

		for _, fi := range sectionMap[name] {
			if currentLine == absoluteY {
				m.selected = fi
				idx := m.filtered[fi]
				return m.items[idx], true
			}
			currentLine++
		}
	}
	return ChannelItem{}, false
}
```

- [ ] **Step 6: Add `ClickAt` method to messages model**

In `internal/ui/messages/model.go`, add:

```go
// ClickAt handles a mouse click at the given y-coordinate (relative to message pane content top).
// Selects the message at that position.
func (m *Model) ClickAt(y int) {
	absoluteY := y + m.vp.YOffset()

	// Walk through cached view entries to find which message is at this line
	currentLine := 0
	for _, entry := range m.cache {
		if entry.msgIdx < 0 {
			// Date separator or "new messages" line — skip
			currentLine += entry.height
			continue
		}
		if absoluteY >= currentLine && absoluteY < currentLine+entry.height {
			m.selected = entry.msgIdx
			m.viewCacheValid = false // force re-render with new selection
			return
		}
		currentLine += entry.height
	}
}
```

- [ ] **Step 7: Add `ClickAt` method to thread model**

The thread model's `cache` field is `[]string` (one rendered string per reply), not `[]viewEntry`. Each reply has a height computed via `lipgloss.Height()`. The thread panel also has a header + parent message + separator that appear before the reply list.

In `internal/ui/thread/model.go`, add:

```go
// ClickAt handles a mouse click at the given y-coordinate (relative to thread panel content top).
// Selects the reply at that position.
func (m *Model) ClickAt(y int) {
	if len(m.replies) == 0 || len(m.cache) == 0 {
		return
	}

	// The thread viewport shows only replies (header/parent/separator are above the viewport).
	// Adjust for viewport scroll offset within the reply area.
	absoluteY := y + m.vp.YOffset()

	currentLine := 0
	for i, cached := range m.cache {
		h := lipgloss.Height(cached) + 1 // +1 for the border added during View rendering
		if absoluteY >= currentLine && absoluteY < currentLine+h {
			m.selected = i
			m.viewCacheValid = false
			return
		}
		currentLine += h
	}
}
```

Note: The exact height calculation depends on how the View() method adds borders. Each reply gets a left border via `borderFill.Width(width-1).Render(content)` then `borderSelect/borderInvis.Render(filled)`. The border adds no extra height lines, but the join uses `\n` between replies which adds 1 line. The implementer should verify the click coordinates against the actual rendering and adjust as needed.

- [ ] **Step 8: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/app.go internal/ui/sidebar/model.go internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "feat: add mouse click support for panel focus and item selection"
```

---

### Task 7: Verify all changes together

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 2: Run any existing tests**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 3: Manual verification checklist**

Launch the app with `./bin/slk` and verify:
1. Workspace rail is 6 columns wide with `●` dot on same line as initials
2. Sidebar is 30 columns wide, unread dots always on same line as channel name
3. "Connected" indicator aligns with inner right edge of message panel
4. External guest DMs show display names (may take a moment to resolve)
5. DM presence dots update (green for active, gray for away)
6. Clicking in message panel focuses it and highlights the clicked message
7. Clicking a channel in the sidebar opens it
