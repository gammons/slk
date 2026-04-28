// internal/ui/statusbar/model_test.go
package statusbar

import (
	"strings"
	"testing"
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
