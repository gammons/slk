// internal/ui/app.go
package ui

import (
	"fmt"
	"log"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/thread"
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

// Messages sent between components
type (
	ChannelSelectedMsg struct {
		ID   string
		Name string
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
		Channels    []sidebar.ChannelItem
		FinderItems []channelfinder.Item
		UserNames   map[string]string
		UserID      string
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

// ThreadFetchFunc is called when the user opens a thread.
type ThreadFetchFunc func(channelID, threadTS string) tea.Msg

// ThreadReplySendFunc is called when the user sends a thread reply.
type ThreadReplySendFunc func(channelID, threadTS, text string) tea.Msg

type ReactionAddFunc func(channelID, messageTS, emoji string) error
type ReactionRemoveFunc func(channelID, messageTS, emoji string) error
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

	// State
	mode           Mode
	focusedPanel   Panel
	sidebarVisible bool
	threadVisible  bool
	width          int
	height         int
	keys           KeyMap

	// Cached layout widths for mouse hit-testing
	layoutRailWidth    int
	layoutSidebarEnd   int // railWidth + sidebarWidth + sidebarBorder
	layoutMsgEnd       int // layoutSidebarEnd + msgWidth + msgBorder
	layoutThreadEnd    int // layoutMsgEnd + threadWidth + threadBorder

	// Current context
	activeChannelID string
	activeTeamID    string // workspace whose data is currently loaded into the side panels

	// Callbacks
	channelFetcher       ChannelFetchFunc
	olderMessagesFetcher OlderMessagesFetchFunc
	messageSender        MessageSendFunc
	threadFetcher        ThreadFetchFunc
	threadReplySender    ThreadReplySendFunc
	channelJoiner        JoinChannelFunc
	fetchingOlder        bool

	// Reaction picker
	reactionPicker   *reactionpicker.Model
	reactionAddFn    ReactionAddFunc
	reactionRemoveFn ReactionRemoveFunc
	frecentLoadFn    FrecentLoadFunc
	frecentRecordFn  FrecentRecordFunc
	currentUserID    string

	// Workspace switching
	workspaceSwitcher SwitchWorkspaceFunc
	workspaceItems    []workspace.WorkspaceItem // cached for lookup

	// Theme switching
	themeSaveFn    func(name string)
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
}

func NewApp() *App {
	return &App{
		workspaceRail:   workspace.New(nil, 0),
		sidebar:         sidebar.New(nil),
		messagepane:     messages.New(nil, ""),
		compose:         compose.New(""),
		statusbar:       statusbar.New(),
		channelFinder:   channelfinder.New(),
		workspaceFinder: workspacefinder.New(),
		themeSwitcher:   themeswitcher.New(),
		threadPanel:     thread.New(),
		threadCompose:   compose.New("thread"),
		reactionPicker:  reactionpicker.New(),
		mode:            ModeNormal,
		focusedPanel:    PanelSidebar,
		sidebarVisible:  true,
		keys:            DefaultKeyMap(),
		typingUsers:     make(map[string]map[string]time.Time),
	}
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

	// Handle Shift+Enter from terminals using CSI u mode.
	// Shift+Enter sends ESC[13;2u which bubbletea reports as unknownCSISequenceMsg.
	// That type is unexported, so we detect it via fmt.Stringer.
	// Its String() returns "?CSI[49 51 59 50 117]?" for the bytes "13;2u".
	if a.mode == ModeInsert {
		if s, ok := msg.(fmt.Stringer); ok {
			if s.String() == "?CSI[49 51 59 50 117]?" {
				enterMsg := tea.KeyPressMsg{Code: tea.KeyEnter}
				if a.focusedPanel == PanelThread && a.threadVisible {
					a.threadCompose, _ = a.threadCompose.Update(enterMsg)
				} else {
					a.compose, _ = a.compose.Update(enterMsg)
				}
				return a, nil
			}
		}
	}

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

	case ChannelSelectedMsg:
		// Close thread panel when switching channels
		a.CloseThread()
		a.activeChannelID = msg.ID
		a.lastTypingSent = time.Time{} // reset typing throttle for new channel
		a.messagepane.SetChannel(msg.Name, "")
		a.messagepane.SetLoading(true)
		a.messagepane.SetMessages(nil) // clear while loading
		a.compose.SetChannel(msg.Name)
		a.statusbar.SetChannel(msg.Name)
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

	case ThreadRepliesLoadedMsg:
		if a.threadVisible && msg.ThreadTS == a.threadPanel.ThreadTS() {
			a.threadPanel.SetThread(a.threadPanel.ParentMsg(), msg.Replies, a.threadPanel.ChannelID(), msg.ThreadTS)
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
		a.CloseThread()
		a.compose.Reset()
		a.messagepane.SetMessages(nil)
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.sidebar.SetItems(msg.Channels)
		a.channelFinder.SetItems(msg.FinderItems)
		a.SetUserNames(msg.UserNames)
		a.currentUserID = msg.UserID
		a.activeTeamID = msg.TeamID
		a.workspaceRail.SelectByID(msg.TeamID)
		// Load first channel
		if len(msg.Channels) > 0 {
			first := msg.Channels[0]
			cmds = append(cmds, func() tea.Msg {
				return ChannelSelectedMsg{ID: first.ID, Name: first.Name}
			})
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
		// If this is the first workspace, set it up as active
		if a.activeChannelID == "" {
			a.sidebar.SetItems(msg.Channels)
			a.channelFinder.SetItems(msg.FinderItems)
			a.SetUserNames(msg.UserNames)
			a.currentUserID = msg.UserID
			a.activeTeamID = msg.TeamID
			a.workspaceRail.SelectByID(msg.TeamID)
			if len(msg.Channels) > 0 {
				first := msg.Channels[0]
				cmds = append(cmds, func() tea.Msg {
					return ChannelSelectedMsg{ID: first.ID, Name: first.Name}
				})
			}
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
			return ChannelSelectedMsg{ID: msg.ID, Name: msg.Name}
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
		if a.focusedPanel == PanelThread {
			return a.threadCompose.Focus()
		}
		a.focusedPanel = PanelMessages
		return a.compose.Focus()

	case key.Matches(msg, a.keys.Escape):
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
		a.handleDown()

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
		a.handleGoToBottom()

	case key.Matches(msg, a.keys.WorkspaceFinder):
		a.workspaceFinder.Open()
		a.SetMode(ModeWorkspaceFinder)

	case key.Matches(msg, a.keys.ThemeSwitcher):
		a.themeSwitcher.Open()
		a.SetMode(ModeThemeSwitcher)

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
	if key.Matches(msg, a.keys.Escape) {
		// If mention picker is active, close it instead of exiting insert mode
		if a.focusedPanel == PanelThread && a.threadVisible && a.threadCompose.IsMentionActive() {
			a.threadCompose.CloseMention()
			return nil
		}
		if a.focusedPanel != PanelThread && a.compose.IsMentionActive() {
			a.compose.CloseMention()
			return nil
		}
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}

	isSend := msg.Key().Code == tea.KeyEnter
	isNewline := msg.Key().Code == 'j' && msg.Key().Mod == tea.ModCtrl

	// Determine which compose box is active based on focused panel
	if a.focusedPanel == PanelThread && a.threadVisible {
		// If mention picker is active, forward all keys to compose (including Enter)
		if a.threadCompose.IsMentionActive() {
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
	// If mention picker is active, forward all keys to compose (including Enter)
	if a.compose.IsMentionActive() {
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
				return ChannelSelectedMsg{ID: result.ID, Name: result.Name}
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
			go a.themeSaveFn(result.Name)
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

func (a *App) handleDown() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveDown()
	case PanelMessages:
		a.messagepane.MoveDown()
	case PanelThread:
		a.threadPanel.MoveDown()
	}
}

func (a *App) handleUp() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveUp()
	case PanelMessages:
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

func (a *App) handleGoToBottom() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.GoToBottom()
	case PanelMessages:
		a.messagepane.GoToBottom()
	case PanelThread:
		a.threadPanel.GoToBottom()
	}
}

func (a *App) handleEnter() tea.Cmd {
	if a.focusedPanel == PanelSidebar {
		item, ok := a.sidebar.SelectedItem()
		if ok {
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: item.ID, Name: item.Name}
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
	a.mode = mode
	a.statusbar.SetMode(mode)
}

func (a *App) FocusNext() {
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
	a.sidebarVisible = !a.sidebarVisible
	if !a.sidebarVisible && a.focusedPanel == PanelSidebar {
		a.focusedPanel = PanelMessages
	}
}

func (a *App) ToggleThread() {
	if a.threadVisible {
		a.CloseThread()
	}
	// Don't open on toggle if no thread is loaded -- use Enter for that
}

func (a *App) CloseThread() {
	a.threadVisible = false
	a.statusbar.SetInThread(false)
	a.threadPanel.Clear()
	a.threadCompose.Blur()
	if a.focusedPanel == PanelThread {
		a.focusedPanel = PanelMessages
	}
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

// SetThreadFetcher sets the callback used to load thread replies.
func (a *App) SetThreadFetcher(fn ThreadFetchFunc) {
	a.threadFetcher = fn
}

// SetThreadReplySender sets the callback used to send thread replies.
func (a *App) SetThreadReplySender(fn ThreadReplySendFunc) {
	a.threadReplySender = fn
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

func (a *App) SetCurrentUserID(userID string) {
	a.currentUserID = userID
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

// SetThemeSaver sets the callback for saving the theme selection.
func (a *App) SetThemeSaver(fn func(name string)) {
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

	// Helper to force a panel to an exact width and height.
	// Uses an explicit width parameter instead of lipgloss.Width(s)
	// to avoid ANSI miscounting in complex rendered content.
	exactSize := func(s string, w, h int) string {
		return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Background(styles.Background).Render(s)
	}

	// Render workspace rail
	rail := exactSize(a.workspaceRail.View(contentHeight), railWidth, contentHeight)

	var panels []string
	panels = append(panels, rail)

	// Render sidebar
	if a.sidebarVisible {
		borderStyle := styles.UnfocusedBorder.Width(sidebarWidth)
		if a.focusedPanel == PanelSidebar && a.mode != ModeInsert {
			borderStyle = styles.FocusedBorder.Width(sidebarWidth)
		}
		sidebarView := a.sidebar.View(contentHeight-2, sidebarWidth)
		sidebarView = borderStyle.Render(sidebarView)
		panels = append(panels, exactSize(sidebarView, sidebarWidth+sidebarBorder, contentHeight))
	}

	// Render message pane with border
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if a.focusedPanel == PanelMessages && a.mode != ModeInsert {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
	a.compose.SetWidth(msgWidth - 2)
	composeView := a.compose.View(msgWidth-2, a.mode == ModeInsert && a.focusedPanel != PanelThread)
	mentionView := a.compose.MentionPickerView(msgWidth - 2)
	if mentionView != "" {
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
	msgPanel := exactSize(
		msgBorderStyle.Render(msgInner),
		msgWidth+msgBorder, contentHeight,
	)
	panels = append(panels, msgPanel)

	// Render thread panel if visible
	if a.threadVisible && threadWidth > 0 {
		threadBorderStyle := styles.UnfocusedBorder.Width(threadWidth)
		if a.focusedPanel == PanelThread && a.mode != ModeInsert {
			threadBorderStyle = styles.FocusedBorder.Width(threadWidth)
		}
		a.threadCompose.SetWidth(threadWidth - 2)
		threadComposeView := a.threadCompose.View(threadWidth-2, a.mode == ModeInsert && a.focusedPanel == PanelThread)
		threadMentionView := a.threadCompose.MentionPickerView(threadWidth - 2)
		if threadMentionView != "" {
			threadComposeView = threadMentionView + "\n" + threadComposeView
		}
		threadComposeSpacer := lipgloss.NewStyle().Background(styles.Background).Width(threadWidth - 2).Render("")
		threadComposeView = threadComposeSpacer + "\n" + threadComposeView
		threadComposeHeight := lipgloss.Height(threadComposeView)
		threadContentHeight := contentHeight - 2 - threadComposeHeight
		if threadContentHeight < 3 {
			threadContentHeight = 3
		}
		threadView := a.threadPanel.View(threadContentHeight, threadWidth-2)
		threadInner := lipgloss.JoinVertical(lipgloss.Left, threadView, threadComposeView)
		threadInner = messages.ReapplyBgAfterResets(threadInner, messages.BgANSI())
		threadPanel := exactSize(
			threadBorderStyle.Render(threadInner),
			threadWidth+threadBorder, contentHeight,
		)
		panels = append(panels, threadPanel)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	statusWidth := a.width - railWidth
	railSpacer := lipgloss.NewStyle().
		Width(railWidth).
		Background(styles.SurfaceDark).
		Render("")
	status := lipgloss.JoinHorizontal(lipgloss.Center, railSpacer, a.statusbar.View(statusWidth))

	screen := lipgloss.JoinVertical(lipgloss.Left, content, status)

	// Render channel finder overlay on top of existing layout
	if a.channelFinder.IsVisible() {
		screen = a.channelFinder.ViewOverlay(a.width, a.height, screen)
	}

	if a.reactionPicker.IsVisible() {
		screen = a.reactionPicker.ViewOverlay(a.width, a.height, screen)
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

	// Fill any uncolored cells with the theme background color.
	// Use a full-screen wrapper that forces exact dimensions and fills
	// all padding with the theme background. This catches any width gaps
	// between panels and height gaps below content.
	finalScreen := lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		MaxHeight(a.height).
		Background(styles.Background).
		Render(screen)
	v := tea.NewView(finalScreen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
