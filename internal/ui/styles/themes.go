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
	// ComposeInsertBG, SelectionBgFocused, and SelectionBgUnfocused are
	// optional explicit overrides for the tints derived in tint.go. When
	// empty, tint.go computes them from Accent/TextMuted+Background.
	ComposeInsertBG      string `toml:"compose_insert_bg"`
	SelectionBgFocused   string `toml:"selection_bg_focused"`
	SelectionBgUnfocused string `toml:"selection_bg_unfocused"`
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
		SidebarBackground: "#13132B", RailBackground: "#0A0A1A",
	}},
	"light": {"Light", ThemeColors{
		Primary: "#0366D6", Accent: "#28A745", Warning: "#D9840D", Error: "#CB2431",
		Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
		Text: "#24292E", TextMuted: "#6A737D", Border: "#D1D5DA",
		SidebarBackground: "#24292E", SidebarText: "#F6F8FA", SidebarTextMuted: "#8C959F",
		RailBackground: "#1B1F23",
	}},
	"dracula": {"Dracula", ThemeColors{
		Primary: "#BD93F9", Accent: "#50FA7B", Warning: "#FFB86C", Error: "#FF5555",
		Background: "#282A36", Surface: "#343746", SurfaceDark: "#21222C",
		Text: "#F8F8F2", TextMuted: "#6272A4", Border: "#44475A",
		SidebarBackground: "#1E1F2B", RailBackground: "#15161E",
	}},
	"solarized dark": {"Solarized Dark", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#002B36", Surface: "#073642", SurfaceDark: "#001E26",
		Text: "#839496", TextMuted: "#586E75", Border: "#073642",
		SidebarBackground: "#001F28", RailBackground: "#00141B",
	}},
	"solarized light": {"Solarized Light", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#FDF6E3", Surface: "#EEE8D5", SurfaceDark: "#E4DCCA",
		Text: "#657B83", TextMuted: "#93A1A1", Border: "#EEE8D5",
		SidebarBackground: "#002B36", SidebarText: "#93A1A1", SidebarTextMuted: "#586E75",
		RailBackground: "#001E26",
	}},
	"gruvbox dark": {"Gruvbox Dark", ThemeColors{
		Primary: "#83A598", Accent: "#B8BB26", Warning: "#FABD2F", Error: "#FB4934",
		Background: "#282828", Surface: "#3C3836", SurfaceDark: "#1D2021",
		Text: "#EBDBB2", TextMuted: "#928374", Border: "#504945",
		SidebarBackground: "#1D2021", RailBackground: "#141414",
	}},
	"gruvbox light": {"Gruvbox Light", ThemeColors{
		Primary: "#076678", Accent: "#79740E", Warning: "#B57614", Error: "#9D0006",
		Background: "#FBF1C7", Surface: "#EBDBB2", SurfaceDark: "#D5C4A1",
		Text: "#3C3836", TextMuted: "#928374", Border: "#BDAE93",
		SidebarBackground: "#282828", SidebarText: "#EBDBB2", SidebarTextMuted: "#A89984",
		RailBackground: "#1D2021",
	}},
	"nord": {"Nord", ThemeColors{
		Primary: "#88C0D0", Accent: "#A3BE8C", Warning: "#EBCB8B", Error: "#BF616A",
		Background: "#2E3440", Surface: "#3B4252", SurfaceDark: "#242933",
		Text: "#ECEFF4", TextMuted: "#7B88A1", Border: "#4C566A",
		SidebarBackground: "#242933", RailBackground: "#191D26",
	}},
	"tokyo night": {"Tokyo Night", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#1A1B26", Surface: "#24283B", SurfaceDark: "#16161E",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
		SidebarBackground: "#15161F", RailBackground: "#0D0E14",
	}},
	"catppuccin mocha": {"Catppuccin Mocha", ThemeColors{
		Primary: "#89B4FA", Accent: "#A6E3A1", Warning: "#F9E2AF", Error: "#F38BA8",
		Background: "#1E1E2E", Surface: "#313244", SurfaceDark: "#181825",
		Text: "#CDD6F4", TextMuted: "#6C7086", Border: "#45475A",
		SidebarBackground: "#181825", RailBackground: "#11111B",
	}},
	"one dark": {"One Dark", ThemeColors{
		Primary: "#61AFEF", Accent: "#98C379", Warning: "#E5C07B", Error: "#E06C75",
		Background: "#282C34", Surface: "#2C313C", SurfaceDark: "#21252B",
		Text: "#ABB2BF", TextMuted: "#636D83", Border: "#3E4452",
		SidebarBackground: "#21252B", RailBackground: "#181B20",
	}},
	"rosé pine": {"Rosé Pine", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#191724", Surface: "#1F1D2E", SurfaceDark: "#16141F",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#26233A",
		SidebarBackground: "#13111C", RailBackground: "#0D0B14",
	}},
	"rosé pine moon": {"Rosé Pine Moon", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#232136", Surface: "#2A273F", SurfaceDark: "#1A1825",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#393552",
		SidebarBackground: "#1A1825", RailBackground: "#100F18",
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
		SidebarBackground: "#1E1F1C", RailBackground: "#141511",
	}},
	"github dark": {"GitHub Dark", ThemeColors{
		Primary: "#58A6FF", Accent: "#3FB950", Warning: "#D29922", Error: "#F85149",
		Background: "#0D1117", Surface: "#161B22", SurfaceDark: "#010409",
		Text: "#C9D1D9", TextMuted: "#8B949E", Border: "#30363D",
		SidebarBackground: "#010409", RailBackground: "#000000",
	}},
	"ayu mirage": {"Ayu Mirage", ThemeColors{
		Primary: "#73D0FF", Accent: "#BAE67E", Warning: "#FFD580", Error: "#F28779",
		Background: "#1F2430", Surface: "#232834", SurfaceDark: "#191E2A",
		Text: "#CBCCC6", TextMuted: "#707A8C", Border: "#33415E",
		SidebarBackground: "#191E2A", RailBackground: "#10141C",
	}},
	"everforest dark": {"Everforest Dark", ThemeColors{
		Primary: "#7FBBB3", Accent: "#A7C080", Warning: "#DBBC7F", Error: "#E67E80",
		Background: "#2D353B", Surface: "#343F44", SurfaceDark: "#232A2E",
		Text: "#D3C6AA", TextMuted: "#859289", Border: "#3D484D",
		SidebarBackground: "#232A2E", RailBackground: "#1A1F22",
	}},
	"kanagawa": {"Kanagawa", ThemeColors{
		Primary: "#7FB4CA", Accent: "#98BB6C", Warning: "#E6C384", Error: "#E46876",
		Background: "#1F1F28", Surface: "#2A2A37", SurfaceDark: "#16161D",
		Text: "#DCD7BA", TextMuted: "#727169", Border: "#363646",
		SidebarBackground: "#16161D", RailBackground: "#0F0F14",
	}},
	"material ocean": {"Material Ocean", ThemeColors{
		Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
		Background: "#0F111A", Surface: "#1A1C25", SurfaceDark: "#090B10",
		Text: "#A6ACCD", TextMuted: "#4B526D", Border: "#1F2233",
		SidebarBackground: "#090B10", RailBackground: "#04060A",
	}},
	"synthwave": {"Synthwave", ThemeColors{
		Primary: "#36F9F6", Accent: "#72F1B8", Warning: "#FEDE5D", Error: "#FF6E96",
		Background: "#241B2F", Surface: "#2D2139", SurfaceDark: "#1A1226",
		Text: "#F8F8F2", TextMuted: "#848BBD", Border: "#495495",
		SidebarBackground: "#1A1226", RailBackground: "#100A1A",
	}},
	"catppuccin latte": {"Catppuccin Latte", ThemeColors{
		Primary: "#1E66F5", Accent: "#40A02B", Warning: "#DF8E1D", Error: "#D20F39",
		Background: "#EFF1F5", Surface: "#E6E9EF", SurfaceDark: "#DCE0E8",
		Text: "#4C4F69", TextMuted: "#6C6F85", Border: "#BCC0CC",
		SidebarBackground: "#1E1E2E", SidebarText: "#CDD6F4", SidebarTextMuted: "#9399B2",
		RailBackground: "#181825",
	}},
	"github light": {"GitHub Light", ThemeColors{
		Primary: "#0969DA", Accent: "#1A7F37", Warning: "#9A6700", Error: "#CF222E",
		Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
		Text: "#1F2328", TextMuted: "#656D76", Border: "#D0D7DE",
		SidebarBackground: "#24292F", SidebarText: "#F6F8FA", SidebarTextMuted: "#8C959F",
		RailBackground: "#1B1F23",
	}},
	"tokyo night light": {"Tokyo Night Light", ThemeColors{
		Primary: "#34548A", Accent: "#485E30", Warning: "#8F5E15", Error: "#8C4351",
		Background: "#D5D6DB", Surface: "#CBCCD1", SurfaceDark: "#C4C8DA",
		Text: "#343B58", TextMuted: "#6172B0", Border: "#9699A8",
		SidebarBackground: "#1A1B26", SidebarText: "#A9B1D6", SidebarTextMuted: "#565F89",
		RailBackground: "#16161E",
	}},
	"atom one light": {"Atom One Light", ThemeColors{
		Primary: "#4078F2", Accent: "#50A14F", Warning: "#C18401", Error: "#E45649",
		Background: "#FAFAFA", Surface: "#F0F0F0", SurfaceDark: "#E5E5E6",
		Text: "#383A42", TextMuted: "#A0A1A7", Border: "#D3D3D3",
		SidebarBackground: "#282C34", SidebarText: "#ABB2BF", SidebarTextMuted: "#5C6370",
		RailBackground: "#21252B",
	}},
	"catppuccin frappé": {"Catppuccin Frappé", ThemeColors{
		Primary: "#8CAAEE", Accent: "#A6D189", Warning: "#E5C890", Error: "#E78284",
		Background: "#303446", Surface: "#414559", SurfaceDark: "#292C3C",
		Text: "#C6D0F5", TextMuted: "#838BA7", Border: "#51576D",
		SidebarBackground: "#232634", RailBackground: "#1A1C25",
	}},
	"catppuccin macchiato": {"Catppuccin Macchiato", ThemeColors{
		Primary: "#8AADF4", Accent: "#A6DA95", Warning: "#EED49F", Error: "#ED8796",
		Background: "#24273A", Surface: "#363A4F", SurfaceDark: "#1E2030",
		Text: "#CAD3F5", TextMuted: "#6E738D", Border: "#494D64",
		SidebarBackground: "#1E2030", RailBackground: "#15162A",
	}},
	"tokyo night storm": {"Tokyo Night Storm", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#24283B", Surface: "#2F334D", SurfaceDark: "#1F2335",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
		SidebarBackground: "#1F2335", RailBackground: "#16182A",
	}},
	"cobalt2": {"Cobalt2", ThemeColors{
		Primary: "#FFC600", Accent: "#3AD900", Warning: "#FF9D00", Error: "#FF628C",
		Background: "#193549", Surface: "#1F4662", SurfaceDark: "#15232D",
		Text: "#E1EFFF", TextMuted: "#6E96B5", Border: "#0D3A58",
		SidebarBackground: "#0F2638", RailBackground: "#091A28",
	}},
	"iceberg": {"Iceberg", ThemeColors{
		Primary: "#84A0C6", Accent: "#B4BE82", Warning: "#E2A478", Error: "#E27878",
		Background: "#161821", Surface: "#1E2132", SurfaceDark: "#0F1117",
		Text: "#C6C8D1", TextMuted: "#6B7089", Border: "#2E313F",
		SidebarBackground: "#0F1117", RailBackground: "#08090D",
	}},
	"oceanic next": {"Oceanic Next", ThemeColors{
		Primary: "#6699CC", Accent: "#99C794", Warning: "#FAC863", Error: "#EC5F67",
		Background: "#1B2B34", Surface: "#343D46", SurfaceDark: "#16232B",
		Text: "#CDD3DE", TextMuted: "#65737E", Border: "#4F5B66",
		SidebarBackground: "#16232B", RailBackground: "#0E1A20",
	}},
	"cyberpunk neon": {"Cyberpunk Neon", ThemeColors{
		Primary: "#0ABDC6", Accent: "#00FF9C", Warning: "#FCEE0C", Error: "#EA00D9",
		Background: "#000B1E", Surface: "#0D1B2A", SurfaceDark: "#000814",
		Text: "#D7D7D7", TextMuted: "#7E7E8E", Border: "#133E7C",
		SidebarBackground: "#000814", RailBackground: "#00050D",
	}},
	"material palenight": {"Material Palenight", ThemeColors{
		Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
		Background: "#292D3E", Surface: "#34324A", SurfaceDark: "#1F1F2E",
		Text: "#A6ACCD", TextMuted: "#676E95", Border: "#3A3F58",
		SidebarBackground: "#1F1F2E", RailBackground: "#15151F",
	}},
	// Hot Dog Stand — a tribute to the legendary Windows 3.1 color scheme
	// of the same name. Bright yellow background, red accents, black text.
	// It was widely panned at the time and is now a beloved relic. Use at
	// your own peril.
	"hot dog stand": {"Hot Dog Stand", ThemeColors{
		Primary:           "#FF0000", // pure red, the iconic accent
		Accent:            "#000000", // black, for absolute maximum contrast
		Warning:           "#FF8000", // mustard
		Error:             "#800000", // dark red
		Background:        "#FFFF00", // BRIGHT YELLOW — the iconic background
		Surface:           "#FFFF80", // pale yellow
		SurfaceDark:       "#FFCC00", // amber
		Text:              "#000000", // black on yellow
		TextMuted:         "#7F6F00", // muddy olive
		Border:            "#FF0000", // red borders everywhere
		SidebarBackground: "#FF0000", // red sidebar — Windows 3.1 active title bar
		SidebarText:       "#FFFF00", // yellow text on red
		SidebarTextMuted:  "#FFCC00",
		RailBackground:    "#800000", // dark red rail
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
