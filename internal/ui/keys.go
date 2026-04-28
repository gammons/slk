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
	Tab                 key.Binding
	ShiftTab            key.Binding
	ToggleSidebar       key.Binding
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
	Reaction            key.Binding
	ReactionNav         key.Binding
	Edit                key.Binding
	Yank                key.Binding
	CopyPermalink       key.Binding
	WorkspaceFinder     key.Binding
	ThemeSwitcher       key.Binding
	ThemeSwitcherGlobal key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:                  key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/up", "up")),
		Down:                key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/down", "down")),
		Left:                key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/left", "left")),
		Right:               key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/right", "right")),
		Enter:               key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/confirm")),
		Escape:              key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		InsertMode:          key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "insert mode")),
		CommandMode:         key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command mode")),
		SearchMode:          key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Tab:                 key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ShiftTab:            key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev panel")),
		ToggleSidebar:       key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "toggle sidebar")),
		ToggleThread:        key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "toggle thread")),
		FuzzyFinder:         key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "fuzzy find")),
		FuzzyFinderAlt:      key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "fuzzy find")),
		Top:                 key.NewBinding(key.WithKeys("g"), key.WithHelp("gg", "top")),
		Bottom:              key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		PageUp:              key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		PageDown:            key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "page down")),
		HalfPageUp:          key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfPageDown:        key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		Quit:                key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Reaction:            key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "add reaction")),
		ReactionNav:         key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "navigate reactions")),
		Edit:                key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit message")),
		Yank:                key.NewBinding(key.WithKeys("y"), key.WithHelp("yy", "copy permalink")),
		CopyPermalink:       key.NewBinding(key.WithKeys("C"), key.WithHelp("yy/C", "copy permalink")),
		WorkspaceFinder:     key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "switch workspace")),
		ThemeSwitcher:       key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "switch theme (per workspace)")),
		ThemeSwitcherGlobal: key.NewBinding(key.WithKeys("ctrl+shift+y"), key.WithHelp("ctrl+shift+y", "set default theme")),
	}
}
