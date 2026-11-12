package statusbar

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/styles"
)

// With the transparent background on, no segment of the bar paints
// surface_dark: the toast, presence, DND, custom status, syncing mark and
// the command candidates all sit on the terminal's background.
func TestTransparentStatusBarPaintsNoSurfaceDark(t *testing.T) {
	styles.RegisterCustomTheme("Transparent Status Test", styles.ThemeColors{
		Primary: "#4A9EFF", Accent: "#50C878", Warning: "#E0A030", Error: "#E04040",
		Background: "#101820", SidebarBackground: "#111921", RailBackground: "#121A22",
		SurfaceDark: "#131B23", Surface: "#202830",
		Text: "#E0E0E0", TextMuted: "#888888", Border: "#333333",
	})
	const surfaceDark = "48;2;19;27;35"
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", core.Theme{})
	})

	bars := func() map[string]string {
		dnd := New()
		dnd.SetMode(testMode("NORMAL"))
		dnd.SetChannel("general")
		dnd.SetWorkspace("Acme")
		dnd.SetSyncing(true)
		dnd.SetToast("Copied")
		dnd.SetStatus("active", true, time.Now().Add(time.Hour))
		dnd.SetCustomStatus("in a meeting", "📅")
		dnd.SetConnectionState(StateConnected)

		active := New()
		active.SetMode(testMode("NORMAL"))
		active.SetStatus("active", false, time.Time{})
		active.SetConnectionState(StateConnecting)

		away := New()
		away.SetMode(testMode("NORMAL"))
		away.SetStatus("away", false, time.Time{})
		away.SetConnectionState(StateDisconnected)

		cmd := New()
		cmd.SetMode(testMode("COMMAND"))
		cmd.SetCommandLine(":th")
		cmd.SetCommandCandidates([]string{"theme", "thread", "threads"}, 0)

		return map[string]string{
			"dnd": dnd.View(120), "active": active.View(120),
			"away": away.View(120), "command": cmd.View(120),
		}
	}

	// The check can fail: opaque, every bar paints surface_dark.
	styles.SetTransparentBackground(false)
	styles.Apply("Transparent Status Test", core.Theme{})
	for name, view := range bars() {
		if !strings.Contains(view, surfaceDark) {
			t.Fatalf("opaque %s bar does not paint surface_dark; the check below would pass vacuously", name)
		}
	}

	styles.SetTransparentBackground(true)
	styles.Apply("Transparent Status Test", core.Theme{})
	for name, view := range bars() {
		if strings.Contains(view, surfaceDark) {
			t.Errorf("transparent %s bar paints surface_dark: %q", name, view)
		}
	}
}
