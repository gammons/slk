package sidebar

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestRightAlignBadges_ThreadsAndActivityCounts(t *testing.T) {
	saved := snapshotStyleGlobals()
	defer saved.restore()
	styles.RegisterCustomTheme("countbadge", styles.ThemeColors{
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E",
		Error: "#C01343", Background: "#FFFFFF", Surface: "#F8F8F8",
		SurfaceDark: "#F2F2F2", Text: "#1D1C1D", TextMuted: "#616061",
		Border: "#DDDDDD", MentionBadgeBg: "#C01343", MentionBadgeFg: "#FFFFFF",
	})
	styles.Apply("countbadge", config.Theme{})

	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	m.SetActivityUnreadCount(120)
	plain := func(row string) string { return ansiRe.ReplaceAllString(row, "") }

	view := m.View(10, 30)
	if !strings.Contains(view, "•3") || !strings.Contains(view, "•120") {
		t.Errorf("want the •N counts with the option off:\n%s", plain(view))
	}

	m.SetRightAlignBadges(true)
	view = m.View(10, 30)
	const badgeBg = "48;2;192;19;67"
	for _, tc := range []struct{ name, suffix string }{{"Threads", " 3   "}, {"Activity", " 99+   "}} {
		row := rowFor(t, view, tc.name)
		got := plain(row)
		if len([]rune(got)) != 28 || !strings.HasSuffix(got, tc.suffix) || strings.Contains(got, "•") {
			t.Errorf("%s: want the badge at the row's end, got %q", tc.name, got)
		}
		if !strings.Contains(row, badgeBg) {
			t.Errorf("%s: badge not drawn in mention_badge_bg:\n%q", tc.name, row)
		}
	}
}
