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

// TestThemeCycleSave characterizes the delayed save a cycle press
// schedules (theme.go): the save tick; the workspace each save goes
// to, including while a workspace switch is in flight; and every place
// that saves a pending theme early (a press for another workspace,
// switchWorkspace, the theme switcher, cmd/slk at exit). The ticks are
// delivered by hand instead of waited for.
func TestThemeCycleSave(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	// press dispatches one normal-mode key.
	press := func(a *App, k tea.KeyMsg) tea.Cmd {
		a.SetMode(ModeNormal)
		return dispatchModeKey(a, k)
	}
	// dueTick is the save tick of the latest press.
	dueTick := func(a *App) themeSaveDueMsg { return themeSaveDueMsg{gen: a.themeSaveGen} }
	// switchedTo is the result a real switch to T2 delivers.
	switchedTo := func(theme string) WorkspaceSwitchedMsg {
		return WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "beta", Theme: theme}
	}
	onT1 := func(opts ...testOpt) []testOpt { return append(opts, withActiveTeam("T1")) }

	t.Run("the save tick saves the pending theme for the workspace", func(t *testing.T) {
		a := newTestApp(t, onT1()...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey)
		a.Update(dueTick(a))
		if want := []themeSave{workspaceSave("T1", "Dracula")}; !slices.Equal(rec.saves, want) {
			t.Errorf("saves = %+v, want %+v", rec.saves, want)
		}
		if a.pendingTheme != (pendingThemeSave{}) {
			t.Errorf("pending save = %+v after the tick, want none", a.pendingTheme)
		}
	})

	t.Run("holding alt+y saves once, for the theme it stops on", func(t *testing.T) {
		a := newTestApp(t, onT1()...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey) // Dracula
		first := dueTick(a)
		press(a, themeNextKey) // Light
		a.Update(first)
		if len(rec.saves) != 0 {
			t.Fatalf("saves = %+v after a stale tick, want none", rec.saves)
		}
		last := dueTick(a)
		a.Update(last)
		a.Update(last)
		if want := []themeSave{workspaceSave("T1", "Light")}; !slices.Equal(rec.saves, want) {
			t.Errorf("saves = %+v, want %+v", rec.saves, want)
		}
	})

	// switchesAfterSave runs a switch command and asserts that the
	// pending save landed while the command was built, before the
	// switch itself ran.
	switchesAfterSave := func(t *testing.T, rec *themeCycleRecorder, cmd tea.Cmd) {
		t.Helper()
		if want := []string{"save Dracula for T1"}; !slices.Equal(rec.events, want) {
			t.Fatalf("events before the switch ran = %v, want %v", rec.events, want)
		}
		if cmd == nil {
			t.Fatal("cmd = nil, want the workspace-switch cmd")
		}
		if msg, ok := cmd().(switchedTeamMsg); !ok || msg.teamID != "T2" {
			t.Fatalf("cmd() = %#v, want a switch to T2", msg)
		}
		if want := []string{"save Dracula for T1", "switch T2"}; !slices.Equal(rec.events, want) {
			t.Errorf("events = %v, want %v", rec.events, want)
		}
	}

	t.Run("a number-key workspace switch saves the pending theme first", func(t *testing.T) {
		a := newTestApp(t, onT1(workspaceOpts()...)...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey)
		switchesAfterSave(t, rec, press(a, keyPress('2')))
	})

	t.Run("a workspace finder switch saves the pending theme first", func(t *testing.T) {
		a := newTestApp(t, onT1(workspaceFinderOpts()...)...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey)
		a.workspaceFinder.Open()
		a.SetMode(ModeWorkspaceFinder)
		moveFinderDown(t, a, "beta")
		switchesAfterSave(t, rec, dispatchModeKey(a, keyCode(tea.KeyEnter)))
	})

	t.Run("a workspace rail click saves the pending theme first", func(t *testing.T) {
		// Same geometry as TestApp_ClickOnWorkspaceRailSwitches: the
		// rail has no top border, and its tiles sit on rows 1, 3, ...
		a := NewApp()
		a.width, a.height = 120, 30
		a.SetWorkspaces([]workspace.WorkspaceItem{
			{ID: "T1", Name: "alpha", Initials: "AL"},
			{ID: "T2", Name: "beta", Initials: "BE"},
		})
		a.activeTeamID = "T1"
		_ = a.View()
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey)
		_, cmd := a.Update(tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseLeft})
		switchesAfterSave(t, rec, cmd)
	})

	// The backend moves its active workspace when the switch command
	// runs, and the UI shows T2 only when the result arrives. A press
	// between the two still shows T1, so its theme belongs to T1 in
	// every ordering of the command, the result and the save tick.
	t.Run("a cycle during a switch saves for the workspace on screen when the tick comes first", func(t *testing.T) {
		a := newTestApp(t, onT1(workspaceOpts()...)...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		switchCmd := press(a, keyPress('2'))
		press(a, themeNextKey)
		_ = switchCmd()
		a.Update(dueTick(a))
		a.Update(switchedTo("Light"))
		if want := []string{"switch T2", "save Dracula for T1"}; !slices.Equal(rec.events, want) {
			t.Errorf("events = %v, want %v", rec.events, want)
		}
		if got := styles.CurrentTheme(); got != "Light" {
			t.Errorf("current theme = %q, want T2's Light", got)
		}
	})

	t.Run("a cycle during a switch saves for the workspace on screen when the result comes first", func(t *testing.T) {
		a := newTestApp(t, onT1(workspaceOpts()...)...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		switchCmd := press(a, keyPress('2'))
		press(a, themeNextKey)
		_ = switchCmd()
		a.Update(switchedTo("Light"))
		if got := styles.CurrentTheme(); got != "Light" {
			t.Errorf("current theme = %q, want T2's Light", got)
		}
		a.Update(dueTick(a))
		if want := []string{"switch T2", "save Dracula for T1"}; !slices.Equal(rec.events, want) {
			t.Errorf("events = %v, want %v: the result must not discard T1's choice", rec.events, want)
		}
	})

	t.Run("a press in the new workspace first saves the choice left for the old one", func(t *testing.T) {
		a := newTestApp(t, onT1(workspaceOpts()...)...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		switchCmd := press(a, keyPress('2'))
		press(a, themeNextKey) // Dracula, for T1
		stale := dueTick(a)
		_ = switchCmd()
		a.Update(switchedTo("Light"))
		press(a, themeNextKey) // Dark, for T2
		if want := []themeSave{workspaceSave("T1", "Dracula")}; !slices.Equal(rec.saves, want) {
			t.Fatalf("saves after the press in T2 = %+v, want %+v", rec.saves, want)
		}
		a.Update(stale)
		a.Update(dueTick(a))
		want := []themeSave{workspaceSave("T1", "Dracula"), workspaceSave("T2", "Dark")}
		if !slices.Equal(rec.saves, want) {
			t.Errorf("saves = %+v, want %+v", rec.saves, want)
		}
	})

	t.Run("the theme switcher saves after a pending cycle save", func(t *testing.T) {
		a := newTestApp(t, onT1()...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey) // Dracula, pending
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeWorkspace, "")
		a.SetMode(ModeThemeSwitcher)
		_ = dispatchModeKey(a, keyCode(tea.KeyDown))  // row 1: Light
		_ = dispatchModeKey(a, keyCode(tea.KeyEnter)) // pick it
		a.Update(dueTick(a))
		want := []themeSave{workspaceSave("T1", "Dracula"), workspaceSave("T1", "Light")}
		if !slices.Equal(rec.saves, want) {
			t.Errorf("saves = %+v, want %+v: the picker's save must land last", rec.saves, want)
		}
	})

	t.Run("SavePendingTheme saves a pending theme once", func(t *testing.T) {
		a := newTestApp(t, onT1()...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		a.SavePendingTheme()
		if len(rec.saves) != 0 {
			t.Fatalf("saves = %+v with nothing pending, want none", rec.saves)
		}
		press(a, themeNextKey)
		// cmd/slk calls this after the program exits, which can be
		// before the tick is due.
		a.SavePendingTheme()
		a.SavePendingTheme()
		if want := []themeSave{workspaceSave("T1", "Dracula")}; !slices.Equal(rec.saves, want) {
			t.Errorf("saves = %+v, want %+v", rec.saves, want)
		}
	})

	t.Run("the initial workspace's own theme replaces a cycle made before it", func(t *testing.T) {
		a := newTestApp(t)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		press(a, themeNextKey)
		a.Update(WorkspaceReadyMsg{
			TeamID:        "T1",
			TeamName:      "alpha",
			Channels:      []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
			InitialActive: true,
			Theme:         "Light",
		})
		if got := styles.CurrentTheme(); got != "Light" {
			t.Errorf("current theme = %q, want the workspace's Light", got)
		}
		a.Update(dueTick(a))
		a.SavePendingTheme()
		if len(rec.saves) != 0 {
			t.Errorf("saves = %+v, want none: no workspace showed at the press", rec.saves)
		}
	})

	// runKeyCases calls dispatchModeKey directly and so bypasses the
	// reducer chain; this row sends the keys and the tick through
	// App.Update, the path a real key press takes.
	t.Run("the keys and the save tick work through App.Update", func(t *testing.T) {
		a := newTestApp(t, onT1()...)
		rec := startThemeCycle(t, a, "Dark", themeSwitcherItems())
		a.Update(themeNextKey)
		if got := styles.CurrentTheme(); got != "Dracula" {
			t.Fatalf("current theme after alt+y = %q, want Dracula", got)
		}
		a.Update(keyMod('Y', tea.ModAlt))
		if got := styles.CurrentTheme(); got != "Dark" {
			t.Fatalf("current theme after alt+Y = %q, want Dark", got)
		}
		a.Update(dueTick(a))
		if want := []themeSave{workspaceSave("T1", "Dark")}; !slices.Equal(rec.saves, want) {
			t.Errorf("saves = %+v, want %+v", rec.saves, want)
		}
	})

	if got := styles.CurrentTheme(); got != "Dark" {
		t.Errorf("theme leaked out of the test: current theme = %q, want Dark", got)
	}
}

// TestThemeCycleToast characterizes the "Theme: …" toast a press shows
// when something else then applies a theme: the toast names a theme no
// longer on screen, so applyTheme clears it. A toast shown after it is
// not the theme's, and stays.
func TestThemeCycleToast(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	// cycled returns an App on T1 whose last press showed "Theme:
	// Dracula".
	cycled := func(t *testing.T) *App {
		t.Helper()
		a := newTestApp(t, withActiveTeam("T1"))
		startThemeCycle(t, a, "Dark", themeSwitcherItems())
		_ = dispatchModeKey(a, themeNextKey)
		if got := statusbarText(a); !strings.Contains(got, "Theme: Dracula") {
			t.Fatalf("precondition: status bar = %q, want the theme toast", got)
		}
		return a
	}
	noThemeToast := func(t *testing.T, a *App) {
		t.Helper()
		if got := statusbarText(a); strings.Contains(got, "Theme: Dracula") {
			t.Errorf("status bar = %q, want the stale theme toast gone", got)
		}
	}

	t.Run("a workspace switch clears it", func(t *testing.T) {
		a := cycled(t)
		a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "beta", Theme: "Light"})
		noThemeToast(t, a)
	})

	t.Run("a theme switcher pick clears it", func(t *testing.T) {
		a := cycled(t)
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeWorkspace, "")
		a.SetMode(ModeThemeSwitcher)
		_ = dispatchModeKey(a, keyCode(tea.KeyDown)) // row 1: Light
		_ = dispatchModeKey(a, keyCode(tea.KeyEnter))
		noThemeToast(t, a)
	})

	t.Run("a later toast survives the next theme change", func(t *testing.T) {
		a := cycled(t)
		a.Update(statusbar.CopiedMsg{N: 5})
		a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "beta", Theme: "Light"})
		if got := statusbarText(a); !strings.Contains(got, "Copied 5 chars") {
			t.Errorf("status bar = %q, want the later toast kept", got)
		}
	})
}
