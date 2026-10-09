package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// TestConfirmPromptFollowsThemeChange: the prompt holds a snapshot of its
// styles, so every theme change has to push a fresh one. The workspace
// reducers apply a per-workspace theme with no mode gate, so a quit prompt
// opened while a workspace connects (ctrl+c during startup) is still open when
// the theme changes. It must then render exactly as a prompt opened under the
// new theme does, not keep the old theme's colours.
//
// The theme change goes through the real Update chain rather than calling
// styles.Apply directly: the point is that the App's theme path pushes the
// styles, and a bare styles.Apply bypasses the App.
func TestConfirmPromptFollowsThemeChange(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
	}{
		{"WorkspaceReadyMsg", WorkspaceReadyMsg{TeamID: "T1", TeamName: "One", InitialActive: true, Theme: "nord"}},
		{"WorkspaceSwitchedMsg", WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Two", Theme: "nord"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

			styles.Apply("nord", config.Theme{})
			fresh := newTestApp(t, withSize(120, 40))
			fresh.openQuitConfirm()
			want := fresh.confirmPrompt.View()

			styles.Apply("dracula", config.Theme{})
			a := newTestApp(t, withSize(120, 40))
			a.openQuitConfirm()
			before := a.confirmPrompt.View()
			if before == want {
				t.Fatal("precondition: dracula and nord render the prompt identically")
			}

			a.Update(c.msg)
			if !a.confirmPrompt.IsVisible() {
				t.Fatal("precondition: the theme change closed the prompt")
			}
			if got := a.confirmPrompt.View(); got != want {
				if got == before {
					t.Error("prompt kept the previous theme after the theme change")
				} else {
					t.Errorf("prompt does not match one opened under the new theme:\ngot  %q\nwant %q", got, want)
				}
			}
		})
	}
}
