package compose

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

func TestPreviewOffByDefault(t *testing.T) {
	m := New("g")
	if m.PreviewOn() {
		t.Fatal("a new box has the draft preview on")
	}
}

func TestTogglePreview(t *testing.T) {
	m := New("g")
	if !m.TogglePreview() || !m.PreviewOn() {
		t.Fatal("first toggle should turn the preview on")
	}
	if m.TogglePreview() || m.PreviewOn() {
		t.Fatal("second toggle should turn the preview off")
	}
	m.SetPreview(true)
	if !m.PreviewOn() {
		t.Fatal("SetPreview(true) left the preview off")
	}
}

func TestResetClosesPreview(t *testing.T) {
	m := New("g")
	m.SetPreview(true)
	m.Reset()
	if m.PreviewOn() {
		t.Fatal("Reset (a send) left the preview on")
	}
}

func TestSwitchingConversationClosesPreview(t *testing.T) {
	m := New("g")
	m.SetDraftContext("T1", "C1", "")
	m.SetPreview(true)
	m.SetDraftContext("T1", "C2", "")
	if m.PreviewOn() {
		t.Fatal("moving the box to another conversation left the preview on")
	}

	m = New("g")
	m.SetDraftContext("T1", "C1", "")
	m.SetPreview(true)
	m.SetDraftContext("T1", "C1", "")
	if !m.PreviewOn() {
		t.Fatal("rebinding to the same conversation closed the preview")
	}
}

func identityRender(s string, _ int) string { return s }

func TestPreviewBodyIsTheSendPath(t *testing.T) {
	m := New("g")
	m.SetUsers([]mentionpicker.User{{ID: "U2", DisplayName: "bob"}})
	m.SetValue("**hi** @bob")
	want, _ := mrkdwn.Convert(m.TranslateMentionsForSend(m.Value()))
	got := m.PreviewBody(40, RenderKey{}, identityRender)
	if got != want {
		t.Fatalf("PreviewBody = %q, want the send path's %q", got, want)
	}
	if !strings.Contains(got, "<@U2>") {
		t.Fatalf("PreviewBody = %q, want the translated mention <@U2>", got)
	}
}

func TestPreviewBodyCaches(t *testing.T) {
	m := New("g")
	calls := 0
	render := func(s string, _ int) string { calls++; return s }
	check := func(step string, want int) {
		t.Helper()
		if calls != want {
			t.Fatalf("%s: %d renders, want %d", step, calls, want)
		}
	}

	m.SetValue("hi @bob")
	k := RenderKey{}
	m.PreviewBody(40, k, render)
	m.PreviewBody(40, k, render)
	check("same draft, width and key twice", 1)

	m.SetValue("hi @bob!")
	m.PreviewBody(40, k, render)
	check("changed draft", 2)

	m.PreviewBody(41, k, render)
	check("changed width", 3)

	k = RenderKey{Theme: 1}
	m.PreviewBody(41, k, render)
	check("changed key", 4)

	m.SetUsers([]mentionpicker.User{{ID: "U7", DisplayName: "zed"}})
	m.PreviewBody(41, k, render)
	check("user the draft does not mention", 4)

	m.SetUsers([]mentionpicker.User{{ID: "U2", DisplayName: "bob"}})
	m.PreviewBody(41, k, render)
	check("user the draft mentions", 5)
}
