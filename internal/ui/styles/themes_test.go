package styles

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
)

func TestLoadCustomThemes(t *testing.T) {
	dir := t.TempDir()

	themeData := []byte(`
name = "My Custom"

[colors]
primary = "#AABBCC"
accent = "#112233"
warning = "#445566"
error = "#778899"
background = "#000000"
surface = "#111111"
surface_dark = "#222222"
text = "#FFFFFF"
text_muted = "#999999"
border = "#555555"
`)
	if err := os.WriteFile(filepath.Join(dir, "mycustom.toml"), themeData, 0644); err != nil {
		t.Fatal(err)
	}

	// Also write a non-toml file that should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a theme"), 0644); err != nil {
		t.Fatal(err)
	}

	LoadCustomThemes(os.DirFS(dir))

	// Verify the custom theme was loaded
	names := ThemeNames()
	found := false
	for _, n := range names {
		if n == "My Custom" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'My Custom' in theme names, got %v", names)
	}

	// Verify it can be applied
	Apply("my custom", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#AABBCC")) {
		t.Errorf("expected custom primary #AABBCC")
	}

	// Clean up custom themes for other tests
	customThemes = map[string]struct {
		Name   string
		Colors ThemeColors
	}{}
	Apply("dark", config.Theme{})
}

func TestLoadCustomThemesMissingDir(t *testing.T) {
	// Should not panic on non-existent directory
	LoadCustomThemes(os.DirFS("/tmp/nonexistent-theme-dir-12345"))
}

func TestNewBuiltinThemesRegistered(t *testing.T) {
	newThemes := []string{
		"Catppuccin Latte",
		"GitHub Light",
		"Tokyo Night Light",
		"Atom One Light",
		"Catppuccin Frappé",
		"Catppuccin Macchiato",
		"Tokyo Night Storm",
		"Cobalt2",
		"Iceberg",
		"Oceanic Next",
		"Cyberpunk Neon",
		"Material Palenight",
	}

	names := ThemeNames()
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}

	for _, want := range newThemes {
		if !have[want] {
			t.Errorf("new built-in theme %q not registered (ThemeNames: %v)", want, names)
		}
	}
}

func TestNewThemesHaveRequiredColors(t *testing.T) {
	newThemes := []string{
		"catppuccin latte",
		"github light",
		"tokyo night light",
		"atom one light",
		"catppuccin frappé",
		"catppuccin macchiato",
		"tokyo night storm",
		"cobalt2",
		"iceberg",
		"oceanic next",
		"cyberpunk neon",
		"material palenight",
	}
	for _, key := range newThemes {
		c := lookupTheme(key)
		if c.Primary == "" || c.Accent == "" || c.Warning == "" || c.Error == "" ||
			c.Background == "" || c.Surface == "" || c.SurfaceDark == "" ||
			c.Text == "" || c.TextMuted == "" || c.Border == "" {
			t.Errorf("theme %q is missing one or more required color fields: %+v", key, c)
		}
	}
}

func TestLightThemesHaveDarkSidebars(t *testing.T) {
	// Light themes should set SidebarBackground/etc explicitly so the
	// sidebar/rail aren't washed out against the light message pane.
	lightThemes := []string{
		"catppuccin latte",
		"github light",
		"tokyo night light",
		"atom one light",
	}
	for _, key := range lightThemes {
		c := lookupTheme(key)
		if c.SidebarBackground == "" {
			t.Errorf("light theme %q must set SidebarBackground", key)
		}
		if c.RailBackground == "" {
			t.Errorf("light theme %q must set RailBackground", key)
		}
	}
}

// TestANSIDarkThemeRegistered asserts the ansi-dark theme is present
// in the theme switcher and that every color field is populated with
// a value that resolves to ansi.BasicColor — confirming the theme
// will inherit the user's terminal palette rather than emit truecolor.
func TestANSIDarkThemeRegistered(t *testing.T) {
	names := ThemeNames()
	found := false
	for _, n := range names {
		if n == "ANSI Dark" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected \"ANSI Dark\" in ThemeNames, got %v", names)
	}

	c := lookupTheme("ANSI Dark")
	required := map[string]string{
		"Primary":     c.Primary,
		"Accent":      c.Accent,
		"Warning":     c.Warning,
		"Error":       c.Error,
		"Background":  c.Background,
		"Surface":     c.Surface,
		"SurfaceDark": c.SurfaceDark,
		"Text":        c.Text,
		"TextMuted":   c.TextMuted,
		"Border":      c.Border,
	}
	for name, val := range required {
		if val == "" {
			t.Errorf("ansi dark.%s is empty", name)
			continue
		}
		col := lipgloss.Color(val)
		if _, ok := col.(ansi.BasicColor); !ok {
			t.Errorf("ansi dark.%s = %q resolves to %T, want ansi.BasicColor",
				name, val, col)
		}
	}
}

// TestANSILightThemeRegistered: mirror of TestANSIDarkThemeRegistered
// for the light variant.
func TestANSILightThemeRegistered(t *testing.T) {
	names := ThemeNames()
	found := false
	for _, n := range names {
		if n == "ANSI Light" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected \"ANSI Light\" in ThemeNames, got %v", names)
	}

	c := lookupTheme("ANSI Light")
	required := map[string]string{
		"Primary":     c.Primary,
		"Accent":      c.Accent,
		"Warning":     c.Warning,
		"Error":       c.Error,
		"Background":  c.Background,
		"Surface":     c.Surface,
		"SurfaceDark": c.SurfaceDark,
		"Text":        c.Text,
		"TextMuted":   c.TextMuted,
		"Border":      c.Border,
	}
	for name, val := range required {
		if val == "" {
			t.Errorf("ansi light.%s is empty", name)
			continue
		}
		col := lipgloss.Color(val)
		if _, ok := col.(ansi.BasicColor); !ok {
			t.Errorf("ansi light.%s = %q resolves to %T, want ansi.BasicColor",
				name, val, col)
		}
	}
}

// TestANSIThemeLookupViaDisplayName regression-pins the realistic
// theme-switcher path: when the user picks "ANSI Dark" via Ctrl+y,
// the display name is saved verbatim to config.toml. On the next
// render, lookupTheme must resolve "ANSI Dark" to the ansi-dark
// theme — not fall through to the default "dark" theme.
//
// The key in builtinThemes must therefore lowercase-match "ANSI Dark"
// after strings.ToLower, i.e. it must use a space separator like every
// other multi-word built-in theme ("tokyo night", "gruvbox dark", etc).
func TestANSIThemeLookupViaDisplayName(t *testing.T) {
	dark := lookupTheme("ANSI Dark")
	if dark.Background != "0" {
		t.Errorf("lookupTheme(\"ANSI Dark\").Background = %q, want \"0\" — likely fell through to default \"dark\" theme", dark.Background)
	}

	light := lookupTheme("ANSI Light")
	if light.Background != "15" {
		t.Errorf("lookupTheme(\"ANSI Light\").Background = %q, want \"15\" — likely fell through to default \"dark\" theme", light.Background)
	}
}

// TestANSIThemesSelectionTintPaletteInherited regression-pins that
// SelectionTintColor for the ANSI themes returns a palette-inherited
// ansi.BasicColor — not a near-black RGB mix from the default
// mixColors(Accent, Background=ANSI 0, 0.15) path.
//
// Without explicit SelectionBgFocused/SelectionBgUnfocused on the
// theme, ansi-dark's selection tint computes to roughly RGB(0,25,25)
// which renders as effectively black against any dark terminal bg.
// The fix sets the optional theme fields to a palette ANSI color
// ("8" = bright black / gray) so the tint is visible and tracks
// the user's terminal palette.
func TestANSIThemesSelectionTintPaletteInherited(t *testing.T) {
	cases := []struct {
		theme string
	}{
		{"ansi dark"},
		{"ansi light"},
	}
	for _, tc := range cases {
		t.Run(tc.theme, func(t *testing.T) {
			Apply(tc.theme, config.Theme{})
			t.Cleanup(func() { Apply("dark", config.Theme{}) })

			focused := SelectionTintColor(true)
			if _, ok := focused.(ansi.BasicColor); !ok {
				t.Errorf("%s focused selection tint = %T, want ansi.BasicColor (palette-inherited)", tc.theme, focused)
			}

			unfocused := SelectionTintColor(false)
			if _, ok := unfocused.(ansi.BasicColor); !ok {
				t.Errorf("%s unfocused selection tint = %T, want ansi.BasicColor (palette-inherited)", tc.theme, unfocused)
			}
		})
	}
}

// --- channels-panel contrast guard ---

// contrastAllowlist are themes intentionally exempt from the
// channels-panel contrast requirement: the ANSI themes use palette
// numbers (not hex), so a CIELAB hex computation doesn't apply to them
// (they inherit the terminal palette). "hot dog stand" is a deliberately
// garish novelty whose hand-picked red-on-yellow palette we never want
// the contrast retune to touch; it already clears the threshold on its
// own, so the exemption is belt-and-suspenders.
var contrastAllowlist = map[string]bool{
	"ansi dark":     true,
	"ansi light":    true,
	"hot dog stand": true,
}

// minChannelsPanelDeltaLstar is the minimum perceptual lightness
// difference (CIELAB L*) between a theme's message-pane Background and
// its channels-panel SidebarBackground. 6.0 is calibrated so the
// slack-default split (ΔL*≈72) passes easily while a 1–2% nudge
// (ΔL*≈3) fails. See spec 2026-06-07-more-themes-design.md.
const minChannelsPanelDeltaLstar = 6.0

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// lstar returns the CIELAB L* (0..100) of a "#RRGGBB" hex string.
func lstar(hex string) float64 {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return 0
	}
	ri, _ := strconv.ParseInt(h[0:2], 16, 0)
	gi, _ := strconv.ParseInt(h[2:4], 16, 0)
	bi, _ := strconv.ParseInt(h[4:6], 16, 0)
	r := srgbToLinear(float64(ri) / 255)
	g := srgbToLinear(float64(gi) / 255)
	b := srgbToLinear(float64(bi) / 255)
	y := 0.2126*r + 0.7152*g + 0.0722*b
	if y > 0.008856 {
		return 116*math.Cbrt(y) - 16
	}
	return 903.3 * y
}

// TestChannelsPanelContrast asserts every non-allowlisted built-in
// theme gives the channels panel a perceptibly distinct surface from
// the message pane. Reports each theme's measured ΔL* on failure so the
// retune is a deterministic adjust-and-rerun loop.
func TestChannelsPanelContrast(t *testing.T) {
	for key, theme := range builtinThemes {
		if contrastAllowlist[key] {
			continue
		}
		bg := theme.Colors.Background
		sb := theme.Colors.SidebarBackground
		if sb == "" {
			sb = bg // falls back to Background -> zero contrast
		}
		if !strings.HasPrefix(bg, "#") || !strings.HasPrefix(sb, "#") {
			t.Errorf("theme %q: non-hex background/sidebar not allowlisted (bg=%q sidebar=%q)", key, bg, sb)
			continue
		}
		delta := math.Abs(lstar(bg) - lstar(sb))
		if delta < minChannelsPanelDeltaLstar {
			t.Errorf("theme %q channels-panel contrast too low: ΔL*=%.1f (bg=%s L*=%.1f, sidebar=%s L*=%.1f), want >= %.1f",
				key, delta, bg, lstar(bg), sb, lstar(sb), minChannelsPanelDeltaLstar)
		}
	}
}

var darkEditorThemes = []string{
	"Zenburn", "Gruvbox Material Dark", "Nightfox", "Carbonfox",
	"Melange Dark", "Vesper", "Flexoki Dark", "Modus Vivendi",
	"Night Owl", "Poimandres", "Ayu Dark", "Kanagawa Dragon",
}

func TestDarkEditorThemesRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range ThemeNames() {
		have[n] = true
	}
	for _, want := range darkEditorThemes {
		if !have[want] {
			t.Errorf("dark editor theme %q not registered", want)
		}
	}
}

func TestDarkEditorThemesHaveRequiredColors(t *testing.T) {
	for _, name := range darkEditorThemes {
		c := lookupTheme(strings.ToLower(name))
		if c.Primary == "" || c.Accent == "" || c.Warning == "" || c.Error == "" ||
			c.Background == "" || c.Surface == "" || c.SurfaceDark == "" ||
			c.Text == "" || c.TextMuted == "" || c.Border == "" {
			t.Errorf("theme %q missing required color(s): %+v", name, c)
		}
	}
}

var lightEditorThemes = []string{
	"Rosé Pine Dawn", "Everforest Light", "Flexoki Light",
	"Modus Operandi", "Kanagawa Lotus", "PaperColor Light",
}

func TestLightEditorThemesRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range ThemeNames() {
		have[n] = true
	}
	for _, want := range lightEditorThemes {
		if !have[want] {
			t.Errorf("light editor theme %q not registered", want)
		}
	}
}

func TestLightEditorThemesHaveRequiredColors(t *testing.T) {
	for _, name := range lightEditorThemes {
		c := lookupTheme(strings.ToLower(name))
		if c.Primary == "" || c.Accent == "" || c.Warning == "" || c.Error == "" ||
			c.Background == "" || c.Surface == "" || c.SurfaceDark == "" ||
			c.Text == "" || c.TextMuted == "" || c.Border == "" {
			t.Errorf("theme %q missing required color(s): %+v", name, c)
		}
	}
}

func TestLightEditorThemesHaveDarkSidebars(t *testing.T) {
	for _, name := range lightEditorThemes {
		c := lookupTheme(strings.ToLower(name))
		if c.SidebarBackground == "" {
			t.Errorf("light theme %q must set SidebarBackground", name)
		}
		if c.RailBackground == "" {
			t.Errorf("light theme %q must set RailBackground", name)
		}
	}
}

var slackBrandedThemes = []string{
	"Aubergine", "Ochin", "Choco Mint", "Mocha", "Nocturne",
}

func TestSlackBrandedThemesRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range ThemeNames() {
		have[n] = true
	}
	for _, want := range slackBrandedThemes {
		if !have[want] {
			t.Errorf("slack-branded theme %q not registered", want)
		}
	}
}

func TestSlackBrandedThemesHaveRequiredColors(t *testing.T) {
	for _, name := range slackBrandedThemes {
		c := lookupTheme(strings.ToLower(name))
		if c.Primary == "" || c.Accent == "" || c.Warning == "" || c.Error == "" ||
			c.Background == "" || c.Surface == "" || c.SurfaceDark == "" ||
			c.Text == "" || c.TextMuted == "" || c.Border == "" {
			t.Errorf("theme %q missing required color(s): %+v", name, c)
		}
	}
}

func TestBuiltinThemeCount(t *testing.T) {
	const want = 59
	if got := len(builtinThemes); got != want {
		t.Errorf("builtinThemes count = %d, want %d (update docs in README.md and wiki/Features.md if this changed intentionally)", got, want)
	}
}
