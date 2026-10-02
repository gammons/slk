package userprofile

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// The 📋 is written as literal text (never swapped for a kitty image
// placement), so the terminal draws it at its Unicode width even when
// emoji image mode reserves 1 cell per emoji. Positions here are
// measured with uniseg, the terminal's own view, not emoji.Width.
func TestClickAt_ImageModeOneCellUsesDrawnWidth(t *testing.T) {
	emoji.SetImageMode(true, 1)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })

	m := loadedWithEmail("priya@example.com")
	lines := boxLines(ansi.Strip(m.ViewOverlay(80, 24, "", Live{Now: time.Now()})))
	y, x := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Email") {
			y, x = i, uniseg.StringWidth(l[:strings.Index(l, CopyIcon)])
		}
		if w := uniseg.StringWidth(l); w != 56 {
			t.Errorf("row %d drawn %d cells wide, want 56: %q", i, w, l)
		}
	}
	if y < 0 {
		t.Fatal("no Email row")
	}
	for dx := 0; dx < uniseg.StringWidth(CopyIcon); dx++ {
		if !m.ClickAt(80, 24, x+dx, y) {
			t.Errorf("ClickAt(%d, %d) = false on drawn icon cell %d", x+dx, y, dx)
		}
	}
}

// loadedWithEmail returns an open modal whose fetch has returned email.
func loadedWithEmail(email string) *Model {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	m.SetProfile("T1", "U1", core.UserProfile{
		UserID: "U1", TeamID: "T1", Handle: "priya", DisplayName: "Priya",
		Email: email,
	})
	return m
}

// emailRow returns the ANSI-stripped box row that carries the Email
// label, with the centering padding and borders removed.
func emailRow(t *testing.T, out string) string {
	t.Helper()
	for _, l := range boxLines(ansi.Strip(out)) {
		if strings.Contains(l, "Email") {
			return l
		}
	}
	t.Fatalf("no Email row in:\n%s", ansi.Strip(out))
	return ""
}

func TestEmail_OnlyOnceLoaded(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1"})
	if got := m.Email(); got != "" {
		t.Errorf("Email() while loading = %q, want empty", got)
	}
	m.SetProfile("T1", "U1", core.UserProfile{Email: "priya@example.com"})
	if got := m.Email(); got != "priya@example.com" {
		t.Errorf("Email() after load = %q, want priya@example.com", got)
	}
	m.Close()
	if got := m.Email(); got != "" {
		t.Errorf("Email() after Close = %q, want empty", got)
	}
}

func TestView_CopyIconBesideEmail(t *testing.T) {
	out := loadedWithEmail("priya@example.com").ViewOverlay(80, 24, "", Live{Now: time.Now()})
	row := emailRow(t, out)
	if !strings.Contains(row, "priya@example.com "+CopyIcon) {
		t.Errorf("email row = %q, want the email followed by a space and %s", row, CopyIcon)
	}
	if !containsAll(out, "e copy email \u00b7 K / esc / q close") {
		t.Errorf("footer should advertise e:\n%s", ansi.Strip(out))
	}
}

func TestView_NoCopyAffordanceWithoutEmail(t *testing.T) {
	loading := New()
	loading.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	noEmail := loadedWithEmail("")

	for name, m := range map[string]*Model{"loading": loading, "no email": noEmail} {
		out := m.ViewOverlay(80, 24, "", Live{Now: time.Now()})
		if !containsNone(out, CopyIcon, "copy email") {
			t.Errorf("%s: copy affordance shown without an email:\n%s", name, ansi.Strip(out))
		}
		if !containsAll(out, "K / esc / q close") {
			t.Errorf("%s: plain footer missing:\n%s", name, ansi.Strip(out))
		}
	}
}

func TestView_LongEmailKeepsIconInsideBox(t *testing.T) {
	long := strings.Repeat("a", 80) + "@example.com"
	for _, termW := range []int{40, 80} {
		out := loadedWithEmail(long).ViewOverlay(termW, 24, "", Live{Now: time.Now()})
		row := emailRow(t, out)
		// Row is "│ <content> │": the icon must sit just before the
		// closing padding, after an ellipsised email.
		if !strings.Contains(row, "\u2026 "+CopyIcon) {
			t.Errorf("termW=%d: row = %q, want a truncated email then the icon", termW, row)
		}
		want := 56
		if termW-4 < want {
			want = termW - 4
		}
		if w := emoji.Width(row); w > want {
			t.Errorf("termW=%d: email row width %d exceeds box width %d", termW, w, want)
		}
	}
}

func TestBoxSize_MatchesRenderedBox(t *testing.T) {
	m := loadedWithEmail("priya@example.com")
	w, h := m.BoxSize(80, 24)
	lines := boxLines(ansi.Strip(m.ViewOverlay(80, 24, "", Live{Now: time.Now()})))
	if h != len(lines) {
		t.Errorf("BoxSize height = %d, rendered box has %d rows", h, len(lines))
	}
	if w != 56 {
		t.Errorf("BoxSize width = %d, want 56 (min(56, 80-4))", w)
	}
	hidden := New()
	if w, h := hidden.BoxSize(80, 24); w != 0 || h != 0 {
		t.Errorf("hidden BoxSize = (%d, %d), want (0, 0)", w, h)
	}
}

// iconCells locates the email row's icon in the box rendered with live:
// box-local row and first display column. It anchors on the "Email"
// label so a 📋 elsewhere (e.g. a :clipboard: status) can't match.
func iconCells(t *testing.T, m *Model, termW, termH int, live Live) (row, col int) {
	t.Helper()
	lines := boxLines(ansi.Strip(m.ViewOverlay(termW, termH, "", live)))
	for y, l := range lines {
		if !strings.Contains(l, "Email") {
			continue
		}
		if i := strings.Index(l, CopyIcon); i >= 0 {
			return y, emoji.Width(l[:i])
		}
	}
	t.Fatal("icon not rendered on the Email row")
	return 0, 0
}

func TestClickAt_HitsOnlyTheIcon(t *testing.T) {
	m := loadedWithEmail("priya@example.com")
	y, x := iconCells(t, m, 80, 24, Live{Now: time.Now()})
	iw := emoji.Width(CopyIcon)

	for dx := 0; dx < iw; dx++ {
		if !m.ClickAt(80, 24, x+dx, y) {
			t.Errorf("ClickAt(x=%d, y=%d) = false, want a hit on icon cell %d", x+dx, y, dx)
		}
	}
	misses := [][2]int{{x - 1, y}, {x + iw, y}, {x, y - 1}, {x, y + 1}}
	for _, p := range misses {
		if m.ClickAt(80, 24, p[0], p[1]) {
			t.Errorf("ClickAt(x=%d, y=%d) = true, want a miss next to the icon", p[0], p[1])
		}
	}
}

// Status rows sit above the details, so a live status moves the email
// row down; the hit-test must follow what the last frame drew.
func TestClickAt_FollowsLiveStatusRows(t *testing.T) {
	m := loadedWithEmail("priya@example.com")
	plainY, _ := iconCells(t, m, 80, 24, Live{Now: time.Now()})

	withStatus := Live{Now: time.Now(), Status: peerstatus.Status{Emoji: ":palm_tree:", Text: "Vacation", DND: true}}
	y, x := iconCells(t, m, 80, 24, withStatus)
	if y == plainY {
		t.Fatalf("precondition: status rows did not move the email row (y=%d)", y)
	}
	if !m.ClickAt(80, 24, x, y) {
		t.Errorf("ClickAt(%d, %d) missed the icon after status rows moved it", x, y)
	}
	if m.ClickAt(80, 24, x, plainY) {
		t.Errorf("ClickAt(%d, %d) hit the icon's old, pre-status row", x, plainY)
	}
	if _, h := m.BoxSize(80, 24); h != len(boxLines(ansi.Strip(m.ViewOverlay(80, 24, "", withStatus)))) {
		t.Errorf("BoxSize height %d does not match the frame drawn with status rows", h)
	}
}

// A :clipboard: status draws the same glyph; only the email row's icon
// is the copy target.
func TestClickAt_IgnoresClipboardStatusGlyph(t *testing.T) {
	m := loadedWithEmail("priya@example.com")
	live := Live{Now: time.Now(), Status: peerstatus.Status{Emoji: ":clipboard:", Text: "Auditing"}}
	lines := boxLines(ansi.Strip(m.ViewOverlay(80, 24, "", live)))
	statusY, statusX := -1, -1
	for y, l := range lines {
		if strings.Contains(l, "Auditing") {
			if i := strings.Index(l, CopyIcon); i >= 0 {
				statusY, statusX = y, emoji.Width(l[:i])
			}
		}
	}
	if statusY < 0 {
		t.Fatal("precondition: the :clipboard: status glyph is not rendered as the copy icon")
	}
	if m.ClickAt(80, 24, statusX, statusY) {
		t.Errorf("ClickAt on the status row's 📋 (%d, %d) = true, want false", statusX, statusY)
	}
	y, x := iconCells(t, m, 80, 24, live)
	if !m.ClickAt(80, 24, x, y) {
		t.Errorf("ClickAt on the email icon (%d, %d) = false, want true", x, y)
	}
}

func TestClickAt_NoIconNoHit(t *testing.T) {
	m := loadedWithEmail("")
	m.ViewOverlay(80, 24, "", Live{Now: time.Now()})
	w, h := m.BoxSize(80, 24)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if m.ClickAt(80, 24, x, y) {
				t.Fatalf("ClickAt(%d, %d) = true with no email shown", x, y)
			}
		}
	}
}

// On a short terminal the details (and so the icon) are dropped while
// the title and status survive; no cell may still count as the icon.
func TestClickAt_DetailsDroppedNoHit(t *testing.T) {
	m := loadedWithEmail("priya@example.com")
	m.profile.Title = "Staff Engineer"
	live := Live{Now: time.Now(), Status: peerstatus.Status{Emoji: ":palm_tree:", Text: "Vacation"}}
	const termH = 12 // tallest height that keeps title+status but drops details
	out := m.ViewOverlay(80, termH, "", live)
	if !containsAll(out, "Staff Engineer", "Vacation") || !containsNone(out, "Email") {
		t.Fatalf("precondition: want title+status kept and details dropped at h=%d:\n%s", termH, ansi.Strip(out))
	}
	w, h := m.BoxSize(80, termH)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if m.ClickAt(80, termH, x, y) {
				t.Fatalf("ClickAt(%d, %d) = true with the email row dropped", x, y)
			}
		}
	}
}
