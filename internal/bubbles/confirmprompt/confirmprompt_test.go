package confirmprompt

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type sentinelMsg struct{}

func keyPress(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
func keyCode(c rune) tea.KeyPressMsg  { return tea.KeyPressMsg{Code: c} }
func shifted(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: strings.ToUpper(string(r)), Mod: tea.ModShift}
}

func opened(t *testing.T, onConfirm func() tea.Msg) Model {
	t.Helper()
	m := New(WithWidth(80))
	m.Open("Delete message?", "hello world", onConfirm)
	return m
}

func TestOpenClose(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Fatal("new prompt should be hidden")
	}
	m.Open("Delete?", "preview", nil)
	if !m.IsVisible() {
		t.Fatal("should be visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Fatal("should be hidden after Close")
	}
}

func TestUpdateConfirmRunsAction(t *testing.T) {
	for _, press := range []tea.KeyPressMsg{keyPress('y'), shifted('y'), keyCode(tea.KeyEnter)} {
		called := false
		m, cmd := opened(t, func() tea.Msg {
			called = true
			return sentinelMsg{}
		}).Update(press)

		if cmd == nil {
			t.Fatalf("%q: expected a command", press.String())
		}
		if _, ok := cmd().(sentinelMsg); !ok {
			t.Errorf("%q: command did not resolve to the registered action", press.String())
		}
		if !called {
			t.Errorf("%q: action not invoked", press.String())
		}
		if m.IsVisible() {
			t.Errorf("%q: prompt should close on confirm", press.String())
		}
	}
}

func TestUpdateCancels(t *testing.T) {
	// n/N/Esc cancel explicitly; any unbound key cancels too.
	for _, press := range []tea.KeyPressMsg{keyPress('n'), shifted('n'), keyCode(tea.KeyEscape), keyPress('z'), keyCode(tea.KeyTab)} {
		m, cmd := opened(t, func() tea.Msg { return sentinelMsg{} }).Update(press)
		if cmd != nil {
			t.Errorf("%q: expected no command on cancel", press.String())
		}
		if m.IsVisible() {
			t.Errorf("%q: prompt should close on cancel", press.String())
		}
	}
}

func TestUpdateConfirmWithoutAction(t *testing.T) {
	m, cmd := opened(t, nil).Update(keyPress('y'))
	if cmd != nil {
		t.Error("expected no command when no action was registered")
	}
	if m.IsVisible() {
		t.Error("prompt should close on confirm")
	}
}

func TestUpdateIgnoresNonKeyAndHidden(t *testing.T) {
	if _, cmd := opened(t, nil).Update(sentinelMsg{}); cmd != nil {
		t.Error("non-key message should be ignored")
	}
	m, cmd := New().Update(keyPress('y'))
	if cmd != nil || m.IsVisible() {
		t.Error("a hidden prompt should ignore keys")
	}
}

func TestUpdateHonorsCustomKeyMap(t *testing.T) {
	m := opened(t, func() tea.Msg { return sentinelMsg{} })
	m.KeyMap.Confirm = key.NewBinding(key.WithKeys("d"))

	if _, cmd := m.Update(keyPress('y')); cmd != nil {
		t.Error("y should no longer confirm")
	}
	if _, cmd := m.Update(keyPress('d')); cmd == nil {
		t.Error("d should confirm")
	}
}

func TestViewHiddenIsEmpty(t *testing.T) {
	if New().View() != "" {
		t.Error("hidden prompt should render nothing")
	}
}

func TestViewShowsTitleBodyAndHelp(t *testing.T) {
	out := opened(t, nil).View()
	for _, want := range []string{"Delete message?", "hello world", "[y] confirm", "[n/Esc] cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestViewHelpFollowsKeyMap(t *testing.T) {
	m := opened(t, nil)
	m.KeyMap.Confirm = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	if out := m.View(); !strings.Contains(out, "[d] delete") {
		t.Errorf("help text should follow the key map:\n%s", out)
	}
}

func TestViewFlattensBody(t *testing.T) {
	m := New(WithWidth(80))
	m.Open("Title", "line1\nline2\tline3", nil)

	out := m.View()
	if strings.Contains(out, "line1\nline2") {
		t.Errorf("body newlines should collapse:\n%s", out)
	}
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(out, want) {
			t.Errorf("body lost %q:\n%s", want, out)
		}
	}
}

func TestViewTruncatesLongBody(t *testing.T) {
	m := New(WithWidth(40))
	m.Open("Title", strings.Repeat("long ", 40), nil)

	out := m.View()
	if !strings.Contains(out, "…") {
		t.Errorf("over-wide body should be truncated with a tail:\n%s", out)
	}
	if got := lipgloss.Width(out); got != minWidth {
		t.Errorf("box width = %d, want %d", got, minWidth)
	}
}

func TestBoxWidthClampsToTerminalShare(t *testing.T) {
	cases := []struct{ term, want int }{
		{0, minWidth},
		{80, minWidth},  // 28% -> clamped up
		{140, 49},       // 35% of 140
		{200, maxWidth}, // 70% -> clamped down
	}
	for _, c := range cases {
		if got := boxWidth(c.term); got != c.want {
			t.Errorf("boxWidth(%d) = %d, want %d", c.term, got, c.want)
		}
	}
}

func TestSetWidthDrivesRenderedWidth(t *testing.T) {
	m := opened(t, nil)
	m.SetWidth(200)
	if got := lipgloss.Width(m.View()); got != maxWidth {
		t.Errorf("width = %d, want %d", got, maxWidth)
	}
}

func TestDefaultStylesUsable(t *testing.T) {
	for _, dark := range []bool{true, false} {
		m := New(WithStyles(DefaultStyles(dark)), WithWidth(80))
		m.Open("Title", "Body", nil)
		if m.View() == "" {
			t.Errorf("isDark=%v: default styles should render", dark)
		}
	}
}

func TestInitNoCommand(t *testing.T) {
	if New().Init() != nil {
		t.Error("Init should not schedule work")
	}
}

func TestUpdateIgnoresModifiersOnSpecialKeys(t *testing.T) {
	// A stray modifier on Enter must still confirm; on a printable key
	// the shifted text is the binding, so Shift+y is "Y".
	m, cmd := opened(t, func() tea.Msg { return sentinelMsg{} }).
		Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if cmd == nil {
		t.Error("shift+enter should confirm")
	}
	if m.IsVisible() {
		t.Error("prompt should close")
	}

	if _, cmd := opened(t, func() tea.Msg { return sentinelMsg{} }).
		Update(tea.KeyPressMsg{Code: 'z', Text: "z", Mod: tea.ModCtrl}); cmd != nil {
		t.Error("ctrl+z should cancel, not confirm")
	}
}

func TestNewAppliesOptions(t *testing.T) {
	styles := DefaultStyles(false)
	styles.BaseANSI = "\x1b[0m"
	keys := KeyMap{
		Confirm: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Cancel:  key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "keep")),
	}

	m := New(WithStyles(styles), WithKeyMap(keys), WithWidth(200))
	if got := m.Styles().BaseANSI; got != styles.BaseANSI {
		t.Errorf("BaseANSI = %q, want %q", got, styles.BaseANSI)
	}

	m.Open("Title", "Body", func() tea.Msg { return sentinelMsg{} })
	if got := lipgloss.Width(m.View()); got != maxWidth {
		t.Errorf("width = %d, want %d from WithWidth(200)", got, maxWidth)
	}
	if !strings.Contains(m.View(), "[d] delete") {
		t.Error("help text should come from the supplied key map")
	}
	if _, cmd := m.Update(keyPress('d')); cmd == nil {
		t.Error("d should confirm with the supplied key map")
	}
}

func TestNewDefaultsWithoutOptions(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("should start hidden")
	}
	m.Open("Title", "Body", nil)
	// No width set: the box floors at its minimum.
	if got := lipgloss.Width(m.View()); got != minWidth {
		t.Errorf("width = %d, want the %d floor", got, minWidth)
	}
	if !strings.Contains(m.View(), "[y] confirm") {
		t.Error("default key map should drive the help text")
	}
}
