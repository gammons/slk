package messages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// longTSClock is Tuesday 2026-09-29 16:00 in the LOCAL zone. Local (not
// UTC) so the message's local calendar day and the clock's year agree in
// every zone; mid-afternoon so fixture times stay clear of midnight.
func longTSClock() time.Time {
	return time.Date(2026, 9, 29, 16, 0, 0, 0, time.Local)
}

// longTSAt formats t as a Slack ts.
func longTSAt(t time.Time) string {
	return fmt.Sprintf("%d.000100", t.Unix())
}

func pinLongTSClock(t *testing.T) {
	t.Helper()
	styles.Apply("dark", config.Theme{})
	SetNowFunc(longTSClock)
	t.Cleanup(func() { SetNowFunc(nil) })
}

func TestLongTimestamp_SameYear(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	if got, want := LongTimestamp(ts, "3:42 PM"), "Tue Sep 29, 3:42 PM"; got != want {
		t.Fatalf("LongTimestamp = %q, want %q", got, want)
	}
}

func TestLongTimestamp_EarlierYear(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2025, 9, 29, 15, 42, 0, 0, time.Local))
	if got, want := LongTimestamp(ts, "3:42 PM"), "Mon Sep 29 2025, 3:42 PM"; got != want {
		t.Fatalf("LongTimestamp = %q, want %q", got, want)
	}
}

func TestLongTimestamp_LaterYear(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2027, 9, 29, 15, 42, 0, 0, time.Local))
	if got, want := LongTimestamp(ts, "3:42 PM"), "Wed Sep 29 2027, 3:42 PM"; got != want {
		t.Fatalf("LongTimestamp = %q, want %q", got, want)
	}
}

func TestLongTimestamp_UnparseableTSUnchanged(t *testing.T) {
	pinLongTSClock(t)
	for _, ts := range []string{"", "abc", "x.123"} {
		if got := LongTimestamp(ts, "3:42 PM"); got != "3:42 PM" {
			t.Errorf("LongTimestamp(%q) = %q, want short unchanged", ts, got)
		}
	}
}

func TestLongTimestamp_EmptyShortUnchanged(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	if got := LongTimestamp(ts, ""); got != "" {
		t.Fatalf("LongTimestamp(ts, \"\") = %q, want \"\"", got)
	}
}

// selectedHeaderFixture builds a header and a rendered message the way
// the renderers do: styled name + gap + styled short timestamp, then body.
func selectedHeaderFixture(name, short, body string) (header, rendered string) {
	header = name + "  " + styles.Timestamp.Render(short)
	return header, header + "\n" + body
}

func TestSelectedHeader_SubstitutesWhenItFits(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "hello")
	got := SelectedHeader(rendered, header, ts, "3:42 PM", 80)
	want := strings.Replace(rendered, styles.Timestamp.Render("3:42 PM"),
		styles.Timestamp.Render("Tue Sep 29, 3:42 PM"), 1)
	if got != want {
		t.Fatalf("SelectedHeader did not substitute:\n got %q\nwant %q", got, want)
	}
}

func TestSelectedHeader_TooWideUnchanged(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "hello")
	// "sam  Tue Sep 29, 3:42 PM" is 24 columns; one short of that must refuse.
	longWidth := lipgloss.Width("sam  Tue Sep 29, 3:42 PM")
	if got := SelectedHeader(rendered, header, ts, "3:42 PM", longWidth-1); got != rendered {
		t.Fatalf("SelectedHeader substituted despite exceeding maxWidth:\n%q", got)
	}
	if got := SelectedHeader(rendered, header, ts, "3:42 PM", longWidth); got == rendered {
		t.Fatal("SelectedHeader refused a header exactly at maxWidth")
	}
}

func TestSelectedHeader_ReplacesOnlyHeaderOccurrence(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	styledShort := styles.Timestamp.Render("3:42 PM")
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "see you at "+styledShort)
	got := SelectedHeader(rendered, header, ts, "3:42 PM", 80)
	styledLong := styles.Timestamp.Render("Tue Sep 29, 3:42 PM")
	if n := strings.Count(got, styledLong); n != 1 {
		t.Fatalf("long timestamp occurs %d times, want 1:\n%q", n, got)
	}
	if !strings.HasSuffix(got, "see you at "+styledShort) {
		t.Fatalf("body occurrence was rewritten:\n%q", got)
	}
}

func TestSelectedHeader_EmptyShortUnchanged(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	header, rendered := selectedHeaderFixture("sam", "", "hello")
	if got := SelectedHeader(rendered, header, ts, "", 80); got != rendered {
		t.Fatalf("SelectedHeader with empty short changed output:\n%q", got)
	}
}

func TestSelectedHeader_UnparseableTSUnchanged(t *testing.T) {
	pinLongTSClock(t)
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "hello")
	if got := SelectedHeader(rendered, header, "not-a-ts", "3:42 PM", 80); got != rendered {
		t.Fatalf("SelectedHeader with bad ts changed output:\n%q", got)
	}
}
