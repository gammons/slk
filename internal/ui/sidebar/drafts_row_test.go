package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
	if !strings.Contains(plain(), "•4") {
		t.Fatalf("Drafts row = %q, want the •4 badge", plain())
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
	m.SetDraftsActive(true)
	got := ansi.Strip(rowFor(t, m.View(10, 30), "Drafts"))
	if !strings.Contains(got, "▌") {
		t.Fatalf("active Drafts row = %q, want the ▌ indicator", got)
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
