package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sgrCodes is the ANSI styling of s with the text removed, so two rows
// can be compared for style without their labels matching.
func sgrCodes(s string) string {
	return strings.Join(regexp.MustCompile(`\x1b\[[0-9;]*m`).FindAllString(s, -1), "")
}

// rowBold reports whether s carries a bold SGR, either on its own
// (\x1b[1m) or combined with a color (\x1b[1;...).
func rowBold(s string) bool {
	return strings.Contains(s, "\x1b[1m") || strings.Contains(s, "\x1b[1;")
}

// The Drafts row sits directly under Threads and lines up with it and
// with Activity, whichever of the three the cursor is on.
func TestDraftsRow_SitsUnderThreadsAndAligns(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	plain := ansi.Strip(m.View(12, 30))
	threads := strings.Index(plain, "Threads")
	drafts := strings.Index(plain, "Drafts")
	activity := strings.Index(plain, "Activity")
	if threads < 0 || drafts < 0 || activity < 0 || !(threads < drafts && drafts < activity) {
		t.Fatalf("want Threads, then Drafts, then Activity:\n%s", plain)
	}
	// One MoveDown from the default Threads row lands on Drafts.
	m.MoveDown()
	if !m.IsDraftsSelected() {
		t.Fatal("one step down from Threads should select Drafts")
	}
	for _, where := range []string{"on Drafts", "on Activity", "on a section header"} {
		view := m.View(20, 30)
		th, dr, ac := labelColumn(t, view, "Threads"), labelColumn(t, view, "Drafts"), labelColumn(t, view, "Activity")
		if th != dr || dr != ac {
			t.Errorf("cursor %s: Threads %d, Drafts %d, Activity %d", where, th, dr, ac)
		}
		m.MoveDown()
	}
}

func TestDraftsRow_CountBadge(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	plain := func() string { return ansi.Strip(rowFor(t, m.View(10, 30), "Drafts")) }
	if strings.Contains(plain(), "•") {
		t.Fatalf("zero drafts should hide the badge, got %q", plain())
	}
	m.SetDraftsCount(4)
	if !strings.Contains(plain(), "Drafts 4") {
		t.Fatalf("Drafts row = %q, want the plain count", plain())
	}
	if strings.Contains(plain(), "•") {
		t.Fatalf("Drafts count must not use the unread marker, got %q", plain())
	}
	m.SetDraftsCount(0)
	if strings.Contains(plain(), "•") {
		t.Fatalf("clearing the count should hide the badge, got %q", plain())
	}
	if m.DraftsCount() != 0 {
		t.Fatalf("DraftsCount = %d, want 0", m.DraftsCount())
	}
}

func TestDraftsRow_ActiveIndicatorWhenCursorIsElsewhere(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if strings.Contains(ansi.Strip(rowFor(t, m.View(10, 30), "Drafts")), "▌") {
		t.Fatal("the Drafts row shows a bar before it is the active view")
	}
	m.SetDraftsCount(46)
	m.SetDraftsActive(true)
	got := ansi.Strip(rowFor(t, m.View(10, 30), "Drafts"))
	if !strings.Contains(got, "▌") {
		t.Fatalf("active Drafts row = %q, want the ▌ indicator", got)
	}
	if !strings.Contains(got, "Drafts 46") || strings.Contains(got, "•") {
		t.Fatalf("active Drafts row = %q, want the plain count and no unread marker", got)
	}
}

// A draft count is inventory, not an unread. The resting row stays in
// the same style as Activity with nothing unread, while an unread
// Threads row beside it keeps its bold accent badge. The active-view
// bar is a separate style and still matches Activity's.
func TestDraftsRow_CountDoesNotHighlight(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetDraftsCount(46)
	m.SetThreadsUnreadCount(3)
	m.MoveDown() // Drafts
	m.MoveDown() // Activity
	m.MoveDown() // section header, so none of the three rows is selected

	view := m.View(16, 40)
	drafts := rowFor(t, view, "Drafts")
	activity := rowFor(t, view, "Activity")
	threads := rowFor(t, view, "Threads")
	if plain := ansi.Strip(drafts); !strings.Contains(plain, "Drafts 46") || strings.Contains(plain, "•") {
		t.Fatalf("Drafts row = %q, want a plain count and no unread marker", plain)
	}
	if sgrCodes(drafts) != sgrCodes(activity) {
		t.Fatalf("draft count changed the row style\ndrafts:   %q\nactivity: %q", drafts, activity)
	}
	if rowBold(drafts) {
		t.Fatalf("draft count bolded the row:\n%q", drafts)
	}
	if !rowBold(threads) || !strings.Contains(ansi.Strip(threads), "•3") {
		t.Fatalf("unread Threads row = %q, want it still bold with •3", threads)
	}

	m.SetDraftsActive(true)
	m.SetActivityActive(true)
	view = m.View(16, 40)
	drafts = rowFor(t, view, "Drafts")
	activity = rowFor(t, view, "Activity")
	if sgrCodes(drafts) != sgrCodes(activity) {
		t.Fatalf("active Drafts styled differently from active Activity\ndrafts:   %q\nactivity: %q", drafts, activity)
	}
	if plain := ansi.Strip(drafts); !strings.Contains(plain, "▌") || !strings.Contains(plain, "Drafts 46") || strings.Contains(plain, "•") {
		t.Fatalf("active Drafts row = %q, want the bar, the plain count, and no unread marker", plain)
	}

	// right_align_badges turns an unread count into a mention badge.
	// A draft count stays inline and plain.
	m.SetDraftsActive(false)
	m.SetActivityActive(false)
	m.SetActivityUnreadCount(2)
	m.SetRightAlignBadges(true)
	view = m.View(16, 40)
	drafts = rowFor(t, view, "Drafts")
	activity = rowFor(t, view, "Activity")
	if plain := ansi.Strip(drafts); !strings.Contains(plain, "Drafts 46") || strings.Contains(plain, "•") || rowBold(drafts) {
		t.Fatalf("right-aligned badges restyled Drafts: %q", drafts)
	}
	if strings.Contains(ansi.Strip(activity), "•") {
		t.Fatalf("precondition: Activity's unread count should leave the inline marker, got %q", ansi.Strip(activity))
	}

	m.MoveUp() // Activity
	m.MoveUp() // Drafts
	if !m.IsDraftsSelected() {
		t.Fatal("cursor should be on Drafts")
	}
	drafts = rowFor(t, m.View(16, 40), "Drafts")
	if !rowBold(drafts) || !strings.Contains(ansi.Strip(drafts), "Drafts 46") || strings.Contains(ansi.Strip(drafts), "•") {
		t.Fatalf("selected Drafts row = %q, want the selection highlight and the plain count", drafts)
	}
}

func TestSelectDraftsRow_SurvivesARefresh(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SelectDraftsRow()
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}, {ID: "C2", Name: "random", Type: "channel"}})
	if !m.IsDraftsSelected() {
		t.Fatal("a list refresh should keep the cursor on Drafts")
	}
}
