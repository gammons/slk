package userprofile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// loadedWithHuddle returns an open modal whose fetch has returned a
// profile carrying the huddle channel ("" for none).
func loadedWithHuddle(channel string) *Model {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	m.SetProfile("T1", "U1", core.UserProfile{
		UserID: "U1", TeamID: "T1", Handle: "priya", DisplayName: "Priya",
		HuddleChannelID: channel,
	})
	return m
}

// inHuddleLive is the render-time Live for a user currently in a huddle.
func inHuddleLive() Live {
	return Live{Now: time.Now(), Status: peerstatus.Status{Huddle: peerstatus.HuddleActive}}
}

// huddleRow returns the ANSI-stripped box row that carries the huddle
// status, or "" when none is drawn.
func huddleRow(t *testing.T, out string) string {
	t.Helper()
	for _, l := range boxLines(ansi.Strip(out)) {
		if strings.Contains(l, "In a huddle") {
			return l
		}
	}
	return ""
}

func TestHuddleURL_OnlyWithLoadedChannel(t *testing.T) {
	loading := New()
	loading.Open(Seed{TeamID: "T1", UserID: "U1"})

	if got := loading.HuddleURL(); got != "" {
		t.Errorf("HuddleURL() while loading = %q, want empty", got)
	}
	if got := loadedWithHuddle("").HuddleURL(); got != "" {
		t.Errorf("HuddleURL() without a huddle channel = %q, want empty", got)
	}
	want := "https://app.slack.com/huddle/T1/C9"
	if got := loadedWithHuddle("C9").HuddleURL(); got != want {
		t.Errorf("HuddleURL() = %q, want %q", got, want)
	}
	closed := loadedWithHuddle("C9")
	closed.Close()
	if got := closed.HuddleURL(); got != "" {
		t.Errorf("HuddleURL() after Close = %q, want empty", got)
	}
}

func TestView_HuddleIconOnlyWhenChannelKnown(t *testing.T) {
	withLink := loadedWithHuddle("C9")
	out := withLink.ViewOverlay(80, 24, "", inHuddleLive())
	row := huddleRow(t, out)
	if row == "" {
		t.Fatalf("no huddle row in:\n%s", ansi.Strip(out))
	}
	if !strings.Contains(row, "In a huddle "+CopyIcon) {
		t.Errorf("huddle row = %q, want the status followed by a space and %s", row, CopyIcon)
	}
	if !containsAll(out, "c huddle link") {
		t.Errorf("footer should advertise the huddle link:\n%s", ansi.Strip(out))
	}

	without := loadedWithHuddle("")
	out = without.ViewOverlay(80, 24, "", inHuddleLive())
	row = huddleRow(t, out)
	if row == "" {
		t.Fatalf("no huddle row in:\n%s", ansi.Strip(out))
	}
	if strings.Contains(row, CopyIcon) {
		t.Errorf("huddle row = %q, want no copy icon without a huddle channel", row)
	}
	if containsAll(out, "c huddle link") {
		t.Errorf("footer should not advertise the huddle link without a channel:\n%s", ansi.Strip(out))
	}
}

func TestView_HuddleFooterWithBothCopiables(t *testing.T) {
	m := loadedWithHuddle("C9")
	m.profile.Email = "priya@example.com"
	out := m.ViewOverlay(80, 24, "", inHuddleLive())
	if !containsAll(out, "e copy email \u00b7 c huddle link") {
		t.Errorf("footer should advertise both copies:\n%s", ansi.Strip(out))
	}
}

func TestClickAt_HitsTheHuddleIcon(t *testing.T) {
	m := loadedWithHuddle("C9")
	out := m.ViewOverlay(80, 24, "", inHuddleLive())
	row, y := "", -1
	for i, l := range boxLines(ansi.Strip(out)) {
		if strings.Contains(l, "In a huddle") {
			row, y = l, i
			break
		}
	}
	if row == "" {
		t.Fatal("precondition: no huddle row")
	}
	i := strings.Index(row, CopyIcon)
	if i < 0 {
		t.Fatalf("precondition: no icon on the huddle row: %q", row)
	}
	x := uniseg.StringWidth(row[:i])
	iw := emoji.Width(CopyIcon)

	for dx := 0; dx < iw; dx++ {
		if !m.ClickAt(80, 24, x+dx, y) {
			t.Errorf("ClickAt(x=%d, y=%d) = false, want a hit on huddle icon cell %d", x+dx, y, dx)
		}
	}
	if got := m.LastClickTarget(); got != ClickTargetHuddle {
		t.Errorf("LastClickTarget = %q, want %q", got, ClickTargetHuddle)
	}

	// A miss clears it.
	if m.ClickAt(80, 24, x-1, y) {
		t.Error("ClickAt just left of the huddle icon = true, want a miss")
	}
	if got := m.LastClickTarget(); got != "" {
		t.Errorf("LastClickTarget after a miss = %q, want empty", got)
	}

	// The email icon still reports itself.
	m2 := loadedWithHuddle("C9")
	m2.profile.Email = "priya@example.com"
	m2.ViewOverlay(80, 24, "", inHuddleLive())
	lines := boxLines(ansi.Strip(m2.ViewOverlay(80, 24, "", inHuddleLive())))
	for yy, l := range lines {
		if strings.Contains(l, "Email") {
			if j := strings.Index(l, CopyIcon); j >= 0 {
				xx := uniseg.StringWidth(l[:j])
				if !m2.ClickAt(80, 24, xx, yy) {
					t.Error("ClickAt on the email icon = false, want true")
				}
				if got := m2.LastClickTarget(); got != ClickTargetEmail {
					t.Errorf("LastClickTarget = %q, want %q", got, ClickTargetEmail)
				}
			}
		}
	}
}

func TestClickAt_NoHuddleIconNoHit(t *testing.T) {
	m := loadedWithHuddle("")
	m.ViewOverlay(80, 24, "", inHuddleLive())
	w, h := m.BoxSize(80, 24)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if m.ClickAt(80, 24, x, y) {
				// Only the email icon may hit; none is shown here
				// (this profile carries no email).
				t.Fatalf("ClickAt(%d, %d) = true with no copiable row shown", x, y)
			}
		}
	}
}

// A refresh that fails behind an already-shown (cached) profile keeps
// that profile on screen; with nothing shown it still reports the error.
func TestSetError_KeepsAProfileAlreadyShown(t *testing.T) {
	m := loadedWithHuddle("C9")
	if !m.SetError("T1", "U1", context.DeadlineExceeded) {
		t.Fatal("SetError = false, want the failure consumed")
	}
	if got := m.HuddleURL(); got == "" {
		t.Error("profile was replaced by the error; want the cached view kept")
	}

	fresh := New()
	fresh.Open(Seed{TeamID: "T1", UserID: "U1"})
	fresh.SetError("T1", "U1", context.DeadlineExceeded)
	out := fresh.ViewOverlay(80, 24, "", Live{Now: time.Now()})
	if !containsAll(out, "Couldn't load full profile") {
		t.Errorf("a failed first load should show the error:\n%s", ansi.Strip(out))
	}
}

// The copy action is offered under the same conditions the view draws
// the row's icon: a named channel and a live status that still says the
// user is in a huddle. A cached profile whose huddle has ended must not
// copy its dead link.
func TestHuddleLinkOffered_FollowsTheLiveStatus(t *testing.T) {
	m := loadedWithHuddle("C9")

	m.ViewOverlay(80, 24, "", inHuddleLive())
	if !m.HuddleLinkOffered() {
		t.Error("HuddleLinkOffered = false in a huddle, want true")
	}

	m.ViewOverlay(80, 24, "", Live{Now: time.Now()})
	if m.HuddleLinkOffered() {
		t.Error("HuddleLinkOffered = true after the huddle ended, want false")
	}
	// HuddleURL itself stays pure: the link exists, the offer does not.
	if m.HuddleURL() == "" {
		t.Error("HuddleURL = empty after the huddle ended, want the built link")
	}

	// A profile with no channel is never offered, huddle or not.
	without := loadedWithHuddle("")
	without.ViewOverlay(80, 24, "", inHuddleLive())
	if without.HuddleLinkOffered() {
		t.Error("HuddleLinkOffered = true without a huddle channel, want false")
	}
}
