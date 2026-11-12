package messages

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// transparentDark applies the dark theme with the transparent background
// on, and puts it back opaque after the test.
func transparentDark(t *testing.T) {
	t.Helper()
	styles.SetTransparentBackground(true)
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", config.Theme{})
	})
}

// With the option on, NoColor is the default background. With it off,
// NoColor (a theme colour that doesn't parse) keeps upstream's escape,
// so the off path doesn't change.
func TestBgANSIForNoColor(t *testing.T) {
	styles.SetTransparentBackground(false)
	styles.Apply("dark", config.Theme{})
	if got := bgANSIFor(lipgloss.NoColor{}); got != "\x1b[48;2;0;0;0m" {
		t.Errorf("option off: bgANSIFor(NoColor) = %q, want the old \\x1b[48;2;0;0;0m", got)
	}
	transparentDark(t)
	if got := bgANSIFor(lipgloss.NoColor{}); got != "\x1b[49m" {
		t.Errorf("option on: bgANSIFor(NoColor) = %q, want the default background \\x1b[49m", got)
	}
}

// The bg escape caches are keyed on the colour, but a NoColor
// background's escape also depends on the switch, so flipping it must
// not serve the escape cached before.
func TestBgANSICacheFollowsTheSwitch(t *testing.T) {
	broken := config.Theme{Background: "none"} // parses to NoColor
	styles.SetTransparentBackground(false)
	styles.Apply("dark", broken)
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", config.Theme{})
	})
	if got := BgANSI(); got != "\x1b[48;2;0;0;0m" {
		t.Fatalf("option off, NoColor background: BgANSI() = %q, want the old black", got)
	}
	styles.SetTransparentBackground(true)
	styles.Apply("dark", broken)
	if got := BgANSI(); got != "\x1b[49m" {
		t.Errorf("after turning the option on: BgANSI() = %q, want \\x1b[49m", got)
	}
}

func TestTransparentBgANSI(t *testing.T) {
	transparentDark(t)
	if got := BgANSI(); got != "\x1b[49m" {
		t.Errorf("BgANSI() = %q, want \\x1b[49m", got)
	}
	if got := SidebarBgANSI(); got != "\x1b[49m" {
		t.Errorf("SidebarBgANSI() = %q, want \\x1b[49m", got)
	}
	if got, want := SolidBgANSI(), bgANSIFor(styles.ThemeBackground()); got != want || got == "\x1b[49m" {
		t.Errorf("SolidBgANSI() = %q, want the theme colour %q", got, want)
	}
}

func TestSolidBgANSIMatchesBgANSIWhenOpaque(t *testing.T) {
	styles.SetTransparentBackground(false)
	styles.Apply("dark", config.Theme{})
	if SolidBgANSI() != BgANSI() {
		t.Errorf("SolidBgANSI() = %q, BgANSI() = %q; they must match with the option off", SolidBgANSI(), BgANSI())
	}
}

func TestSubstituteBgSGR_DefaultBackground(t *testing.T) {
	in := "a\x1b[49mb\x1b[38;5;49mc\x1b[1;49md"
	want := "a\x1b[48;2;1;2;3mb\x1b[38;5;49mc\x1b[1;48;2;1;2;3md"
	if got := substituteBgSGR(in, "49", "48;2;1;2;3"); got != want {
		t.Errorf("substituteBgSGR = %q, want %q (a 49 inside 38;5 is a colour index, not a bg)", got, want)
	}
}

// The selected row must be tinted on every cell. With the transparent
// background the base spans carry no bg, so after each reset the tint has
// to come back by itself.
func TestTransparentSelectedVariantTintsAfterResets(t *testing.T) {
	// ANSI Dark covers the basic 16-colour tint ("100"), dark the truecolor one.
	for _, theme := range []string{"dark", "ansi dark"} {
		t.Run(theme, func(t *testing.T) {
			transparentDark(t)
			styles.Apply(theme, config.Theme{})
			tint := SelectionTintBgANSI(true)
			content := styles.Username("U1", false).Render("alice") + " " +
				styles.Timestamp.Render("3:04 PM") + "\n" +
				ReapplyBgAfterResets(styles.MessageText.Render("hi ")+lipgloss.NewStyle().Bold(true).Render("there")+" you", BgANSI()+FgANSI())

			got := RepaintBgToSelectionTint(content, true)
			if !strings.HasPrefix(got, tint) {
				t.Errorf("selected content does not start with the tint: %q", got)
			}
			if strings.Contains(got, "\x1b[49m") {
				t.Errorf("selected content still sets the default background: %q", got)
			}
			for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
				rest := got
				for {
					i := strings.Index(rest, reset)
					if i < 0 {
						break
					}
					rest = rest[i+len(reset):]
					if !strings.HasPrefix(rest, tint) {
						t.Fatalf("a reset is not followed by the tint: ...%q", rest)
					}
				}
			}
		})
	}
}

func TestTransparentChromeStylesStaySolid(t *testing.T) {
	transparentDark(t)
	s := ChromeStyles()
	bg := styles.ThemeBackground()
	for name, st := range map[string]lipgloss.Style{"Box": s.Box, "Input": s.Input, "Row": s.Row, "Footer": s.Footer} {
		if st.GetBackground() != bg {
			t.Errorf("ChromeStyles().%s background = %v, want the theme's %v", name, st.GetBackground(), bg)
		}
	}
	if !strings.HasPrefix(s.BaseANSI, SolidBgANSI()) {
		t.Errorf("BaseANSI = %q, want it to start with the solid %q", s.BaseANSI, SolidBgANSI())
	}
	if _, end, ok := DialogSearchHighlightSGR(); !ok || strings.Contains(end, "\x1b[49m") {
		t.Errorf("DialogSearchHighlightSGR close = %q, must not restore the default background", end)
	}
}

func TestTransparentWrapRowsInvisibleBorder(t *testing.T) {
	transparentDark(t)
	msg := MessageItem{TS: "1.0", UserName: "alice", Text: "hi", Timestamp: "3:04 PM"}
	rows := WrapRows(msg, RenderedMessage{Content: "hi"}, 20, NewWrapStyles(true, false))
	for _, l := range rows.Lines {
		if strings.Contains(l, "▌") {
			t.Errorf("unselected row draws the ▌ border glyph, which shows on a transparent cell: %q", l)
		}
	}
	if !strings.Contains(strings.Join(rows.Selected, "\n"), "▌") {
		t.Error("selected row lost its ▌ marker")
	}
}
