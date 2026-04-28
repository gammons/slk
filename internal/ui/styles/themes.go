package styles

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ThemeColors holds the semantic colors for a theme.
//
// The "Sidebar*" / "RailBackground" and "Selection*" colors are optional;
// when empty they fall back to a sensible default in Apply (see styles.go),
// so existing themes don't need to specify them. The sidebar/rail fields
// allow themes like "Slack Default" to have a dark sidebar/rail combined
// with a light message pane; the selection fields let a theme customize the
// highlight used for selected message text (defaults: Primary background,
// Background foreground).
type ThemeColors struct {
	Primary             string `toml:"primary"`
	Accent              string `toml:"accent"`
	Warning             string `toml:"warning"`
	Error               string `toml:"error"`
	Background          string `toml:"background"`
	Surface             string `toml:"surface"`
	SurfaceDark         string `toml:"surface_dark"`
	Text                string `toml:"text"`
	TextMuted           string `toml:"text_muted"`
	Border              string `toml:"border"`
	SidebarBackground   string `toml:"sidebar_background"`
	SidebarText         string `toml:"sidebar_text"`
	SidebarTextMuted    string `toml:"sidebar_text_muted"`
	RailBackground      string `toml:"rail_background"`
	SelectionBackground string `toml:"selection_background"`
	SelectionForeground string `toml:"selection_foreground"`
}

// builtinThemes maps lowercase theme names to their display name and colors.
var builtinThemes = map[string]struct {
	Name   string
	Colors ThemeColors
}{
	"dark": {"Dark", ThemeColors{
		Primary: "#4A9EFF", Accent: "#50C878", Warning: "#E0A030", Error: "#E04040",
		Background: "#1A1A2E", Surface: "#16162B", SurfaceDark: "#0F0F23",
		Text: "#E0E0E0", TextMuted: "#888888", Border: "#333333",
	}},
	"light": {"Light", ThemeColors{
		Primary: "#0366D6", Accent: "#28A745", Warning: "#D9840D", Error: "#CB2431",
		Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
		Text: "#24292E", TextMuted: "#6A737D", Border: "#D1D5DA",
	}},
	"dracula": {"Dracula", ThemeColors{
		Primary: "#BD93F9", Accent: "#50FA7B", Warning: "#FFB86C", Error: "#FF5555",
		Background: "#282A36", Surface: "#343746", SurfaceDark: "#21222C",
		Text: "#F8F8F2", TextMuted: "#6272A4", Border: "#44475A",
	}},
	"solarized dark": {"Solarized Dark", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#002B36", Surface: "#073642", SurfaceDark: "#001E26",
		Text: "#839496", TextMuted: "#586E75", Border: "#073642",
	}},
	"solarized light": {"Solarized Light", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#FDF6E3", Surface: "#EEE8D5", SurfaceDark: "#E4DCCA",
		Text: "#657B83", TextMuted: "#93A1A1", Border: "#EEE8D5",
	}},
	"gruvbox dark": {"Gruvbox Dark", ThemeColors{
		Primary: "#83A598", Accent: "#B8BB26", Warning: "#FABD2F", Error: "#FB4934",
		Background: "#282828", Surface: "#3C3836", SurfaceDark: "#1D2021",
		Text: "#EBDBB2", TextMuted: "#928374", Border: "#504945",
	}},
	"gruvbox light": {"Gruvbox Light", ThemeColors{
		Primary: "#076678", Accent: "#79740E", Warning: "#B57614", Error: "#9D0006",
		Background: "#FBF1C7", Surface: "#EBDBB2", SurfaceDark: "#D5C4A1",
		Text: "#3C3836", TextMuted: "#928374", Border: "#BDAE93",
	}},
	"nord": {"Nord", ThemeColors{
		Primary: "#88C0D0", Accent: "#A3BE8C", Warning: "#EBCB8B", Error: "#BF616A",
		Background: "#2E3440", Surface: "#3B4252", SurfaceDark: "#242933",
		Text: "#ECEFF4", TextMuted: "#7B88A1", Border: "#4C566A",
	}},
	"tokyo night": {"Tokyo Night", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#1A1B26", Surface: "#24283B", SurfaceDark: "#16161E",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
	}},
	"catppuccin mocha": {"Catppuccin Mocha", ThemeColors{
		Primary: "#89B4FA", Accent: "#A6E3A1", Warning: "#F9E2AF", Error: "#F38BA8",
		Background: "#1E1E2E", Surface: "#313244", SurfaceDark: "#181825",
		Text: "#CDD6F4", TextMuted: "#6C7086", Border: "#45475A",
	}},
	"one dark": {"One Dark", ThemeColors{
		Primary: "#61AFEF", Accent: "#98C379", Warning: "#E5C07B", Error: "#E06C75",
		Background: "#282C34", Surface: "#2C313C", SurfaceDark: "#21252B",
		Text: "#ABB2BF", TextMuted: "#636D83", Border: "#3E4452",
	}},
	"rosé pine": {"Rosé Pine", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#191724", Surface: "#1F1D2E", SurfaceDark: "#16141F",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#26233A",
	}},
	"rosé pine moon": {"Rosé Pine Moon", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#232136", Surface: "#2A273F", SurfaceDark: "#1A1825",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#393552",
	}},
	"slack default": {"Slack Default", ThemeColors{
		// Slack's iconic look: white message pane with dark sidebar and a
		// slightly darker workspace rail. Slack-blue links, Slack-green
		// accent. Sidebar/rail colors come from a real Slack screenshot.
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E", Error: "#E01E5A",
		Background: "#FFFFFF", Surface: "#F8F8F8", SurfaceDark: "#F0F0F0",
		Text: "#1D1C1D", TextMuted: "#616061", Border: "#DDDDDD",
		SidebarBackground: "#434243", SidebarText: "#D1D2D3", SidebarTextMuted: "#9A9B9E",
		RailBackground: "#2E2C2E",
	}},
	"monokai": {"Monokai", ThemeColors{
		Primary: "#66D9EF", Accent: "#A6E22E", Warning: "#E6DB74", Error: "#F92672",
		Background: "#272822", Surface: "#3E3D32", SurfaceDark: "#1E1F1C",
		Text: "#F8F8F2", TextMuted: "#75715E", Border: "#49483E",
	}},
	"github dark": {"GitHub Dark", ThemeColors{
		Primary: "#58A6FF", Accent: "#3FB950", Warning: "#D29922", Error: "#F85149",
		Background: "#0D1117", Surface: "#161B22", SurfaceDark: "#010409",
		Text: "#C9D1D9", TextMuted: "#8B949E", Border: "#30363D",
	}},
	"ayu mirage": {"Ayu Mirage", ThemeColors{
		Primary: "#73D0FF", Accent: "#BAE67E", Warning: "#FFD580", Error: "#F28779",
		Background: "#1F2430", Surface: "#232834", SurfaceDark: "#191E2A",
		Text: "#CBCCC6", TextMuted: "#707A8C", Border: "#33415E",
	}},
	"everforest dark": {"Everforest Dark", ThemeColors{
		Primary: "#7FBBB3", Accent: "#A7C080", Warning: "#DBBC7F", Error: "#E67E80",
		Background: "#2D353B", Surface: "#343F44", SurfaceDark: "#232A2E",
		Text: "#D3C6AA", TextMuted: "#859289", Border: "#3D484D",
	}},
	"kanagawa": {"Kanagawa", ThemeColors{
		Primary: "#7FB4CA", Accent: "#98BB6C", Warning: "#E6C384", Error: "#E46876",
		Background: "#1F1F28", Surface: "#2A2A37", SurfaceDark: "#16161D",
		Text: "#DCD7BA", TextMuted: "#727169", Border: "#363646",
	}},
	"material ocean": {"Material Ocean", ThemeColors{
		Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
		Background: "#0F111A", Surface: "#1A1C25", SurfaceDark: "#090B10",
		Text: "#A6ACCD", TextMuted: "#4B526D", Border: "#1F2233",
	}},
	"synthwave": {"Synthwave", ThemeColors{
		Primary: "#36F9F6", Accent: "#72F1B8", Warning: "#FEDE5D", Error: "#FF6E96",
		Background: "#241B2F", Surface: "#2D2139", SurfaceDark: "#1A1226",
		Text: "#F8F8F2", TextMuted: "#848BBD", Border: "#495495",
	}},
}

// customThemes stores themes loaded from the user's themes directory.
var customThemes = map[string]struct {
	Name   string
	Colors ThemeColors
}{}

// RegisterCustomTheme adds a custom theme to the registry.
func RegisterCustomTheme(name string, colors ThemeColors) {
	customThemes[strings.ToLower(name)] = struct {
		Name   string
		Colors ThemeColors
	}{Name: name, Colors: colors}
}

// ThemeNames returns the display names of all available themes (built-in + custom),
// sorted alphabetically.
func ThemeNames() []string {
	seen := map[string]string{}
	for _, t := range builtinThemes {
		seen[strings.ToLower(t.Name)] = t.Name
	}
	for _, t := range customThemes {
		seen[strings.ToLower(t.Name)] = t.Name
	}
	var names []string
	for _, name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lookupTheme finds a theme by name (case-insensitive). Custom themes take
// priority over built-in. Returns dark theme if not found.
func lookupTheme(name string) ThemeColors {
	key := strings.ToLower(name)
	if t, ok := customThemes[key]; ok {
		return t.Colors
	}
	if t, ok := builtinThemes[key]; ok {
		return t.Colors
	}
	return builtinThemes["dark"].Colors
}

// customThemeFile is the TOML structure for a custom theme file.
type customThemeFile struct {
	Name   string      `toml:"name"`
	Colors ThemeColors `toml:"colors"`
}

// LoadCustomThemes scans a directory for .toml theme files and registers them.
func LoadCustomThemes(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory doesn't exist or can't be read — silently skip
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var tf customThemeFile
		if err := toml.Unmarshal(data, &tf); err != nil {
			continue
		}
		if tf.Name == "" {
			continue
		}
		// Fill missing colors from dark defaults
		dark := builtinThemes["dark"].Colors
		if tf.Colors.Primary == "" {
			tf.Colors.Primary = dark.Primary
		}
		if tf.Colors.Accent == "" {
			tf.Colors.Accent = dark.Accent
		}
		if tf.Colors.Warning == "" {
			tf.Colors.Warning = dark.Warning
		}
		if tf.Colors.Error == "" {
			tf.Colors.Error = dark.Error
		}
		if tf.Colors.Background == "" {
			tf.Colors.Background = dark.Background
		}
		if tf.Colors.Surface == "" {
			tf.Colors.Surface = dark.Surface
		}
		if tf.Colors.SurfaceDark == "" {
			tf.Colors.SurfaceDark = dark.SurfaceDark
		}
		if tf.Colors.Text == "" {
			tf.Colors.Text = dark.Text
		}
		if tf.Colors.TextMuted == "" {
			tf.Colors.TextMuted = dark.TextMuted
		}
		if tf.Colors.Border == "" {
			tf.Colors.Border = dark.Border
		}
		RegisterCustomTheme(tf.Name, tf.Colors)
	}
}
