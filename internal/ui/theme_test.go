package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/workspace"
)

// themeCycleRecorder records, in order, the theme saves and workspace
// switches a theme-cycle test causes, so a test can assert which
// workspace a save went to and whether it landed before a switch.
type themeCycleRecorder struct {
	saves  []themeSave
	events []string
}

// startThemeCycle is the shared precondition of the theme-cycle tests.
// Like TestThemeSwitcherModeKeys, every case mutates the process-global
// palette, so it uses the same containment: it fails when an earlier
// case leaked a theme, and it re-applies dark when this case ends. It
// then wires a recording theme saver and workspace switcher, sets items
// as the theme list, and applies current as the theme to cycle from.
func startThemeCycle(t *testing.T, a *App, current string, items []string) *themeCycleRecorder {
	t.Helper()
	if got := styles.CurrentTheme(); got != "Dark" {
		t.Fatalf("a previous case leaked its theme: current theme = %q, want Dark", got)
	}
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	rec := &themeCycleRecorder{}
	a.setThemeSaverForTest(func(teamID, name string, sc core.ThemeScope) {
		rec.saves = append(rec.saves, themeSave{teamID: teamID, name: name, scope: sc})
		rec.events = append(rec.events, "save "+name+" for "+teamID)
	})
	a.setWorkspaceSwitcherForTest(func(teamID string) tea.Msg {
		rec.events = append(rec.events, "switch "+teamID)
		return switchedTeamMsg{teamID: teamID}
	})
	a.SetThemeItems(items)
	styles.Apply(current, config.Theme{})
	if got := styles.CurrentTheme(); got != current {
		t.Fatalf("precondition: current theme = %q, want %q", got, current)
	}
	return rec
}

var (
	themeNextKey = keyMod('y', tea.ModAlt)
	themePrevKey = keyMod('y', tea.ModAlt|tea.ModShift)
)

// workspaceSave is the save a cycle makes for teamID.
func workspaceSave(teamID, name string) themeSave {
	return themeSave{teamID: teamID, name: name, scope: themeswitcher.ScopeWorkspace}
}

// TestThemeCycleKeys characterizes the normal-mode ThemeNext /
// ThemePrev arms (handleNormalMode) and cycleTheme up to the press
// itself. The save each press schedules is TestThemeCycleSave's. The
// fixture is themeSwitcherItems() — Dracula, Light, Dark — so starting
// on Dark (the last item) exercises the forward wrap and starting on
// Dracula (the first) the backward wrap. Every row but one shows
// workspace T1, so the press has a workspace to save for.
func TestThemeCycleKeys(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	onT1 := []testOpt{withActiveTeam("T1")}
	var rec *themeCycleRecorder
	startOn := func(current string, items []string) func(*testing.T, *App) {
		return func(t *testing.T, a *App) { rec = startThemeCycle(t, a, current, items) }
	}

	// cyclesTo asserts the theme a press applied, its toast, and that
	// its save is pending for T1 rather than written.
	cyclesTo := func(t *testing.T, a *App, want string) {
		t.Helper()
		if got := styles.CurrentTheme(); got != want {
			t.Errorf("current theme = %q, want %q", got, want)
		}
		if got := statusbarText(a); !strings.Contains(got, "Theme: "+want) {
			t.Errorf("status bar = %q, want a %q toast", got, "Theme: "+want)
		}
		if p := (pendingThemeSave{name: want, teamID: "T1"}); a.pendingTheme != p {
			t.Errorf("pending save = %+v, want %+v", a.pendingTheme, p)
		}
		if len(rec.saves) != 0 {
			t.Errorf("saves = %+v, want none before the save tick", rec.saves)
		}
	}

	runKeyCases(t, ModeNormal, []keyCase{
		{
			name:     "alt+y applies the next theme and shows its name",
			opts:     onT1,
			setup:    startOn("Dracula", themeSwitcherItems()),
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				cyclesTo(t, a, "Light")
				if want := lipgloss.Color(lightPrimary); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want %v (Light)", styles.Primary, want)
				}
				if cmd == nil {
					t.Error("cmd = nil, want the toast-clear and save ticks")
				}
			},
		},
		{
			name:     "alt+y on the last theme wraps to the first",
			opts:     onT1,
			setup:    startOn("Dark", themeSwitcherItems()),
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Dracula")
				if want := lipgloss.Color(draculaPrimary); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want %v (Dracula)", styles.Primary, want)
				}
			},
		},
		{
			name:     "alt+shift+y applies the previous theme",
			opts:     onT1,
			setup:    startOn("Dark", themeSwitcherItems()),
			key:      themePrevKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Light")
			},
		},
		{
			name:     "alt+shift+y on the first theme wraps to the last",
			opts:     onT1,
			setup:    startOn("Dracula", themeSwitcherItems()),
			key:      themePrevKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Dark")
			},
		},
		{
			// tmux with extended-keys on sends Alt+Shift+Y as the
			// shifted letter with only the alt modifier (\e[27;3;89~
			// or \e[89;3u), which decodes as "alt+Y".
			name:     "alt+Y from tmux extended-keys applies the previous theme",
			opts:     onT1,
			setup:    startOn("Dark", themeSwitcherItems()),
			key:      keyMod('Y', tea.ModAlt),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Light")
			},
		},
		{
			// xterm's modifyOtherKeys form, \e[27;4;89~, decodes as
			// "alt+shift+Y".
			name:     "alt+shift+Y from xterm modifyOtherKeys applies the previous theme",
			opts:     onT1,
			setup:    startOn("Dark", themeSwitcherItems()),
			key:      keyMod('Y', tea.ModAlt|tea.ModShift),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Light")
			},
		},
		{
			// Each press starts from the theme the previous one
			// applied, so len(items) presses visit every theme once and
			// land back where they started. Each press also replaces
			// the previous press's toast.
			name:     "repeated alt+y visits every theme and returns to the start",
			opts:     onT1,
			setup:    startOn("Light", themeSwitcherItems()),
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				got := []string{styles.CurrentTheme()}
				for range len(themeSwitcherItems()) - 1 {
					_ = dispatchModeKey(a, themeNextKey)
					got = append(got, styles.CurrentTheme())
				}
				if want := []string{"Dark", "Dracula", "Light"}; !slices.Equal(got, want) {
					t.Errorf("themes visited = %v, want %v", got, want)
				}
				cyclesTo(t, a, "Light")
				if got := statusbarText(a); strings.Contains(got, "Theme: Dracula") {
					t.Errorf("status bar = %q, want only the last press's toast", got)
				}
			},
		},
		{
			name: "a cycle bumps the messagepane, sidebar and thread render versions",
			opts: onT1,
			setup: func(t *testing.T, a *App) {
				startOn("Dark", themeSwitcherItems())(t, a)
				if v := themeVersionProbe(a); v.sidebar != 0 || v.thread != 0 || v.messages != 0 {
					t.Fatalf("precondition: version counters = %+v, want all 0 on a fresh App", v)
				}
			},
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				v := themeVersionProbe(a)
				if v.sidebar == 0 || v.thread == 0 || v.messages == 0 {
					t.Errorf("version counters = %+v, want all bumped", v)
				}
			},
		},
		{
			// With text and focus, a compose keeps the textarea styles
			// RefreshStyles last set, so a missing restyle leaves the
			// old theme's SurfaceDark behind the text.
			name: "a cycle restyles the channel and thread composes",
			opts: onT1,
			setup: func(t *testing.T, a *App) {
				startOn("Dark", themeSwitcherItems())(t, a)
				for _, c := range []*compose.Model{&a.compose, &a.threadCompose} {
					c.SetValue("hello")
					_ = c.Focus()
					c.RefreshStyles()
					if v := c.View(80, true); !strings.Contains(v, darkSurfaceDarkBG) {
						t.Fatalf("precondition: compose does not render Dark's SurfaceDark: %q", v)
					}
				}
			},
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				for i, c := range []*compose.Model{&a.compose, &a.threadCompose} {
					v := c.View(80, true)
					if strings.Contains(v, darkSurfaceDarkBG) || !strings.Contains(v, draculaSurfaceDarkBG) {
						t.Errorf("compose %d still renders Dark's SurfaceDark, want Dracula's: %q", i, v)
					}
				}
			},
		},
		{
			// applyTheme passes the App's config overrides to
			// styles.Apply, so they win over the cycled theme just as
			// they win over the picker's.
			name: "config theme overrides win over the cycled theme",
			opts: onT1,
			setup: func(t *testing.T, a *App) {
				startOn("Dark", themeSwitcherItems())(t, a)
				a.SetThemeOverrides(config.Theme{Primary: "#FF00FF"})
			},
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := styles.CurrentTheme(); got != "Dracula" {
					t.Errorf("current theme = %q, want Dracula", got)
				}
				if want := lipgloss.Color("#FF00FF"); !colorEqual(styles.Primary, want) {
					t.Errorf("styles.Primary = %v, want the override %v", styles.Primary, want)
				}
			},
		},
		{
			// A theme missing from the list (here Dark) starts the
			// cycle at the first item going forward...
			name:     "alt+y from a theme outside the list starts at the first",
			opts:     onT1,
			setup:    startOn("Dark", []string{"Dracula", "Light"}),
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Dracula")
			},
		},
		{
			// ...and at the last going backward.
			name:     "alt+shift+y from a theme outside the list starts at the last",
			opts:     onT1,
			setup:    startOn("Dark", []string{"Dracula", "Light"}),
			key:      themePrevKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				cyclesTo(t, a, "Light")
			},
		},
		{
			name:     "alt+y with no themes is a no-op",
			opts:     onT1,
			setup:    startOn("Dark", nil),
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := styles.CurrentTheme(); got != "Dark" {
					t.Errorf("current theme = %q, want the palette untouched", got)
				}
				if a.pendingTheme != (pendingThemeSave{}) {
					t.Errorf("pending save = %+v, want none", a.pendingTheme)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// Before the first workspace connects there is none to save
			// for; that workspace then applies its own theme.
			name:     "alt+y with no workspace on screen applies the theme but saves nothing",
			setup:    startOn("Dark", themeSwitcherItems()),
			key:      themeNextKey,
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := styles.CurrentTheme(); got != "Dracula" {
					t.Errorf("current theme = %q, want Dracula", got)
				}
				if a.pendingTheme != (pendingThemeSave{}) {
					t.Errorf("pending save = %+v, want none", a.pendingTheme)
				}
				a.SavePendingTheme()
				if len(rec.saves) != 0 {
					t.Errorf("saves = %+v, want none", rec.saves)
				}
			},
		},
	})

	if got := styles.CurrentTheme(); got != "Dark" {
		t.Errorf("theme leaked out of the table: current theme = %q, want Dark", got)
	}
}

// The SurfaceDark backgrounds of Dark (#0F0F23) and Dracula (#21222C)
// as the SGR parameters a compose renders them with.
const (
	darkSurfaceDarkBG    = "48;2;15;15;35"
	draculaSurfaceDarkBG = "48;2;33;34;44"
)

// TestThemeCycle_FullRegistry cycles the real theme list both ways:
// every listed theme must apply as itself (styles.CurrentTheme reports
// the listed name), or the cycle would jump from it to the wrong
// neighbor. Custom themes follow the same rule;
// TestCurrentThemeRoundTripsEveryListedTheme in package styles checks
// them, because this package cannot remove a registered theme again.
func TestThemeCycle_FullRegistry(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	names := styles.ThemeNames()
	n := len(names)
	a := newTestApp(t, withActiveTeam("T1"))
	startThemeCycle(t, a, names[0], names)
	for i := 1; i <= n; i++ {
		_ = dispatchModeKey(a, themeNextKey)
		if got, want := styles.CurrentTheme(), names[i%n]; got != want {
			t.Fatalf("alt+y press %d applied %q, want %q", i, got, want)
		}
	}
	for i := 1; i <= n; i++ {
		_ = dispatchModeKey(a, themePrevKey)
		if got, want := styles.CurrentTheme(), names[(n-i)%n]; got != want {
			t.Fatalf("alt+shift+y press %d applied %q, want %q", i, got, want)
		}
	}
}

