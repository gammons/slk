package confirmprompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type sentinelMsg struct{}

func TestOpenAndClose(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("should not be visible before Open")
	}
	m.Open("Delete?", "preview text", func() tea.Msg { return sentinelMsg{} })
	if !m.IsVisible() {
		t.Error("should be visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("should not be visible after Close")
	}
}

func TestHandleKey_ConfirmReturnsCmd(t *testing.T) {
	for _, k := range []string{"y", "Y", "enter"} {
		m := New()
		called := false
		m.Open("Delete?", "preview", func() tea.Msg {
			called = true
			return sentinelMsg{}
		})
		res := m.HandleKey(k)
		if !res.Confirmed {
			t.Errorf("key %q: expected Confirmed=true", k)
		}
		if res.Cancelled {
			t.Errorf("key %q: expected Cancelled=false", k)
		}
		if res.Cmd == nil {
			t.Fatalf("key %q: expected non-nil Cmd", k)
		}
		_ = res.Cmd()
		if !called {
			t.Errorf("key %q: expected onConfirm to be called", k)
		}
		if m.IsVisible() {
			t.Errorf("key %q: prompt should be closed after confirm", k)
		}
	}
}

func TestHandleKey_CancelKeys(t *testing.T) {
	for _, k := range []string{"n", "N", "esc", "escape", "x", "z"} {
		m := New()
		m.Open("Delete?", "preview", func() tea.Msg { return sentinelMsg{} })
		res := m.HandleKey(k)
		if res.Confirmed {
			t.Errorf("key %q: should not confirm", k)
		}
		if !res.Cancelled {
			t.Errorf("key %q: expected Cancelled=true", k)
		}
		if res.Cmd != nil {
			t.Errorf("key %q: expected nil Cmd on cancel", k)
		}
		if m.IsVisible() {
			t.Errorf("key %q: prompt should be closed after cancel", k)
		}
	}
}

func TestHandleKey_NilOnConfirm(t *testing.T) {
	m := New()
	m.Open("Title", "Body", nil)
	res := m.HandleKey("y")
	if !res.Confirmed {
		t.Error("expected Confirmed=true")
	}
	if res.Cmd != nil {
		t.Error("expected nil Cmd when onConfirm is nil")
	}
}

func TestView_ContainsTitleAndBody(t *testing.T) {
	m := New()
	m.Open("Delete message?", "hello world", func() tea.Msg { return nil })
	out := m.View(80)
	if !strings.Contains(out, "Delete message?") {
		t.Errorf("expected title in view: %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected body in view: %q", out)
	}
}

func TestView_HiddenWhenNotVisible(t *testing.T) {
	m := New()
	if m.View(80) != "" {
		t.Error("View should return empty when not visible")
	}
}

func TestViewOverlay_PassthroughWhenHidden(t *testing.T) {
	m := New()
	bg := "background-content"
	if got := m.ViewOverlay(80, 24, bg); got != bg {
		t.Errorf("expected passthrough background when hidden, got %q", got)
	}
}

func TestView_CollapsesBodyNewlines(t *testing.T) {
	m := New()
	m.Open("Title", "line1\nline2\nline3", func() tea.Msg { return nil })
	out := m.View(80)
	if strings.Contains(out, "line1\nline2") {
		t.Errorf("expected newlines to be collapsed, got: %q", out)
	}
	// All three should still appear on one rendered line, with spaces.
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") || !strings.Contains(out, "line3") {
		t.Errorf("expected all body words preserved: %q", out)
	}
}

func TestHandleKey_NoOpWhenHidden(t *testing.T) {
	m := New()
	res := m.HandleKey("y")
	if res.Confirmed || res.Cancelled || res.Cmd != nil {
		t.Errorf("expected zero Result when hidden, got %+v", res)
	}
}
