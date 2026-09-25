package sidebar

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

func TestUpdateHuddleByUser_ShowsHeadphonesAndKeepsStatus(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "hana", Type: "dm", DMUserID: "U1", Presence: "active"}})
	m.UpdateStatusByUser("U1", ":calendar:", "In a meeting", time.Time{})
	m.UpdateHuddleByUser("U1", "in_a_huddle", time.Time{})

	if st := statusOf(t, &m, "U1"); st.Emoji != ":calendar:" || st.Huddle != "in_a_huddle" {
		t.Fatalf("status = %+v; want the custom status and the huddle", st)
	}
	row := ansiRe.ReplaceAllString(rowFor(t, m.View(10, 40), "hana"), "")
	if !strings.Contains(row, "hana "+peerstatus.HuddleGlyph) {
		t.Errorf("row %q does not show the huddle glyph after the name", row)
	}

	m.UpdateHuddleByUser("U1", "default_unset", time.Time{})
	row = ansiRe.ReplaceAllString(rowFor(t, m.View(10, 40), "hana"), "")
	if strings.Contains(row, peerstatus.HuddleGlyph) || !strings.Contains(row, "hana "+emoji.CodeMap()[":calendar:"]) {
		t.Errorf("row %q after the huddle ended; want the status emoji back", row)
	}
}
