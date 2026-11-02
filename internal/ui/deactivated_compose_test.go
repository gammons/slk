package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

const deactivatedNotice = "You can't send messages to a deactivated account"

func selectDM(a *App, id string) {
	a.Update(ChannelSelectedMsg{ID: id, Name: id, Type: "dm"})
}

func TestDeactivatedDMDisablesComposer(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true})
	selectDM(a, "D2")

	if !a.composeDisabled {
		t.Fatal("composer enabled in a DM with a deactivated account")
	}
	if enterSends(a) {
		t.Error("Enter sent a message to a deactivated account")
	}
	a.SetMode(ModeNormal)
	a.Update(keyPress('i'))
	if a.mode != ModeNormal {
		t.Errorf("i entered insert mode (mode=%v)", a.mode)
	}
	if got := statusbarText(a); !strings.Contains(got, deactivatedNotice) {
		t.Errorf("statusbar = %q, want the deactivated notice", got)
	}

	// Another DM is sendable, and the option does not matter: Slack
	// refuses the post whether the DM is hidden or not.
	selectDM(a, "D1")
	if a.composeDisabled {
		t.Error("composer of an active person's DM disabled")
	}
	a.SetHideDeactivatedUsers(false)
	selectDM(a, "D2")
	if !a.composeDisabled {
		t.Error("composer enabled with hide_deactivated_users off")
	}
}

func TestDeactivatedDMComposerNotice(t *testing.T) {
	a := newTestApp(t, withSize(140, 30))
	a.SetChannels(deactivatedApp().sidebar.Items())
	a.SetDeactivatedUsers(map[string]bool{"U2": true})
	selectDM(a, "D2")
	if got := stripANSI(a.View().Content); !strings.Contains(got, deactivatedNotice+".") {
		t.Errorf("composer area does not show the deactivated notice:\n%s", got)
	}
}

func TestLiveDeactivationOfTheOpenDM(t *testing.T) {
	a := deactivatedApp()
	selectDM(a, "D2")
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.compose.Focus()

	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: true})
	if !a.composeDisabled || a.mode != ModeNormal {
		t.Fatalf("composeDisabled=%v mode=%v, want disabled and out of insert", a.composeDisabled, a.mode)
	}

	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: false})
	if a.composeDisabled {
		t.Error("composer still disabled after a reactivation")
	}

	// A reactivation keeps a read-only mark.
	failSend(a, "D2", readOnlyReason)
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: true})
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: false})
	if !a.composeDisabled {
		t.Error("a reactivation re-enabled a read-only conversation")
	}
}

func TestDeactivatedDMRefusesThreadReply(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true})
	selectDM(a, "D1")
	// The thread is in D2 while the channel pane shows D1.
	a.threadPanel.SetThread(
		messages.MessageItem{TS: "10.0", UserID: "U2", Text: "parent", ThreadTS: "10.0"},
		nil, "D2", "10.0")
	a.threadVisible = true
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelThread
	a.threadCompose.SetValue("reply")
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(SendThreadReplyMsg); ok {
			t.Fatal("thread reply sent to a deactivated account")
		}
	}
	if got := statusbarText(a); !strings.Contains(got, deactivatedNotice) {
		t.Errorf("statusbar = %q, want the deactivated notice", got)
	}
}

func TestDeactivationBeforeTheConversationRow(t *testing.T) {
	// cmd/slk reports the deactivated peer before the conversation's
	// row, so the DM on screen learns its peer only from the row.
	a := deactivatedApp()
	selectDM(a, "D9")
	a.Update(UserDeactivatedMsg{UserID: "U9", Deactivated: true})
	if a.composeDisabled {
		t.Fatal("composer disabled before the peer was known")
	}
	a.Update(ConversationOpenedMsg{
		TeamID:     "T1",
		Item:       sidebar.ChannelItem{ID: "D9", Name: "Left", Type: "dm", DMUserID: "U9"},
		FinderItem: core.ChannelFinderItem{ID: "D9", Name: "Left", Type: "dm", DMUserID: "U9", Joined: true},
	})
	if !a.composeDisabled {
		t.Error("composer enabled after the row named a deactivated peer")
	}
}

func TestDeactivatedAppDMOutranksAWritableProbe(t *testing.T) {
	// A1 is only in the sidebar, so its peer comes from there.
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U3": true})
	a.Update(ChannelSelectedMsg{ID: "A1", Name: "A1", Type: "app"})
	a.Update(ChannelMessagingCapabilityMsg{ChannelID: "A1", CanSend: true})
	if !a.composeDisabled {
		t.Fatal("a CanSend:true probe re-enabled a DM with a deactivated app")
	}
	if got := a.composeBlockedText(); got != deactivatedToast {
		t.Errorf("reason = %q, want %q", got, deactivatedToast)
	}
}
