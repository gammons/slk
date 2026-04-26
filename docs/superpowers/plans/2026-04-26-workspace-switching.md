# Workspace Switching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable runtime switching between multiple Slack workspaces with live WebSocket connections, loading overlay, number-key shortcuts, and Ctrl+w workspace picker.

**Architecture:** A `WorkspaceContext` struct per workspace holds the client, connection manager, channels, user names, etc. All workspaces connect in parallel at startup. The App receives `WorkspaceSwitchedMsg` to reset the UI. Each workspace has its own `rtmEventHandler`; inactive handlers send unread notifications instead of full messages.

**Tech Stack:** Go, bubbletea, lipgloss, gorilla/websocket

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `internal/ui/workspacefinder/model.go` | Workspace picker overlay (Ctrl+w) |
| `internal/ui/workspacefinder/model_test.go` | Tests for workspace picker |

### Modified Files
| File | Changes |
|------|---------|
| `cmd/slk/main.go` | WorkspaceContext, parallel startup, callback re-wiring, workspace switcher |
| `internal/ui/app.go` | WorkspaceSwitchedMsg, WorkspaceUnreadMsg, loading overlay, number keys, Ctrl+w, ModeWorkspaceFinder |
| `internal/ui/mode.go` | Add ModeWorkspaceFinder |
| `internal/ui/keys.go` | Add WorkspaceFinder key binding (Ctrl+w) |
| `internal/ui/workspace/model.go` | Add SetUnread, SelectByID methods |
| `internal/ui/styles/styles.go` | Spinner/loading styles |

---

### Task 1: WorkspaceContext and Parallel Startup

**Files:**
- Modify: `cmd/slk/main.go`

This is the largest task — restructures the entire startup flow.

- [ ] **Step 1: Define WorkspaceContext struct**

Add at the top of `main.go` (after imports):

```go
// WorkspaceContext holds all state for a single connected workspace.
type WorkspaceContext struct {
	Client      *slackclient.Client
	ConnMgr     *slackclient.ConnectionManager
	RTMHandler  *rtmEventHandler
	UserNames   map[string]string
	LastReadMap map[string]string
	Channels    []sidebar.ChannelItem
	FinderItems []channelfinder.Item
	TeamID      string
	TeamName    string
	UserID      string
}
```

- [ ] **Step 2: Restructure run() to connect all workspaces**

Replace the entire workspace connection loop (from "Build workspace rail items" through the `break`) with:

1. Build `wsItems` for the rail (existing code — keep as-is)
2. Create a `workspaces` map and a channel for results:

```go
	type wsResult struct {
		ctx  *WorkspaceContext
		err  error
		name string
	}

	workspaces := make(map[string]*WorkspaceContext)
	resultCh := make(chan wsResult, len(tokens))

	for _, token := range tokens {
		go func(tok slackclient.Token) {
			wctx, err := connectWorkspace(ctx, tok, db, cfg, avatarCache)
			resultCh <- wsResult{ctx: wctx, err: err, name: tok.TeamName}
		}(token)
	}
```

3. Create the `connectWorkspace` helper function:

```go
func connectWorkspace(ctx context.Context, token slackclient.Token, db *cache.DB, cfg *config.Config, avatarCache *avatar.Cache) (*WorkspaceContext, error) {
	client := slackclient.NewClient(token.AccessToken, token.Cookie)
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connecting %s: %w", token.TeamName, err)
	}

	wctx := &WorkspaceContext{
		Client:      client,
		TeamID:      client.TeamID(),
		TeamName:    token.TeamName,
		UserID:      client.UserID(),
		UserNames:   make(map[string]string),
		LastReadMap: make(map[string]string),
	}

	// Seed user names from cache
	cachedUsers, _ := db.ListUsers(client.TeamID())
	for _, u := range cachedUsers {
		name := u.DisplayName
		if name == "" {
			name = u.Name
		}
		wctx.UserNames[u.ID] = name
	}

	// Background user fetch
	go func() {
		users, err := client.GetUsers(ctx)
		if err != nil {
			return
		}
		for _, u := range users {
			name := u.Profile.DisplayName
			if name == "" {
				name = u.RealName
			}
			if name == "" {
				name = u.Name
			}
			wctx.UserNames[u.ID] = name
			db.UpsertUser(cache.User{
				ID:          u.ID,
				WorkspaceID: client.TeamID(),
				Name:        u.Name,
				DisplayName: name,
				AvatarURL:   u.Profile.Image32,
				Presence:    "away",
			})
			avatarCache.Preload(u.ID, u.Profile.Image32)
		}
	}()

	// Fetch channels
	channels, err := client.GetChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching channels for %s: %w", token.TeamName, err)
	}

	tsFormat := cfg.Appearance.TimestampFormat
	for _, ch := range channels {
		chType := "channel"
		if ch.IsIM {
			chType = "dm"
		} else if ch.IsMpIM {
			chType = "group_dm"
		} else if ch.IsPrivate {
			chType = "private"
		}

		db.UpsertChannel(cache.Channel{
			ID:          ch.ID,
			WorkspaceID: client.TeamID(),
			Name:        ch.Name,
			Type:        chType,
			Topic:       ch.Topic.Value,
			IsMember:    ch.IsMember,
		})

		displayName := ch.Name
		if ch.IsIM {
			if resolved, ok := wctx.UserNames[ch.User]; ok {
				displayName = resolved
			} else {
				displayName = ch.User
			}
		}

		section := cfg.MatchSection(ch.Name)
		wctx.Channels = append(wctx.Channels, sidebar.ChannelItem{
			ID:      ch.ID,
			Name:    displayName,
			Type:    chType,
			Section: section,
		})
	}

	// Fetch unread counts
	unreadCounts, _ := client.GetUnreadCounts()
	unreadMap := make(map[string]int)
	for _, u := range unreadCounts {
		if u.HasUnread {
			unreadMap[u.ChannelID] = u.Count
		}
		if u.LastRead != "" {
			wctx.LastReadMap[u.ChannelID] = u.LastRead
			_ = db.UpdateLastReadTS(u.ChannelID, u.LastRead)
		}
	}
	for i := range wctx.Channels {
		if count, ok := unreadMap[wctx.Channels[i].ID]; ok {
			wctx.Channels[i].UnreadCount = count
		}
	}

	// Build finder items
	for _, ch := range wctx.Channels {
		wctx.FinderItems = append(wctx.FinderItems, channelfinder.Item{
			ID:       ch.ID,
			Name:     ch.Name,
			Type:     ch.Type,
			Presence: ch.Presence,
		})
	}

	return wctx, nil
}
```

4. Collect results and wire up:

```go
	// Wait for all workspaces to connect (with timeout)
	var activeTeamID string
	timeout := time.After(15 * time.Second)
	remaining := len(tokens)

	for remaining > 0 {
		select {
		case result := <-resultCh:
			remaining--
			if result.err != nil {
				log.Printf("Warning: failed to connect workspace %s: %v", result.name, result.err)
				continue
			}
			wctx := result.ctx
			workspaces[wctx.TeamID] = wctx
			if activeTeamID == "" {
				activeTeamID = wctx.TeamID
			}
		case <-timeout:
			log.Printf("Warning: timed out waiting for workspaces to connect")
			remaining = 0
		}
	}

	if activeTeamID == "" {
		return fmt.Errorf("no workspaces connected")
	}

	active := workspaces[activeTeamID]
```

5. Wire callbacks using the active workspace, and store `workspaces` + `activeTeamID` in a closure for the switcher. Replace all the existing callback wiring to use `active.Client`, `active.UserNames`, etc.

6. Create and start RTM handlers + connection managers for ALL workspaces:

```go
	var p *tea.Program

	// ... wire callbacks using active workspace ...

	p = tea.NewProgram(app, tea.WithAltScreen())

	// Start WebSocket for all workspaces
	for _, wctx := range workspaces {
		activeID := activeTeamID // capture for closure
		handler := &rtmEventHandler{
			program:     p,
			userNames:   wctx.UserNames,
			tsFormat:    tsFormat,
			db:          db,
			workspaceID: wctx.TeamID,
			isActive:    func() bool { return wctx.TeamID == activeID },
		}
		wctx.RTMHandler = handler
		wctx.ConnMgr = slackclient.NewConnectionManager(wctx.Client, handler)
		go wctx.ConnMgr.Run(ctx)
		defer wctx.ConnMgr.Stop()
	}
```

Note: The `isActive` closure needs to reference a variable that changes when workspace switches. Use a pointer or atomic. Simplest: store `activeTeamID` as a `*string` pointer that the switcher updates.

- [ ] **Step 3: Add isActive to rtmEventHandler**

Add field:
```go
	isActive func() bool
```

Modify `OnMessage` to send different messages for inactive workspaces:

```go
func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	h.db.UpsertMessage(cache.Message{
		TS:          ts,
		ChannelID:   channelID,
		WorkspaceID: h.workspaceID,
		UserID:      userID,
		Text:        text,
		ThreadTS:    threadTS,
		CreatedAt:   time.Now().Unix(),
	})

	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — just notify about unread
		h.program.Send(ui.WorkspaceUnreadMsg{
			TeamID:    h.workspaceID,
			ChannelID: channelID,
		})
		return
	}

	userName := userID
	if resolved, ok := h.userNames[userID]; ok {
		userName = resolved
	}
	h.program.Send(ui.NewMessageMsg{
		ChannelID: channelID,
		Message: messages.MessageItem{
			TS:        ts,
			UserID:    userID,
			UserName:  userName,
			Text:      text,
			Timestamp: formatTimestamp(ts, h.tsFormat),
			ThreadTS:  threadTS,
			IsEdited:  edited,
		},
	})
}
```

Similarly, modify `OnReactionAdded`/`OnReactionRemoved` to skip UI messages for inactive workspaces (still cache).

- [ ] **Step 4: Add workspace switcher callback**

Wire a `SwitchWorkspaceFunc` on the app. In `main.go`:

```go
	app.SetWorkspaceSwitcher(func(teamID string) tea.Msg {
		wctx, ok := workspaces[teamID]
		if !ok {
			return nil
		}

		// Update active pointer
		activeTeamID = teamID

		// Re-wire all callbacks to the new workspace's client
		client := wctx.Client
		// ... (same callback wiring as initial setup, using wctx.Client, wctx.UserNames, etc.)

		return ui.WorkspaceSwitchedMsg{
			TeamID:      wctx.TeamID,
			TeamName:    wctx.TeamName,
			Channels:    wctx.Channels,
			FinderItems: wctx.FinderItems,
			UserNames:   wctx.UserNames,
			UserID:      wctx.UserID,
		}
	})
```

- [ ] **Step 5: Build and test**

Run: `go build ./...`
Run: `go test ./...`

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: parallel workspace startup with WorkspaceContext"
```

---

### Task 2: App Message Types and Switching Handler

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/workspace/model.go`

- [ ] **Step 1: Add new message types and callback**

In `app.go`, add to the message type block:

```go
	WorkspaceSwitchedMsg struct {
		TeamID      string
		TeamName    string
		Channels    []sidebar.ChannelItem
		FinderItems []channelfinder.Item
		UserNames   map[string]string
		UserID      string
	}

	WorkspaceUnreadMsg struct {
		TeamID    string
		ChannelID string
	}
```

Add callback type and field:

```go
type SwitchWorkspaceFunc func(teamID string) tea.Msg
```

Add to App struct:

```go
	workspaceSwitcher SwitchWorkspaceFunc
	workspaceItems    []workspace.WorkspaceItem // cached for number-key lookup
```

Add setter:

```go
func (a *App) SetWorkspaceSwitcher(fn SwitchWorkspaceFunc) {
	a.workspaceSwitcher = fn
}
```

- [ ] **Step 2: Handle WorkspaceSwitchedMsg in Update**

```go
	case WorkspaceSwitchedMsg:
		a.CloseThread()
		a.compose.Reset()
		a.messagepane.SetMessages(nil)
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.sidebar.SetItems(msg.Channels)
		a.channelFinder.SetItems(msg.FinderItems)
		a.messagepane.SetUserNames(msg.UserNames)
		a.threadPanel.SetUserNames(msg.UserNames)
		a.currentUserID = msg.UserID
		a.statusbar.SetWorkspace(msg.TeamName)
		// Select the workspace in the rail
		a.workspaceRail.SelectByID(msg.TeamID)
		// Load first channel
		if len(msg.Channels) > 0 {
			first := msg.Channels[0]
			return a, func() tea.Msg {
				return ChannelSelectedMsg{ID: first.ID, Name: first.Name}
			}
		}
```

- [ ] **Step 3: Handle WorkspaceUnreadMsg in Update**

```go
	case WorkspaceUnreadMsg:
		a.workspaceRail.SetUnread(msg.TeamID, true)
```

- [ ] **Step 4: Add SelectByID and SetUnread to workspace.Model**

In `internal/ui/workspace/model.go`:

```go
func (m *Model) SelectByID(teamID string) {
	for i, item := range m.items {
		if item.ID == teamID {
			m.selected = i
			return
		}
	}
}

func (m *Model) SetUnread(teamID string, hasUnread bool) {
	for i := range m.items {
		if m.items[i].ID == teamID {
			m.items[i].HasUnread = hasUnread
			return
		}
	}
}
```

- [ ] **Step 5: Cache workspaceItems for number-key lookup**

Override `SetWorkspaces` to also cache items:

```go
func (a *App) SetWorkspaces(items []workspace.WorkspaceItem) {
	a.workspaceRail.SetItems(items)
	a.workspaceItems = items
}
```

- [ ] **Step 6: Build and test**

Run: `go build ./...`
Run: `go test ./...`

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/workspace/model.go
git commit -m "feat: workspace switching message types and UI handler"
```

---

### Task 3: Number Key Switching (1-9)

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add number key handling in handleNormalMode**

Add a new case in the switch block of `handleNormalMode`, before the closing:

```go
	default:
		// Number keys 1-9 switch workspaces
		keyStr := msg.String()
		if len(keyStr) == 1 && keyStr[0] >= '1' && keyStr[0] <= '9' {
			idx := int(keyStr[0] - '1') // 0-indexed
			if idx < len(a.workspaceItems) && a.workspaceSwitcher != nil {
				// Don't switch if already on this workspace
				if a.workspaceItems[idx].ID != a.workspaceRail.SelectedID() {
					switcher := a.workspaceSwitcher
					teamID := a.workspaceItems[idx].ID
					return func() tea.Msg {
						return switcher(teamID)
					}
				}
			}
		}
```

- [ ] **Step 2: Build and test**

Run: `go build ./...`

- [ ] **Step 3: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: number keys 1-9 for workspace switching"
```

---

### Task 4: Workspace Finder Overlay (Ctrl+w)

**Files:**
- Create: `internal/ui/workspacefinder/model.go`
- Create: `internal/ui/workspacefinder/model_test.go`
- Modify: `internal/ui/mode.go`
- Modify: `internal/ui/keys.go`
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add ModeWorkspaceFinder and key binding**

In `mode.go`, add after `ModeReactionPicker`:
```go
	ModeWorkspaceFinder
```

Add String case:
```go
	case ModeWorkspaceFinder:
		return "WORKSPACE"
```

In `keys.go`, add field to KeyMap:
```go
	WorkspaceFinder key.Binding
```

Add binding in DefaultKeyMap:
```go
	WorkspaceFinder: key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "switch workspace")),
```

- [ ] **Step 2: Create workspace finder model**

Create `internal/ui/workspacefinder/model.go` following the channel finder pattern exactly. Same structure: `Model` with `items []Item`, `filtered []int`, `query string`, `selected int`, `visible bool`. Same methods: `New()`, `SetItems()`, `Open()`, `Close()`, `IsVisible()`, `HandleKey()`, `View()`, `ViewOverlay()`.

The `Item` struct:
```go
type Item struct {
	ID       string
	Name     string
	Initials string
}

type WorkspaceResult struct {
	ID   string
	Name string
}
```

Rendering: same overlay box, title "Switch Workspace", `▌` selection indicator. Show initials + name for each item.

- [ ] **Step 3: Create tests**

Create `internal/ui/workspacefinder/model_test.go` with tests for: New, OpenClose, FilterByQuery, Navigation, SelectWorkspace, EscapeCloses, Backspace.

- [ ] **Step 4: Wire into App**

Add `workspaceFinder workspacefinder.Model` to App struct. Initialize in `NewApp()`.

Add `SetWorkspaceFinderItems` method and call it alongside `SetWorkspaces`.

Add `handleWorkspaceFinderMode` method (same pattern as `handleChannelFinderMode`):

```go
func (a *App) handleWorkspaceFinderMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	// ... map key types same as channel finder ...
	result := a.workspaceFinder.HandleKey(keyStr)
	if result != nil {
		a.workspaceFinder.Close()
		a.SetMode(ModeNormal)
		if a.workspaceSwitcher != nil && result.ID != a.workspaceRail.SelectedID() {
			switcher := a.workspaceSwitcher
			teamID := result.ID
			return func() tea.Msg {
				return switcher(teamID)
			}
		}
	}
	if !a.workspaceFinder.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
```

Add case in `handleKey`:
```go
	case ModeWorkspaceFinder:
		return a.handleWorkspaceFinderMode(msg)
```

Add Ctrl+w handler in `handleNormalMode`:
```go
	case key.Matches(msg, a.keys.WorkspaceFinder):
		a.workspaceFinder.Open()
		a.SetMode(ModeWorkspaceFinder)
```

Add overlay in `View()` after reaction picker:
```go
	if a.workspaceFinder.IsVisible() {
		screen = a.workspaceFinder.ViewOverlay(a.width, a.height, screen)
	}
```

- [ ] **Step 5: Build and test**

Run: `go build ./...`
Run: `go test ./...`

- [ ] **Step 6: Commit**

```bash
git add internal/ui/workspacefinder/ internal/ui/mode.go internal/ui/keys.go internal/ui/app.go
git commit -m "feat: Ctrl+w workspace finder overlay"
```

---

### Task 5: Loading Overlay

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/styles/styles.go`

- [ ] **Step 1: Add loading state to App**

Add fields:
```go
	loading        bool
	loadingStates  []loadingEntry
	spinnerFrame   int
```

Where:
```go
type loadingEntry struct {
	TeamName string
	Status   string // "connecting", "ready", "failed"
}
```

Set `loading: true` in `NewApp()`.

Add spinner message type:
```go
type SpinnerTickMsg struct{}
```

In `Init()`, return a tick command:
```go
func (a *App) Init() tea.Cmd {
	if a.loading {
		return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
			return SpinnerTickMsg{}
		})
	}
	return nil
}
```

Handle `SpinnerTickMsg` in Update:
```go
	case SpinnerTickMsg:
		if a.loading {
			a.spinnerFrame = (a.spinnerFrame + 1) % 10
			return a, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			})
		}
```

- [ ] **Step 2: Add loading overlay rendering**

Add method:
```go
var spinnerChars = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func (a *App) renderLoadingOverlay(width, height int, background string) string {
	var rows []string
	spinner := string(spinnerChars[a.spinnerFrame])
	
	for _, entry := range a.loadingStates {
		switch entry.Status {
		case "ready":
			rows = append(rows, styles.Accent.Render("✓")+" "+entry.TeamName)
		case "failed":
			rows = append(rows, styles.Error.Render("✗")+" "+entry.TeamName+" (failed)")
		default:
			rows = append(rows, styles.Primary.Render(spinner)+" Connecting to "+entry.TeamName+"...")
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0F0F1A")),
	)
}
```

In `View()`, render loading overlay last (after all others) if `a.loading`:
```go
	if a.loading {
		screen = a.renderLoadingOverlay(a.width, a.height, screen)
	}
```

- [ ] **Step 3: Add methods to control loading state**

```go
func (a *App) SetLoadingWorkspaces(names []string) {
	a.loading = true
	a.loadingStates = nil
	for _, name := range names {
		a.loadingStates = append(a.loadingStates, loadingEntry{
			TeamName: name,
			Status:   "connecting",
		})
	}
}

func (a *App) SetWorkspaceReady(teamName string) {
	for i := range a.loadingStates {
		if a.loadingStates[i].TeamName == teamName {
			a.loadingStates[i].Status = "ready"
			break
		}
	}
	// Check if all done
	allDone := true
	for _, e := range a.loadingStates {
		if e.Status == "connecting" {
			allDone = false
			break
		}
	}
	if allDone {
		a.loading = false
	}
}

func (a *App) SetWorkspaceFailed(teamName string) {
	for i := range a.loadingStates {
		if a.loadingStates[i].TeamName == teamName {
			a.loadingStates[i].Status = "failed"
			break
		}
	}
	allDone := true
	for _, e := range a.loadingStates {
		if e.Status == "connecting" {
			allDone = false
			break
		}
	}
	if allDone {
		a.loading = false
	}
}
```

- [ ] **Step 4: Wire loading state from main.go**

Before starting the TUI, set loading workspace names:
```go
	var wsNames []string
	for _, t := range tokens {
		wsNames = append(wsNames, t.TeamName)
	}
	app.SetLoadingWorkspaces(wsNames)
```

When `WorkspaceReadyMsg` is processed in `main.go`, call `app.SetWorkspaceReady(name)`. When failed, call `app.SetWorkspaceFailed(name)`.

Actually, since this happens via `p.Send()`, add message types:

```go
type WorkspaceReadyMsg struct {
	Context *WorkspaceContext
}

type WorkspaceFailedMsg struct {
	TeamName string
}
```

Handle in `App.Update`:
```go
	case WorkspaceReadyMsg:
		// ... store context, wire if first, mark ready in loading overlay ...

	case WorkspaceFailedMsg:
		a.SetWorkspaceFailed(msg.TeamName)
```

- [ ] **Step 5: Add timeout**

Add `LoadingTimeoutMsg` handling:
```go
	case LoadingTimeoutMsg:
		a.loading = false
```

Send it from Init or main.go.

- [ ] **Step 6: Build and test**

Run: `go build ./...`
Run: `go test ./...`

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/styles/styles.go cmd/slk/main.go
git commit -m "feat: loading overlay with spinner during workspace connection"
```

---

### Task 6: Final Verification and STATUS.md

- [ ] **Step 1: Full build and test**

Run: `go build ./...`
Run: `go test ./...`
Run: `go vet ./...`

- [ ] **Step 2: Build and test manually**

Run: `make build`

Verify:
1. App shows loading overlay with spinner during startup
2. Both workspaces appear in rail after loading
3. Press `1` to switch to first workspace — sidebar replaces with that workspace's channels
4. Press `2` to switch to second workspace
5. Press `Ctrl+w` to open workspace picker, type to filter, Enter to switch
6. Inactive workspace shows `*` unread indicator on rail when messages arrive

- [ ] **Step 3: Update STATUS.md**

Move "Multi-workspace switching at runtime" from "Not Yet Implemented" to "What's Working":

Add under "### Core":
```
- [x] Multi-workspace runtime switching (1-9 number keys + Ctrl+w picker)
- [x] All workspaces maintain live WebSocket connections
- [x] Loading overlay with spinner during startup
```

Remove from "Not Yet Implemented":
```
- [ ] Multi-workspace switching at runtime (workspace rail click)
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: complete multi-workspace runtime switching"
```
