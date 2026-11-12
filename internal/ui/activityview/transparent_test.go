package activityview

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// An unselected row's left border keeps its column, but with the
// transparent background it is a space: a "▌" drawn in the background
// colour shows in the terminal's text colour when that colour is none.
func TestTransparentUnselectedBorderIsBlank(t *testing.T) {
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", config.Theme{})
	})
	styles.SetTransparentBackground(false)
	styles.Apply("dark", config.Theme{})
	if got := borderInvisStyle().Render("x"); !strings.Contains(got, "▌") {
		t.Fatalf("opaque border lost its ▌: %q", got)
	}
	styles.SetTransparentBackground(true)
	styles.Apply("dark", config.Theme{})
	if got := borderInvisStyle().Render("x"); strings.Contains(got, "▌") {
		t.Errorf("transparent border draws ▌: %q", got)
	}
}
