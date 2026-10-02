package userprofile

import (
	"strconv"
	"time"
)

// formatLocalTime renders the author's wall clock and its offset from
// this machine's local time, given tzOffset (seconds east of UTC) and
// an optional zone abbreviation. now supplies both the instant and,
// via its own zone, "your" offset to compare against.
func formatLocalTime(now time.Time, tzOffset int, abbrev string) string {
	loc := time.FixedZone(abbrev, tzOffset)
	clock := now.In(loc).Format("3:04 PM")

	_, localOffset := now.Zone()
	delta := formatOffsetDelta(tzOffset - localOffset)

	if abbrev == "" {
		return clock + " (" + delta + ")"
	}
	return clock + " (" + abbrev + ", " + delta + ")"
}

// formatOffsetDelta renders the difference between the author's offset
// and "your" offset: "same time as you" when equal, else a signed
// duration like "\u22123h" or "+9h30m" followed by " from you".
func formatOffsetDelta(deltaSeconds int) string {
	if deltaSeconds == 0 {
		return "same time as you"
	}
	sign := "+"
	d := deltaSeconds
	if d < 0 {
		sign = "\u2212"
		d = -d
	}
	h := d / 3600
	m := (d % 3600) / 60
	s := strconv.Itoa(h) + "h"
	if m != 0 {
		s += strconv.Itoa(m) + "m"
	}
	return sign + s + " from you"
}
