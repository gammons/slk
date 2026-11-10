package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/help"
)

// Keys for the draft preview go through a.Update, not runKeyCases, so
// the gates ahead of the mode handler (the upload guard) run too.

var ctrlR = keyMod('r', tea.ModCtrl)

func previewKeysApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, append(insertOpts(), withSize(120, 40), withActiveTeam("T1"), withMode(ModeInsert))...)
	a.SetCurrentUserID("U1")
	a.focusedPanel = PanelMessages
	a.compose.SetDraftContext("T1", "C1", "")
	_ = a.compose.Focus()
	return a
}

func send(a *App, k tea.Msg) tea.Cmd {
	_, cmd := a.Update(k)
	return cmd
}

// previewOnWithCtrlR presses ctrl+r and fails unless it showed c's
// draft preview, so a test of what closes it can't pass vacuously.
func previewOnWithCtrlR(t *testing.T, a *App, c *compose.Model) {
	t.Helper()
	send(a, ctrlR)
	if !c.PreviewOn() {
		t.Fatal("precondition: ctrl+r did not show the draft preview")
	}
}

func TestCtrlRTogglesChannelPreview(t *testing.T) {
	a := previewKeysApp(t)
	a.compose.SetValue("**hi**")
	send(a, ctrlR)
	if !a.compose.PreviewOn() {
		t.Fatal("ctrl+r in insert mode did not show the draft preview")
	}
	send(a, ctrlR)
	if a.compose.PreviewOn() {
		t.Fatal("second ctrl+r did not hide the draft preview")
	}
	if got := a.compose.Value(); got != "**hi**" {
		t.Fatalf("ctrl+r changed the draft to %q", got)
	}
}

func TestCtrlRTogglesOnlyTheThreadBox(t *testing.T) {
	a := previewKeysApp(t)
	focusThreadPanel(t, a)
	send(a, ctrlR)
	if !a.threadCompose.PreviewOn() {
		t.Fatal("ctrl+r with the thread focused did not show its preview")
	}
	if a.compose.PreviewOn() {
		t.Fatal("ctrl+r with the thread focused showed the channel box's preview")
	}
}

func TestCtrlRInNormalModeDoesNothing(t *testing.T) {
	a := previewKeysApp(t)
	a.SetMode(ModeNormal)
	send(a, ctrlR)
	if a.compose.PreviewOn() || a.threadCompose.PreviewOn() {
		t.Fatal("ctrl+r in normal mode showed a draft preview")
	}
	if a.mode != ModeNormal {
		t.Fatalf("ctrl+r in normal mode changed the mode to %v", a.mode)
	}
}

func TestSendClosesPreview(t *testing.T) {
	a := previewKeysApp(t)
	a.compose.SetValue("hello")
	previewOnWithCtrlR(t, a, &a.compose)
	send(a, keyCode(tea.KeyEnter))
	if a.compose.PreviewOn() {
		t.Fatal("sending from the channel box left its preview on")
	}

	a = previewKeysApp(t)
	focusThreadPanel(t, a)
	_ = a.threadCompose.Focus()
	a.threadCompose.SetValue("hello")
	previewOnWithCtrlR(t, a, &a.threadCompose)
	send(a, keyCode(tea.KeyEnter))
	if a.threadCompose.PreviewOn() {
		t.Fatal("sending from the thread box left its preview on")
	}
}

func TestEscKeepsPreview(t *testing.T) {
	a := previewKeysApp(t)
	send(a, ctrlR)
	send(a, keyCode(tea.KeyEscape))
	if a.mode != ModeNormal {
		t.Fatalf("precondition: Esc left mode %v", a.mode)
	}
	send(a, keyPress('i'))
	if a.mode != ModeInsert {
		t.Fatalf("precondition: i left mode %v", a.mode)
	}
	if !a.compose.PreviewOn() {
		t.Fatal("Esc and i again closed the draft preview")
	}
}

func TestCtrlUKeepsPreview(t *testing.T) {
	a := previewKeysApp(t)
	a.compose.SetValue("x")
	send(a, ctrlR)
	send(a, keyMod('u', tea.ModCtrl))
	if got := a.compose.Value(); got != "" {
		t.Fatalf("precondition: ctrl+u left %q", got)
	}
	if !a.compose.PreviewOn() {
		t.Fatal("ctrl+u closed the draft preview")
	}
}

func TestEditSaveAndCancelClosePreview(t *testing.T) {
	a := previewKeysApp(t)
	a.SetMode(ModeNormal)
	focusMessages(t, a)
	send(a, a.beginEditOfSelected())
	if !a.editing.IsActive() {
		t.Fatal("precondition: no edit in progress")
	}
	previewOnWithCtrlR(t, a, &a.compose)
	send(a, keyCode(tea.KeyEscape))
	if a.editing.IsActive() {
		t.Fatal("precondition: Esc did not cancel the edit")
	}
	if a.compose.PreviewOn() {
		t.Fatal("cancelling an edit left the draft preview on")
	}

	a.SetMode(ModeNormal)
	a.beginEditOfSelected()
	previewOnWithCtrlR(t, a, &a.compose)
	sel, _ := a.messagepane.SelectedMessage()
	send(a, MessageEditedMsg{ChannelID: "C1", TS: sel.TS})
	if a.editing.IsActive() {
		t.Fatal("precondition: the saved edit is still active")
	}
	if a.compose.PreviewOn() {
		t.Fatal("saving an edit left the draft preview on")
	}
}

func TestSlashCommandClosesPreview(t *testing.T) {
	a, _ := slashApp(t, nil)
	withSlashBuiltin(t, "noop", func(*App, commandArgs) tea.Cmd { return nil })
	_ = a.compose.Focus()
	a.compose.SetValue("/noop")
	previewOnWithCtrlR(t, a, &a.compose)
	pressEnter(a)
	if a.compose.PreviewOn() {
		t.Fatal("running a slash command left the draft preview on")
	}
}

func TestHelpListsDraftPreview(t *testing.T) {
	for _, e := range help.FromKeyMap(DefaultKeyMap()) {
		if e.Key == "ctrl+r" && e.Desc == "show / hide the draft preview" {
			return
		}
	}
	t.Fatal("the help overlay does not list ctrl+r for the draft preview")
}
