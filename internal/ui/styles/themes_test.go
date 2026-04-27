package styles

import (
	"os"
	"path/filepath"
	"testing"

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
	if Primary != "#AABBCC" {
		t.Errorf("expected custom primary #AABBCC, got %s", string(Primary))
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
