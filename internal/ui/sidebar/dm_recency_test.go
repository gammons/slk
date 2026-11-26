package sidebar

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
)

func dmRecencyModel() Model {
	m := New([]ChannelItem{
		{ID: "D-alpha", Name: "Alpha", Type: "dm"},
		{ID: "D-bravo", Name: "Bravo", Type: "dm"},
		{ID: "D-charlie", Name: "Charlie", Type: "dm"},
		{ID: "D-delta", Name: "Delta", Type: "dm"},
		{ID: "C-zulu", Name: "Zulu", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"D-alpha":   {LastReadTS: "100.000000"},
			"D-bravo":   {LastReadTS: "300.000000"},
			"D-charlie": {LastReadTS: "200.000000"},
			"C-zulu":    {LastReadTS: "400.000000"},
		}
	})
	m.SetSortDMsByRecency(true)
	return m
}

func dmNames(m Model) []string {
	var out []string
	for _, item := range m.VisibleItems() {
		if isDMRow(item) {
			out = append(out, item.Name)
		}
	}
	return out
}

func TestSortDMsByRecency(t *testing.T) {
	m := dmRecencyModel()
	got := strings.Join(dmNames(m), ",")
	// Bravo read most recently, then Charlie, then Alpha; Delta has no
	// read state, so it sorts after the DMs with one.
	if got != "Bravo,Charlie,Alpha,Delta" {
		t.Errorf("DM order = %q, want recent then unreadable", got)
	}
}

// Ordering follows the newest message, not just the last read mark: a
// DM read earlier but with a newer message comes first.
func TestSortDMsByLatestMessage(t *testing.T) {
	m := dmRecencyModel()
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"D-alpha":   {LastReadTS: "500.000000", LatestTS: "100.000000"},
			"D-bravo":   {LastReadTS: "200.000000", LatestTS: "200.000000"},
			"D-charlie": {LastReadTS: "300.000000", LatestTS: "400.000000"},
		}
	})
	m.refilter()
	if got := strings.Join(dmNames(m), ","); got != "Charlie,Bravo,Alpha,Delta" {
		t.Errorf("DM order = %q, want latest-message order", got)
	}
}

func TestUnreadDMBeforeRecentRead(t *testing.T) {
	m := dmRecencyModel()
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"D-alpha":   {LastReadTS: "100.000000"},
			"D-bravo":   {LastReadTS: "300.000000"},
			"D-charlie": {LastReadTS: "200.000000", HasUnread: true},
		}
	})
	m.refilter()
	if got := strings.Join(dmNames(m), ","); got != "Charlie,Bravo,Alpha,Delta" {
		t.Errorf("DM order = %q, want unread first then recent", got)
	}
}

func TestSortDMsByRecencyOffIsAlphabetical(t *testing.T) {
	m := dmRecencyModel()
	m.SetSortDMsByRecency(false)
	if got := strings.Join(dmNames(m), ","); got != "Alpha,Bravo,Charlie,Delta" {
		t.Errorf("DM order = %q, want alphabetical", got)
	}
}

func TestSortDMsInMixedSection(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "D-alpha", Name: "Alpha", Type: "dm", Section: "Mixed"},
		{ID: "A-bravo", Name: "Bravo", Type: "app", Section: "Mixed"},
		{ID: "D-charlie", Name: "Charlie", Type: "dm", Section: "Mixed"},
	})
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"D-alpha":   {LastReadTS: "100.000000"},
			"D-charlie": {LastReadTS: "300.000000"},
		}
	})
	m.SetSortDMsByRecency(true)
	var got []string
	for _, item := range m.VisibleItems() {
		got = append(got, item.Name)
	}
	// Only the DM slots move: Charlie takes Alpha's slot, the app stays
	// between them.
	if strings.Join(got, ",") != "Charlie,Bravo,Alpha" {
		t.Errorf("mixed section = %q, want DM slots reordered around the app", strings.Join(got, ","))
	}
}

func TestPinnedDMsKeepOrder(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "D-alpha", Name: "Alpha", Type: "dm", Section: "Pinned", ChannelOrder: 5},
		{ID: "D-bravo", Name: "Bravo", Type: "dm", Section: "Pinned", ChannelOrder: 5},
	})
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"D-alpha": {LastReadTS: "100.000000"},
			"D-bravo": {LastReadTS: "300.000000"},
		}
	})
	m.SetSortDMsByRecency(true)
	if got := strings.Join(dmNames(m), ","); got != "Alpha,Bravo" {
		t.Errorf("pinned DM order = %q, want alphabetical for equal ChannelOrder", got)
	}
}

func TestChannelsStayAlphabetical(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C-b", Name: "Bravo", Type: "channel"},
		{ID: "C-a", Name: "Alpha", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"C-a": {LastReadTS: "100.000000"},
			"C-b": {LastReadTS: "300.000000"},
		}
	})
	m.SetSortDMsByRecency(true)
	var got []string
	for _, item := range m.VisibleItems() {
		got = append(got, item.Name)
	}
	if strings.Join(got, ",") != "Alpha,Bravo" {
		t.Errorf("channel order = %q, want alphabetical", strings.Join(got, ","))
	}
}

// The Direct Messages header draws the control and answers a click on
// its cells, not on the rest of the header.
func TestDMsHeaderSortControl(t *testing.T) {
	m := dmRecencyModel()
	_ = m.View(30, 40)
	found := false
	for y, row := range m.cacheRows {
		if row.sortEnd <= row.sortStart {
			continue
		}
		found = true
		if row.navIdx < 0 {
			t.Fatalf("sort control on row %d is not on a header", y)
		}
		if m.nav[row.navIdx].header != defaultDMSection {
			t.Errorf("sort control on row %d belongs to %q, want Direct Messages", y, m.nav[row.navIdx].header)
		}
		if !strings.Contains(row.normal, "⇅") {
			t.Errorf("recency header does not draw the control: %q", row.normal)
		}
		rowY := y - m.yOffset
		if !m.HitTestSectionSort(rowY, row.sortStart) {
			t.Error("click on the control start missed")
		}
		if m.HitTestSectionSort(rowY, row.sortStart-1) {
			t.Error("click left of the control hit it")
		}
		if m.HitTestSectionSort(rowY, row.sortEnd) {
			t.Error("click past the control end hit it")
		}
	}
	if !found {
		t.Fatal("the Direct Messages header has no sort control")
	}

	if got := m.ToggleSortDMsByRecency(); got {
		t.Fatal("toggling from recency should turn it off")
	}
	_ = m.View(30, 40)
	found = false
	for _, row := range m.cacheRows {
		if row.sortEnd > row.sortStart {
			found = true
			if !strings.Contains(row.normal, "A-Z") {
				t.Errorf("alphabetical header does not draw its control: %q", row.normal)
			}
		}
	}
	if !found {
		t.Fatal("the sort control disappeared after toggling")
	}
}

// A row View() replaced with an "Unread mentions" pill must not answer
// for the header's sort control underneath it.
func TestSortControlIgnoredUnderMentionPill(t *testing.T) {
	m := dmRecencyModel()
	_ = m.View(30, 40)
	for y, row := range m.cacheRows {
		if row.sortEnd <= row.sortStart {
			continue
		}
		m.pillTop = y - m.yOffset
		m.mentionAbove = 0
		if m.HitTestSectionSort(m.pillTop, row.sortStart) {
			t.Error("the pill row still answered for the sort control")
		}
		return
	}
	t.Fatal("the Direct Messages header has no sort control")
}
