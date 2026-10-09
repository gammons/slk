package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/wintree"
	"github.com/gammons/slk/internal/ui/workspace"
)

// TestDraftSafety_ChannelPickerSwitchKeepsDraft pins that an open
// channel picker cannot smuggle text into a channel the user switched
// to: switching away mid-query saves the draft under the original
// channel (not the destination), sends nothing, and restores the
// original draft on return.
func TestDraftSafety_ChannelPickerSwitchKeepsDraft(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))

	a.SetInitialChannel("C1", "alpha", nil)
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "alpha", Type: "channel"},
		{ID: "C2", Name: "beta", Type: "channel"},
	})

	_, _ = a.Update(keyPress('i'))
	for _, r := range "hello #" {
		a.Update(keyPress(r))
	}

	if !a.compose.IsChannelActive() {
		t.Fatal("precondition: typing '#' did not open the channel picker")
	}

	// Switch to C2 while the picker is open. The draft must not follow.
	a.Update(ChannelSelectedMsg{ID: "C2", Name: "beta", Type: "channel"})

	// Enter on the (empty) destination sends nothing and the picker is gone.
	a.Update(keyCode(tea.KeyEnter))
	if got := a.compose.Value(); got != "" {
		t.Errorf("destination C2 draft = %q, want empty", got)
	}
	if a.compose.IsChannelActive() {
		t.Error("channel picker survived the channel switch")
	}

	// Returning to C1 restores the pre-switch draft.
	a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})
	if got := a.compose.Value(); got != "hello #" {
		t.Errorf("returning to C1: draft = %q, want %q", got, "hello #")
	}
}

// TestDraftSafety_UploadInFlightBlocksWindowOps pins that an in-flight
// upload freezes window management (a no-op, not a channel switch), so
// the uploading window's caption cannot be stranded or resurrected,
// and a failed upload preserves the source text + attachments.
func TestDraftSafety_UploadInFlightBlocksWindowOps(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))

	a.SetInitialChannel("C1", "alpha", nil)
	a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})

	first := a.focusedWin

	// Split off a second window and bind it to C2 with its own draft.
	a.splitWindow(wintree.SplitSideBySide)
	a.Update(ChannelSelectedMsg{ID: "C2", Name: "beta", Type: "channel"})
	a.compose.SetValue("other draft")
	second := a.focusedWin

	// Back to C1 with a caption + attachment queued for upload.
	a.focusWindow(first)
	const caption = "upload caption"
	a.compose.SetValue(caption)
	a.compose.AddAttachment(core.PendingAttachment{Filename: "shot.png", Bytes: []byte("x")})

	// An unrelated thread draft that must not be disturbed.
	a.threadCompose.SetValue("thread keep")

	a.setUploaderForTest(func(channelID, threadTS, captionText string, attachments []core.PendingAttachment) tea.Cmd {
		return nil
	})
	a.submitWithAttachments(&a.compose)
	if !a.compose.Uploading() {
		t.Fatal("precondition: main compose did not enter the uploading state")
	}

	// Window churn while uploading is refused wholesale: focus cannot
	// move, no window opens or closes, and the caption is untouched.
	a.focusWindow(second)
	a.closeWindow()
	a.splitWindow(wintree.SplitSideBySide)
	a.onlyWindow()

	if a.focusedWin != first {
		t.Errorf("focused window changed during upload: got %v, want %v", a.focusedWin, first)
	}
	if got := a.wins.Len(); got != 2 {
		t.Errorf("window count changed during upload: got %d, want 2", got)
	}
	if got := a.compose.Value(); got != caption {
		t.Errorf("caption changed during upload: got %q, want %q", got, caption)
	}

	// Successful upload clears only the source composer.
	a.Update(UploadResultMsg{})
	if got := a.compose.Value(); got != "" {
		t.Errorf("main draft after success = %q, want empty", got)
	}
	if got := a.compose.Attachments(); len(got) != 0 {
		t.Errorf("main attachments after success = %d, want 0", len(got))
	}
	if got := a.threadCompose.Value(); got != "thread keep" {
		t.Errorf("thread draft clobbered by main upload: got %q, want %q", got, "thread keep")
	}

	// The other window's draft is still intact.
	a.focusWindow(second)
	if got := a.compose.Value(); got != "other draft" {
		t.Errorf("second window draft = %q, want %q", got, "other draft")
	}

	// Returning to the source window must not resurrect the cleared caption.
	a.focusWindow(first)
	if got := a.compose.Value(); got != "" {
		t.Errorf("caption resurrected on return: got %q, want empty", got)
	}

	// A failed upload preserves the source caption + attachment.
	a.compose.SetValue("retry caption")
	a.compose.AddAttachment(core.PendingAttachment{Filename: "retry.png", Bytes: []byte("y")})
	a.submitWithAttachments(&a.compose)
	a.Update(UploadResultMsg{Err: errors.New("network")})

	if got := a.compose.Value(); got != "retry caption" {
		t.Errorf("caption after failed upload = %q, want %q", got, "retry caption")
	}
	if got := len(a.compose.Attachments()); got != 1 {
		t.Errorf("attachments after failed upload = %d, want 1", got)
	}
	if a.compose.Uploading() {
		t.Error("still uploading after failure; retry would be refused")
	}
}

// TestDraftSafety_SendTargetsCaptureChannel pins that a queued send
// resolves to the channel it was composed in, even if the active
// channel switches before the command runs, and that clearing the
// composer is durable across a switch away and back.
func TestDraftSafety_SendTargetsCaptureChannel(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))

	a.SetInitialChannel("C1", "alpha", nil)
	a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})

	a.compose.SetValue("send me")
	a.SetMode(ModeInsert)
	cmd := a.handleInsertMode(keyCode(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter with text produced no send command")
	}

	// The channel changes before the send command is executed.
	a.Update(ChannelSelectedMsg{ID: "C2", Name: "beta", Type: "channel"})

	msg, ok := cmd().(SendMessageMsg)
	if !ok {
		t.Fatalf("send command returned %T, want SendMessageMsg", cmd())
	}
	if msg.ChannelID != "C1" {
		t.Errorf("send target = %q, want %q (composed-in channel)", msg.ChannelID, "C1")
	}
	if msg.Text != "send me" {
		t.Errorf("send text = %q, want %q", msg.Text, "send me")
	}

	// Back on C1 the composer is empty: the send consumed the draft.
	a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})
	if got := a.compose.Value(); got != "" {
		t.Errorf("C1 draft after send = %q, want empty", got)
	}

	// Ctrl+U clears the composer, and the empty draft stays empty
	// across a switch away and back.
	a.compose.SetValue("clear me")
	a.SetMode(ModeInsert)
	a.Update(keyMod('u', tea.ModCtrl))
	a.Update(ChannelSelectedMsg{ID: "C2", Name: "beta", Type: "channel"})
	a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})
	if got := a.compose.Value(); got != "" {
		t.Errorf("cleared draft resurrected after round trip: got %q, want empty", got)
	}
}

// TestDraftSafety_ParentDeletedDuringUploadClosesThreadAfter pins that
// a WS delete of the open thread's parent, landing while an upload is
// in flight, still closes the thread once the upload ends. CloseThread
// refuses during an upload, and the delete event is not redelivered.
func TestDraftSafety_ParentDeletedDuringUploadClosesThreadAfter(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"failure", errors.New("network")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))
			a.SetInitialChannel("C1", "alpha", nil)
			a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")

			a.threadCompose.SetValue("caption")
			a.threadCompose.AddAttachment(core.PendingAttachment{Filename: "t.png", Bytes: []byte("x")})
			a.setUploaderForTest(func(_, _, _ string, _ []core.PendingAttachment) tea.Cmd { return nil })
			a.submitWithAttachments(&a.threadCompose)
			if !a.threadCompose.Uploading() {
				t.Fatal("precondition: thread compose did not enter the uploading state")
			}

			a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "100.0"})
			if !a.threadVisible {
				t.Fatal("thread closed mid-upload; the uploading caption would be detached")
			}

			a.Update(UploadResultMsg{Err: tc.err})
			if a.threadVisible {
				t.Error("thread of a deleted parent still open after the upload ended")
			}
		})
	}
}

// uploadGuardApp builds an App with workspaces T1/T2, channel C1, a
// thread open on parent 100.0, and the main composer mid-upload. The
// thread composer holds an unrelated draft. It returns the number of
// uploader calls and the team IDs the workspace switcher was asked for.
func uploadGuardApp(t *testing.T) (a *App, uploads *int, switched *[]string) {
	t.Helper()
	a = newTestApp(t,
		withActiveTeam("T1"),
		withWindowSize(200, 60),
		withWorkspaces(
			workspace.WorkspaceItem{ID: "T1", Name: "Acme", Initials: "AC"},
			workspace.WorkspaceItem{ID: "T2", Name: "Beta", Initials: "BE"},
		),
	)
	a.SetInitialChannel("C1", "alpha", []messages.MessageItem{
		{TS: "100.0", Text: "parent"},
		{TS: "200.0", Text: "other"},
	})
	_ = a.View() // populate layout bands for the rail click

	uploads, switched = new(int), new([]string)
	a.setUploaderForTest(func(_, _, _ string, _ []core.PendingAttachment) tea.Cmd {
		*uploads++
		return nil
	})
	a.setWorkspaceSwitcherForTest(func(teamID string) tea.Msg {
		*switched = append(*switched, teamID)
		return nil
	})

	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	a.threadCompose.SetValue("thread keep")

	a.compose.SetValue("main caption")
	a.compose.AddAttachment(core.PendingAttachment{Filename: "shot.png", Bytes: []byte("x")})
	a.submitWithAttachments(&a.compose)
	if !a.compose.Uploading() || *uploads != 1 {
		t.Fatalf("precondition: main compose not uploading (uploads=%d)", *uploads)
	}
	return a, uploads, switched
}

// TestDraftSafety_UploadInFlightRefusesEntryPoints drives every entry
// point that could move the composer, the thread or the workspace
// while an upload is in flight, and pins that each one is refused
// without moving state. Entry points that return a command also toast.
func TestDraftSafety_UploadInFlightRefusesEntryPoints(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(a *App)
		msg   tea.Msg
		// silent rows reach CloseThread, which refuses without a toast.
		silent bool
	}{
		{
			name:  "insert-mode printable key",
			setup: func(a *App) { a.SetMode(ModeInsert); a.focusedPanel = PanelThread },
			msg:   keyPress('x'),
		},
		{
			name:  "insert-mode enter",
			setup: func(a *App) { a.SetMode(ModeInsert); a.focusedPanel = PanelThread },
			msg:   keyCode(tea.KeyEnter),
		},
		{
			name:  "paste",
			setup: func(a *App) { a.SetMode(ModeInsert); a.focusedPanel = PanelThread },
			msg:   tea.PasteMsg{Content: "pasted"},
		},
		{
			name: "digit workspace key",
			msg:  keyPress('2'),
		},
		{
			name: "workspace finder enter",
			setup: func(a *App) {
				a.workspaceFinder.Open()
				a.SetMode(ModeWorkspaceFinder)
				a.Update(keyCode(tea.KeyDown))
			},
			msg: keyCode(tea.KeyEnter),
		},
		{
			name: "workspace rail click",
			msg:  tea.MouseClickMsg{X: 0, Y: 3, Button: tea.MouseLeft},
		},
		{
			// A switch already dispatched before the upload began.
			name: "workspace switch result",
			msg:  WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Beta"},
		},
		{
			name: "open thread from a message",
			setup: func(a *App) {
				a.focusedPanel = PanelMessages
				a.messagepane.SelectByTS("200.0")
			},
			msg: keyCode(tea.KeyEnter),
		},
		{
			name: "open thread from the threads view",
			setup: func(a *App) {
				a.threadsView.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "300.0"}})
				a.view = ViewThreads
				a.focusedPanel = PanelMessages
			},
			msg: keyCode(tea.KeyEnter),
		},
		{
			name:   "esc on the open thread",
			setup:  func(a *App) { a.focusedPanel = PanelThread },
			msg:    keyCode(tea.KeyEscape),
			silent: true,
		},
		{
			name:   "q on the open thread",
			setup:  func(a *App) { a.focusedPanel = PanelThread },
			msg:    keyPress('q'),
			silent: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, uploads, switched := uploadGuardApp(t)
			if tc.setup != nil {
				tc.setup(a)
			}

			_, cmd := a.Update(tc.msg)
			if !tc.silent {
				firstBatchCmd(t, cmd)
				if got := statusbarText(a); !strings.Contains(got, "Upload in progress") {
					t.Errorf("status bar = %q, want it to contain %q", got, "Upload in progress")
				}
			}

			if got := a.compose.Value(); got != "main caption" {
				t.Errorf("main caption = %q, want %q", got, "main caption")
			}
			if got := len(a.compose.Attachments()); got != 1 {
				t.Errorf("main attachments = %d, want 1", got)
			}
			if got := a.threadCompose.Value(); got != "thread keep" {
				t.Errorf("thread draft = %q, want %q", got, "thread keep")
			}
			if *uploads != 1 {
				t.Errorf("uploader calls = %d, want 1", *uploads)
			}
			if !a.threadVisible || a.threadPanel.ThreadTS() != "100.0" {
				t.Errorf("thread = (visible %v, ts %q), want (true, %q)", a.threadVisible, a.threadPanel.ThreadTS(), "100.0")
			}
			if len(*switched) != 0 || a.activeTeamID != "T1" {
				t.Errorf("workspace switched to %v (active %q), want none", *switched, a.activeTeamID)
			}
		})
	}
}

// TestDraftSafety_SubmitWithAttachmentsRefusesDuringUpload pins the
// guard inside submitWithAttachments directly. Its callers sit behind
// handleInsertMode's own upload guard, so no key can reach it.
func TestDraftSafety_SubmitWithAttachmentsRefusesDuringUpload(t *testing.T) {
	a, uploads, _ := uploadGuardApp(t)
	a.threadCompose.AddAttachment(core.PendingAttachment{Filename: "t.png", Bytes: []byte("y")})

	firstBatchCmd(t, a.submitWithAttachments(&a.threadCompose))
	if *uploads != 1 {
		t.Errorf("uploader calls = %d, want 1", *uploads)
	}
	if a.threadCompose.Uploading() {
		t.Error("thread compose started a second upload")
	}
}

// TestDraftSafety_DeferredThreadCloseFiresOnce pins that the deferred
// close of a deleted thread is cleared once it runs: a later upload in
// another thread must leave that thread open.
func TestDraftSafety_DeferredThreadCloseFiresOnce(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))
	a.SetInitialChannel("C1", "alpha", nil)
	a.setUploaderForTest(func(_, _, _ string, _ []core.PendingAttachment) tea.Cmd { return nil })

	upload := func() {
		t.Helper()
		a.threadCompose.SetValue("caption")
		a.threadCompose.AddAttachment(core.PendingAttachment{Filename: "t.png", Bytes: []byte("x")})
		a.submitWithAttachments(&a.threadCompose)
		if !a.threadCompose.Uploading() {
			t.Fatal("precondition: thread compose did not enter the uploading state")
		}
	}

	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	upload()
	a.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "100.0"})
	a.Update(UploadResultMsg{})
	if a.threadVisible {
		t.Fatal("precondition: deleted thread still open after its upload ended")
	}

	a.openThreadPanel(messages.MessageItem{TS: "200.0"}, "C1", "200.0")
	upload()
	a.Update(UploadResultMsg{})
	if !a.threadVisible || a.threadPanel.ThreadTS() != "200.0" {
		t.Errorf("thread = (visible %v, ts %q), want (true, %q): a stale deferred close fired", a.threadVisible, a.threadPanel.ThreadTS(), "200.0")
	}
}
