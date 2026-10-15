package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

const readOnlyReason = "sending message: restricted_action_read_only_channel"

func failSend(a *App, ch, reason string) {
	a.Update(MessageSendFailedMsg{ChannelID: ch, Reason: reason})
}

func selectChannel(a *App, id string) {
	a.Update(ChannelSelectedMsg{ID: id, Name: id, Type: "channel"})
}

// Enter must not send; reports whether a SendMessageMsg came out.
func enterSends(a *App) bool {
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.compose.SetValue("hello")
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(SendMessageMsg); ok {
			return true
		}
	}
	return false
}

func TestReadOnlySendFailureDisablesComposer(t *testing.T) {
	a := NewApp()
	selectChannel(a, "X")

	failSend(a, "X", readOnlyReason)

	if !a.composeDisabled || !a.readOnlyChannels["X"] {
		t.Fatalf("composeDisabled=%v mark=%v, want both true", a.composeDisabled, a.readOnlyChannels["X"])
	}
	if got := statusbarText(a); !strings.Contains(got, "This conversation is read-only") {
		t.Errorf("statusbar = %q, want the read-only notice", got)
	}
	if enterSends(a) {
		t.Error("Enter sent a message in a read-only conversation")
	}
	a.SetMode(ModeNormal)
	a.Update(keyPress('i'))
	if a.mode != ModeNormal {
		t.Errorf("i entered insert mode in a read-only conversation (mode=%v)", a.mode)
	}
	if got := statusbarText(a); !strings.Contains(got, "read-only") {
		t.Errorf("statusbar = %q, want the read-only notice on i", got)
	}
}

func TestComposerNoticeWording(t *testing.T) {
	const appNotice = "You can't send messages to this app."
	const roNotice = "This conversation is read-only."
	cases := []struct {
		name     string
		disabled bool
		marked   bool
		want     string
	}{
		{"read-only mark", true, true, roNotice},
		{"probe-disabled app DM", true, false, appNotice},
		{"marked and probed", true, true, roNotice},
		{"sendable", false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t, withSize(80, 30))
			a.activeChannelID = "X"
			a.composeDisabled = c.disabled
			if c.marked {
				a.readOnlyChannels = map[string]bool{"X": true}
			}
			got := a.renderChannelMessagesPanel(60, 1, 20, false, false, 0)
			for _, n := range []string{appNotice, roNotice} {
				if has := strings.Contains(got, n); has != (n == c.want) {
					t.Errorf("contains %q = %v, want %v", n, has, n == c.want)
				}
			}
		})
	}
}

func TestReadOnlyMarkSurvivesSwitches(t *testing.T) {
	a := NewApp()
	selectChannel(a, "X")
	failSend(a, "X", readOnlyReason)

	selectChannel(a, "Y")
	if a.composeDisabled {
		t.Error("composer still disabled on an unrelated channel Y")
	}
	selectChannel(a, "X")
	if !a.composeDisabled {
		t.Error("composer not disabled at once on returning to X")
	}
}

func TestOtherSendFailuresDoNotMark(t *testing.T) {
	for _, reason := range []string{"sending message: rate_limited", "sending message: not_in_channel", ""} {
		a := NewApp()
		selectChannel(a, "X")
		failSend(a, "X", reason)
		if a.composeDisabled || a.readOnlyChannels["X"] {
			t.Errorf("reason %q marked the channel read-only", reason)
		}
	}
}

func TestReadOnlyMarkForInactiveChannel(t *testing.T) {
	a := NewApp()
	selectChannel(a, "Y")

	failSend(a, "X", "restricted_action_read_only")

	if a.composeDisabled {
		t.Error("a mark for X disabled the composer of the active channel Y")
	}
	selectChannel(a, "X")
	if !a.composeDisabled {
		t.Error("X not disabled when selected after being marked while inactive")
	}
}

func TestReadOnlyMarkOutlivesLaterProbe(t *testing.T) {
	a := NewApp()
	selectChannel(a, "X")
	failSend(a, "X", readOnlyReason)

	a.Update(ChannelMessagingCapabilityMsg{ChannelID: "X", CanSend: true})

	if !a.composeDisabled {
		t.Error("a CanSend:true probe result re-enabled a read-only conversation")
	}
}

func TestReadOnlyFailureInInsertModeLeavesInsert(t *testing.T) {
	a := NewApp()
	selectChannel(a, "X")
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.compose.Focus()

	failSend(a, "X", readOnlyReason)

	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal", a.mode)
	}
}

func TestProbeUnsendableBlocksThreadReply(t *testing.T) {
	sends := func(a *App) bool {
		a.SetMode(ModeInsert)
		a.focusedPanel = PanelThread
		a.threadVisible = true
		a.threadCompose.SetValue("reply")
		_, cmd := a.Update(keyCode(tea.KeyEnter))
		for _, m := range drainCmd(cmd) {
			if _, ok := m.(SendThreadReplyMsg); ok {
				return true
			}
		}
		return false
	}

	a := NewApp()
	selectChannel(a, "C1")
	showThread(t, a)
	a.Update(ChannelMessagingCapabilityMsg{ChannelID: "C1", CanSend: false})
	if sends(a) {
		t.Error("thread reply sent in an app DM the probe found unsendable")
	}
	if got := statusbarText(a); !strings.Contains(got, "can't send messages to this app") {
		t.Errorf("statusbar = %q, want the app notice", got)
	}

	// A later writable verdict lifts the block.
	a.Update(ChannelMessagingCapabilityMsg{ChannelID: "C1", CanSend: true})
	if !sends(a) {
		t.Error("thread reply blocked after a CanSend:true probe")
	}

	// A verdict for another channel does not block this thread.
	b := NewApp()
	selectChannel(b, "C1")
	showThread(t, b)
	b.Update(ChannelMessagingCapabilityMsg{ChannelID: "C2", CanSend: false})
	if !sends(b) {
		t.Error("an unrelated channel's verdict blocked this thread")
	}
}

func TestUnsendableVerdictSeedsComposerOnReturn(t *testing.T) {
	a := NewApp()
	selectChannel(a, "X")
	a.Update(ChannelMessagingCapabilityMsg{ChannelID: "X", CanSend: false})
	selectChannel(a, "Y")
	if a.composeDisabled {
		t.Fatal("composer of Y disabled by X's verdict")
	}

	selectChannel(a, "X")
	if !a.composeDisabled {
		t.Error("composer not disabled on returning to a channel the probe found unsendable")
	}
}

func TestReadOnlyThreadReplyFailure(t *testing.T) {
	a := NewApp()
	selectChannel(a, "C1")
	showThread(t, a)

	a.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "10.0", Reason: readOnlyReason})

	if !a.readOnlyChannels["C1"] {
		t.Fatal("thread reply failure did not mark the channel")
	}
	if got := statusbarText(a); !strings.Contains(got, "This conversation is read-only") {
		t.Errorf("statusbar = %q, want the read-only notice", got)
	}
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelThread
	a.threadVisible = true
	a.threadCompose.SetValue("reply")
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(SendThreadReplyMsg); ok {
			t.Error("Enter sent a thread reply into a read-only conversation")
		}
	}

	// Other reasons do not mark.
	b := NewApp()
	selectChannel(b, "C1")
	b.Update(ThreadReplySendFailedMsg{ChannelID: "C1", ThreadTS: "10.0", Reason: "rate_limited"})
	if b.readOnlyChannels["C1"] {
		t.Error("rate_limited marked the channel read-only")
	}
}
