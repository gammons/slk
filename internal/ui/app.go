// internal/ui/app.go
package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/channelfinder"
	"github.com/gammons/slack-tui/internal/ui/compose"
	"github.com/gammons/slack-tui/internal/ui/messages"
	"github.com/gammons/slack-tui/internal/ui/sidebar"
	"github.com/gammons/slack-tui/internal/ui/statusbar"
	"github.com/gammons/slack-tui/internal/ui/styles"
	"github.com/gammons/slack-tui/internal/ui/thread"
	"github.com/gammons/slack-tui/internal/ui/workspace"
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
		ChannelID string
		Messages  []messages.MessageItem
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
)

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

type App struct {
	// Sub-models
	workspaceRail workspace.Model
	sidebar       sidebar.Model
	messagepane   messages.Model
	compose       compose.Model
	statusbar     statusbar.Model
	channelFinder channelfinder.Model
	threadPanel   *thread.Model
	threadCompose compose.Model

	// State
	mode           Mode
	focusedPanel   Panel
	sidebarVisible bool
	threadVisible  bool
	width          int
	height         int
	keys           KeyMap

	// Current context
	activeChannelID string

	// Callbacks
	channelFetcher       ChannelFetchFunc
	olderMessagesFetcher OlderMessagesFetchFunc
	messageSender        MessageSendFunc
	threadFetcher        ThreadFetchFunc
	threadReplySender    ThreadReplySendFunc
	fetchingOlder        bool
}

func NewApp() *App {
	return &App{
		workspaceRail:  workspace.New(nil, 0),
		sidebar:        sidebar.New(nil),
		messagepane:    messages.New(nil, ""),
		compose:        compose.New(""),
		statusbar:      statusbar.New(),
		channelFinder:  channelfinder.New(),
		threadPanel:    thread.New(),
		threadCompose:  compose.New("thread"),
		mode:           ModeNormal,
		focusedPanel:   PanelSidebar,
		sidebarVisible: true,
		keys:           DefaultKeyMap(),
	}
}

func (a *App) Init() tea.Cmd {
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
				enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
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

	case ChannelSelectedMsg:
		// Close thread panel when switching channels
		a.CloseThread()
		a.activeChannelID = msg.ID
		a.messagepane.SetChannel(msg.Name, "")
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
	}

	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Always handle quit
	if key.Matches(msg, a.keys.Quit) {
		return tea.Quit
	}

	// Mode-specific handling
	switch a.mode {
	case ModeInsert:
		return a.handleInsertMode(msg)
	case ModeCommand:
		return a.handleCommandMode(msg)
	case ModeChannelFinder:
		return a.handleChannelFinderMode(msg)
	default:
		return a.handleNormalMode(msg)
	}
}

func (a *App) handleNormalMode(msg tea.KeyMsg) tea.Cmd {
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

	case key.Matches(msg, a.keys.FuzzyFinder) || key.Matches(msg, a.keys.FuzzyFinderAlt):
		a.channelFinder.Open()
		a.SetMode(ModeChannelFinder)
	}
	return nil
}

func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}

	// Enter sends the message.
	// Ctrl+J inserts a newline (LF byte 0x0A is always distinguishable from
	// Enter/CR byte 0x0D, unlike Shift+Enter which most terminals can't detect).
	isSend := msg.Type == tea.KeyEnter
	isNewline := msg.Type == tea.KeyCtrlJ

	// Determine which compose box is active based on focused panel
	if a.focusedPanel == PanelThread && a.threadVisible {
		// Thread reply compose
		if isNewline {
			// Insert newline by passing a plain Enter to the textarea
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(tea.KeyMsg{Type: tea.KeyEnter})
			return cmd
		}
		if isSend {
			text := a.threadCompose.Value()
			if text != "" {
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
		return cmd
	}

	// Channel message compose
	if isNewline {
		// Insert newline by passing a plain Enter to the textarea
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return cmd
	}
	if isSend {
		text := a.compose.Value()
		if text != "" {
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

	// Forward other keys to compose box
	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
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
	if msg.Type == tea.KeyEnter {
		keyStr = "enter"
	} else if msg.Type == tea.KeyEsc {
		keyStr = "esc"
	} else if msg.Type == tea.KeyUp {
		keyStr = "up"
	} else if msg.Type == tea.KeyDown {
		keyStr = "down"
	} else if msg.Type == tea.KeyBackspace {
		keyStr = "backspace"
	}

	result := a.channelFinder.HandleKey(keyStr)
	if result != nil {
		a.channelFinder.Close()
		a.SetMode(ModeNormal)
		a.sidebar.SelectByID(result.ID)
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: result.ID, Name: result.Name}
		}
	}

	// Check if finder closed itself (Esc)
	if !a.channelFinder.IsVisible() {
		a.SetMode(ModeNormal)
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

// Setters for external use (wiring services)
func (a *App) SetWorkspaces(items []workspace.WorkspaceItem) {
	a.workspaceRail.SetItems(items)
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
}

// SetInitialChannel sets the active channel and its messages before the TUI starts.
func (a *App) SetInitialChannel(channelID, channelName string, msgs []messages.MessageItem) {
	a.activeChannelID = channelID
	a.messagepane.SetChannel(channelName, "")
	a.messagepane.SetMessages(msgs)
	a.compose.SetChannel(channelName)
	a.statusbar.SetChannel(channelName)
}

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Initializing..."
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

	// Helper to force a panel to an exact height
	exactHeight := func(s string, h int) string {
		return lipgloss.NewStyle().Width(lipgloss.Width(s)).Height(h).MaxHeight(h).Render(s)
	}

	// Render workspace rail
	rail := exactHeight(a.workspaceRail.View(contentHeight), contentHeight)

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
		panels = append(panels, exactHeight(sidebarView, contentHeight))
	}

	// Render message pane with border
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if a.focusedPanel == PanelMessages && a.mode != ModeInsert {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
	composeView := a.compose.View(msgWidth-2, a.mode == ModeInsert && a.focusedPanel != PanelThread)
	composeHeight := lipgloss.Height(composeView)
	msgContentHeight := contentHeight - 2 - composeHeight
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	msgView := a.messagepane.View(msgContentHeight, msgWidth-2)
	msgInner := lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
	msgPanel := exactHeight(
		msgBorderStyle.Render(msgInner),
		contentHeight,
	)
	panels = append(panels, msgPanel)

	// Render thread panel if visible
	if a.threadVisible && threadWidth > 0 {
		threadBorderStyle := styles.UnfocusedBorder.Width(threadWidth)
		if a.focusedPanel == PanelThread && a.mode != ModeInsert {
			threadBorderStyle = styles.FocusedBorder.Width(threadWidth)
		}
		threadComposeView := a.threadCompose.View(threadWidth-2, a.mode == ModeInsert && a.focusedPanel == PanelThread)
		threadComposeHeight := lipgloss.Height(threadComposeView)
		threadContentHeight := contentHeight - 2 - threadComposeHeight
		if threadContentHeight < 3 {
			threadContentHeight = 3
		}
		threadView := a.threadPanel.View(threadContentHeight, threadWidth-2)
		threadInner := lipgloss.JoinVertical(lipgloss.Left, threadView, threadComposeView)
		threadPanel := exactHeight(
			threadBorderStyle.Render(threadInner),
			contentHeight,
		)
		panels = append(panels, threadPanel)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	status := a.statusbar.View(a.width)

	screen := lipgloss.JoinVertical(lipgloss.Left, content, status)

	// Render channel finder overlay on top of existing layout
	if a.channelFinder.IsVisible() {
		screen = a.channelFinder.ViewOverlay(a.width, a.height, screen)
	}

	return screen
}
