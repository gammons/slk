package export

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// mustWindow builds a Window or fails the test.
func mustWindow(t *testing.T, since, until, tz string, overlap int) Window {
	t.Helper()
	w, err := NewWindow(since, until, tz, overlap, time.Now())
	if err != nil {
		t.Fatalf("NewWindow(%q, %q, %q, %d): %v", since, until, tz, overlap, err)
	}
	return w
}

// tsAt renders the Slack timestamp for a wall-clock time in tz.
func tsAt(t *testing.T, tz, wall string) string {
	t.Helper()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", tz, err)
	}
	at, err := time.ParseInLocation("2006-01-02 15:04:05", wall, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", wall, err)
	}
	return fmt.Sprintf("%d.000100", at.Unix())
}

func TestNewWindow_BoundsAreMidnightsInTimezone(t *testing.T) {
	w := mustWindow(t, "2026-04-01", "2026-07-01", "America/New_York", 0)
	ny, _ := time.LoadLocation("America/New_York")

	if want := time.Date(2026, 4, 1, 0, 0, 0, 0, ny); !w.Since.Equal(want) {
		t.Errorf("Since = %v, want %v", w.Since, want)
	}
	if want := time.Date(2026, 7, 1, 0, 0, 0, 0, ny); !w.Until.Equal(want) {
		t.Errorf("Until = %v, want %v", w.Until, want)
	}
	if !w.Start.Equal(w.Since) || !w.End.Equal(w.Until) {
		t.Errorf("zero overlap must leave Start/End at Since/Until, got %v / %v", w.Start, w.End)
	}
}

func TestNewWindow_OverlapAddsCalendarDaysAcrossDST(t *testing.T) {
	// US DST starts 2026-03-08, so the 14 days before 2026-03-15 hold
	// one 23-hour day. Calendar-day arithmetic must still land on
	// local midnight, which a 14*24h subtraction would miss by an hour.
	w := mustWindow(t, "2026-03-15", "2026-03-20", "America/New_York", 14)
	ny, _ := time.LoadLocation("America/New_York")

	if want := time.Date(2026, 3, 1, 0, 0, 0, 0, ny); !w.Start.Equal(want) {
		t.Errorf("Start = %v, want %v", w.Start, want)
	}
	if want := time.Date(2026, 4, 3, 0, 0, 0, 0, ny); !w.End.Equal(want) {
		t.Errorf("End = %v, want %v", w.End, want)
	}
	if w.OverlapDays != 14 {
		t.Errorf("OverlapDays = %d, want 14", w.OverlapDays)
	}
}

func TestNewWindow_EmptyUntilRunsThroughToday(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 03:30 UTC on the 18th is still the 17th in New York: "today" must
	// be judged in the export timezone, not the clock's.
	now := time.Date(2026, 9, 18, 3, 30, 0, 0, time.UTC)

	w, err := NewWindow("2026-09-01", "", "America/New_York", 0, now)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if want := time.Date(2026, 9, 18, 0, 0, 0, 0, ny); !w.Until.Equal(want) {
		t.Errorf("Until = %v, want %v", w.Until, want)
	}
}

func TestNewWindow_EmptyTimezoneIsLocal(t *testing.T) {
	w := mustWindow(t, "2026-04-01", "2026-04-02", "", 0)
	if w.Location != time.Local {
		t.Errorf("Location = %v, want time.Local", w.Location)
	}
}

func TestNewWindow_Rejects(t *testing.T) {
	cases := []struct {
		name             string
		since, until, tz string
		overlap          int
	}{
		{"missing since", "", "2026-07-01", "UTC", 0},
		{"malformed since", "04/01/2026", "2026-07-01", "UTC", 0},
		{"malformed until", "2026-04-01", "July", "UTC", 0},
		{"since equals until", "2026-04-01", "2026-04-01", "UTC", 0},
		{"since after until", "2026-07-01", "2026-04-01", "UTC", 0},
		{"unknown timezone", "2026-04-01", "2026-07-01", "Mars/Olympus", 0},
		{"negative overlap", "2026-04-01", "2026-07-01", "UTC", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWindow(tc.since, tc.until, tc.tz, tc.overlap, time.Now())
			if !errors.Is(err, ErrInvalidWindow) {
				t.Errorf("err = %v, want ErrInvalidWindow", err)
			}
		})
	}
}

func TestWindow_Contains(t *testing.T) {
	const tz = "America/New_York"
	w := mustWindow(t, "2026-04-01", "2026-07-01", tz, 0)
	cases := []struct {
		name string
		wall string
		want bool
	}{
		{"second before since", "2026-03-31 23:59:59", false},
		{"since is inclusive", "2026-04-01 00:00:00", true},
		{"mid range", "2026-05-15 12:00:00", true},
		{"last second", "2026-06-30 23:59:59", true},
		{"until is exclusive", "2026-07-01 00:00:00", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := w.Contains(tsAt(t, tz, tc.wall)); got != tc.want {
				t.Errorf("Contains(%s) = %v, want %v", tc.wall, got, tc.want)
			}
		})
	}
}

func TestWindow_ContainsUsesOverlap(t *testing.T) {
	const tz = "America/New_York"
	w := mustWindow(t, "2026-04-01", "2026-07-01", tz, 14)
	cases := []struct {
		wall string
		want bool
	}{
		{"2026-03-17 23:59:59", false},
		{"2026-03-18 00:00:00", true},
		{"2026-07-14 23:59:59", true},
		{"2026-07-15 00:00:00", false},
	}
	for _, tc := range cases {
		if got := w.Contains(tsAt(t, tz, tc.wall)); got != tc.want {
			t.Errorf("Contains(%s) = %v, want %v", tc.wall, got, tc.want)
		}
	}
}

func TestWindow_ContainsRejectsMalformedTS(t *testing.T) {
	w := mustWindow(t, "2026-04-01", "2026-07-01", "UTC", 0)
	for _, ts := range []string{"", "abc", ".000100"} {
		if w.Contains(ts) {
			t.Errorf("Contains(%q) = true, want false", ts)
		}
	}
}

func TestWindow_SlackBounds(t *testing.T) {
	w := mustWindow(t, "2026-04-01", "2026-07-01", "UTC", 1)
	if want := fmt.Sprintf("%d.000000", w.Start.Unix()); w.OldestTS() != want {
		t.Errorf("OldestTS = %q, want %q", w.OldestTS(), want)
	}
	if want := fmt.Sprintf("%d.000000", w.End.Unix()); w.LatestTS() != want {
		t.Errorf("LatestTS = %q, want %q", w.LatestTS(), want)
	}
}

func TestTimeFromTS(t *testing.T) {
	got, ok := TimeFromTS("1700000001.000100")
	if !ok || got.Unix() != 1700000001 {
		t.Errorf("TimeFromTS = %v, %v; want 1700000001, true", got.Unix(), ok)
	}
	if got, ok := TimeFromTS("1700000001"); !ok || got.Unix() != 1700000001 {
		t.Errorf("TimeFromTS without fraction = %v, %v", got.Unix(), ok)
	}
	if _, ok := TimeFromTS(""); ok {
		t.Error("TimeFromTS(\"\") ok = true, want false")
	}
}
