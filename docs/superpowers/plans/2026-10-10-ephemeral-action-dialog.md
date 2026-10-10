# Ephemeral Action Dialog (Part B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the legacy-attachment buttons on ephemeral ("only visible to you") messages pressable, through a dialog that opens on its own, by mouse and keyboard.

**Architecture:**
- **Component.** `internal/bubbles/confirmprompt` grows from yes/no into a prompt of N buttons, each carrying an action closure. Yes/no becomes a preset with unchanged behaviour. The button-row layout moves into a new shared `internal/bubbles/buttonrow` package, so the Block Kit renderer and the prompt use one implementation.
- **App.** In `internal/ui`, the app queues ephemerals with buttons and opens them when the user isn't typing. Each button's action calls a new `core.InteractionService` port, wired in `cmd/slk` to Part A's `Client.AttachmentAction`.

**Tech Stack:** Go 1.26, bubbletea v2, bubbles v2 `key`, lipgloss v2, `charmbracelet/x/ansi`, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-10-10-ephemeral-action-dialog-design.md`. Read it first. Part A's spec, `docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md`, holds the captured Slack frames.

**Branch:** `feat/ephemeral-action-dialog` (worktree `/home/dev/local_code/slk/.worktrees/ephemeral-action-dialog`). It is built on PR #298's branch with `main` merged in, and is rebased onto `main` when #298 merges.

## Plan refinements of the spec

The spec is the authority; these are the places the plan settles detail it left open.

1. **`Button.Keys` is a `key.Binding`, not `[]string`.** It's the bubbles idiom, and it reuses #302's modifier rule through one `matches` helper.
2. **`Button.Hint` and `Button.Then` are added.**
   - `Hint` is the key shown beside the label. The yes/no preset takes it from `KeyMap`'s help text, so the rendered text stays exactly `[y] confirm   [n/Esc] cancel`.
   - `Then` is a `*Choice` that replaces the prompt on press instead of closing it. Slack's confirm step and its "back" use it, entirely inside the component.
3. **Enter-on-highlight and movement are off when `CancelOnOtherKeys` is true.** The spec stated this for movement; the plan extends it to Enter-on-highlight, so the yes/no preset's keys stay exactly #302's, including custom key maps.
4. **Mouse presses the clicked button directly.** The router's usual "synthesise Enter" would press *Confirm* in a yes/no prompt, because Enter is a Confirm key. So a click on Cancel would quit. The router gains an `onPoint` press callback for this prompt.
5. **Button text is `[hint] label`**, or `[label]` with no hint, three spaces apart. The yes/no footer text is therefore unchanged.
6. **The yes/no body changes.** It wraps (up to 8 lines) instead of being flattened to one line, and loses its `> ` prefix. This is the spec's rendering change. `TestViewFlattensBody` and `TestViewTruncatesLongBody` pin the old rendering and are rewritten. Every key test stays unchanged.

## Global Constraints

- Tests are plain `testing.T`, stdlib only, white-box (`package <pkg>`). No testify. New `internal/bubbles` tests call `t.Parallel()`.
- `internal/bubbles/**` imports neither `internal/ui/...` nor any I/O package; `internal/ui/boundary_test.go` enforces this. `internal/ui` does no I/O: pressing goes through a `core` port.
- **Never add a second implementation.** One button-row layout (`internal/bubbles/buttonrow`), one dialog (`confirmprompt`), one modifier rule (`matches`).
- App behaviour goes through the reducer chain. Add nothing post-chain in `App.Update`, and never call `tryOpenActionPrompt` from `SetMode`.
- Change `messages.Model` and `thread.Model` together.
- A new reusable helper gets an `AGENTS.md` shared-code table row **in the same commit**.
- The 8 App goldens stay byte-identical. Never run `go test ./internal/ui -run TestGolden -update`. The Block Kit renderer's output stays byte-identical; its existing tests are the guard.
- The yes/no prompt's key behaviour stays exactly as on main. `TestUpdateModifierGrid`, `TestUpdateCancels`, `TestUpdateConfirmRunsAction`, `TestUpdateHonorsCustomKeyMap` and `TestNewAppliesOptions` stay unchanged and green.
- **Copy, verbatim:**
  - Toasts:
    - `Couldn't send <label>: <err>`
    - `<sender>: press b to respond`
    - `No buttons on this message`
    - `Only visible to you — press b to respond`
    - `Only visible to you`
  - Hint line: `b to respond`.
  - `b`'s help text: `respond to message buttons`.
- Fixtures use placeholder IDs (`C0EXAMPLE01`, `U0EXAMPLE01`) or the repo's usual `C1`/`U1`. No real workspace IDs.
- Before the final commit: `go build ./...`, `go vet ./...`, `gofmt -l .` (empty), `golangci-lint run` (0 issues), `go test ./... -race`.

## Review Focus

Failure modes the spec implies but its happy path doesn't exercise, most likely first. Each has a test in the task named.

1. **A mouse click on Cancel in the quit or delete prompt must never confirm.** (Task 4)
2. **An ephemeral that arrives while another modal (for example the quit prompt) is open** waits, and opens when that modal closes, not on top of it. (Task 7)
3. **A queued ephemeral whose message has left every pane** (the user switched channel) is skipped silently, and the next one opens. (Task 7)
4. **A message deleted while its dialog is open** closes the dialog. A second keypress must not press a button for a message that no longer exists. (Task 7)
5. **Buttons beyond the ninth** have no number key, but remain reachable with Tab and the mouse. (Task 6)

## File map

| File | Task | Responsibility |
|---|---|---|
| `internal/bubbles/buttonrow/buttonrow.go` (new) | 1 | lay out labels into wrapped rows with positions |
| `internal/ui/messages/blockkit/render.go` | 1 | `appendActions` uses `buttonrow` |
| `internal/bubbles/confirmprompt/{confirmprompt,choice,keymap,styles,update,view}.go` | 2, 3 | N-button prompt; yes/no preset |
| `internal/ui/confirm.go`, `internal/ui/mode_confirm.go`, `internal/ui/reducer_modal_click.go` | 4 | theme styles for buttons; click-to-press; shared after-close hook |
| `internal/core/ports.go`, `internal/core/adapters.go`, `internal/ui/service_defaults.go`, `internal/ui/app.go`, `cmd/slk/` | 5 | `InteractionService` port and wiring |
| `internal/ui/messages/ephemeral.go` | 6, 10 | `ButtonActions`, `HasButtonActions`, `InteractiveHint` |
| `internal/ui/action_prompt.go` (new) | 6, 7 | build the Slack choice; open; queue; interruption rule |
| `internal/ui/reducer_action_prompt.go` (new) | 6, 7 | reducer arms for press results |
| `internal/ui/reducer_send.go`, `internal/ui/mode_insert.go` | 7 | enqueue; delete handling; re-check points |
| `internal/ui/keys.go`, `internal/ui/mode_normal.go` | 9 | `b`; inert keys on ephemerals |
| `internal/ui/messages/model.go`, `internal/ui/thread/model.go` | 10 | hint line |

---

### Task 1: `internal/bubbles/buttonrow`, the one button-row layout

**Files:**
- Create: `internal/bubbles/buttonrow/buttonrow.go`, `internal/bubbles/buttonrow/buttonrow_test.go`
- Modify: `internal/ui/messages/blockkit/render.go` (`appendActions`, ~lines 236–278)
- Modify: `AGENTS.md` (shared-code table "UI chrome")

**Interfaces:**
- Produces:
  - `buttonrow.Placed{Index, Row, Col int; Text string; Width int}`
  - `buttonrow.Layout(labels []string, width, gap int) []Placed`
  - `buttonrow.Rows(placed []Placed, gap int) []string`
  - `buttonrow.RowsFunc(placed []Placed, gap string, render func(Placed) string) []string`

- [ ] **Step 1: Write the failing tests**

Create `internal/bubbles/buttonrow/buttonrow_test.go`:

```go
package buttonrow

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestLayoutOneRow(t *testing.T) {
	t.Parallel()
	got := Layout([]string{"[a]", "[bb]"}, 20, 2)
	want := []Placed{
		{Index: 0, Row: 0, Col: 0, Text: "[a]", Width: 3},
		{Index: 1, Row: 0, Col: 5, Text: "[bb]", Width: 4},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Layout = %v, want %v", got, want)
	}
}

func TestLayoutWraps(t *testing.T) {
	t.Parallel()
	got := Layout([]string{"[aaaa]", "[bbbb]", "[cc]"}, 14, 2)
	// [aaaa]  [bbbb] is 14 wide: fits. [cc] would make it 20: wraps.
	if got[1].Row != 0 || got[1].Col != 8 || got[2].Row != 1 || got[2].Col != 0 {
		t.Errorf("Layout = %v", got)
	}
}

func TestLayoutShortensOverWideLabel(t *testing.T) {
	t.Parallel()
	got := Layout([]string{"[ Don't Show Again ]"}, 12, 2)
	if got[0].Width != 12 || lipgloss.Width(got[0].Text) != 12 {
		t.Errorf("width = %d (%q), want 12", got[0].Width, got[0].Text)
	}
	if r := []rune(got[0].Text); r[len(r)-1] != '…' {
		t.Errorf("want an ellipsis, got %q", got[0].Text)
	}
}

func TestRowsJoinsWithGap(t *testing.T) {
	t.Parallel()
	placed := Layout([]string{"[aaaa]", "[bbbb]", "[cc]"}, 14, 2)
	got := Rows(placed, 2)
	want := []string{"[aaaa]  [bbbb]", "[cc]"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Rows = %q, want %q", got, want)
	}
}

func TestRowsFuncRendersEachLabel(t *testing.T) {
	t.Parallel()
	placed := Layout([]string{"a", "b", "c"}, 3, 1)
	got := RowsFunc(placed, "_", func(p Placed) string { return "<" + p.Text + ">" })
	want := []string{"<a>_<b>", "<c>"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("RowsFunc = %q, want %q", got, want)
	}
}

func TestLayoutEmpty(t *testing.T) {
	t.Parallel()
	if got := Rows(Layout(nil, 10, 2), 2); len(got) != 0 {
		t.Errorf("Rows(nil) = %q, want none", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bubbles/buttonrow/ -count=1`
Expected: FAIL to compile — `undefined: Layout`.

- [ ] **Step 3: Implement**

Create `internal/bubbles/buttonrow/buttonrow.go`:

```go
// Package buttonrow lays out a row of labelled controls (buttons, select
// labels) that wraps to further rows at a width. It is the one
// implementation shared by the Block Kit renderer and the confirm prompt.
package buttonrow

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Placed is one label's position after Layout.
type Placed struct {
	Index int    // position in the labels passed to Layout
	Row   int    // 0-based row
	Col   int    // display column the label starts at
	Text  string // the label as drawn; shortened with "…" if wider than width
	Width int    // display width of Text
}

// Layout places labels left to right, gap columns apart, starting a new row
// when the next label would pass width. A label wider than width (when
// width > 0) is shortened with "…"; it is ANSI-aware, so styled labels keep
// their escapes. With width <= 0 every label after the first starts a row.
func Layout(labels []string, width, gap int) []Placed {
	out := make([]Placed, 0, len(labels))
	row, end := 0, 0 // end: column just past the last placed label on row
	for i, label := range labels {
		if width > 0 && lipgloss.Width(label) > width {
			label = ansi.Truncate(label, width, "…")
		}
		w := lipgloss.Width(label)
		col := 0
		if i > 0 {
			col = end + gap
			if col+w > width {
				row++
				col = 0
			}
		}
		out = append(out, Placed{Index: i, Row: row, Col: col, Text: label, Width: w})
		end = col + w
	}
	return out
}

// Rows joins placed labels into one string per row, gap spaces apart.
func Rows(placed []Placed, gap int) []string {
	return RowsFunc(placed, strings.Repeat(" ", gap), func(p Placed) string { return p.Text })
}

// RowsFunc joins placed labels into one string per row, drawing each with
// render and putting gap between neighbours on a row. gap must be as wide
// as the gap passed to Layout, or the columns in Placed stop matching.
func RowsFunc(placed []Placed, gap string, render func(Placed) string) []string {
	var rows []string
	var b strings.Builder
	cur := -1
	for _, p := range placed {
		switch {
		case p.Row != cur && cur >= 0:
			rows = append(rows, b.String())
			b.Reset()
		case p.Row == cur:
			b.WriteString(gap)
		}
		cur = p.Row
		b.WriteString(render(p))
	}
	if cur >= 0 {
		rows = append(rows, b.String())
	}
	return rows
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/bubbles/buttonrow/ -count=1`
Expected: PASS.

- [ ] **Step 5: Move `appendActions` onto it**

In `internal/ui/messages/blockkit/render.go`, replace the body of `appendActions` after `out.Interactive = true` with:

```go
	const gapW = 2
	labels := make([]string, len(a.Elements))
	for i, el := range a.Elements {
		labels[i] = renderControlLabel(el.Kind, el.Label)
	}
	out.Lines = append(out.Lines, buttonrow.Rows(buttonrow.Layout(labels, width, gapW), gapW)...)
```

Add the import `"github.com/gammons/slk/internal/bubbles/buttonrow"`. Remove the `github.com/charmbracelet/x/ansi` import (and `strings`, if it is now unused) if nothing else in `render.go` uses it; `go build` reports any import left unused.

- [ ] **Step 6: Confirm the Block Kit output is byte-identical**

Run: `go test ./internal/ui/messages/... ./internal/bubbles/... ./internal/ui/ -run 'Action|Legacy|Golden|Boundary|TUI' -count=1`
Expected: PASS, with no test edited. That includes `TestRenderActionsBlockWrapsAtWidth`, `TestRenderActionsBlockTruncatesControlWiderThanRow` and `TestRenderLegacyActionsNarrowerThanOneButton`.

- [ ] **Step 7: Record it in `AGENTS.md`**

In the "UI chrome" table, after the `ui/scrollbar.Overlay` row, add:

```markdown
| Lay out a wrapping row of labelled controls (buttons, select labels) with positions for hit-testing | `internal/bubbles/buttonrow` (`Layout`, `Rows`, `RowsFunc`); used by Block Kit actions and the confirm prompt |
```

- [ ] **Step 8: Commit**

```bash
git add internal/bubbles/buttonrow internal/ui/messages/blockkit/render.go AGENTS.md
git commit -m "refactor(bubbles): move the button-row layout into internal/bubbles/buttonrow"
```

---

### Task 2: `confirmprompt` choices: buttons, keys, button row

The model and keys become general; yes/no becomes a preset whose keys are exactly #302's. The body is still the one-line preview here (Task 3 changes it).

**Files:**
- Create: `internal/bubbles/confirmprompt/choice.go`, `internal/bubbles/confirmprompt/choice_test.go`
- Modify: `internal/bubbles/confirmprompt/confirmprompt.go`, `keymap.go`, `styles.go`, `update.go`, `view.go`
- Modify: `AGENTS.md` (the confirm-prompt mention, if any; otherwise add a row in "UI chrome")

**Interfaces:**
- Consumes: `buttonrow.Layout`, `buttonrow.RowsFunc` (Task 1).
- Produces:
  - `confirmprompt.Button{Label, Hint string; Keys key.Binding; Action func() tea.Msg; Then *Choice}`
  - `confirmprompt.Choice{Title, Body string; Buttons []Button; Default int; CancelOnOtherKeys bool}`
  - `(*Model).OpenChoice(c Choice)`; `(*Model).Highlight(i int)`; `(Model).Highlighted() int`
  - `(Model).PressHighlighted() (Model, tea.Cmd)`
  - `KeyMap` gains `Press`, `Close`, `Next`, `Prev`; `Styles` gains `Button`, `ButtonActive`
  - `Open(title, body string, onConfirm ConfirmFunc)`: unchanged signature and key behaviour

- [ ] **Step 1: Write the failing tests**

Create `internal/bubbles/confirmprompt/choice_test.go`:

```go
package confirmprompt

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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
	out := openChoice(t, slackChoice()).View()
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
	if out := openChoice(t, c).View(); !strings.Contains(out, "[Don't Show Again]") {
		t.Errorf("a button with no keys should render as [label]:\n%s", out)
	}
}

func TestPresetHighlightsConfirm(t *testing.T) {
	t.Parallel()
	if got := opened(t, nil).Highlighted(); got != 0 {
		t.Errorf("yes/no preset: Highlighted = %d, want 0 (Confirm)", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bubbles/confirmprompt/ -count=1`
Expected: FAIL to compile — `undefined: Choice`, `undefined: Button`.

- [ ] **Step 3: Add `Button` and `Choice`**

Create `internal/bubbles/confirmprompt/choice.go`:

```go
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
	b := m.button(i)
	hint := b.Hint
	if hint == "" {
		if ks := b.Keys.Keys(); len(ks) > 0 {
			hint = ks[0]
		}
	}
	if hint == "" {
		return "[" + b.Label + "]"
	}
	return "[" + hint + "] " + b.Label
}
```

- [ ] **Step 4: Rework the model**

In `internal/bubbles/confirmprompt/confirmprompt.go`:

1. Change the package doc's first line to: `// Package confirmprompt provides a prompt overlay: a title, a body and a row of buttons. Open is the yes/no preset; OpenChoice shows any buttons.`
2. Replace the `title`, `body` and `onConfirm` fields of `Model` with:

```go
	choice    Choice
	preset    bool // opened with Open: keys and labels follow KeyMap
	highlight int  // highlighted button; -1 = none
```

3. In `New`, set `highlight: -1` in the literal.
4. Replace `Open` and `Close`, and add the new methods:

```go
// Open shows the yes/no preset: Confirm (KeyMap.Confirm) runs onConfirm,
// Cancel (KeyMap.Cancel) and any other key close. onConfirm may be nil.
func (m *Model) Open(title, body string, onConfirm ConfirmFunc) {
	m.open(Choice{
		Title:             title,
		Body:              body,
		Buttons:           []Button{presetConfirm: {Action: onConfirm}, presetCancel: {}},
		Default:           presetConfirm,
		CancelOnOtherKeys: true,
	})
	m.preset = true
}

// OpenChoice shows c.
func (m *Model) OpenChoice(c Choice) {
	m.open(c)
	m.preset = false
}

func (m *Model) open(c Choice) {
	m.choice = c
	m.highlight = -1
	if c.Default >= 0 && c.Default < len(c.Buttons) {
		m.highlight = c.Default
	}
	m.visible = true
}

// Close hides the prompt and clears its state.
func (m *Model) Close() {
	m.visible = false
	m.choice = Choice{}
	m.preset = false
	m.highlight = -1
}

// Highlight highlights button i; out of range is ignored.
func (m *Model) Highlight(i int) {
	if i >= 0 && i < len(m.choice.Buttons) {
		m.highlight = i
	}
}

// Highlighted returns the highlighted button, or -1.
func (m Model) Highlighted() int { return m.highlight }
```

(`Buttons: []Button{presetConfirm: …, presetCancel: …}` is an indexed composite literal. `onConfirm` is a `ConfirmFunc` and assigns to `func() tea.Msg` because the underlying types match.)

- [ ] **Step 5: Keys**

Replace `internal/bubbles/confirmprompt/keymap.go` with:

```go
package confirmprompt

import "charm.land/bubbles/v2/key"

// KeyMap is the prompt's key bindings. Confirm and Cancel are the yes/no
// preset's buttons. Press, Next and Prev act only in a Choice that does not
// cancel on other keys; Close always closes without pressing.
type KeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
	Press   key.Binding
	Close   key.Binding
	Next    key.Binding
	Prev    key.Binding
}

// DefaultKeyMap returns the default bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Confirm: key.NewBinding(key.WithKeys("y", "Y", "enter"), key.WithHelp("y", "confirm")),
		Cancel:  key.NewBinding(key.WithKeys("n", "N", "esc"), key.WithHelp("n/Esc", "cancel")),
		Press:   key.NewBinding(key.WithKeys("enter")),
		Close:   key.NewBinding(key.WithKeys("esc")),
		Next:    key.NewBinding(key.WithKeys("l", "right", "tab")),
		Prev:    key.NewBinding(key.WithKeys("h", "left", "shift+tab")),
	}
}
```

Replace `internal/bubbles/confirmprompt/update.go` with:

```go
package confirmprompt

import (
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update handles one key press, in this order: a button's keys press it;
// Enter presses the highlighted button; Esc closes; h/l/arrows/Tab move the
// highlight; anything else closes when the choice cancels on other keys and
// is ignored otherwise. Enter-on-highlight and movement are off when the
// choice cancels on other keys, which keeps the yes/no preset exactly as it
// was: its keys are y/Y/Enter, n/N/Esc, and everything else cancels.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.visible {
		return m, nil
	}
	for i := range m.choice.Buttons {
		if matches(press, m.button(i).Keys) {
			return m.press(i)
		}
	}
	free := !m.choice.CancelOnOtherKeys
	switch {
	case free && matches(press, m.KeyMap.Press):
		return m.PressHighlighted()
	case matches(press, m.KeyMap.Close):
		m.Close()
		return m, nil
	case free && key.Matches(press, m.KeyMap.Next):
		m.move(1)
		return m, nil
	case free && key.Matches(press, m.KeyMap.Prev):
		m.move(-1)
		return m, nil
	}
	if m.choice.CancelOnOtherKeys {
		m.Close()
	}
	return m, nil
}

// PressHighlighted presses the highlighted button, or does nothing when
// none is. Hosts use it for a mouse click after Highlight.
func (m Model) PressHighlighted() (Model, tea.Cmd) {
	if !m.visible || m.highlight < 0 {
		return m, nil
	}
	return m.press(m.highlight)
}

func (m Model) press(i int) (Model, tea.Cmd) {
	b := m.button(i)
	var cmd tea.Cmd
	if b.Action != nil {
		fn := b.Action
		cmd = func() tea.Msg { return fn() }
	}
	if b.Then != nil {
		m.OpenChoice(*b.Then)
	} else {
		m.Close()
	}
	return m, cmd
}

func (m *Model) move(delta int) {
	n := len(m.choice.Buttons)
	if n == 0 {
		return
	}
	if m.highlight < 0 {
		if delta > 0 {
			m.highlight = 0
		} else {
			m.highlight = n - 1
		}
		return
	}
	m.highlight = ((m.highlight+delta)%n + n) % n
}

// matches reports whether press triggers b. A modifier on a non-printable
// key is ignored, so a stray shift on Enter still counts. On a printable key
// the modifier is part of the key: ctrl+y and alt+y are not y. The test is on
// the base key's Code, not on Text, because bubbletea leaves Text empty for a
// ctrl- or alt-modified letter too.
func matches(press tea.KeyPressMsg, b key.Binding) bool {
	if key.Matches(press, b) {
		return true
	}
	if press.Mod == 0 || printable(press.Code) {
		return false
	}
	press.Mod = 0
	return key.Matches(press, b)
}

// printable reports whether code is a printable character rather than a
// special key (Enter, Esc, arrows, ...). Special keys above unicode.MaxRune
// are never printable.
func printable(code rune) bool {
	return code <= unicode.MaxRune && unicode.IsPrint(code)
}
```

(`move` sets the first highlight to button 0 on a forward move. In `TestChoiceMovementThenEnter`, `l` lands on 0, `right` on 1 and Tab on 2.)

- [ ] **Step 6: Styles and the button row**

In `internal/bubbles/confirmprompt/styles.go`, add two fields to `Styles` after `Footer`:

```go
	// Button and ButtonActive draw the buttons; ButtonActive is the
	// highlighted one.
	Button       lipgloss.Style
	ButtonActive lipgloss.Style
```

In `DefaultStyles`, add to the returned literal:

```go
		Button:       lipgloss.NewStyle().Background(bg).Foreground(muted),
		ButtonActive: lipgloss.NewStyle().Background(accent).Foreground(bg).Bold(true),
```

In `internal/bubbles/confirmprompt/view.go`:
- import `"github.com/gammons/slk/internal/bubbles/buttonrow"`;
- add `const buttonGap = 3` beside the existing constants;
- replace the `m.title` / `m.body` reads with `m.choice.Title` / `m.choice.Body`;
- replace the `m.styles.Footer.Render(m.footer())` element with `m.renderButtons(width - chrome)`;
- delete `footer()` and add:

```go
// renderButtons draws the buttons as wrapping rows, the highlighted one in
// ButtonActive.
func (m Model) renderButtons(width int) string {
	labels := make([]string, len(m.choice.Buttons))
	for i := range labels {
		labels[i] = m.label(i)
	}
	placed := buttonrow.Layout(labels, width, buttonGap)
	gap := m.styles.Footer.Render(strings.Repeat(" ", buttonGap))
	rows := buttonrow.RowsFunc(placed, gap, func(p buttonrow.Placed) string {
		if p.Index == m.highlight {
			return m.styles.ButtonActive.Render(p.Text)
		}
		return m.styles.Button.Render(p.Text)
	})
	return strings.Join(rows, "\n")
}
```

- [ ] **Step 7: Run the component tests**

Run: `go test ./internal/bubbles/confirmprompt/ -count=1`
Expected: PASS. That covers every new test and every existing test, unedited. The yes/no row renders `[y] confirm   [n/Esc] cancel`, the old footer text, so `TestViewShowsTitleBodyAndHelp`, `TestViewHelpFollowsKeyMap` and `TestNewAppliesOptions` still hold.

Then run: `go build ./... && go test ./internal/ui/ -run 'Confirm|Quit|Delete|Golden' -count=1`
Expected: PASS. The host still compiles against the unchanged `Open`.

- [ ] **Step 8: `AGENTS.md`**

If `AGENTS.md` mentions the confirm prompt (`grep -n confirmprompt AGENTS.md`), update that line to say it takes N buttons via `OpenChoice` with yes/no as `Open`. Otherwise add to "UI chrome":

```markdown
| A centered prompt with a title, wrapped body and clickable buttons (yes/no is `Open`; any buttons via `OpenChoice(confirmprompt.Choice)`, `Button.Then` for a follow-up step) | `internal/bubbles/confirmprompt`; host wiring in `internal/ui/confirm.go` |
```

- [ ] **Step 9: Commit**

```bash
git add internal/bubbles/confirmprompt AGENTS.md
git commit -m "feat(bubbles): confirmprompt shows N buttons; yes/no becomes a preset"
```

---

### Task 3: `confirmprompt` wraps the body and hit-tests buttons

**Files:**
- Modify: `internal/bubbles/confirmprompt/view.go`
- Modify: `internal/bubbles/confirmprompt/confirmprompt_test.go` (rewrite `TestViewFlattensBody` and `TestViewTruncatesLongBody` only)
- Test: `internal/bubbles/confirmprompt/choice_test.go`

**Interfaces:**
- Consumes: Task 2's `Choice`, `label`, `renderButtons`; Task 1's `buttonrow.Placed`.
- Produces: `(Model).ButtonAt(x, y int) (int, bool)`, where `x` and `y` are cells relative to the box's top-left corner.

- [ ] **Step 1: Rewrite the two rendering tests and add the new ones**

In `confirmprompt_test.go`, replace `TestViewFlattensBody` and `TestViewTruncatesLongBody` with the following, adding `"github.com/charmbracelet/x/ansi"` to that file's imports:

```go
func plainLines(view string) []string { return strings.Split(ansi.Strip(view), "\n") }

// The body wraps and keeps its paragraph breaks (spec: it used to be
// flattened to one line).
func TestViewWrapsBodyKeepingParagraphs(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(80))
	m.Open("Title", "line1\nline2\tline3", nil)
	var one, two int = -1, -1
	for i, l := range plainLines(m.View()) {
		if strings.Contains(l, "line1") {
			one = i
		}
		if strings.Contains(l, "line2 line3") {
			two = i
		}
	}
	if one < 0 || two < 0 || two != one+1 {
		t.Errorf("want line1 and 'line2 line3' on consecutive lines:\n%s", m.View())
	}
}

func TestViewCapsBodyAtEightLines(t *testing.T) {
	t.Parallel()
	m := New(WithWidth(40))
	m.Open("Title", strings.Repeat("long ", 100), nil)
	var body []string
	for _, l := range plainLines(m.View()) {
		if strings.Contains(l, "long") {
			body = append(body, l)
		}
	}
	if len(body) != maxBodyLines {
		t.Errorf("body lines = %d, want %d:\n%s", len(body), maxBodyLines, m.View())
	}
	if last := strings.TrimRight(strings.TrimRight(body[len(body)-1], " │"), " "); !strings.HasSuffix(last, "…") {
		t.Errorf("last body line should end with …: %q", last)
	}
	if got := lipgloss.Width(m.View()); got != minWidth {
		t.Errorf("box width = %d, want %d", got, minWidth)
	}
}
```

Append to `choice_test.go`, adding `"github.com/charmbracelet/x/ansi"` to its imports:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bubbles/confirmprompt/ -count=1`
Expected: FAIL to compile — `undefined: maxBodyLines`, `m.ButtonAt undefined`.

- [ ] **Step 3: Lay out once, for both `View` and `ButtonAt`**

In `internal/bubbles/confirmprompt/view.go`:
- Import `xansi "github.com/charmbracelet/x/ansi"`; the package already imports `internal/bubbles/ansi` as `ansi`.
- Remove the `github.com/muesli/reflow/truncate` import and the `preview` function.
- Add `maxBodyLines = 8` to the constants.
- Replace `View` and `renderButtons` with:

```go
// layout is the prompt's content measured at a content width: the title
// and body lines, and where each button sits in the button rows.
type layout struct {
	title, body []string
	buttons     []buttonrow.Placed
}

func (m Model) layoutAt(width int) layout {
	labels := make([]string, len(m.choice.Buttons))
	for i := range labels {
		labels[i] = m.label(i)
	}
	return layout{
		title:   wrapLines(m.choice.Title, width, 0),
		body:    wrapLines(m.choice.Body, width, maxBodyLines),
		buttons: buttonrow.Layout(labels, width, buttonGap),
	}
}

// View renders the prompt box, or "" when hidden: the title, a blank line,
// the body, a blank line, then the button rows.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	width := boxWidth(m.width)
	l := m.layoutAt(width - chrome)
	content := strings.Join([]string{
		m.styles.Title.Render(strings.Join(l.title, "\n")),
		m.styles.Body.Render(strings.Join(l.body, "\n")),
		m.renderButtons(l.buttons),
	}, "\n\n")
	return m.styles.Box.Width(width).Render(ansi.ReapplyAfterResets(content, m.styles.BaseANSI))
}

// ButtonAt reports which button, if any, is at cell (x, y) relative to the
// box's top-left corner. Hosts use it to turn a click into a press.
func (m Model) ButtonAt(x, y int) (int, bool) {
	if !m.visible {
		return 0, false
	}
	l := m.layoutAt(boxWidth(m.width) - chrome)
	top := m.styles.Box.GetBorderTopSize() + m.styles.Box.GetPaddingTop()
	left := m.styles.Box.GetBorderLeftSize() + m.styles.Box.GetPaddingLeft()
	firstRow := top + len(l.title) + 1 + len(l.body) + 1
	for _, p := range l.buttons {
		if y == firstRow+p.Row && x >= left+p.Col && x < left+p.Col+p.Width {
			return p.Index, true
		}
	}
	return 0, false
}

// renderButtons draws placed buttons as rows, the highlighted one in
// ButtonActive.
func (m Model) renderButtons(placed []buttonrow.Placed) string {
	gap := m.styles.Footer.Render(strings.Repeat(" ", buttonGap))
	rows := buttonrow.RowsFunc(placed, gap, func(p buttonrow.Placed) string {
		if p.Index == m.highlight {
			return m.styles.ButtonActive.Render(p.Text)
		}
		return m.styles.Button.Render(p.Text)
	})
	return strings.Join(rows, "\n")
}

// wrapLines word-wraps s to width, keeping paragraph breaks and turning tabs
// into spaces. With max > 0 it keeps max lines, ending the last with "…".
func wrapLines(s string, width, max int) []string {
	s = strings.ReplaceAll(s, "\t", " ")
	var out []string
	for _, para := range strings.Split(s, "\n") {
		out = append(out, strings.Split(xansi.Wrap(para, width, ""), "\n")...)
	}
	if max > 0 && len(out) > max {
		out = out[:max]
		last := strings.TrimRight(out[max-1], " ")
		if xansi.StringWidth(last) >= width {
			last = xansi.Truncate(last, width-1, "")
		}
		out[max-1] = last + "…"
	}
	return out
}
```

If lipgloss v2 names the frame getters differently from `GetBorderTopSize`, `GetPaddingTop`, `GetBorderLeftSize` and `GetPaddingLeft`, use its equivalents (`go doc charm.land/lipgloss/v2 Style | grep -i 'Get.*\(Border\|Padding\)'`). `TestButtonAt` measures against the rendered box, so it catches a wrong offset.

- [ ] **Step 4: Run the component tests**

Run: `go test ./internal/bubbles/confirmprompt/ -count=1`
Expected: PASS, including every key test, unedited.

- [ ] **Step 5: Run the host tests**

Run: `go build ./... && go test ./internal/ui/ -run 'Confirm|Quit|Delete|Golden' -count=1`
Expected: PASS. If an `internal/ui` test asserts the old `> ` prefix or a one-line body for the delete preview, update that assertion to the wrapped body, and say so in the commit message (refinement 6).

- [ ] **Step 6: Commit**

```bash
git add internal/bubbles/confirmprompt
git commit -m "feat(bubbles): confirmprompt wraps its body and hit-tests buttons"
```

---

### Task 4: Host: themed buttons and click-to-press

**Files:**
- Modify: `internal/ui/confirm.go` (`confirmPromptStyles`, `confirmPromptBox`)
- Modify: `internal/ui/mode_confirm.go`
- Modify: `internal/ui/reducer_modal_click.go` (`modalClickTarget`, the `ModeConfirm` case, the point branch of `reduceModalClick`)
- Test: create `internal/ui/confirm_click_test.go`

**Interfaces:**
- Consumes: `(*confirmprompt.Model).Highlight`, `(Model).PressHighlighted`, `(Model).ButtonAt` (Tasks 2–3).
- Produces:
  - `(*App).afterConfirmPrompt(cmd tea.Cmd) tea.Cmd`: the single after-key/after-click hook. Task 7 extends it.
  - `(confirmPromptBox).ClickAt(termW, termH, localX, localY int) bool`
  - `modalClickTarget.onPoint func() tea.Cmd`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/confirm_click_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// clickConfirmText clicks the screen cell where text is drawn inside the
// open confirm prompt, positioned the way the router centres it.
func clickConfirmText(t *testing.T, a *App, text string) tea.Cmd {
	t.Helper()
	box := a.confirmPrompt.View()
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	startX, startY := max((a.width-w)/2, 0), max((a.height-h)/2, 0)
	for row, line := range strings.Split(ansi.Strip(box), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return reduceModalClick(a, tea.MouseClickMsg{
				X: startX + ansi.StringWidth(line[:i]) + 1, Y: startY + row, Button: tea.MouseLeft,
			})
		}
	}
	t.Fatalf("%q not in prompt:\n%s", text, box)
	return nil
}

// Review Focus 1: a click on Cancel must never confirm. The router's usual
// "synthesise Enter" would, because Enter is a Confirm key.
func TestQuitPromptClickCancelDoesNotQuit(t *testing.T) {
	a := newTestApp(t, withSize(120, 40))
	a.openQuitConfirm()

	cmd := clickConfirmText(t, a, "[n/Esc] cancel")
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("clicking Cancel quit slk")
		}
	}
	if a.confirmPrompt.IsVisible() || a.mode != ModeNormal {
		t.Errorf("after Cancel: visible=%v mode=%v, want hidden and normal", a.confirmPrompt.IsVisible(), a.mode)
	}
}

func TestQuitPromptClickConfirmQuits(t *testing.T) {
	a := newTestApp(t, withSize(120, 40))
	a.openQuitConfirm()

	cmd := clickConfirmText(t, a, "[y] confirm")
	if cmd == nil {
		t.Fatal("clicking Confirm returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("clicking Confirm should quit")
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want normal after the prompt closed", a.mode)
	}
}

func TestConfirmPromptClickOffButtonDoesNothing(t *testing.T) {
	a := newTestApp(t, withSize(120, 40))
	a.openQuitConfirm()

	if cmd := clickConfirmText(t, a, "Quit slk?"); cmd != nil {
		t.Error("a click on the title should do nothing")
	}
	if !a.confirmPrompt.IsVisible() || a.mode != ModeConfirm {
		t.Error("a click on the title should leave the prompt open")
	}
}

// The host must set both button styles; unset styles render plain text.
func TestConfirmPromptButtonStylesAreThemed(t *testing.T) {
	s := confirmPromptStyles()
	if s.Button.Render("x") == "x" || s.ButtonActive.Render("x") == "x" {
		t.Error("the theme must style both buttons")
	}
	if s.Button.Render("x") == s.ButtonActive.Render("x") {
		t.Error("the highlighted button must look different")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'QuitPromptClick|ConfirmPromptClick|ConfirmPromptButtonStyles' -count=1`
Expected: FAIL. `TestQuitPromptClickConfirmQuits` returns no command, because clicks inside the confirm box are a no-op today, and the styles test fails on a nil background.

- [ ] **Step 3: Themed button styles**

In `internal/ui/confirm.go`, add to the `confirmprompt.Styles` literal in `confirmPromptStyles`:

```go
		Button:       lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted),
		ButtonActive: lipgloss.NewStyle().Background(styles.Primary).Foreground(bg).Bold(true),
```

`applyTheme` already pushes `confirmPromptStyles()` on every theme change, so these follow the theme.

- [ ] **Step 4: Click-to-press**

In `internal/ui/confirm.go`, below `confirmPromptBox.BoxSize`, add:

```go
var _ pointClickable = confirmPromptBox{}

// ClickAt highlights the button under the box-local cell and reports
// whether there was one; the router then calls the target's onPoint.
func (b confirmPromptBox) ClickAt(_, _, localX, localY int) bool {
	i, ok := b.m.ButtonAt(localX, localY)
	if ok {
		b.m.Highlight(i)
	}
	return ok
}

// pressConfirmPromptHighlighted presses the button a click just
// highlighted. It does not go through a synthesised key: in a yes/no prompt
// Enter is a Confirm key, so "click Cancel, send Enter" would confirm.
func (a *App) pressConfirmPromptHighlighted() tea.Cmd {
	var cmd tea.Cmd
	a.confirmPrompt, cmd = a.confirmPrompt.PressHighlighted()
	return a.afterConfirmPrompt(cmd)
}
```

Add `tea "charm.land/bubbletea/v2"` to `confirm.go`'s imports.

Replace the body of `handleConfirmMode` in `internal/ui/mode_confirm.go` with:

```go
	var cmd tea.Cmd
	a.confirmPrompt, cmd = a.confirmPrompt.Update(msg)
	return a.afterConfirmPrompt(cmd)
}

// afterConfirmPrompt runs after every key or click the confirm prompt
// handles: once the prompt reports hidden, mode drops back to normal.
func (a *App) afterConfirmPrompt(cmd tea.Cmd) tea.Cmd {
	if !a.confirmPrompt.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return cmd
```

(keep the function's opening line and the file's header comment).

In `internal/ui/reducer_modal_click.go`:

1. Add a field to `modalClickTarget`:

```go
	onPoint    func() tea.Cmd   // set: a point hit calls this instead of synthesising activation
```

2. Change the `ModeConfirm` case to:

```go
	case ModeConfirm:
		// A click on a button presses it directly; elsewhere inside is a
		// no-op, outside dismisses.
		box := confirmPromptBox{&a.confirmPrompt}
		return modalClickTarget{box: box, point: box, onPoint: a.pressConfirmPromptHighlighted}, true
```

3. Change the point branch of `reduceModalClick` to:

```go
	// Inside the box on a point hot spot -> its press, or its activation.
	if target.point != nil {
		if target.point.ClickAt(a.width, a.height, m.X-startX, m.Y-startY) {
			if target.onPoint != nil {
				return target.onPoint()
			}
			if target.activation != nil {
				return dispatchModeKey(a, target.activation)
			}
		}
		return nil
	}
```

For the user-profile dialog the behaviour is unchanged: its `ClickAt` has no side effects and its target has no `onPoint`.

Also update the `ModeConfirm` line in this file's package comment, if it describes confirm clicks as a no-op.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/ui/ -run 'QuitPromptClick|ConfirmPromptClick|ConfirmPromptButtonStyles|Confirm|ModalClick|UserProfile|Golden' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/confirm.go internal/ui/mode_confirm.go internal/ui/reducer_modal_click.go internal/ui/confirm_click_test.go
git commit -m "feat(ui): confirm prompt buttons are clickable and themed"
```

---

### Task 5: `core.InteractionService`, the press port

**Files:**
- Modify: `internal/core/ports.go` (add the interface after `ReactionService`)
- Modify: `internal/core/adapters.go` (funcs struct and adapter)
- Create: `internal/core/interaction_adapter_test.go`
- Modify: `internal/ui/service_defaults.go`, `internal/ui/app.go` (field, `NewApp` default, setter), `internal/ui/services_helpers_test.go`
- Create: `cmd/slk/interactions.go`, `cmd/slk/interactions_test.go`
- Modify: `cmd/slk/main.go` (wire it beside `app.SetReactionService`)
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: `slackclient.AttachmentActionRequest`, `(*slackclient.Client).AttachmentAction` (from #298); `blocks.LegacyAction`.
- Produces:
  - `core.InteractionService` with `PressAttachmentAction(channelID ids.ChannelID, messageTS ids.MessageTS, attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error`
  - `core.InteractionServiceFuncs{PressAttachmentAction func(...)}` and `core.NewInteractionService(InteractionServiceFuncs) InteractionService`
  - `(*App).SetInteractionService(core.InteractionService)`; field `a.interactions`
  - test helper `(*App).setInteractionPressForTest(fn func(ids.ChannelID, ids.MessageTS, int, string, blocks.LegacyAction, bool) error)`

- [ ] **Step 1: Write the failing adapter test**

Create `internal/core/interaction_adapter_test.go`:

```go
package core

import (
	"errors"
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
)

func TestInteractionServiceNilFuncIsUnsupported(t *testing.T) {
	err := NewInteractionService(InteractionServiceFuncs{}).PressAttachmentAction("C1", "1.0", 1, "cb", blocks.LegacyAction{}, true)
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestInteractionServicePassesArguments(t *testing.T) {
	var got []any
	svc := NewInteractionService(InteractionServiceFuncs{
		PressAttachmentAction: func(ch ids.ChannelID, ts ids.MessageTS, attID int, cb string, act blocks.LegacyAction, eph bool) error {
			got = []any{ch, ts, attID, cb, act.Name, eph}
			return nil
		},
	})
	if err := svc.PressAttachmentAction("C1", "1.0", 7, "cb", blocks.LegacyAction{Name: "ignore"}, true); err != nil {
		t.Fatal(err)
	}
	want := []any{ids.ChannelID("C1"), ids.MessageTS("1.0"), 7, "cb", "ignore", true}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %v, want %v", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/ -run InteractionService -count=1`
Expected: FAIL to compile — `undefined: NewInteractionService`.

- [ ] **Step 3: Port and adapter**

In `internal/core/ports.go`, after `ReactionService`, add (importing `github.com/gammons/slk/internal/core/blocks` if the file doesn't yet):

```go
// InteractionService presses interactive message elements. Pressing is
// intent from the action dialog; on success Slack answers over the
// WebSocket (an ephemeral is deleted), so only the error comes back and App
// turns it into a toast. Blocking: callers run it from a tea.Cmd.
//
// Build one with NewInteractionService.
type InteractionService interface {
	// PressAttachmentAction presses a legacy attachment button, echoing
	// action back as received (chat.attachmentAction).
	PressAttachmentAction(channelID ids.ChannelID, messageTS ids.MessageTS,
		attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error
}
```

In `internal/core/adapters.go`, add (importing `blocks` and `errors` as needed):

```go
// InteractionServiceFuncs bundles the closures behind an InteractionService.
// A nil func makes its method return errors.ErrUnsupported.
type InteractionServiceFuncs struct {
	PressAttachmentAction func(channelID ids.ChannelID, messageTS ids.MessageTS,
		attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error
}

// NewInteractionService builds an InteractionService from fns. Used by
// cmd/slk/main.go (production wiring) and tests.
func NewInteractionService(fns InteractionServiceFuncs) InteractionService {
	return interactionAdapter{fns: fns}
}

type interactionAdapter struct{ fns InteractionServiceFuncs }

func (i interactionAdapter) PressAttachmentAction(channelID ids.ChannelID, messageTS ids.MessageTS,
	attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error {
	if i.fns.PressAttachmentAction == nil {
		return errors.ErrUnsupported
	}
	return i.fns.PressAttachmentAction(channelID, messageTS, attachmentID, callbackID, action, ephemeral)
}
```

Run: `go test ./internal/core/ -count=1`
Expected: PASS.

- [ ] **Step 4: App field, default and setter**

- In `internal/ui/service_defaults.go`, add beside the other no-ops: `noopInteractionService = core.NewInteractionService(core.InteractionServiceFuncs{})`.
- In `internal/ui/app.go`:
  - add the field `interactions core.InteractionService` next to `reactions`;
  - add `interactions: noopInteractionService,` next to `reactions: noopReactionService,` in `NewApp`'s literal;
  - add after `SetReactionService`:

```go
// SetInteractionService wires the service the action dialog presses
// buttons through. A nil s restores the no-op, whose presses fail with
// errors.ErrUnsupported and so surface as a toast.
func (a *App) SetInteractionService(s core.InteractionService) {
	if s == nil {
		s = noopInteractionService
	}
	a.interactions = s
}
```

In `internal/ui/services_helpers_test.go`, add (with the `blocks` import):

```go
func (a *App) setInteractionPressForTest(fn func(ids.ChannelID, ids.MessageTS, int, string, blocks.LegacyAction, bool) error) {
	a.SetInteractionService(core.NewInteractionService(core.InteractionServiceFuncs{PressAttachmentAction: fn}))
}
```

- [ ] **Step 5: `cmd/slk` wiring, with a test of the field mapping**

Create `cmd/slk/interactions_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
	slackclient "github.com/gammons/slk/internal/slack"
)

type fakeActioner struct{ got slackclient.AttachmentActionRequest }

func (f *fakeActioner) AttachmentAction(_ context.Context, req slackclient.AttachmentActionRequest) error {
	f.got = req
	return nil
}

func TestPressAttachmentActionMapsFields(t *testing.T) {
	f := &fakeActioner{}
	act := blocks.LegacyAction{ID: "2", Name: "ignore", Text: "Dismiss", Type: "button", Value: "ignore"}
	if err := pressAttachmentAction(context.Background(), f, "C0EXAMPLE01", "1.000100", 1, "cb", act, true); err != nil {
		t.Fatal(err)
	}
	want := slackclient.AttachmentActionRequest{
		ChannelID: "C0EXAMPLE01", MessageTS: "1.000100", AttachmentID: 1,
		CallbackID: "cb", Ephemeral: true, Action: act,
	}
	if f.got != want {
		t.Errorf("request = %+v, want %+v", f.got, want)
	}
}
```

(If `AttachmentActionRequest` holds a pointer field such as `Action.Confirm` and the `!=` comparison doesn't compile, compare the fields individually.)

Run: `go test ./cmd/slk/ -run PressAttachmentAction -count=1`
Expected: FAIL to compile — `undefined: pressAttachmentAction`.

Create `cmd/slk/interactions.go`:

```go
package main

import (
	"context"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	slackclient "github.com/gammons/slk/internal/slack"
)

// attachmentActioner is the part of *slackclient.Client a button press
// needs.
type attachmentActioner interface {
	AttachmentAction(ctx context.Context, req slackclient.AttachmentActionRequest) error
}

// pressAttachmentAction presses a legacy attachment button through client.
func pressAttachmentAction(ctx context.Context, client attachmentActioner, channelID ids.ChannelID,
	messageTS ids.MessageTS, attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error {
	return client.AttachmentAction(ctx, slackclient.AttachmentActionRequest{
		ChannelID:    string(channelID),
		MessageTS:    string(messageTS),
		AttachmentID: attachmentID,
		CallbackID:   callbackID,
		Ephemeral:    ephemeral,
		Action:       action,
	})
}
```

In `cmd/slk/main.go`, directly after the `app.SetReactionService(...)` call, add (with `errors` and `blocks` imports as needed):

```go
		app.SetInteractionService(core.NewInteractionService(core.InteractionServiceFuncs{
			PressAttachmentAction: func(channelID ids.ChannelID, messageTS ids.MessageTS,
				attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error {
				wctx := router.Active()
				if wctx == nil {
					return errors.New("no active workspace")
				}
				return pressAttachmentAction(ctx, wctx.Client, channelID, messageTS, attachmentID, callbackID, action, ephemeral)
			},
		}))
```

(Use the same `ctx` and `router` the reaction wiring uses.)

Run: `go build ./... && go test ./cmd/slk/ ./internal/core/ ./internal/ui/ -count=1`
Expected: PASS.

- [ ] **Step 6: `AGENTS.md`**

In "Text and rendering", after the `(*slackclient.Client).AttachmentAction` row, add:

```markdown
| Press a legacy attachment button from the UI | `core.InteractionService.PressAttachmentAction` (App field `a.interactions`); wired in `cmd/slk` via `pressAttachmentAction` to the active workspace's `Client.AttachmentAction` |
```

- [ ] **Step 7: Commit**

```bash
git add internal/core internal/ui/service_defaults.go internal/ui/app.go internal/ui/services_helpers_test.go cmd/slk/interactions.go cmd/slk/interactions_test.go cmd/slk/main.go AGENTS.md
git commit -m "feat(core): InteractionService port for pressing attachment buttons"
```

---

### Task 6: The Slack action dialog: build, open, press

**Files:**
- Modify: `internal/ui/messages/ephemeral.go` (add `ButtonAction`, `ButtonActions`, `HasButtonActions`)
- Create: `internal/ui/action_prompt.go`, `internal/ui/reducer_action_prompt.go`, `internal/ui/action_prompt_test.go`
- Modify: `internal/ui/confirm.go` (extract `prepareConfirmPrompt`), `internal/ui/mode_confirm.go` (`afterConfirmPrompt` clears `shownPrompt`), `internal/ui/app.go` (field `shownPrompt`; add `reduceActionPrompt` to the `dispatchReducers` list after `reduceSend`)
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: `confirmprompt.Choice`/`Button` (Task 2), `a.interactions` (Task 5), `afterConfirmPrompt` (Task 4), `OpenLinkMsg`, `ToastMsg`, `messages.FlattenMrkdwn`, `messages.MessageTextSource`.
- Produces:
  - `messages.ButtonAction{Attachment blockkit.LegacyAttachment; Action blockkit.LegacyAction}`; `messages.ButtonActions(msg MessageItem) []ButtonAction`; `messages.HasButtonActions(msg MessageItem) bool`
  - `actionPromptRef{channelID, ts, threadTS string}` with `key() string`
  - `actionPressedMsg{label string; err error}`
  - `(*App).findActionMessage(ref) (messages.MessageItem, bool)`
  - `(*App).openActionPrompt(ref) bool`
  - `(*App).prepareConfirmPrompt()`
  - field `a.shownPrompt *actionPromptRef`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/action_prompt_test.go`:

```go
package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

const ephTS = "5.000000"

// mentionEphemeral is Slackbot's "not in channel" notice as Part A parses it.
func mentionEphemeral(ts string) messages.MessageItem {
	return messages.MessageItem{
		TS: ts, UserID: "USLACKBOT", UserName: "Slackbot", Ephemeral: true,
		Text: "You mentioned <@U0EXAMPLE01>, but they're not in this channel.",
		LegacyAttachments: []blocks.LegacyAttachment{{
			ID: 1, CallbackID: "cb", Text: "You may want to invite them.",
			Actions: []blocks.LegacyAction{
				{ID: "1", Name: "invite", Text: "Add Them", Type: "button", Value: "invite",
					Confirm: &blocks.ActionConfirm{Title: "Are you sure you want to add them?", Text: "History is visible.", OKText: "Add", DismissText: "Cancel"}},
				{ID: "2", Name: "ignore", Text: "Dismiss", Type: "button", Value: "ignore"},
				{ID: "3", Name: "dont-show-again", Text: "Don't Show Again", Type: "button", Value: "dont-show-again"},
			},
		}},
	}
}

type pressCall struct {
	channel, ts string
	attID       int
	callbackID  string
	action      blocks.LegacyAction
	ephemeral   bool
}

// actionApp is an App on C1 showing msgs, whose presses are recorded and
// answered with err.
func actionApp(t *testing.T, err error, msgs ...messages.MessageItem) (*App, *[]pressCall) {
	t.Helper()
	a := newTestApp(t, withSize(120, 40), withActiveChannel("C1"), withMessages(msgs...))
	calls := &[]pressCall{}
	a.setInteractionPressForTest(func(ch ids.ChannelID, ts ids.MessageTS, attID int, cb string, act blocks.LegacyAction, eph bool) error {
		*calls = append(*calls, pressCall{string(ch), string(ts), attID, cb, act, eph})
		return err
	})
	return a, calls
}

// pressKey sends one key to the App and runs every resulting command back
// through Update, returning the messages produced.
func pressKey(t *testing.T, a *App, k tea.KeyMsg) []tea.Msg {
	t.Helper()
	_, cmd := a.Update(k)
	var out []tea.Msg
	for _, m := range drainBatch(cmd) {
		if m == nil {
			continue
		}
		out = append(out, m)
		_, next := a.Update(m)
		out = append(out, drainBatch(next)...)
	}
	return out
}

func toastText(msgs []tea.Msg) string {
	for _, m := range msgs {
		if t, ok := m.(ToastMsg); ok {
			return t.Text
		}
	}
	return ""
}

func TestOpenActionPromptShowsButtons(t *testing.T) {
	a, _ := actionApp(t, nil, mentionEphemeral(ephTS))
	if !a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS}) {
		t.Fatal("openActionPrompt = false")
	}
	if a.mode != ModeConfirm {
		t.Errorf("mode = %v, want ModeConfirm", a.mode)
	}
	view := a.confirmPrompt.View()
	for _, want := range []string{"Slackbot", "not in this channel", "invite them", "[1] Add Them", "[2] Dismiss", "[3] Don't Show Again"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog missing %q:\n%s", want, view)
		}
	}
	if a.confirmPrompt.Highlighted() != -1 {
		t.Error("no button may start highlighted")
	}
}

func TestOpenActionPromptMissingMessage(t *testing.T) {
	a, _ := actionApp(t, nil)
	if a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS}) {
		t.Error("openActionPrompt should fail when the message is not in a pane")
	}
}

func TestActionPromptPressSendsTheAction(t *testing.T) {
	a, calls := actionApp(t, nil, mentionEphemeral(ephTS))
	a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS})

	if toast := toastText(pressKey(t, a, keyPress('2'))); toast != "" {
		t.Errorf("a successful press toasted %q", toast)
	}
	if len(*calls) != 1 {
		t.Fatalf("presses = %d, want 1", len(*calls))
	}
	c := (*calls)[0]
	if c.channel != "C1" || c.ts != ephTS || c.attID != 1 || c.callbackID != "cb" ||
		c.action.ID != "2" || c.action.Name != "ignore" || !c.ephemeral {
		t.Errorf("press = %+v", c)
	}
	if a.confirmPrompt.IsVisible() || a.mode != ModeNormal {
		t.Error("the dialog should close on press")
	}
}

func TestActionPromptFailedPressToasts(t *testing.T) {
	a, _ := actionApp(t, errors.New("boom"), mentionEphemeral(ephTS))
	a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS})

	if got, want := toastText(pressKey(t, a, keyPress('2'))), "Couldn't send Dismiss: boom"; got != want {
		t.Errorf("toast = %q, want %q", got, want)
	}
	if _, ok := a.findActionMessage(actionPromptRef{channelID: "C1", ts: ephTS}); !ok {
		t.Error("the message must stay so the press can be retried")
	}
}

func TestActionPromptConfirmStep(t *testing.T) {
	a, calls := actionApp(t, nil, mentionEphemeral(ephTS))
	a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS})

	pressKey(t, a, keyPress('1'))
	if !strings.Contains(a.confirmPrompt.View(), "Are you sure you want to add them?") || len(*calls) != 0 {
		t.Fatalf("Add Them should ask first, without pressing:\n%s", a.confirmPrompt.View())
	}
	pressKey(t, a, keyPress('1')) // Cancel: back to the buttons
	if !strings.Contains(a.confirmPrompt.View(), "[2] Dismiss") {
		t.Fatalf("Cancel should return to the buttons:\n%s", a.confirmPrompt.View())
	}
	pressKey(t, a, keyPress('1'))
	pressKey(t, a, keyPress('2')) // Add
	if len(*calls) != 1 || (*calls)[0].action.Name != "invite" {
		t.Errorf("presses = %+v, want one invite", *calls)
	}
}

func TestActionPromptURLButtonOpensLink(t *testing.T) {
	msg := mentionEphemeral(ephTS)
	msg.LegacyAttachments[0].Actions[1].URL = "https://example.com/docs"
	a, calls := actionApp(t, nil, msg)
	a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS})

	var opened string
	_, cmd := a.Update(keyPress('2'))
	for _, m := range drainBatch(cmd) {
		if o, ok := m.(OpenLinkMsg); ok {
			opened = o.URL
		}
	}
	if opened != "https://example.com/docs" || len(*calls) != 0 {
		t.Errorf("opened %q, presses %d; want the URL and no press", opened, len(*calls))
	}
}

// Review Focus 5: buttons past the ninth have no number key but are still
// reachable with Tab.
func TestActionPromptTenthButtonReachableByTab(t *testing.T) {
	msg := mentionEphemeral(ephTS)
	var acts []blocks.LegacyAction
	for i := 1; i <= 10; i++ {
		acts = append(acts, blocks.LegacyAction{ID: fmt.Sprint(i), Name: fmt.Sprint("b", i), Text: fmt.Sprint("B", i), Type: "button"})
	}
	msg.LegacyAttachments[0].Actions = acts
	a, calls := actionApp(t, nil, msg)
	a.openActionPrompt(actionPromptRef{channelID: "C1", ts: ephTS})

	if strings.Contains(a.confirmPrompt.View(), "[10]") {
		t.Error("the tenth button must not get a number key")
	}
	for range 10 {
		pressKey(t, a, keyCode(tea.KeyTab))
	}
	pressKey(t, a, keyCode(tea.KeyEnter))
	if len(*calls) != 1 || (*calls)[0].action.Name != "b10" {
		t.Errorf("presses = %+v, want b10", *calls)
	}
}

func TestButtonActionsSkipsSelects(t *testing.T) {
	msg := mentionEphemeral(ephTS)
	msg.LegacyAttachments[0].Actions = []blocks.LegacyAction{{Name: "env", Text: "Env", Type: "select"}}
	if messages.HasButtonActions(msg) || len(messages.ButtonActions(msg)) != 0 {
		t.Error("select actions are not buttons")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'ActionPrompt|ButtonActions' -count=1`
Expected: FAIL to compile — `undefined: actionPromptRef`, `messages.HasButtonActions`.

- [ ] **Step 3: `messages.ButtonActions`**

Append to `internal/ui/messages/ephemeral.go`, adding the `blockkit` import if the file lacks it:

```go
// ButtonAction is one pressable legacy button and the attachment it is on.
type ButtonAction struct {
	Attachment blockkit.LegacyAttachment
	Action     blockkit.LegacyAction
}

// ButtonActions returns msg's legacy buttons in order. Select menus are not
// buttons and are skipped.
func ButtonActions(msg MessageItem) []ButtonAction {
	var out []ButtonAction
	for _, att := range msg.LegacyAttachments {
		for _, act := range att.Actions {
			if act.Type == "button" {
				out = append(out, ButtonAction{Attachment: att, Action: act})
			}
		}
	}
	return out
}

// HasButtonActions reports whether msg has a legacy button to press.
func HasButtonActions(msg MessageItem) bool {
	for _, att := range msg.LegacyAttachments {
		for _, act := range att.Actions {
			if act.Type == "button" {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Shared prompt setup and the `shownPrompt` field**

In `internal/ui/confirm.go`, split `openConfirmPrompt`:

```go
// prepareConfirmPrompt pushes the current theme and width before the
// prompt opens.
func (a *App) prepareConfirmPrompt() {
	a.confirmPrompt.SetStyles(confirmPromptStyles())
	a.confirmPrompt.SetWidth(a.width)
}

// openConfirmPrompt raises the yes/no prompt and switches to ModeConfirm.
func (a *App) openConfirmPrompt(title, body string, onConfirm confirmprompt.ConfirmFunc) {
	a.prepareConfirmPrompt()
	a.confirmPrompt.Open(title, body, onConfirm)
	a.SetMode(ModeConfirm)
}
```

In `internal/ui/app.go`, add next to `confirmPrompt`:

```go
	// shownPrompt is the ephemeral message whose buttons the confirm
	// prompt is showing; nil when it shows anything else, or nothing.
	shownPrompt *actionPromptRef
```

In `afterConfirmPrompt` (`mode_confirm.go`), clear it when the prompt is hidden:

```go
	if !a.confirmPrompt.IsVisible() {
		a.shownPrompt = nil
		a.SetMode(ModeNormal)
	}
	return cmd
```

- [ ] **Step 5: Build, open and press**

Create `internal/ui/action_prompt.go`:

```go
// internal/ui/action_prompt.go
//
// The action dialog: the confirm prompt showing an ephemeral message's
// legacy buttons. Building the choice and pressing live here; the reducer
// arm for press results is in reducer_action_prompt.go.
package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// actionPromptRef identifies an ephemeral message with buttons.
type actionPromptRef struct {
	channelID, ts, threadTS string
}

func (r actionPromptRef) key() string { return r.channelID + "/" + r.ts }

// actionPressedMsg reports a button press; err is nil on success, when
// Slack answers by deleting the ephemeral over the WebSocket.
type actionPressedMsg struct {
	label string
	err   error
}

// findActionMessage looks ref's message up in the pane showing it: the
// open thread for a reply, otherwise any window on the channel.
func (a *App) findActionMessage(ref actionPromptRef) (messages.MessageItem, bool) {
	if ref.threadTS != "" && ref.threadTS != ref.ts {
		if a.threadVisible && a.threadPanel.ChannelID() == ref.channelID && a.threadPanel.ThreadTS() == ref.threadTS {
			for _, r := range a.threadPanel.Replies() {
				if r.TS == ref.ts {
					return r, true
				}
			}
		}
		return messages.MessageItem{}, false
	}
	for _, mm := range a.modelsForChannel(ref.channelID) {
		for _, m := range mm.Messages() {
			if m.TS == ref.ts {
				return m, true
			}
		}
	}
	return messages.MessageItem{}, false
}

// openActionPrompt opens the dialog for ref's buttons. False when the
// message is no longer shown or has no buttons.
func (a *App) openActionPrompt(ref actionPromptRef) bool {
	msg, ok := a.findActionMessage(ref)
	if !ok || !messages.HasButtonActions(msg) {
		return false
	}
	if a.mode == ModeInsert {
		a.compose.Blur()
		a.threadCompose.Blur()
	}
	a.prepareConfirmPrompt()
	a.confirmPrompt.OpenChoice(a.actionChoice(ref, msg))
	a.shownPrompt = &ref
	a.SetMode(ModeConfirm)
	return true
}

// actionChoice is the dialog for msg: its sender, its text, one button per
// legacy button with number keys 1-9, nothing highlighted, and unbound keys
// ignored.
func (a *App) actionChoice(ref actionPromptRef, msg messages.MessageItem) confirmprompt.Choice {
	choice := &confirmprompt.Choice{Title: msg.UserName, Body: a.actionBody(msg), Default: -1}
	for i, ba := range messages.ButtonActions(msg) {
		b := confirmprompt.Button{Label: ba.Action.Text}
		if i < 9 {
			b.Keys = key.NewBinding(key.WithKeys(strconv.Itoa(i + 1)))
		}
		switch {
		case ba.Action.URL != "":
			url := ba.Action.URL
			b.Action = func() tea.Msg { return OpenLinkMsg{URL: url} }
		case ba.Action.Confirm != nil:
			b.Then = a.actionConfirmChoice(ref, ba, choice)
		default:
			b.Action = a.pressAction(ref, ba)
		}
		choice.Buttons = append(choice.Buttons, b)
	}
	return *choice
}

// actionConfirmChoice is Slack's confirmation step for ba. Its dismiss
// button returns to back; its OK button presses.
func (a *App) actionConfirmChoice(ref actionPromptRef, ba messages.ButtonAction, back *confirmprompt.Choice) *confirmprompt.Choice {
	c := ba.Action.Confirm
	title, dismiss, ok := c.Title, c.DismissText, c.OKText
	if title == "" {
		title = "Are you sure?"
	}
	if dismiss == "" {
		dismiss = "Cancel"
	}
	if ok == "" {
		ok = "Okay"
	}
	return &confirmprompt.Choice{
		Title:   title,
		Body:    c.Text,
		Default: -1,
		Buttons: []confirmprompt.Button{
			{Label: dismiss, Keys: key.NewBinding(key.WithKeys("1")), Then: back},
			{Label: ok, Keys: key.NewBinding(key.WithKeys("2")), Action: a.pressAction(ref, ba)},
		},
	}
}

// actionBody is the message text, then each attachment's pretext and text,
// flattened to plain text.
func (a *App) actionBody(msg messages.MessageItem) string {
	user := func(id string) (string, bool) {
		n, ok := a.userNames[id]
		return n, ok && n != ""
	}
	parts := []string{messages.FlattenMrkdwn(messages.MessageTextSource(msg), user, nil)}
	for _, att := range msg.LegacyAttachments {
		for _, s := range []string{att.Pretext, att.Text} {
			if s != "" {
				parts = append(parts, messages.FlattenMrkdwn(s, user, nil))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// pressAction is a button's action: press it through the interaction
// service, off the UI goroutine, and report the result.
func (a *App) pressAction(ref actionPromptRef, ba messages.ButtonAction) func() tea.Msg {
	svc := a.interactions
	att, act := ba.Attachment, ba.Action
	return func() tea.Msg {
		err := svc.PressAttachmentAction(ids.ChannelID(ref.channelID), ids.MessageTS(ref.ts),
			att.ID, att.CallbackID, act, true)
		return actionPressedMsg{label: act.Text, err: err}
	}
}
```

Create `internal/ui/reducer_action_prompt.go`:

```go
// internal/ui/reducer_action_prompt.go
//
// Reducer for the action dialog's asynchronous results.
package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

var reduceActionPrompt reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case actionPressedMsg:
		if m.err == nil {
			return nil, true // Slack deletes the ephemeral over the WebSocket
		}
		text := fmt.Sprintf("Couldn't send %s: %v", m.label, m.err)
		return func() tea.Msg { return ToastMsg{Text: text} }, true
	}
	return nil, false
}
```

In `internal/ui/app.go`, add `reduceActionPrompt,` to the `dispatchReducers(...)` list, directly after `reduceSend,`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/ui/ ./internal/ui/messages/ -run 'ActionPrompt|ButtonActions|Confirm|Golden' -count=1`
Expected: PASS.

- [ ] **Step 7: `AGENTS.md`**

In "Text and rendering", after the `messages.EphemeralLabel()` row, add:

```markdown
| A message's pressable legacy buttons (selects excluded) | `messages.ButtonActions(msg)`, `messages.HasButtonActions(msg)` |
| Open the action dialog for an ephemeral's buttons | `(*App).openActionPrompt(actionPromptRef)` (`internal/ui/action_prompt.go`); queueing and the interruption rule are `enqueueActionPrompt` / `tryOpenActionPrompt` |
```

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/ephemeral.go internal/ui/action_prompt.go internal/ui/reducer_action_prompt.go internal/ui/action_prompt_test.go internal/ui/confirm.go internal/ui/mode_confirm.go internal/ui/app.go AGENTS.md
git commit -m "feat(ui): action dialog presses an ephemeral's buttons"
```

---

### Task 7: Queue, auto-open and the interruption rule

**Files:**
- Modify: `internal/ui/action_prompt.go` (queue functions), `internal/ui/app.go` (fields `actionQueue`, `promptToasted`)
- Modify: `internal/ui/reducer_send.go` (the ephemeral branch of `reduceNewMessage`; the `WSMessageDeletedMsg` arm)
- Modify: `internal/ui/mode_confirm.go` (`afterConfirmPrompt` re-checks the queue)
- Modify: `internal/ui/mode_insert.go` (Esc exit; the two send paths)
- Create: `internal/ui/action_prompt_queue_test.go`

**Interfaces:**
- Consumes: `openActionPrompt`, `findActionMessage`, `actionPromptRef`, `afterConfirmPrompt`, `shownPrompt` (Task 6); `messages.HasButtonActions`.
- Produces:
  - `(*App).enqueueActionPrompt(ref) tea.Cmd`
  - `(*App).tryOpenActionPrompt() tea.Cmd`
  - `(*App).dropActionPrompt(channelID, ts string) tea.Cmd`
  - fields `a.actionQueue []actionPromptRef`, `a.promptToasted map[string]bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/action_prompt_queue_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// arrive delivers an ephemeral over the real Update path and returns the
// messages its commands produced.
func arrive(t *testing.T, a *App, msg messages.MessageItem) []tea.Msg {
	t.Helper()
	_, cmd := a.Update(NewMessageMsg{ChannelID: "C1", Message: msg})
	return drainBatch(cmd)
}

func dialogShows(a *App, text string) bool {
	return a.confirmPrompt.IsVisible() && strings.Contains(a.confirmPrompt.View(), text)
}

func TestEphemeralWithButtonsOpensDialog(t *testing.T) {
	a, _ := actionApp(t, nil)
	arrive(t, a, mentionEphemeral(ephTS))
	if a.mode != ModeConfirm || !dialogShows(a, "[2] Dismiss") {
		t.Errorf("mode = %v; the dialog should open for an ephemeral with buttons", a.mode)
	}
}

func TestEphemeralWithoutButtonsDoesNotOpen(t *testing.T) {
	a, _ := actionApp(t, nil)
	msg := mentionEphemeral(ephTS)
	msg.LegacyAttachments = nil
	arrive(t, a, msg)
	if a.confirmPrompt.IsVisible() {
		t.Error("no buttons, no dialog")
	}
}

func TestTypingDefersDialogWithOneToast(t *testing.T) {
	a, _ := actionApp(t, nil)
	a.SetMode(ModeInsert)
	a.compose.SetValue("half a thought")

	if got := toastText(arrive(t, a, mentionEphemeral(ephTS))); got != "Slackbot: press b to respond" {
		t.Errorf("toast = %q", got)
	}
	if a.mode != ModeInsert || a.confirmPrompt.IsVisible() {
		t.Fatal("the dialog must not interrupt typing")
	}
	if got := toastText(drainBatch(a.tryOpenActionPrompt())); got != "" {
		t.Errorf("the waiting toast repeated: %q", got)
	}

	pressKey(t, a, keyCode(tea.KeyEscape)) // leave insert mode
	if !dialogShows(a, "[2] Dismiss") {
		t.Error("leaving insert mode should open the waiting dialog")
	}
	if a.compose.Value() != "half a thought" {
		t.Error("the draft must survive")
	}
}

func TestEmptyComposerInInsertOpensDialog(t *testing.T) {
	a, _ := actionApp(t, nil)
	a.SetMode(ModeInsert)
	arrive(t, a, mentionEphemeral(ephTS))
	if !dialogShows(a, "[2] Dismiss") {
		t.Error("an empty composer is not typing: the dialog should open")
	}
}

// Review Focus 2: another modal is up -> wait, then open when it closes.
func TestDialogWaitsForOtherModal(t *testing.T) {
	a, _ := actionApp(t, nil)
	a.openQuitConfirm()
	arrive(t, a, mentionEphemeral(ephTS))
	if !dialogShows(a, "Quit slk?") {
		t.Fatal("the quit prompt must stay on top")
	}
	pressKey(t, a, keyPress('n'))
	if !dialogShows(a, "[2] Dismiss") {
		t.Error("the action dialog should open once the quit prompt closes")
	}
}

func TestQueueOpensInOrder(t *testing.T) {
	a, _ := actionApp(t, nil)
	first, second := mentionEphemeral("5.000000"), mentionEphemeral("6.000000")
	second.UserName = "Giphy"
	arrive(t, a, first)
	arrive(t, a, second)
	if !dialogShows(a, "Slackbot") {
		t.Fatal("the first ephemeral should be showing")
	}
	pressKey(t, a, keyCode(tea.KeyEscape))
	if !dialogShows(a, "Giphy") {
		t.Error("closing the first should open the second")
	}
}

// Review Focus 3: a queued message no longer in any pane is skipped.
func TestQueueSkipsMessageNoLongerShown(t *testing.T) {
	a, _ := actionApp(t, nil)
	a.openQuitConfirm()
	gone, kept := mentionEphemeral("5.000000"), mentionEphemeral("6.000000")
	kept.UserName = "Giphy"
	arrive(t, a, gone)
	arrive(t, a, kept)
	a.messagepane.RemoveMessageByTS("5.000000") // e.g. the user switched away and back
	pressKey(t, a, keyPress('n'))
	if !dialogShows(a, "Giphy") {
		t.Error("the missing message should be skipped and the next one opened")
	}
}

// Review Focus 4: deleting the shown message closes its dialog, and no
// later key presses a button of a message that no longer exists.
func TestDeleteClosesItsDialog(t *testing.T) {
	a, calls := actionApp(t, nil)
	arrive(t, a, mentionEphemeral(ephTS))
	_, _ = a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: ephTS})
	if a.confirmPrompt.IsVisible() || a.mode != ModeNormal {
		t.Fatal("deleting the message should close its dialog")
	}
	pressKey(t, a, keyPress('2'))
	if len(*calls) != 0 {
		t.Error("a key after the delete pressed a button")
	}
}

func TestDeleteDropsQueuedMessage(t *testing.T) {
	a, _ := actionApp(t, nil)
	a.openQuitConfirm()
	arrive(t, a, mentionEphemeral(ephTS))
	_, _ = a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: ephTS})
	pressKey(t, a, keyPress('n'))
	if a.confirmPrompt.IsVisible() {
		t.Error("a deleted message must not open once the quit prompt closes")
	}
}

func TestDeleteOfOtherMessageLeavesQuitPrompt(t *testing.T) {
	a, _ := actionApp(t, nil)
	a.openQuitConfirm()
	_, _ = a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "9.000000"})
	if !dialogShows(a, "Quit slk?") {
		t.Error("an unrelated delete must not close the quit prompt")
	}
}

func TestEphemeralReplyInOpenThreadOpensDialog(t *testing.T) {
	a, _ := actionApp(t, nil)
	openThreadPanel(a, "C1", "1.000000")
	reply := mentionEphemeral(ephTS)
	reply.ThreadTS = "1.000000"
	arrive(t, a, reply)
	if !dialogShows(a, "[2] Dismiss") {
		t.Error("an ephemeral reply in the open thread should open its dialog")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'Ephemeral.*Dialog|Typing|EmptyComposer|DialogWaits|Queue|Delete.*Dialog|DeleteDrops|DeleteOfOther|EphemeralWith' -count=1`
Expected: FAIL to compile — `a.tryOpenActionPrompt undefined`.

- [ ] **Step 3: Queue state and functions**

In `internal/ui/app.go`, next to `shownPrompt`, add:

```go
	// actionQueue holds ephemerals with buttons waiting for the action
	// dialog, oldest first; promptToasted records which have shown the
	// "press b" toast, so it appears once per message.
	actionQueue   []actionPromptRef
	promptToasted map[string]bool
```

Append to `internal/ui/action_prompt.go`:

```go
// enqueueActionPrompt queues ref and opens it if the user is not busy.
func (a *App) enqueueActionPrompt(ref actionPromptRef) tea.Cmd {
	a.actionQueue = append(a.actionQueue, ref)
	return a.tryOpenActionPrompt()
}

// tryOpenActionPrompt opens the oldest queued dialog unless the user is
// busy: another modal is up, or they are typing (insert mode with text in
// the focused composer), in which case it waits and toasts once. Queued
// messages no longer shown are dropped. Called on enqueue and from the
// three re-check points only: the confirm prompt closing, leaving insert
// mode, and a send clearing the composer.
func (a *App) tryOpenActionPrompt() tea.Cmd {
	for len(a.actionQueue) > 0 {
		if a.mode.IsModalOverlay() {
			return nil
		}
		ref := a.actionQueue[0]
		if a.mode == ModeInsert && a.focusedComposerValue() != "" {
			return a.toastActionWaiting(ref)
		}
		a.actionQueue = a.actionQueue[1:]
		delete(a.promptToasted, ref.key())
		if a.openActionPrompt(ref) {
			return nil
		}
	}
	return nil
}

func (a *App) focusedComposerValue() string {
	if a.focusedPanel == PanelThread && a.threadVisible {
		return a.threadCompose.Value()
	}
	return a.compose.Value()
}

// toastActionWaiting tells the user, once per message, that a dialog is
// waiting for them.
func (a *App) toastActionWaiting(ref actionPromptRef) tea.Cmd {
	if a.promptToasted[ref.key()] {
		return nil
	}
	if a.promptToasted == nil {
		a.promptToasted = map[string]bool{}
	}
	a.promptToasted[ref.key()] = true
	sender := "A message"
	if msg, ok := a.findActionMessage(ref); ok && msg.UserName != "" {
		sender = msg.UserName
	}
	text := sender + ": press b to respond"
	return func() tea.Msg { return ToastMsg{Text: text} }
}

// dropActionPrompt forgets a deleted message: it leaves the queue, and if
// its dialog is showing, the dialog closes (and the next one may open).
func (a *App) dropActionPrompt(channelID, ts string) tea.Cmd {
	kept := a.actionQueue[:0]
	for _, r := range a.actionQueue {
		if r.channelID != channelID || r.ts != ts {
			kept = append(kept, r)
		}
	}
	a.actionQueue = kept
	delete(a.promptToasted, actionPromptRef{channelID: channelID, ts: ts}.key())
	if s := a.shownPrompt; s != nil && s.channelID == channelID && s.ts == ts && a.mode == ModeConfirm {
		a.confirmPrompt.Close()
		return a.afterConfirmPrompt(nil)
	}
	return nil
}
```

- [ ] **Step 4: Enqueue on arrival, drop on delete**

In `internal/ui/reducer_send.go`, `reduceNewMessage`, replace the ephemeral branch's `return nil` with:

```go
		onScreen := inOpenThreadPanel ||
			(m.ChannelID == a.activeChannelID && (m.Message.ThreadTS == "" || m.Message.ThreadTS == m.Message.TS))
		if onScreen && messages.HasButtonActions(m.Message) {
			return a.enqueueActionPrompt(actionPromptRef{
				channelID: m.ChannelID, ts: m.Message.TS, threadTS: m.Message.ThreadTS,
			})
		}
		return nil
```

In the `WSMessageDeletedMsg` arm, replace the final `return nil, true` with:

```go
		return a.dropActionPrompt(m.ChannelID, m.TS), true
```

- [ ] **Step 5: Re-check points**

1. **The prompt closing.** In `afterConfirmPrompt` (`mode_confirm.go`):

```go
	if !a.confirmPrompt.IsVisible() {
		a.shownPrompt = nil
		a.SetMode(ModeNormal)
		return tea.Batch(cmd, a.tryOpenActionPrompt())
	}
	return cmd
```

2. **Leaving insert mode.** In `handleInsertMode` (`mode_insert.go`), the Esc exit ends with `a.SetMode(ModeNormal)`, `a.compose.Blur()`, `a.threadCompose.Blur()`, `return nil`. Change that `return nil` to `return a.tryOpenActionPrompt()`.

3. **A send clearing the composer.** Both send paths in `handleInsertMode`, the thread reply after `a.threadCompose.Reset()` and the channel message after `a.compose.Reset()`, call `a.exitInsertAfterSend()` and then `return func() tea.Msg { return Send…Msg{…} }`. In each, bind the send to a local and batch the re-check:

```go
			send := func() tea.Msg {
				return SendMessageMsg{ /* unchanged fields */ }
			}
			return tea.Batch(send, a.tryOpenActionPrompt())
```

(and the same for the `SendThreadReplyMsg` path). Leave the fields exactly as they are.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/ui/ -count=1`
Expected: PASS: the new tests, Task 6's, every existing insert/send/delete/confirm test, and the goldens.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/action_prompt.go internal/ui/action_prompt_queue_test.go internal/ui/app.go internal/ui/reducer_send.go internal/ui/mode_confirm.go internal/ui/mode_insert.go
git commit -m "feat(ui): ephemeral buttons open a dialog unless the user is busy"
```

---

### Task 8: Human checkpoint: try it in a real workspace

The spec requires this: the owner judges the interaction by feel, before the guard, hint and polish land. **This is a planned stop. Do not start Task 9 until the owner has answered.** Any change they ask for becomes a ruling recorded in the ledger before Task 9.

**Files:** none.

- [ ] **Step 1: Verify and build**

Run: `go build ./... && go vet ./... && go test ./... -count=1 && go build -o /tmp/opencode/slk-action-dialog ./cmd/slk`
Expected: all pass; the binary is at `/tmp/opencode/slk-action-dialog`.

- [ ] **Step 2: Hand it over and stop**

Send the owner exactly this, then wait:

> Part B is far enough to try. Binary: `/tmp/opencode/slk-action-dialog` (run it with `SLK_DEBUG=1` to get `slk-debug.log`). Branch `feat/ephemeral-action-dialog`.
>
> 1. In a private channel, send a message that @-mentions a non-member. The Slackbot dialog should open by itself with `[1] Add Them  [2] Dismiss  [3] Don't Show Again`, nothing highlighted.
> 2. Try the keys: `h`/`l`/arrows/Tab move the highlight, Enter presses it, `1`–`3` press directly, Esc closes.
> 3. Try the mouse: click Dismiss, then trigger it again and click Add Them. Add Them should ask "Are you sure you want to add them?" first.
> 4. Start typing a message (leave text in the composer), then trigger the notice. It should wait and toast "Slackbot: press b to respond", then open when you press Esc.
> 5. Quit and delete prompts now show `[y] confirm   [n/Esc] cancel` as buttons you can click.
>
> `b`, blocking `r`/Enter/permalink and the rest on these messages, and the "b to respond" hint come next. Anything about how it feels you'd like changed first?

---

### Task 9: `b` to respond, and inert keys on ephemerals

**Files:**
- Modify: `internal/ui/keys.go` (`KeyMap` field, default binding)
- Modify: `internal/ui/action_prompt.go` (`selectedMessageInFocus`, `respondToSelected`, `ephemeralSelectionGuard`, `isMessageOpKey`, `toastCmd`; `openActionPrompt` also leaves the queue)
- Modify: `internal/ui/mode_normal.go` (the guard before the main switch; the `b` case)
- Create: `internal/ui/ephemeral_keys_test.go`

**Interfaces:**
- Consumes: `openActionPrompt`, `actionQueue`, `promptToasted` (Tasks 6–7); `messages.HasButtonActions`; `a.messagepane.SelectedMessage()`, `a.threadPanel.SelectedReply()`.
- Produces: `KeyMap.RespondButtons`; `(*App).respondToSelected() tea.Cmd`; `(*App).ephemeralSelectionGuard() (tea.Cmd, bool)`; `toastCmd(text string) tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/ephemeral_keys_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/help"
	"github.com/gammons/slk/internal/ui/messages"
)

const guardText = "Only visible to you — press b to respond"

// selectedEphemeralApp has a normal message and then msg in C1, msg
// selected in a focused messages pane.
func selectedEphemeralApp(t *testing.T, msg messages.MessageItem) (*App, *[]pressCall) {
	t.Helper()
	a, calls := actionApp(t, nil, messages.MessageItem{TS: "1.000000", UserID: "U1", UserName: "alice", Text: "hi"}, msg)
	a.focusedPanel = PanelMessages
	a.messagepane.SelectByIndex(1)
	return a, calls
}

func TestBOpensDialogForSelectedEphemeral(t *testing.T) {
	a, _ := selectedEphemeralApp(t, mentionEphemeral(ephTS))
	pressKey(t, a, keyPress('b'))
	if !dialogShows(a, "[2] Dismiss") {
		t.Error("b on an ephemeral with buttons should open its dialog")
	}
}

func TestBWithoutButtons(t *testing.T) {
	plain := mentionEphemeral(ephTS)
	plain.LegacyAttachments = nil
	for name, a := range map[string]*App{
		"ephemeral without buttons": func() *App { a, _ := selectedEphemeralApp(t, plain); return a }(),
		"normal message": func() *App {
			a, _ := selectedEphemeralApp(t, mentionEphemeral(ephTS))
			a.messagepane.SelectByIndex(0)
			return a
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if got := toastText(pressKey(t, a, keyPress('b'))); got != "No buttons on this message" {
				t.Errorf("toast = %q", got)
			}
			if a.confirmPrompt.IsVisible() {
				t.Error("no dialog without buttons")
			}
		})
	}
}

// The keys that would send an ephemeral's ts to Slack are inert on it.
func TestMessageOpKeysAreInertOnEphemeral(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		keyCode(tea.KeyEnter), keyPress('r'), keyPress('R'), keyPress('L'),
		keyPress('Y'), keyPress('C'), keyPress('F'), keyPress('U'), keyPress('S'),
	} {
		t.Run(k.String(), func(t *testing.T) {
			a, calls := selectedEphemeralApp(t, mentionEphemeral(ephTS))
			if got := toastText(pressKey(t, a, k)); got != guardText {
				t.Errorf("toast = %q, want %q", got, guardText)
			}
			if a.mode != ModeNormal || a.threadVisible || len(*calls) != 0 {
				t.Errorf("mode=%v threadVisible=%v presses=%d: the key must do nothing else", a.mode, a.threadVisible, len(*calls))
			}
		})
	}
}

func TestGuardTextWithoutButtons(t *testing.T) {
	plain := mentionEphemeral(ephTS)
	plain.LegacyAttachments = nil
	a, _ := selectedEphemeralApp(t, plain)
	if got := toastText(pressKey(t, a, keyPress('r'))); got != "Only visible to you" {
		t.Errorf("toast = %q", got)
	}
}

func TestCopyStillWorksOnEphemeral(t *testing.T) {
	a, _ := selectedEphemeralApp(t, mentionEphemeral(ephTS))
	if got := toastText(pressKey(t, a, keyPress('y'))); strings.HasPrefix(got, "Only visible to you") {
		t.Errorf("y must not be guarded; toast = %q", got)
	}
}

func TestGuardLeavesNormalMessagesAlone(t *testing.T) {
	a, _ := selectedEphemeralApp(t, mentionEphemeral(ephTS))
	a.messagepane.SelectByIndex(0)
	if got := toastText(pressKey(t, a, keyPress('r'))); strings.HasPrefix(got, "Only visible to you") {
		t.Errorf("a normal message must not be guarded; toast = %q", got)
	}
}

func TestBDoesNotReopenAfterClose(t *testing.T) {
	a, _ := selectedEphemeralApp(t, mentionEphemeral(ephTS))
	a.actionQueue = []actionPromptRef{{channelID: "C1", ts: ephTS}} // queued earlier, while typing
	pressKey(t, a, keyPress('b'))
	pressKey(t, a, keyCode(tea.KeyEscape))
	if a.confirmPrompt.IsVisible() {
		t.Error("opening with b must take the message off the queue")
	}
}

func TestHelpListsB(t *testing.T) {
	for _, e := range help.FromKeyMap(DefaultKeyMap()) {
		if e.Key == "b" && e.Desc == "respond to message buttons" {
			return
		}
	}
	t.Error("help overlay is missing b")
}
```



- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestB|Inert|GuardText|CopyStill|GuardLeaves|HelpListsB' -count=1`
Expected: FAIL — `b` does nothing, and the guarded keys act.

- [ ] **Step 3: The binding**

In `internal/ui/keys.go`, add `RespondButtons key.Binding` to the `KeyMap` struct next to `SaveThread`, and to the default key map:

```go
		RespondButtons: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "respond to message buttons")),
```

- [ ] **Step 4: Respond, guard, and leave the queue**

Append to `internal/ui/action_prompt.go` (add `"charm.land/bubbles/v2/key"` if absent; it's already imported in Task 6):

```go
func toastCmd(text string) tea.Cmd { return func() tea.Msg { return ToastMsg{Text: text} } }

// selectedMessageInFocus returns the selected message in the focused pane
// and the ref that finds it again.
func (a *App) selectedMessageInFocus() (messages.MessageItem, actionPromptRef, bool) {
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		return msg, actionPromptRef{channelID: a.activeChannelID, ts: msg.TS}, ok
	case PanelThread:
		if r := a.threadPanel.SelectedReply(); r != nil {
			return *r, actionPromptRef{channelID: a.threadPanel.ChannelID(), ts: r.TS, threadTS: a.threadPanel.ThreadTS()}, true
		}
	}
	return messages.MessageItem{}, actionPromptRef{}, false
}

// respondToSelected is b: open the selected ephemeral's dialog now (the user
// asked, so the interruption rule does not apply).
func (a *App) respondToSelected() tea.Cmd {
	msg, ref, ok := a.selectedMessageInFocus()
	if !ok || !msg.Ephemeral || !messages.HasButtonActions(msg) || !a.openActionPrompt(ref) {
		return toastCmd("No buttons on this message")
	}
	return nil
}

// isMessageOpKey reports keys that send the selected message's ts to Slack.
func (a *App) isMessageOpKey(msg tea.KeyMsg) bool {
	k := a.keys
	for _, b := range []key.Binding{k.Enter, k.Reaction, k.ReactionNav, k.ListReactions,
		k.CopyPermalink, k.ForwardMessage, k.MarkUnread, k.SaveThread} {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

// ephemeralSelectionGuard blocks a message op on a selected ephemeral:
// Slack does not know its ts, so the op would fail or act on a ghost.
func (a *App) ephemeralSelectionGuard() (tea.Cmd, bool) {
	msg, _, ok := a.selectedMessageInFocus()
	if !ok || !msg.Ephemeral {
		return nil, false
	}
	if messages.HasButtonActions(msg) {
		return toastCmd("Only visible to you — press b to respond"), true
	}
	return toastCmd("Only visible to you"), true
}
```

In `openActionPrompt`, just before `a.prepareConfirmPrompt()`, take the message off the queue so it doesn't reopen after closing:

```go
	kept := a.actionQueue[:0]
	for _, r := range a.actionQueue {
		if r.key() != ref.key() {
			kept = append(kept, r)
		}
	}
	a.actionQueue = kept
	delete(a.promptToasted, ref.key())
```

and remove the now-redundant `delete(a.promptToasted, ref.key())` line from `tryOpenActionPrompt`.

In `internal/ui/mode_normal.go`, `handleNormalMode`, immediately before the main `switch {` (the one whose first case is `key.Matches(msg, a.keys.InsertMode)`, right after the thread reaction-nav check), add:

```go
	if a.isMessageOpKey(msg) {
		if cmd, blocked := a.ephemeralSelectionGuard(); blocked {
			return cmd
		}
	}
```

and in that switch, next to `case key.Matches(msg, a.keys.SaveThread):`, add:

```go
	case key.Matches(msg, a.keys.RespondButtons):
		return a.respondToSelected()
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/ui/ -count=1`
Expected: PASS: the new tests, Tasks 6–7, every normal-mode key test, and the goldens.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/keys.go internal/ui/action_prompt.go internal/ui/mode_normal.go internal/ui/ephemeral_keys_test.go
git commit -m "feat(ui): b responds to an ephemeral's buttons; message ops are inert on ephemerals"
```

---

### Task 10: "b to respond" hint in both panes

**Files:**
- Modify: `internal/ui/messages/ephemeral.go` (`InteractiveHint`)
- Modify: `internal/ui/messages/model.go` (the `↗ open in Slack to interact` site, ~line 2364)
- Modify: `internal/ui/thread/model.go` (the matching site, ~line 2109)
- Test: `internal/ui/messages/blockkit_integration_test.go`, `internal/ui/thread/render_test.go`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: `messages.HasButtonActions` (Task 6).
- Produces: `messages.InteractiveHint(msg MessageItem) string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/messages/blockkit_integration_test.go` (it already imports `blockkit`, whose `LegacyAction` aliases `blocks.LegacyAction`):

```go
func TestEphemeralButtonsHintSaysPressB(t *testing.T) {
	msg := MessageItem{
		TS: "1700000000.000000", UserName: "Slackbot", Timestamp: "1:23 PM", Ephemeral: true,
		Text: "not in channel",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Actions: []blockkit.LegacyAction{{Name: "ignore", Text: "Dismiss", Type: "button"}},
		}},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "b to respond") || strings.Contains(plain, "open in Slack") {
		t.Errorf("want the b hint instead of the Slack one: %q", plain)
	}
	msg.Ephemeral = false
	if plain := renderedFor(t, msg, 100); !strings.Contains(plain, "↗ open in Slack to interact") {
		t.Errorf("a non-ephemeral keeps the Slack hint: %q", plain)
	}
}
```

Append to `internal/ui/thread/render_test.go` (it already imports `blockkit` and `ansi`):

```go
func TestRenderThreadMessageEphemeralButtonsHint(t *testing.T) {
	m := New()
	msg := messages.MessageItem{
		TS: "1700000006.000000", UserName: "Slackbot", Timestamp: "10:35 AM", Ephemeral: true,
		Text: "not in channel",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Actions: []blockkit.LegacyAction{{Name: "ignore", Text: "Dismiss", Type: "button"}},
		}},
	}
	got, _, _, _, _ := m.renderThreadMessage(msg, 60, nil, nil, false)
	if plain := ansi.Strip(got); !strings.Contains(plain, "b to respond") || strings.Contains(plain, "open in Slack") {
		t.Errorf("want the b hint in the thread pane too: %q", plain)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/messages/ ./internal/ui/thread/ -run 'ButtonsHint' -count=1`
Expected: FAIL — the Slack hint is shown.

- [ ] **Step 3: Implement**

Append to `internal/ui/messages/ephemeral.go`:

```go
// InteractiveHint is the line under a message with interactive elements:
// an ephemeral with buttons can be answered in slk with b; anything else
// still needs Slack.
func InteractiveHint(msg MessageItem) string {
	if msg.Ephemeral && HasButtonActions(msg) {
		return "b to respond"
	}
	return "↗ open in Slack to interact"
}
```

In `internal/ui/messages/model.go`, replace `hint := styles.Timestamp.Render("↗ open in Slack to interact")` with `hint := styles.Timestamp.Render(InteractiveHint(msg))`.

In `internal/ui/thread/model.go`, replace `styles.Timestamp.Render("↗ open in Slack to interact")` with `styles.Timestamp.Render(messages.InteractiveHint(msg))`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/ui/... -count=1`
Expected: PASS, including `TestRenderMessagePlainAppendsHintWhenInteractive`, the lockstep test and the goldens.

- [ ] **Step 5: `AGENTS.md`**

In "Text and rendering", after the `messages.ButtonActions` row, add:

```markdown
| The hint under an interactive message ("b to respond" for an ephemeral with buttons, else "open in Slack") | `messages.InteractiveHint(msg)`; both panes |
```

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/ephemeral.go internal/ui/messages/model.go internal/ui/thread/model.go internal/ui/messages/blockkit_integration_test.go internal/ui/thread/render_test.go AGENTS.md
git commit -m "feat(ui): ephemeral buttons hint 'b to respond' in both panes"
```

---

### Task 11: Full verification

**Files:** none, unless a check fails.

- [ ] **Step 1: Format, build, vet, lint**

Run: `gofmt -l . && go build ./... && go vet ./... && golangci-lint run`
Expected: no `gofmt` output; build and vet succeed; `0 issues.`

- [ ] **Step 2: Full race suite**

Run: `go test ./... -race -count=1`
Expected: PASS. The goldens pass without `-update`.

- [ ] **Step 3: Live check (human)**

Repeat Task 8's list on a fresh build, plus:
- **`b`:** with the dialog dismissed by Esc, select the Slackbot message and press `b`. The dialog reopens.
- **Blocked keys:** on that message, `r`, Enter, `Y`, `F` and `U` each show "Only visible to you — press b to respond" and do nothing else; `y` still copies.
- **Hint:** under the message, the hint reads "b to respond".
- **Each button:** pressing Dismiss makes the message disappear (Slack's `message_deleted`). Add Them asks first, and on Add the person is invited. Don't Show Again works like Dismiss.

- [ ] **Step 4: Commit any fixes**

If any step required a change, commit it with a message naming the check that failed (e.g. `style: gofmt`).
