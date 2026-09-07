package ui

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
)

// colorEqual compares two color.Color values by their RGBA components.
//
// styles.Primary is a color.Color interface value. Comparing two of
// them with == happens to work today, because lipgloss.Color returns a
// comparable color.RGBA for "#RRGGBB" input and every theme in the
// table is hex — but == on an interface holding a non-comparable
// dynamic type panics at runtime instead of failing cleanly, and this
// comparison is the load-bearing mechanism of the only global-state
// canary in the tree (see the three-layer containment note on
// TestThemeSwitcherModeKeys). RGBA() has neither failure mode.
//
// Same shape as styles.colorEqual (styles/styles_test.go:13), which is
// unexported in package styles and so unreachable from package ui.
func colorEqual(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// themeSave records one invocation of the App's theme saver.
type themeSave struct {
	name  string
	scope themeswitcher.ThemeScope
}

// themeSwitcherItems is the picker's fixture. Order matters: the
// picker filters into item order, so row 0 is Dracula and row 1 is
// Light. All three have distinct Primary colors in the built-in table,
// which is what makes "the theme actually changed" observable.
func themeSwitcherItems() []string { return []string{"Dracula", "Light", "Dark"} }

const (
	draculaPrimary = "#BD93F9"
	lightPrimary   = "#0366D6"
)

// TestThemeSwitcherModeKeys characterizes handleThemeSwitcherMode
// (mode_theme_switcher.go:22).
//
// This is the one handler in PR 0d that mutates process-global state:
// styles.Apply rewrites the package-level palette every other test in
// internal/ui renders against, including the goldens. Containment is
// three-layered and each layer is load-bearing:
//
//  1. every case's setup registers a t.Cleanup on ITS OWN subtest T,
//     so the revert runs at the end of that case, not at the end of
//     the table;
//  2. every case's setup also ASSERTS the palette is back at dark on
//     entry, so a case whose cleanup failed is reported by the next
//     case rather than silently poisoning it;
//  3. the table-level check below catches a leak from the last case,
//     which has no successor to notice.
//
// "Revert" means re-applying dark, not restoring a pristine package
// state: styles.Apply is not idempotent with respect to the package's
// var initialisers. That is the same convention newGoldenApp and
// reducer_search_test.go use.
func TestThemeSwitcherModeKeys(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	// Compared with colorEqual, never ==: see the note there.
	darkPrimary := styles.Primary
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	var saves []themeSave

	// pinDark is the per-case guard described above.
	pinDark := func(t *testing.T, _ *App) {
		if !colorEqual(styles.Primary, darkPrimary) {
			t.Fatalf("a previous case leaked its theme: styles.Primary = %v, want %v",
				styles.Primary, darkPrimary)
		}
		t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	}

	// open is the shared precondition: dark pinned, a recording theme
	// saver, three themes, and the picker open at the given scope.
	open := func(scope themeswitcher.ThemeScope, withSaver bool) func(*testing.T, *App) {
		return func(t *testing.T, a *App) {
			pinDark(t, a)
			saves = nil
			if withSaver {
				a.SetThemeSaver(func(name string, sc themeswitcher.ThemeScope) {
					saves = append(saves, themeSave{name: name, scope: sc})
				})
			}
			a.SetThemeItems(themeSwitcherItems())
			a.themeSwitcher.OpenWithScope(scope, "")
			if !a.themeSwitcher.IsVisible() {
				t.Fatal("precondition: theme switcher did not open")
			}
			if got := modalRows(&a.themeSwitcher); got != len(themeSwitcherItems()) {
				t.Fatalf("precondition: %d rows, want %d", got, len(themeSwitcherItems()))
			}
		}
	}
	openGlobal := open(themeswitcher.ScopeGlobal, true)

	// commitsTo presses enter and reports which theme the cursor was
	// on, which is how this table observes cursor position.
	commitsTo := func(t *testing.T, a *App, want string) {
		t.Helper()
		saves = nil
		_ = dispatchModeKey(a, keyCode(tea.KeyEnter))
		if len(saves) != 1 {
			t.Fatalf("enter produced %d saves, want 1; the cursor probe proves nothing", len(saves))
		}
		if saves[0].name != want {
			t.Errorf("cursor committed %q, want %q", saves[0].name, want)
		}
	}

	runKeyCases(t, ModeThemeSwitcher, []keyCase{
		{
			name:     "esc closes the picker and applies nothing",
			setup:    openGlobal,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.themeSwitcher.IsVisible() {
					t.Error("picker still visible after esc")
				}
				if len(saves) != 0 {
					t.Errorf("themeSaveFn called %d times, want 0", len(saves))
				}
				if !colorEqual(styles.Primary, darkPrimary) {
					t.Errorf("styles.Primary = %v, want the palette untouched", styles.Primary)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "enter applies the highlighted theme, invalidates caches and saves",
			setup:    openGlobal,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.themeSwitcher.IsVisible() {
					t.Error("picker still visible after enter")
				}
				if want := lipgloss.Color(draculaPrimary); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want %v (Dracula)", styles.Primary, want)
				}
				if len(saves) != 1 {
					t.Fatalf("themeSaveFn called %d times, want 1", len(saves))
				}
				if saves[0].name != "Dracula" {
					t.Errorf("saved theme = %q, want %q", saves[0].name, "Dracula")
				}
				if saves[0].scope != themeswitcher.ScopeGlobal {
					t.Errorf("saved scope = %v, want ScopeGlobal", saves[0].scope)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The three InvalidateCache calls are the part of this
			// handler a refactor is most likely to drop, and dropping
			// them shows up as stale colors, not as a crash.
			name: "enter bumps the sidebar and thread render versions",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				if v := themeVersionProbe(a); v.sidebar != 0 || v.thread != 0 {
					t.Fatalf("precondition: version counters = %+v, want both 0 on a fresh App", v)
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				// Both counters start at 0 on a freshly built App and
				// InvalidateCache increments them, so any non-zero
				// value means the call happened.
				v := themeVersionProbe(a)
				if v.sidebar == 0 {
					t.Error("sidebar.Version() = 0; InvalidateCache was not called")
				}
				if v.thread == 0 {
					t.Error("threadPanel.Version() = 0; InvalidateCache was not called")
				}
			},
		},
		{
			name:     "the picker's scope is forwarded to the saver",
			setup:    open(themeswitcher.ScopeWorkspace, true),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if len(saves) != 1 {
					t.Fatalf("themeSaveFn called %d times, want 1", len(saves))
				}
				if saves[0].scope != themeswitcher.ScopeWorkspace {
					t.Errorf("saved scope = %v, want ScopeWorkspace", saves[0].scope)
				}
			},
		},
		{
			// styles.Apply(result.Name, a.themeOverrides): the config
			// overrides win over the theme's own palette.
			name: "config theme overrides win over the chosen theme",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				a.SetThemeOverrides(config.Theme{Primary: "#FF00FF"})
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if want := lipgloss.Color("#FF00FF"); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want the override %v", styles.Primary, want)
				}
			},
		},
		{
			name:     "enter with no saver wired still applies the theme",
			setup:    open(themeswitcher.ScopeGlobal, false),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.themeSaveFn != nil {
					t.Fatal("precondition: themeSaveFn should be nil")
				}
				if want := lipgloss.Color(draculaPrimary); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want %v (Dracula)", styles.Primary, want)
				}
			},
		},
		{
			name: "enter with no matching theme is a no-op",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				for _, r := range "zzz" {
					_ = dispatchModeKey(a, keyPress(r))
				}
				if got := modalRows(&a.themeSwitcher); got != 1 {
					t.Fatalf("precondition: %d rows for a non-matching query, want the 1-row floor", got)
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.themeSwitcher.IsVisible() {
					t.Error("picker closed on a no-match enter")
				}
				if !colorEqual(styles.Primary, darkPrimary) {
					t.Errorf("styles.Primary = %v, want the palette untouched", styles.Primary)
				}
				if len(saves) != 0 {
					t.Errorf("themeSaveFn called %d times, want 0", len(saves))
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "down moves the cursor to the next theme",
			setup:    openGlobal,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Light")
				if want := lipgloss.Color(lightPrimary); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want %v (Light)", styles.Primary, want)
				}
			},
		},
		{
			name:     "j moves the cursor like down",
			setup:    openGlobal,
			key:      keyPress('j'),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Light")
			},
		},
		{
			// Pins the normalisation switch at the top of the handler.
			// Key.String() prefixes active modifiers before the
			// special-key name (ultraviolet key.go:413-431, 459), so
			// shift+down arrives as "shift+down" and
			// themeswitcher.HandleKey ignores it; `case tea.KeyDown`
			// rewrites it to "down". No unmodified row can distinguish
			// the arm from a no-op.
			name:     "shift+down navigates: the Code switch strips the modifier",
			setup:    openGlobal,
			key:      keyMod(tea.KeyDown, tea.ModShift),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Light")
			},
		},
		{
			name:     "ctrl+n moves the cursor like down",
			setup:    openGlobal,
			key:      keyMod('n', tea.ModCtrl),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Light")
			},
		},
		{
			name: "up moves the cursor back",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Dracula")
			},
		},
		{
			// The setup walks DOWN to "Dark" (row 2) and back UP with
			// the same 'k' under test, so this row separates "clamped
			// at the top" from "ignored entirely": a 'k' the model
			// ignores leaves the cursor on "Dark" and commitsTo says
			// so. Asserting only that a single 'k' still commits
			// "Dracula" would pass against a handler that did nothing.
			//
			// The intermediate positions cannot be asserted here:
			// themeswitcher exposes neither View nor Selected, and the
			// only cursor probe (commit an enter) closes the picker.
			name: "k at the top clamps rather than wrapping",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				for range 2 {
					_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				}
				for range 2 {
					_ = dispatchModeKey(a, keyPress('k'))
				}
			},
			key:      keyPress('k'),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Dracula")
			},
		},
		{
			// Same construction as the 'k' row above, same reason.
			name: "ctrl+p at the top clamps rather than wrapping",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				for range 2 {
					_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				}
				for range 2 {
					_ = dispatchModeKey(a, keyMod('p', tea.ModCtrl))
				}
			},
			key:      keyMod('p', tea.ModCtrl),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				commitsTo(t, a, "Dracula")
			},
		},
		{
			// "g" is a substring of "Light" only: "Dracula" and "Dark"
			// have no g. ("l" would keep Dracula as a substring match.)
			name:     "a printable key filters the list",
			setup:    openGlobal,
			key:      keyPress('g'),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(&a.themeSwitcher); got != 1 {
					t.Errorf("rows = %d, want 1 after filtering on \"g\"", got)
				}
				commitsTo(t, a, "Light")
			},
		},
		{
			name: "backspace removes the last query rune and restores the list",
			setup: func(t *testing.T, a *App) {
				openGlobal(t, a)
				_ = dispatchModeKey(a, keyPress('g'))
				if got := modalRows(&a.themeSwitcher); got != 1 {
					t.Fatalf("precondition: rows = %d, want 1", got)
				}
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(&a.themeSwitcher); got != len(themeSwitcherItems()) {
					t.Errorf("rows = %d, want %d", got, len(themeSwitcherItems()))
				}
			},
		},
		{
			name:     "backspace on an empty query is inert",
			setup:    openGlobal,
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(&a.themeSwitcher); got != len(themeSwitcherItems()) {
					t.Errorf("rows = %d, want %d", got, len(themeSwitcherItems()))
				}
			},
		},
		{
			name:     "an unhandled modified key changes nothing",
			setup:    openGlobal,
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModeThemeSwitcher,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := modalRows(&a.themeSwitcher); got != len(themeSwitcherItems()) {
					t.Errorf("rows = %d, want %d: ctrl+x should not have filtered", got, len(themeSwitcherItems()))
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				commitsTo(t, a, "Dracula")
			},
		},
		{
			name: "key with the picker closed falls through to Normal",
			setup: func(t *testing.T, a *App) {
				pinDark(t, a)
				if a.themeSwitcher.IsVisible() {
					t.Fatal("precondition: picker should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !colorEqual(styles.Primary, darkPrimary) {
					t.Errorf("styles.Primary = %v, want the palette untouched", styles.Primary)
				}
			},
		},
	})

	// Layer 3: the last case has no successor to notice its leak.
	if !colorEqual(styles.Primary, darkPrimary) {
		t.Errorf("theme leaked out of the table: styles.Primary = %v, want %v",
			styles.Primary, darkPrimary)
	}
}

// themeVersions bundles the two render-cache counters the theme
// handler is supposed to bump.
type themeVersions struct {
	sidebar int64
	thread  int64
}

func themeVersionProbe(a *App) themeVersions {
	return themeVersions{sidebar: a.sidebar.Version(), thread: a.threadPanel.Version()}
}
