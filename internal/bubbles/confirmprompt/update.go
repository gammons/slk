package confirmprompt

import (
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update handles one key press; anything that is not Confirm cancels. The
// prompt closes itself either way, so callers check IsVisible.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.visible {
		return m, nil
	}

	var cmd tea.Cmd
	if m.confirms(press) && m.onConfirm != nil {
		fn := m.onConfirm
		cmd = func() tea.Msg { return fn() }
	}
	m.Close()
	return m, cmd
}

// confirms matches the Confirm binding. A modifier on a non-printable key is
// ignored, so a stray shift on Enter still confirms. On a printable key the
// modifier is part of the key: ctrl+y and alt+y are not y. The test is on the
// base key's Code, not on Text, because bubbletea leaves Text empty for a
// ctrl- or alt-modified letter too.
func (m Model) confirms(press tea.KeyPressMsg) bool {
	if key.Matches(press, m.KeyMap.Confirm) {
		return true
	}
	if press.Mod == 0 || printable(press.Code) {
		return false
	}
	press.Mod = 0
	return key.Matches(press, m.KeyMap.Confirm)
}

// printable reports whether code is a printable character rather than a
// special key (Enter, Esc, arrows, ...). Special keys above unicode.MaxRune
// are never printable.
func printable(code rune) bool {
	return code <= unicode.MaxRune && unicode.IsPrint(code)
}
