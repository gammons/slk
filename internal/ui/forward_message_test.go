package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestForwardMessageSelected(t *testing.T) {
	for _, pane := range []string{"channel", "thread"} {
		t.Run(pane, func(t *testing.T) {
			a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"),
				withMessages(messages.MessageItem{TS: "1.0", Text: "original", UserID: "OTHER"}))
			focusMessages(t, a)
			wantTS := ids.MessageTS("1.0")
			if pane == "thread" {
				focusThreadPanel(t, a)
				wantTS = "12.0"
				// The thread's channel must win over the active channel.
				a.activeChannelID = "COTHER"
			}
			a.compose.SetValue("unsent draft")
			a.threadCompose.SetValue("unsent reply")
			a.SetChannelFinderItems([]channelfinder.Item{
				{ID: "C2", Name: "destination", Type: "channel", Joined: true},
				{ID: "D2", Name: "alice", Type: "dm", Joined: true},
				{ID: "C3", Name: "browse only", Type: "channel"},
			})
			calls := 0
			a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
				Forward: func(ctx context.Context, team string, source ids.ChannelID, ts ids.MessageTS, dest ids.ChannelID) (core.ForwardResult, error) {
					calls++
					if team != "T1" || source != "C1" || ts != wantTS || dest != "C2" {
						t.Errorf("forward arguments = %q %q %q %q", team, source, ts, dest)
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Error("forward request has no deadline")
					}
					return core.ForwardResult{TS: "20.0", Text: "https://example.slack.com/archives/C1/p1000000"}, nil
				},
			}))
			channel, panel, selection := a.activeChannelID, a.focusedPanel, a.messagepane.SelectedIndex()
			if cmd := dispatchModeKey(a, keyPress('F')); cmd != nil {
				t.Fatal("opening picker should not perform I/O")
			}
			if a.mode != ModeChannelFinder || !a.channelFinder.IsVisible() {
				t.Fatal("f did not open channel finder")
			}
			if !strings.Contains(a.channelFinder.View(120), "Forward message to") {
				t.Fatal("picker missing forwarding title")
			}
			if got := a.channelFinder.FilteredItems(); len(got) != 2 {
				t.Fatalf("forwarding destinations = %+v, want joined conversations only", got)
			}
			// Typing uses the local matcher, without searching for unjoined channels.
			if cmd := dispatchModeKey(a, keyPress('d')); cmd != nil {
				t.Fatal("forward picker scheduled a remote channel search")
			}
			cmd := dispatchModeKey(a, keyCode(tea.KeyEnter))
			if cmd == nil || calls != 0 {
				t.Fatal("forward must be deferred to a command")
			}
			if a.mode != ModeNormal || a.channelFinder.IsVisible() {
				t.Fatal("picker not closed after selection")
			}
			if a.activeChannelID != channel || a.focusedPanel != panel || a.messagepane.SelectedIndex() != selection ||
				a.compose.Value() != "unsent draft" || a.threadCompose.Value() != "unsent reply" {
				t.Fatal("forward changed navigation, selection, focus or drafts")
			}
			// An async workspace switch must not retarget the queued operation.
			a.activeTeamID = "T2"
			result := cmd()
			forwarded, ok := result.(messageForwardedMsg)
			if !ok || forwarded.teamID != "T1" || forwarded.channelID != "C2" || forwarded.message.TS != "20.0" || calls != 1 {
				t.Fatalf("forward result = %#v, calls = %d", result, calls)
			}
			if links := messages.ExtractLinks(forwarded.message.Text); len(links) != 1 || links[0].URL != "https://example.slack.com/archives/C1/p1000000" {
				t.Fatalf("forwarded permalink is not clickable: %q", forwarded.message.Text)
			}
			a.activeTeamID = "T1"
			cmd, handled := reduceSend(a, result)
			if !handled || cmd == nil || cmd().(ToastMsg).Text != "Message forwarded to #destination" {
				t.Fatal("missing forward confirmation")
			}
		})
	}
}

func TestForwardMessageCancelRestoresSwitcher(t *testing.T) {
	for _, cancel := range []string{"escape", "outside click", "mode change"} {
		t.Run(cancel, func(t *testing.T) {
			a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"), withMessages(testMessageItems(1)...))
			focusMessages(t, a)
			a.SetChannelFinderItems(channelFinderItems())
			dispatchModeKey(a, keyPress('F'))
			if a.mode != ModeChannelFinder {
				t.Fatal("forward picker did not open")
			}
			var cmd tea.Cmd
			switch cancel {
			case "escape":
				cmd = dispatchModeKey(a, keyCode(tea.KeyEscape))
			case "outside click":
				cmd = reduceModalClick(a, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
			case "mode change":
				a.SetMode(ModeConfirm)
				a.SetMode(ModeNormal)
			}
			if cmd != nil || a.channelFinder.IsVisible() || a.mode != ModeNormal {
				t.Fatal("cancel did not close picker without sending")
			}
			dispatchModeKey(a, keyMod('t', tea.ModCtrl))
			if got := a.channelFinder.FilteredItems(); len(got) != 4 || !got[0].Synthetic {
				t.Fatalf("normal switcher destinations not restored: %+v", got)
			}
			cmd = dispatchModeKey(a, keyCode(tea.KeyEnter))
			if cmd == nil {
				t.Fatal("normal switcher did not select Threads")
			}
			if result := cmd(); result != (ThreadsViewActivatedMsg{}) {
				t.Fatalf("normal switcher dispatched %#v", result)
			}
		})
	}
}

func TestForwardMessageThreadsListDoesNotUseHiddenMessage(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"), withMessages(testMessageItems(1)...))
	focusMessages(t, a)
	a.Update(ThreadsViewActivatedMsg{})
	if cmd := dispatchModeKey(a, keyPress('F')); cmd != nil || a.mode != ModeNormal || a.channelFinder.IsVisible() {
		t.Fatal("Threads list forwarded a hidden channel message")
	}
}

func TestForwardMessageCompletionReconcilesSuppressedEcho(t *testing.T) {
	for _, order := range []string{"suppressed echo first", "echo first", "response first", "different workspace"} {
		t.Run(order, func(t *testing.T) {
			a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
			a.SetCurrentUserID("SELF")
			a.compose.SetValue("keep draft")
			a.threadCompose.SetValue("keep reply")
			item := messages.MessageItem{TS: "20.0", Text: "https://example.slack.com/archives/C2/p1000000", UserID: "SELF"}
			echo := NewMessageMsg{ChannelID: "C1", Message: item}
			completion := messageForwardedMsg{teamID: "T1", channelID: "C1", destination: "#general", message: item}
			if order == "suppressed echo first" {
				a.selfSend.MarkInFlight("C1")
			}
			if order == "different workspace" {
				a.activeTeamID = "T2"
			} else if order != "response first" {
				a.Update(echo)
			}
			a.Update(completion)
			if order == "response first" {
				a.Update(echo)
			}
			want := 1
			if order == "different workspace" {
				want = 0
			}
			got := a.messagepane.Messages()
			if len(got) != want || (want == 1 && got[0].TS != item.TS) {
				t.Fatalf("reconciled messages = %+v, want %d copies of forward", got, want)
			}
			if a.compose.Value() != "keep draft" || a.threadCompose.Value() != "keep reply" {
				t.Fatal("forward completion changed drafts")
			}
		})
	}
}

func TestForwardMessageNoSelection(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
	for _, panel := range []Panel{PanelMessages, PanelThread, PanelSidebar} {
		a.focusedPanel = panel
		if cmd := dispatchModeKey(a, keyPress('F')); cmd != nil || a.mode != ModeNormal || a.channelFinder.IsVisible() {
			t.Errorf("panel %v opened forwarding without a selected message", panel)
		}
	}
}

func TestForwardMessageClickAndFailure(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"), withMessages(testMessageItems(1)...))
	focusMessages(t, a)
	a.SetChannelFinderItems([]channelfinder.Item{{ID: "D2", Name: "alice", Type: "dm", Joined: true}})
	calls := 0
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Forward: func(_ context.Context, _ string, _ ids.ChannelID, _ ids.MessageTS, dest ids.ChannelID) (core.ForwardResult, error) {
			calls++
			if dest != "D2" {
				t.Errorf("destination = %s, want D2", dest)
			}
			return core.ForwardResult{}, errors.New("restricted_action")
		},
	}))
	dispatchModeKey(a, keyPress('F'))
	if a.mode != ModeChannelFinder {
		t.Fatal("forward picker did not open")
	}
	w, h := a.channelFinder.BoxSize(a.width, a.height)
	cmd := reduceModalClick(a, tea.MouseClickMsg{X: (a.width-w)/2 + 3, Y: (a.height-h)/2 + 5, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("row click did not forward")
	}
	result := cmd()
	toast, ok := result.(ToastMsg)
	if !ok || !strings.Contains(toast.Text, "Failed to forward") || !strings.Contains(toast.Text, "restricted_action") || calls != 1 {
		t.Fatalf("failure = %#v, calls = %d", result, calls)
	}
}

func TestForwardMessageWorkspaceChangeBeforeSelection(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"), withMessages(testMessageItems(1)...))
	focusMessages(t, a)
	a.SetChannelFinderItems(channelFinderItems())
	dispatchModeKey(a, keyPress('F'))
	if a.mode != ModeChannelFinder {
		t.Fatal("forward picker did not open")
	}
	a.activeTeamID = "T2"
	cmd := dispatchModeKey(a, keyCode(tea.KeyEnter))
	if a.mode != ModeNormal || a.channelFinder.IsVisible() || cmd == nil {
		t.Fatal("workspace mismatch should cancel forwarding with feedback")
	}
	if result, ok := cmd().(ToastMsg); !ok || !strings.Contains(result.Text, "workspace changed") {
		t.Fatalf("workspace mismatch result = %#v", result)
	}
}
