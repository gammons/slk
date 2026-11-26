package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

// Clicking a link in an Activity card opens the link; the card's own
// select/open behavior is for the rest of the rows.
func TestActivityCardLinkClickOpens(t *testing.T) {
	a := newTestApp(t, withSize(120, 40), withView(ViewActivity), withActiveTeam("T1"))
	item := core.ActivityItem{
		Key: "k1", Type: "at_user", FeedTS: "1700000000.000100",
		ChannelID: "C1", TS: "1700000000.000100", AuthorID: "U1",
	}
	a.activityView.SetItems([]core.ActivityItem{item})
	a.activityView.SetBodies(map[string]core.ActivityMessage{
		core.ActivityMsgKey("C1", item.TS): {Text: "see <https://example.com/x|label> here", UserID: "U1"},
	})
	_ = a.View()
	_ = a.View()

	row, col := -1, -1
	for y, line := range strings.Split(ansi.Strip(a.View().Content), "\n") {
		if i := strings.Index(line, "label"); i >= 0 {
			row, col = y, ansi.StringWidth(line[:i])
			break
		}
	}
	if row < 0 {
		t.Fatal("the activity card does not draw the link")
	}
	_, cmd := a.Update(tea.MouseClickMsg{X: col, Y: row, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("the click produced no command")
	}
	msg := cmd()
	link, ok := msg.(OpenLinkMsg)
	if !ok {
		t.Fatalf("click produced %T, want OpenLinkMsg", msg)
	}
	if link.URL != "https://example.com/x" {
		t.Errorf("opened %q, want the card's link", link.URL)
	}
}

func activityLinkApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, withSize(120, 40), withView(ViewActivity), withActiveTeam("T1"))
	// Activity draws in the content pane; y and o act on it only from there.
	a.focusedPanel = PanelMessages
	item := core.ActivityItem{
		Key: "k1", Type: "at_user", FeedTS: "1700000000.000100",
		ChannelID: "C1", TS: "1700000000.000100", AuthorID: "U1",
	}
	a.activityView.SetItems([]core.ActivityItem{item})
	a.activityView.SetBodies(map[string]core.ActivityMessage{
		core.ActivityMsgKey("C1", item.TS): {Text: "see <https://example.com/x|label> here", UserID: "U1"},
	})
	return a
}

// `o` acts on the selected Activity card, not the hidden message pane.
func TestActivityOpenLinksUsesTheCard(t *testing.T) {
	a := activityLinkApp(t)
	_, cmd := a.Update(keyPress('o'))
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	msg := cmd()
	if link, ok := msg.(OpenLinkMsg); !ok || link.URL != "https://example.com/x" {
		t.Fatalf("o produced %#v, want the card's link", msg)
	}
}

// `y` copies the selected Activity card's body.
func TestActivityCopyMessageUsesTheCard(t *testing.T) {
	a := activityLinkApp(t)
	var copied string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	_, cmd := a.Update(keyPress('y'))
	for _, m := range drainBatch(cmd) {
		if m != nil {
			a.Update(m)
		}
	}
	if !strings.Contains(copied, "example.com/x") {
		t.Errorf("clipboard = %q, want the card's text with its link", copied)
	}
}

// A selected card whose body has not loaded yet answers y and o with a
// toast instead of doing nothing.
func TestActivityKeysToastWhenCardBodyNotLoaded(t *testing.T) {
	a := activityLinkApp(t)
	a.activityView.SetBodies(nil)
	for _, key := range []rune{'y', 'o'} {
		_, cmd := a.Update(keyPress(key))
		if cmd == nil {
			t.Fatalf("%c produced no command", key)
		}
		msg := cmd()
		if toast, ok := msg.(ToastMsg); !ok || toast.Text != "Message not loaded yet" {
			t.Errorf("%c produced %#v, want the not-loaded toast", key, msg)
		}
	}
}

// With the thread pane focused, y/o target the thread reply, never the
// Activity card behind it.
func TestActivityKeysYieldToTheFocusedThread(t *testing.T) {
	a := activityLinkApp(t)
	a.focusedPanel = PanelThread
	a.threadVisible = true
	if cmd := a.copyMessageOfSelected(); cmd != nil {
		t.Error("y copied the Activity card while the thread was focused")
	}
	if cmd := a.openLinksOfSelected(); cmd != nil {
		t.Error("o opened the Activity card's links while the thread was focused")
	}
}
