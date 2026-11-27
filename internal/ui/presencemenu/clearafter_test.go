package presencemenu

import (
	"testing"
	"time"
)

func TestClearAfterExpiry(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("tzdata: %v", err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, berlin) }
	// Wednesday 2026-11-25 10:00.
	wed := at(2026, 11, 25, 10, 0)
	cases := []struct {
		name string
		c    ClearAfter
		now  time.Time
		want time.Time
	}{
		{"30 minutes", ClearIn30Minutes, wed, at(2026, 11, 25, 10, 30)},
		{"1 hour", ClearIn1Hour, wed, at(2026, 11, 25, 11, 0)},
		{"4 hours", ClearIn4Hours, wed, at(2026, 11, 25, 14, 0)},
		{"today", ClearToday, wed, time.Date(2026, 11, 25, 23, 59, 59, 0, berlin)},
		{"this week from Wednesday", ClearThisWeek, wed, time.Date(2026, 11, 29, 23, 59, 59, 0, berlin)},
		{"this week on Sunday is today", ClearThisWeek, at(2026, 11, 29, 9, 0), time.Date(2026, 11, 29, 23, 59, 59, 0, berlin)},
		{"this week from Monday", ClearThisWeek, at(2026, 11, 30, 9, 0), time.Date(2026, 12, 6, 23, 59, 59, 0, berlin)},
		// Clocks go back on 2026-10-25 in Berlin: end of day is still 23:59:59 local.
		{"today across DST", ClearToday, at(2026, 10, 25, 1, 0), time.Date(2026, 10, 25, 23, 59, 59, 0, berlin)},
		{"this week across DST", ClearThisWeek, at(2026, 10, 22, 9, 0), time.Date(2026, 10, 25, 23, 59, 59, 0, berlin)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Expiry(tc.now); got != tc.want.Unix() {
				t.Errorf("Expiry = %v, want %v", time.Unix(got, 0).In(berlin), tc.want)
			}
		})
	}
	if got := DontClear.Expiry(wed); got != 0 {
		t.Errorf("Don't clear = %d, want 0", got)
	}
}
