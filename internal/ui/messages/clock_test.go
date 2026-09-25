// internal/ui/messages/clock_test.go
package messages

import (
	"testing"
	"time"
)

// fixedClock is the instant every golden and clock-dependent test
// anchors to: Sunday 2026-03-15 12:00 UTC.
func fixedClock() time.Time {
	return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
}

func TestSetNowFunc_TodayAndYesterday(t *testing.T) {
	SetNowFunc(fixedClock)
	t.Cleanup(func() { SetNowFunc(nil) })

	if got := FormatDateSeparator("2026-03-15"); got != "Today" {
		t.Errorf("2026-03-15 = %q, want \"Today\"", got)
	}
	if got := FormatDateSeparator("2026-03-14"); got != "Yesterday" {
		t.Errorf("2026-03-14 = %q, want \"Yesterday\"", got)
	}
}

func TestSetNowFunc_WeekdayWithinAWeek(t *testing.T) {
	SetNowFunc(fixedClock)
	t.Cleanup(func() { SetNowFunc(nil) })

	// 2026-03-11 is 4 days before 2026-03-15, so days<7 -> weekday name.
	if got := FormatDateSeparator("2026-03-11"); got != "Wednesday" {
		t.Errorf("2026-03-11 = %q, want \"Wednesday\"", got)
	}
}

func TestSetNowFunc_NilRevertsToTimeNow(t *testing.T) {
	SetNowFunc(fixedClock)
	SetNowFunc(nil)

	// Local, not UTC: FormatDateSeparator derives "today" from the
	// calendar fields of the clock it reads, and time.Now() carries the
	// local zone. Formatting in UTC would label the current day
	// "Yesterday" for the hours a zone east of UTC is ahead of it.
	today := time.Now().Format("2006-01-02")
	if got := FormatDateSeparator(today); got != "Today" {
		t.Errorf("after SetNowFunc(nil), today = %q, want \"Today\"", got)
	}
}
