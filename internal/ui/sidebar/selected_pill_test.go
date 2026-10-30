package sidebar

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestSelectedPill_ThemedCursorRowIsAFilledPill(t *testing.T) {
	saved := snapshotStyleGlobals()
	defer saved.restore()
	styles.RegisterCustomTheme("pilltest", styles.ThemeColors{
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E",
		Error: "#C01343", Background: "#FFFFFF", Surface: "#F8F8F8",
		SurfaceDark: "#F2F2F2", Text: "#1D1C1D", TextMuted: "#616061",
		Border: "#DDDDDD", SidebarSelectedBg: "#333133", SidebarSelectedFg: "#F8F8F8",
	})
	styles.Apply("pilltest", config.Theme{})

	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SelectByID("C1")
	view := m.View(10, 30)

	row := rowFor(t, view, "deploys")
	if strings.Contains(row, "▌") {
		t.Errorf("pill row still draws the ▌ marker:\n%q", row)
	}
	cells := paintCells(row)
	const pillBg = "48;2;51;49;51"
	runs := bgRuns(cells, pillBg)
	if len(runs) != 1 || len([]rune(runs[0])) != 26 || !strings.Contains(runs[0], "# deploys") {
		t.Errorf("want one 26-cell pill holding the name, got %q:\n%q", runs, row)
	}
	if len(cells) != 28 || cells[0].bg == pillBg || cells[27].bg == pillBg {
		t.Errorf("pill must be inset one cell inside the 28-cell row:\n%q", row)
	}
	if strings.Contains(rowFor(t, view, "general"), pillBg) {
		t.Errorf("non-cursor row painted with the pill")
	}
}

func TestSelectedPill_UnthemedKeepsMarker(t *testing.T) {
	saved := snapshotStyleGlobals()
	defer saved.restore()
	styles.Apply("dark", config.Theme{})

	m := New([]ChannelItem{{ID: "C1", Name: "deploys", Type: "channel"}})
	m.SelectByID("C1")
	if row := rowFor(t, m.View(10, 30), "deploys"); !strings.Contains(row, "▌") {
		t.Errorf("theme without sidebar_selected_bg lost the ▌ marker:\n%q", row)
	}
}

func TestRightAlignBadges_BadgeEndsTheRow(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 3}}
	})
	m.ToggleCollapse("Channels")
	plain := func(row string) string { return ansiRe.ReplaceAllString(row, "") }

	if got := plain(rowFor(t, m.View(10, 30), "deploys")); !strings.Contains(got, "# deploys  3 ") {
		t.Errorf("want the badge right after the name with the option off, got %q", got)
	}
	m.SetRightAlignBadges(true)
	got := plain(rowFor(t, m.View(10, 30), "deploys"))
	if len([]rune(got)) != 28 || !strings.HasSuffix(got, " 3   ") {
		t.Errorf("want the badge at the row's end (one spare cell, then padding), got %q", got)
	}
}
