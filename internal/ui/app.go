// internal/ui/app.go
package ui

import (
	"context"
	"image/color"
	"log"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/confirmprompt"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/thread"
	"github.com/gammons/slk/internal/ui/threadsview"
	"github.com/gammons/slk/internal/ui/workspace"
	"github.com/gammons/slk/internal/ui/workspacefinder"
)

type Panel int

const (
	PanelWorkspace Panel = iota
	PanelSidebar
	PanelMessages
	PanelThread
)

// editState tracks an in-progress message edit. When active, the
// channel or thread compose box is repurposed: its existing draft is
// stashed, the message text seeded, and Enter submits an
// EditMessageMsg instead of sending. Cancellation (Esc, channel
// switch, panel switch, etc.) restores the stashed draft.
type editState struct {
	active       bool
	channelID    string
	ts           string
	panel        Panel // PanelMessages or PanelThread
	stashedDraft string
}

// View identifies which "page" the message pane is displaying. The default
// is ViewChannels (a channel's message history); ViewThreads swaps the
// pane's contents for the involved-threads list.
type View int

const (
	ViewChannels View = iota
	ViewThreads
)

// Messages sent between components
type (
	ChannelSelectedMsg struct {
		ID   string
		Name string
		// Type is the channel type ("channel", "private", "dm",
		// "group_dm"); used to render a type-aware glyph in the
		// message-pane header and status bar. May be empty when
		// callers don't yet know the type \u2014 the UI then falls
		// back to a default `#` glyph.
		Type string
	}
	MessagesLoadedMsg struct {
		ChannelID  string
		Messages   []messages.MessageItem
		LastReadTS string
	}
	OlderMessagesLoadedMsg struct {
		ChannelID string
		Messages  []messages.MessageItem
	}
	NewMessageMsg struct {
		ChannelID string
		Message   messages.MessageItem
	}
	SendMessageMsg struct {
		ChannelID string
		Text      string
	}
	ThreadOpenedMsg struct {
		ChannelID string
		ThreadTS  string
		ParentMsg messages.MessageItem
	}
	ThreadRepliesLoadedMsg struct {
		ThreadTS string
		Replies  []messages.MessageItem
	}
	SendThreadReplyMsg struct {
		ChannelID string
		ThreadTS  string
		Text      string
	}
	ThreadReplySentMsg struct {
		ChannelID string
		ThreadTS  string
		Message   messages.MessageItem
	}
	// ThreadsViewActivatedMsg is dispatched when the user picks the
	// synthetic Threads sidebar row. The App switches the message pane to
	// the threads-list view and (re)fetches the involved-threads list.
	ThreadsViewActivatedMsg struct{}
	// ThreadsListLoadedMsg carries a freshly loaded list of involved-thread
	// summaries for the named workspace. The App ignores it if it doesn't
	// match the active team.
	ThreadsListLoadedMsg struct {
		TeamID    string
		Summaries []cache.ThreadSummary
	}
	// ThreadsListDirtyMsg is dispatched when something that could affect
	// the involved-threads list has changed (new message, mention, etc.)
	// and the list should be refetched. Ignored if not the active team.
	ThreadsListDirtyMsg struct {
		TeamID string
	}
	ConnectionStateMsg struct {
		State int // 0=connecting, 1=connected, 2=disconnected
	}
	ReactionAddedMsg struct {
		ChannelID string
		MessageTS string
		UserID    string
		Emoji     string
	}
	ReactionRemovedMsg struct {
		ChannelID string
		MessageTS string
		UserID    string
		Emoji     string
	}
	ReactionSentMsg struct {
		Err error
	}
	ChannelMarkedReadMsg struct {
		ChannelID string
	}
	DMNameResolvedMsg struct {
		ChannelID   string
		DisplayName string
	}
	WorkspaceSwitchedMsg struct {
		TeamID      string
		TeamName    string
		Theme       string // resolved theme name (per-workspace or global default)
		Channels    []sidebar.ChannelItem
		FinderItems []channelfinder.Item
		UserNames   map[string]string
		UserID      string
		CustomEmoji map[string]string
	}
	WorkspaceUnreadMsg struct {
		TeamID    string
		ChannelID string
	}
	WorkspaceReadyMsg struct {
		TeamID      string
		TeamName    string
		Channels    []sidebar.ChannelItem
		FinderItems []channelfinder.Item
		UserNames   map[string]string
		UserID      string
		CustomEmoji map[string]string
	}
	// CustomEmojisLoadedMsg is sent when a workspace's custom emoji list
	// finishes loading in the background, after WorkspaceReadyMsg has
	// already fired with whatever the goroutine had written so far. App
	// refreshes the active compose's emoji entry list if this matches the
	// active workspace.
	CustomEmojisLoadedMsg struct {
		TeamID      string
		CustomEmoji map[string]string
	}
	WorkspaceFailedMsg struct {
		TeamName string
	}
	// BrowseableChannelsLoadedMsg is sent after the background fetch of all
	// public channels (including ones the user has not joined) completes.
	// The Items have Joined=false; the App merges them into the channel
	// finder for the matching workspace.
	BrowseableChannelsLoadedMsg struct {
		TeamID string
		Items  []channelfinder.Item
	}
	SpinnerTickMsg    struct{}
	LoadingTimeoutMsg struct{}
	UserTypingMsg     struct {
		ChannelID   string
		UserID      string
		WorkspaceID string
	}
	TypingExpiredMsg struct{}
	PresenceChangeMsg struct {
		UserID   string
		Presence string
	}
)

type loadingEntry struct {
	TeamName string
	Status   string // "connecting", "ready", "failed"
}

// dragState captures an in-progress mouse drag for text selection. The
// FSM lives in App.Update: MouseClickMsg seeds it, MouseMotionMsg
// extends, MouseReleaseMsg finalizes (or clears it on a plain click).
//
// autoScrollActive is reserved for Task 9 (edge auto-scroll) and is
// declared here so that future task can wire it in without re-touching
// this struct definition.
type dragState struct {
	panel            Panel // PanelMessages or PanelThread; PanelWorkspace == idle
	pressX, pressY   int
	lastX, lastY     int
	moved            bool
	autoScrollActive bool
}

// autoScrollTickMsg is dispatched by tea.Tick while a drag is held near
// the top or bottom edge of a pane. Each tick scrolls the pane one line
// in the indicated direction, extends the selection to the new lastY,
// and (if still at an edge) schedules the next tick. The loop self-
// terminates when the cursor leaves the edge or the drag ends.
type autoScrollTickMsg struct{}

// panelCache stores the fully-wrapped (border + exactSize) output of a panel
// keyed on a tuple of inputs that affect its rendering. A cache hit returns
// the previous frame's string verbatim; a miss recomputes and stores.
//
// layoutKey is a free-form int64 callers can use to encode focus state,
// mode, theme version, and layout-toggle bits as a single comparable value.
type panelCache struct {
	output       string
	panelVersion int64
	width        int
	height       int
	layoutKey    int64
	valid        bool
}

func (c *panelCache) hit(panelVersion int64, width, height int, layoutKey int64) bool {
	return c.valid &&
		c.panelVersion == panelVersion &&
		c.width == width &&
		c.height == height &&
		c.layoutKey == layoutKey
}

func (c *panelCache) store(out string, panelVersion int64, width, height int, layoutKey int64) {
	c.output = out
	c.panelVersion = panelVersion
	c.width = width
	c.height = height
	c.layoutKey = layoutKey
	c.valid = true
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// mixVersions combines two monotonic int64 counters into one. Used when a
// panel cache must invalidate on either of two underlying versions changing.
// The mix isn't a hash -- it's just any function that yields a unique value
// per (a, b) pair within practical ranges, which (a*small_prime ^ b) does for
// counters that increment by 1 over a session.
func mixVersions(a, b int64) int64 {
	return a*1_000_003 ^ b
}

// SwitchWorkspaceFunc is called to switch the active workspace.
type SwitchWorkspaceFunc func(teamID string) tea.Msg

// ChannelFetchFunc is called when the user selects a channel.
type ChannelFetchFunc func(channelID, channelName string) tea.Msg

// OlderMessagesFetchFunc is called when the user scrolls to the top of a channel.
type OlderMessagesFetchFunc func(channelID, oldestTS string) tea.Msg

// MessageSendFunc is called when the user sends a message. Returns a tea.Msg with the result.
type MessageSendFunc func(channelID, text string) tea.Msg

// MessageSentMsg is returned after a message is successfully sent.
type MessageSentMsg struct {
	ChannelID string
	Message   messages.MessageItem
}

// EditMessageMsg is emitted when the user submits an edit. App.Update
// invokes the configured messageEditor and converts the result to
// MessageEditedMsg.
type EditMessageMsg struct {
	ChannelID string
	TS        string
	NewText   string
}

// DeleteMessageMsg is emitted when the user confirms a delete.
type DeleteMessageMsg struct {
	ChannelID string
	TS        string
}

// MessageEditedMsg carries the result of the chat.update API call.
type MessageEditedMsg struct {
	ChannelID string
	TS        string
	Err       error
}

// MessageDeletedMsg carries the result of the chat.delete API call.
type MessageDeletedMsg struct {
	ChannelID string
	TS        string
	Err       error
}

// WSMessageDeletedMsg is dispatched by the RTM event handler when a
// message_deleted event arrives. App.Update handles it by removing the
// message from both panes and the cache.
type WSMessageDeletedMsg struct {
	ChannelID string
	TS        string
}

// MessageEditFunc performs the chat.update API call. Returns a tea.Msg
// (typically MessageEditedMsg) describing the result.
type MessageEditFunc func(channelID, ts, newText string) tea.Msg

// MessageDeleteFunc performs the chat.delete API call. Returns a tea.Msg
// (typically MessageDeletedMsg) describing the result.
type MessageDeleteFunc func(channelID, ts string) tea.Msg

// ThreadFetchFunc is called when the user opens a thread.
type ThreadFetchFunc func(channelID, threadTS string) tea.Msg

// ThreadReplySendFunc is called when the user sends a thread reply.
type ThreadReplySendFunc func(channelID, threadTS, text string) tea.Msg

// ThreadsListFetchFunc loads the involved-threads list for a workspace.
// Returns the resulting tea.Msg (typically ThreadsListLoadedMsg).
type ThreadsListFetchFunc func(teamID string) tea.Msg

type ReactionAddFunc func(channelID, messageTS, emoji string) error
type ReactionRemoveFunc func(channelID, messageTS, emoji string) error

// PermalinkFetchFunc is called to fetch the Slack permalink for a message.
// For thread replies, pass the reply's ts; Slack returns a thread-aware URL.
type PermalinkFetchFunc func(ctx context.Context, channelID, ts string) (string, error)
type FrecentLoadFunc func(limit int) []reactionpicker.EmojiEntry
type FrecentRecordFunc func(emoji string)

// TypingSendFunc is called to broadcast a typing indicator.
type TypingSendFunc func(channelID string)

// JoinChannelFunc is called to join a public channel by ID. Returns a tea.Msg
// describing the result (typically ChannelJoinedMsg or ChannelJoinFailedMsg).
type JoinChannelFunc func(channelID, channelName string) tea.Msg

// ChannelJoinedMsg is sent after the user successfully joins a channel from
// the channel finder. The App responds by adding the channel to the sidebar
// (so it appears in the user's regular channel list), marking it as joined in
// the finder, and switching to it.
type ChannelJoinedMsg struct {
	ID   string
	Name string
}

// ChannelJoinFailedMsg is sent when the join API call fails.
type ChannelJoinFailedMsg struct {
	ID   string
	Name string
	Err  error
}

type App struct {
	// Sub-models
	workspaceRail   workspace.Model
	sidebar         sidebar.Model
	messagepane     messages.Model
	compose         compose.Model
	statusbar       statusbar.Model
	channelFinder   channelfinder.Model
	workspaceFinder workspacefinder.Model
	themeSwitcher   themeswitcher.Model
	threadPanel     *thread.Model
	threadCompose   compose.Model
	threadsView     threadsview.Model

	// State
	mode           Mode
	focusedPanel   Panel
	sidebarVisible bool
	threadVisible  bool
	view           View
	width          int
	height         int
	keys           KeyMap

	// Cached layout widths for mouse hit-testing
	layoutRailWidth    int
	layoutSidebarEnd   int // railWidth + sidebarWidth + sidebarBorder
	layoutMsgEnd       int // layoutSidebarEnd + msgWidth + msgBorder
	layoutThreadEnd    int // layoutMsgEnd + threadWidth + threadBorder
	// Cached pane content heights, used for page-up/down distance calculations.
	layoutMsgHeight     int
	layoutSidebarHeight int
	layoutThreadHeight  int

	// Per-panel render caches. Each panel exposes Version() that increments
	// on any state change that could alter its View() output. The App caches
	// the FULLY-WRAPPED panel output (panel.View + border + exactSize) keyed
	// on (panelVersion, width, height, layoutKey). On compose keystrokes,
	// only compose's version changes so all the other panels' wrapped
	// outputs are reused -- saving the bulk of the per-keystroke render cost.
	panelCacheRail     panelCache
	panelCacheSidebar  panelCache
	panelCacheMsgPanel panelCache
	panelCacheThread   panelCache
	panelCacheStatus   panelCache

	// Current context
	activeChannelID string
	activeTeamID    string // workspace whose data is currently loaded into the side panels

	// Callbacks
	channelFetcher       ChannelFetchFunc
	olderMessagesFetcher OlderMessagesFetchFunc
	messageSender        MessageSendFunc
	messageEditor        MessageEditFunc
	messageDeleter       MessageDeleteFunc
	threadFetcher        ThreadFetchFunc
	threadReplySender    ThreadReplySendFunc
	channelJoiner        JoinChannelFunc
	threadsListFetcher   ThreadsListFetchFunc
	threadsDirtyDebounce time.Duration
	fetchingOlder        bool

	// Cached user-id -> display-name map (mirror of what SetUserNames
	// last received). Used by openSelectedThreadCmd to populate the
	// thread panel parent's UserName without round-tripping through any
	// sub-component's API.
	userNames map[string]string

	// Last (channelID, threadTS) auto-opened from the threads view.
	// openSelectedThreadCmd compares against these to dedup repeat calls
	// (j/k keystrokes and ThreadsListLoadedMsg refreshes both fire
	// openSelectedThreadCmd; without dedup we'd hammer the Slack API and
	// clobber the right thread panel mid-read). Cleared whenever the
	// user leaves the threads view (ChannelSelectedMsg, CloseThread,
	// workspace switch).
	lastOpenedChannelID string
	lastOpenedThreadTS  string

	// Reaction picker
	reactionPicker   *reactionpicker.Model
	confirmPrompt    *confirmprompt.Model
	reactionAddFn    ReactionAddFunc
	reactionRemoveFn ReactionRemoveFunc
	frecentLoadFn    FrecentLoadFunc
	frecentRecordFn  FrecentRecordFunc
	currentUserID    string

	// editing tracks in-progress message edit state. See editState.
	editing editState

	// Permalink copying
	permalinkFetchFn PermalinkFetchFunc

	// Workspace switching
	workspaceSwitcher SwitchWorkspaceFunc
	workspaceItems    []workspace.WorkspaceItem // cached for lookup

	// Theme switching
	themeSaveFn    func(name string, scope themeswitcher.ThemeScope)
	themeOverrides config.Theme

	// Typing indicators
	typingUsers    map[string]map[string]time.Time // channelID -> userID -> expiresAt
	typingTickerOn bool
	typingEnabled  bool

	// Outbound typing
	typingSendFn   TypingSendFunc
	lastTypingSent time.Time

	// Loading overlay
	loading       bool
	loadingStates []loadingEntry
	spinnerFrame  int

	// Mouse drag selection FSM (set by MouseClickMsg, advanced by
	// MouseMotionMsg, drained by MouseReleaseMsg).
	drag dragState
}

func NewApp() *App {
	app := &App{
		workspaceRail:        workspace.New(nil, 0),
		sidebar:              sidebar.New(nil),
		messagepane:          messages.New(nil, ""),
		compose:              compose.New(""),
		statusbar:            statusbar.New(),
		channelFinder:        channelfinder.New(),
		workspaceFinder:      workspacefinder.New(),
		themeSwitcher:        themeswitcher.New(),
		threadPanel:          thread.New(),
		threadCompose:        compose.New("thread"),
		threadsView:          threadsview.New(nil, ""),
		reactionPicker:       reactionpicker.New(),
		confirmPrompt:        confirmprompt.New(),
		mode:                 ModeNormal,
		focusedPanel:         PanelSidebar,
		sidebarVisible:       true,
		view:                 ViewChannels,
		keys:                 DefaultKeyMap(),
		typingUsers:          make(map[string]map[string]time.Time),
		threadsDirtyDebounce: 150 * time.Millisecond,
		userNames:            map[string]string{},
	}
	// Seed the picker with built-in emojis so the autocomplete works even
	// before the first workspace finishes loading customs.
	app.compose.SetEmojiEntries(emoji.BuildEntries(nil))
	app.threadCompose.SetEmojiEntries(emoji.BuildEntries(nil))
	return app
}

func (a *App) Init() tea.Cmd {
	if a.loading {
		return tea.Batch(
			tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			}),
			tea.Tick(15*time.Second, func(time.Time) tea.Msg {
				return LoadingTimeoutMsg{}
			}),
		)
	}
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil

	case tea.KeyMsg:
		cmd := a.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseWheelMsg:
		if a.loading {
			break
		}
		// Translate the wheel direction into a scroll delta. Three lines per
		// wheel notch matches typical terminal-emulator behavior.
		const wheelLines = 3
		dir := 0
		switch msg.Button {
		case tea.MouseWheelUp:
			dir = -wheelLines
		case tea.MouseWheelDown:
			dir = wheelLines
		default:
			break
		}
		if dir == 0 {
			break
		}
		x := msg.X
		switch {
		case x < a.layoutRailWidth:
			// Workspace rail: nothing scrollable yet.
		case a.sidebarVisible && x < a.layoutSidebarEnd:
			if dir < 0 {
				a.sidebar.ScrollUp(-dir)
			} else {
				a.sidebar.ScrollDown(dir)
			}
		case x < a.layoutMsgEnd:
			if a.view == ViewThreads {
				if dir < 0 {
					a.threadsView.ScrollUp(-dir)
				} else {
					a.threadsView.ScrollDown(dir)
				}
			} else {
				if dir < 0 {
					a.messagepane.ScrollUp(-dir)
				} else {
					a.messagepane.ScrollDown(dir)
				}
			}
		case a.threadVisible && x < a.layoutThreadEnd:
			// Thread panel: scroll if it exposes the methods.
			if dir < 0 {
				a.threadPanel.ScrollUp(-dir)
			} else {
				a.threadPanel.ScrollDown(dir)
			}
		}

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
						return ChannelSelectedMsg{ID: item.ID, Name: item.Name, Type: item.Type}
					}
				}
				// ClickAt returns ok=false for the synthetic Threads
				// row; if the click landed there (sidebar updates its
				// own selection state), activate the threads view.
				if a.sidebar.IsThreadsSelected() {
					return a, func() tea.Msg { return ThreadsViewActivatedMsg{} }
				}
			}
		} else if x < a.layoutMsgEnd {
			a.focusedPanel = PanelMessages
			panel, px, py, ok := a.panelAt(msg.X, msg.Y)
			if ok && panel == PanelMessages && py >= 0 {
				a.drag = dragState{panel: PanelMessages, pressX: px, pressY: py, lastX: px, lastY: py}
				a.messagepane.BeginSelectionAt(py, px)
				a.messagepane.ClickAt(py)
			}
		} else if a.threadVisible && x < a.layoutThreadEnd {
			a.focusedPanel = PanelThread
			panel, px, py, ok := a.panelAt(msg.X, msg.Y)
			if ok && panel == PanelThread && py >= 0 {
				a.drag = dragState{panel: PanelThread, pressX: px, pressY: py, lastX: px, lastY: py}
				a.threadPanel.BeginSelectionAt(py, px)
				a.threadPanel.ClickAt(py)
			}
		}

	case tea.MouseMotionMsg:
		if a.loading {
			break
		}
		if msg.Button != tea.MouseLeft {
			break
		}
		if a.drag.panel != PanelMessages && a.drag.panel != PanelThread {
			break
		}
		panel, px, py, _ := a.panelAt(msg.X, msg.Y)
		// Clamp to the originating pane: if the cursor leaves the pane,
		// pin extension at the last known coordinates inside it.
		if panel != a.drag.panel {
			px, py = a.drag.lastX, a.drag.lastY
		}
		a.drag.lastX, a.drag.lastY = px, py
		a.drag.moved = true
		switch a.drag.panel {
		case PanelMessages:
			a.messagepane.ExtendSelectionAt(py, px)
		case PanelThread:
			a.threadPanel.ExtendSelectionAt(py, px)
		}
		// If the cursor is at the top/bottom edge of the originating pane,
		// schedule an auto-scroll tick. The autoScrollActive gate ensures
		// only one tick is in-flight at a time -- otherwise every motion
		// event would queue another timer.
		var hint int
		switch a.drag.panel {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(py)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(py)
		}
		if hint != 0 && !a.drag.autoScrollActive {
			a.drag.autoScrollActive = true
			cmds = append(cmds, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
				return autoScrollTickMsg{}
			}))
		}

	case autoScrollTickMsg:
		// If the drag ended (release clears a.drag), self-terminate.
		if a.drag.panel != PanelMessages && a.drag.panel != PanelThread {
			a.drag.autoScrollActive = false
			break
		}
		var hint int
		switch a.drag.panel {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(a.drag.lastY)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(a.drag.lastY)
		}
		if hint == 0 {
			// Cursor left the edge -- stop ticking. Re-entering the edge
			// in a future motion event will re-arm the loop.
			a.drag.autoScrollActive = false
			break
		}
		switch a.drag.panel {
		case PanelMessages:
			if hint < 0 {
				a.messagepane.ScrollUp(1)
			} else {
				a.messagepane.ScrollDown(1)
			}
			a.messagepane.ExtendSelectionAt(a.drag.lastY, a.drag.lastX)
		case PanelThread:
			if hint < 0 {
				a.threadPanel.ScrollUp(1)
			} else {
				a.threadPanel.ScrollDown(1)
			}
			a.threadPanel.ExtendSelectionAt(a.drag.lastY, a.drag.lastX)
		}
		// Schedule the next tick. autoScrollActive remains true.
		cmds = append(cmds, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
			return autoScrollTickMsg{}
		}))

	case tea.PasteMsg:
		// Bracketed-paste from the terminal. Forward the paste to the
		// active compose when in insert mode. In other modes paste is
		// ignored — there's no reasonable destination (the messages /
		// thread panes are read-only, and the various finders use
		// per-keystroke filtering rather than a paste-friendly buffer).
		if a.mode != ModeInsert {
			break
		}
		if a.focusedPanel == PanelThread && a.threadVisible {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		} else {
			var cmd tea.Cmd
			a.compose, cmd = a.compose.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseReleaseMsg:
		if a.drag.panel != PanelMessages && a.drag.panel != PanelThread {
			break
		}
		moved := a.drag.moved
		panel := a.drag.panel
		a.drag = dragState{}
		if !moved {
			// Plain click — drop any previous pinned selection.
			switch panel {
			case PanelMessages:
				a.messagepane.ClearSelection()
			case PanelThread:
				a.threadPanel.ClearSelection()
			}
			break
		}
		var (
			text string
			ok   bool
		)
		switch panel {
		case PanelMessages:
			text, ok = a.messagepane.EndSelection()
		case PanelThread:
			text, ok = a.threadPanel.EndSelection()
		}
		if ok && text != "" {
			n := len([]rune(text))
			cmds = append(cmds, tea.SetClipboard(text))
			cmds = append(cmds, func() tea.Msg { return statusbar.CopiedMsg{N: n} })
		}

	case statusbar.CopiedMsg:
		a.statusbar.ShowCopied(msg.N)
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.CopiedClearMsg:
		a.statusbar.ClearCopied()

	case statusbar.PermalinkCopiedMsg:
		a.statusbar.SetToast("Copied permalink")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.PermalinkCopyFailedMsg:
		a.statusbar.SetToast("Failed to copy link")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.EditFailedMsg:
		a.statusbar.SetToast("Edit failed: " + truncateReason(msg.Reason, 40))
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case editEmptyToastMsg:
		a.statusbar.SetToast("Edit must have text (use D to delete)")
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.DeleteFailedMsg:
		a.statusbar.SetToast("Delete failed: " + truncateReason(msg.Reason, 40))
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.EditNotOwnMsg:
		a.statusbar.SetToast("Can only edit your own messages")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.DeleteNotOwnMsg:
		a.statusbar.SetToast("Can only delete your own messages")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case ChannelSelectedMsg:
		a.cancelEdit()
		// Picking a channel always exits the Threads view.
		a.view = ViewChannels
		a.lastOpenedChannelID = ""
		a.lastOpenedThreadTS = ""
		// Close thread panel when switching channels
		a.CloseThread()
		a.clearSelections()
		a.activeChannelID = msg.ID
		a.lastTypingSent = time.Time{} // reset typing throttle for new channel
		// Tell the sidebar which channel is active so the staleness
		// filter never hides it out from under the user.
		a.sidebar.SetActiveChannelID(msg.ID)
		a.messagepane.SetChannel(msg.Name, "")
		a.messagepane.SetChannelType(msg.Type)
		a.messagepane.SetLoading(true)
		a.messagepane.SetMessages(nil) // clear while loading
		a.compose.SetChannel(msg.Name)
		a.statusbar.SetChannel(msg.Name)
		a.statusbar.SetChannelType(msg.Type)
		// Fetch messages for the newly selected channel
		if a.channelFetcher != nil {
			fetcher := a.channelFetcher
			chID, chName := msg.ID, msg.Name
			cmds = append(cmds, func() tea.Msg {
				return fetcher(chID, chName)
			})
		}

	case MessagesLoadedMsg:
		if msg.ChannelID == a.activeChannelID {
			a.messagepane.SetLoading(false)
			a.messagepane.SetLastReadTS(msg.LastReadTS)
			a.messagepane.SetMessages(msg.Messages)
		}

	case OlderMessagesLoadedMsg:
		if msg.ChannelID == a.activeChannelID {
			a.fetchingOlder = false
			a.messagepane.SetLoading(false)
			a.messagepane.PrependMessages(msg.Messages)
		}

	case NewMessageMsg:
		if msg.Message.IsEdited {
			// Edit echo: update existing message in place rather than appending.
			a.messagepane.UpdateMessageInPlace(msg.Message.TS, msg.Message.Text)
			a.threadPanel.UpdateMessageInPlace(msg.Message.TS, msg.Message.Text)
			a.threadPanel.UpdateParentInPlace(msg.Message.TS, msg.Message.Text)
			break
		}
		if msg.ChannelID == a.activeChannelID {
			// Route thread replies to the thread panel if it matches the open thread
			if a.threadVisible && msg.Message.ThreadTS == a.threadPanel.ThreadTS() {
				a.threadPanel.AddReply(msg.Message)
			}
			// Always add to main pane if it's a top-level message (no ThreadTS or is the parent)
			if msg.Message.ThreadTS == "" || msg.Message.ThreadTS == msg.Message.TS {
				a.messagepane.AppendMessage(msg.Message)
			}
			// Update reply count on parent message when a thread reply arrives
			if msg.Message.ThreadTS != "" && msg.Message.ThreadTS != msg.Message.TS {
				a.messagepane.IncrementReplyCount(msg.Message.ThreadTS)
			}
		}
		// A thread reply (regardless of channel) may have changed the
		// involved-threads list — schedule a debounced re-query so a burst
		// of replies coalesces into a single fetch.
		if msg.Message.ThreadTS != "" {
			if c := a.scheduleThreadsDirty(); c != nil {
				cmds = append(cmds, c)
			}
		}

	case SendMessageMsg:
		if a.messageSender != nil {
			sender := a.messageSender
			chID, text := msg.ChannelID, msg.Text
			cmds = append(cmds, func() tea.Msg {
				return sender(chID, text)
			})
		}

	case MessageSentMsg:
		// Message will arrive via RTM WebSocket event (NewMessageMsg).
		// Don't append here to avoid doubling.

	case EditMessageMsg:
		if a.messageEditor != nil {
			editor := a.messageEditor
			chID, ts, text := msg.ChannelID, msg.TS, msg.NewText
			cmds = append(cmds, func() tea.Msg {
				return editor(chID, ts, text)
			})
		}

	case MessageEditedMsg:
		// Only exit edit mode if this result matches the edit that's
		// currently in flight. A stale result from a previously
		// cancelled or replaced edit must not clobber the current one.
		if a.editing.active &&
			a.editing.channelID == msg.ChannelID &&
			a.editing.ts == msg.TS {
			a.cancelEdit()
		}
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return statusbar.EditFailedMsg{Reason: msg.Err.Error()}
			})
		}

	case DeleteMessageMsg:
		if a.messageDeleter != nil {
			deleter := a.messageDeleter
			chID, ts := msg.ChannelID, msg.TS
			cmds = append(cmds, func() tea.Msg {
				return deleter(chID, ts)
			})
		}

	case MessageDeletedMsg:
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return statusbar.DeleteFailedMsg{Reason: msg.Err.Error()}
			})
		}

	case ThreadRepliesLoadedMsg:
		if a.threadVisible && msg.ThreadTS == a.threadPanel.ThreadTS() {
			a.threadPanel.SetThread(a.threadPanel.ParentMsg(), msg.Replies, a.threadPanel.ChannelID(), msg.ThreadTS)
		}

	case ThreadsViewActivatedMsg:
		a.view = ViewThreads
		a.focusedPanel = PanelMessages
		if a.threadsListFetcher != nil && a.activeTeamID != "" {
			fetcher := a.threadsListFetcher
			team := a.activeTeamID
			cmds = append(cmds, func() tea.Msg { return fetcher(team) })
		}
		if cmd := a.openSelectedThreadCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case ThreadsListLoadedMsg:
		if msg.TeamID == a.activeTeamID {
			a.threadsView.SetSummaries(msg.Summaries)
			a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
			if a.view == ViewThreads {
				if cmd := a.openSelectedThreadCmd(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

	case ThreadsListDirtyMsg:
		if msg.TeamID == a.activeTeamID && a.threadsListFetcher != nil {
			fetcher := a.threadsListFetcher
			team := a.activeTeamID
			cmds = append(cmds, func() tea.Msg { return fetcher(team) })
		}

	case SendThreadReplyMsg:
		if a.threadReplySender != nil {
			sender := a.threadReplySender
			chID, ts, text := msg.ChannelID, msg.ThreadTS, msg.Text
			cmds = append(cmds, func() tea.Msg {
				return sender(chID, ts, text)
			})
		}

	case ThreadReplySentMsg:
		// Reply will arrive via RTM WebSocket event (NewMessageMsg).
		// Don't append here to avoid doubling.

	case ConnectionStateMsg:
		a.statusbar.SetConnectionState(statusbar.ConnectionState(msg.State))

	case WSMessageDeletedMsg:
		a.messagepane.RemoveMessageByTS(msg.TS)
		a.threadPanel.RemoveMessageByTS(msg.TS)
		// If the deleted message was the open thread's parent, close
		// the thread panel — Slack deletes the entire thread when the
		// parent is deleted.
		if a.threadVisible && a.threadPanel.ThreadTS() == msg.TS {
			a.CloseThread()
		}

	case ReactionAddedMsg:
		// Skip WebSocket echo of our own optimistic updates.
		// When we add/remove a reaction, we update the UI immediately.
		// The WebSocket echo arrives later with our own userID — ignore it.
		if msg.UserID != a.currentUserID {
			a.updateReactionOnMessage(msg.ChannelID, msg.MessageTS, msg.Emoji, msg.UserID, false)
		}

	case ReactionRemovedMsg:
		if msg.UserID != a.currentUserID {
			a.updateReactionOnMessage(msg.ChannelID, msg.MessageTS, msg.Emoji, msg.UserID, true)
		}

	case ReactionSentMsg:
		// API call completed. If err, optimistic update stays (could add status bar error later).

	case ChannelMarkedReadMsg:
		a.sidebar.ClearUnread(msg.ChannelID)

	case DMNameResolvedMsg:
		items := a.sidebar.Items()
		for i := range items {
			if items[i].ID == msg.ChannelID {
				items[i].Name = msg.DisplayName
				break
			}
		}
		a.sidebar.SetItems(items)

	case WorkspaceSwitchedMsg:
		a.cancelEdit()
		// Always land in ViewChannels and drop any per-workspace
		// threads-view state so stale summaries / unread badges from the
		// previous workspace can't leak in. Reset sidebar selection back
		// to the synthetic Threads row for a "fresh" feel on each
		// workspace switch.
		a.sidebar.SelectThreadsRow()
		a.view = ViewChannels
		a.threadsView.SetSummaries(nil)
		a.sidebar.SetThreadsUnreadCount(0)
		a.lastOpenedChannelID = ""
		a.lastOpenedThreadTS = ""
		a.CloseThread()
		a.clearSelections()
		a.compose.Reset()
		a.messagepane.SetMessages(nil)
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.sidebar.SetItems(msg.Channels)
		a.channelFinder.SetItems(msg.FinderItems)
		a.SetUserNames(msg.UserNames)
		a.SetCustomEmoji(msg.CustomEmoji)
		a.currentUserID = msg.UserID
		a.activeTeamID = msg.TeamID
		// Apply per-workspace theme. Must run on Update goroutine so the
		// component cache invalidations and compose-style refreshes below
		// take effect on the next render.
		if msg.Theme != "" {
			styles.Apply(msg.Theme, a.themeOverrides)
			a.messagepane.InvalidateCache()
			a.threadPanel.InvalidateCache()
			a.sidebar.InvalidateCache()
			a.compose.RefreshStyles()
			a.threadCompose.RefreshStyles()
		}
		a.workspaceRail.SelectByID(msg.TeamID)
		// Load first channel
		if len(msg.Channels) > 0 {
			first := msg.Channels[0]
			cmds = append(cmds, func() tea.Msg {
				return ChannelSelectedMsg{ID: first.ID, Name: first.Name, Type: first.Type}
			})
		}
		// Kick off an initial threads-list fetch so the sidebar Threads
		// row badge populates before the user opens the view.
		if a.threadsListFetcher != nil {
			fetcher := a.threadsListFetcher
			team := msg.TeamID
			cmds = append(cmds, func() tea.Msg { return fetcher(team) })
		}

	case WorkspaceUnreadMsg:
		a.workspaceRail.SetUnread(msg.TeamID, true)

	case SpinnerTickMsg:
		if a.loading {
			a.spinnerFrame = (a.spinnerFrame + 1) % 10
			return a, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			})
		}

	case LoadingTimeoutMsg:
		if a.loading {
			for i := range a.loadingStates {
				if a.loadingStates[i].Status == "connecting" {
					a.loadingStates[i].Status = "failed"
				}
			}
			a.loading = false
		}

	case WorkspaceReadyMsg:
		a.MarkWorkspaceReady(msg.TeamName)
		// If this is the first workspace, set it up as active. Threads-view
		// state reset only happens here — background workspaces becoming
		// ready must NOT clobber the active workspace's loaded summaries,
		// unread badge, or current view.
		if a.activeChannelID == "" {
			a.view = ViewChannels
			a.threadsView.SetSummaries(nil)
			a.sidebar.SetThreadsUnreadCount(0)
			a.lastOpenedChannelID = ""
			a.lastOpenedThreadTS = ""
			a.sidebar.SetItems(msg.Channels)
			a.channelFinder.SetItems(msg.FinderItems)
			a.SetUserNames(msg.UserNames)
			a.SetCustomEmoji(msg.CustomEmoji)
			a.currentUserID = msg.UserID
			a.activeTeamID = msg.TeamID
			a.workspaceRail.SelectByID(msg.TeamID)
			if len(msg.Channels) > 0 {
				first := msg.Channels[0]
				cmds = append(cmds, func() tea.Msg {
					return ChannelSelectedMsg{ID: first.ID, Name: first.Name, Type: first.Type}
				})
			}
		}
		// Initial threads-list fetch fires for every workspace as it
		// becomes ready; the result is gated by ThreadsListLoadedMsg's
		// TeamID == activeTeamID check, so background fetches are
		// dropped without affecting the active sidebar.
		if a.threadsListFetcher != nil {
			fetcher := a.threadsListFetcher
			team := msg.TeamID
			cmds = append(cmds, func() tea.Msg { return fetcher(team) })
		}

	case CustomEmojisLoadedMsg:
		if msg.TeamID == a.activeTeamID {
			a.SetCustomEmoji(msg.CustomEmoji)
		}

	case ChannelJoinedMsg:
		// Add the newly-joined channel to the sidebar (so it shows up in the
		// regular list) and mark it joined in the finder. Then dispatch a
		// ChannelSelectedMsg to open it.
		newItem := sidebar.ChannelItem{
			ID:   msg.ID,
			Name: msg.Name,
			Type: "channel",
		}
		items := a.sidebar.Items()
		// Avoid double-add if a presence/list event raced ahead.
		alreadyInSidebar := false
		for _, it := range items {
			if it.ID == msg.ID {
				alreadyInSidebar = true
				break
			}
		}
		if !alreadyInSidebar {
			items = append(items, newItem)
			a.sidebar.SetItems(items)
		}
		a.channelFinder.MarkJoined(msg.ID)
		a.sidebar.SelectByID(msg.ID)
		cmds = append(cmds, func() tea.Msg {
			// ChannelJoinedMsg only fires for public channels via the
			// channel finder; type is always "channel".
			return ChannelSelectedMsg{ID: msg.ID, Name: msg.Name, Type: "channel"}
		})

	case ChannelJoinFailedMsg:
		// Nothing fancy yet -- could surface a status-bar toast in future.
		log.Printf("warning: failed to join channel %s: %v", msg.Name, msg.Err)

	case BrowseableChannelsLoadedMsg:
		// Only apply to the channel finder if this matches the workspace
		// whose items are currently loaded. Per-workspace browseable items
		// are kept in main.go's WorkspaceContext for any future switch.
		if msg.TeamID == a.activeTeamID {
			a.channelFinder.SetBrowseable(msg.Items)
		}

	case WorkspaceFailedMsg:
		a.MarkWorkspaceFailed(msg.TeamName)

	case UserTypingMsg:
		if !a.typingEnabled {
			return a, nil
		}
		a.addTypingUser(msg.ChannelID, msg.UserID)
		if !a.typingTickerOn {
			a.typingTickerOn = true
			cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return TypingExpiredMsg{}
			}))
		}

	case PresenceChangeMsg:
		a.sidebar.UpdatePresenceByUser(msg.UserID, msg.Presence)

	case TypingExpiredMsg:
		a.expireTypingUsers()
		// Continue ticking if there are still active typers
		hasTypers := len(a.typingUsers) > 0
		a.typingTickerOn = hasTypers
		if hasTypers {
			cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return TypingExpiredMsg{}
			}))
		}
	}

	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Always handle quit
	if key.Matches(msg, a.keys.Quit) {
		return tea.Quit
	}

	if a.loading {
		return nil
	}

	// Mode-specific handling
	switch a.mode {
	case ModeInsert:
		return a.handleInsertMode(msg)
	case ModeCommand:
		return a.handleCommandMode(msg)
	case ModeChannelFinder:
		return a.handleChannelFinderMode(msg)
	case ModeReactionPicker:
		return a.handleReactionPickerMode(msg)
	case ModeConfirm:
		return a.handleConfirmMode(msg)
	case ModeWorkspaceFinder:
		return a.handleWorkspaceFinderMode(msg)
	case ModeThemeSwitcher:
		return a.handleThemeSwitcherMode(msg)
	default:
		return a.handleNormalMode(msg)
	}
}

func (a *App) handleNormalMode(msg tea.KeyMsg) tea.Cmd {
	// Reaction-nav sub-state (intercept before normal keys)
	if a.focusedPanel == PanelMessages && a.messagepane.ReactionNavActive() {
		return a.handleReactionNav(msg)
	}
	if a.focusedPanel == PanelThread && a.threadPanel.ReactionNavActive() {
		return a.handleThreadReactionNav(msg)
	}

	switch {
	case key.Matches(msg, a.keys.InsertMode):
		a.SetMode(ModeInsert)
		// In the Threads view there is no main compose box — the only
		// way to type is into the right-side thread panel's compose.
		// Force focus there even when the threads list itself was the
		// focused panel.
		if a.focusedPanel == PanelThread || (a.view == ViewThreads && a.threadVisible) {
			a.focusedPanel = PanelThread
			return a.threadCompose.Focus()
		}
		a.focusedPanel = PanelMessages
		return a.compose.Focus()

	case key.Matches(msg, a.keys.Escape):
		a.cancelEdit()
		a.SetMode(ModeNormal)
		a.compose.Blur()
		if a.threadVisible {
			a.CloseThread()
		}

	case key.Matches(msg, a.keys.Tab):
		a.FocusNext()

	case key.Matches(msg, a.keys.ShiftTab):
		a.FocusPrev()

	case key.Matches(msg, a.keys.ToggleSidebar):
		a.ToggleSidebar()

	case key.Matches(msg, a.keys.ToggleThread):
		a.ToggleThread()

	case key.Matches(msg, a.keys.Down):
		if cmd := a.handleDown(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.Up):
		if cmd := a.handleUp(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.Left):
		a.FocusPrev()

	case key.Matches(msg, a.keys.Right):
		a.FocusNext()

	case key.Matches(msg, a.keys.Enter):
		return a.handleEnter()

	case key.Matches(msg, a.keys.Bottom):
		if cmd := a.handleGoToBottom(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.PageUp):
		a.scrollFocusedPanel(-a.pageSize())

	case key.Matches(msg, a.keys.PageDown):
		a.scrollFocusedPanel(a.pageSize())

	case key.Matches(msg, a.keys.HalfPageUp):
		a.scrollFocusedPanel(-a.halfPageSize())

	case key.Matches(msg, a.keys.HalfPageDown):
		a.scrollFocusedPanel(a.halfPageSize())

	case key.Matches(msg, a.keys.WorkspaceFinder):
		a.workspaceFinder.Open()
		a.SetMode(ModeWorkspaceFinder)

	case key.Matches(msg, a.keys.ThemeSwitcher):
		// Per-workspace scope. Header text shows the current workspace name.
		header := "Theme for " + a.activeTeamName()
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeWorkspace, header)
		a.SetMode(ModeThemeSwitcher)
		return nil
	case key.Matches(msg, a.keys.ThemeSwitcherGlobal):
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeGlobal, "Default theme for new workspaces")
		a.SetMode(ModeThemeSwitcher)
		return nil

	case key.Matches(msg, a.keys.FuzzyFinder) || key.Matches(msg, a.keys.FuzzyFinderAlt):
		a.channelFinder.Open()
		a.SetMode(ModeChannelFinder)

	case key.Matches(msg, a.keys.Reaction):
		if a.focusedPanel == PanelMessages {
			return a.openPickerFromMessage()
		} else if a.focusedPanel == PanelThread {
			return a.openPickerFromThread()
		}

	case key.Matches(msg, a.keys.ReactionNav):
		if a.focusedPanel == PanelMessages {
			a.messagepane.EnterReactionNav()
		} else if a.focusedPanel == PanelThread {
			a.threadPanel.EnterReactionNav()
		}

	case key.Matches(msg, a.keys.CopyPermalink):
		return a.copyPermalinkOfSelected()

	case key.Matches(msg, a.keys.Edit):
		return a.beginEditOfSelected()

	default:
		// Number keys 1-9 switch workspaces
		keyStr := msg.String()
		if len(keyStr) == 1 && keyStr[0] >= '1' && keyStr[0] <= '9' {
			idx := int(keyStr[0] - '1') // 0-indexed
			if idx < len(a.workspaceItems) && a.workspaceSwitcher != nil {
				if a.workspaceItems[idx].ID != a.workspaceRail.SelectedID() {
					switcher := a.workspaceSwitcher
					teamID := a.workspaceItems[idx].ID
					return func() tea.Msg {
						return switcher(teamID)
					}
				}
			}
		}
	}
	return nil
}

func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if a.editing.active && key.Matches(msg, a.keys.Escape) {
		// If a picker is active in the relevant compose, close it
		// instead of cancelling the edit.
		if a.editing.panel == PanelThread {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
		}
		a.cancelEdit()
		return nil
	}
	if key.Matches(msg, a.keys.Escape) {
		// If a picker is active, close it instead of exiting insert mode.
		if a.focusedPanel == PanelThread && a.threadVisible {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
		}
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}

	code := msg.Key().Code
	mod := msg.Key().Mod
	// Plain Enter sends; Shift+Enter (and Ctrl+J as a fallback for terminals
	// that don't disambiguate modifiers) inserts a newline.
	isSend := code == tea.KeyEnter && !mod.Contains(tea.ModShift)
	isNewline := (code == tea.KeyEnter && mod.Contains(tea.ModShift)) ||
		(code == 'j' && mod == tea.ModCtrl)

	// Determine which compose box is active based on focused panel
	if a.focusedPanel == PanelThread && a.threadVisible {
		// If a picker is active, forward all keys to compose (including Enter).
		if a.threadCompose.IsEmojiActive() || a.threadCompose.IsMentionActive() {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			return cmd
		}

		// Thread reply compose
		if isNewline {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		}
		if isSend {
			if a.editing.active && a.editing.panel == PanelThread {
				return a.submitEdit(a.threadCompose.Value(), a.threadCompose.TranslateMentionsForSend(a.threadCompose.Value()))
			}
			text := a.threadCompose.Value()
			if text != "" {
				text = a.threadCompose.TranslateMentionsForSend(text)
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				return func() tea.Msg {
					return SendThreadReplyMsg{
						ChannelID: channelID,
						ThreadTS:  threadTS,
						Text:      text,
					}
				}
			}
			return nil
		}
		var cmd tea.Cmd
		a.threadCompose, cmd = a.threadCompose.Update(msg)
		a.maybeSendTyping()
		return cmd
	}

	// Channel message compose
	// If a picker is active, forward all keys to compose (including Enter).
	if a.compose.IsEmojiActive() || a.compose.IsMentionActive() {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(msg)
		return cmd
	}

	if isNewline {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return cmd
	}
	if isSend {
		if a.editing.active && a.editing.panel == PanelMessages {
			return a.submitEdit(a.compose.Value(), a.compose.TranslateMentionsForSend(a.compose.Value()))
		}
		text := a.compose.Value()
		if text != "" {
			text = a.compose.TranslateMentionsForSend(text)
			a.compose.Reset()
			return func() tea.Msg {
				return SendMessageMsg{
					ChannelID: a.activeChannelID,
					Text:      text,
				}
			}
		}
		return nil
	}

	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	a.maybeSendTyping()
	return cmd
}

func (a *App) handleCommandMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.SetMode(ModeNormal)
	}
	return nil
}

func (a *App) handleChannelFinderMode(msg tea.KeyMsg) tea.Cmd {
	// Map tea.KeyMsg to string for the finder
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	}

	result := a.channelFinder.HandleKey(keyStr)
	if result != nil {
		a.channelFinder.Close()
		a.SetMode(ModeNormal)
		// Already-joined: switch immediately. Not joined: kick off a join
		// command; ChannelJoinedMsg will fold the channel into the sidebar
		// and switch to it.
		if result.Joined {
			a.sidebar.SelectByID(result.ID)
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: result.ID, Name: result.Name, Type: result.Type}
			}
		}
		if a.channelJoiner != nil {
			joiner := a.channelJoiner
			id, name := result.ID, result.Name
			return func() tea.Msg {
				return joiner(id, name)
			}
		}
	}

	// Check if finder closed itself (Esc)
	if !a.channelFinder.IsVisible() {
		a.SetMode(ModeNormal)
	}

	return nil
}

func (a *App) handleWorkspaceFinderMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	}

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

func (a *App) handleThemeSwitcherMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	}

	result := a.themeSwitcher.HandleKey(keyStr)
	if result != nil {
		a.themeSwitcher.Close()
		a.SetMode(ModeNormal)
		// Apply theme immediately
		styles.Apply(result.Name, a.themeOverrides)
		// Invalidate render caches so they rebuild with new theme colors
		a.messagepane.InvalidateCache()
		a.threadPanel.InvalidateCache()
		a.sidebar.InvalidateCache()
		// Refresh compose textarea styles for new theme
		a.compose.RefreshStyles()
		a.threadCompose.RefreshStyles()
		// Save selection
		if a.themeSaveFn != nil {
			a.themeSaveFn(result.Name, result.Scope)
		}
		return nil
	}
	if !a.themeSwitcher.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}

func (a *App) handleReactionPickerMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()

	switch msg.Key().Code {
	case tea.KeyEscape:
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

	// Capture values before HandleKey (which may call Close and reset them)
	channelID := a.reactionPicker.ChannelID()
	messageTS := a.reactionPicker.MessageTS()

	result := a.reactionPicker.HandleKey(keyStr)

	if !a.reactionPicker.IsVisible() {
		// Esc was pressed
		a.SetMode(ModeNormal)
		return nil
	}

	if result != nil {
		emojiName := result.Emoji

		a.reactionPicker.Close()
		a.SetMode(ModeNormal)

		// Record frecent usage on add (not remove)
		if !result.Remove && a.frecentRecordFn != nil {
			a.frecentRecordFn(emojiName)
		}

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

func (a *App) handleConfirmMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyEnter:
		keyStr = "enter"
	}

	res := a.confirmPrompt.HandleKey(keyStr)
	if !a.confirmPrompt.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return res.Cmd
}

func (a *App) updateReactionOnMessage(channelID, messageTS, emojiName, userID string, remove bool) {
	a.messagepane.UpdateReaction(messageTS, emojiName, userID, remove)
	a.threadPanel.UpdateReaction(messageTS, emojiName, userID, remove)
}

func (a *App) handleReactionNav(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.Left):
		a.messagepane.ReactionNavLeft()
	case key.Matches(msg, a.keys.Right):
		a.messagepane.ReactionNavRight()
	case key.Matches(msg, a.keys.Enter):
		emojiName, isPlus := a.messagepane.SelectedReaction()
		if isPlus {
			return a.openPickerFromMessage()
		}
		return a.toggleReactionOnSelectedMessage(emojiName)
	case key.Matches(msg, a.keys.Reaction):
		return a.openPickerFromMessage()
	case key.Matches(msg, a.keys.Escape):
		a.messagepane.ExitReactionNav()
	}
	return nil
}

func (a *App) handleThreadReactionNav(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.Left):
		a.threadPanel.ReactionNavLeft()
	case key.Matches(msg, a.keys.Right):
		a.threadPanel.ReactionNavRight()
	case key.Matches(msg, a.keys.Enter):
		emojiName, isPlus := a.threadPanel.SelectedReaction()
		if isPlus {
			return a.openPickerFromThread()
		}
		return a.toggleReactionOnSelectedThread(emojiName)
	case key.Matches(msg, a.keys.Reaction):
		return a.openPickerFromThread()
	case key.Matches(msg, a.keys.Escape):
		a.threadPanel.ExitReactionNav()
	}
	return nil
}

func (a *App) openPickerFromMessage() tea.Cmd {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return nil
	}
	var existing []string
	for _, r := range msg.Reactions {
		if r.HasReacted {
			existing = append(existing, r.Emoji)
		}
	}
	a.messagepane.ExitReactionNav()
	if a.frecentLoadFn != nil {
		a.reactionPicker.SetFrecentEmoji(a.frecentLoadFn(10))
	}
	a.reactionPicker.Open(a.activeChannelID, msg.TS, existing)
	a.SetMode(ModeReactionPicker)
	return nil
}

func (a *App) openPickerFromThread() tea.Cmd {
	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		return nil
	}
	var existing []string
	for _, r := range reply.Reactions {
		if r.HasReacted {
			existing = append(existing, r.Emoji)
		}
	}
	a.threadPanel.ExitReactionNav()
	if a.frecentLoadFn != nil {
		a.reactionPicker.SetFrecentEmoji(a.frecentLoadFn(10))
	}
	a.reactionPicker.Open(a.threadPanel.ChannelID(), reply.TS, existing)
	a.SetMode(ModeReactionPicker)
	return nil
}

func (a *App) toggleReactionOnSelectedMessage(emojiName string) tea.Cmd {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return nil
	}
	remove := false
	for _, r := range msg.Reactions {
		if r.Emoji == emojiName && r.HasReacted {
			remove = true
			break
		}
	}
	a.updateReactionOnMessage(a.activeChannelID, msg.TS, emojiName, a.currentUserID, remove)
	channelID := a.activeChannelID
	ts := msg.TS
	if remove {
		if a.reactionRemoveFn != nil {
			return func() tea.Msg {
				err := a.reactionRemoveFn(channelID, ts, emojiName)
				return ReactionSentMsg{Err: err}
			}
		}
	} else {
		if a.reactionAddFn != nil {
			return func() tea.Msg {
				err := a.reactionAddFn(channelID, ts, emojiName)
				return ReactionSentMsg{Err: err}
			}
		}
	}
	return nil
}

func (a *App) toggleReactionOnSelectedThread(emojiName string) tea.Cmd {
	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		return nil
	}
	remove := false
	for _, r := range reply.Reactions {
		if r.Emoji == emojiName && r.HasReacted {
			remove = true
			break
		}
	}
	channelID := a.threadPanel.ChannelID()
	a.updateReactionOnMessage(channelID, reply.TS, emojiName, a.currentUserID, remove)
	ts := reply.TS
	if remove {
		if a.reactionRemoveFn != nil {
			return func() tea.Msg {
				err := a.reactionRemoveFn(channelID, ts, emojiName)
				return ReactionSentMsg{Err: err}
			}
		}
	} else {
		if a.reactionAddFn != nil {
			return func() tea.Msg {
				err := a.reactionAddFn(channelID, ts, emojiName)
				return ReactionSentMsg{Err: err}
			}
		}
	}
	return nil
}

// copyPermalinkOfSelected resolves the currently-selected message or thread
// reply, calls the permalink fetcher, and returns a tea.Cmd that writes the
// URL to the clipboard and emits a status-bar toast.
func (a *App) copyPermalinkOfSelected() tea.Cmd {
	if a.permalinkFetchFn == nil {
		return nil
	}
	var channelID, ts string
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		channelID = a.activeChannelID
		ts = msg.TS
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		channelID = a.threadPanel.ChannelID()
		ts = reply.TS
	default:
		return nil
	}
	if channelID == "" || ts == "" {
		return nil
	}
	fetch := a.permalinkFetchFn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		url, err := fetch(ctx, channelID, ts)
		if err != nil {
			log.Printf("copy permalink: %v", err)
			return statusbar.PermalinkCopyFailedMsg{}
		}
		return tea.BatchMsg{
			tea.SetClipboard(url),
			func() tea.Msg { return statusbar.PermalinkCopiedMsg{} },
		}
	}
}

func (a *App) handleDown() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveDown()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.MoveDown()
			return a.openSelectedThreadCmd()
		}
		a.messagepane.MoveDown()
	case PanelThread:
		a.threadPanel.MoveDown()
	}
	return nil
}

func (a *App) handleUp() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveUp()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.MoveUp()
			return a.openSelectedThreadCmd()
		}
		a.messagepane.MoveUp()
		// If at top, fetch older messages
		if a.messagepane.AtTop() && !a.fetchingOlder && a.olderMessagesFetcher != nil {
			a.fetchingOlder = true
			a.messagepane.SetLoading(true)
			chID := a.activeChannelID
			oldestTS := a.messagepane.OldestTS()
			fetcher := a.olderMessagesFetcher
			return func() tea.Msg {
				return fetcher(chID, oldestTS)
			}
		}
	case PanelThread:
		a.threadPanel.MoveUp()
	}
	return nil
}

func (a *App) handleGoToBottom() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.GoToBottom()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.GoToBottom()
			return a.openSelectedThreadCmd()
		}
		a.messagepane.GoToBottom()
	case PanelThread:
		a.threadPanel.GoToBottom()
	}
	return nil
}

// pageSize returns the number of lines to scroll for a full-page jump in the
// currently-focused panel. Falls back to a sensible default if the layout
// hasn't been measured yet (i.e. before the first render).
func (a *App) pageSize() int {
	var h int
	switch a.focusedPanel {
	case PanelSidebar:
		h = a.layoutSidebarHeight
	case PanelMessages:
		h = a.layoutMsgHeight
	case PanelThread:
		h = a.layoutThreadHeight
	}
	if h < 4 {
		h = 4
	}
	// Leave one line of context across the page boundary (vim-style).
	return h - 1
}

// halfPageSize returns the half-page scroll distance for ctrl+u / ctrl+d.
func (a *App) halfPageSize() int {
	n := a.pageSize() / 2
	if n < 1 {
		n = 1
	}
	return n
}

// panelAt classifies the (x, y) coordinate into the panel under the
// cursor and returns pane-local content coordinates (after subtracting
// layout offsets and the 1-row top border). ok=false means the cursor
// is outside the messages/thread panes (status bar, sidebar, rail —
// drag selection is not supported there).
func (a *App) panelAt(x, y int) (panel Panel, paneX, paneY int, ok bool) {
	if y >= a.height-1 {
		return PanelWorkspace, 0, 0, false // status bar
	}
	switch {
	case x < a.layoutRailWidth:
		return PanelWorkspace, 0, 0, false
	case a.sidebarVisible && x < a.layoutSidebarEnd:
		return PanelSidebar, 0, 0, false
	case x < a.layoutMsgEnd:
		// Messages pane content: subtract the message-pane left edge
		// (after sidebar) and account for the panel's top border (1 row).
		return PanelMessages, x - a.layoutSidebarEnd - 1, y - 1, true
	case a.threadVisible && x < a.layoutThreadEnd:
		return PanelThread, x - a.layoutMsgEnd - 1, y - 1, true
	}
	return PanelWorkspace, 0, 0, false
}

// scrollFocusedPanel scrolls the focused panel by delta lines (negative = up).
func (a *App) scrollFocusedPanel(delta int) {
	if delta == 0 {
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		if delta < 0 {
			a.sidebar.ScrollUp(-delta)
		} else {
			a.sidebar.ScrollDown(delta)
		}
	case PanelMessages:
		if a.view == ViewThreads {
			if delta < 0 {
				a.threadsView.ScrollUp(-delta)
			} else {
				a.threadsView.ScrollDown(delta)
			}
		} else {
			if delta < 0 {
				a.messagepane.ScrollUp(-delta)
			} else {
				a.messagepane.ScrollDown(delta)
			}
		}
	case PanelThread:
		if delta < 0 {
			a.threadPanel.ScrollUp(-delta)
		} else {
			a.threadPanel.ScrollDown(delta)
		}
	}
}

func (a *App) handleEnter() tea.Cmd {
	if a.focusedPanel == PanelSidebar {
		if a.sidebar.IsThreadsSelected() {
			return func() tea.Msg { return ThreadsViewActivatedMsg{} }
		}
		item, ok := a.sidebar.SelectedItem()
		if ok {
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: item.ID, Name: item.Name, Type: item.Type}
			}
		}
	}

	if a.focusedPanel == PanelMessages {
		msg, ok := a.messagepane.SelectedMessage()
		if ok {
			// Use the message's own TS as the thread parent.
			// If it's already a thread reply, use its ThreadTS instead.
			threadTS := msg.TS
			if msg.ThreadTS != "" && msg.ThreadTS != msg.TS {
				threadTS = msg.ThreadTS
			}
			a.threadVisible = true
			a.statusbar.SetInThread(true)
			a.focusedPanel = PanelThread
			a.threadPanel.SetThread(msg, nil, a.activeChannelID, threadTS)
			a.threadCompose.SetChannel("thread")

			if a.threadFetcher != nil {
				fetcher := a.threadFetcher
				chID := a.activeChannelID
				ts := threadTS
				return func() tea.Msg {
					return fetcher(chID, ts)
				}
			}
		}
	}

	return nil
}

func (a *App) SetMode(mode Mode) {
	if mode == ModeInsert {
		a.clearSelections()
	}
	a.mode = mode
	a.statusbar.SetMode(mode)
}

// clearSelections drops any active mouse selection from both message
// and thread panes. Called from any handler that changes focus, mode,
// or visible content in a way that makes the existing selection
// nonsensical (workspace switch, mode change, focus cycle, etc.).
func (a *App) clearSelections() {
	a.messagepane.ClearSelection()
	a.threadPanel.ClearSelection()
}

func (a *App) FocusNext() {
	a.cancelEdit()
	a.clearSelections()
	if !a.sidebarVisible {
		if a.threadVisible {
			if a.focusedPanel == PanelMessages {
				a.focusedPanel = PanelThread
			} else {
				a.focusedPanel = PanelMessages
			}
		}
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		a.focusedPanel = PanelMessages
	case PanelMessages:
		if a.threadVisible {
			a.focusedPanel = PanelThread
		} else {
			a.focusedPanel = PanelSidebar
		}
	case PanelThread:
		a.focusedPanel = PanelSidebar
	}
}

func (a *App) FocusPrev() {
	a.cancelEdit()
	a.clearSelections()
	if !a.sidebarVisible {
		if a.threadVisible {
			if a.focusedPanel == PanelThread {
				a.focusedPanel = PanelMessages
			} else {
				a.focusedPanel = PanelThread
			}
		}
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		if a.threadVisible {
			a.focusedPanel = PanelThread
		} else {
			a.focusedPanel = PanelMessages
		}
	case PanelMessages:
		a.focusedPanel = PanelSidebar
	case PanelThread:
		a.focusedPanel = PanelMessages
	}
}

func (a *App) ToggleSidebar() {
	a.clearSelections()
	a.sidebarVisible = !a.sidebarVisible
	if !a.sidebarVisible && a.focusedPanel == PanelSidebar {
		a.focusedPanel = PanelMessages
	}
}

func (a *App) ToggleThread() {
	a.clearSelections()
	if a.threadVisible {
		a.CloseThread()
	}
	// Don't open on toggle if no thread is loaded -- use Enter for that
}

func (a *App) CloseThread() {
	a.clearSelections()
	a.threadVisible = false
	a.statusbar.SetInThread(false)
	a.threadPanel.Clear()
	a.threadCompose.Blur()
	// Drop dedup state so a future activation re-opens this thread.
	a.lastOpenedChannelID = ""
	a.lastOpenedThreadTS = ""
	if a.focusedPanel == PanelThread {
		a.focusedPanel = PanelMessages
	}
}

// openSelectedThreadCmd opens the right thread panel on whichever row is
// currently highlighted in the threads view. No-op if the list is empty,
// no thread fetcher is wired, OR the selected thread is already the one
// open in the right panel (dedup: avoids hammering the Slack API and
// clobbering an in-progress read on every j/k press or list reload).
func (a *App) openSelectedThreadCmd() tea.Cmd {
	sum, ok := a.threadsView.SelectedSummary()
	if !ok {
		return nil
	}
	if sum.ChannelID == a.lastOpenedChannelID && sum.ThreadTS == a.lastOpenedThreadTS {
		return nil
	}
	a.lastOpenedChannelID = sum.ChannelID
	a.lastOpenedThreadTS = sum.ThreadTS
	a.threadVisible = true
	a.statusbar.SetInThread(true)
	parent := messages.MessageItem{
		TS:       sum.ParentTS,
		UserID:   sum.ParentUserID,
		UserName: a.userNameFor(sum.ParentUserID),
		Text:     sum.ParentText,
		ThreadTS: sum.ThreadTS,
	}
	a.threadPanel.SetThread(parent, nil, sum.ChannelID, sum.ThreadTS)
	a.threadCompose.SetChannel("thread")
	if a.threadFetcher != nil {
		fetcher := a.threadFetcher
		chID, threadTS := sum.ChannelID, sum.ThreadTS
		return func() tea.Msg { return fetcher(chID, threadTS) }
	}
	return nil
}

// scheduleThreadsDirty returns a tea.Cmd that fires a ThreadsListDirtyMsg
// after the configured debounce interval. Used to coalesce bursts of thread
// replies (each delivered as its own NewMessageMsg) into a single re-query
// of the involved-threads list. Returns nil when no workspace is active —
// without an activeTeamID the dirty handler would just drop the message
// anyway.
func (a *App) scheduleThreadsDirty() tea.Cmd {
	if a.activeTeamID == "" {
		return nil
	}
	team := a.activeTeamID
	d := a.threadsDirtyDebounce
	if d == 0 {
		d = 150 * time.Millisecond
	}
	return tea.Tick(d, func(time.Time) tea.Msg {
		return ThreadsListDirtyMsg{TeamID: team}
	})
}

// userNameFor returns the display name for a Slack user ID, falling back
// to the raw ID when the names map has no entry. Returns empty string for
// an empty userID.
func (a *App) userNameFor(userID string) string {
	if userID == "" {
		return ""
	}
	if n, ok := a.userNames[userID]; ok && n != "" {
		return n
	}
	return userID
}

// Loading overlay methods

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

func (a *App) MarkWorkspaceReady(teamName string) {
	for i := range a.loadingStates {
		if a.loadingStates[i].TeamName == teamName {
			a.loadingStates[i].Status = "ready"
			break
		}
	}
	a.checkLoadingDone()
}

func (a *App) MarkWorkspaceFailed(teamName string) {
	for i := range a.loadingStates {
		if a.loadingStates[i].TeamName == teamName {
			a.loadingStates[i].Status = "failed"
			break
		}
	}
	a.checkLoadingDone()
}

func (a *App) checkLoadingDone() {
	// Dismiss loading as soon as at least one workspace is ready.
	// Other workspaces continue connecting in the background.
	for _, e := range a.loadingStates {
		if e.Status == "ready" {
			a.loading = false
			return
		}
	}
	// If none ready, check if all are failed/done
	for _, e := range a.loadingStates {
		if e.Status == "connecting" {
			return
		}
	}
	a.loading = false
}

var spinnerChars = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func (a *App) renderLoadingOverlay(width, height int) string {
	var rows []string
	spinner := string(spinnerChars[a.spinnerFrame])

	for _, entry := range a.loadingStates {
		switch entry.Status {
		case "ready":
			rows = append(rows, lipgloss.NewStyle().Foreground(styles.Accent).Render("✓")+" "+entry.TeamName)
		case "failed":
			rows = append(rows, lipgloss.NewStyle().Foreground(styles.Error).Render("✗")+" "+entry.TeamName+" (failed)")
		default:
			rows = append(rows, lipgloss.NewStyle().Foreground(styles.Primary).Render(spinner)+" Connecting to "+entry.TeamName+"...")
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
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(styles.SurfaceDark)),
	)
}

// SetInitialLastReadTS sets the last read timestamp for the initial channel load.
func (a *App) SetInitialLastReadTS(ts string) {
	a.messagepane.SetLastReadTS(ts)
}

// Setters for external use (wiring services)
func (a *App) SetWorkspaces(items []workspace.WorkspaceItem) {
	a.workspaceRail.SetItems(items)
	a.workspaceItems = items
	// Update workspace finder items
	var finderItems []workspacefinder.Item
	for _, ws := range items {
		finderItems = append(finderItems, workspacefinder.Item{
			ID:       ws.ID,
			Name:     ws.Name,
			Initials: ws.Initials,
		})
	}
	a.workspaceFinder.SetItems(finderItems)
}

func (a *App) SetChannels(items []sidebar.ChannelItem) {
	a.sidebar.SetItems(items)
}

// SetChannelFetcher sets the callback used to load messages when a channel is selected.
func (a *App) SetChannelFetcher(fn ChannelFetchFunc) {
	a.channelFetcher = fn
}

// SetOlderMessagesFetcher sets the callback used to load older messages when scrolling up.
func (a *App) SetOlderMessagesFetcher(fn OlderMessagesFetchFunc) {
	a.olderMessagesFetcher = fn
}

// SetMessageSender sets the callback used to send messages.
func (a *App) SetMessageSender(fn MessageSendFunc) {
	a.messageSender = fn
}

// SetMessageEditor wires the chat.update callback used by edit submit.
func (a *App) SetMessageEditor(fn MessageEditFunc) {
	a.messageEditor = fn
}

// SetMessageDeleter wires the chat.delete callback used by delete confirm.
func (a *App) SetMessageDeleter(fn MessageDeleteFunc) {
	a.messageDeleter = fn
}

// SetThreadFetcher sets the callback used to load thread replies.
func (a *App) SetThreadFetcher(fn ThreadFetchFunc) {
	a.threadFetcher = fn
}

// SetThreadReplySender sets the callback used to send thread replies.
func (a *App) SetThreadReplySender(fn ThreadReplySendFunc) {
	a.threadReplySender = fn
}

// SetThreadsListFetcher wires the function that loads the involved-threads
// list for a workspace. Called by main.go.
func (a *App) SetThreadsListFetcher(f ThreadsListFetchFunc) {
	a.threadsListFetcher = f
}

func (a *App) SetChannelFinderItems(items []channelfinder.Item) {
	a.channelFinder.SetItems(items)
}

// SetAvatarFunc sets the function used to get rendered avatars for messages.
func (a *App) SetAvatarFunc(fn messages.AvatarFunc) {
	a.messagepane.SetAvatarFunc(fn)
	a.threadPanel.SetAvatarFunc(fn)
}

// SetUserNames passes the user ID -> display name map to the message pane for mention resolution.
func (a *App) SetUserNames(names map[string]string) {
	a.userNames = names
	a.threadsView.SetUserNames(names)
	a.messagepane.SetUserNames(names)
	a.threadPanel.SetUserNames(names)

	// Build user list for mention picker
	users := make([]mentionpicker.User, 0, len(names))
	for id, displayName := range names {
		users = append(users, mentionpicker.User{
			ID:          id,
			DisplayName: displayName,
			Username:    "",
		})
	}
	a.compose.SetUsers(users)
	a.threadCompose.SetUsers(users)
}

// SetCustomEmoji rebuilds the emoji entry list (built-ins + the active
// workspace's customs) and pushes it into both compose boxes.
func (a *App) SetCustomEmoji(customs map[string]string) {
	entries := emoji.BuildEntries(customs)
	a.compose.SetEmojiEntries(entries)
	a.threadCompose.SetEmojiEntries(entries)
}

// SetInitialChannel sets the active channel and its messages before the TUI starts.
func (a *App) SetInitialChannel(channelID, channelName string, msgs []messages.MessageItem) {
	a.activeChannelID = channelID
	a.messagepane.SetChannel(channelName, "")
	a.messagepane.SetMessages(msgs)
	a.compose.SetChannel(channelName)
	a.statusbar.SetChannel(channelName)
}

func (a *App) SetReactionSender(add ReactionAddFunc, remove ReactionRemoveFunc) {
	a.reactionAddFn = add
	a.reactionRemoveFn = remove
}

// SetPermalinkFetcher sets the callback used to look up message permalinks
// for the copy-permalink action.
func (a *App) SetPermalinkFetcher(fn PermalinkFetchFunc) {
	a.permalinkFetchFn = fn
}

func (a *App) SetCurrentUserID(userID string) {
	a.currentUserID = userID
	a.threadsView.SetSelfUserID(userID)
}

func (a *App) SetFrecentFuncs(load FrecentLoadFunc, record FrecentRecordFunc) {
	a.frecentLoadFn = load
	a.frecentRecordFn = record
}

// ActiveChannelID returns the ID of the currently viewed channel.
func (a *App) ActiveChannelID() string {
	return a.activeChannelID
}

// SetWorkspaceSwitcher sets the callback used to switch workspaces.
func (a *App) SetWorkspaceSwitcher(fn SwitchWorkspaceFunc) {
	a.workspaceSwitcher = fn
}

// SetThemeItems sets the available themes for the switcher.
func (a *App) SetThemeItems(names []string) {
	a.themeSwitcher.SetItems(names)
}

// activeTeamName returns the human-readable name of the active workspace,
// falling back to the team ID if no name is known. Used as a label in the
// theme picker header.
func (a *App) activeTeamName() string {
	for _, w := range a.workspaceItems {
		if w.ID == a.activeTeamID {
			if w.Name != "" {
				return w.Name
			}
			return w.ID
		}
	}
	if a.activeTeamID != "" {
		return a.activeTeamID
	}
	return "this workspace"
}

// SetThemeSaver sets the callback for saving the theme selection. The
// callback receives the chosen theme name and the scope (workspace vs.
// global) so the implementation can route to the correct save target.
func (a *App) SetThemeSaver(fn func(name string, scope themeswitcher.ThemeScope)) {
	a.themeSaveFn = fn
}

// SetThemeOverrides stores the config theme overrides for applying on switch.
func (a *App) SetThemeOverrides(overrides config.Theme) {
	a.themeOverrides = overrides
}

// SetTypingEnabled controls whether typing indicators are shown and sent.
func (a *App) SetTypingEnabled(enabled bool) {
	a.typingEnabled = enabled
}

// SetSidebarStaleThreshold configures auto-hiding of inactive
// channels in the sidebar. d is the maximum age (since LastReadTS)
// before a channel is hidden; pass 0 to disable.
func (a *App) SetSidebarStaleThreshold(d time.Duration) {
	a.sidebar.SetStaleThreshold(d)
}

// SetTypingSender sets the callback for sending typing indicators.
func (a *App) SetTypingSender(fn TypingSendFunc) {
	a.typingSendFn = fn
}

// SetChannelJoiner sets the callback for joining a channel via the Slack API.
func (a *App) SetChannelJoiner(fn JoinChannelFunc) {
	a.channelJoiner = fn
}

// shouldSendTyping returns true if enough time has passed since the last typing send.
func (a *App) shouldSendTyping() bool {
	if !a.typingEnabled {
		return false
	}
	return time.Since(a.lastTypingSent) >= 3*time.Second
}

// maybeSendTyping sends a typing indicator if the throttle allows it.
func (a *App) maybeSendTyping() {
	if a.typingSendFn == nil || !a.shouldSendTyping() {
		return
	}
	a.lastTypingSent = time.Now()
	channelID := a.activeChannelID
	if a.focusedPanel == PanelThread && a.threadVisible {
		channelID = a.threadPanel.ChannelID()
	}
	go a.typingSendFn(channelID)
}

// addTypingUser records that a user is typing in a channel.
func (a *App) addTypingUser(channelID, userID string) {
	if a.typingUsers[channelID] == nil {
		a.typingUsers[channelID] = make(map[string]time.Time)
	}
	a.typingUsers[channelID][userID] = time.Now().Add(5 * time.Second)
}

// expireTypingUsers removes expired typing entries.
func (a *App) expireTypingUsers() {
	now := time.Now()
	for ch, users := range a.typingUsers {
		for uid, expires := range users {
			if now.After(expires) {
				delete(users, uid)
			}
		}
		if len(users) == 0 {
			delete(a.typingUsers, ch)
		}
	}
}

// getTypingUsers returns user IDs currently typing in the given channel.
func (a *App) getTypingUsers(channelID string) []string {
	users := a.typingUsers[channelID]
	if len(users) == 0 {
		return nil
	}
	now := time.Now()
	var result []string
	for uid, expires := range users {
		if now.Before(expires) {
			result = append(result, uid)
		}
	}
	return result
}

// getTypingUsersFiltered returns typing user IDs excluding the current user.
func (a *App) getTypingUsersFiltered(channelID string) []string {
	all := a.getTypingUsers(channelID)
	var filtered []string
	for _, uid := range all {
		if uid != a.currentUserID {
			filtered = append(filtered, uid)
		}
	}
	return filtered
}

// renderTypingLine returns the styled typing indicator for the current channel,
// or an empty string if no one is typing.
func (a *App) renderTypingLine() string {
	if !a.typingEnabled {
		return ""
	}
	userIDs := a.getTypingUsersFiltered(a.activeChannelID)
	if len(userIDs) == 0 {
		return ""
	}

	// Resolve user IDs to display names
	names := make([]string, 0, len(userIDs))
	for _, uid := range userIDs {
		name := a.messagepane.ResolveUserName(uid)
		if name == "" {
			name = uid
		}
		names = append(names, name)
	}

	text := a.typingIndicatorText(names)
	return styles.TypingIndicator.Render(text)
}

// typingIndicatorText formats the typing indicator string from display names.
func (a *App) typingIndicatorText(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing..."
	case 2:
		return names[0] + " and " + names[1] + " are typing..."
	default:
		return "Several people are typing..."
	}
}

func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		v := tea.NewView("Initializing...")
		v.AltScreen = true
		return v
	}

	statusHeight := 1
	contentHeight := a.height - statusHeight

	// Calculate widths, accounting for borders (2 cols each for left+right)
	railWidth := a.workspaceRail.Width()
	sidebarWidth := 0
	sidebarBorder := 0
	if a.sidebarVisible {
		sidebarWidth = a.sidebar.Width()
		sidebarBorder = 2 // left + right border
	}

	// Calculate the message area (everything right of sidebar)
	msgAreaWidth := a.width - railWidth - sidebarWidth - sidebarBorder

	// Determine thread and message pane widths
	msgBorder := 2
	threadWidth := 0
	threadBorder := 0
	if a.threadVisible {
		threadBorder = 2
		// 35% of message area for thread, but enforce minimums
		threadWidth = msgAreaWidth * 35 / 100
		msgPaneWidth := msgAreaWidth - threadWidth - msgBorder - threadBorder
		// Enforce minimum widths
		if msgPaneWidth < 40 || threadWidth < 30 {
			// Too narrow -- auto-hide thread
			a.threadVisible = false
			threadWidth = 0
			threadBorder = 0
			if a.focusedPanel == PanelThread {
				a.focusedPanel = PanelMessages
			}
		}
	}

	msgWidth := msgAreaWidth - msgBorder - threadWidth - threadBorder
	if msgWidth < 10 {
		msgWidth = 10
	}

	// Store layout widths for mouse hit-testing in Update()
	a.layoutRailWidth = railWidth
	a.layoutSidebarEnd = railWidth + sidebarWidth + sidebarBorder
	a.layoutMsgEnd = a.layoutSidebarEnd + msgWidth + msgBorder
	if a.threadVisible && threadWidth > 0 {
		a.layoutThreadEnd = a.layoutMsgEnd + threadWidth + threadBorder
	} else {
		a.layoutThreadEnd = a.layoutMsgEnd
	}

	// Helper to force a panel to an exact width and height with a given
	// background color. Uses an explicit width parameter instead of
	// lipgloss.Width(s) to avoid ANSI miscounting in complex rendered content.
	exactSizeBg := func(s string, w, h int, bg color.Color) string {
		return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Background(bg).Render(s)
	}
	exactSize := func(s string, w, h int) string {
		return exactSizeBg(s, w, h, styles.Background)
	}

	themeVer := styles.Version()

	// Render workspace rail (uses rail background so empty cells around
	// the workspace tiles match the rail color, not the message pane).
	railLayoutKey := themeVer
	if c := &a.panelCacheRail; !c.hit(a.workspaceRail.Version(), railWidth, contentHeight, railLayoutKey) {
		out := exactSizeBg(a.workspaceRail.View(contentHeight), railWidth, contentHeight, styles.RailBackground)
		c.store(out, a.workspaceRail.Version(), railWidth, contentHeight, railLayoutKey)
	}
	rail := a.panelCacheRail.output

	var panels []string
	panels = append(panels, rail)

	// Render sidebar. Sidebar uses SidebarBackground so themes with a
	// distinct dark sidebar (e.g. Slack Default) render correctly: both the
	// rounded border's own background and the right-padding fill match the
	// sidebar's panel color rather than the message pane's.
	if a.sidebarVisible {
		sbFocused := a.focusedPanel == PanelSidebar && a.mode != ModeInsert
		sbLayoutKey := themeVer<<1 | boolToInt(sbFocused)
		if c := &a.panelCacheSidebar; !c.hit(a.sidebar.Version(), sidebarWidth, contentHeight, sbLayoutKey) {
			borderStyle := lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(styles.Border).
				BorderBackground(styles.SidebarBackground).
				Background(styles.SidebarBackground).
				Width(sidebarWidth)
			if sbFocused {
				borderStyle = lipgloss.NewStyle().
					BorderStyle(lipgloss.ThickBorder()).
					BorderForeground(styles.Primary).
					BorderBackground(styles.SidebarBackground).
					Background(styles.SidebarBackground).
					Width(sidebarWidth)
			}
			sidebarView := a.sidebar.View(contentHeight-2, sidebarWidth)
			sidebarView = borderStyle.Render(sidebarView)
			out := exactSizeBg(sidebarView, sidebarWidth+sidebarBorder, contentHeight, styles.SidebarBackground)
			c.store(out, a.sidebar.Version(), sidebarWidth, contentHeight, sbLayoutKey)
		}
		panels = append(panels, a.panelCacheSidebar.output)
		a.layoutSidebarHeight = contentHeight - 2
	}

	// Render message pane with border. The message pane bundles three
	// things that change at very different rates: the messages panel
	// itself, the typing-indicator line, and the compose box. We compose
	// a single int64 panelVersion that mixes their individual versions so
	// the cache invalidates whenever any of them changes.
	msgFocused := a.focusedPanel == PanelMessages && a.mode != ModeInsert
	composeFocused := a.mode == ModeInsert && a.focusedPanel != PanelThread
	typingActive := len(a.typingUsers) > 0
	// Mix the view-mode bit into the layout key so a Channels<->Threads
	// switch invalidates the cached output (the cache is otherwise
	// indistinguishable across views at the same focus/mode/theme).
	msgLayoutKey := themeVer<<4 |
		boolToInt(a.view == ViewThreads)<<3 |
		boolToInt(msgFocused)<<2 |
		boolToInt(composeFocused)<<1 |
		boolToInt(typingActive)
	var msgPanelVersion int64
	if a.view == ViewThreads {
		msgPanelVersion = mixVersions(a.threadsView.Version(), 0)
	} else {
		msgPanelVersion = mixVersions(a.messagepane.Version(), a.compose.Version())
	}
	a.compose.SetWidth(msgWidth - 2)
	if c := &a.panelCacheMsgPanel; !c.hit(msgPanelVersion, msgWidth, contentHeight, msgLayoutKey) {
		msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
		if msgFocused {
			msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
		}
		if a.view == ViewThreads {
			// Threads view: replace the entire inner content with the
			// threads list. No compose, no typing line.
			a.threadsView.SetUserNames(a.userNames)
			a.threadsView.SetSelfUserID(a.currentUserID)
			msgContentHeight := contentHeight - 2
			a.layoutMsgHeight = msgContentHeight
			if msgContentHeight < 3 {
				msgContentHeight = 3
			}
			tvView := a.threadsView.View(msgContentHeight, msgWidth-2)
			tvView = messages.ReapplyBgAfterResets(tvView, messages.BgANSI())
			out := exactSize(
				msgBorderStyle.Render(tvView),
				msgWidth+msgBorder, contentHeight,
			)
			c.store(out, msgPanelVersion, msgWidth, contentHeight, msgLayoutKey)
		} else {
			composeView := a.compose.View(msgWidth-2, composeFocused)
			// Inline pickers stack above the compose box. Both should never be
			// visible simultaneously (mutually exclusive in compose.Update);
			// emoji wins if somehow both are.
			if pickerView := a.compose.EmojiPickerView(msgWidth - 2); pickerView != "" {
				composeView = pickerView + "\n" + composeView
			} else if mentionView := a.compose.MentionPickerView(msgWidth - 2); mentionView != "" {
				composeView = mentionView + "\n" + composeView
			}
			// Add a background-colored spacer line above the compose box
			// (replaces MarginTop which produced unstyled/black margin cells)
			composeSpacer := lipgloss.NewStyle().Background(styles.Background).Width(msgWidth - 2).Render("")
			composeView = composeSpacer + "\n" + composeView
			composeHeight := lipgloss.Height(composeView)
			typingLine := a.renderTypingLine()
			typingHeight := 0
			if typingLine != "" {
				typingHeight = 1
			}
			msgContentHeight := contentHeight - 2 - composeHeight - typingHeight
			a.layoutMsgHeight = msgContentHeight
			if msgContentHeight < 3 {
				msgContentHeight = 3
			}
			msgView := a.messagepane.View(msgContentHeight, msgWidth-2)
			var msgInner string
			if typingLine != "" {
				msgInner = lipgloss.JoinVertical(lipgloss.Left, msgView, typingLine, composeView)
			} else {
				msgInner = lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
			}
			// Re-apply theme background after ANSI resets so the border style's
			// right-side padding gets the correct background instead of terminal default.
			msgInner = messages.ReapplyBgAfterResets(msgInner, messages.BgANSI())
			out := exactSize(
				msgBorderStyle.Render(msgInner),
				msgWidth+msgBorder, contentHeight,
			)
			c.store(out, msgPanelVersion, msgWidth, contentHeight, msgLayoutKey)
		}
	}
	panels = append(panels, a.panelCacheMsgPanel.output)

	// Render thread panel if visible. Same caching pattern as the message
	// panel: panelVersion mixes the thread reply panel and the thread
	// compose box.
	if a.threadVisible && threadWidth > 0 {
		threadFocused := a.focusedPanel == PanelThread && a.mode != ModeInsert
		threadComposeFocused := a.mode == ModeInsert && a.focusedPanel == PanelThread
		threadLayoutKey := themeVer<<2 | boolToInt(threadFocused)<<1 | boolToInt(threadComposeFocused)
		threadPanelVersion := mixVersions(a.threadPanel.Version(), a.threadCompose.Version())
		a.threadCompose.SetWidth(threadWidth - 2)
		if c := &a.panelCacheThread; !c.hit(threadPanelVersion, threadWidth, contentHeight, threadLayoutKey) {
			threadBorderStyle := styles.UnfocusedBorder.Width(threadWidth)
			if threadFocused {
				threadBorderStyle = styles.FocusedBorder.Width(threadWidth)
			}
			threadComposeView := a.threadCompose.View(threadWidth-2, threadComposeFocused)
			if pickerView := a.threadCompose.EmojiPickerView(threadWidth - 2); pickerView != "" {
				threadComposeView = pickerView + "\n" + threadComposeView
			} else if mentionView := a.threadCompose.MentionPickerView(threadWidth - 2); mentionView != "" {
				threadComposeView = mentionView + "\n" + threadComposeView
			}
			threadComposeSpacer := lipgloss.NewStyle().Background(styles.Background).Width(threadWidth - 2).Render("")
			threadComposeView = threadComposeSpacer + "\n" + threadComposeView
			threadComposeHeight := lipgloss.Height(threadComposeView)
			threadContentHeight := contentHeight - 2 - threadComposeHeight
			a.layoutThreadHeight = threadContentHeight
			if threadContentHeight < 3 {
				threadContentHeight = 3
			}
			threadView := a.threadPanel.View(threadContentHeight, threadWidth-2)
			threadInner := lipgloss.JoinVertical(lipgloss.Left, threadView, threadComposeView)
			threadInner = messages.ReapplyBgAfterResets(threadInner, messages.BgANSI())
			out := exactSize(
				threadBorderStyle.Render(threadInner),
				threadWidth+threadBorder, contentHeight,
			)
			c.store(out, threadPanelVersion, threadWidth, contentHeight, threadLayoutKey)
		}
		panels = append(panels, a.panelCacheThread.output)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	statusWidth := a.width - railWidth

	// Cache the status row (rail-spacer + statusbar). It depends only on
	// statusbar.Version, statusWidth, and theme.
	if c := &a.panelCacheStatus; !c.hit(a.statusbar.Version(), statusWidth, 1, themeVer) {
		railSpacer := lipgloss.NewStyle().
			Width(railWidth).
			Background(styles.RailBackground).
			Render("")
		out := lipgloss.JoinHorizontal(lipgloss.Center, railSpacer, a.statusbar.View(statusWidth))
		c.store(out, a.statusbar.Version(), statusWidth, 1, themeVer)
	}
	status := a.panelCacheStatus.output

	screen := lipgloss.JoinVertical(lipgloss.Left, content, status)

	// Render channel finder overlay on top of existing layout
	if a.channelFinder.IsVisible() {
		screen = a.channelFinder.ViewOverlay(a.width, a.height, screen)
	}

	if a.reactionPicker.IsVisible() {
		screen = a.reactionPicker.ViewOverlay(a.width, a.height, screen)
	}

	if a.confirmPrompt.IsVisible() {
		screen = a.confirmPrompt.ViewOverlay(a.width, a.height, screen)
	}

	if a.workspaceFinder.IsVisible() {
		screen = a.workspaceFinder.ViewOverlay(a.width, a.height, screen)
	}

	if a.themeSwitcher.IsVisible() {
		screen = a.themeSwitcher.ViewOverlay(a.width, a.height, screen)
	}

	if a.loading {
		screen = a.renderLoadingOverlay(a.width, a.height)
	}

	// All panels are wrapped in exactSize / exactSizeBg before joining, so
	// `screen` is already exactly (a.width, a.height) with every cell themed.
	// We skip the previously-mandatory full-screen lipgloss wrapper -- it
	// walked every cell of the entire ANSI output (~3.4 ms / frame, the
	// single largest cost in the prior profile) just to apply background
	// padding that's already there. If an overlay is active we still need
	// the wrapper because overlay compositors don't always produce exact-
	// sized output; conservatively re-wrap in that case.
	finalScreen := screen
	overlayActive := a.channelFinder.IsVisible() ||
		a.reactionPicker.IsVisible() ||
		a.confirmPrompt.IsVisible() ||
		a.workspaceFinder.IsVisible() ||
		a.themeSwitcher.IsVisible() ||
		a.loading
	if overlayActive {
		finalScreen = lipgloss.NewStyle().
			Width(a.width).
			Height(a.height).
			MaxHeight(a.height).
			Background(styles.Background).
			Render(screen)
	}
	v := tea.NewView(finalScreen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// cancelEdit exits edit mode, restoring the stashed draft to its
// source compose. Safe to call when no edit is active (no-op).
func (a *App) cancelEdit() {
	if !a.editing.active {
		return
	}
	switch a.editing.panel {
	case PanelMessages:
		a.compose.SetValue(a.editing.stashedDraft)
		a.compose.SetPlaceholderOverride("")
	case PanelThread:
		a.threadCompose.SetValue(a.editing.stashedDraft)
		a.threadCompose.SetPlaceholderOverride("")
	}
	a.editing = editState{}
	a.SetMode(ModeNormal)
	a.compose.Blur()
	a.threadCompose.Blur()
}

// isOwnMessage returns whether the given message is owned by the
// current user. Bot/system messages and unauthenticated states fail.
func (a *App) isOwnMessage(m messages.MessageItem) bool {
	return a.currentUserID != "" && m.UserID == a.currentUserID
}

// beginEditOfSelected starts editing the currently-selected message
// in the focused pane. Returns a no-op + status toast if not owned;
// returns nil if no message is selected.
func (a *App) beginEditOfSelected() tea.Cmd {
	var (
		channelID string
		ts        string
		text      string
		userID    string
		panel     Panel
	)
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		channelID = a.activeChannelID
		ts = msg.TS
		text = msg.Text
		userID = msg.UserID
		panel = PanelMessages
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		channelID = a.threadPanel.ChannelID()
		ts = reply.TS
		text = reply.Text
		userID = reply.UserID
		panel = PanelThread
	default:
		return nil
	}
	// Build a synthetic MessageItem just for the ownership check;
	// avoids fetching the full struct twice.
	if !a.isOwnMessage(messages.MessageItem{UserID: userID}) {
		return func() tea.Msg { return statusbar.EditNotOwnMsg{} }
	}
	if channelID == "" || ts == "" {
		return nil
	}

	var stashed string
	switch panel {
	case PanelMessages:
		stashed = a.compose.Value()
		a.compose.SetValue(text)
		a.compose.SetPlaceholderOverride("Editing message — Enter to save, Esc to cancel")
	case PanelThread:
		stashed = a.threadCompose.Value()
		a.threadCompose.SetValue(text)
		a.threadCompose.SetPlaceholderOverride("Editing message — Enter to save, Esc to cancel")
	}

	a.editing = editState{
		active:       true,
		channelID:    channelID,
		ts:           ts,
		panel:        panel,
		stashedDraft: stashed,
	}
	a.SetMode(ModeInsert)
	a.focusedPanel = panel
	if panel == PanelThread {
		return a.threadCompose.Focus()
	}
	return a.compose.Focus()
}

// submitEdit emits an EditMessageMsg if the edit text is non-empty.
// Empty text refuses with an inline toast and keeps edit mode open.
func (a *App) submitEdit(rawValue, translated string) tea.Cmd {
	if strings.TrimSpace(rawValue) == "" {
		return func() tea.Msg {
			return editEmptyToastMsg{}
		}
	}
	chID := a.editing.channelID
	ts := a.editing.ts
	return func() tea.Msg {
		return EditMessageMsg{
			ChannelID: chID,
			TS:        ts,
			NewText:   translated,
		}
	}
}

// editEmptyToastMsg is delivered when the user tries to submit an
// edit with empty text.
type editEmptyToastMsg struct{}

// truncateReason returns s truncated to max characters with an ellipsis.
// Used for status-bar error toasts.
func truncateReason(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
