package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/workspace"
)

// modalHighlightedRow returns the plain text of the highlighted row in
// a rendered modal box.
//
// Every finder-style modal in this package marks its selected row with
// a U+258C left half-block in styles.Accent (workspacefinder,
// themeswitcher, presencemenu, reactionpicker, newmessagepicker,
// linkpicker all render the same glyph). The same glyph is also the
// query input's left border, which is why the scan starts at box-local
// row 5 — the `listTopOffset` every one of those packages declares:
// top border, top padding, title, input, blank separator.
//
// Returns the row with the indicator, surrounding box chrome and any
// scrollbar gutter trimmed, so callers should use strings.Contains
// rather than equality.
func modalHighlightedRow(t *testing.T, box string) string {
	t.Helper()
	const firstRow = 5
	lines := strings.Split(stripANSI(box), "\n")
	for i := firstRow; i < len(lines); i++ {
		idx := strings.Index(lines[i], "\u258c")
		if idx < 0 {
			continue
		}
		row := lines[i][idx+len("\u258c"):]
		return strings.TrimSpace(strings.Trim(row, "\u2502\u2588 "))
	}
	t.Fatalf("no highlighted row (\u258c) at or below box row %d in:\n%s", firstRow, stripANSI(box))
	return ""
}

// modalRows is the number of result rows a finder-style modal is
// currently showing, derived from the modal's own height math: every
// one of these packages sizes its box as nRows + 7 (top border, top
// padding, title, input, blank separator, bottom padding, bottom
// border), so h - 7 recovers the row count.
//
// This is the only window onto the filter state of presencemenu and
// themeswitcher, which expose neither a Query getter nor a Selected
// getter, and it is the portable form for the ones that do.
//
// Two gotchas, both load-bearing for callers:
//   - most of these models floor nRows at 1, so an empty filtered list
//     reports ONE row, not zero;
//   - newmessagepicker.BoxSize measures the rendered box instead of
//     nRows + 7, so it is NOT usable here.
func modalRows(bs interface{ BoxSize(int, int) (int, int) }) int {
	_, h := bs.BoxSize(120, 30)
	return h - 7
}

// workspaceFinderOpts seeds three workspaces. SetWorkspaces fills both
// the rail (whose SelectedID becomes "T1") and the finder's item list,
// which is what makes the handler's "did the user pick a different
// workspace" guard testable.
func workspaceFinderOpts() []testOpt {
	return []testOpt{withWorkspaces(
		workspace.WorkspaceItem{ID: "T1", Name: "alpha", Initials: "AL"},
		workspace.WorkspaceItem{ID: "T2", Name: "beta", Initials: "BE"},
		workspace.WorkspaceItem{ID: "T3", Name: "gamma", Initials: "GA"},
	)}
}

// switchedTeamMsg is what the test's SwitchWorkspaceFunc returns, so
// running the handler's tea.Cmd proves which team ID was captured.
type switchedTeamMsg struct{ teamID string }

func openWorkspaceFinder(t *testing.T, a *App) {
	a.SetWorkspaceSwitcher(func(teamID string) tea.Msg {
		return switchedTeamMsg{teamID: teamID}
	})
	a.workspaceFinder.Open()
	if !a.workspaceFinder.IsVisible() {
		t.Fatal("precondition: workspace finder did not open")
	}
	if got := a.workspaceRail.SelectedID(); got != "T1" {
		t.Fatalf("precondition: rail SelectedID = %q, want %q", got, "T1")
	}
	if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "alpha") {
		t.Fatalf("precondition: highlighted row = %q, want it to contain %q", got, "alpha")
	}
}

// TestWorkspaceFinderModeKeys characterizes handleWorkspaceFinderMode
// (mode_workspace_finder.go:16).
func TestWorkspaceFinderModeKeys(t *testing.T) {
	runKeyCases(t, ModeWorkspaceFinder, []keyCase{
		{
			name:     "esc closes the finder and returns to Normal",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.workspaceFinder.IsVisible() {
					t.Error("finder still visible after esc")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:  "enter on a different workspace returns the switcher cmd",
			opts:  workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) { openWorkspaceFinder(t, a); moveFinderDown(t, a, "beta") },
			key:   keyCode(tea.KeyEnter),

			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.workspaceFinder.IsVisible() {
					t.Error("finder still visible after enter")
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the workspace-switch cmd")
				}
				msg, ok := cmd().(switchedTeamMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want switchedTeamMsg", cmd())
				}
				if msg.teamID != "T2" {
					t.Errorf("switched to %q, want %q", msg.teamID, "T2")
				}
			},
		},
		{
			// The guard is `result.ID != workspaceRail.SelectedID()`,
			// so re-picking the workspace you are already on closes the
			// finder without a network round trip.
			name:     "enter on the already-active workspace closes without switching",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.workspaceFinder.IsVisible() {
					t.Error("finder still visible after enter")
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil: re-picking the active workspace must not switch", cmd())
				}
			},
		},
		{
			name: "enter with no switcher wired still closes to Normal",
			opts: workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) {
				a.workspaceFinder.Open()
				moveFinderDown(t, a, "beta")
				if a.workspaceSwitcher != nil {
					t.Fatal("precondition: workspaceSwitcher should be nil")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
				if a.workspaceFinder.IsVisible() {
					t.Error("finder still visible after enter")
				}
			},
		},
		{
			// filtered is empty, so workspacefinder.HandleKey's "enter"
			// arm returns nil and the finder stays open.
			name: "enter with no matches is a no-op",
			opts: workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) {
				openWorkspaceFinder(t, a)
				for _, r := range "zzz" {
					_ = dispatchModeKey(a, keyPress(r))
				}
				if got := modalRows(&a.workspaceFinder); got != 1 {
					t.Fatalf("precondition: %d rows for a non-matching query, want the 1-row floor", got)
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.workspaceFinder.IsVisible() {
					t.Error("finder closed on a no-match enter")
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			name:     "down moves the highlight to the next workspace",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "beta") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "beta")
				}
			},
		},
		{
			// Pins the normalisation switch, which is otherwise
			// invisible: Key.String() prefixes active modifiers before
			// the special-key name (ultraviolet key.go:413-431, 459),
			// so shift+down arrives as "shift+down" and
			// workspacefinder.HandleKey ignores it; `case tea.KeyDown`
			// rewrites it to "down". An unmodified KeyDown stringifies
			// to "down" on its own, so no other row can tell the arm
			// from a no-op.
			name:     "shift+down navigates: the Code switch strips the modifier",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyMod(tea.KeyDown, tea.ModShift),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "beta") {
					t.Errorf("highlighted row = %q, want it to contain %q: shift+down should normalise to down", got, "beta")
				}
			},
		},
		{
			name:     "ctrl+n moves the highlight like down",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyMod('n', tea.ModCtrl),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "beta") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "beta")
				}
			},
		},
		{
			name:  "up moves the highlight back",
			opts:  workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) { openWorkspaceFinder(t, a); moveFinderDown(t, a, "beta") },
			key:   keyCode(tea.KeyUp),

			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "alpha") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "alpha")
				}
			},
		},
		{
			// The setup walks DOWN to gamma and back UP with the same
			// ctrl+p under test, so this row separates "clamped at the
			// top" from "ignored entirely". Asserting only that the
			// highlight is on alpha after one ctrl+p would pass against
			// a handler that did nothing — alpha is where the highlight
			// already was.
			name: "ctrl+p at the top clamps rather than wrapping",
			opts: workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) {
				openWorkspaceFinder(t, a)
				for range 2 {
					_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				}
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "gamma") {
					t.Fatalf("precondition: highlighted row = %q, want %q after two downs", got, "gamma")
				}
				for range 2 {
					_ = dispatchModeKey(a, keyMod('p', tea.ModCtrl))
				}
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "alpha") {
					t.Fatalf("precondition: highlighted row = %q, want %q: ctrl+p did not walk back to the top", got, "alpha")
				}
			},
			key:      keyMod('p', tea.ModCtrl),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "alpha") {
					t.Errorf("highlighted row = %q, want %q: a further ctrl+p must clamp, not wrap", got, "alpha")
				}
			},
		},
		{
			name:     "a printable key filters the list",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyPress('b'),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(&a.workspaceFinder); got != 1 {
					t.Errorf("rows = %d, want 1 after filtering on \"b\"", got)
				}
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "beta") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "beta")
				}
			},
		},
		{
			name: "backspace removes the last query rune and restores the list",
			opts: workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) {
				openWorkspaceFinder(t, a)
				_ = dispatchModeKey(a, keyPress('b'))
				if got := modalRows(&a.workspaceFinder); got != 1 {
					t.Fatalf("precondition: rows = %d, want 1", got)
				}
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(&a.workspaceFinder); got != 3 {
					t.Errorf("rows = %d, want 3 after backspacing the query away", got)
				}
			},
		},
		{
			name:     "backspace on an empty query is inert",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(&a.workspaceFinder); got != 3 {
					t.Errorf("rows = %d, want 3", got)
				}
			},
		},
		{
			// Multi-byte keystrokes miss the `len(keyStr) == 1`
			// printable test, so they neither navigate nor type.
			name:     "an unhandled modified key changes nothing",
			opts:     workspaceFinderOpts(),
			setup:    openWorkspaceFinder,
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModeWorkspaceFinder,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := modalRows(&a.workspaceFinder); got != 3 {
					t.Errorf("rows = %d, want 3", got)
				}
				if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, "alpha") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "alpha")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "key with the finder closed falls through to Normal",
			opts: workspaceFinderOpts(),
			setup: func(t *testing.T, a *App) {
				if a.workspaceFinder.IsVisible() {
					t.Fatal("precondition: finder should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
		},
	})
}

// moveFinderDown presses "down" once and asserts the highlight landed
// on want, so a case whose precondition silently failed to move cannot
// pass by accident.
func moveFinderDown(t *testing.T, a *App, want string) {
	t.Helper()
	_ = dispatchModeKey(a, keyCode(tea.KeyDown))
	if got := modalHighlightedRow(t, a.workspaceFinder.View(120)); !strings.Contains(got, want) {
		t.Fatalf("precondition: highlighted row = %q, want it to contain %q", got, want)
	}
}
