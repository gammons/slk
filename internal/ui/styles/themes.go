package styles

import (
	"io/fs"
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
	SearchHighlightBg   string `toml:"search_highlight_bg"`
	SearchHighlightFg   string `toml:"search_highlight_fg"`
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
		SidebarBackground: "#0D0D1A", RailBackground: "#060610",
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
		SidebarBackground: "#14151C", RailBackground: "#0C0C10",
	}},
	"solarized dark": {"Solarized Dark", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#002B36", Surface: "#073642", SurfaceDark: "#001E26",
		Text: "#839496", TextMuted: "#586E75", Border: "#073642",
		SidebarBackground: "#001820", RailBackground: "#000C10",
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
		SidebarBackground: "#181818", RailBackground: "#0E0E0E",
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
		SidebarBackground: "#1B2028", RailBackground: "#11141A",
	}},
	"tokyo night": {"Tokyo Night", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#1A1B26", Surface: "#24283B", SurfaceDark: "#16161E",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
		SidebarBackground: "#262838", RailBackground: "#1A1B26",
	}},
	"catppuccin mocha": {"Catppuccin Mocha", ThemeColors{
		Primary: "#89B4FA", Accent: "#A6E3A1", Warning: "#F9E2AF", Error: "#F38BA8",
		Background: "#1E1E2E", Surface: "#313244", SurfaceDark: "#181825",
		Text: "#CDD6F4", TextMuted: "#6C7086", Border: "#45475A",
		SidebarBackground: "#0E0E16", RailBackground: "#08080F",
	}},
	"one dark": {"One Dark", ThemeColors{
		Primary: "#61AFEF", Accent: "#98C379", Warning: "#E5C07B", Error: "#E06C75",
		Background: "#282C34", Surface: "#2C313C", SurfaceDark: "#21252B",
		Text: "#ABB2BF", TextMuted: "#636D83", Border: "#3E4452",
		SidebarBackground: "#1A1D23", RailBackground: "#101216",
	}},
	"rosé pine": {"Rosé Pine", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#191724", Surface: "#1F1D2E", SurfaceDark: "#16141F",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#26233A",
		SidebarBackground: "#262433", RailBackground: "#191724",
	}},
	"rosé pine moon": {"Rosé Pine Moon", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#232136", Surface: "#2A273F", SurfaceDark: "#1A1825",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#393552",
		SidebarBackground: "#15131F", RailBackground: "#0C0B12",
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
		SidebarBackground: "#181811", RailBackground: "#0F0F0A",
	}},
	"github dark": {"GitHub Dark", ThemeColors{
		Primary: "#58A6FF", Accent: "#3FB950", Warning: "#D29922", Error: "#F85149",
		Background: "#0D1117", Surface: "#161B22", SurfaceDark: "#010409",
		Text: "#C9D1D9", TextMuted: "#8B949E", Border: "#30363D",
		SidebarBackground: "#1C2128", RailBackground: "#0D1117",
	}},
	"ayu mirage": {"Ayu Mirage", ThemeColors{
		Primary: "#73D0FF", Accent: "#BAE67E", Warning: "#FFD580", Error: "#F28779",
		Background: "#1F2430", Surface: "#232834", SurfaceDark: "#191E2A",
		Text: "#CBCCC6", TextMuted: "#707A8C", Border: "#33415E",
		SidebarBackground: "#0E121C", RailBackground: "#080C16",
	}},
	"everforest dark": {"Everforest Dark", ThemeColors{
		Primary: "#7FBBB3", Accent: "#A7C080", Warning: "#DBBC7F", Error: "#E67E80",
		Background: "#2D353B", Surface: "#343F44", SurfaceDark: "#232A2E",
		Text: "#D3C6AA", TextMuted: "#859289", Border: "#3D484D",
		SidebarBackground: "#1E2429", RailBackground: "#141819",
	}},
	"kanagawa": {"Kanagawa", ThemeColors{
		Primary: "#7FB4CA", Accent: "#98BB6C", Warning: "#E6C384", Error: "#E46876",
		Background: "#1F1F28", Surface: "#2A2A37", SurfaceDark: "#16161D",
		Text: "#DCD7BA", TextMuted: "#727169", Border: "#363646",
		SidebarBackground: "#0C0C10", RailBackground: "#060608",
	}},
	"material ocean": {"Material Ocean", ThemeColors{
		Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
		Background: "#0F111A", Surface: "#1A1C25", SurfaceDark: "#090B10",
		Text: "#A6ACCD", TextMuted: "#4B526D", Border: "#1F2233",
		SidebarBackground: "#2A2D38", RailBackground: "#0F111A",
	}},
	"synthwave": {"Synthwave", ThemeColors{
		Primary: "#36F9F6", Accent: "#72F1B8", Warning: "#FEDE5D", Error: "#FF6E96",
		Background: "#241B2F", Surface: "#2D2139", SurfaceDark: "#1A1226",
		Text: "#F8F8F2", TextMuted: "#848BBD", Border: "#495495",
		SidebarBackground: "#150E20", RailBackground: "#0B0714",
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
		SidebarBackground: "#1E212C", RailBackground: "#14161D",
	}},
	"catppuccin macchiato": {"Catppuccin Macchiato", ThemeColors{
		Primary: "#8AADF4", Accent: "#A6DA95", Warning: "#EED49F", Error: "#ED8796",
		Background: "#24273A", Surface: "#363A4F", SurfaceDark: "#1E2030",
		Text: "#CAD3F5", TextMuted: "#6E738D", Border: "#494D64",
		SidebarBackground: "#181A28", RailBackground: "#0F1020",
	}},
	"tokyo night storm": {"Tokyo Night Storm", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#24283B", Surface: "#2F334D", SurfaceDark: "#1F2335",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
		SidebarBackground: "#181B29", RailBackground: "#0F1019",
	}},
	"cobalt2": {"Cobalt2", ThemeColors{
		Primary: "#FFC600", Accent: "#3AD900", Warning: "#FF9D00", Error: "#FF628C",
		Background: "#193549", Surface: "#1F4662", SurfaceDark: "#15232D",
		Text: "#E1EFFF", TextMuted: "#6E96B5", Border: "#0D3A58",
		SidebarBackground: "#0C2030", RailBackground: "#06141F",
	}},
	"iceberg": {"Iceberg", ThemeColors{
		Primary: "#84A0C6", Accent: "#B4BE82", Warning: "#E2A478", Error: "#E27878",
		Background: "#161821", Surface: "#1E2132", SurfaceDark: "#0F1117",
		Text: "#C6C8D1", TextMuted: "#6B7089", Border: "#2E313F",
		SidebarBackground: "#242736", RailBackground: "#161821",
	}},
	"oceanic next": {"Oceanic Next", ThemeColors{
		Primary: "#6699CC", Accent: "#99C794", Warning: "#FAC863", Error: "#EC5F67",
		Background: "#1B2B34", Surface: "#343D46", SurfaceDark: "#16232B",
		Text: "#CDD3DE", TextMuted: "#65737E", Border: "#4F5B66",
		SidebarBackground: "#0A1620", RailBackground: "#06121A",
	}},
	"cyberpunk neon": {"Cyberpunk Neon", ThemeColors{
		Primary: "#0ABDC6", Accent: "#00FF9C", Warning: "#FCEE0C", Error: "#EA00D9",
		Background: "#000B1E", Surface: "#0D1B2A", SurfaceDark: "#000814",
		Text: "#D7D7D7", TextMuted: "#7E7E8E", Border: "#133E7C",
		SidebarBackground: "#0D1B2A", RailBackground: "#000814",
	}},
	"material palenight": {"Material Palenight", ThemeColors{
		Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
		Background: "#292D3E", Surface: "#34324A", SurfaceDark: "#1F1F2E",
		Text: "#A6ACCD", TextMuted: "#676E95", Border: "#3A3F58",
		SidebarBackground: "#1B1B28", RailBackground: "#111119",
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
	// ansi-dark uses ANSI 16 color numbers ("0"–"15") instead of hex.
	// Values are passed through lipgloss.Color() which returns
	// ansi.BasicColor, so rendering uses native 16-color SGR escapes
	// and inherits the user's terminal palette.
	//
	// SelectionBgFocused / SelectionBgUnfocused are set explicitly to
	// ANSI 8 (bright black / palette gray) to bypass mixColors. With
	// Background=ANSI 0 the default mix produces a near-black RGB
	// tint (~RGB(0,25,25)) that's invisible against the terminal bg.
	"ansi dark": {"ANSI Dark", ThemeColors{
		Primary: "4", Accent: "6", Warning: "3", Error: "1",
		Background: "0", Surface: "8", SurfaceDark: "0",
		Text: "15", TextMuted: "8", Border: "8",
		SelectionBgFocused: "8", SelectionBgUnfocused: "8",
	}},
	// ansi-light is the light-terminal counterpart to ansi-dark. Same
	// ANSI-16-only constraint; values chosen for readability on light
	// terminal backgrounds.
	//
	// SelectionBgFocused / SelectionBgUnfocused use ANSI 8 (bright
	// black / palette gray) for the same reason as ansi-dark — bypass
	// mixColors so the selection tint is palette-inherited and
	// visible against the light terminal background.
	"ansi light": {"ANSI Light", ThemeColors{
		Primary: "4", Accent: "6", Warning: "3", Error: "1",
		Background: "15", Surface: "7", SurfaceDark: "7",
		Text: "0", TextMuted: "8", Border: "8",
		SelectionBgFocused: "8", SelectionBgUnfocused: "8",
	}},
	"zenburn": {"Zenburn", ThemeColors{
		Primary: "#8CD0D3", Accent: "#7F9F7F", Warning: "#F0DFAF", Error: "#CC9393",
		Background: "#3F3F3F", Surface: "#4F4F4F", SurfaceDark: "#2B2B2B",
		Text: "#DCDCCC", TextMuted: "#989890", Border: "#5F5F5F",
		SidebarBackground: "#2B2B2B", RailBackground: "#1F1F1F",
	}},
	"gruvbox material dark": {"Gruvbox Material Dark", ThemeColors{
		Primary: "#7DAEA3", Accent: "#A9B665", Warning: "#D8A657", Error: "#EA6962",
		Background: "#282828", Surface: "#32302F", SurfaceDark: "#1D2021",
		Text: "#D4BE98", TextMuted: "#928374", Border: "#45403D",
		SidebarBackground: "#1A1A1A", RailBackground: "#0F0F0F",
	}},
	"nightfox": {"Nightfox", ThemeColors{
		Primary: "#719CD6", Accent: "#81B29A", Warning: "#DBC074", Error: "#C94F6D",
		Background: "#192330", Surface: "#212E3F", SurfaceDark: "#131A24",
		Text: "#CDCECF", TextMuted: "#738091", Border: "#2B3B51",
		SidebarBackground: "#0E141C", RailBackground: "#080B11",
	}},
	"carbonfox": {"Carbonfox", ThemeColors{
		Primary: "#78A9FF", Accent: "#25BE6A", Warning: "#FF832B", Error: "#EE5396",
		Background: "#161616", Surface: "#262626", SurfaceDark: "#0C0C0C",
		Text: "#F2F4F8", TextMuted: "#7B7C7E", Border: "#393939",
		SidebarBackground: "#242424", RailBackground: "#0C0C0C",
	}},
	"melange dark": {"Melange Dark", ThemeColors{
		Primary: "#A3A9CE", Accent: "#85B695", Warning: "#EBC06D", Error: "#D47766",
		Background: "#292522", Surface: "#34302C", SurfaceDark: "#1F1B18",
		Text: "#ECE1D7", TextMuted: "#867462", Border: "#403A36",
		SidebarBackground: "#16130F", RailBackground: "#0D0B09",
	}},
	"vesper": {"Vesper", ThemeColors{
		Primary: "#FFC799", Accent: "#99FFE4", Warning: "#FFC799", Error: "#FF8080",
		Background: "#101010", Surface: "#1C1C1C", SurfaceDark: "#0A0A0A",
		Text: "#FFFFFF", TextMuted: "#8B8B8B", Border: "#2A2A2A",
		SidebarBackground: "#1F1F1F", RailBackground: "#000000",
	}},
	"flexoki dark": {"Flexoki Dark", ThemeColors{
		Primary: "#4385BE", Accent: "#879A39", Warning: "#D0A215", Error: "#D14D41",
		Background: "#100F0F", Surface: "#1C1B1A", SurfaceDark: "#0A0908",
		Text: "#CECDC3", TextMuted: "#878580", Border: "#282726",
		SidebarBackground: "#201F1E", RailBackground: "#0A0908",
	}},
	"modus vivendi": {"Modus Vivendi", ThemeColors{
		Primary: "#2FAFFF", Accent: "#44BC44", Warning: "#FEC43F", Error: "#FF5F59",
		Background: "#000000", Surface: "#1E1E1E", SurfaceDark: "#0A0A0A",
		Text: "#FFFFFF", TextMuted: "#989898", Border: "#303030",
		SidebarBackground: "#1A1A1A", RailBackground: "#0A0A0A",
	}},
	"night owl": {"Night Owl", ThemeColors{
		Primary: "#82AAFF", Accent: "#ADDB67", Warning: "#ECC48D", Error: "#EF5350",
		Background: "#011627", Surface: "#0E293F", SurfaceDark: "#010E1A",
		Text: "#D6DEEB", TextMuted: "#5F7E97", Border: "#1D3B53",
		SidebarBackground: "#0E293F", RailBackground: "#010E1A",
	}},
	"poimandres": {"Poimandres", ThemeColors{
		Primary: "#89DDFF", Accent: "#5DE4C7", Warning: "#FFFAC2", Error: "#D0679D",
		Background: "#1B1E28", Surface: "#252B37", SurfaceDark: "#171922",
		Text: "#E4F0FB", TextMuted: "#767C9D", Border: "#303340",
		SidebarBackground: "#0F1118", RailBackground: "#08090D",
	}},
	"ayu dark": {"Ayu Dark", ThemeColors{
		Primary: "#39BAE6", Accent: "#C2D94C", Warning: "#FFB454", Error: "#FF3333",
		Background: "#0B0E14", Surface: "#131721", SurfaceDark: "#06080D",
		Text: "#BFBDB6", TextMuted: "#565B66", Border: "#1B1F28",
		SidebarBackground: "#1B202B", RailBackground: "#0B0E14",
	}},
	"kanagawa dragon": {"Kanagawa Dragon", ThemeColors{
		Primary: "#8BA4B0", Accent: "#8A9A7B", Warning: "#C4B28A", Error: "#C4746E",
		Background: "#181616", Surface: "#282423", SurfaceDark: "#0D0C0C",
		Text: "#C5C9C5", TextMuted: "#737C73", Border: "#2D2C29",
		SidebarBackground: "#282423", RailBackground: "#100E0E",
	}},
	"rosé pine dawn": {"Rosé Pine Dawn", ThemeColors{
		Primary: "#56949F", Accent: "#286983", Warning: "#EA9D34", Error: "#B4637A",
		Background: "#FAF4ED", Surface: "#FFFAF3", SurfaceDark: "#F2E9E1",
		Text: "#575279", TextMuted: "#797593", Border: "#DFDAD9",
		SidebarBackground: "#575279", SidebarText: "#FAF4ED", SidebarTextMuted: "#9893A5",
		RailBackground: "#423F5C",
	}},
	"everforest light": {"Everforest Light", ThemeColors{
		Primary: "#3A94C5", Accent: "#8DA101", Warning: "#DFA000", Error: "#F85552",
		Background: "#FDF6E3", Surface: "#F4F0D9", SurfaceDark: "#EFEBD4",
		Text: "#5C6A72", TextMuted: "#939F91", Border: "#E0DCC7",
		SidebarBackground: "#343F44", SidebarText: "#D3C6AA", SidebarTextMuted: "#859289",
		RailBackground: "#232A2E",
	}},
	"flexoki light": {"Flexoki Light", ThemeColors{
		Primary: "#205EA6", Accent: "#66800B", Warning: "#AD8301", Error: "#AF3029",
		Background: "#FFFCF0", Surface: "#F2F0E5", SurfaceDark: "#E6E4D9",
		Text: "#100F0F", TextMuted: "#6F6E69", Border: "#DAD8CE",
		SidebarBackground: "#100F0F", SidebarText: "#CECDC3", SidebarTextMuted: "#878580",
		RailBackground: "#1C1B1A",
	}},
	"modus operandi": {"Modus Operandi", ThemeColors{
		Primary: "#0031A9", Accent: "#006800", Warning: "#6F5500", Error: "#A60000",
		Background: "#FFFFFF", Surface: "#F2F2F2", SurfaceDark: "#E5E5E5",
		Text: "#000000", TextMuted: "#595959", Border: "#D0D0D0",
		SidebarBackground: "#1E1E1E", SidebarText: "#FFFFFF", SidebarTextMuted: "#989898",
		RailBackground: "#000000",
	}},
	"kanagawa lotus": {"Kanagawa Lotus", ThemeColors{
		Primary: "#4D699B", Accent: "#6F894E", Warning: "#C4781E", Error: "#C84053",
		Background: "#F2ECBC", Surface: "#E7DBA0", SurfaceDark: "#DCD5AC",
		Text: "#545464", TextMuted: "#8A8980", Border: "#DCD5AC",
		SidebarBackground: "#1F1F28", SidebarText: "#DCD7BA", SidebarTextMuted: "#8A8980",
		RailBackground: "#16161D",
	}},
	"papercolor light": {"PaperColor Light", ThemeColors{
		Primary: "#0087AF", Accent: "#008700", Warning: "#D75F00", Error: "#AF0000",
		Background: "#EEEEEE", Surface: "#E4E4E4", SurfaceDark: "#D0D0D0",
		Text: "#444444", TextMuted: "#878787", Border: "#D0D0D0",
		SidebarBackground: "#1C1C1C", SidebarText: "#E4E4E4", SidebarTextMuted: "#878787",
		RailBackground: "#080808",
	}},
	"aubergine": {"Aubergine", ThemeColors{
		// Slack's classic dark-aubergine sidebar on a white message pane.
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E", Error: "#E01E5A",
		Background: "#FFFFFF", Surface: "#F8F8F8", SurfaceDark: "#F0F0F0",
		Text: "#1D1C1D", TextMuted: "#616061", Border: "#DDDDDD",
		SidebarBackground: "#4D394B", SidebarText: "#FFFFFF", SidebarTextMuted: "#BAA2B8",
		RailBackground: "#3E313C",
	}},
	"ochin": {"Ochin", ThemeColors{
		// Popular slate-blue Slack sidebar theme, light message pane.
		Primary: "#4A90D9", Accent: "#4CA64C", Warning: "#ECB22E", Error: "#EB4D5C",
		Background: "#FFFFFF", Surface: "#F6F7F8", SurfaceDark: "#EDEFF1",
		Text: "#1D1C1D", TextMuted: "#616061", Border: "#DCDFE3",
		SidebarBackground: "#303E4D", SidebarText: "#DAE3ED", SidebarTextMuted: "#8B97A5",
		RailBackground: "#232D38",
	}},
	"choco mint": {"Choco Mint", ThemeColors{
		// Dark-chocolate sidebar with a mint accent on a warm light pane.
		Primary: "#16A085", Accent: "#16C098", Warning: "#D9A441", Error: "#C0563B",
		Background: "#FAF7F2", Surface: "#F0EBE3", SurfaceDark: "#E6DFD5",
		Text: "#2B2017", TextMuted: "#6E6258", Border: "#DDD4C8",
		SidebarBackground: "#25190F", SidebarText: "#E8E2DB", SidebarTextMuted: "#A8998C",
		RailBackground: "#1A1109",
	}},
	"mocha": {"Mocha", ThemeColors{
		// Warm coffee sidebar on a light pane.
		Primary: "#A0522D", Accent: "#C58A5E", Warning: "#D89A4E", Error: "#B5453B",
		Background: "#F7F3F0", Surface: "#EDE7E2", SurfaceDark: "#E2DAD3",
		Text: "#2A2220", TextMuted: "#6B5E58", Border: "#DAD0C8",
		SidebarBackground: "#2E2422", SidebarText: "#E6DCD6", SidebarTextMuted: "#A38F86",
		RailBackground: "#211A18",
	}},
	"nocturne": {"Nocturne", ThemeColors{
		// Deep blue-black dark theme; sidebar raised for separation.
		Primary: "#4F9CD9", Accent: "#4FB477", Warning: "#E0B14F", Error: "#E0556B",
		Background: "#0F1620", Surface: "#18222F", SurfaceDark: "#0A0F16",
		Text: "#C3CCD9", TextMuted: "#66717F", Border: "#1F2C3A",
		SidebarBackground: "#1A2736", RailBackground: "#0A0F16",
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

// LoadCustomThemes scans the top level of fsys for .toml theme files and
// registers them.
func LoadCustomThemes(fsys fs.FS) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return // directory doesn't exist or can't be read — silently skip
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		data, err := fs.ReadFile(fsys, entry.Name())
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
