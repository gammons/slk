package confirmprompt

import (
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

// confirms matches the Confirm binding, ignoring modifiers on non-printable
// keys so a stray shift on Enter still confirms.
func (m Model) confirms(press tea.KeyPressMsg) bool {
	if key.Matches(press, m.KeyMap.Confirm) {
		return true
	}
	if press.Text != "" || press.Mod == 0 {
		return false
	}
	press.Mod = 0
	return key.Matches(press, m.KeyMap.Confirm)
}
