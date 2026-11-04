package confirmprompt

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type pressedMsg struct{ n int }

func numKey(n string) key.Binding { return key.NewBinding(key.WithKeys(n)) }

// slackChoice mirrors Slackbot's "not in channel" buttons: number keys,
// nothing highlighted, other keys ignored.
func slackChoice() Choice {
	return Choice{
		Title: "Slackbot",
		Body:  "You mentioned @x, but they're not in this channel.",
		Buttons: []Button{
			{Label: "Add Them", Keys: numKey("1"), Action: func() tea.Msg { return pressedMsg{1} }},
			{Label: "Dismiss", Keys: numKey("2"), Action: func() tea.Msg { return pressedMsg{2} }},
			{Label: "Don't Show Again", Keys: numKey("3"), Action: func() tea.Msg { return pressedMsg{3} }},
		},
		Default: -1,
	}
}

func openChoice(t *testing.T, c Choice) Model {
	t.Helper()
	m := New(WithWidth(80))
	m.OpenChoice(c)
	return m
}

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	return cmd()
}

func TestChoiceShortcutPressesButton(t *testing.T) {
	t.Parallel()
	m, cmd := openChoice(t, slackChoice()).Update(keyPress('2'))
	if got := runCmd(t, cmd); got != (pressedMsg{2}) {
		t.Errorf("pressed %v, want button 2", got)
	}
	if m.IsVisible() {
		t.Error("prompt should close after a press")
	}
}

func TestChoiceEnterWithoutHighlightDoesNothing(t *testing.T) {
	t.Parallel()
	m, cmd := openChoice(t, slackChoice()).Update(keyCode(tea.KeyEnter))
	if cmd != nil || !m.IsVisible() {
		t.Errorf("Enter with nothing highlighted: cmd=%v visible=%v, want nil and open", cmd != nil, m.IsVisible())
	}
}

func TestChoiceMovementThenEnter(t *testing.T) {
	t.Parallel()
	m := openChoice(t, slackChoice())
	for _, k := range []tea.KeyPressMsg{keyPress('l'), keyCode(tea.KeyRight), keyCode(tea.KeyTab)} {
		m, _ = m.Update(k)
	}
	// l -> 0, right -> 1, tab -> 2
	if m.Highlighted() != 2 {
		t.Fatalf("Highlighted = %d, want 2", m.Highlighted())
	}
	m, _ = m.Update(keyCode(tea.KeyTab)) // wraps to 0
	m, _ = m.Update(keyPress('h'))       // wraps back to 2
	if m.Highlighted() != 2 {
		t.Fatalf("Highlighted = %d after wrap, want 2", m.Highlighted())
	}
	_, cmd := m.Update(keyCode(tea.KeyEnter))
	if got := runCmd(t, cmd); got != (pressedMsg{3}) {
		t.Errorf("Enter pressed %v, want button 3", got)
	}
}

func TestChoiceEscClosesWithoutPressing(t *testing.T) {
	t.Parallel()
	m, cmd := openChoice(t, slackChoice()).Update(keyCode(tea.KeyEscape))
	if cmd != nil || m.IsVisible() {
		t.Error("Esc should close without a command")
	}
}

func TestChoiceUnboundKey(t *testing.T) {
	t.Parallel()
	m, cmd := openChoice(t, slackChoice()).Update(keyPress('z'))
	if cmd != nil || !m.IsVisible() {
		t.Error("an unbound key should be ignored when CancelOnOtherKeys is false")
	}
	c := slackChoice()
	c.CancelOnOtherKeys = true
	m, cmd = openChoice(t, c).Update(keyPress('z'))
	if cmd != nil || m.IsVisible() {
		t.Error("an unbound key should close when CancelOnOtherKeys is true")
	}
}

func TestChoiceShortcutBeatsMovement(t *testing.T) {
	t.Parallel()
	c := slackChoice()
	c.Buttons[0].Keys = numKey("l")
	_, cmd := openChoice(t, c).Update(keyPress('l'))
	if got := runCmd(t, cmd); got != (pressedMsg{1}) {
		t.Errorf("l pressed %v, want button 1 (its shortcut)", got)
	}
}

func TestChoiceThenReplacesChoice(t *testing.T) {
	t.Parallel()
	confirm := Choice{
		Title:   "Are you sure?",
		Buttons: []Button{{Label: "Cancel", Keys: numKey("1")}, {Label: "Add", Keys: numKey("2"), Action: func() tea.Msg { return pressedMsg{9} }}},
		Default: -1,
	}
	c := slackChoice()
	c.Buttons[0].Action = nil
	c.Buttons[0].Then = &confirm
	confirm.Buttons[0].Then = &c

	m, cmd := openChoice(t, c).Update(keyPress('1'))
	if cmd != nil || !m.IsVisible() || !strings.Contains(m.View(), "Are you sure?") {
		t.Fatalf("Then should swap to the confirm step in place:\n%s", m.View())
	}
	m, _ = m.Update(keyPress('1')) // Cancel goes back
	if !strings.Contains(m.View(), "Add Them") {
		t.Fatalf("Cancel should return to the original choice:\n%s", m.View())
	}
	m, _ = m.Update(keyPress('1'))
	_, cmd = m.Update(keyPress('2'))
	if got := runCmd(t, cmd); got != (pressedMsg{9}) {
		t.Errorf("confirm OK pressed %v, want pressedMsg{9}", got)
	}
}

func TestChoiceNilActionJustCloses(t *testing.T) {
	t.Parallel()
	c := slackChoice()
	c.Buttons[1].Action = nil
	m, cmd := openChoice(t, c).Update(keyPress('2'))
	if cmd != nil || m.IsVisible() {
		t.Error("a button without Action should close with no command")
	}
}

func TestPressHighlighted(t *testing.T) {
	t.Parallel()
	m := openChoice(t, slackChoice())
	if _, cmd := m.PressHighlighted(); cmd != nil {
		t.Error("nothing highlighted: PressHighlighted should do nothing")
	}
	m.Highlight(1)
	_, cmd := m.PressHighlighted()
	if got := runCmd(t, cmd); got != (pressedMsg{2}) {
		t.Errorf("pressed %v, want button 2", got)
	}
}

func TestChoiceDefaultHighlights(t *testing.T) {
	t.Parallel()
	c := slackChoice()
	c.Default = 1
	if got := openChoice(t, c).Highlighted(); got != 1 {
		t.Errorf("Highlighted = %d, want Default 1", got)
	}
	c.Default = 7
	if got := openChoice(t, c).Highlighted(); got != -1 {
		t.Errorf("out-of-range Default: Highlighted = %d, want -1", got)
	}
}

func TestChoiceViewShowsButtons(t *testing.T) {
	t.Parallel()
	m := openChoice(t, slackChoice())
	out := m.View()
	for _, want := range []string{"Slackbot", "[1] Add Them", "[2] Dismiss", "[3] Don't Show Again"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestChoiceButtonWithoutKeysShowsBareLabel(t *testing.T) {
	t.Parallel()
	c := slackChoice()
	c.Buttons[2].Keys = key.Binding{}
	m := openChoice(t, c)
	if out := m.View(); !strings.Contains(out, "[Don't Show Again]") {
		t.Errorf("a button with no keys should render as [label]:\n%s", out)
	}
}

func TestPresetHighlightsConfirm(t *testing.T) {
	t.Parallel()
	if got := opened(t, nil).Highlighted(); got != 0 {
		t.Errorf("yes/no preset: Highlighted = %d, want 0 (Confirm)", got)
	}
}

// cellOf finds text in the rendered box and returns its box-local cell.
func cellOf(t *testing.T, m Model, text string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), row
		}
	}
	t.Fatalf("%q not in view:\n%s", text, m.View())
	return 0, 0
}

func TestButtonAt(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(40)) // narrow: the third button wraps to a second row
	m.OpenChoice(slackChoice())
	for i, text := range []string{"[1] Add Them", "[2] Dismiss", "[3] Don't Show Again"} {
		x, y := cellOf(t, m, text)
		for _, dx := range []int{0, len(text) - 1} {
			if got, ok := m.ButtonAt(x+dx, y); !ok || got != i {
				t.Errorf("ButtonAt(%d,%d) on %q = %d,%v; want %d,true", x+dx, y, text, got, ok, i)
			}
		}
	}
	x, y := cellOf(t, m, "[2] Dismiss")
	if _, ok := m.ButtonAt(x-1, y); ok {
		t.Error("the gap before a button is not a hit")
	}
	if _, ok := m.ButtonAt(x, 0); ok {
		t.Error("the top border row is not a hit")
	}
	tx, ty := cellOf(t, m, "Slackbot")
	if _, ok := m.ButtonAt(tx, ty); ok {
		t.Error("the title is not a hit")
	}
}

func TestButtonAtHidden(t *testing.T) {
	t.Parallel()
	if _, ok := New().ButtonAt(5, 5); ok {
		t.Error("a hidden prompt has no buttons")
	}
}

func TestViewWrapsLongTitle(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(40))
	m.OpenChoice(Choice{Title: strings.Repeat("title ", 12), Buttons: []Button{{Label: "OK", Keys: numKey("1")}}, Default: -1})
	x, y := cellOf(t, m, "[1] OK")
	if got, ok := m.ButtonAt(x, y); !ok || got != 0 {
		t.Errorf("with a wrapped title, ButtonAt on the button = %d,%v; want 0,true", got, ok)
	}
}

func okChoice(body string) Choice {
	return Choice{Title: "T", Body: body, Buttons: []Button{{Label: "OK", Keys: numKey("1")}}, Default: -1}
}

// A leading space makes xansi.Wrap emit a line one column too wide, which
// the box would re-wrap; the button must still be where ButtonAt says.
func TestButtonAtWithOverlongBodyLine(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80))
	m.OpenChoice(okChoice(" " + strings.Repeat("d", 50)))
	x, y := cellOf(t, m, "[1] OK")
	if got, ok := m.ButtonAt(x, y); !ok || got != 0 {
		t.Errorf("ButtonAt on the button = %d,%v; want 0,true:\n%s", got, ok, m.View())
	}
}

func TestWrapLinesNeverExceedsWidth(t *testing.T) {
	t.Parallel()
	const width = 36
	for _, in := range []string{
		"",
		"short",
		" " + strings.Repeat("d", 36),
		" " + strings.Repeat("d", 120),
		strings.Repeat("d", 120),
		"a b " + strings.Repeat("é", 80) + " tail",
		"one\n " + strings.Repeat("d", 50) + "\ntwo",
	} {
		for _, max := range []int{0, 3} {
			for i, line := range wrapLines(in, width, max) {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("wrapLines(%q, %d, %d)[%d] width = %d, want <= %d: %q", in, width, max, i, w, width, line)
				}
			}
		}
	}
}

func TestBodyRowsStayCappedWithOverlongLines(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80))
	m.OpenChoice(okChoice(" " + strings.Repeat("d", 50) + "\n" + strings.Repeat("wxyz ", 100)))
	rows := 0
	for _, l := range plainLines(m.View()) {
		if strings.ContainsAny(l, "dw") {
			rows++
		}
	}
	if rows != maxBodyLines {
		t.Errorf("body rows = %d, want %d:\n%s", rows, maxBodyLines, m.View())
	}
	x, y := cellOf(t, m, "[1] OK")
	if got, ok := m.ButtonAt(x, y); !ok || got != 0 {
		t.Errorf("ButtonAt on the button = %d,%v; want 0,true", got, ok)
	}
}

// With no body there is no body block and no second blank separator.
func TestEmptyBodyOmitsBodyBlock(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80))
	m.OpenChoice(okChoice(""))
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	titleRow := -1
	for i, r := range rows {
		if strings.Contains(r, "T") && titleRow < 0 {
			titleRow = i
		}
	}
	x, y := cellOf(t, m, "[1] OK")
	if y != titleRow+2 {
		t.Errorf("button row = %d, title row = %d; want the button two rows below the title:\n%s", y, titleRow, m.View())
	}
	if got, ok := m.ButtonAt(x, y); !ok || got != 0 {
		t.Errorf("ButtonAt on the button = %d,%v; want 0,true", got, ok)
	}
	if _, ok := m.ButtonAt(x, y-1); ok {
		t.Error("the blank separator row is not a hit")
	}
}

func tallBodyChoice() Choice {
	c := slackChoice()
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("body line %d", i+1)
	}
	c.Body = strings.Join(lines, "\n")
	return c
}

func assertButtonsHit(t *testing.T, m Model) {
	t.Helper()
	for i, text := range []string{"[1] Add Them", "[2] Dismiss", "[3] Don't Show Again"} {
		x, y := cellOf(t, m, text)
		for _, dx := range []int{0, len(text) - 1} {
			if got, ok := m.ButtonAt(x+dx, y); !ok || got != i {
				t.Errorf("ButtonAt(%d,%d) on %q = %d,%v; want %d,true", x+dx, y, text, got, ok, i)
			}
		}
	}
}

// A body that would push the buttons past the terminal's bottom edge is
// trimmed to fit.
func TestBodyFitsTerminalHeight(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80), WithHeight(12))
	m.OpenChoice(tallBodyChoice())
	if h := lipgloss.Height(m.View()); h > 12 {
		t.Errorf("height = %d, want <= 12:\n%s", h, m.View())
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "body line 1") || !strings.Contains(view, "…") {
		t.Errorf("want a trimmed body ending in an ellipsis:\n%s", view)
	}
	assertButtonsHit(t, m)
}

func TestSetHeightRefitsBody(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80))
	m.OpenChoice(tallBodyChoice())
	m.SetHeight(12)
	if h := lipgloss.Height(m.View()); h > 12 {
		t.Errorf("height = %d, want <= 12", h)
	}
}

// Height 0 is unknown: only the 8-line cap applies.
func TestUnknownHeightKeepsBodyCap(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80))
	m.OpenChoice(tallBodyChoice())
	rows := 0
	for _, l := range plainLines(m.View()) {
		if strings.Contains(l, "body line") {
			rows++
		}
	}
	if rows != maxBodyLines {
		t.Errorf("body rows = %d, want %d", rows, maxBodyLines)
	}
}

// With room for the title and buttons but no body, the body is dropped
// whole, separator included, and the buttons stay hittable.
func TestNoRoomForBodyKeepsButtons(t *testing.T) {
	t.Parallel()
	probe := New(WithWidth(80))
	probe.OpenChoice(tallBodyChoice())
	l := probe.layoutAt(boxWidth(80) - frameWidth)
	frame := 2 + 2 // border + padding, top and bottom
	h := frame + len(l.title) + 1 + buttonRowCount(l)
	m := New(WithWidth(80), WithHeight(h))
	m.OpenChoice(tallBodyChoice())
	if strings.Contains(ansi.Strip(m.View()), "body line") {
		t.Errorf("no room for a body, but one is shown:\n%s", m.View())
	}
	if got := lipgloss.Height(m.View()); got > h {
		t.Errorf("height = %d, want <= %d", got, h)
	}
	assertButtonsHit(t, m)
}

func buttonRowCount(l layout) int {
	rows := 0
	for _, p := range l.buttons {
		rows = max(rows, p.Row+1)
	}
	return rows
}
