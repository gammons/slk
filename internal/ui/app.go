// internal/ui/app.go
package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/compose"
	"github.com/gammons/slack-tui/internal/ui/messages"
	"github.com/gammons/slack-tui/internal/ui/sidebar"
	"github.com/gammons/slack-tui/internal/ui/statusbar"
	"github.com/gammons/slack-tui/internal/ui/styles"
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
	NewMessageMsg struct {
		Message messages.MessageItem
	}
	SendMessageMsg struct {
		ChannelID string
		Text      string
	}
)

// ChannelFetchFunc is called when the user selects a channel.
// It should return a tea.Msg (typically MessagesLoadedMsg) with the channel's messages.
type ChannelFetchFunc func(channelID, channelName string) tea.Msg

type App struct {
	// Sub-models
	workspaceRail workspace.Model
	sidebar       sidebar.Model
	messagepane   messages.Model
	compose       compose.Model
	statusbar     statusbar.Model

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
	channelFetcher ChannelFetchFunc
}

func NewApp() *App {
	return &App{
		workspaceRail:  workspace.New(nil, 0),
		sidebar:        sidebar.New(nil),
		messagepane:    messages.New(nil, ""),
		compose:        compose.New(""),
		statusbar:      statusbar.New(),
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

	case NewMessageMsg:
		a.messagepane.AppendMessage(msg.Message)
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
	default:
		return a.handleNormalMode(msg)
	}
}

func (a *App) handleNormalMode(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.InsertMode):
		a.SetMode(ModeInsert)
		a.focusedPanel = PanelMessages
		return a.compose.Focus()

	case key.Matches(msg, a.keys.Escape):
		a.SetMode(ModeNormal)
		a.compose.Blur()

	case key.Matches(msg, a.keys.Tab):
		a.FocusNext()

	case key.Matches(msg, a.keys.ShiftTab):
		a.FocusPrev()

	case key.Matches(msg, a.keys.ToggleSidebar):
		a.ToggleSidebar()

	case key.Matches(msg, a.keys.Down):
		a.handleDown()

	case key.Matches(msg, a.keys.Up):
		a.handleUp()

	case key.Matches(msg, a.keys.Left):
		a.FocusPrev()

	case key.Matches(msg, a.keys.Right):
		a.FocusNext()

	case key.Matches(msg, a.keys.Enter):
		return a.handleEnter()

	case key.Matches(msg, a.keys.Bottom):
		a.handleGoToBottom()
	}
	return nil
}

func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.SetMode(ModeNormal)
		a.compose.Blur()
		return nil
	}

	// Handle Enter in insert mode to send message
	if msg.Type == tea.KeyEnter {
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

	// Forward to compose box
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

func (a *App) handleDown() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveDown()
	case PanelMessages:
		a.messagepane.MoveDown()
	}
}

func (a *App) handleUp() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveUp()
	case PanelMessages:
		a.messagepane.MoveUp()
	}
}

func (a *App) handleGoToBottom() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.GoToBottom()
	case PanelMessages:
		a.messagepane.GoToBottom()
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
	return nil
}

func (a *App) SetMode(mode Mode) {
	a.mode = mode
	a.statusbar.SetMode(mode)
}

func (a *App) FocusNext() {
	if a.sidebarVisible {
		if a.focusedPanel == PanelSidebar {
			a.focusedPanel = PanelMessages
		} else {
			a.focusedPanel = PanelSidebar
		}
	}
}

func (a *App) FocusPrev() {
	if a.sidebarVisible {
		if a.focusedPanel == PanelMessages {
			a.focusedPanel = PanelSidebar
		} else {
			a.focusedPanel = PanelMessages
		}
	}
}

func (a *App) ToggleSidebar() {
	a.sidebarVisible = !a.sidebarVisible
	if !a.sidebarVisible && a.focusedPanel == PanelSidebar {
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

	// Calculate widths
	railWidth := a.workspaceRail.Width()
	sidebarWidth := 0
	if a.sidebarVisible {
		sidebarWidth = a.sidebar.Width()
	}
	msgWidth := a.width - railWidth - sidebarWidth
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

	// Render sidebar -- always show border, change color on focus
	if a.sidebarVisible {
		borderStyle := styles.UnfocusedBorder.Width(sidebarWidth)
		if a.focusedPanel == PanelSidebar {
			borderStyle = styles.FocusedBorder.Width(sidebarWidth)
		}
		sidebarView := a.sidebar.View(contentHeight-2, sidebarWidth) // -2 for top+bottom border
		sidebarView = borderStyle.Render(sidebarView)
		panels = append(panels, exactHeight(sidebarView, contentHeight))
	}

	// Render message pane: compose first (to measure), then messages get the rest
	composeView := a.compose.View(msgWidth, a.mode == ModeInsert)
	composeHeight := lipgloss.Height(composeView)
	msgContentHeight := contentHeight - composeHeight
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	msgView := a.messagepane.View(msgContentHeight, msgWidth)
	msgPanel := exactHeight(
		lipgloss.JoinVertical(lipgloss.Left, msgView, composeView),
		contentHeight,
	)
	panels = append(panels, msgPanel)

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	status := a.statusbar.View(a.width)

	return lipgloss.JoinVertical(lipgloss.Left, content, status)
}
