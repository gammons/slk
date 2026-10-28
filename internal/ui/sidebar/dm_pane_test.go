package sidebar

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func dmPaneModel(dms int) *Model {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "alerts", Type: "channel", Section: "Alerts", SectionOrder: 1},
	}
	for i := 1; i <= dms; i++ {
		items = append(items, ChannelItem{ID: fmt.Sprintf("D%d", i), Name: fmt.Sprintf("dm%d", i), Type: "dm"})
	}
	m := New(items)
	m.ToggleCollapse("Channels")
	return &m
}

func TestDMPane_OffByDefault(t *testing.T) {
	m := dmPaneModel(2)
	if got := m.DMPaneRows(30); got != 0 {
		t.Fatalf("DMPaneRows = %d without dm_pane, want 0", got)
	}
}

// TestDMPane_SplitsDirectMessagesIntoTheirOwnBox: the DM section is drawn
// by ViewSplit's second window only, and nothing else is.
func TestDMPane_SplitsDirectMessagesIntoTheirOwnBox(t *testing.T) {
	m := dmPaneModel(2)
	m.SetDMPane(true)
	if got := m.DMPaneRows(30); got != 3 {
		t.Fatalf("DMPaneRows = %d, want 3 (header + 2 DMs)", got)
	}
	top, dms := m.ViewSplit(12, 3, 30)
	top, dms = ansi.Strip(top), ansi.Strip(dms)
	for _, name := range []string{"Direct Messages", "dm1", "dm2"} {
		if !strings.Contains(dms, name) {
			t.Errorf("DM box missing %q:\n%s", name, dms)
		}
		if strings.Contains(top, name) {
			t.Errorf("channel box shows %q:\n%s", name, top)
		}
	}
	for _, name := range []string{"Alerts", "alerts", "Channels", "general"} {
		if !strings.Contains(top, name) {
			t.Errorf("channel box missing %q:\n%s", name, top)
		}
	}
	if n := len(strings.Split(top, "\n")); n != 12 {
		t.Errorf("channel box has %d lines, want 12", n)
	}
	if n := len(strings.Split(dms, "\n")); n != 3 {
		t.Errorf("DM box has %d lines, want 3", n)
	}
}

// TestDMPane_MovementStaysInItsBox: with the split drawn, j/k and gg/G
// move within the box holding the cursor; FocusDMPane / FocusChannelPane
// switch boxes.
func TestDMPane_MovementStaysInItsBox(t *testing.T) {
	m := dmPaneModel(2)
	m.SetDMPane(true)
	m.ViewSplit(12, 3, 30)

	m.GoToBottom()
	if it, ok := m.SelectedItem(); !ok || it.ID != "C1" {
		t.Fatalf("G in the channel box selected %+v, want general (C1), its last row", it)
	}
	m.MoveDown()
	if it, _ := m.SelectedItem(); it.ID != "C1" {
		t.Errorf("j past the channel box's last row moved to %+v", it)
	}

	if !m.FocusDMPane() {
		t.Fatal("FocusDMPane reported no DM box")
	}
	m.ViewSplit(12, 3, 30)
	if it, ok := m.SelectedItem(); !ok || it.ID != "D1" || !m.CursorInDMPane() {
		t.Fatalf("FocusDMPane selected %+v (in DM box: %v), want dm1", it, m.CursorInDMPane())
	}
	m.MoveUp() // the DM header
	m.MoveUp()
	if m.cursor != m.dmNavStart {
		t.Errorf("k above the DM header left the DM box (cursor %d, DM header %d)", m.cursor, m.dmNavStart)
	}
	m.GoToBottom()
	if it, _ := m.SelectedItem(); it.ID != "D2" {
		t.Errorf("G in the DM box selected %+v, want dm2", it)
	}
}

// TestDMPane_FocusRemembersEachBox: switching boxes returns to the row
// each box held last.
func TestDMPane_FocusRemembersEachBox(t *testing.T) {
	m := dmPaneModel(2)
	m.SetDMPane(true)
	m.ViewSplit(12, 3, 30)
	m.GoToBottom() // general
	m.FocusDMPane()
	m.MoveDown() // dm2
	m.FocusChannelPane()
	if it, _ := m.SelectedItem(); it.ID != "C1" {
		t.Errorf("FocusChannelPane returned to %+v, want general", it)
	}
	m.FocusDMPane()
	if it, _ := m.SelectedItem(); it.ID != "D2" {
		t.Errorf("FocusDMPane returned to %+v, want dm2", it)
	}
}

func TestDMPane_FocusNeedsTheSplitDrawn(t *testing.T) {
	m := dmPaneModel(2)
	m.SetDMPane(true)
	m.View(30, 30)
	if m.FocusDMPane() {
		t.Error("FocusDMPane moved into a DM box that was not drawn")
	}
}

func TestDMPane_ClickAtDM(t *testing.T) {
	m := dmPaneModel(2)
	m.SetDMPane(true)
	m.ViewSplit(12, 3, 30)
	if it, ok := m.ClickAtDM(1); !ok || it.ID != "D1" {
		t.Fatalf("ClickAtDM(1) = %+v, %v; want dm1", it, ok)
	}
	for y := 0; y < 12; y++ {
		if it, ok := m.ClickAt(y); ok && strings.HasPrefix(it.ID, "D") {
			t.Errorf("ClickAt(%d) in the channel box reached DM %s", y, it.ID)
		}
	}
}

// TestDMPane_ScrollsOnItsOwn: the DM box keeps the selected DM visible and
// scrolls without moving the channel box.
func TestDMPane_ScrollsOnItsOwn(t *testing.T) {
	m := dmPaneModel(6)
	m.SetDMPane(true)
	m.ViewSplit(4, 3, 30)

	m.FocusDMPane()
	m.GoToBottom()
	_, dms := m.ViewSplit(4, 3, 30)
	if !strings.Contains(ansi.Strip(dms), "dm6") {
		t.Errorf("selected dm6 not visible in the DM box:\n%s", ansi.Strip(dms))
	}
	if m.yOffset != 0 {
		t.Errorf("selecting a DM scrolled the channel box to offset %d", m.yOffset)
	}
	if m.dmYOffset == 0 {
		t.Error("the DM box did not scroll to the selected DM")
	}

	m.ScrollDMUp(10)
	_, dms = m.ViewSplit(4, 3, 30)
	if !strings.Contains(ansi.Strip(dms), "Direct Messages") {
		t.Errorf("ScrollDMUp did not bring the DM header back:\n%s", ansi.Strip(dms))
	}
}
