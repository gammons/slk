package sidebar

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The Find a DM row sits directly above the Direct Messages header and
// answers a click even though it is not a conversation row.
func TestFindDMRowAboveDirectMessages(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm"}})
	view := ansi.Strip(m.View(20, 30))
	if !strings.Contains(view, "Find a DM") {
		t.Fatalf("sidebar does not draw the Find a DM row:\n%s", view)
	}

	findAt, headerAt := -1, -1
	for i, row := range m.cacheRows {
		if row.isFindDMRow {
			findAt = i
		}
		if row.navIdx >= 0 {
			n := m.nav[row.navIdx]
			if n.kind == navHeader && n.header == defaultDMSection {
				headerAt = i
			}
		}
	}
	if findAt < 0 || headerAt < 0 {
		t.Fatalf("find row = %d, DM header = %d", findAt, headerAt)
	}
	if findAt+1 != headerAt {
		t.Errorf("find row is %d, DM header %d; want the search row directly above", findAt, headerAt)
	}

	y := findAt - m.yOffset
	if !m.HitTestFindDM(y) {
		t.Error("the Find a DM row did not hit-test")
	}
	if m.HitTestFindDM(y + 1) {
		t.Error("the DM header hit-tested as the Find a DM row")
	}
}

// With no Direct Messages section, the finder still exists, right after
// Activity and above the first section.
func TestFindDMWithoutDMSection(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", Section: "Eng", SectionOrder: 1},
	})
	view := ansi.Strip(m.View(20, 30))
	if !strings.Contains(view, "Find a DM") {
		t.Fatalf("sidebar without DMs lost the finder:\n%s", view)
	}
	if len(m.nav) < 4 || m.nav[3].kind != navFindDM {
		t.Fatalf("nav[3] = %+v, want the finder after Activity", m.nav)
	}
}

func TestFindDMHitIgnoresMentionPill(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm"}})
	_ = m.View(20, 30)
	for y, row := range m.cacheRows {
		if !row.isFindDMRow {
			continue
		}
		m.pillTop = y - m.yOffset
		m.mentionAbove = 0
		if m.HitTestFindDM(m.pillTop) {
			t.Error("the pill row answered for the finder")
		}
		return
	}
	t.Fatal("no finder row")
}

// A workspace switch clears the live presence map but leaves cached
// presence on the DM rows; the finder falls back to those rows.
func TestPresenceByUserFallsBackToItems(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm", DMUserID: "U1", Presence: "active"}})
	m.ResetPresence()
	if got, ok := m.PresenceByUser("U1"); !ok || got != "active" {
		t.Errorf("PresenceByUser = %q, %v; want active from the item", got, ok)
	}
}

func TestDNDByUserIncludesExpiry(t *testing.T) {
	m := New(nil)
	m.UpdateDNDByUser("U1", true, time.Now().Add(time.Hour))
	if !m.DNDByUser("U1", time.Now()) {
		t.Error("DNDByUser = false before the deadline")
	}
	if m.DNDByUser("U1", time.Now().Add(2*time.Hour)) {
		t.Error("DNDByUser = true after the deadline")
	}
}
