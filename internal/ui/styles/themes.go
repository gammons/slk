package styles

import (
	"sort"
	"strings"
)

// ThemeColors holds the 10 semantic colors for a theme.
type ThemeColors struct {
	Primary     string
	Accent      string
	Warning     string
	Error       string
	Background  string
	Surface     string
	SurfaceDark string
	Text        string
	TextMuted   string
	Border      string
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
