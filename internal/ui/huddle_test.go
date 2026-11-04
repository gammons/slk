// internal/ui/huddle_test.go
//
// Copying a conversation's huddle deep link: the H key in Normal mode,
// the :huddle command (the same handler), and the no-conversation
// refusal. See cmdHuddle (command.go) and slackurl.HuddleURL.
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// copyHuddleApp returns an App on C1/T1 whose clipboard writes are
// recorded.
func copyHuddleApp(t *testing.T, opts ...testOpt) (*App, *string) {
	t.Helper()
	opts = append([]testOpt{withActiveChannel("C1")}, opts...)
	a := newTestApp(t, opts...)
	a.activeTeamID = "T1"
	var copied string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	return a, &copied
}

// drainHuddleBatch walks a tea.BatchMsg for the clipboard text the
// writer recorded (the writer itself records it; this only asserts the
// batch ran) and whether the batch carries a toast clear tick.
func drainHuddleBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd = nil")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			_ = c()
		}
	}
}

func TestHuddleKey_CopiesLink(t *testing.T) {
	a, copied := copyHuddleApp(t, withMessages(testMessageItems(3)...))
	focusMessages(t, a)

	_, cmd := a.Update(keyPress('H'))
	drainHuddleBatch(t, cmd)

	if *copied != "https://app.slack.com/huddle/T1/C1" {
		t.Errorf("clipboard = %q, want the T1/C1 huddle link", *copied)
	}
	if got := statusbarText(a); !containsToast(got, "Copied huddle link") {
		t.Errorf("statusbar = %q, want a Copied huddle link toast", got)
	}
}

func TestHuddleKey_NoConversationRefusal(t *testing.T) {
	a, copied := copyHuddleApp(t, withActiveChannel(""))
	a.focusedPanel = PanelMessages

	_, cmd := a.Update(keyPress('H'))
	drainHuddleBatch(t, cmd)

	if *copied != "" {
		t.Errorf("clipboard = %q, want nothing copied without a conversation", *copied)
	}
	if got := statusbarText(a); !containsToast(got, "No conversation open") {
		t.Errorf("statusbar = %q, want a No conversation open toast", got)
	}
}

func TestHuddleCommand_MatchesTheKey(t *testing.T) {
	a, copied := copyHuddleApp(t, withMessages(testMessageItems(3)...))

	cmd := executeCommand(a, "huddle")
	drainHuddleBatch(t, cmd)

	if *copied != "https://app.slack.com/huddle/T1/C1" {
		t.Errorf("clipboard = %q, want the T1/C1 huddle link", *copied)
	}
}

func TestHuddleToast_Clears(t *testing.T) {
	a, _ := copyHuddleApp(t, withMessages(testMessageItems(3)...))
	focusMessages(t, a)

	_, cmd := a.Update(keyPress('H'))
	msgs := deliveredMsgs[statusbar.CopiedClearMsg](t, cmd)
	if len(msgs) != 1 {
		t.Fatalf("clear ticks = %d, want 1", len(msgs))
	}
	a.Update(msgs[0])
	if got := statusbarText(a); strings.Contains(got, "Copied huddle link") {
		t.Errorf("statusbar = %q after the clear tick, want the toast gone", got)
	}
}

// A thread opened from the Threads view belongs to another channel
// than a.activeChannelID; the focused thread pane's channel wins.
func TestHuddleKey_FocusedThreadUsesItsOwnChannel(t *testing.T) {
	a, copied := copyHuddleApp(t, withMessages(testMessageItems(3)...))
	a.threadPanel.SetThread(testMessageItems(1)[0], nil, "C2", "1.0")
	a.threadVisible = true
	a.focusedPanel = PanelThread

	_, cmd := a.Update(keyPress('H'))
	drainHuddleBatch(t, cmd)

	if *copied != "https://app.slack.com/huddle/T1/C2" {
		t.Errorf("clipboard = %q, want the thread's own channel C2", *copied)
	}
}

// The Threads and Activity views leave activeChannelID pointing at the
// last-opened channel, so the highlighted row's channel is what on
// screen means there. With no highlighted row there is nothing to copy.
func TestHuddleKey_ListViewsUseTheHighlightedRow(t *testing.T) {
	t.Run("threads view", func(t *testing.T) {
		a, copied := copyHuddleApp(t, withThreadsView([]core.ThreadSummary{
			{ChannelID: "C1", ThreadTS: "1.0"},
			{ChannelID: "C2", ThreadTS: "2.0"},
		}), withActiveChannel("C1"))
		a.view = ViewThreads

		_, cmd := a.Update(keyPress('H'))
		drainHuddleBatch(t, cmd)
		if *copied != "https://app.slack.com/huddle/T1/C1" {
			t.Errorf("clipboard = %q, want the highlighted row's C1", *copied)
		}

		// Move the selection to the C2 row and copy again.
		a.threadsView.MoveDown()
		_, cmd = a.Update(keyPress('H'))
		drainHuddleBatch(t, cmd)
		if *copied != "https://app.slack.com/huddle/T1/C2" {
			t.Errorf("clipboard = %q, want the now-highlighted C2", *copied)
		}
	})

	t.Run("threads view, empty list refuses", func(t *testing.T) {
		a, copied := copyHuddleApp(t, withThreadsView(nil), withActiveChannel("C1"))
		a.view = ViewThreads

		_, cmd := a.Update(keyPress('H'))
		drainHuddleBatch(t, cmd)
		if *copied != "" {
			t.Errorf("clipboard = %q, want nothing copied without a highlighted row", *copied)
		}
		if got := statusbarText(a); !containsToast(got, "No conversation open") {
			t.Errorf("statusbar = %q, want the refusal toast", got)
		}
	})

	t.Run("activity view", func(t *testing.T) {
		a, copied := copyHuddleApp(t, withActiveChannel("C1"))
		a.view = ViewActivity
		a.activityView.SetItems([]core.ActivityItem{
			{Key: "k1", ChannelID: "C9"},
		})

		_, cmd := a.Update(keyPress('H'))
		drainHuddleBatch(t, cmd)
		if *copied != "https://app.slack.com/huddle/T1/C9" {
			t.Errorf("clipboard = %q, want the highlighted activity row's C9", *copied)
		}
	})
}
