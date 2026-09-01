// internal/ui/keys.go
package ui

import "charm.land/bubbles/v2/key"

type KeyMap struct {
	Up                  key.Binding
	Down                key.Binding
	Left                key.Binding
	Right               key.Binding
	Enter               key.Binding
	Escape              key.Binding
	InsertMode          key.Binding
	CommandMode         key.Binding
	SearchMode          key.Binding
	SearchNext          key.Binding
	SearchPrev          key.Binding
	WorkspaceSearch     key.Binding
	Tab                 key.Binding
	ShiftTab            key.Binding
	ToggleSidebar       key.Binding
	SidebarGrow         key.Binding
	SidebarShrink       key.Binding
	ToggleThread        key.Binding
	FuzzyFinder         key.Binding
	FuzzyFinderAlt      key.Binding
	Top                 key.Binding
	Bottom              key.Binding
	PageUp              key.Binding
	PageDown            key.Binding
	HalfPageUp          key.Binding
	HalfPageDown        key.Binding
	Quit                key.Binding
	QuitConfirm         key.Binding
	CloseThreadView     key.Binding
	Reaction            key.Binding
	ReactionNav         key.Binding
	Edit                key.Binding
	Delete              key.Binding
	CopyMessage         key.Binding
	CopyPermalink       key.Binding
	OpenPreview         key.Binding
	OpenLink            key.Binding
	DownloadFile        key.Binding
	MarkUnread          key.Binding
	NextUnread          key.Binding
	PrevUnread          key.Binding
	WorkspaceFinder     key.Binding
	NewMessage          key.Binding
	ThemeSwitcher       key.Binding
	ThemeSwitcherGlobal key.Binding
	PresenceMenu        key.Binding
	ToggleSection       key.Binding
	NavBack             key.Binding
	NavForward          key.Binding
	Help                key.Binding
	SaveThread          key.Binding
	ListReactions       key.Binding
	WindowPrefix        key.Binding
	WinSplit            key.Binding
	WinVSplit           key.Binding
	WinNavigate         key.Binding
	WinCycle            key.Binding
	WinClose            key.Binding
	WinOnly             key.Binding
	ToggleBroadcast     key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:              key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/up", "up")),
		Down:            key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/down", "down")),
		Left:            key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/left", "left")),
		Right:           key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/right", "right")),
		Enter:           key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/confirm")),
		Escape:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		InsertMode:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "insert mode")),
		CommandMode:     key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command mode")),
		SearchMode:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		SearchNext:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
		SearchPrev:      key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
		WorkspaceSearch: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "search workspace")),
		Tab:             key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ShiftTab:        key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev panel")),
		ToggleSidebar:   key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "toggle sidebar")),
		SidebarGrow:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "widen sidebar")),
		SidebarShrink:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "narrow sidebar")),
		ToggleThread:    key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "toggle thread")),
		FuzzyFinder:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "switch channel")),
		FuzzyFinderAlt:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "switch channel")),
		Top:             key.NewBinding(key.WithKeys("g"), key.WithHelp("gg", "top")),
		Bottom:          key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		PageUp:          key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		PageDown:        key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "page down")),
		HalfPageUp:      key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfPageDown:    key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		Quit:            key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit (confirm)")),
		QuitConfirm:     key.NewBinding(key.WithKeys("Q"), key.WithHelp("Q", "quit (confirm)")),
		CloseThreadView: key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "close thread view")),
		Reaction:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "add reaction")),
		ReactionNav:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "navigate reactions")),
		Edit:            key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit message")),
		Delete:          key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete message")),
		CopyMessage:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy message")),
		CopyPermalink:   key.NewBinding(key.WithKeys("Y", "C"), key.WithHelp("Y/C", "copy permalink")),
		OpenPreview:     key.NewBinding(key.WithKeys("O", "v"), key.WithHelp("O/v", "open image preview")),
		OpenLink:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open link in message")),
		DownloadFile:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download file in message")),
		MarkUnread:      key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "mark unread")),
		NextUnread:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "next unread channel")),
		PrevUnread:      key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "prev unread channel")),
		// Keyless: ctrl+w is reserved as the window-command prefix
		// (window-management design §4). The keyless binding never
		// matches but keeps the help-overlay entry pointing at :ws
		// (1-9 also switch workspaces directly).
		WorkspaceFinder:     key.NewBinding(key.WithHelp(":ws", "switch workspace")),
		NewMessage:          key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new message")),
		ThemeSwitcher:       key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "switch theme (per workspace)")),
		ThemeSwitcherGlobal: key.NewBinding(key.WithKeys("ctrl+shift+y"), key.WithHelp("ctrl+shift+y", "set default theme")),
		PresenceMenu:        key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "set status")),
		ToggleSection:       key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle section")),
		NavBack:             key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "navigate back")),
		NavForward:          key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "navigate forward")),
		Help:                key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show keybindings")),
		SaveThread:          key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "save thread")),
		ListReactions:       key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "list reactions")),
		// Window commands (design §4). WindowPrefix is the only real
		// binding; the Win* entries are keyless help-only bindings
		// (same trick as WorkspaceFinder above) — actual dispatch of
		// the chord key happens in handleWindowChord.
		WindowPrefix: key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "window commands")),
		WinSplit:     key.NewBinding(key.WithHelp("ctrl+w s / :sp", "split window")),
		WinVSplit:    key.NewBinding(key.WithHelp("ctrl+w v / :vsp", "vertical split window")),
		WinNavigate:  key.NewBinding(key.WithHelp("ctrl+w h/j/k/l", "focus window in direction")),
		WinCycle:     key.NewBinding(key.WithHelp("ctrl+w w", "cycle windows")),
		WinClose:     key.NewBinding(key.WithHelp("ctrl+w q / :q", "close window")),
		WinOnly:      key.NewBinding(key.WithHelp("ctrl+w o / :only", "close other windows")),
		// Insert mode, thread compose only: toggles Slack's
		// "Also send to #channel" checkbox for the next reply.
		// Alt+Enter sends and broadcasts in a single keystroke.
		ToggleBroadcast: key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o / alt+enter", "also send reply to channel")),
	}
}
