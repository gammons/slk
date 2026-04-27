package styles

import (
	"testing"

	"github.com/gammons/slk/internal/config"
)

func TestApplyDarkDefaults(t *testing.T) {
	Apply("dark", config.Theme{})
	if Primary != "#4A9EFF" {
		t.Errorf("expected dark primary #4A9EFF, got %s", string(Primary))
	}
	if Background != "#1A1A2E" {
		t.Errorf("expected dark background #1A1A2E, got %s", string(Background))
	}
}

func TestApplyLightDefaults(t *testing.T) {
	Apply("light", config.Theme{})
	if Primary != "#0366D6" {
		t.Errorf("expected light primary #0366D6, got %s", string(Primary))
	}
	if Background != "#FFFFFF" {
		t.Errorf("expected light background #FFFFFF, got %s", string(Background))
	}
	Apply("dark", config.Theme{})
}

func TestApplyDracula(t *testing.T) {
	Apply("dracula", config.Theme{})
	if Primary != "#BD93F9" {
		t.Errorf("expected dracula primary #BD93F9, got %s", string(Primary))
	}
	Apply("dark", config.Theme{})
}

func TestApplyOverrides(t *testing.T) {
	Apply("dark", config.Theme{Primary: "#FF0000"})
	if Primary != "#FF0000" {
		t.Errorf("expected overridden primary #FF0000, got %s", string(Primary))
	}
	if Accent != "#50C878" {
		t.Errorf("expected dark accent #50C878, got %s", string(Accent))
	}
	Apply("dark", config.Theme{})
}

func TestApplyUnknownPresetFallsToDark(t *testing.T) {
	Apply("nonexistent", config.Theme{})
	if Primary != "#4A9EFF" {
		t.Errorf("expected dark fallback primary #4A9EFF, got %s", string(Primary))
	}
}

func TestApplyCaseInsensitive(t *testing.T) {
	Apply("Dracula", config.Theme{})
	if Primary != "#BD93F9" {
		t.Errorf("expected dracula primary #BD93F9, got %s", string(Primary))
	}
	Apply("dark", config.Theme{})
}

func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) < 12 {
		t.Errorf("expected at least 12 built-in themes, got %d", len(names))
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, expected := range []string{"Dark", "Light", "Dracula", "Nord"} {
		if !found[expected] {
			t.Errorf("expected theme %q in list", expected)
		}
	}
}
