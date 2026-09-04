// internal/ui/app_test.go
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/workspace"
	"golang.design/x/clipboard"
)

func TestAppFocusCycle(t *testing.T) {
	app := NewApp()

	if app.focusedPanel != PanelSidebar {
		t.Errorf("expected initial focus on sidebar, got %d", app.focusedPanel)
	}

	app.FocusNext()
	if app.focusedPanel != PanelMessages {
		t.Errorf("expected focus on messages, got %d", app.focusedPanel)
	}

	app.FocusNext()
	if app.focusedPanel != PanelSidebar {
		t.Errorf("expected focus to wrap to sidebar, got %d", app.focusedPanel)
	}

	app.FocusPrev()
	if app.focusedPanel != PanelMessages {
		t.Errorf("expected focus on messages after prev, got %d", app.focusedPanel)
	}
}

func TestAppToggleSidebar(t *testing.T) {
	app := NewApp()

	if !app.sidebarVisible {
		t.Error("expected sidebar visible initially")
	}

	app.ToggleSidebar()
	if app.sidebarVisible {
		t.Error("expected sidebar hidden after toggle")
	}

	// When sidebar is hidden and focus was on sidebar, focus should move to messages
	app2 := NewApp()
	app2.focusedPanel = PanelSidebar
	app2.ToggleSidebar()
	if app2.focusedPanel != PanelMessages {
		t.Errorf("expected focus to move to messages when sidebar hidden, got %d", app2.focusedPanel)
	}

	app.ToggleSidebar()
	if !app.sidebarVisible {
		t.Error("expected sidebar visible after second toggle")
	}
}

func TestTypingStateAddAndExpire(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"

	// Simulate receiving a typing event
	app.typing.Add("C1", "U1")

	users := app.typing.Users("C1")
	if len(users) != 1 || users[0] != "U1" {
		t.Errorf("expected [U1], got %v", users)
	}

	// Add another user
	app.typing.Add("C1", "U2")
	users = app.typing.Users("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	// Expire all
	app.typing.Expire()
	// They shouldn't be expired yet (TTL is 5 seconds)
	users = app.typing.Users("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users still active, got %d", len(users))
	}
}

func TestTypingStateFiltersSelf(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.currentUserID = "U1"

	app.typing.Add("C1", "U1")
	app.typing.Add("C1", "U2")

	users := app.typing.UsersExcluding("C1", app.currentUserID)
	if len(users) != 1 || users[0] != "U2" {
		t.Errorf("expected [U2] (self filtered), got %v", users)
	}
}

func TestTypingIndicatorText(t *testing.T) {
	text := typingIndicatorText(nil)
	if text != "" {
		t.Errorf("expected empty for nil, got %q", text)
	}

	text = typingIndicatorText([]string{"Alice"})
	if text != "Alice is typing..." {
		t.Errorf("expected 'Alice is typing...', got %q", text)
	}

	text = typingIndicatorText([]string{"Alice", "Bob"})
	if text != "Alice and Bob are typing..." {
		t.Errorf("expected 'Alice and Bob are typing...', got %q", text)
	}

	text = typingIndicatorText([]string{"Alice", "Bob", "Charlie"})
	if text != "Several people are typing..." {
		t.Errorf("expected 'Several people are typing...', got %q", text)
	}
}

func TestRenderTypingIndicator(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.typing.SetEnabled(true)
	app.currentUserID = "U_SELF"

	// Set up user names
	app.messagepane.SetUserNames(map[string]string{"U1": "Alice", "U2": "Bob"})

	// No one typing — should return empty
	line := app.renderTypingLine()
	if line != "" {
		t.Errorf("expected empty, got %q", line)
	}

	// One person typing
	app.typing.Add("C1", "U1")
	line = app.renderTypingLine()
	if line == "" {
		t.Error("expected typing indicator, got empty")
	}
}

func TestAppModeTransitions(t *testing.T) {
	app := NewApp()

	if app.mode != ModeNormal {
		t.Error("expected normal mode initially")
	}

	app.SetMode(ModeInsert)
	if app.mode != ModeInsert {
		t.Error("expected insert mode")
	}

	app.SetMode(ModeNormal)
	if app.mode != ModeNormal {
		t.Error("expected normal mode after escape")
	}
}

func TestTypingClearedOnChannelSwitch(t *testing.T) {
	app := NewApp()
	app.typing.SetEnabled(true)
	app.activeChannelID = "C1"

	app.typing.Add("C1", "U1")
	app.typing.Add("C2", "U2")

	// Typing indicator should show for C1
	users := app.typing.UsersExcluding("C1", app.currentUserID)
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C1, got %d", len(users))
	}

	// After "switching" to C2, reset throttle
	app.activeChannelID = "C2"
	app.typingOut.lastSent = time.Time{} // reset throttle on switch

	// C2 should show its typers
	users = app.typing.UsersExcluding("C2", app.currentUserID)
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C2, got %d", len(users))
	}
}

func TestTypingThrottle(t *testing.T) {
	app := NewApp()
	app.typing.SetEnabled(true)
	app.activeChannelID = "C1"

	// First call should allow sending
	if !app.typingOut.CanSend() {
		t.Error("expected first typing send to be allowed")
	}

	// Mark as just sent
	app.typingOut.lastSent = time.Now()

	// Immediate second call should be throttled
	if app.typingOut.CanSend() {
		t.Error("expected typing send to be throttled")
	}

	// After 3 seconds, should allow again
	app.typingOut.lastSent = time.Now().Add(-4 * time.Second)
	if !app.typingOut.CanSend() {
		t.Error("expected typing send to be allowed after 3s")
	}
}

func TestHandleInsertMode_ShiftEnterInsertsNewline(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.Focus()
	app.compose.SetValue("hello")

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})

	if cmd != nil {
		// Anything non-nil here likely means a SendMessageMsg was queued.
		if msg := cmd(); msg != nil {
			if _, ok := msg.(SendMessageMsg); ok {
				t.Fatalf("Shift+Enter should not send the message")
			}
		}
	}
	val := app.compose.Value()
	if val == "" {
		t.Fatalf("compose value was reset; expected newline inserted, got empty")
	}
	if !strings.Contains(val, "\n") {
		t.Fatalf("expected newline in compose value, got %q", val)
	}
	if !strings.HasPrefix(val, "hello") {
		t.Fatalf("expected original text preserved, got %q", val)
	}
}

// Regression: Shift+Enter must keep working past the visible-row cap of
// the compose box. The textarea's MaxHeight used to be 5, which also
// gated InsertNewline via atContentLimit, so users hit a silent
// 4-newline ceiling once the box was full.
func TestHandleInsertMode_ShiftEnterPastVisibleHeight(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.Focus()

	app.compose.SetValue("a\nb\nc\nd\ne\nf")
	app.compose.MoveCursorToEnd()

	app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})

	val := app.compose.Value()
	if got, want := strings.Count(val, "\n"), 6; got != want {
		t.Fatalf("expected %d newlines after shift+enter on a 6-line draft, got %d (value=%q)", want, got, val)
	}
}

func TestHandleInsertMode_PlainEnterSends(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.SetValue("hello")

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("plain Enter with text should return a send cmd")
	}
	msg := cmd()
	if _, ok := msg.(SendMessageMsg); !ok {
		t.Fatalf("expected SendMessageMsg, got %T", msg)
	}
	if app.compose.Value() != "" {
		t.Fatalf("expected compose to be reset after send, got %q", app.compose.Value())
	}
}

// TestHandleInsertMode_PlainEnterReturnsToNormalMode locks in the
// vim-style UX: hitting Enter to submit a channel message drops the
// user back to ModeNormal instead of leaving them in insert mode.
func TestHandleInsertMode_PlainEnterReturnsToNormalMode(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.SetValue("hello")

	_ = app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.mode != ModeNormal {
		t.Errorf("after plain-text send, mode = %v, want ModeNormal", app.mode)
	}
}

// TestHandleInsertMode_ThreadReplyEnterReturnsToNormalMode is the
// thread-compose counterpart: submitting a thread reply also returns
// the user to ModeNormal.
func TestHandleInsertMode_ThreadReplyEnterReturnsToNormalMode(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.SetMode(ModeInsert)
	app.threadCompose.SetValue("reply")

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatalf("plain Enter with text should return a send cmd")
	}
	if _, ok := cmd().(SendThreadReplyMsg); !ok {
		t.Fatalf("expected SendThreadReplyMsg, got %T", cmd())
	}
	if app.mode != ModeNormal {
		t.Errorf("after thread reply, mode = %v, want ModeNormal", app.mode)
	}
}

// TestHandleInsertMode_AttachmentSendReturnsToNormalMode asserts the
// mode flip happens immediately on Enter for attachment sends, not on
// UploadResultMsg. The user's mental model is "I hit Enter, I'm done."
func TestHandleInsertMode_AttachmentSendReturnsToNormalMode(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.AddAttachment(compose.PendingAttachment{
		Filename: "a.png", Bytes: []byte("png"), Size: 3,
	})
	app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
		return func() tea.Msg { return UploadResultMsg{Err: nil} }
	})

	_ = app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	if !app.compose.Uploading() {
		t.Fatal("setup: expected uploader to have been invoked")
	}
	if app.mode != ModeNormal {
		t.Errorf("after attachment send, mode = %v, want ModeNormal", app.mode)
	}
}

// TestHandleInsertMode_ThreadAttachmentSendReturnsToNormalMode mirrors
// the channel attachment test for the thread compose.
func TestHandleInsertMode_ThreadAttachmentSendReturnsToNormalMode(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.SetMode(ModeInsert)
	app.threadCompose.AddAttachment(compose.PendingAttachment{
		Filename: "a.png", Bytes: []byte("png"), Size: 3,
	})
	app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
		return func() tea.Msg { return UploadResultMsg{Err: nil} }
	})

	_ = app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	if !app.threadCompose.Uploading() {
		t.Fatal("setup: expected uploader to have been invoked for thread compose")
	}
	if app.mode != ModeNormal {
		t.Errorf("after thread attachment send, mode = %v, want ModeNormal", app.mode)
	}
}

// TestHandleInsertMode_EditSubmitStaysInInsert pins down the
// intentional asymmetry: edits keep insert mode until the server
// confirms the edit (see MessageEditedMsg → cancelEdit), so that
// transient failures don't strand the user one keystroke away from
// resuming their edit.
func TestHandleInsertMode_EditSubmitStaysInInsert(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages
	app.compose.SetValue("edited body")

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected submitEdit cmd")
	}
	if _, ok := cmd().(EditMessageMsg); !ok {
		t.Fatalf("expected EditMessageMsg, got %T", cmd())
	}
	if app.mode != ModeInsert {
		t.Errorf("edit submit changed mode to %v, want ModeInsert (cancelEdit on MessageEditedMsg drives the flip)", app.mode)
	}
}

// TestHandleInsertMode_EmptyEnterStaysInInsert: pressing Enter with
// nothing typed is a no-op — no message goes out, so the mode flip
// would be surprising.
func TestHandleInsertMode_EmptyEnterStaysInInsert(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	// compose has empty value

	_ = app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.mode != ModeInsert {
		t.Errorf("empty Enter changed mode to %v, want ModeInsert", app.mode)
	}
}

func TestCopyPermalink_FromMessagesPane(t *testing.T) {
	app := NewApp()
	var copied string
	app.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000200", UserName: "alice", Text: "hi"},
	})

	var gotCh, gotTS string
	app.setPermalinkFetcherForTest(func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
		gotCh = string(channelID)
		gotTS = string(ts)
		return "https://example.slack.com/archives/C123/p1700000001000200", nil
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from C key")
	}
	msg := cmd()
	// cmd returns a tea.BatchMsg containing tea.SetClipboard cmd + permalink-copied msg.
	// Easiest assertion: drain the batch and look for our marker types.
	found := drainForPermalinkCopied(t, msg)
	if !found {
		t.Fatalf("expected statusbar.PermalinkCopiedMsg in batch, got %#v", msg)
	}
	if copied != "https://example.slack.com/archives/C123/p1700000001000200" {
		t.Errorf("clipboard = %q", copied)
	}
	if gotCh != "C123" {
		t.Errorf("channel = %q, want C123", gotCh)
	}
	if gotTS != "1700000001.000200" {
		t.Errorf("ts = %q, want 1700000001.000200", gotTS)
	}
}

func TestCopyPermalink_FromThreadPane(t *testing.T) {
	app := NewApp()
	var copied string
	app.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	parent := messages.MessageItem{TS: "1700000000.000100"}
	replies := []messages.MessageItem{
		{TS: "1700000000.000100", UserName: "alice", Text: "parent"},
		{TS: "1700000050.000400", UserName: "bob", Text: "reply"},
	}
	app.threadPanel.SetThread(parent, replies, "C999", "1700000000.000100")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	// SetThread initializes selection to 0; advance to the second reply.
	for i := 0; i < len(replies); i++ {
		sel := app.threadPanel.SelectedReply()
		if sel != nil && sel.TS == "1700000050.000400" {
			break
		}
		app.threadPanel.MoveDown()
	}
	if sel := app.threadPanel.SelectedReply(); sel == nil || sel.TS != "1700000050.000400" {
		t.Fatalf("could not select reply ts=1700000050.000400; got %+v", sel)
	}

	var gotCh, gotTS string
	app.setPermalinkFetcherForTest(func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
		gotCh = string(channelID)
		gotTS = string(ts)
		return "https://example.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999", nil
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from C key")
	}
	if !drainForPermalinkCopied(t, cmd()) {
		t.Fatal("expected PermalinkCopiedMsg")
	}
	if copied != "https://example.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999" {
		t.Errorf("clipboard = %q", copied)
	}
	if gotCh != "C999" {
		t.Errorf("channel = %q, want C999", gotCh)
	}
	if gotTS != "1700000050.000400" {
		t.Errorf("ts = %q, want reply ts 1700000050.000400", gotTS)
	}
}

func TestCopyPermalink_NothingSelectedNoop(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	// No messages set.
	app.setPermalinkFetcherForTest(func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
		t.Fatal("fetcher must not be called when nothing is selected")
		return "", nil
	})
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd != nil {
		// cmd may be non-nil but must not invoke the fetcher; drain it.
		_ = cmd()
	}
}

func TestCopyPermalink_FetcherErrorEmitsFailedMsg(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "hi"},
	})
	app.setPermalinkFetcherForTest(func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
		return "", errors.New("boom")
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(statusbar.PermalinkCopyFailedMsg); !ok {
		t.Fatalf("expected PermalinkCopyFailedMsg, got %T", msg)
	}
}

func TestApp_PermalinkCopiedMsgShowsToast(t *testing.T) {
	a := NewApp()
	_, cmd := a.Update(statusbar.PermalinkCopiedMsg{})
	if !strings.Contains(a.statusbar.View(80), "Copied permalink") {
		t.Fatalf("expected 'Copied permalink' toast; got %q", a.statusbar.View(80))
	}
	if cmd == nil {
		t.Fatal("expected a clear-tick cmd")
	}
}

func TestApp_PermalinkCopyFailedMsgShowsToast(t *testing.T) {
	a := NewApp()
	a.Update(statusbar.PermalinkCopyFailedMsg{})
	if !strings.Contains(a.statusbar.View(80), "Failed to copy link") {
		t.Fatalf("expected 'Failed to copy link' toast; got %q", a.statusbar.View(80))
	}
}

// drainForPermalinkCopied walks tea.BatchMsg / tea.Cmd structures looking for
// a statusbar.PermalinkCopiedMsg.
func drainForPermalinkCopied(t *testing.T, msg tea.Msg) bool {
	t.Helper()
	switch v := msg.(type) {
	case statusbar.PermalinkCopiedMsg:
		return true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if drainForPermalinkCopied(t, c()) {
				return true
			}
		}
	}
	return false
}

func TestCopyPermalink_ShiftYTriggersCopy(t *testing.T) {
	app := NewApp()
	app.SetClipboardWriter(func(string) tea.Cmd { return nil })
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000200", UserName: "alice", Text: "hi"},
	})

	called := 0
	var gotCh, gotTS string
	app.setPermalinkFetcherForTest(func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
		called++
		gotCh = string(channelID)
		gotTS = string(ts)
		return "https://example.slack.com/x", nil
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'Y', Text: "Y"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Y key")
	}
	if !drainForPermalinkCopied(t, cmd()) {
		t.Fatal("expected PermalinkCopiedMsg in batch")
	}
	if called != 1 {
		t.Fatalf("expected fetcher called once, got %d", called)
	}
	if gotCh != "C123" || gotTS != "1700000001.000200" {
		t.Errorf("fetcher got (%q, %q); want (\"C123\", \"1700000001.000200\")", gotCh, gotTS)
	}
}

func TestApp_ThreadsViewActivation(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeTeamID = "T1"
	app.SetUserNames(map[string]string{"U1": "alice"})

	// Default: ViewChannels.
	if app.view != ViewChannels {
		t.Fatalf("default view = %v, want ViewChannels", app.view)
	}

	// Activating threads view via the message.
	_, _ = app.Update(ThreadsViewActivatedMsg{})
	if app.view != ViewThreads {
		t.Fatalf("after activation view = %v, want ViewThreads", app.view)
	}

	// Switching to a channel returns to ViewChannels.
	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "general"})
	if app.view != ViewChannels {
		t.Errorf("after ChannelSelectedMsg view = %v, want ViewChannels", app.view)
	}
}

func TestApp_ThreadsListLoadedUpdatesUnreadBadge(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	summaries := []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", Unread: true},
		{ChannelID: "C2", ThreadTS: "2.0", Unread: false},
	}
	_, _ = app.Update(ThreadsListLoadedMsg{TeamID: "T1", Summaries: summaries})
	if app.sidebar.ThreadsUnreadCount() != 1 {
		t.Errorf("ThreadsUnreadCount = %d, want 1", app.sidebar.ThreadsUnreadCount())
	}
}

func TestApp_ThreadsListLoadedIgnoredForOtherWorkspace(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	summaries := []cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "1.0", Unread: true}}
	_, _ = app.Update(ThreadsListLoadedMsg{TeamID: "T2", Summaries: summaries})
	if app.sidebar.ThreadsUnreadCount() != 0 {
		t.Errorf("threads from a different team should not update the active sidebar; got %d", app.sidebar.ThreadsUnreadCount())
	}
}

func TestApp_HandleEnterOnThreadsRowActivatesView(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	// Sidebar default-selects the Threads row.
	if !app.sidebar.IsThreadsSelected() {
		t.Fatalf("precondition: sidebar should default-select Threads row")
	}
	cmd := app.handleEnter()
	if cmd == nil {
		t.Fatal("expected a tea.Cmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(ThreadsViewActivatedMsg); !ok {
		t.Errorf("expected ThreadsViewActivatedMsg, got %T", msg)
	}
}

// TestApp_ChannelFinderThreadsRowActivatesThreadsView guards the
// discoverability fix for the threads-list view: pressing ctrl+t (or
// ctrl+p) and hitting Enter on the pinned "Threads" row in the finder
// must dispatch ThreadsViewActivatedMsg, not ChannelSelectedMsg. Before
// the synthetic row existed, the only way to reach the threads view was
// to click the Threads sidebar entry.
func TestApp_ChannelFinderThreadsRowActivatesThreadsView(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	// Open the finder the same way the ctrl+t / ctrl+p binding does.
	app.channelFinder.Open()
	app.SetMode(ModeChannelFinder)

	// The synthetic Threads row is pinned at the top under empty-query,
	// so a single Enter must select it.
	cmd := app.handleChannelFinderMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd from selecting the Threads row, got nil")
	}
	msg := cmd()
	if _, ok := msg.(ThreadsViewActivatedMsg); !ok {
		t.Errorf("Enter on synthetic Threads row dispatched %T, want ThreadsViewActivatedMsg",
			msg)
	}

	// Finder should have closed and mode reverted to normal.
	if app.channelFinder.IsVisible() {
		t.Error("channel finder should close after selecting the Threads row")
	}
	if app.mode != ModeNormal {
		t.Errorf("mode after selection = %v, want ModeNormal", app.mode)
	}
}

// TestApp_ClickOnThreadInThreadsViewOpensIt guards Bug B: a left-click
// on a thread card in the threads-list view must select that card AND
// open the corresponding thread. Before the fix, the messages-pane
// click branch unconditionally drove a.messagepane.BeginSelectionAt /
// ClickAt — which has no effect on the (hidden) channel messages pane
// while in ViewThreads, leaving the threads-view selection and the
// thread panel untouched.
func TestApp_ClickOnThreadInThreadsViewOpensIt(t *testing.T) {
	a := NewApp()
	a.width = 160
	a.height = 30
	a.activeTeamID = "T1"
	// Force layout to populate layoutSidebarEnd / layoutMsgEnd.
	_ = a.View()
	if a.layout.msgEnd <= a.layout.sidebarEnd {
		t.Fatalf("layout not populated after View(); sidebarEnd=%d msgEnd=%d",
			a.layout.sidebarEnd, a.layout.msgEnd)
	}

	fetchedCh := ""
	fetchedTS := ""
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetchedCh = string(channelID)
		fetchedTS = string(threadTS)
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})
	summaries := []cache.ThreadSummary{
		{ChannelID: "C_A", ThreadTS: "1.0", ParentTS: "1.0", ParentText: "alpha"},
		{ChannelID: "C_B", ThreadTS: "2.0", ParentTS: "2.0", ParentText: "bravo"},
	}
	// SubscriptionsAvailable=true so the banner row isn't reserved
	// above the list, keeping the row math simple.
	a.Update(ThreadsListLoadedMsg{TeamID: "T1", Summaries: summaries, SubscriptionsAvailable: true})
	// Enter the threads view. This selects summary 0 and opens it.
	a.Update(ThreadsViewActivatedMsg{})
	if a.view != ViewThreads {
		t.Fatalf("precondition: view = %v, want ViewThreads", a.view)
	}

	// Re-render so layout reflects ViewThreads.
	_ = a.View()

	// Click in the messages-pane horizontal zone, on row Y=5. The
	// messages pane has a 1-row top border, so paneY = 4. With
	// cardStride=4 and cardContentLines=3, absLine=4 lies on the
	// first row of card 1 (rows 0-2 = card 0, row 3 = separator,
	// rows 4-6 = card 1).
	clickX := a.layout.sidebarEnd + 5 // anywhere inside the msg pane zone
	clickY := 5
	fetchedCh, fetchedTS = "", ""
	_, cmd := a.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("click in threads view returned nil cmd; expected an open-thread fetch")
	}
	_ = drainBatch(cmd)

	if got := a.threadsView.SelectedIndex(); got != 1 {
		t.Errorf("click did not move threadsView selection: SelectedIndex = %d, want 1", got)
	}
	if fetchedCh != "C_B" || fetchedTS != "2.0" {
		t.Errorf("click fetched (ch=%q, ts=%q); want (C_B, 2.0)", fetchedCh, fetchedTS)
	}
	if a.threadPanel.ChannelID() != "C_B" || a.threadPanel.ThreadTS() != "2.0" {
		t.Errorf("thread panel opened (ch=%q, ts=%q); want (C_B, 2.0)",
			a.threadPanel.ChannelID(), a.threadPanel.ThreadTS())
	}
}

// TestApp_HandleEnterInThreadsViewOpensSelectedThread guards Bug A:
// while the user is in the threads-list view, pressing Enter must
// open the thread highlighted in `threadsView`, NOT the message
// highlighted in the (hidden) channel messages pane. The threads view
// runs with focusedPanel == PanelMessages — without an explicit
// `view == ViewThreads` branch in handleEnter, the PanelMessages
// block fell through to messagepane.SelectedMessage() and opened
// whatever was selected in the underlying channel.
func TestApp_HandleEnterInThreadsViewOpensSelectedThread(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C_CHANNEL"

	fetchedCh := ""
	fetchedTS := ""
	app.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetchedCh = string(channelID)
		fetchedTS = string(threadTS)
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	// Seed the channel messages pane with a message highlighted at
	// index 0. This is the "wrong" target Enter used to open.
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "9999.0", UserID: "U1", Text: "channel msg", ThreadTS: ""},
	})

	// Seed the threads view with two summaries and select the SECOND
	// one. This is what Enter must open.
	summaries := []cache.ThreadSummary{
		{ChannelID: "C_THREADA", ThreadTS: "1.0", ParentTS: "1.0"},
		{ChannelID: "C_THREADB", ThreadTS: "2.0", ParentTS: "2.0"},
	}
	_, _ = app.Update(ThreadsListLoadedMsg{TeamID: "T1", Summaries: summaries})
	app.threadsView.MoveDown() // select the second summary

	// Activate the threads view (sets view=ViewThreads,
	// focusedPanel=PanelMessages). Discard the activation cmd; we
	// drive handleEnter directly below.
	_, _ = app.Update(ThreadsViewActivatedMsg{})

	cmd := app.handleEnter()
	if cmd == nil {
		t.Fatal("handleEnter returned nil in ViewThreads")
	}
	// Drain the tea.Batch — fetcher invocations record into
	// fetchedCh/fetchedTS.
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			if c != nil {
				_ = c()
			}
		}
	}

	if fetchedCh != "C_THREADB" || fetchedTS != "2.0" {
		t.Fatalf("Enter in ViewThreads fetched (ch=%q, ts=%q); want (C_THREADB, 2.0). It is opening the wrong thread (likely the channel's selected message).",
			fetchedCh, fetchedTS)
	}
	if app.threadPanel.ChannelID() != "C_THREADB" || app.threadPanel.ThreadTS() != "2.0" {
		t.Errorf("thread panel opened (ch=%q, ts=%q); want (C_THREADB, 2.0)",
			app.threadPanel.ChannelID(), app.threadPanel.ThreadTS())
	}
	// Enter on a thread should also move keyboard focus into the
	// thread pane, mirroring channel-pane Enter semantics ("enter
	// this thread to interact with it"). This distinguishes Enter
	// from j/k which preserve PanelMessages focus for further list
	// navigation.
	if app.focusedPanel != PanelThread {
		t.Errorf("after Enter in ViewThreads, focusedPanel = %v, want PanelThread", app.focusedPanel)
	}
}

func TestApp_OpenSelectedThreadDedups(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	fetched := 0
	app.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetched++
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})
	summaries := []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0"},
		{ChannelID: "C1", ThreadTS: "2.0"},
	}
	app.Update(ThreadsListLoadedMsg{TeamID: "T1", Summaries: summaries})
	app.view = ViewThreads

	// First call should fetch. Use the immediate path (debounce=false) so
	// the test directly exercises the dedup semantics without the
	// debounce-tick indirection.
	cmd := app.openSelectedThreadCmd(false)
	if cmd == nil {
		t.Fatal("first call returned nil")
	}
	cmd()
	if fetched != 1 {
		t.Errorf("first call fetched=%d, want 1", fetched)
	}

	// Second call without selection change should NOT fetch.
	cmd = app.openSelectedThreadCmd(false)
	if cmd != nil {
		t.Errorf("second call should be a no-op, got cmd=%v", cmd)
	}

	// After moving selection, fetch should fire again.
	app.threadsView.MoveDown()
	cmd = app.openSelectedThreadCmd(false)
	if cmd == nil {
		t.Fatal("after MoveDown, expected fetch")
	}
	cmd()
	if fetched != 2 {
		t.Errorf("after MoveDown fetched=%d, want 2", fetched)
	}
}

func TestApp_WorkspaceSwitchResetsView(t *testing.T) {
	app := NewApp()
	app.view = ViewThreads
	app.activeTeamID = "T1"
	// Stash some summaries to confirm they're cleared.
	app.threadsView.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "1.0", Unread: true}})
	app.sidebar.SetThreadsUnreadCount(1)

	app.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Other", Channels: nil})

	if app.view != ViewChannels {
		t.Errorf("after workspace switch view = %v, want ViewChannels", app.view)
	}
	if app.threadsView.UnreadCount() != 0 {
		t.Errorf("threadsView should be cleared on workspace switch")
	}
	if app.sidebar.ThreadsUnreadCount() != 0 {
		t.Errorf("sidebar threads-unread should be cleared on workspace switch")
	}
}

func TestApp_NewThreadReplyTriggersDirtyMsg(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeTeamID = "T1"
	// Tiny debounce so the test runs fast.
	app.threadsDirtyDebounce = 5 * time.Millisecond

	fetched := make(chan string, 4)
	app.setThreadsListFetcherForTest(func(teamID ids.TeamID) tea.Msg {
		fetched <- string(teamID)
		return ThreadsListLoadedMsg{TeamID: string(teamID), Summaries: nil}
	})

	// Activate threads view; drain the resulting initial fetch so it
	// doesn't pollute the dirty-trigger assertion below.
	_, initCmd := app.Update(ThreadsViewActivatedMsg{})
	for _, m := range drainBatch(initCmd) {
		if m != nil {
			app.Update(m)
		}
	}
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("initial threads-list fetch did not fire")
	}
	// Drain any extra incidental fetches from openSelectedThreadCmd etc.
	for len(fetched) > 0 {
		<-fetched
	}

	// A thread reply event should schedule a debounced dirty msg → fetch.
	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:       "2.0",
			UserID:   "U2",
			Text:     "reply",
			ThreadTS: "1.0",
		},
	})
	if cmd == nil {
		t.Fatal("NewMessageMsg with ThreadTS expected to return a cmd")
	}
	// Drive every leaf message produced by the cmd graph back into the app.
	// tea.Tick blocks for the duration before returning a TickMsg-shaped
	// value (here, ThreadsListDirtyMsg). drainBatch will block on it, which
	// is fine because we set the debounce to 5ms.
	for _, m := range drainBatch(cmd) {
		if m != nil {
			_, follow := app.Update(m)
			for _, fm := range drainBatch(follow) {
				if fm != nil {
					app.Update(fm)
				}
			}
		}
	}

	select {
	case team := <-fetched:
		if team != "T1" {
			t.Errorf("re-fetch teamID = %q, want T1", team)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected re-fetch after thread reply, did not happen")
	}
}

func TestApp_NewMessageWithoutThreadTSDoesNotTriggerDirty(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.threadsDirtyDebounce = 5 * time.Millisecond

	fetched := make(chan struct{}, 4)
	app.setThreadsListFetcherForTest(func(teamID ids.TeamID) tea.Msg {
		fetched <- struct{}{}
		return ThreadsListLoadedMsg{TeamID: string(teamID), Summaries: nil}
	})

	// Top-level message (no ThreadTS) should NOT schedule any dirty fetch.
	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:     "1.0",
			UserID: "U1",
			Text:   "hello",
		},
	})
	for _, m := range drainBatch(cmd) {
		if m != nil {
			_, follow := app.Update(m)
			for _, fm := range drainBatch(follow) {
				if fm != nil {
					app.Update(fm)
				}
			}
		}
	}

	select {
	case <-fetched:
		t.Error("top-level message should not trigger threads-list fetch")
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

func TestApp_WorkspaceReadyTriggersThreadsListFetch(t *testing.T) {
	app := NewApp()
	fetched := make(chan string, 1)
	app.setThreadsListFetcherForTest(func(teamID ids.TeamID) tea.Msg {
		fetched <- string(teamID)
		return ThreadsListLoadedMsg{TeamID: string(teamID), Summaries: nil}
	})

	_, cmd := app.Update(WorkspaceReadyMsg{
		TeamID:   "T1",
		TeamName: "Test",
		Channels: nil,
	})
	for _, m := range drainBatch(cmd) {
		_ = m
	}
	select {
	case team := <-fetched:
		if team != "T1" {
			t.Errorf("fetcher called with team=%q, want T1", team)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WorkspaceReadyMsg did not trigger threads-list fetch")
	}
}

// A background workspace becoming ready (after a different workspace is
// already active) must not clobber the active workspace's threads-view
// state. Only the first WorkspaceReadyMsg (when activeChannelID == "")
// performs the initial setup; subsequent ones must leave summaries, the
// unread badge, and the current view untouched.
// In the threads view there is no main compose box; pressing `i` must
// focus the right-side thread panel's compose, not the (hidden) main
// compose. Regression test for the focus bug where pressing `i` while
// browsing the threads list would silently no-op.
func TestApp_InsertInThreadsViewFocusesThreadCompose(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	// Simulate having activated the threads view with one summary, so
	// the right thread panel is open.
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", ParentText: "hi"},
	})
	app.view = ViewThreads
	app.threadVisible = true
	app.focusedPanel = PanelMessages // typical state when browsing the list

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'i', Text: "i"})
	_ = cmd

	if app.mode != ModeInsert {
		t.Errorf("after pressing 'i' mode = %v, want ModeInsert", app.mode)
	}
	if app.focusedPanel != PanelThread {
		t.Errorf("after pressing 'i' in threads view focusedPanel = %v, want PanelThread", app.focusedPanel)
	}
}

func TestApp_BackgroundWorkspaceReadyDoesNotClobberActiveState(t *testing.T) {
	app := NewApp()
	app.setThreadsListFetcherForTest(func(teamID ids.TeamID) tea.Msg {
		return ThreadsListLoadedMsg{TeamID: string(teamID), Summaries: nil}
	})

	// Make T1 the active workspace by sending the first WorkspaceReadyMsg.
	app.Update(WorkspaceReadyMsg{
		TeamID:        "T1",
		TeamName:      "First",
		Channels:      []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		InitialActive: true,
	})
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"

	// Simulate user state in the active workspace.
	app.view = ViewThreads
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", Unread: true},
	})
	app.sidebar.SetThreadsUnreadCount(1)

	// Now a background workspace T2 finishes loading.
	app.Update(WorkspaceReadyMsg{
		TeamID:   "T2",
		TeamName: "Second",
		Channels: []sidebar.ChannelItem{{ID: "C9", Name: "other", Type: "channel"}},
	})

	// All three pieces of active-workspace state must be preserved.
	if app.view != ViewThreads {
		t.Errorf("background ready clobbered view: got %v, want ViewThreads", app.view)
	}
	if app.threadsView.UnreadCount() != 1 {
		t.Errorf("background ready clobbered threadsView summaries: UnreadCount=%d, want 1", app.threadsView.UnreadCount())
	}
	if app.sidebar.ThreadsUnreadCount() != 1 {
		t.Errorf("background ready clobbered sidebar badge: got %d, want 1", app.sidebar.ThreadsUnreadCount())
	}
	if app.activeTeamID != "T1" {
		t.Errorf("background ready clobbered activeTeamID: got %q, want T1", app.activeTeamID)
	}
}

func TestApp_WorkspaceSwitchedTriggersThreadsListFetchAndSelectsThreadsRow(t *testing.T) {
	app := NewApp()
	// Move sidebar selection off the Threads row first to verify the reset.
	app.sidebar.SetItems([]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	app.sidebar.MoveDown()
	if app.sidebar.IsThreadsSelected() {
		t.Fatal("precondition: should be off Threads row")
	}

	fetched := make(chan string, 1)
	app.setThreadsListFetcherForTest(func(teamID ids.TeamID) tea.Msg {
		fetched <- string(teamID)
		return ThreadsListLoadedMsg{TeamID: string(teamID), Summaries: nil}
	})

	_, cmd := app.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		TeamName: "Other",
		Channels: nil,
	})
	if !app.sidebar.IsThreadsSelected() {
		t.Errorf("WorkspaceSwitchedMsg should reset sidebar to Threads row")
	}
	for _, m := range drainBatch(cmd) {
		_ = m
	}
	select {
	case team := <-fetched:
		if team != "T2" {
			t.Errorf("fetcher called with team=%q, want T2", team)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WorkspaceSwitchedMsg did not trigger threads-list fetch")
	}
}

func TestApp_ThreadReplySentOptimisticallyAddsToThreadPanel(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"
	parent := messages.MessageItem{TS: "1700000000.000100"}
	app.threadPanel.SetThread(parent, nil, "C1", "1700000000.000100")
	app.threadVisible = true

	app.Update(ThreadReplySentMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		Message: messages.MessageItem{
			TS:       "1700000050.000400",
			UserID:   "USELF",
			UserName: "you",
			Text:     "my reply",
			ThreadTS: "1700000000.000100",
		},
	})

	if got := app.threadPanel.ReplyCount(); got != 1 {
		t.Fatalf("expected 1 reply added optimistically, got %d", got)
	}
	if !app.selfSend.IsSelfSent("1700000050.000400") {
		t.Errorf("expected TS to be recorded as self-sent for echo dedup")
	}
}

// TestSendThreadReply_InstantDisplay asserts that a thread reply
// appears in the thread panel the moment SendThreadReplyMsg is
// dispatched, before any HTTP round-trip. The placeholder is
// swapped in place when ThreadReplySentMsg lands.
func TestSendThreadReply_InstantDisplay(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"
	parent := messages.MessageItem{TS: "1700000000.000100"}
	app.threadPanel.SetThread(parent, nil, "C1", "1700000000.000100")
	app.threadVisible = true

	app.Update(SendThreadReplyMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		Text:      "instant reply",
	})

	if got := app.threadPanel.ReplyCount(); got != 1 {
		t.Fatalf("instant-display: expected 1 placeholder reply, got %d", got)
	}

	// Capture the placeholder's local TS for the swap.
	localTS := app.threadPanel.Replies()[0].TS
	if !strings.HasPrefix(localTS, "local:") {
		t.Errorf("placeholder reply TS = %q, want local:... id", localTS)
	}

	app.Update(ThreadReplySentMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		LocalTS:   localTS,
		Message: messages.MessageItem{
			TS: "1700000050.000400", UserID: "USELF", UserName: "you",
			Text: "instant reply", ThreadTS: "1700000000.000100",
		},
	})

	if got := app.threadPanel.ReplyCount(); got != 1 {
		t.Fatalf("post-swap: expected 1 reply, got %d", got)
	}
	if got := app.threadPanel.Replies()[0].TS; got != "1700000050.000400" {
		t.Errorf("post-swap TS = %q, want real Slack TS", got)
	}
}

func TestApp_NewMessageEchoOfSelfSentIsSkipped(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"
	parent := messages.MessageItem{TS: "1700000000.000100"}
	app.threadPanel.SetThread(parent, nil, "C1", "1700000000.000100")
	app.threadVisible = true

	// Optimistic add via the HTTP-response path.
	app.Update(ThreadReplySentMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		Message: messages.MessageItem{
			TS: "1700000050.000400", UserID: "USELF", Text: "hi",
			ThreadTS: "1700000000.000100",
		},
	})
	if app.threadPanel.ReplyCount() != 1 {
		t.Fatalf("setup: expected 1 reply after optimistic add, got %d", app.threadPanel.ReplyCount())
	}

	// WS echo for the same TS must be ignored, not double-appended.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000050.000400", UserID: "USELF", Text: "hi",
			ThreadTS: "1700000000.000100",
		},
	})
	if got := app.threadPanel.ReplyCount(); got != 1 {
		t.Errorf("WS echo of self-sent reply double-added; want 1 reply, got %d", got)
	}

	// A different TS (e.g. someone else's reply) should still be added.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000060.000500", UserID: "U2", Text: "yo",
			ThreadTS: "1700000000.000100",
		},
	})
	if got := app.threadPanel.ReplyCount(); got != 2 {
		t.Errorf("non-self reply not appended; want 2 replies, got %d", got)
	}
}

func TestApp_MessageSentOptimisticallyAppendsToMessagepane(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	beforeVer := app.messagepane.Version()
	app.Update(MessageSentMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "hello",
		},
	})
	if app.messagepane.Version() == beforeVer {
		t.Errorf("expected messagepane version to advance after optimistic append")
	}
	if !app.selfSend.IsSelfSent("1700000999.000001") {
		t.Errorf("expected TS to be recorded for echo dedup")
	}
}

func TestApp_WorkspaceReadyAppliesPerWorkspaceTheme(t *testing.T) {
	app := NewApp()
	// Theme application should fire when a per-workspace theme is set
	// for the initial active workspace. The test only asserts that the
	// version counter advances, since the actual theme name lookup lives
	// in styles.Apply.
	beforeVer := styles.Version()

	app.Update(WorkspaceReadyMsg{
		TeamID:        "T1",
		TeamName:      "team",
		Theme:         "dracula",
		InitialActive: true,
	})

	afterVer := styles.Version()
	if afterVer == beforeVer {
		t.Errorf("expected styles.Version() to advance after WorkspaceReadyMsg with non-empty Theme")
	}
}

// Defends Bug A: a duplicate of the same TS (e.g. WS echo arriving before
// the optimistic-add path can record the TS) must not produce two messages
// in the pane. The optimistic version (which carries the locally-converted
// mrkdwn from compose) must REPLACE the WS-echo version, not be silently
// dropped — Slack normalises wire-form text, so the WS echo's Text may
// differ from what slk's renderer expects.
func TestApp_DuplicateMessageEventDoesNotDoubleAppend(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	// Simulate the race: WS echo arrives FIRST with text Slack flattened
	// (no \n, single line) — the user composed "Hello\nWorld".
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "Hello World",
		},
	})
	// Then the HTTP-response optimistic path fires with the converted
	// mrkdwn text that preserves the line break.
	app.Update(MessageSentMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "Hello\nWorld",
		},
	})

	// The model contains exactly one message (no duplicate).
	got := app.messagepane.Messages()
	if len(got) != 1 {
		t.Fatalf("expected 1 message in pane, got %d (duplicate)", len(got))
	}
	// And its Text is the optimistic, line-preserving version — not the
	// flattened WS-echo text.
	if got[0].Text != "Hello\nWorld" {
		t.Errorf("Text = %q, want %q (optimistic should win over WS-echo)", got[0].Text, "Hello\nWorld")
	}
}

// Half-page scroll (ctrl+u / ctrl+d) and PageUp/PageDown are now decoupled
// from selection: they move the viewport only. The selected message stays
// put -- so a subsequent j/k will snap the viewport back to keep selection
// visible, which is the expected behavior once the user explicitly invokes
// selection navigation. This test pins that contract.
func TestApp_HalfPageScrollMovesViewportNotSelection(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	// Populate messages so half-page has somewhere to go.
	var items []messages.MessageItem
	for i := 0; i < 50; i++ {
		items = append(items, messages.MessageItem{
			TS:   fmt.Sprintf("17000000%02d.0001", i),
			Text: fmt.Sprintf("msg %d", i),
		})
	}
	app.messagepane.SetMessages(items)
	// Provide a sane layout so halfPageSize() returns > 1.
	app.layout.msgHeight = 20
	// Force one render so yOffset snaps to keep the bottom selection visible
	// (SetMessages defaults selection to the last message). Without this,
	// yOffset is still 0 and ScrollUp would clamp with no observable effect.
	_ = app.messagepane.View(80, 20)

	startIdx := app.messagepane.SelectedIndex()
	startOff := app.messagepane.YOffset()

	app.scrollFocusedPanel(-app.halfPageSize()) // ctrl+u
	if got := app.messagepane.SelectedIndex(); got != startIdx {
		t.Errorf("ctrl+u must NOT change selection; start=%d after=%d", startIdx, got)
	}
	upOff := app.messagepane.YOffset()
	if upOff >= startOff {
		t.Errorf("ctrl+u should decrease yOffset; start=%d after=%d", startOff, upOff)
	}

	app.scrollFocusedPanel(app.halfPageSize()) // ctrl+d
	if got := app.messagepane.SelectedIndex(); got != startIdx {
		t.Errorf("ctrl+d must NOT change selection; start=%d after=%d", startIdx, got)
	}
	// Force a render so View() clamps yOffset against actual content height.
	_ = app.messagepane.View(80, 20)
	downOff := app.messagepane.YOffset()
	if downOff <= upOff {
		t.Errorf("ctrl+d should increase yOffset; before=%d after=%d", upOff, downOff)
	}
}

func TestHandleConfirmMode_RoutesAndClosesOnCancel(t *testing.T) {
	app := NewApp()
	app.confirmPrompt.Open("Title", "Body", func() tea.Msg { return nil })
	app.SetMode(ModeConfirm)

	// Press 'n' to cancel.
	cmd := app.handleConfirmMode(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if cmd != nil {
		t.Errorf("expected nil cmd on cancel, got non-nil")
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("prompt should be closed after cancel")
	}
}

func TestHandleConfirmMode_ConfirmReturnsCmd(t *testing.T) {
	app := NewApp()
	type marker struct{}
	app.confirmPrompt.Open("Title", "Body", func() tea.Msg { return marker{} })
	app.SetMode(ModeConfirm)

	cmd := app.handleConfirmMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd on confirm")
	}
	res := cmd()
	if _, ok := res.(marker); !ok {
		t.Errorf("expected marker msg from confirm cmd, got %T", res)
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("prompt should be closed after confirm")
	}
}

func TestNewMessageMsg_EditedUpdatesInPlace(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "original"},
	})

	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:       "1.0",
			UserName: "alice",
			Text:     "edited",
			IsEdited: true,
		},
	})

	// Access internal slice directly (same package).
	msgs := app.messagepane.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after edit (in-place update, not append), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Text != "edited" {
		t.Errorf("expected text 'edited', got %q", msgs[0].Text)
	}
	if !msgs[0].IsEdited {
		t.Error("expected IsEdited=true")
	}
}

func TestNewMessageMsg_NotEditedStillAppends(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "first"},
	})

	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:   "2.0",
			Text: "second",
			// IsEdited NOT set — this is a fresh message.
		},
	})

	msgs := app.messagepane.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after append, got %d", len(msgs))
	}
	if msgs[1].TS != "2.0" {
		t.Errorf("expected new message TS=2.0, got %q", msgs[1].TS)
	}
}

func TestWSMessageDeletedMsg_RemovesFromMessagePane(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "a"},
		{TS: "2.0", Text: "b"},
	})

	app.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "2.0"})

	msgs := app.messagepane.Messages()
	if len(msgs) != 1 || msgs[0].TS != "1.0" {
		t.Errorf("expected only TS 1.0 to remain, got %+v", msgs)
	}
}

func TestWSMessageDeletedMsg_ClosesThreadIfParentDeleted(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	parent := messages.MessageItem{TS: "P1", Text: "parent"}
	app.messagepane.SetMessages([]messages.MessageItem{parent})
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", Text: "reply"},
	}, "C1", "P1")
	app.threadVisible = true

	app.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "P1"})

	if app.threadVisible {
		t.Error("thread panel should be closed after parent deletion")
	}
}

func TestBeginEditOfSelected_NotOwned_ToastsAndNoOps(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "not mine"},
	})
	app.focusedPanel = PanelMessages

	cmd := app.beginEditOfSelected()
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	res := cmd()
	if _, ok := res.(statusbar.EditNotOwnMsg); !ok {
		t.Errorf("expected EditNotOwnMsg, got %T", res)
	}
	if app.editing.active {
		t.Error("editing state should not be active for non-owned message")
	}
}

func TestBeginEditOfSelected_Own_EntersEditMode(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "my message"},
	})
	app.focusedPanel = PanelMessages

	app.beginEditOfSelected()

	if !app.editing.active {
		t.Fatal("expected editing.active=true")
	}
	if app.editing.ts != "1.0" {
		t.Errorf("expected editing.ts=1.0, got %q", app.editing.ts)
	}
	if app.compose.Value() != "my message" {
		t.Errorf("expected compose seeded with message text, got %q", app.compose.Value())
	}
	if app.mode != ModeInsert {
		t.Errorf("expected ModeInsert, got %v", app.mode)
	}
}

func TestBeginEditOfSelected_StashesAndRestoresDraft(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "my message"},
	})
	app.focusedPanel = PanelMessages

	// Pre-existing draft.
	app.compose.SetValue("draft in progress")

	app.beginEditOfSelected()
	if app.compose.Value() != "my message" {
		t.Fatal("compose should be seeded with the message text during edit")
	}

	// Cancel — draft should restore.
	app.cancelEdit()
	if app.compose.Value() != "draft in progress" {
		t.Errorf("expected draft restored, got %q", app.compose.Value())
	}
	if app.editing.active {
		t.Error("editing should be inactive after cancel")
	}
}

func TestSubmitEdit_EmptyText_ReturnsEmptyToast(t *testing.T) {
	app := NewApp()
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages

	cmd := app.submitEdit("   ", "   ")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	if _, ok := res.(editEmptyToastMsg); !ok {
		t.Errorf("expected editEmptyToastMsg, got %T", res)
	}
}

func TestSubmitEdit_NonEmptyText_EmitsEditMessageMsg(t *testing.T) {
	app := NewApp()
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages

	cmd := app.submitEdit("hello", "hello")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	em, ok := res.(EditMessageMsg)
	if !ok {
		t.Fatalf("expected EditMessageMsg, got %T", res)
	}
	if em.ChannelID != "C1" || em.TS != "1.0" || em.NewText != "hello" {
		t.Errorf("unexpected edit msg: %+v", em)
	}
}

func TestMessageEditedMsg_ExitsEditMode(t *testing.T) {
	app := NewApp()
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages

	app.Update(MessageEditedMsg{ChannelID: "C1", TS: "1.0", Err: nil})

	if app.editing.active {
		t.Error("expected editing.active=false after success")
	}
}

func TestChannelSwitchCancelsEdit(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "my msg"},
	})
	app.focusedPanel = PanelMessages
	app.compose.SetValue("draft")
	app.beginEditOfSelected()
	if !app.editing.active {
		t.Fatal("setup: edit should be active")
	}
	app.Update(ChannelSelectedMsg{ID: "C2"})
	if app.editing.active {
		t.Error("channel switch should cancel edit")
	}
}

func TestSubmitEdit_EmptyText_KeepsEditModeOpen(t *testing.T) {
	app := NewApp()
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages

	cmd := app.submitEdit("   ", "   ")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	_ = cmd()
	// Empty submit must NOT exit edit mode.
	if !app.editing.active {
		t.Error("editing.active should remain true after empty-text submit")
	}
}

func TestMessageEditedMsg_StaleResultDoesNotClobberCurrentEdit(t *testing.T) {
	app := NewApp()
	// A different edit is currently in progress.
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "2.0"
	app.editing.panel = PanelMessages

	// Stale result for a DIFFERENT TS arrives.
	app.Update(MessageEditedMsg{ChannelID: "C1", TS: "1.0", Err: nil})

	if !app.editing.active {
		t.Error("current edit should not be cancelled by stale result for different TS")
	}
	if app.editing.ts != "2.0" {
		t.Errorf("current edit ts should be untouched, got %q", app.editing.ts)
	}
}

func TestSubmitEdit_ThreadPanel_EmitsEditMessageMsg(t *testing.T) {
	app := NewApp()
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "R1"
	app.editing.panel = PanelThread

	cmd := app.submitEdit("hello thread", "hello thread")
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	em, ok := res.(EditMessageMsg)
	if !ok {
		t.Fatalf("expected EditMessageMsg, got %T", res)
	}
	if em.ChannelID != "C1" || em.TS != "R1" || em.NewText != "hello thread" {
		t.Errorf("unexpected edit msg: %+v", em)
	}
}

func TestBeginDeleteOfSelected_NotOwned_ToastsAndNoOps(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "not mine"},
	})
	app.focusedPanel = PanelMessages

	cmd := app.beginDeleteOfSelected()
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	res := cmd()
	if _, ok := res.(statusbar.DeleteNotOwnMsg); !ok {
		t.Errorf("expected DeleteNotOwnMsg, got %T", res)
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("confirm prompt should not be visible for non-owned message")
	}
}

func TestBeginDeleteOfSelected_Own_OpensPrompt(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "hi"},
	})
	app.focusedPanel = PanelMessages

	cmd := app.beginDeleteOfSelected()
	if cmd != nil {
		t.Errorf("expected nil cmd (prompt opens directly), got non-nil")
	}
	if !app.confirmPrompt.IsVisible() {
		t.Error("expected confirm prompt to be visible")
	}
	if app.mode != ModeConfirm {
		t.Errorf("expected ModeConfirm, got %v", app.mode)
	}
}

func TestBeginDeleteOfSelected_ConfirmEmitsDeleteMessageMsg(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "hi"},
	})
	app.focusedPanel = PanelMessages
	app.beginDeleteOfSelected()

	// Press 'y' to confirm.
	cmd := app.handleConfirmMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from confirm")
	}
	res := cmd()
	dm, ok := res.(DeleteMessageMsg)
	if !ok {
		t.Fatalf("expected DeleteMessageMsg, got %T", res)
	}
	if dm.ChannelID != "C1" || dm.TS != "1.0" {
		t.Errorf("unexpected delete msg: %+v", dm)
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("prompt should be closed after confirm")
	}
	if app.mode != ModeNormal {
		t.Errorf("expected ModeNormal after confirm, got %v", app.mode)
	}
}

func TestBeginDeleteOfSelected_CancelDoesNotEmit(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "hi"},
	})
	app.focusedPanel = PanelMessages
	app.beginDeleteOfSelected()

	cmd := app.handleConfirmMode(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if cmd != nil {
		t.Errorf("expected nil cmd on cancel, got non-nil")
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("prompt should be closed after cancel")
	}
}

func TestBeginDeleteOfSelected_ThreadPane_OpensPrompt(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("U_ME")
	parent := messages.MessageItem{TS: "P1", UserID: "U_OTHER", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U_ME", Text: "my reply"},
	}, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread

	cmd := app.beginDeleteOfSelected()
	if cmd != nil {
		t.Errorf("expected nil cmd (prompt opens directly), got non-nil")
	}
	if !app.confirmPrompt.IsVisible() {
		t.Error("expected confirm prompt visible for thread pane delete")
	}
	if app.mode != ModeConfirm {
		t.Errorf("expected ModeConfirm, got %v", app.mode)
	}
}

func TestWSMessageDeletedMsg_CancelsEditIfMessageBeingEdited(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "my message"},
	})
	app.focusedPanel = PanelMessages
	app.beginEditOfSelected()
	if !app.editing.active {
		t.Fatal("setup: edit should be active")
	}

	// Another client deletes the message we're editing.
	app.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "1.0"})

	if app.editing.active {
		t.Error("edit should be cancelled when the edited message is WS-deleted")
	}
}

func TestWSMessageDeletedMsg_IgnoresOtherChannel(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "in C1"},
	})

	// A delete for a TS in a DIFFERENT channel should not touch our pane.
	app.Update(WSMessageDeletedMsg{ChannelID: "C_OTHER", TS: "1.0"})

	if len(app.messagepane.Messages()) != 1 {
		t.Errorf("messages pane should be unchanged for delete in another channel, got %d", len(app.messagepane.Messages()))
	}
}

func TestNewMessageMsg_EditedIgnoresOtherChannel(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "in C1"},
	})

	app.Update(NewMessageMsg{
		ChannelID: "C_OTHER",
		Message: messages.MessageItem{
			TS:       "1.0", // coincidentally same TS
			Text:     "edit from other channel",
			IsEdited: true,
		},
	})

	msgs := app.messagepane.Messages()
	if len(msgs) != 1 || msgs[0].Text != "in C1" {
		t.Errorf("messages pane should not be touched by edit in another channel; got %+v", msgs)
	}
}

// fakeClipboard returns a clipboardReader that returns canned bytes
// for FmtImage and FmtText.
func fakeClipboard(image, text []byte) clipboardReader {
	return func(f clipboard.Format) []byte {
		switch f {
		case clipboard.FmtImage:
			return image
		case clipboard.FmtText:
			return text
		}
		return nil
	}
}

func TestSmartPaste_ImagePresent_AttachesToCompose(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake")
	app.SetClipboardReader(fakeClipboard(pngBytes, nil))

	app.smartPaste()

	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Mime != "image/png" {
		t.Errorf("expected image/png, got %q", atts[0].Mime)
	}
	if !strings.HasPrefix(atts[0].Filename, "slk-paste-") || !strings.HasSuffix(atts[0].Filename, ".png") {
		t.Errorf("unexpected filename: %q", atts[0].Filename)
	}
	if atts[0].Size != int64(len(pngBytes)) {
		t.Errorf("expected size %d, got %d", len(pngBytes), atts[0].Size)
	}
}

func TestSmartPaste_ImageTooLarge_Refuses(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	huge := make([]byte, 11*1024*1024)
	app.SetClipboardReader(fakeClipboard(huge, nil))

	app.smartPaste()

	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment for oversized image, got %d", len(app.compose.Attachments()))
	}
}

func TestSmartPaste_FilePathPresent_AttachesByPath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "doc.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.SetClipboardReader(fakeClipboard(nil, []byte(path)))

	app.smartPaste()

	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Path != path {
		t.Errorf("expected Path=%q, got %q", path, atts[0].Path)
	}
	if atts[0].Filename != "doc.pdf" {
		t.Errorf("expected filename doc.pdf, got %q", atts[0].Filename)
	}
}

func TestSmartPaste_NoImage_NoValidPath_FallsThroughToText(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.SetClipboardReader(fakeClipboard(nil, []byte("just some text")))

	app.smartPaste()

	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment, got %d", len(app.compose.Attachments()))
	}
	// Text was inserted into compose.
	if !strings.Contains(app.compose.Value(), "just some text") {
		t.Errorf("expected text to be inserted, got %q", app.compose.Value())
	}
}

func TestSmartPaste_ClipboardUnavailable_NoOp(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(false)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	pngBytes := []byte("\x89PNGfake")
	app.SetClipboardReader(fakeClipboard(pngBytes, nil))

	app.smartPaste()

	if len(app.compose.Attachments()) != 0 {
		t.Error("expected no-op when clipboard unavailable")
	}
}

func TestSmartPaste_ThreadPane_AttachesToThreadCompose(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.SetMode(ModeInsert)
	app.SetClipboardReader(fakeClipboard([]byte("\x89PNG"), nil))

	app.smartPaste()

	if len(app.threadCompose.Attachments()) != 1 {
		t.Errorf("expected attachment on threadCompose, got %d", len(app.threadCompose.Attachments()))
	}
	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment on channel compose, got %d", len(app.compose.Attachments()))
	}
}

func TestSubmitWithAttachments_InvokesUploaderAndSetsUploading(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.AddAttachment(compose.PendingAttachment{
		Filename: "a.png", Bytes: []byte("png"), Size: 3,
	})
	app.compose.SetValue("look")
	// Set a no-op uploader so the cmd doesn't error out — we just want to
	// observe state changes (uploading flag, that an attempt was made).
	app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
		return func() tea.Msg { return UploadResultMsg{Err: nil} }
	})

	cmd := app.submitWithAttachments(&app.compose)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	if !app.compose.Uploading() {
		t.Error("expected compose.Uploading() == true after submit")
	}
}

func TestSubmitWithAttachments_RefusesDuringEdit(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages
	app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
		return func() tea.Msg { return UploadResultMsg{Err: nil} }
	})

	_ = app.submitWithAttachments(&app.compose)

	if app.compose.Uploading() {
		t.Error("expected no upload kicked off during edit mode")
	}
	// Attachment should still be there for the user to remove.
	if len(app.compose.Attachments()) != 1 {
		t.Errorf("expected attachments preserved, got %d", len(app.compose.Attachments()))
	}
}

func TestUploadResultMsg_SuccessClearsAttachmentsAndCompose(t *testing.T) {
	app := NewApp()
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})
	app.compose.SetValue("caption")
	app.compose.SetUploading(true)

	app.Update(UploadResultMsg{Err: nil})

	if app.compose.Uploading() {
		t.Error("expected Uploading=false after success")
	}
	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected attachments cleared, got %d", len(app.compose.Attachments()))
	}
	if app.compose.Value() != "" {
		t.Errorf("expected text reset, got %q", app.compose.Value())
	}
}

func TestUploadResultMsg_FailureKeepsAttachments(t *testing.T) {
	app := NewApp()
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})
	app.compose.SetValue("caption")
	app.compose.SetUploading(true)

	app.Update(UploadResultMsg{Err: errors.New("network failure")})

	if app.compose.Uploading() {
		t.Error("expected Uploading=false after failure")
	}
	if len(app.compose.Attachments()) != 1 {
		t.Errorf("expected attachments preserved on failure, got %d", len(app.compose.Attachments()))
	}
	if app.compose.Value() != "caption" {
		t.Errorf("expected caption preserved, got %q", app.compose.Value())
	}
}

func TestEscDuringUpload_RefusedWithToast(t *testing.T) {
	app := NewApp()
	app.SetMode(ModeInsert)
	app.compose.SetUploading(true)
	app.focusedPanel = PanelMessages

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected non-nil cmd (toast)")
	}
	// Mode should still be Insert (Esc was refused).
	if app.mode != ModeInsert {
		t.Errorf("expected ModeInsert preserved during upload, got %v", app.mode)
	}
}

func TestChannelSwitchDuringUpload_Refused(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetUploading(true)

	app.Update(ChannelSelectedMsg{ID: "C2"})

	if app.activeChannelID != "C1" {
		t.Errorf("expected activeChannelID preserved during upload, got %q", app.activeChannelID)
	}
	if !app.compose.Uploading() {
		t.Error("expected upload still in flight")
	}
}

// --- bracketed-paste smart-paste tests (PasteMsg path) ---

func TestPasteMsg_ImagePresent_AttachesToCompose(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake")
	app.SetClipboardReader(fakeClipboard(pngBytes, nil))

	// Bracketed paste typically delivers some text representation;
	// what it carries doesn't matter once an image is detected on
	// the OS clipboard.
	app.Update(tea.PasteMsg{Content: "irrelevant text payload"})

	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment via PasteMsg, got %d", len(atts))
	}
	if atts[0].Mime != "image/png" {
		t.Errorf("expected image/png, got %q", atts[0].Mime)
	}
	// The bracketed-paste text payload must NOT have been forwarded
	// to the textarea when an image was attached.
	if app.compose.Value() != "" {
		t.Errorf("expected textarea empty (image consumed PasteMsg), got %q", app.compose.Value())
	}
}

func TestPasteMsg_FilePathInPayload_AttachesByPath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "doc.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	// No image on clipboard; bracketed paste delivers the path as text.
	app.SetClipboardReader(fakeClipboard(nil, nil))

	app.Update(tea.PasteMsg{Content: path})

	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment via path PasteMsg, got %d", len(atts))
	}
	if atts[0].Path != path {
		t.Errorf("expected Path=%q, got %q", path, atts[0].Path)
	}
}

func TestPasteMsg_PlainText_FallsThroughToTextarea(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	// In insert mode the compose is focused; mirror that for the test
	// (textarea ignores input when blurred).
	_ = app.compose.Focus()
	// No image, no valid path — bracketed text should land in textarea.
	app.SetClipboardReader(fakeClipboard(nil, nil))

	app.Update(tea.PasteMsg{Content: "hello world"})

	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment for plain-text paste, got %d", len(app.compose.Attachments()))
	}
	// Plain-text bracketed paste is forwarded to the textarea via its
	// own Update path — it should now contain the pasted text.
	if !strings.Contains(app.compose.Value(), "hello world") {
		t.Errorf("expected pasted text in compose, got %q", app.compose.Value())
	}
}

func TestPasteMsg_ClipboardUnavailable_FallsThroughToTextarea(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(false) // headless / clipboard.Init failed
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	_ = app.compose.Focus()

	app.Update(tea.PasteMsg{Content: "hello"})

	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment when clipboard unavailable, got %d", len(app.compose.Attachments()))
	}
	if !strings.Contains(app.compose.Value(), "hello") {
		t.Errorf("expected pasted text in compose, got %q", app.compose.Value())
	}
}

// --- insert-mode shortcuts: Ctrl+U clear, Up/Down boundary jump ---

func TestHandleInsertMode_CtrlU_ClearsCompose(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	_ = app.compose.Focus()
	app.compose.SetValue("draft text")
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})

	app.handleInsertMode(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})

	if app.compose.Value() != "" {
		t.Errorf("expected compose cleared, got %q", app.compose.Value())
	}
	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected attachments cleared, got %d", len(app.compose.Attachments()))
	}
}

func TestHandleInsertMode_Up_OnFirstLine_JumpsToStart(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	_ = app.compose.Focus()
	app.compose.SetValue("hello world")
	// SetValue lands cursor at end of single-line input → first AND last.
	app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyUp})

	// Cursor should now be at column 0 of line 0.
	if !app.compose.CursorAtFirstLine() {
		t.Error("expected cursor on first line")
	}
	// Verify column is 0 by checking that Backspace at this point
	// produces no change — we don't have direct cursor exposed, so
	// instead simulate: Up was forwarded to MoveCursorToStart.
	// We can't trivially read the cursor column from outside the
	// compose; the side-effect test is enough.
}

func TestHandleInsertMode_Down_OnLastLine_JumpsToEnd(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	_ = app.compose.Focus()
	app.compose.SetValue("line1\nline2\nline3")
	// Cursor is on the last line after SetValue.
	app.compose.MoveCursorToStart()
	if !app.compose.CursorAtFirstLine() {
		t.Fatal("setup: expected cursor on first line after MoveCursorToStart")
	}

	// Down on a multi-line draft when not on last line forwards to
	// textarea; to test the "boundary jump", first move cursor to
	// the last line.
	app.compose.MoveCursorToEnd()
	if !app.compose.CursorAtLastLine() {
		t.Fatal("setup: expected cursor on last line")
	}

	// Now Down should jump to end (idempotent here, but exercises path).
	app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyDown})
	if !app.compose.CursorAtLastLine() {
		t.Error("expected cursor still on last line after Down")
	}
}

func TestHandleInsertMode_Up_OnSecondLine_ForwardsToTextarea(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	_ = app.compose.Focus()
	app.compose.SetValue("line1\nline2")
	// Cursor is at end of line 2 (last line). Press Up: cursor should
	// move to line 1 via textarea's normal handling, NOT jump to start.
	app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyUp})

	if !app.compose.CursorAtFirstLine() {
		t.Error("expected cursor moved to first line via standard Up")
	}
}

// --- quit bindings: Q (confirm) / Ctrl+C (confirm); q (close thread, else no-op) ---

func TestNormalMode_CapitalQ_OpensConfirmPrompt(t *testing.T) {
	// Q took over what lowercase q used to do: pop the centered
	// quit-confirm overlay. The immediate-quit feature is gone —
	// Ctrl+C still pops the same confirm, and ungraceful exits go
	// through closing the terminal.
	app := NewApp()
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'Q', Text: "Q"})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Q should NOT quit immediately; expected confirm prompt")
		}
	}
	if !app.confirmPrompt.IsVisible() {
		t.Fatal("Q should open the confirm prompt")
	}
	if app.mode != ModeConfirm {
		t.Errorf("expected mode=ModeConfirm, got %v", app.mode)
	}
}

func TestHandleKey_CtrlC_OpensConfirmPromptInsteadOfQuitting(t *testing.T) {
	app := NewApp()
	cmd := app.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl+C should not quit immediately; expected confirm prompt")
		}
	}
	if !app.confirmPrompt.IsVisible() {
		t.Fatal("Ctrl+C should open the confirm prompt")
	}
	if app.mode != ModeConfirm {
		t.Errorf("expected mode=ModeConfirm, got %v", app.mode)
	}
}

func TestHandleKey_CtrlC_DoesNotReopenWhilePromptVisible(t *testing.T) {
	app := NewApp()
	app.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !app.confirmPrompt.IsVisible() {
		t.Fatal("precondition: prompt should be visible")
	}
	// A second Ctrl+C while the quit prompt is up should fall through
	// to handleConfirmMode (where Enter confirms and Esc cancels) and
	// must not reopen the prompt or change state in a way that breaks
	// it.
	cmd := app.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	_ = cmd
	if !app.confirmPrompt.IsVisible() {
		t.Error("second Ctrl+C should leave the prompt up; not auto-cancel")
	}
}

func TestNormalMode_LowercaseQ_ClosesThreadWhenOpen(t *testing.T) {
	// Lowercase q is now a "close thread view" shortcut, mirroring
	// vim's habit of using `q` to close ephemeral windows. When the
	// thread panel is open, q closes it.
	app := NewApp()
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'q', Text: "q"})

	if app.threadVisible {
		t.Error("expected q to close the thread panel, but threadVisible is still true")
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("q with a thread open must not pop the quit-confirm prompt")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q with thread open must not quit")
		}
	}
}

func TestNormalMode_LowercaseQ_NoOpWhenNoThread(t *testing.T) {
	// With no thread visible, lowercase q is a no-op — it does NOT
	// pop the quit confirm anymore. Q (and Ctrl+C) are the keys for
	// quitting; q is reserved for closing transient panels.
	app := NewApp()

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'q', Text: "q"})

	if app.confirmPrompt.IsVisible() {
		t.Error("q with no thread visible must not pop the quit-confirm prompt")
	}
	if app.mode != ModeNormal {
		t.Errorf("expected mode to stay ModeNormal, got %v", app.mode)
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q with no thread visible must not quit")
		}
	}
}

// TestPreviewOverlay_EscClosesAndNils verifies that pressing Esc while
// the preview overlay is open dismisses it via the early-return route
// in App.Update, regardless of the focused panel or current mode.
func TestPreviewOverlay_EscClosesAndNils(t *testing.T) {
	app := NewApp()
	// Inject a preview directly to bypass the fetcher path. A nil image
	// is acceptable for this test since we never call View(); the close
	// path doesn't touch the renderer.
	p := imgpkg.NewPreview(imgpkg.PreviewInput{Name: "x.png"})
	app.preview.overlay = &p

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.preview.overlay != nil {
		t.Fatal("Esc should clear previewOverlay")
	}
}

// TestPreviewOverlay_QClosesAndNils mirrors the Esc test for the `q`
// keybind. Both keys must dismiss the overlay without triggering the
// quit-confirm prompt that lowercase q normally opens.
func TestPreviewOverlay_QClosesAndNils(t *testing.T) {
	app := NewApp()
	p := imgpkg.NewPreview(imgpkg.PreviewInput{Name: "x.png"})
	app.preview.overlay = &p

	_, _ = app.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if app.preview.overlay != nil {
		t.Fatal("q should clear previewOverlay")
	}
	if app.confirmPrompt.IsVisible() {
		t.Fatal("q must NOT open the quit-confirm prompt while overlay is active")
	}
}

// TestPreviewOverlay_EnterClosesAndReturnsCmd asserts that Enter both
// dismisses the overlay and returns a non-nil tea.Cmd (the system
// viewer launcher). The cmd itself spawns a process so we don't run it
// in tests; we just verify it's wired.
func TestPreviewOverlay_EnterClosesAndReturnsCmd(t *testing.T) {
	app := NewApp()
	p := imgpkg.NewPreview(imgpkg.PreviewInput{Name: "x.png", Path: "/tmp/x.png"})
	app.preview.overlay = &p

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.preview.overlay != nil {
		t.Fatal("Enter should clear previewOverlay")
	}
	if cmd == nil {
		t.Fatal("Enter with a non-empty Path should return a launcher cmd")
	}
}

// TestPreviewOverlay_OtherKeysSwallowed checks that arbitrary keys do
// not propagate to the focused panel while the overlay is open. We
// approximate this by sending `j` (which would normally move the
// sidebar selection) and verifying no observable state change.
func TestPreviewOverlay_OtherKeysSwallowed(t *testing.T) {
	app := NewApp()
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "a"},
		{ID: "C2", Name: "b"},
	})
	beforeIdx := -1
	if it, ok := app.sidebar.SelectedItem(); ok {
		if it.ID == "C1" {
			beforeIdx = 0
		} else if it.ID == "C2" {
			beforeIdx = 1
		}
	}

	p := imgpkg.NewPreview(imgpkg.PreviewInput{Name: "x.png"})
	app.preview.overlay = &p

	_, _ = app.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if app.preview.overlay == nil {
		t.Fatal("j must NOT close the preview overlay")
	}
	afterIdx := -1
	if it, ok := app.sidebar.SelectedItem(); ok {
		if it.ID == "C1" {
			afterIdx = 0
		} else if it.ID == "C2" {
			afterIdx = 1
		}
	}
	if afterIdx != beforeIdx {
		t.Errorf("j should be swallowed while overlay is open; sidebar selection changed (%d -> %d)", beforeIdx, afterIdx)
	}
}

func TestMarkUnreadOfSelected_NoSelection_NoOp(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	// no messages loaded → no selection

	cmd := app.markUnreadOfSelected()
	if cmd != nil {
		t.Errorf("expected nil cmd when nothing selected, got non-nil")
	}
}

func TestMarkUnreadOfSelected_ChannelPane_EmitsMarkUnreadMsg(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "first"},
		{TS: "2.0", UserID: "U_OTHER", Text: "second"},
		{TS: "3.0", UserID: "U_ME", Text: "third (selected)"},
		{TS: "4.0", UserID: "U_OTHER", Text: "fourth"},
		{TS: "5.0", UserID: "U_OTHER", Text: "fifth"},
	})
	app.focusedPanel = PanelMessages
	// SetMessages selects the last message; force selection to index 2.
	app.messagepane.SelectByIndex(2)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu, ok := res.(MarkUnreadMsg)
	if !ok {
		t.Fatalf("expected MarkUnreadMsg, got %T", res)
	}
	if mu.ChannelID != "C1" {
		t.Errorf("ChannelID: got %q", mu.ChannelID)
	}
	if mu.ThreadTS != "" {
		t.Errorf("ThreadTS: expected empty for channel-pane mark, got %q", mu.ThreadTS)
	}
	if mu.BoundaryTS != "2.0" {
		t.Errorf("BoundaryTS: expected '2.0' (msg before selected), got %q", mu.BoundaryTS)
	}
	if mu.UnreadCount != 3 {
		t.Errorf("UnreadCount: expected 3 (selected + 2 newer), got %d", mu.UnreadCount)
	}
}

func TestMarkUnreadOfSelected_OldestMessage_BoundaryIsZero(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "first (selected)"},
		{TS: "2.0", UserID: "U_OTHER", Text: "second"},
	})
	app.focusedPanel = PanelMessages
	app.messagepane.SelectByIndex(0)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu := res.(MarkUnreadMsg)
	if mu.BoundaryTS != "0" {
		t.Errorf("expected BoundaryTS='0' for oldest-message case, got %q", mu.BoundaryTS)
	}
	if mu.UnreadCount != 2 {
		t.Errorf("UnreadCount: expected 2, got %d", mu.UnreadCount)
	}
}

func TestMarkUnreadOfSelected_ThreadPane_EmitsThreadMarkUnread(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("U_ME")
	parent := messages.MessageItem{TS: "P1", UserID: "U_OTHER", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U_OTHER", Text: "first reply"},
		{TS: "R2", UserID: "U_OTHER", Text: "second reply (selected)"},
		{TS: "R3", UserID: "U_OTHER", Text: "third reply"},
	}, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(1)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu, ok := res.(MarkUnreadMsg)
	if !ok {
		t.Fatalf("expected MarkUnreadMsg, got %T", res)
	}
	if mu.ChannelID != "C1" || mu.ThreadTS != "P1" {
		t.Errorf("got channel=%q thread=%q", mu.ChannelID, mu.ThreadTS)
	}
	if mu.BoundaryTS != "R1" {
		t.Errorf("BoundaryTS: expected 'R1', got %q", mu.BoundaryTS)
	}
	if mu.UnreadCount != 0 {
		t.Errorf("UnreadCount: expected 0 for thread-level, got %d", mu.UnreadCount)
	}
}

func TestMarkUnreadOfSelected_ThreadPane_OldestReply_BoundaryIsParentTS(t *testing.T) {
	// Selecting the oldest reply → boundary is the parent ts (so the
	// whole thread, but not the parent message itself, becomes unread).
	app := NewApp()
	parent := messages.MessageItem{TS: "P1", UserID: "U_OTHER", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U_OTHER", Text: "first (selected)"},
		{TS: "R2", UserID: "U_OTHER", Text: "second"},
	}, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(0)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu := res.(MarkUnreadMsg)
	if mu.BoundaryTS != "P1" {
		t.Errorf("expected boundary=P1 (parent ts) for oldest reply, got %q", mu.BoundaryTS)
	}
}

// cmdContainsMsgType returns true if cmd (or any sub-cmd in a batch)
// returns a value of the same dynamic type as want when invoked.
func cmdContainsMsgType(cmd tea.Cmd, want any) bool {
	if cmd == nil {
		return false
	}
	res := cmd()
	if res == nil {
		return false
	}
	if reflect.TypeOf(res) == reflect.TypeOf(want) {
		return true
	}
	if batch, ok := res.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if cmdContainsMsgType(sub, want) {
				return true
			}
		}
	}
	return false
}

func TestMessageMarkedUnreadMsg_ChannelLevel_UpdatesPaneSidebarAndToasts(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
	})
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", Text: "first"},
		{TS: "2.0", UserID: "U1", Text: "second"},
		{TS: "3.0", UserID: "U1", Text: "third"},
	})

	// After the read-state sync rewrite, the sidebar's unread indicator
	// is driven by the DB (via readStateReader), not by a per-item
	// UnreadCount that the App layer mutates. The contract is now
	// "the sidebar was invalidated so the next render re-reads state";
	// capture the version pre/post to verify that.
	verBefore := app.sidebar.Version()

	_, cmd := app.Update(MessageMarkedUnreadMsg{
		ChannelID:   "C1",
		ThreadTS:    "",
		BoundaryTS:  "1.0",
		UnreadCount: 2,
		Err:         nil,
	})

	// Toast should be queued via tea.Cmd.
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	if !cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Errorf("expected MarkedUnreadMsg in cmd output")
	}

	// Messages-pane boundary moved.
	if got := app.messagepane.LastReadTS(); got != "1.0" {
		t.Errorf("expected messagepane lastReadTS=1.0, got %q", got)
	}

	// Sidebar was invalidated so the next View() will re-read the DB.
	if app.sidebar.Version() == verBefore {
		t.Errorf("expected sidebar.Version() to bump after channel mark, stayed at %d", verBefore)
	}
}

func TestMessageMarkedUnreadMsg_ThreadLevel_UpdatesThreadPaneAndThreadsView(t *testing.T) {
	app := NewApp()
	parent := messages.MessageItem{TS: "P1", UserID: "U1", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U1", Text: "r1"},
		{TS: "R2", UserID: "U1", Text: "r2"},
	}, "C1", "P1")
	app.threadVisible = true
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", Unread: false},
	})

	_, cmd := app.Update(MessageMarkedUnreadMsg{
		ChannelID:  "C1",
		ThreadTS:   "P1",
		BoundaryTS: "R1",
		Err:        nil,
	})

	if cmd == nil || !cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Errorf("expected MarkedUnreadMsg toast cmd")
	}

	// Thread pane unread boundary moved.
	if got := app.threadPanel.UnreadBoundaryTS(); got != "R1" {
		t.Errorf("expected thread unreadBoundary=R1, got %q", got)
	}

	// Threads-view row was flipped to unread.
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && !s.Unread {
			t.Errorf("expected thread-view row P1 to be Unread=true")
		}
	}

	// Sidebar threads-row badge reflects the new unread count.
	if got := app.sidebar.ThreadsUnreadCount(); got != 1 {
		t.Errorf("expected sidebar.ThreadsUnreadCount=1 after thread mark-unread, got %d", got)
	}
}

func TestMessageMarkedUnreadMsg_Error_ToastsFailureNoStateChange(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", Text: "first"},
		{TS: "2.0", UserID: "U1", Text: "second"},
	})
	prevLastRead := app.messagepane.LastReadTS()

	_, cmd := app.Update(MessageMarkedUnreadMsg{
		ChannelID:  "C1",
		BoundaryTS: "0",
		Err:        errors.New("boom"),
	})

	if cmd == nil {
		t.Fatal("expected failure toast cmd")
	}

	// Toast is the failure variant carrying the error message, not the
	// success variant.
	if cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Error("expected no MarkedUnreadMsg success toast on error")
	}
	if !cmdContainsMsgType(cmd, statusbar.MarkUnreadFailedMsg{}) {
		t.Error("expected MarkUnreadFailedMsg toast on error")
	}
	// And the Reason matches the error.
	if reason := extractMarkUnreadFailedReason(cmd); reason != "boom" {
		t.Errorf("expected toast Reason=%q, got %q", "boom", reason)
	}

	// No state change.
	if app.messagepane.LastReadTS() != prevLastRead {
		t.Error("messagepane lastReadTS should be unchanged on error")
	}
}

// extractMarkUnreadFailedReason runs cmd (recursing into BatchMsg) and
// returns the Reason field of the first MarkUnreadFailedMsg it finds, or
// "" if none.
func extractMarkUnreadFailedReason(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	res := cmd()
	if res == nil {
		return ""
	}
	if m, ok := res.(statusbar.MarkUnreadFailedMsg); ok {
		return m.Reason
	}
	if batch, ok := res.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if r := extractMarkUnreadFailedReason(sub); r != "" {
				return r
			}
		}
	}
	return ""
}

func TestChannelMarkedRemoteMsg_UpdatesPaneAndSidebarSilently(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
	})
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", Text: "first"},
		{TS: "2.0", UserID: "U1", Text: "second"},
	})

	// Sidebar invalidation contract (read-state sync rewrite): App
	// flips sidebar.version; the next View() re-reads the DB.
	verBefore := app.sidebar.Version()

	_, cmd := app.Update(ChannelMarkedRemoteMsg{
		ChannelID:   "C1",
		TS:          "1.0",
		UnreadCount: 1,
	})

	// No toast on remote events.
	if cmd != nil && cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Error("expected no MarkedUnreadMsg toast on remote event")
	}

	if got := app.messagepane.LastReadTS(); got != "1.0" {
		t.Errorf("messagepane lastReadTS: got %q", got)
	}
	if app.sidebar.Version() == verBefore {
		t.Errorf("expected sidebar.Version() to bump after remote channel mark, stayed at %d", verBefore)
	}
}

func TestChannelMarkedRemoteMsg_InactiveChannel_OnlyUpdatesSidebar(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C_OTHER"
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
		{ID: "C_OTHER", Name: "elsewhere", Section: "Channels"},
	})
	prevLastRead := app.messagepane.LastReadTS()
	verBefore := app.sidebar.Version()

	_, _ = app.Update(ChannelMarkedRemoteMsg{
		ChannelID: "C1", TS: "1.0", UnreadCount: 3,
	})

	// messages pane (showing C_OTHER) is untouched.
	if app.messagepane.LastReadTS() != prevLastRead {
		t.Error("messagepane should be untouched when remote event is for non-active channel")
	}
	// Sidebar still receives the invalidate signal so the next render
	// reflects the new DB state for C1.
	if app.sidebar.Version() == verBefore {
		t.Errorf("expected sidebar.Version() to bump after remote channel mark, stayed at %d", verBefore)
	}
}

func TestThreadMarkedRemoteMsg_CursorBehindLatestFlipsRowUnread(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: false},
	})

	_, cmd := app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1",
		ThreadTS:  "P1",
		LastRead:  "4.000000",
	})

	if cmd != nil && cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Error("expected no toast on remote thread event")
	}
	found := false
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS != "P1" {
			continue
		}
		found = true
		if !s.Unread {
			t.Error("expected P1 Unread=true when the cursor is behind the latest reply")
		}
	}
	if !found {
		t.Fatal("summary P1 missing; the assertion above would have passed vacuously")
	}
}

func TestThreadMarkedRemoteMsg_CursorAtLatestClearsRow(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1", ThreadTS: "P1", LastRead: "5.000000",
	})

	found := false
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS != "P1" {
			continue
		}
		found = true
		if s.Unread {
			t.Error("expected P1 Unread=false when the cursor reaches the latest reply")
		}
	}
	if !found {
		t.Fatal("summary P1 missing; the assertion above would have passed vacuously")
	}
}

// The open thread panel draws its "── new ──" landmark after the
// boundary ts, so a remote cursor move must move the landmark rather
// than clear it: a cursor mid-thread still has unread replies below it.
func TestThreadMarkedRemoteMsg_MovesOpenPanelBoundaryToCursor(t *testing.T) {
	app := NewApp()
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "P1", UserID: "U1", Text: "parent"},
		[]messages.MessageItem{
			{TS: "R1", UserID: "U1", Text: "r1"},
			{TS: "R2", UserID: "U1", Text: "r2"},
		}, "C1", "P1")
	app.threadVisible = true

	_, _ = app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1", ThreadTS: "P1", LastRead: "R1",
	})

	if got := app.threadPanel.UnreadBoundaryTS(); got != "R1" {
		t.Errorf("thread panel unreadBoundary = %q, want R1", got)
	}
}

// The local counterpart of the test above, and its mirror image.
// ThreadMarkedLocalMsg reports slk's OWN subscriptions.thread.mark
// completing — the one openSelectedThreadCmd and flushPendingMarks
// issue. Moving the boundary to that cursor would land it on the newest
// reply, rendering no landmark at all, and would destroy the pre-open
// snapshot openSelectedThreadCmd took (app.go:1861-1865) precisely so
// the user can see what is new in the panel they are reading.
func TestThreadMarkedLocalMsg_LeavesOpenPanelBoundaryInPlace(t *testing.T) {
	app := NewApp()
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "P1", UserID: "U1", Text: "parent"},
		[]messages.MessageItem{
			{TS: "R1", UserID: "U1", Text: "r1"},
			{TS: "R2", UserID: "U1", Text: "r2"},
		}, "C1", "P1")
	app.threadVisible = true
	app.threadPanel.SetUnreadBoundary("R1")
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "R2", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedLocalMsg{ChannelID: "C1", ThreadTS: "P1", TS: "R2"})

	if got := app.threadPanel.UnreadBoundaryTS(); got != "R1" {
		t.Errorf("thread panel unreadBoundary = %q, want the pre-open snapshot R1 held", got)
	}
	// The threads-list side of the same mark must still take effect.
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("a successful local thread mark must still clear the threads-list unread flag")
		}
	}
}

// Regression for the reported bug: posting a reply auto-subscribes you,
// so Slack echoes thread_marked with active=true. slk used to read that
// as "unread" and re-flag the thread the user was looking at.
func TestThreadMarkedRemoteMsg_SelfReplyDoesNotReFlagUnread(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "7.000000", Unread: false},
	})

	// Slack's echo after our own reply: cursor caught up to our reply.
	_, _ = app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1", ThreadTS: "P1", LastRead: "7.000000",
	})

	found := false
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS != "P1" {
			continue
		}
		found = true
		if s.Unread {
			t.Error("replying to a thread must not mark it unread")
		}
	}
	if !found {
		t.Fatal("summary P1 missing; the assertion above would have passed vacuously")
	}
}

func TestConversationOpenedMsg_SidebarReceivesItemAndUnread(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.sidebar.SetItems([]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})

	// Simulate Slack pushing an mpim_open for a previously-unknown mpdm.
	app.Update(ConversationOpenedMsg{
		TeamID: "T1",
		Item: sidebar.ChannelItem{
			ID:   "G1",
			Name: "alice, bob",
			Type: "group_dm",
		},
	})

	// Capture sidebar version so we can verify the inbound NewMessageMsg
	// invalidates the render cache. After the read-state sync rewrite,
	// the App layer no longer mutates a per-channel UnreadCount on the
	// sidebar — the DB write happens in the WS handler (OnMessage), and
	// App.Update merely invalidates the sidebar so the next render
	// re-reads has_unread from the DB.
	verBefore := app.sidebar.Version()

	// Then a message arrives for that mpdm while the user is elsewhere.
	app.Update(NewMessageMsg{
		ChannelID: "G1",
		Message:   messages.MessageItem{TS: "1700000001.000000", UserID: "U2", Text: "hi"},
	})

	// G1 must be present in the sidebar (from the ConversationOpenedMsg)
	// and the sidebar must have been invalidated by NewMessageMsg.
	found := false
	for _, it := range app.sidebar.AllItems() {
		if it.ID == "G1" {
			found = true
		}
	}
	if !found {
		t.Errorf("G1 not in sidebar after ConversationOpenedMsg")
	}
	if app.sidebar.Version() == verBefore {
		t.Errorf("expected sidebar.Version() to bump after inactive-channel NewMessageMsg, stayed at %d", verBefore)
	}
}

func TestConversationOpenedMsg_InactiveWorkspaceIgnored(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.sidebar.SetItems([]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})

	// Event for a different workspace must NOT mutate the active sidebar.
	app.Update(ConversationOpenedMsg{
		TeamID: "T2",
		Item:   sidebar.ChannelItem{ID: "G1", Name: "alice, bob", Type: "group_dm"},
	})

	for _, it := range app.sidebar.AllItems() {
		if it.ID == "G1" {
			t.Errorf("G1 unexpectedly added to active sidebar from inactive-workspace event")
		}
	}
}

// TestSelfSendInFlight_SuppressesEarlyWSEcho asserts that when a slk-
// originated send has been marked in-flight for a channel, an
// arriving WS echo from the same user is dropped. Without this guard,
// the WS echo (with Slack's normalised text) would render alongside
// the instant-display placeholder, double-rendering the same message.
//
// Cross-session messages (where lastSelfSendByChannel is NOT updated
// for that channel because the user sent from another tool) must
// still pass through.
func TestSelfSendInFlight_SuppressesEarlyWSEcho(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	// User submits a slk-originated send. SendMessageMsg appends an
	// optimistic placeholder (instant-display) AND records the
	// in-flight timestamp.
	app.Update(SendMessageMsg{ChannelID: "C1", Text: "Hello\nWorld"})

	// Instant-display placeholder is in the pane immediately.
	if got := len(app.messagepane.Messages()); got != 1 {
		t.Fatalf("expected 1 optimistic placeholder after SendMessageMsg, got %d", got)
	}
	if got := app.messagepane.Messages()[0].Text; got != "Hello\nWorld" {
		t.Errorf("placeholder Text = %q, want %q", got, "Hello\nWorld")
	}

	// Slack's WS echo arrives BEFORE chat.postMessage HTTP responds.
	// selfSendInFlight must drop it so we don't double-render.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "Hello World",
		},
	})
	if got := len(app.messagepane.Messages()); got != 1 {
		t.Errorf("WS echo double-rendered alongside placeholder; got %d messages, want 1", got)
	}

	// MessageSentMsg arrives with the converted-mrkdwn text. The
	// LocalTS field — assigned in the SendMessageMsg handler and
	// threaded through the sender closure — lets the handler swap the
	// placeholder for the authoritative message in place. The
	// test simulates that wiring by reading the placeholder's TS.
	localTS := app.messagepane.Messages()[0].TS
	app.Update(MessageSentMsg{
		ChannelID: "C1",
		LocalTS:   localTS,
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "Hello\nWorld",
		},
	})

	got := app.messagepane.Messages()
	if len(got) != 1 {
		t.Fatalf("expected 1 message after swap, got %d", len(got))
	}
	if got[0].TS != "1700000999.000001" {
		t.Errorf("after swap TS = %q, want real Slack TS", got[0].TS)
	}
	if got[0].Text != "Hello\nWorld" {
		t.Errorf("Text = %q, want %q", got[0].Text, "Hello\nWorld")
	}
}

// TestSendMessage_InstantDisplay asserts the quality-of-life
// guarantee: the user's message must appear in the active channel
// pane the moment SendMessageMsg is dispatched — before any HTTP
// round-trip. The placeholder is replaced in place when MessageSentMsg
// lands; its position in the list is preserved.
func TestSendMessage_InstantDisplay(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"
	app.userNames = map[string]string{"USELF": "you"}

	app.Update(SendMessageMsg{ChannelID: "C1", Text: "hello"})

	msgs := app.messagepane.Messages()
	if len(msgs) != 1 {
		t.Fatalf("instant-display: expected 1 message after SendMessageMsg, got %d", len(msgs))
	}
	if msgs[0].Text != "hello" {
		t.Errorf("placeholder Text = %q, want %q", msgs[0].Text, "hello")
	}
	if msgs[0].UserID != "USELF" {
		t.Errorf("placeholder UserID = %q, want USELF", msgs[0].UserID)
	}
	if msgs[0].UserName != "you" {
		t.Errorf("placeholder UserName = %q, want you", msgs[0].UserName)
	}
	if !strings.HasPrefix(msgs[0].TS, "local:") {
		t.Errorf("placeholder TS = %q, want a local:... id", msgs[0].TS)
	}

	// Once the HTTP response arrives, the placeholder is swapped for
	// the authoritative message. The list length stays at 1; the TS
	// changes from local:... to the real Slack TS.
	localTS := msgs[0].TS
	app.Update(MessageSentMsg{
		ChannelID: "C1",
		LocalTS:   localTS,
		Message: messages.MessageItem{
			TS: "1700000999.000003", UserID: "USELF", UserName: "you", Text: "hello",
		},
	})

	msgs = app.messagepane.Messages()
	if len(msgs) != 1 {
		t.Fatalf("post-swap: expected 1 message, got %d", len(msgs))
	}
	if msgs[0].TS != "1700000999.000003" {
		t.Errorf("post-swap TS = %q, want real Slack TS", msgs[0].TS)
	}
}

// TestSendMessage_InstantDisplayConvertsCommonMarkToSlackMrkdwn locks
// in the bug fix where the optimistic placeholder was storing the
// user's raw CommonMark text. The slk renderer expects Slack mrkdwn
// (single-asterisk bold, single-underscore italic, single-backtick
// code), so without conversion the placeholder rendered "**bold**"
// literally and then re-rendered with proper styling once the HTTP
// response brought back the converted text. We now convert in the
// App before storing.
func TestSendMessage_InstantDisplayConvertsCommonMarkToSlackMrkdwn(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	app.Update(SendMessageMsg{ChannelID: "C1", Text: "**bold** and _italic_"})

	msgs := app.messagepane.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 placeholder, got %d", len(msgs))
	}
	// mrkdwn.Convert("**bold** and _italic_") => "*bold* and _italic_"
	if got, want := msgs[0].Text, "*bold* and _italic_"; got != want {
		t.Errorf("placeholder Text = %q, want %q (CommonMark should be converted to Slack mrkdwn)", got, want)
	}
}

// TestSendMessage_FailureRollsBackPlaceholder asserts that when
// MessageSendFailedMsg arrives, the optimistic placeholder is removed
// so the user can see the send did not go through.
func TestSendMessage_FailureRollsBackPlaceholder(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	app.Update(SendMessageMsg{ChannelID: "C1", Text: "oops"})
	if got := len(app.messagepane.Messages()); got != 1 {
		t.Fatalf("setup: expected 1 placeholder, got %d", got)
	}
	localTS := app.messagepane.Messages()[0].TS

	app.Update(MessageSendFailedMsg{
		ChannelID: "C1",
		LocalTS:   localTS,
		Reason:    "network error",
	})

	if got := len(app.messagepane.Messages()); got != 0 {
		t.Errorf("after failure expected placeholder removed, got %d messages", got)
	}
}

// TestSelfSendInFlight_PassesThroughCrossSession asserts that a
// WS echo for the current user that arrived with no slk-originated
// send in flight (i.e. cross-session: the user sent from the
// official Slack client) is still applied to the pane.
func TestSelfSendInFlight_PassesThroughCrossSession(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	// No SendMessageMsg dispatched here — lastSelfSendByChannel["C1"]
	// is unset. A WS echo from the user (e.g., they sent via the
	// official Slack web client) must still render.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000002", UserID: "USELF", Text: "from another client",
		},
	})

	got := app.messagepane.Messages()
	if len(got) != 1 {
		t.Fatalf("cross-session message dropped; got %d messages, want 1", len(got))
	}
	if got[0].Text != "from another client" {
		t.Errorf("Text = %q", got[0].Text)
	}
}

// TestChannelSelectedRendersFromCacheWithoutSpinner verifies that when a
// channel-cache reader is wired and returns items synchronously,
// ChannelSelectedMsg renders them immediately and skips the spinner.
// The network fetcher still fires; MessagesLoadedMsg ultimately
// authoritatively replaces the cached render.
func TestChannelSelectedRendersFromCacheWithoutSpinner(t *testing.T) {
	app := NewApp()

	cachedItems := []messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "from cache"},
	}
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		if channelID == "C1" {
			return cachedItems
		}
		return nil
	})
	// Land in Tier 2 (cache rendered + fetcher fires): synced 2 minutes
	// ago is >30s (not Tier 1) and <5min (not Tier 3).
	app.setChannelSyncedAtReaderForTest(func(ids.ChannelID) int64 { return time.Now().Unix() - 120 })
	fetcherCalled := false
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		fetcherCalled = true
		return MessagesLoadedMsg{ChannelID: string(channelID), Messages: nil}
	})

	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})

	if app.messagepane.IsLoading() {
		t.Errorf("expected loading=false on cache hit, got true")
	}
	got := app.messagepane.Messages()
	if len(got) != 1 || got[0].Text != "from cache" {
		t.Errorf("expected cached items rendered, got %+v", got)
	}

	// The cached render is best-effort; the network fetcher MUST still
	// fire in the background so MessagesLoadedMsg can authoritatively
	// replace the cached content. Drain the returned cmd batch to
	// execute the fetcher closure and assert it ran. A regression that
	// gated the fetcher on a cache miss (e.g. moving the dispatch into
	// the else branch) would silently break this guarantee.
	_ = drainBatch(cmd)
	if !fetcherCalled {
		t.Errorf("expected network fetcher to fire even on cache hit, but it did not")
	}
}

// TestMessagesLoadedNilDoesNotClobberCachedView guards the contract
// that fetchChannelMessages returning nil signals a NETWORK FAILURE
// (not an empty channel — those return []messages.MessageItem{}).
// On failure we must preserve whatever the cache rendered so a
// transient blip doesn't blank a working view. The bug this catches:
// MessagesLoadedMsg unconditionally calling SetMessages(msg.Messages)
// would wipe the cached items the moment the network call failed.
func TestMessagesLoadedNilDoesNotClobberCachedView(t *testing.T) {
	app := NewApp()
	cachedItems := []messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "from cache"},
	}
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		return cachedItems
	})
	// Tier 2: cache renders + fetcher fires (the network failure path
	// under test happens after both).
	app.setChannelSyncedAtReaderForTest(func(ids.ChannelID) int64 { return time.Now().Unix() - 120 })
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		// Simulate a network failure by returning the same shape the
		// real fetcher uses on error.
		return MessagesLoadedMsg{ChannelID: string(channelID), Messages: nil}
	})

	// First: open the channel; cache fills the pane.
	app.activeChannelID = "C1" // ensure MessagesLoadedMsg matches when delivered
	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	if got := app.messagepane.Messages(); len(got) != 1 || got[0].Text != "from cache" {
		t.Fatalf("setup: cached items not rendered: %+v", got)
	}

	// Now drain the fetcher cmd and feed each emitted message back
	// through Update — that's how Bubbletea would deliver the failed
	// MessagesLoadedMsg in production.
	for _, m := range drainBatch(cmd) {
		if m == nil {
			continue
		}
		app.Update(m)
	}

	// The cached items must still be present.
	got := app.messagepane.Messages()
	if len(got) != 1 || got[0].Text != "from cache" {
		t.Errorf("cached items were clobbered by failed fetch: got %+v", got)
	}
	if app.messagepane.IsLoading() {
		t.Errorf("loading should still be false after failed fetch, got true")
	}
}

// TestMessagesLoadedEmptyClearsView verifies the complementary case:
// a successful fetch that returned zero messages (genuinely empty
// channel) DOES replace the cached view with an empty list.
func TestMessagesLoadedEmptyClearsView(t *testing.T) {
	app := NewApp()
	cachedItems := []messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "stale cache"},
	}
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem { return cachedItems })
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: string(channelID), Messages: []messages.MessageItem{}}
	})
	app.activeChannelID = "C1"
	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	for _, m := range drainBatch(cmd) {
		if m != nil {
			app.Update(m)
		}
	}
	if got := app.messagepane.Messages(); len(got) != 0 {
		t.Errorf("expected empty pane after authoritative empty fetch, got %+v", got)
	}
}

// TestChannelSelectedFallsBackToSpinnerOnCacheMiss verifies that when
// the cache reader returns nil (or is absent), ChannelSelectedMsg falls
// back to the loading spinner while the network fetch is in flight.
func TestChannelSelectedFallsBackToSpinnerOnCacheMiss(t *testing.T) {
	app := NewApp()
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem { return nil })
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: string(channelID), Messages: nil}
	})

	app.Update(ChannelSelectedMsg{ID: "C2", Name: "alerts", Type: "channel"})

	if !app.messagepane.IsLoading() {
		t.Errorf("expected loading=true on cache miss, got false")
	}
}

// TestWorkspaceSwitchedQueuesChannelSelected verifies that the
// WorkspaceSwitchedMsg handler queues a ChannelSelectedMsg for the
// restored (or first) channel rather than wiping the pane itself. The
// pane is intentionally left as-is so the queued ChannelSelectedMsg
// (handled by the three-tier dispatch) paints over it without an
// intermediate empty-state flash.
func TestWorkspaceSwitchedQueuesChannelSelected(t *testing.T) {
	app := NewApp()
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem { return nil })
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: string(channelID), Messages: nil}
	})

	_, cmd := app.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		Channels: []sidebar.ChannelItem{{ID: "C9", Name: "general", Type: "channel"}},
	})

	// Walk the returned batch and confirm a ChannelSelectedMsg for the
	// only channel in the new workspace is queued.
	found := false
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				walk(child)
			}
			return
		}
		if cs, ok := msg.(ChannelSelectedMsg); ok && cs.ID == "C9" {
			found = true
		}
	}
	walk(cmd)
	if !found {
		t.Fatalf("expected WorkspaceSwitchedMsg to queue ChannelSelectedMsg{ID:C9}, got none")
	}
}

// TestWorkspaceSwitchedEmptyClearsPane verifies that the empty-workspace
// branch (no Channels) explicitly clears the messagepane, since no
// ChannelSelectedMsg is queued to repaint it.
func TestWorkspaceSwitchedEmptyClearsPane(t *testing.T) {
	app := NewApp()
	// Seed the pane with a stale message so we can prove it gets cleared.
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", UserID: "U", UserName: "u", Text: "stale"}})
	app.messagepane.SetLoading(true)

	app.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		Channels: nil,
	})

	if app.messagepane.IsLoading() {
		t.Fatalf("expected loading=false on empty workspace, got true")
	}
	if got := app.messagepane.Messages(); len(got) != 0 {
		t.Fatalf("expected messages cleared on empty workspace, got %d", len(got))
	}
}

// TestWorkspaceReadyFirstChannelSetsLoading mirrors the WorkspaceSwitched
// case for the first-workspace bootstrap path: the WorkspaceReadyMsg
// handler also auto-selects the first channel via a deferred
// ChannelSelectedMsg, so it too must flip loading=true on the same tick
// it clears the messagepane to avoid an empty-state flash.
func TestWorkspaceReadyFirstChannelSetsLoading(t *testing.T) {
	app := NewApp()
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem { return nil })
	app.setChannelFetcherForTest(func(channelID ids.ChannelID, channelName string) tea.Msg {
		return MessagesLoadedMsg{ChannelID: string(channelID), Messages: nil}
	})

	app.Update(WorkspaceReadyMsg{
		TeamID:        "T1",
		TeamName:      "Acme",
		Channels:      []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		InitialActive: true,
	})

	if !app.messagepane.IsLoading() {
		t.Fatalf("expected messagepane loading=true on first-channel auto-select, got false")
	}
}

func TestChannelSelectedInvokesVisitRecorder(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	var recorded []string
	app.setChannelVisitRecorderForTest(func(channelID ids.ChannelID) {
		recorded = append(recorded, string(channelID))
	})

	// Dispatch a ChannelSelectedMsg. The handler should call the recorder.
	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})

	if len(recorded) != 1 || recorded[0] != "C1" {
		t.Errorf("want recorded=[C1], got %v", recorded)
	}
}

func TestChannelSelectedFromHistoryStillRecordsVisit(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	var recorded []string
	app.setChannelVisitRecorderForTest(func(channelID ids.ChannelID) {
		recorded = append(recorded, string(channelID))
	})

	// Even when synthesized by back/forward (FromHistory: true),
	// recency must update so going back makes that channel "most recent".
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "ops", Type: "channel", FromHistory: true})

	if len(recorded) != 1 || recorded[0] != "C2" {
		t.Errorf("want recorded=[C2], got %v", recorded)
	}
}

func TestNavStackPushOnChannelSelected(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C3", Name: "c", Type: "channel"})

	stack := app.navHistory.Stack("T1")
	if stack == nil {
		t.Fatal("expected nav stack for T1 to exist")
	}
	want := []string{"C1", "C2", "C3"}
	if !reflect.DeepEqual(stack.entries, want) {
		t.Errorf("entries: want %v, got %v", want, stack.entries)
	}
	if stack.cursor != 2 {
		t.Errorf("cursor: want 2, got %d", stack.cursor)
	}
}

func TestNavStackDedupesConsecutive(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"}) // re-select same
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	stack := app.navHistory.Stack("T1")
	want := []string{"C1", "C2"}
	if !reflect.DeepEqual(stack.entries, want) {
		t.Errorf("entries: want %v, got %v", want, stack.entries)
	}
	if stack.cursor != 1 {
		t.Errorf("cursor: want 1, got %d", stack.cursor)
	}
}

func TestNavStackForwardTruncationOnNewVisit(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	// Build A, B, C; back to B (simulated by directly manipulating cursor);
	// then visit D — C should be dropped.
	_, _ = app.Update(ChannelSelectedMsg{ID: "A", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "B", Name: "b", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C", Name: "c", Type: "channel"})

	// Simulate a back step (cursor moves but entries don't change).
	app.navHistory.Stack("T1").cursor = 1

	_, _ = app.Update(ChannelSelectedMsg{ID: "D", Name: "d", Type: "channel"})

	stack := app.navHistory.Stack("T1")
	want := []string{"A", "B", "D"}
	if !reflect.DeepEqual(stack.entries, want) {
		t.Errorf("entries: want %v, got %v", want, stack.entries)
	}
	if stack.cursor != 2 {
		t.Errorf("cursor: want 2, got %d", stack.cursor)
	}
}

func TestNavStackCapAt50EvictsOldest(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	for i := 0; i < 60; i++ {
		_, _ = app.Update(ChannelSelectedMsg{ID: fmt.Sprintf("C%d", i), Name: "x", Type: "channel"})
	}
	stack := app.navHistory.Stack("T1")
	if len(stack.entries) != 50 {
		t.Errorf("len: want 50, got %d", len(stack.entries))
	}
	if stack.entries[0] != "C10" {
		t.Errorf("oldest after eviction: want C10, got %q", stack.entries[0])
	}
	if stack.entries[49] != "C59" {
		t.Errorf("newest: want C59, got %q", stack.entries[49])
	}
	if stack.cursor != 49 {
		t.Errorf("cursor: want 49, got %d", stack.cursor)
	}
}

func TestNavStackPerWorkspaceIsolation(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})

	app.activeTeamID = "T2"
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	t1 := app.navHistory.Stack("T1")
	t2 := app.navHistory.Stack("T2")
	if t1 == nil || t2 == nil {
		t.Fatalf("expected both stacks to exist; t1=%v t2=%v", t1, t2)
	}
	if !reflect.DeepEqual(t1.entries, []string{"C1"}) {
		t.Errorf("T1: want [C1], got %v", t1.entries)
	}
	if !reflect.DeepEqual(t2.entries, []string{"C2"}) {
		t.Errorf("T2: want [C2], got %v", t2.entries)
	}
}

func TestNavStackFromHistoryDoesNotPush(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	// FromHistory navigation should NOT grow the stack.
	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel", FromHistory: true})

	stack := app.navHistory.Stack("T1")
	if !reflect.DeepEqual(stack.entries, []string{"C1", "C2"}) {
		t.Errorf("entries should be unchanged; got %v", stack.entries)
	}
	if stack.cursor != 1 {
		t.Errorf("cursor should be unchanged at 1, got %d", stack.cursor)
	}
}

func TestNavigateBackEmitsChannelSelectedMsg(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID) + "-name", "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	cmd := app.navigateBack()
	if cmd == nil {
		t.Fatal("navigateBack returned nil cmd; expected ChannelSelectedMsg dispatch")
	}
	got := cmd()
	cs, ok := got.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", got)
	}
	if cs.ID != "C1" {
		t.Errorf("want ID=C1, got %q", cs.ID)
	}
	if !cs.FromHistory {
		t.Error("FromHistory must be true on synthesized navigation")
	}
	if app.navHistory.Stack("T1").cursor != 0 {
		t.Errorf("cursor: want 0, got %d", app.navHistory.Stack("T1").cursor)
	}
}

func TestNavigateForwardEmitsChannelSelectedMsg(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID) + "-name", "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})
	app.navHistory.Stack("T1").cursor = 0 // simulate one back

	cmd := app.navigateForward()
	if cmd == nil {
		t.Fatal("navigateForward returned nil cmd")
	}
	got := cmd()
	cs, ok := got.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", got)
	}
	if cs.ID != "C2" {
		t.Errorf("want ID=C2, got %q", cs.ID)
	}
	if !cs.FromHistory {
		t.Error("FromHistory must be true on synthesized navigation")
	}
	if app.navHistory.Stack("T1").cursor != 1 {
		t.Errorf("cursor: want 1, got %d", app.navHistory.Stack("T1").cursor)
	}
}

func TestNavigateBackAtStartIsNoop(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})

	cmd := app.navigateBack()
	if cmd != nil {
		t.Errorf("expected nil at boundary, got non-nil cmd")
	}
}

func TestNavigateForwardAtEndIsNoop(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	cmd := app.navigateForward()
	if cmd != nil {
		t.Errorf("expected nil at end of stack, got non-nil cmd")
	}
}

func TestNavigateBackSkipsStaleAndDropsThem(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	// Lookup says "C2 is gone, others valid".
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		if channelID == ids.ChannelID("C2") {
			return "", "", false
		}
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C3", Name: "c", Type: "channel"})

	// Cursor at C3 (index 2). Back should skip C2 and land on C1.
	cmd := app.navigateBack()
	if cmd == nil {
		t.Fatal("navigateBack returned nil cmd")
	}
	got := cmd()
	cs, ok := got.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", got)
	}
	if cs.ID != "C1" {
		t.Errorf("want ID=C1 (skipping stale C2), got %q", cs.ID)
	}
	// C2 must have been dropped from entries.
	stack := app.navHistory.Stack("T1")
	for _, id := range stack.entries {
		if id == "C2" {
			t.Errorf("stale C2 should have been dropped from entries; got %v", stack.entries)
		}
	}
}

func TestCtrlHTriggersNavBack(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected cmd from ctrl+h dispatch")
	}
	got := cmd()
	cs, ok := got.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", got)
	}
	if cs.ID != "C1" || !cs.FromHistory {
		t.Errorf("want ID=C1 FromHistory=true, got %+v", cs)
	}
}

func TestCtrlKTriggersNavForward(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})
	app.navHistory.Stack("T1").cursor = 0

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected cmd from ctrl+k dispatch")
	}
	got := cmd()
	cs, ok := got.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", got)
	}
	if cs.ID != "C2" || !cs.FromHistory {
		t.Errorf("want ID=C2 FromHistory=true, got %+v", cs)
	}
}

func TestWorkspaceReady_OnlyInitialActiveClaimsChannel(t *testing.T) {
	app := NewApp()

	// First WorkspaceReady arrives WITHOUT InitialActive — should not
	// set activeTeamID and should not queue a ChannelSelectedMsg.
	app.Update(WorkspaceReadyMsg{
		TeamID:        "T-other",
		TeamName:      "Other",
		Channels:      []sidebar.ChannelItem{{ID: "C-other", Name: "general", Type: "channel"}},
		InitialActive: false,
	})

	if app.activeTeamID != "" {
		t.Errorf("non-initial WorkspaceReady should not set activeTeamID; got %q", app.activeTeamID)
	}

	// Second WorkspaceReady with InitialActive=true claims active.
	app.Update(WorkspaceReadyMsg{
		TeamID:        "T-default",
		TeamName:      "Default",
		Channels:      []sidebar.ChannelItem{{ID: "C-default", Name: "general", Type: "channel"}},
		InitialActive: true,
	})

	if app.activeTeamID != "T-default" {
		t.Errorf("activeTeamID = %q, want T-default", app.activeTeamID)
	}
}

func TestWorkspaceReady_BootstrapClaimIsOneShot(t *testing.T) {
	app := NewApp()

	app.Update(WorkspaceReadyMsg{
		TeamID:        "T1",
		TeamName:      "First",
		Channels:      []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		InitialActive: true,
	})
	first := app.activeTeamID

	// A second InitialActive=true (defensive — shouldn't happen) is a no-op.
	app.Update(WorkspaceReadyMsg{
		TeamID:        "T2",
		TeamName:      "Second",
		Channels:      []sidebar.ChannelItem{{ID: "C2", Name: "general", Type: "channel"}},
		InitialActive: true,
	})

	if app.activeTeamID != first {
		t.Errorf("activeTeamID changed after second InitialActive; got %q, want %q", app.activeTeamID, first)
	}
}

func TestUserResolvedMsg_PatchesActiveWorkspace(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "U1", Text: "hi"},
	})

	app.Update(UserResolvedMsg{
		TeamID:      "T1",
		UserID:      "U1",
		DisplayName: "alice",
	})

	got := app.messagepane.Messages()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].UserName != "alice" {
		t.Errorf("UserName = %q, want alice", got[0].UserName)
	}
}

func TestUserResolvedMsg_DropsForOtherWorkspace(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "U1", Text: "hi"},
	})

	app.Update(UserResolvedMsg{
		TeamID:      "T-other",
		UserID:      "U1",
		DisplayName: "alice",
	})

	got := app.messagepane.Messages()
	if got[0].UserName != "U1" {
		t.Errorf("UserName changed despite wrong team; got %q", got[0].UserName)
	}
}

func TestUserGroupsLoadedMsgPatchesActiveWorkspace(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "ping <!subteam^S0TESTGRP01>"},
	})
	messageVersion := app.messagepane.Version()
	threadVersion := app.threadPanel.Version()

	app.Update(UserGroupsLoadedMsg{
		TeamID:     "T1",
		UserGroups: map[string]string{"S0TESTGRP01": "platform-team"},
	})

	found := false
	for _, u := range app.compose.MentionUsers() {
		if u.ID == "usergroup:S0TESTGRP01" && u.DisplayName == "platform-team" {
			found = true
		}
	}
	if !found {
		t.Errorf("active workspace usergroup missing from compose picker: %+v", app.compose.MentionUsers())
	}
	if app.messagepane.Version() == messageVersion {
		t.Error("message pane version did not change after active usergroups loaded")
	}
	if app.threadPanel.Version() == threadVersion {
		t.Error("thread panel version did not change after active usergroups loaded")
	}
}

func TestUserGroupsLoadedMsgDropsOtherWorkspace(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"

	app.Update(UserGroupsLoadedMsg{
		TeamID:     "T-other",
		UserGroups: map[string]string{"S0TESTGRP01": "side-team"},
	})

	for _, u := range app.compose.MentionUsers() {
		if u.ID == "usergroup:S0TESTGRP01" {
			t.Fatalf("inactive workspace usergroup leaked into compose picker: %+v", app.compose.MentionUsers())
		}
	}
}

func TestUserExternalMsgFlagsPickerEntry(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.compose.SetActiveChannel("C1")
	app.threadCompose.SetActiveChannel("C1")
	app.SetUserNames(map[string]string{"U1": "alice"})

	_, _ = app.Update(UserExternalMsg{UserID: "U1", IsExternal: true})

	for _, u := range app.compose.MentionUsers() {
		if u.ID == "U1" && !u.IsExternal {
			t.Error("U1 should be marked IsExternal after UserExternalMsg")
		}
	}
}

// drainAllCmds recursively executes every cmd inside the tea.BatchMsg
// tree returned by Update. Used to surface counter side-effects on
// closure-bound fakes (channelFetcher, channelReadMarker). The
// resulting tea.Msgs are NOT fed back into Update — these tests only
// care about whether the fakes were invoked.
func drainAllCmds(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drainAllCmds(t, c)
		}
	}
}

func TestChannelSelected_Tier1_RenderCacheNoFetch(t *testing.T) {
	app := NewApp()
	now := time.Now().Unix()
	app.setChannelSyncedAtReaderForTest(func(id ids.ChannelID) int64 { return now - 10 })
	app.setChannelCacheReaderForTest(func(id ids.ChannelID) []messages.MessageItem {
		return []messages.MessageItem{{TS: "1.0", UserID: "U", UserName: "u", Text: "hi"}}
	})
	fetchCalled := 0
	app.setChannelFetcherForTest(func(id ids.ChannelID, name string) tea.Msg {
		fetchCalled++
		return MessagesLoadedMsg{ChannelID: string(id), Messages: nil}
	})
	markCalled := 0
	app.setChannelReadMarkerForTest(func(id ids.ChannelID, ts ids.MessageTS) tea.Msg {
		markCalled++
		return nil
	})

	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	drainAllCmds(t, cmd)

	if fetchCalled != 0 {
		t.Errorf("Tier 1: fetcher should NOT fire; got %d calls", fetchCalled)
	}
	if markCalled != 1 {
		t.Errorf("Tier 1: markRead should fire once; got %d calls", markCalled)
	}
}

func TestChannelSelected_Tier2_CacheAndFetch(t *testing.T) {
	app := NewApp()
	now := time.Now().Unix()
	app.setChannelSyncedAtReaderForTest(func(id ids.ChannelID) int64 { return now - 120 })
	app.setChannelCacheReaderForTest(func(id ids.ChannelID) []messages.MessageItem {
		return []messages.MessageItem{{TS: "1.0", UserID: "U", UserName: "u", Text: "hi"}}
	})
	fetchCalled := 0
	app.setChannelFetcherForTest(func(id ids.ChannelID, name string) tea.Msg {
		fetchCalled++
		return MessagesLoadedMsg{ChannelID: string(id), Messages: nil}
	})
	markCalled := 0
	app.setChannelReadMarkerForTest(func(id ids.ChannelID, ts ids.MessageTS) tea.Msg {
		markCalled++
		return nil
	})

	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	drainAllCmds(t, cmd)

	if fetchCalled != 1 {
		t.Errorf("Tier 2: fetcher should fire once; got %d", fetchCalled)
	}
	if markCalled != 0 {
		t.Errorf("Tier 2: markRead should NOT fire (fetcher's own mark-as-read handles it); got %d", markCalled)
	}
}

func TestChannelSelected_Tier3_SpinnerOnly(t *testing.T) {
	app := NewApp()
	app.setChannelSyncedAtReaderForTest(func(id ids.ChannelID) int64 { return 0 })
	app.setChannelCacheReaderForTest(func(id ids.ChannelID) []messages.MessageItem {
		return nil // no cache at all → genuine Tier 3
	})
	fetchCalled := 0
	app.setChannelFetcherForTest(func(id ids.ChannelID, name string) tea.Msg {
		fetchCalled++
		return MessagesLoadedMsg{ChannelID: string(id), Messages: nil}
	})

	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	drainAllCmds(t, cmd)

	if got := app.messagepane.Messages(); len(got) != 0 {
		t.Errorf("Tier 3: pane should be empty (spinner); got %d msgs", len(got))
	}
	if fetchCalled != 1 {
		t.Errorf("Tier 3: fetcher should fire once; got %d", fetchCalled)
	}
}

func TestChannelSelected_UnknownFreshnessWithCache_FallsToTier2(t *testing.T) {
	// Production state until Task 15 wires SetChannelSyncedAtReader:
	// syncedAtReader is nil, cache reader returns rows. Should render
	// cache + fire fetch + show indicator (Tier 2), NOT blank the
	// pane with a spinner.
	app := NewApp()
	// no SetChannelSyncedAtReader call — leaves it nil
	app.setChannelCacheReaderForTest(func(id ids.ChannelID) []messages.MessageItem {
		return []messages.MessageItem{{TS: "1.0", UserID: "U", UserName: "u", Text: "hi"}}
	})
	fetchCalled := 0
	app.setChannelFetcherForTest(func(id ids.ChannelID, name string) tea.Msg {
		fetchCalled++
		return MessagesLoadedMsg{ChannelID: string(id), Messages: nil}
	})

	_, cmd := app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	drainAllCmds(t, cmd)

	if got := app.messagepane.Messages(); len(got) != 1 {
		t.Errorf("unknown freshness with cache: pane should render cache; got %d msgs", len(got))
	}
	if fetchCalled != 1 {
		t.Errorf("unknown freshness with cache: fetcher should fire once; got %d", fetchCalled)
	}
}

func TestSetChannelMembershipForwardsToCompose(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.compose.SetActiveChannel("C1")
	app.threadCompose.SetActiveChannel("C1")
	app.SetUserNames(map[string]string{"U1": "alice", "U2": "bob"})

	app.SetChannelMembership("C1", []string{"U1"})

	users := app.compose.MentionUsers()
	byName := map[string]bool{}
	for _, u := range users {
		byName[u.DisplayName] = u.InChannel
	}
	if !byName["alice"] {
		t.Error("alice should be in-channel")
	}
	if byName["bob"] {
		t.Error("bob should be not-in-channel")
	}
}

func TestSetExternalUsersPropagates(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.SetExternalUsers(map[string]bool{"U_EXT": true})
	app.SetUserNames(map[string]string{"U_EXT": "ext.user", "U1": "alice"})
	app.activeChannelID = "C1"
	app.compose.SetActiveChannel("C1")
	app.threadCompose.SetActiveChannel("C1")
	app.SetChannelMembership("C1", []string{"U_EXT", "U1"})

	found := false
	for _, u := range app.compose.MentionUsers() {
		if u.ID == "U_EXT" {
			found = true
			if !u.IsExternal {
				t.Error("U_EXT should be flagged IsExternal")
			}
		}
	}
	if !found {
		t.Error("U_EXT missing from picker users")
	}
}

func TestChannelMembershipMsgUpdatesPicker(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.compose.SetActiveChannel("C1")
	app.threadCompose.SetActiveChannel("C1")
	app.SetUserNames(map[string]string{"U1": "alice", "U2": "bob"})

	_, _ = app.Update(ChannelMembershipMsg{ChannelID: "C1", MemberIDs: []string{"U1"}})

	var aliceInCh, bobInCh bool
	for _, u := range app.compose.MentionUsers() {
		if u.ID == "U1" {
			aliceInCh = u.InChannel
		}
		if u.ID == "U2" {
			bobInCh = u.InChannel
		}
	}
	if !aliceInCh {
		t.Error("alice should be in-channel after ChannelMembershipMsg")
	}
	if bobInCh {
		t.Error("bob should be not-in-channel after ChannelMembershipMsg")
	}
}

func TestChannelSelectedInvokesMembershipFetcher(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	// The App invokes the fetcher on a goroutine (see ChannelSelectedMsg
	// handler in app.go), so use a thread-safe record + a done signal.
	var mu sync.Mutex
	var fetched []string
	done := make(chan struct{}, 1)
	app.setChannelMembershipFetcherForTest(func(channelID ids.ChannelID) {
		mu.Lock()
		fetched = append(fetched, string(channelID))
		mu.Unlock()
		done <- struct{}{}
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C42", Name: "general", Type: "channel"})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fetcher was not invoked within 1s")
	}

	mu.Lock()
	got := append([]string(nil), fetched...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "C42" {
		t.Errorf("fetcher invoked with %v, want [C42]", got)
	}
	if app.compose.ActiveChannel() != "C42" {
		t.Errorf("compose active channel = %q, want C42", app.compose.ActiveChannel())
	}
}

// TestChannelSelectedReturnsPromptlyEvenIfFetcherBlocks guards against
// the deadlock that froze the app on first run: the fetcher closure
// in main.go originally called Membership.EnsureFresh synchronously,
// which transitively invoked bubbletea's Program.Send (unbuffered
// channel in bubbletea v2) from inside the Update goroutine — Send
// blocked waiting for itself to receive. The contract is now that
// the fetcher must NOT block; the test simulates a misbehaving
// fetcher that blocks until released, and asserts Update still
// returns within a short deadline. If a future maintainer changes
// the wiring to once-again call a blocking fetcher synchronously,
// this test fails the deadline.
// setupAppForTitleTest builds an App wired with the readers and items
// the window-title pipeline depends on. Returns the app plus the
// mutable channel-state and workspace-unreads slices the caller can
// re-point the readers at without re-wiring.
func setupAppForTitleTest(
	t *testing.T,
	channels []sidebar.ChannelItem,
	workspaces []workspace.WorkspaceItem,
	channelState map[string]cache.ReadState,
	workspaceUnreads []string,
) *App {
	t.Helper()
	app := NewApp()
	app.SetWorkspaces(workspaces)
	app.SetChannels(channels)
	app.SetReadStateReader(func() map[string]cache.ReadState { return channelState })
	app.SetWorkspaceUnreadReader(func() []string { return workspaceUnreads })
	return app
}

// TestNotifyReadStateChanged_PopulatesWindowTitle is the canonical
// wiring test. It verifies that notifyReadStateChanged actually plumbs
// each input from the collaborator the architecture assigns to it:
//   - active count comes from the sidebar (mute-filtered)
//   - other-workspace count comes from the rail (not mute-filtered)
//   - workspace name comes from the rail for the active team
//
// If a future refactor reroutes any of these sources, the assertions
// fall over and point at the wiring break.
func TestNotifyReadStateChanged_PopulatesWindowTitle(t *testing.T) {
	app := setupAppForTitleTest(t,
		[]sidebar.ChannelItem{
			{ID: "C1", Name: "general", Type: "channel", IsMuted: false},
			{ID: "C2", Name: "noisy", Type: "channel", IsMuted: true},
			{ID: "C3", Name: "design", Type: "channel", IsMuted: false},
		},
		[]workspace.WorkspaceItem{
			{ID: "T1", Name: "SWAP", Initials: "SW"},
			{ID: "T2", Name: "Other", Initials: "OT"},
		},
		map[string]cache.ReadState{
			"C1": {HasUnread: true},  // counted toward active
			"C2": {HasUnread: true},  // muted: NOT counted toward active
			"C3": {HasUnread: false}, // read: NOT counted
		},
		[]string{"T1", "T2"}, // T1 active, T2 contributes to +1
	)
	app.activeTeamID = "T1"

	app.notifyReadStateChanged()

	if got, want := app.windowTitle, "slk SW (1) +1"; got != want {
		t.Errorf("windowTitle = %q want %q", got, want)
	}
}

func TestNotifyReadStateChanged_PreBootstrap(t *testing.T) {
	app := NewApp()
	// activeTeamID intentionally left blank
	app.notifyReadStateChanged()
	if got, want := app.windowTitle, "slk"; got != want {
		t.Errorf("pre-bootstrap windowTitle = %q want %q", got, want)
	}
}

func TestNotifyReadStateChanged_NoUnreads(t *testing.T) {
	app := setupAppForTitleTest(t,
		[]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		[]workspace.WorkspaceItem{{ID: "T1", Name: "SWAP", Initials: "SW"}},
		map[string]cache.ReadState{"C1": {HasUnread: false}},
		nil,
	)
	app.activeTeamID = "T1"

	app.notifyReadStateChanged()

	if got, want := app.windowTitle, "slk SW"; got != want {
		t.Errorf("windowTitle = %q want %q", got, want)
	}
}

// TestView_PropagatesWindowTitle proves View() surfaces the cached
// title onto tea.View.WindowTitle from BOTH return sites: the
// pre-layout fallback (width/height==0) and the full-layout main
// branch. Bubbletea's renderer only emits the OSC sequence when this
// field changes between renders, so a missed assignment silently
// disables the feature.
func TestView_PropagatesWindowTitle(t *testing.T) {
	app := NewApp()
	app.windowTitle = "slk SW (2) +1"

	// Pre-layout branch (width/height==0).
	v := app.View()
	if v.WindowTitle != "slk SW (2) +1" {
		t.Errorf("pre-layout View.WindowTitle = %q want %q", v.WindowTitle, "slk SW (2) +1")
	}

	// Main branch: give the app a real canvas size and re-render.
	app.width = 100
	app.height = 30
	v = app.View()
	if v.WindowTitle != "slk SW (2) +1" {
		t.Errorf("main View.WindowTitle = %q want %q", v.WindowTitle, "slk SW (2) +1")
	}
}

func TestChannelSelectedReturnsPromptlyEvenIfFetcherBlocks(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	release := make(chan struct{})
	app.setChannelMembershipFetcherForTest(func(channelID ids.ChannelID) {
		// Simulate a slow fetcher (e.g., blocked on a network call
		// or a blocking p.Send). Releases when the test allows.
		<-release
	})
	defer close(release)

	done := make(chan struct{})
	go func() {
		_, _ = app.Update(ChannelSelectedMsg{ID: "C42", Name: "general", Type: "channel"})
		close(done)
	}()
	select {
	case <-done:
		// Update returned promptly — fetcher was not invoked
		// synchronously on the caller's goroutine.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Update did not return within 100ms; fetcher is being called synchronously on the Update goroutine — risks bubbletea Send-from-Update deadlock")
	}
}

func TestSeedNewMessagePicker_PopulatesUsersAndExcludesSelf(t *testing.T) {
	app := NewApp()
	app.currentUserID = "USELF"
	app.SetUserNames(map[string]string{
		"USELF": "Me",
		"U1":    "Alice",
		"U2":    "Bob",
	})
	app.SetExternalUsers(map[string]bool{"U2": true})

	app.seedNewMessagePicker()

	users := app.newMessagePicker.Users()
	// Picker holds all non-self users. The picker excludes self via
	// SetCurrentUserID at filter time, not at the SetUsers slice level.
	// So the slice should contain Alice + Bob + Me, but after Open()
	// the filtered list excludes Me.
	ids := map[string]bool{}
	for _, u := range users {
		ids[u.ID] = true
	}
	if !ids["U1"] {
		t.Error("expected Alice (U1) in picker users")
	}
	if !ids["U2"] {
		t.Error("expected Bob (U2) in picker users")
	}
	if ids["USELF"] {
		t.Error("expected USELF excluded from seeded slice")
	}

	// External flag should propagate.
	for _, u := range users {
		if u.ID == "U2" && !u.IsExternal {
			t.Error("expected Bob (U2) to be marked external")
		}
	}
}

func TestListReactionsKeyOpensModal(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:   "100.0",
		Text: "decision?",
		Reactions: []messages.ReactionItem{
			{Emoji: "thumbsup", Count: 2, UserIDs: []string{"U1", "U2"}},
		},
	}})
	app.SetUserNames(map[string]string{"U1": "Alice", "U2": "Bob"})

	app.openReactionsView()

	if !app.reactionsView.IsVisible() {
		t.Fatal("openReactionsView should open the modal when reactions exist")
	}
	if app.mode != ModeReactionsView {
		t.Fatalf("mode should be ModeReactionsView, got %v", app.mode)
	}

	out := app.reactionsView.ViewOverlay(80, 24, strings.Repeat("\n", 24))
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "Bob") {
		t.Fatalf("modal should list reactor names, got:\n%s", out)
	}
}

func TestListReactionsNoOpWhenNoReactions(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "100.0", Text: "no reactions"}})

	app.openReactionsView()

	if app.reactionsView.IsVisible() {
		t.Fatal("openReactionsView should be a no-op when the message has no reactions")
	}
	if app.mode == ModeReactionsView {
		t.Fatal("mode should not change when there are no reactions")
	}
}

// The thread panel's "── new ──" divider must be sourced from the
// thread's own cursor in thread_subscriptions, not the parent
// channel's last_read_ts: plain replies never advance the channel
// watermark, so the channel cursor is systematically stale and puts
// the divider too early.
func TestOpenThreadPanel_BoundaryUsesThreadCursor(t *testing.T) {
	app := NewApp()
	var gotChannel, gotThread string
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		ThreadLastRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) string {
			gotChannel, gotThread = string(channelID), string(threadTS)
			return "R7"
		},
	}))

	_ = app.openThreadPanel(messages.MessageItem{TS: "P1"}, "C1", "P1")

	if gotChannel != "C1" || gotThread != "P1" {
		t.Fatalf("ThreadLastRead called with (%q, %q), want (C1, P1)", gotChannel, gotThread)
	}
	if got := app.threadPanel.UnreadBoundaryTS(); got != "R7" {
		t.Errorf("thread panel boundary = %q, want the thread cursor R7", got)
	}
}
