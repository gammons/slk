package confirmprompt

import "charm.land/bubbles/v2/key"

// KeyMap is the prompt's key bindings. Anything that is not Confirm cancels,
// so Cancel's keys feed the footer rather than the match.
type KeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// DefaultKeyMap returns the default bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Confirm: key.NewBinding(
			key.WithKeys("y", "Y", "enter"),
			key.WithHelp("y", "confirm"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("n", "N", "esc"),
			key.WithHelp("n/Esc", "cancel"),
		),
	}
}
