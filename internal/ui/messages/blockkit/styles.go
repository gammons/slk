package blockkit

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

// Style accessors. Defined as functions (not vars) so they pick up
// theme changes — the styles package mutates its color vars on
// theme switch.

func headerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Background(styles.Background)
}

func dividerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Border).
		Background(styles.Background)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(styles.Background)
}

// controlStyle is the muted, non-interactive look for buttons,
// select menus, overflow, and other "you can see this exists but
// you can't drive it" elements. Used by Task 7 (section accessory)
// and Task 9 (actions).
func controlStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(styles.SurfaceDark)
}

// fieldLabelStyle is the bold-muted style used for field titles
// in section field grids and legacy attachment field grids. Used
// by Task 8 (section fields) and Task 12 (legacy fields).
func fieldLabelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.TextMuted).
		Background(styles.Background)
}
