package styles

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/lipgloss/v2"
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

	LoadCustomThemes(dir)

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
	LoadCustomThemes("/tmp/nonexistent-theme-dir-12345")
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
