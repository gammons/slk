package styles

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/config"
)

// Regression net for loading user theme files: which files register,
// which are skipped, and how missing colors are filled.

func TestSeam_CustomThemeDirectory(t *testing.T) {
	t.Cleanup(func() {
		customThemes = map[string]struct {
			Name   string
			Colors ThemeColors
		}{}
		Apply("dark", config.Theme{})
	})
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("partial.toml", "name = \"Seam Partial\"\n[colors]\nprimary = \"#010203\"\n")
	write("nameless.toml", "[colors]\nprimary = \"#FFFFFF\"\n")
	write("broken.toml", "name = \"Seam Broken\"\n[colors\n")
	write("notes.txt", "name = \"Seam Text\"\n")
	if err := os.Mkdir(filepath.Join(dir, "nested.toml"), 0o755); err != nil {
		t.Fatal(err)
	}

	loadThemesFrom(dir)

	names := ThemeNames()
	if !slices.Contains(names, "Seam Partial") {
		t.Fatalf("Seam Partial not registered; names = %v", names)
	}
	for _, skipped := range []string{"Seam Broken", "Seam Text", ""} {
		if slices.Contains(names, skipped) {
			t.Errorf("%q registered, want it skipped", skipped)
		}
	}

	Apply("dark", config.Theme{})
	darkAccent := Accent
	Apply("seam partial", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#010203")) {
		t.Errorf("primary = %v, want the file's #010203", Primary)
	}
	if !colorEqual(Accent, darkAccent) {
		t.Errorf("accent = %v, want dark's %v filled in", Accent, darkAccent)
	}
}

func TestSeam_CustomThemeMissingDirectory(t *testing.T) {
	before := ThemeNames()

	loadThemesFrom(filepath.Join(t.TempDir(), "absent"))

	if after := ThemeNames(); !slices.Equal(before, after) {
		t.Errorf("theme names changed for a missing directory: %v -> %v", before, after)
	}
}
