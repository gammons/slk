package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// makeTransparent turns the transparent background on for a built App,
// through the same applyTheme a theme switch uses, and turns it off
// again after the test.
func makeTransparent(t *testing.T, a *App, theme string) {
	t.Helper()
	styles.SetTransparentBackground(true)
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", config.Theme{})
	})
	a.applyTheme(theme)
}

// The scenarios and dialogs pinned again with the transparent background
// on, as transparent_<name>.
var (
	transparentGoldenScenarios = []string{"base", "thread_open", "thread_parent_selected", "draft_preview", "drag_selection", "overlay_finder"}
	transparentGoldenDialogs   = []string{"dialog_confirm", "dialog_channel_finder_scroll", "dialog_search_results", "dialog_new_message", "dialog_reactions_view", "popup_mention", "popup_emoji", "popup_channel"}
)

func TestGolden_Transparent(t *testing.T) {
	scenarios := map[string]goldenScenario{}
	for _, sc := range goldenScenarios() {
		scenarios[sc.name] = sc
	}
	for _, name := range transparentGoldenScenarios {
		t.Run(name, func(t *testing.T) {
			sc, ok := scenarios[name]
			if !ok {
				t.Fatalf("no golden scenario %q", name)
			}
			a := sc.build(t)
			makeTransparent(t, a, "dark")
			compareGolden(t, "transparent_"+name, a.View().Content)
		})
	}

	dialogs := map[string]dialogGolden{}
	for _, d := range dialogGoldens() {
		dialogs[d.name] = d
	}
	for _, name := range transparentGoldenDialogs {
		t.Run(name, func(t *testing.T) {
			d, ok := dialogs[name]
			if !ok {
				t.Fatalf("no dialog golden %q", name)
			}
			opts := goldenFixtureOpts()
			if d.w != 0 {
				opts = append(opts, withSize(d.w, d.h))
			}
			a := newGoldenApp(t, opts...)
			makeTransparent(t, a, "dark")
			d.open(t, a)
			frame := a.View().Content
			if !d.shown(a) {
				t.Fatalf("%s: the dialog is not open", name)
			}
			compareGolden(t, "transparent_"+name, frame)
		})
	}
}

// transparentTestColors is a theme whose base colours are each unique, so
// a frame can be searched for any one of them.
var transparentTestColors = styles.ThemeColors{
	Primary: "#4A9EFF", Accent: "#50C878", Warning: "#E0A030", Error: "#E04040",
	Background:        "#101820",
	SidebarBackground: "#111921",
	RailBackground:    "#121A22",
	SurfaceDark:       "#131B23",
	Surface:           "#202830",
	Text:              "#E0E0E0", TextMuted: "#888888", Border: "#333333",
}

// cellBg is the background a cell is drawn on, from its canonical SGR
// state (screenCell.attrs): "2;R;G;B" or "5;N" for 48, the code for a
// basic 16-colour background, or "" for the terminal's default.
func cellBg(attrs string) string {
	for _, f := range strings.Fields(attrs) {
		k, v, _ := strings.Cut(f, "=")
		switch {
		case k == "48":
			return v
		case len(k) == 2 && k >= "40" && k <= "47", len(k) == 3 && k >= "100" && k <= "107":
			return k
		}
	}
	return ""
}

// bgParams returns, per row of a frame, the truecolor backgrounds
// ("R;G;B") its cells are drawn on, through the same cell walk as the
// golden cell comparison (screenCells).
func bgParams(frame string) [][]string {
	var out [][]string
	for _, row := range screenCells(frame) {
		var bgs []string
		for _, c := range row {
			if rgb, ok := strings.CutPrefix(cellBg(c.attrs), "2;"); ok {
				bgs = append(bgs, rgb)
			}
		}
		out = append(out, bgs)
	}
	return out
}

// TestTransparentFrameHasNoBaseFill renders whole frames with the
// transparent background on and looks for the base colours: none of the
// pane, sidebar and rail backgrounds may be painted anywhere, and the
// status bar (the last row) may not be painted with surface_dark. The
// composer keeps surface_dark, so that one is only checked on the status
// row.
func TestTransparentFrameHasNoBaseFill(t *testing.T) {
	styles.RegisterCustomTheme("Transparent Frame Test", transparentTestColors)
	banned := map[string]string{
		"16;24;32": "background",
		"17;25;33": "sidebar_background",
		"18;26;34": "rail_background",
		// What a missed NoColor would paint through bgANSIFor's RGBA path.
		"0;0;0": "black",
	}
	const surfaceDark = "19;27;35"

	for _, sc := range goldenScenarios() {
		if sc.name == "overlay_finder" {
			continue // a dialog keeps the theme background on purpose
		}
		t.Run(sc.name, func(t *testing.T) {
			a := sc.build(t)
			makeTransparent(t, a, "Transparent Frame Test")
			lines := bgParams(a.View().Content)
			for i, bgs := range lines {
				for _, bg := range bgs {
					if name, ok := banned[bg]; ok {
						t.Fatalf("row %d paints %s (%s)", i, name, bg)
					}
				}
			}
			for _, bg := range lines[len(lines)-1] {
				if bg == surfaceDark {
					t.Fatalf("the status bar paints surface_dark (%s)", bg)
				}
			}
		})
	}
}

// And the other way round: with the option off the same theme does paint
// those colours, so the test above can fail.
func TestTransparentFrameCheckSeesOpaqueFill(t *testing.T) {
	styles.RegisterCustomTheme("Transparent Frame Test", transparentTestColors)
	a := newGoldenApp(t, goldenFixtureOpts()...)
	a.applyTheme("Transparent Frame Test")
	lines := bgParams(a.View().Content)
	seen := map[string]bool{}
	for _, bgs := range lines {
		for _, bg := range bgs {
			seen[bg] = true
		}
	}
	for _, want := range []string{"16;24;32", "17;25;33", "18;26;34"} {
		if !seen[want] {
			t.Errorf("opaque frame never paints %s; the transparent check would pass vacuously", want)
		}
	}
	statusSeen := false
	for _, bg := range lines[len(lines)-1] {
		statusSeen = statusSeen || bg == "19;27;35"
	}
	if !statusSeen {
		t.Error("opaque status bar does not paint surface_dark")
	}
}

// The selected thread parent is one tinted block: every cell of its rows,
// from the leftmost tinted column to the rightmost, is on the tint. No
// cell shows the terminal's background or any other colour.
func TestTransparentSelectedThreadParentHasNoHoles(t *testing.T) {
	sc := goldenScenarioNamed(t, "thread_parent_selected")
	a := sc.build(t)
	makeTransparent(t, a, "dark")
	tint := lineCells(messages.SelectionTintBgANSI(a.threadPanel.Focused())+" ", map[string]string{})
	want := cellBg(tint[0].attrs)
	rows := screenCells(a.View().Content)
	var tinted []int
	left, right := -1, -1
	for i, row := range rows {
		found := false
		for j, c := range row {
			if cellBg(c.attrs) != want {
				continue
			}
			found = true
			if left < 0 || j < left {
				left = j
			}
			right = max(right, j)
		}
		if found {
			tinted = append(tinted, i)
		}
	}
	if len(tinted) == 0 {
		t.Fatal("no tinted row: the parent is not selected")
	}
	for _, i := range tinted {
		for j := left; j <= right && j < len(rows[i]); j++ {
			if got := cellBg(rows[i][j].attrs); got != want {
				t.Errorf("row %d col %d: %q drawn on %q inside the tint %q", i, j, rows[i][j].glyph, got, want)
				break
			}
		}
	}
}

// defaultBgCells counts the cells of s drawn on the default background.
// s starts with a background set, as a slice from inside a solid span.
func defaultBgCells(s string) int {
	n := 0
	for _, c := range lineCells(s, map[string]string{"48": "set"}) {
		if cellBg(c.attrs) == "" {
			n++
		}
	}
	return n
}

// A composer popup is solid: between its first and last surface_dark
// cell on a row, nothing is drawn on the default background.
func TestTransparentPopupsHaveNoHoles(t *testing.T) {
	dialogs := map[string]dialogGolden{}
	for _, d := range dialogGoldens() {
		dialogs[d.name] = d
	}
	for _, name := range []string{"popup_mention", "popup_emoji", "popup_channel"} {
		t.Run(name, func(t *testing.T) {
			d := dialogs[name]
			a := newGoldenApp(t, goldenFixtureOpts()...)
			makeTransparent(t, a, "dark")
			d.open(t, a)
			surface := "\x1b[48;2;15;15;35m" // dark's surface_dark
			rows := 0
			for i, line := range strings.Split(a.View().Content, "\n") {
				first, last := strings.Index(line, surface), strings.LastIndex(line, surface)
				if first < 0 {
					continue
				}
				rows++
				if n := defaultBgCells(line[first:last]); n > 0 {
					t.Errorf("row %d: %d popup cells show the terminal background", i, n)
				}
			}
			if rows == 0 {
				t.Fatal("no popup row found")
			}
		})
	}
}

// The startup splash fills the screen around its box with
// SplashBackground, so with the transparent background on it paints no
// background at all, and with it off it paints SurfaceDark.
func TestTransparentSplashHasNoFill(t *testing.T) {
	pinRenderGlobals(t)
	b := newWorkspaceBootstrap()
	b.SetWorkspaces([]string{"Acme"})

	off := b.Render(40, 10, "*")
	if !strings.Contains(off, "48;") {
		t.Fatal("with the option off the splash should paint a background")
	}

	a := newGoldenApp(t)
	makeTransparent(t, a, "dark")
	on := b.Render(40, 10, "*")
	if strings.Contains(on, "48;") {
		t.Errorf("transparent splash still paints a background:\n%q", on)
	}
	if !strings.Contains(on, "Connecting to Acme") {
		t.Error("splash lost its text")
	}
}
