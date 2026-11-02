package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
)

// confirmLinkApp is an App focused on a message with the given text, whose
// desktop opener records what it is asked to launch.
func confirmLinkApp(t *testing.T, text string) (*App, *[]string) {
	t.Helper()
	a := newTestApp(t, withSize(0, 0), withActiveTeam("T1"))
	a.focusedPanel = PanelMessages
	a.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: text}})
	opened := &[]string{}
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) {
		d.Open = func(target string) error {
			*opened = append(*opened, target)
			return nil
		}
	})
	return a, opened
}

// sendAndDrain runs msg through Update and every command it yields.
func sendAndDrain(a *App, msg tea.Msg) {
	_, cmd := a.Update(msg)
	for _, m := range drainCmd(cmd) {
		sendAndDrain(a, m)
	}
}

func TestOpenLink_NonWebSchemeAsksFirst(t *testing.T) {
	for _, url := range []string{
		"file:///tmp/x", "ssh://host.example/x", "sftp://host.example/x", "ftp://host.example/x",
	} {
		a, opened := confirmLinkApp(t, "see <"+url+"|Q3 report>")
		sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
		if a.mode != ModeConfirm || !dialogShows(a, url) {
			t.Fatalf("%s: mode = %v, want a confirm prompt showing the full target", url, a.mode)
		}
		if dialogShows(a, "Q3 report") {
			t.Errorf("%s: prompt shows the label", url)
		}
		if len(*opened) != 0 {
			t.Fatalf("%s: opened before confirmation: %v", url, *opened)
		}
	}
}

func TestOpenLink_NonWebSchemeConfirmOpens(t *testing.T) {
	a, opened := confirmLinkApp(t, "see <file:///tmp/x|label>")
	sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
	sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(*opened) != 1 || (*opened)[0] != "file:///tmp/x" {
		t.Errorf("opened = %v, want [file:///tmp/x]", *opened)
	}
	if a.mode == ModeConfirm {
		t.Error("prompt still open after confirming")
	}
}

func TestOpenLink_NonWebSchemeCancelDoesNotOpen(t *testing.T) {
	a, opened := confirmLinkApp(t, "see <file:///tmp/x|label>")
	sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
	sendAndDrain(a, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if len(*opened) != 0 {
		t.Errorf("opened = %v after cancel", *opened)
	}
	if a.mode == ModeConfirm {
		t.Error("prompt still open after cancelling")
	}
}

func TestOpenLink_WebSchemesOpenWithoutPrompt(t *testing.T) {
	for _, url := range []string{
		"https://example.com/a", "http://example.com/a", "mailto:a@example.com", "tel:+15555550100",
	} {
		a, opened := confirmLinkApp(t, "see <"+url+"|label>")
		sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
		if a.mode == ModeConfirm {
			t.Errorf("%s: prompted", url)
		}
		if len(*opened) != 1 || (*opened)[0] != url {
			t.Errorf("%s: opened = %v", url, *opened)
		}
	}
}

func TestOpenLink_PromptKeepsTheEndOfALongTarget(t *testing.T) {
	url := "file:///C:/Users/Public/" + strings.Repeat("./", 200) + "x.exe"
	a, opened := confirmLinkApp(t, "see <"+url+"|Q3 report>")
	sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
	if a.mode != ModeConfirm || !dialogShows(a, "file:///") || !dialogShows(a, "x.exe") {
		t.Fatalf("prompt must show both the scheme and the file name:\n%s", a.confirmPrompt.View())
	}
	sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(*opened) != 1 || (*opened)[0] != url {
		t.Errorf("opener got %v, want the exact URL", *opened)
	}
}

// On a short terminal the prompt's body has fewer lines than the usual 8;
// the budget follows the room it really has, so both ends stay visible.
func TestOpenLink_ShortTerminalKeepsBothEndsOfALongTarget(t *testing.T) {
	url := "file://host.example/" + strings.Repeat("d/", 200) + "x.exe"
	for _, h := range []int{10, 11, 12, 24} {
		a, opened := confirmLinkApp(t, "see <"+url+"|Q3 report>")
		a.width, a.height = 80, h
		sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
		if a.mode != ModeConfirm {
			t.Fatalf("h=%d: mode = %v, want a confirm prompt", h, a.mode)
		}
		if !dialogShows(a, "file://host") || !dialogShows(a, "x.exe") {
			t.Fatalf("h=%d: prompt must show both ends:\n%s", h, a.confirmPrompt.View())
		}
		sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
		if len(*opened) != 1 || (*opened)[0] != url {
			t.Errorf("h=%d: opener got %v, want the exact URL", h, *opened)
		}
	}
}

// Word wrap moves a whole word to the next line, so segments about half the
// box width, separated by a hyphen or a space, waste almost a line each. The
// file name at the end of the target must still be on screen.
func TestOpenLink_WordWrapWasteKeepsTheFileName(t *testing.T) {
	seg := strings.Repeat("a", 18)
	for name, sep := range map[string]string{"hyphen": "-", "space": " "} {
		for _, n := range []int{10, 14, 40} {
			url := "file://host.example/" + strings.Repeat(seg+sep, n) + "evil.exe"
			for _, h := range []int{12, 24} {
				a, opened := confirmLinkApp(t, "see <"+url+"|Q3 report>")
				a.width, a.height = 80, h
				sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
				if a.mode != ModeConfirm {
					t.Fatalf("%s n=%d h=%d: mode = %v, want a confirm prompt", name, n, h, a.mode)
				}
				if !dialogShows(a, "file://host") || !dialogShows(a, "evil.exe") {
					t.Fatalf("%s n=%d h=%d: prompt must show both ends:\n%s", name, n, h, a.confirmPrompt.View())
				}
				sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
				if len(*opened) != 1 || (*opened)[0] != url {
					t.Errorf("%s n=%d h=%d: opener got %v, want the exact URL", name, n, h, *opened)
				}
			}
		}
	}
}

// A base rune followed by VS16 is one cell as a bare rune but two as a
// grapheme cluster, which is how the prompt draws it. The budget must use the
// drawn width, or the wrapped body outgrows the prompt and hides the tail.
func TestOpenLink_ClusterWiderThanItsRunesKeepsTheFileName(t *testing.T) {
	for _, n := range []int{130, 200} {
		url := "file://host.example/" + strings.Repeat("❤️", n) + "/evil.exe"
		for _, h := range []int{12, 24} {
			a, opened := confirmLinkApp(t, "see <"+url+"|Q3 report>")
			a.width, a.height = 80, h
			sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
			if a.mode != ModeConfirm {
				t.Fatalf("n=%d h=%d: mode = %v, want a confirm prompt", n, h, a.mode)
			}
			if !dialogShows(a, "file://host") || !dialogShows(a, "evil.exe") {
				t.Fatalf("n=%d h=%d: prompt must show both ends:\n%s", n, h, a.confirmPrompt.View())
			}
			sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
			if len(*opened) != 1 || (*opened)[0] != url {
				t.Errorf("n=%d h=%d: opener got %v, want the exact URL", n, h, *opened)
			}
		}
	}
}

func TestOpenLink_PromptEscapesControlAndBidiRunes(t *testing.T) {
	url := "file:///tmp/\x1b[8mhidden\u202eexe.txt"
	a, opened := confirmLinkApp(t, "see <"+url+"|label>")
	sendAndDrain(a, a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})())
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want a confirm prompt", a.mode)
	}
	view := a.confirmPrompt.View()
	if strings.Contains(view, "\x1b[8m") || strings.Contains(view, "\u202e") {
		t.Errorf("prompt shows a raw control or bidi rune: %q", view)
	}
	if !dialogShows(a, `\x1b`) || !dialogShows(a, `\u202e`) {
		t.Errorf("prompt should show visible escapes:\n%s", view)
	}
	sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(*opened) != 1 || (*opened)[0] != url {
		t.Errorf("opener got %q, want the exact original URL", *opened)
	}
}

func TestOpenLink_LinkPickerNonWebSchemeAsksFirst(t *testing.T) {
	a, opened := confirmLinkApp(t, "<https://a.example/1> <file:///tmp/x|label>")
	a.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if a.mode != ModeLinkPicker {
		t.Fatalf("mode = %v, want ModeLinkPicker", a.mode)
	}
	a.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	cmd := a.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, m := range drainCmd(cmd) {
		sendAndDrain(a, m)
	}
	if a.mode != ModeConfirm || !dialogShows(a, "file:///tmp/x") {
		t.Fatalf("mode = %v, want a confirm prompt for the picked link", a.mode)
	}
	if len(*opened) != 0 {
		t.Fatalf("opened before confirmation: %v", *opened)
	}
	sendAndDrain(a, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(*opened) != 1 || (*opened)[0] != "file:///tmp/x" {
		t.Errorf("opened = %v after confirm", *opened)
	}
}
