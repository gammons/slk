package userprofile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

var errDeadlineExceeded = context.DeadlineExceeded

// containsAll reports whether s (after ANSI-stripping) contains every
// one of substrs.
func containsAll(s string, substrs ...string) bool {
	plain := ansi.Strip(s)
	for _, sub := range substrs {
		if !strings.Contains(plain, sub) {
			return false
		}
	}
	return true
}

// containsNone reports whether s (after ANSI-stripping) contains none
// of substrs.
func containsNone(s string, substrs ...string) bool {
	plain := ansi.Strip(s)
	for _, sub := range substrs {
		if strings.Contains(plain, sub) {
			return false
		}
	}
	return true
}

// indexOfAll returns the ANSI-stripped index of each substr, failing
// the test if any is missing, so callers can assert relative order.
func indexOfAll(t *testing.T, s string, substrs ...string) []int {
	t.Helper()
	plain := ansi.Strip(s)
	idxs := make([]int, len(substrs))
	for i, sub := range substrs {
		idx := strings.Index(plain, sub)
		if idx < 0 {
			t.Fatalf("expected %q in view:\n%s", sub, plain)
		}
		idxs[i] = idx
	}
	return idxs
}

func TestView_CachedOnly(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya Raman", IsExternal: true})
	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now()})

	if !containsAll(out, "Priya Raman", "external", "Loading profile\u2026", "K / esc / q close") {
		t.Errorf("cached-only view missing expected content:\n%s", ansi.Strip(out))
	}
	if !containsNone(out, "Email", "Phone") {
		t.Errorf("cached-only view should not show Email/Phone:\n%s", ansi.Strip(out))
	}
}

func TestView_Loaded(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya Raman"})
	m.SetProfile("T1", "U1", core.UserProfile{
		UserID: "U1", TeamID: "T1",
		Handle: "priya", RealName: "Priya Raman", DisplayName: "Priya Raman",
		Title: "Staff Engineer, Platform", Pronouns: "she/her",
		Email: "priya@example.com", Phone: "+1 555 0100",
		TZ: "America/New_York", TZAbbrev: "EDT", TZOffset: -4 * 3600,
		IsBot: true, Deleted: true,
	})
	now := time.Date(2026, 9, 30, 18, 42, 0, 0, time.FixedZone("EDT", -4*3600))
	out := m.ViewOverlay(80, 30, "", Live{Now: now})

	if !containsAll(out, "@priya \u00b7 she/her", "APP", "deactivated") {
		t.Errorf("loaded view missing handle/pronoun/badges:\n%s", ansi.Strip(out))
	}
	idx := indexOfAll(t, out, "@priya \u00b7 she/her", "Staff Engineer, Platform", "Local time", "Email", "Phone")
	for i := 1; i < len(idx); i++ {
		if idx[i-1] >= idx[i] {
			t.Errorf("expected order handle < title < Local time < Email < Phone; got indices %v", idx)
		}
	}
}

func TestView_Error(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya Raman"})
	m.SetError("T1", "U1", errDeadlineExceeded)
	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now()})

	if !containsAll(out, "Priya Raman", "Couldn't load full profile: timed out") {
		t.Errorf("error view missing name or error line:\n%s", ansi.Strip(out))
	}
	if !containsNone(out, "Loading profile\u2026") {
		t.Errorf("error view should not show the loading line:\n%s", ansi.Strip(out))
	}
}

func TestView_EmptyFieldsOmitted(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	m.SetProfile("T1", "U1", core.UserProfile{
		UserID: "U1", TeamID: "T1", Handle: "priya", DisplayName: "Priya",
		// Email, Phone, Title, Pronouns all empty.
		TZ: "America/New_York", TZAbbrev: "EDT", TZOffset: -4 * 3600,
	})
	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now()})

	if !containsNone(out, "Email", "Phone", " \u00b7 ", "\u2014") {
		t.Errorf("empty fields should be omitted entirely, not shown as em-dash:\n%s", ansi.Strip(out))
	}
}

func TestView_NoTZOmitsLocalTime(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	m.SetProfile("T1", "U1", core.UserProfile{
		UserID: "U1", TeamID: "T1", Handle: "priya", DisplayName: "Priya",
		Email: "priya@example.com",
		// TZ empty.
	})
	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now()})
	if !containsNone(out, "Local time") {
		t.Errorf("no TZ should omit Local time:\n%s", ansi.Strip(out))
	}
	if !containsAll(out, "Email") {
		t.Errorf("Email should still show:\n%s", ansi.Strip(out))
	}
}

func TestView_PresenceOnlyWhenKnown(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})

	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now(), Presence: ""})
	if !containsNone(out, "active", "away") {
		t.Errorf("unknown presence should not show active/away:\n%s", ansi.Strip(out))
	}

	out = m.ViewOverlay(80, 24, "", Live{Now: time.Now(), Presence: "away"})
	if !containsAll(out, "away") {
		t.Errorf("known presence 'away' should show:\n%s", ansi.Strip(out))
	}
}

func TestView_StatusBlock(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	now := time.Now()

	withStatus := Live{
		Now: now,
		Status: peerstatus.Status{
			Emoji: ":palm_tree:", Text: "Vacation until Fri",
			DND: true, DNDEnd: now.Add(time.Hour),
		},
	}
	out := m.ViewOverlay(80, 30, "", withStatus)
	if !containsAll(out, "Vacation until Fri", "Do not disturb") {
		t.Errorf("status block missing text/DND line:\n%s", ansi.Strip(out))
	}

	// A zero status must not leave two consecutive blank inner rows
	// within the box itself (padding lines from the dimmed backdrop,
	// outside the box, don't count).
	zero := Live{Now: now}
	out = m.ViewOverlay(80, 30, "", zero)
	boxLines := boxContentLines(ansi.Strip(out))
	for i := 1; i < len(boxLines); i++ {
		if strings.TrimSpace(boxLines[i-1]) == "" && strings.TrimSpace(boxLines[i]) == "" {
			t.Errorf("found two consecutive blank inner rows at %d/%d:\n%s", i-1, i, strings.Join(boxLines, "\n"))
		}
	}
}

// TestView_DNDTimeUsesLiveNowZone pins the DND "until" time to Live.Now's
// location, not the machine's local zone: Task 3 renders a golden frame
// from this view, so it must not depend on the test/CI machine's TZ.
func TestView_DNDTimeUsesLiveNowZone(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})

	// A fixed zone 9 hours ahead of UTC, chosen so the expected wall
	// clock differs from both UTC and any plausible machine-local zone.
	loc := time.FixedZone("JST", 9*3600)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, loc) // 10:00 JST
	end := now.Add(3 * time.Hour)                   // 13:00 JST == 04:00 UTC

	live := Live{
		Now: now,
		Status: peerstatus.Status{
			DND: true, DNDEnd: end,
		},
	}
	out := m.ViewOverlay(80, 30, "", live)
	if !containsAll(out, "Do not disturb until 1:00 PM") {
		t.Errorf("DND end time should render in Live.Now's zone (1:00 PM JST), got:\n%s", ansi.Strip(out))
	}
	if !containsNone(out, "4:00 AM") {
		t.Errorf("DND end time must not render in UTC:\n%s", ansi.Strip(out))
	}
}

// boxContentLines returns the lines between (and excluding) a
// rounded-border box's top and bottom edges in a full, possibly padded,
// rendered frame.
func boxContentLines(plain string) []string {
	lines := strings.Split(plain, "\n")
	start, end := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "\u256d") { // ╭
			start = i
		}
		if strings.Contains(l, "\u2570") { // ╰
			end = i
			break
		}
	}
	if start < 0 || end < 0 || start+1 > end {
		return nil
	}
	return lines[start+1 : end]
}

func TestView_AvatarGutter(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})

	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now(), Avatar: "AAAA\nAAAA"})
	plain := ansi.Strip(out)
	found := false
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimLeft(line, "\u2502 ") // border + padding
		if strings.HasPrefix(trimmed, "AAAA  Priya") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a row starting 'AAAA  Priya' with an avatar set:\n%s", plain)
	}

	out = m.ViewOverlay(80, 24, "", Live{Now: time.Now(), Avatar: ""})
	plain = ansi.Strip(out)
	found = false
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimLeft(line, "\u2502 ")
		if strings.HasPrefix(trimmed, "Priya") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a row starting 'Priya' with no avatar:\n%s", plain)
	}
}

func TestView_TruncatesToBoxWidth(t *testing.T) {
	longTitle := strings.Repeat("x", 200)
	longName := strings.Repeat("\U0001F389", 30) // party popper emoji

	for _, termW := range []int{40, 120} {
		m := New()
		m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: longName})
		m.SetProfile("T1", "U1", core.UserProfile{
			UserID: "U1", TeamID: "T1", Handle: "priya", DisplayName: longName,
			Title: longTitle,
		})
		out := m.ViewOverlay(termW, 30, "", Live{Now: time.Now()})

		want := 56
		if termW-4 < want {
			want = termW - 4
		}
		for _, line := range boxLines(ansi.Strip(out)) {
			if w := emoji.Width(line); w > want {
				t.Errorf("termW=%d: line width %d exceeds max %d: %q", termW, w, want, line)
			}
		}
	}
}

// boxLines returns the full box rows (border included) of a rendered,
// possibly centered/padded overlay frame, trimming the padding the
// centering adds on the left of each line.
func boxLines(plain string) []string {
	all := strings.Split(plain, "\n")
	var out []string
	inBox := false
	for _, l := range all {
		trimmed := strings.TrimLeft(l, " ")
		switch {
		case strings.HasPrefix(trimmed, "\u256d"): // ╭
			inBox = true
		case strings.HasPrefix(trimmed, "\u2570"): // ╰
			out = append(out, trimmed)
			inBox = false
			continue
		}
		if inBox || strings.HasPrefix(trimmed, "\u256d") {
			out = append(out, trimmed)
		}
	}
	return out
}

func TestView_ShortTerminalDropsRowsBottomUp(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	m.SetProfile("T1", "U1", core.UserProfile{
		UserID: "U1", TeamID: "T1", Handle: "priya", DisplayName: "Priya",
		Title: "Staff Engineer, Platform",
		Email: "priya@example.com", Phone: "+1 555 0100",
		TZ: "America/New_York", TZAbbrev: "EDT", TZOffset: -4 * 3600,
	})
	live := Live{
		Now:    time.Now(),
		Status: peerstatus.Status{Emoji: ":palm_tree:", Text: "Vacation until Fri"},
	}

	var heights []int
	for h := 30; h >= 6; h-- {
		heights = append(heights, h)
	}

	sawDetailsDrop, sawStatusDrop, sawTitleDrop := false, false, false
	for _, h := range heights {
		out := m.ViewOverlay(80, h, "", live)
		plain := ansi.Strip(out)
		hasDetails := strings.Contains(plain, "Email") || strings.Contains(plain, "Phone") || strings.Contains(plain, "Local time")
		hasStatus := strings.Contains(plain, "Vacation until Fri")
		hasTitle := strings.Contains(plain, "Staff Engineer")
		hasName := strings.Contains(plain, "Priya")
		hasHandle := strings.Contains(plain, "@priya")

		if !hasName || !hasHandle {
			t.Fatalf("at height %d, name/handle must remain: %s", h, plain)
		}
		if !hasDetails {
			sawDetailsDrop = true
		}
		if sawDetailsDrop && hasDetails {
			t.Errorf("at height %d, details reappeared after being dropped", h)
		}
		if !hasStatus {
			sawStatusDrop = true
			if hasDetails {
				t.Errorf("at height %d, status dropped before details", h)
			}
		}
		if !hasTitle {
			sawTitleDrop = true
			if hasStatus {
				t.Errorf("at height %d, title dropped before status", h)
			}
		}
	}
	if !sawDetailsDrop || !sawStatusDrop || !sawTitleDrop {
		t.Errorf("expected to observe details, status and title all drop across heights 30..6 (details=%v status=%v title=%v)",
			sawDetailsDrop, sawStatusDrop, sawTitleDrop)
	}
}
