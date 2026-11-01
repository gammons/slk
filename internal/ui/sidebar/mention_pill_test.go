package sidebar

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/styles"
)

func mentionPillFixture() *Model {
	var items []ChannelItem
	for i := 0; i < 20; i++ {
		items = append(items, ChannelItem{ID: fmt.Sprintf("C%02d", i), Name: fmt.Sprintf("chan%02d", i), Type: "channel", Section: "Work"})
	}
	m := New(items)
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C01": {HasUnread: true, MentionCount: 1},
			"C18": {HasUnread: true, MentionCount: 2},
		}
	})
	m.SetMentionPills(true)
	return &m
}

func TestMentionPill_PointsAtScrolledAwayMentions(t *testing.T) {
	m := mentionPillFixture()
	m.SelectByID("C10")
	m.View(8, 30)
	m.MoveUp()
	lines := strings.Split(m.View(8, 30), "\n")
	if !strings.Contains(lines[0], "↑ Unread mentions") {
		t.Errorf("top row lacks the ↑ pill:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[7], "↓ Unread mentions") {
		t.Errorf("bottom row lacks the ↓ pill:\n%s", strings.Join(lines, "\n"))
	}

	m.ClickAt(7)
	view := m.View(8, 30)
	if got := m.SelectedID(); got != "C18" {
		t.Fatalf("↓ pill click selected %q, want C18", got)
	}
	if strings.Contains(view, "↓ Unread mentions") {
		t.Errorf("↓ pill still shown with the mention in view:\n%s", view)
	}
}

func TestMentionPill_UsesTheMentionBadgeStyle(t *testing.T) {
	m := mentionPillFixture()
	m.SelectByID("C10")
	m.View(8, 30)
	m.MoveUp()
	top := strings.Split(m.View(8, 30), "\n")[0]
	if want := styles.MentionBadgeStyle().Bold(true).Render("↑ Unread mentions"); !strings.Contains(top, want) {
		t.Errorf("pill not drawn in the mention badge style:\n%q\nwant it to contain\n%q", top, want)
	}
}

func TestMentionPill_StepsAsideForTheCursorRow(t *testing.T) {
	m := mentionPillFixture()
	m.SelectByID("C10")
	m.View(8, 30)
	for i := 0; i < 7; i++ {
		m.MoveUp()
	}
	lines := strings.Split(m.View(8, 30), "\n")
	if !strings.Contains(lines[0], "chan03") || !strings.Contains(lines[1], "↑ Unread mentions") {
		t.Errorf("want the cursor row on top and the pill below it:\n%s", strings.Join(lines, "\n"))
	}
}

func TestMentionPill_OffByDefault(t *testing.T) {
	m := mentionPillFixture()
	m.SetMentionPills(false)
	m.SelectByID("C10")
	if view := m.View(8, 30); strings.Contains(view, "Unread mentions") {
		t.Errorf("pill shown with mention_pills off:\n%s", view)
	}
}

func TestMentionPill_NoneWhenMentionsVisible(t *testing.T) {
	m := mentionPillFixture()
	if view := m.View(40, 30); strings.Contains(view, "Unread mentions") {
		t.Errorf("pill shown though every mention is on screen:\n%s", view)
	}
}
