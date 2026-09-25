package sidebar

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

func TestView_DNDReplacesPresenceDotAndStatusEmojiFollowsName(t *testing.T) {
	calendar := emoji.CodeMap()[":calendar:"]
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm", DMUserID: "U1", Presence: "active"}})
	m.UpdateStatusByUser("U1", ":calendar:", "In a meeting", time.Time{})
	m.UpdateDNDByUser("U1", true, time.Time{})

	row := ansiRe.ReplaceAllString(rowFor(t, m.View(10, 40), "alice"), "")
	if !strings.Contains(row, peerstatus.DNDGlyph) {
		t.Errorf("row %q has no DND glyph", row)
	}
	if strings.Contains(row, "●") {
		t.Errorf("row %q still shows the presence dot; DND must replace it", row)
	}
	if !strings.Contains(row, "alice "+calendar) {
		t.Errorf("row %q does not show the status emoji after the name", row)
	}

	m.UpdateDNDByUser("U1", false, time.Time{})
	m.UpdateStatusByUser("U1", "", "", time.Time{})
	row = ansiRe.ReplaceAllString(rowFor(t, m.View(10, 40), "alice"), "")
	if strings.Contains(row, peerstatus.DNDGlyph) || strings.Contains(row, calendar) {
		t.Errorf("row %q still shows cleared status", row)
	}
	if !strings.Contains(row, "●") {
		t.Errorf("row %q lost the presence dot after DND ended", row)
	}
}

// The status emoji is charged to the name budget, so a long name
// truncates further instead of the emoji wrapping the row onto a second
// line.
func TestView_StatusEmojiDoesNotWrapALongName(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz"
	m := New([]ChannelItem{{ID: "D1", Name: long, Type: "dm", DMUserID: "U1"}})
	before := m.View(10, 24)

	m.UpdateStatusByUser("U1", ":calendar:", "", time.Time{})
	after := m.View(10, 24)

	if strings.Count(after, "\n") != strings.Count(before, "\n") {
		t.Fatalf("status emoji changed the sidebar's line count:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	row := ansiRe.ReplaceAllString(rowFor(t, after, long[:5]), "")
	if !strings.Contains(row, emoji.CodeMap()[":calendar:"]) {
		t.Errorf("row %q lost the status emoji to truncation", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("row %q did not truncate the name to make room", row)
	}
}
