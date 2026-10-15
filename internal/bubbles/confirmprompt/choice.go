package confirmprompt

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Button is one choice in the prompt.
type Button struct {
	Label string
	// Hint is the key shown beside the label, e.g. "1". Empty shows the
	// first of Keys' keys, or no hint when Keys has none.
	Hint string
	// Keys press the button directly.
	Keys key.Binding
	// Action is returned as a tea.Cmd when the button is pressed. Nil
	// means nothing to run.
	Action func() tea.Msg
	// Then, when set, replaces the prompt's choice on press instead of
	// closing it (e.g. a confirmation step). Action still runs.
	Then *Choice
}

// Choice is everything a prompt shows.
type Choice struct {
	Title, Body string
	Buttons     []Button
	// Default is the button highlighted on open; -1 (or out of range)
	// highlights none, so Enter does nothing until the user moves.
	Default int
	// CancelOnOtherKeys closes the prompt on any key that is not a
	// button's. It also turns off Enter-on-highlight and movement, so
	// only button keys and Esc act.
	CancelOnOtherKeys bool
}

// The yes/no preset's buttons, in display order.
const (
	presetConfirm = 0
	presetCancel  = 1
)

// button returns button i. In the yes/no preset its keys, hint and label
// come from KeyMap, which callers may change after Open.
func (m Model) button(i int) Button {
	b := m.choice.Buttons[i]
	if !m.preset {
		return b
	}
	kb := m.KeyMap.Cancel
	if i == presetConfirm {
		kb = m.KeyMap.Confirm
	}
	h := kb.Help()
	b.Keys, b.Hint, b.Label = kb, h.Key, h.Desc
	return b
}

// label is button i as drawn: "[hint] label", or "[label]" with no hint.
func (m Model) label(i int) string {
	hint, label := m.labelParts(i)
	if hint == "" {
		return "[" + label + "]"
	}
	return "[" + hint + "] " + label
}

// labelParts is the resolved hint and label of button i. It does not
// allocate, so View can compare it with the cached render's on every frame.
func (m Model) labelParts(i int) (hint, label string) {
	b := m.button(i)
	hint = b.Hint
	if hint == "" {
		if ks := b.Keys.Keys(); len(ks) > 0 {
			hint = ks[0]
		}
	}
	return hint, b.Label
}
