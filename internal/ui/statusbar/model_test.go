// internal/ui/statusbar/model_test.go
package statusbar

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// testMode is a simple fmt.Stringer for testing without importing ui (avoids circular import).
type testMode string

func (m testMode) String() string { return string(m) }

func TestStatusBarNormalMode(t *testing.T) {
	m := New()
	m.SetMode(testMode("NORMAL"))
	m.SetChannel("general")
	m.SetWorkspace("Acme Corp")

	view := m.View(80)

	if !strings.Contains(view, "NORMAL") {
		t.Error("expected 'NORMAL' in status bar")
	}
	if !strings.Contains(view, "general") {
		t.Error("expected 'general' in status bar")
	}
	if !strings.Contains(view, "Acme Corp") {
		t.Error("expected 'Acme Corp' in status bar")
	}
}

func TestStatusBarInsertMode(t *testing.T) {
	m := New()
	m.SetMode(testMode("INSERT"))
	view := m.View(80)

	if !strings.Contains(view, "INSERT") {
		t.Error("expected 'INSERT' in status bar")
	}
}

func TestStatusBarUnreadCount(t *testing.T) {
	m := New()
	m.SetUnreadCount(5)
	view := m.View(80)

	if !strings.Contains(view, "5") {
		t.Error("expected unread count in status bar")
	}
}

func TestModel_CopiedToastShowsAndClears(t *testing.T) {
	m := New()
	m.SetChannel("general")
	m.ShowCopied(42)
	out := m.View(80)
	if !strings.Contains(out, "Copied 42 chars") {
		t.Fatalf("expected toast in status bar; got %q", out)
	}
	m.ClearCopied()
	out = m.View(80)
	if strings.Contains(out, "Copied") {
		t.Fatalf("expected toast cleared; got %q", out)
	}
}

func TestModel_ShowCopiedBumpsVersion(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.ShowCopied(1)
	if m.Version() == v0 {
		t.Fatal("ShowCopied must bump Version()")
	}
}

func TestModel_ShowCopiedZeroIsNoop(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.ShowCopied(0)
	if m.Version() != v0 {
		t.Fatal("ShowCopied(0) must be a no-op (no version bump)")
	}
	if strings.Contains(m.View(80), "Copied") {
		t.Fatal("ShowCopied(0) must not display toast")
	}
}

func TestModel_ClearCopiedIsIdempotent(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.ClearCopied()
	if m.Version() != v0 {
		t.Fatal("ClearCopied with no toast must not bump version")
	}
}

func TestModel_SetToastShowsArbitraryString(t *testing.T) {
	m := New()
	m.SetToast("Copied permalink")
	out := m.View(80)
	if !strings.Contains(out, "Copied permalink") {
		t.Fatalf("expected toast string in view; got %q", out)
	}
}

func TestModel_SetToastEmptyClears(t *testing.T) {
	m := New()
	m.SetToast("hello")
	m.SetToast("")
	out := m.View(80)
	if strings.Contains(out, "hello") {
		t.Fatalf("expected toast cleared after SetToast(\"\"); got %q", out)
	}
}

func TestModel_SetToastBumpsVersionOnChange(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.SetToast("a")
	if m.Version() == v0 {
		t.Fatal("SetToast must bump Version() on change")
	}
	v1 := m.Version()
	m.SetToast("a")
	if m.Version() != v1 {
		t.Fatal("SetToast with same value must be a no-op")
	}
}

func TestModel_ShowCopiedStillRendersCopiedNChars(t *testing.T) {
	// Backwards-compat: existing CopiedMsg path.
	m := New()
	m.ShowCopied(13)
	if !strings.Contains(m.View(80), "Copied 13 chars") {
		t.Fatalf("expected legacy 'Copied N chars' toast; got %q", m.View(80))
	}
}

func TestStatusBar_PresenceSegmentActive(t *testing.T) {
	m := New()
	m.SetStatus("active", false, time.Time{})
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "● Active") {
		t.Errorf("expected '● Active', got: %q", out)
	}
}

func TestStatusBar_PresenceSegmentAway(t *testing.T) {
	m := New()
	m.SetStatus("away", false, time.Time{})
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "○ Away") {
		t.Errorf("expected '○ Away', got: %q", out)
	}
}

func TestStatusBar_DNDSegmentWithCountdown(t *testing.T) {
	m := New()
	end := time.Now().Add(83 * time.Minute) // 1h 23m
	m.SetStatus("active", true, end)
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "🌙 DND") {
		t.Errorf("expected '🌙 DND' prefix, got: %q", out)
	}
	if !strings.Contains(out, "1h 23m") && !strings.Contains(out, "1h 22m") {
		t.Errorf("expected ~1h 23m countdown, got: %q", out)
	}
}

func TestStatusBar_DNDLessThanOneMinute(t *testing.T) {
	m := New()
	end := time.Now().Add(20 * time.Second)
	m.SetStatus("active", true, end)
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "<1m") {
		t.Errorf("expected '<1m', got: %q", out)
	}
}

func TestStatusBar_DNDNoEndTimestamp(t *testing.T) {
	m := New()
	m.SetStatus("active", true, time.Time{})
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "🌙 DND") {
		t.Errorf("expected '🌙 DND', got: %q", out)
	}
}

func TestStatusBar_PresenceUnknown_NoSegment(t *testing.T) {
	m := New()
	// Default state — no SetStatus call. Status segment should not appear.
	out := stripANSI(m.View(120))
	if strings.Contains(out, "Active") || strings.Contains(out, "Away") || strings.Contains(out, "DND") {
		t.Errorf("expected no presence/DND segment when unset, got: %q", out)
	}
}

func TestSetSyncing_ShowsIndicatorInView(t *testing.T) {
	m := New()
	m.SetChannel("random")

	if strings.Contains(stripANSI(m.View(120)), "○") {
		t.Fatal("indicator should NOT be present before SetSyncing(true)")
	}

	m.SetSyncing(true)

	if !strings.Contains(stripANSI(m.View(120)), "○") {
		t.Errorf("indicator should appear after SetSyncing(true); got: %q", stripANSI(m.View(120)))
	}

	m.SetSyncing(false)

	if strings.Contains(stripANSI(m.View(120)), "○") {
		t.Errorf("indicator should disappear after SetSyncing(false); got: %q", stripANSI(m.View(120)))
	}
}

func TestSetSyncing_IdempotentOnNoChange(t *testing.T) {
	m := New()
	m.SetSyncing(true)
	verBefore := m.Version()

	m.SetSyncing(true) // same value

	if m.Version() != verBefore {
		t.Error("Version should NOT bump when SetSyncing called with same value")
	}
}

func TestSetSyncing_BumpsVersionOnChange(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.SetSyncing(true)
	if m.Version() == v0 {
		t.Fatal("SetSyncing(true) must bump Version() on change")
	}
}

func TestHelpHintRendersOnWideBar(t *testing.T) {
	m := New()
	m.SetMode(testMode("NORMAL"))
	m.SetChannel("general")
	m.SetWorkspace("Acme")
	m.SetHelpHint("? for keybindings")

	view := stripANSI(m.View(120))
	if !strings.Contains(view, "? for keybindings") {
		t.Errorf("expected hint in wide status bar, got: %q", view)
	}
}

func TestHelpHintHiddenWhenNarrow(t *testing.T) {
	m := New()
	m.SetMode(testMode("NORMAL"))
	m.SetChannel("a-long-channel-name-that-eats-space")
	m.SetWorkspace("A Workspace With A Long Name")
	m.SetHelpHint("? for keybindings")

	view := stripANSI(m.View(50))
	if strings.Contains(view, "? for keybindings") {
		t.Errorf("hint should drop when no room, got: %q", view)
	}
}

func TestHelpHintEmptyByDefault(t *testing.T) {
	m := New()
	m.SetMode(testMode("NORMAL"))
	m.SetChannel("general")
	view := stripANSI(m.View(120))
	if strings.Contains(view, "for keybindings") {
		t.Error("no hint should appear when unset")
	}
}

func TestSetHelpHint_BumpsVersionOnChange(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.SetHelpHint("? for keybindings")
	if m.Version() == v0 {
		t.Fatal("SetHelpHint must bump Version on change")
	}
	v1 := m.Version()
	m.SetHelpHint("? for keybindings") // same value
	if m.Version() != v1 {
		t.Fatal("SetHelpHint must not bump Version when value unchanged")
	}
}

func TestModel_SetSearchElidesLongText(t *testing.T) {
	m := New()

	m.SetSearch("/abc")
	if got := m.Search(); got != "/abc" {
		t.Fatalf("short search segment mangled: %q", got)
	}

	m.SetSearch("/" + strings.Repeat("q", 100))
	got := m.Search()
	if n := len([]rune(got)); n > 40 {
		t.Fatalf("search segment is %d runes, want <= 40", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("elided search segment missing ellipsis: %q", got)
	}
}

func TestChannelGlyphSpacing(t *testing.T) {
	tests := []struct {
		chType string
		want   string
	}{
		{"channel", "#general"},
		{"private", "◆ general"},
		{"dm", "● general"},
		{"group_dm", "● general"},
	}
	for _, tt := range tests {
		m := New()
		m.SetChannel("general")
		m.SetChannelType(tt.chType)
		if out := stripANSI(m.View(80)); !strings.Contains(out, tt.want) {
			t.Errorf("channelType %q: want %q in %q", tt.chType, tt.want, out)
		}
	}
}

func TestViewIsExactlyWidthColumns(t *testing.T) {
	m := New()
	m.SetMode(testMode("NORMAL"))
	m.SetChannel("Some Person")
	m.SetChannelType("dm")
	m.SetWorkspace("acme")
	m.SetUnreadCount(3)
	m.SetStatus("active", false, time.Time{})
	m.SetConnectionState(StateConnected)
	m.SetHelpHint("? for keybindings")

	// 80 is narrow enough to drop the hint, the rest keep it: both
	// filler branches must land on exactly width.
	for _, width := range []int{80, 120, 240} {
		if got := lipgloss.Width(m.View(width)); got != width {
			t.Errorf("View(%d) is %d columns wide, want %d", width, got, width)
		}
	}
}

// stripANSI removes ANSI escape sequences for substring assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
