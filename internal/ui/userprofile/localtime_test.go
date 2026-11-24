package userprofile

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/messages"
)

func TestFormatLocalTime(t *testing.T) {
	now := time.Date(2026, 9, 30, 18, 42, 0, 0, time.FixedZone("EDT", -4*3600))

	cases := []struct {
		name     string
		tzOffset int
		abbrev   string
		want     string
	}{
		{"west coast (PDT)", -7 * 3600, "PDT", "3:42 PM (PDT, \u22123h from you)"},
		{"half-hour offset ahead (IST)", 19800, "IST", "4:12 AM (IST, +9h30m from you)"},
		{"same offset as you (EDT)", -4 * 3600, "EDT", "6:42 PM (EDT, same time as you)"},
		{"numeric offset, no abbrev", -3 * 3600, "", "7:42 PM (+1h from you)"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatLocalTime(now, c.tzOffset, c.abbrev)
			if got != c.want {
				t.Errorf("formatLocalTime(now, %d, %q) = %q, want %q", c.tzOffset, c.abbrev, got, c.want)
			}
		})
	}
}

// The local-time clock follows appearance.timestamp_format, the layout
// messages.TimestampLayout hands out — not a hardcoded "3:04 PM".
func TestFormatLocalTimeFollowsTimestampFormat(t *testing.T) {
	previous := messages.TimestampLayout()
	messages.SetDateTokenTimeLayout("15:04")
	t.Cleanup(func() { messages.SetDateTokenTimeLayout(previous) })

	now := time.Date(2026, 9, 30, 18, 42, 0, 0, time.FixedZone("EDT", -4*3600))
	got := formatLocalTime(now, -7*3600, "PDT")
	want := "15:42 (PDT, \u22123h from you)" // 18:42 EDT is 15:42 PDT
	if got != want {
		t.Errorf("formatLocalTime with layout 15:04 = %q, want %q", got, want)
	}
}
