// internal/ui/app_test.go
package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
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
	app.addTypingUser("C1", "U1")

	users := app.getTypingUsers("C1")
	if len(users) != 1 || users[0] != "U1" {
		t.Errorf("expected [U1], got %v", users)
	}

	// Add another user
	app.addTypingUser("C1", "U2")
	users = app.getTypingUsers("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	// Expire all
	app.expireTypingUsers()
	// They shouldn't be expired yet (TTL is 5 seconds)
	users = app.getTypingUsers("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users still active, got %d", len(users))
	}
}

func TestTypingStateFiltersSelf(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.currentUserID = "U1"

	app.addTypingUser("C1", "U1")
	app.addTypingUser("C1", "U2")

	users := app.getTypingUsersFiltered("C1")
	if len(users) != 1 || users[0] != "U2" {
		t.Errorf("expected [U2] (self filtered), got %v", users)
	}
}

func TestTypingIndicatorText(t *testing.T) {
	app := NewApp()

	text := app.typingIndicatorText(nil)
	if text != "" {
		t.Errorf("expected empty for nil, got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice"})
	if text != "Alice is typing..." {
		t.Errorf("expected 'Alice is typing...', got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice", "Bob"})
	if text != "Alice and Bob are typing..." {
		t.Errorf("expected 'Alice and Bob are typing...', got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice", "Bob", "Charlie"})
	if text != "Several people are typing..." {
		t.Errorf("expected 'Several people are typing...', got %q", text)
	}
}

func TestRenderTypingIndicator(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.typingEnabled = true
	app.currentUserID = "U_SELF"

	// Set up user names
	app.messagepane.SetUserNames(map[string]string{"U1": "Alice", "U2": "Bob"})

	// No one typing — should return empty
	line := app.renderTypingLine()
	if line != "" {
		t.Errorf("expected empty, got %q", line)
	}

	// One person typing
	app.addTypingUser("C1", "U1")
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
	app.typingEnabled = true
	app.activeChannelID = "C1"

	app.addTypingUser("C1", "U1")
	app.addTypingUser("C2", "U2")

	// Typing indicator should show for C1
	users := app.getTypingUsersFiltered("C1")
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C1, got %d", len(users))
	}

	// After "switching" to C2, reset throttle
	app.activeChannelID = "C2"
	app.lastTypingSent = time.Time{} // reset throttle on switch

	// C2 should show its typers
	users = app.getTypingUsersFiltered("C2")
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C2, got %d", len(users))
	}
}

func TestTypingThrottle(t *testing.T) {
	app := NewApp()
	app.typingEnabled = true
	app.activeChannelID = "C1"

	// First call should allow sending
	if !app.shouldSendTyping() {
		t.Error("expected first typing send to be allowed")
	}

	// Mark as just sent
	app.lastTypingSent = time.Now()

	// Immediate second call should be throttled
	if app.shouldSendTyping() {
		t.Error("expected typing send to be throttled")
	}

	// After 3 seconds, should allow again
	app.lastTypingSent = time.Now().Add(-4 * time.Second)
	if !app.shouldSendTyping() {
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

func TestCopyPermalink_FromMessagesPane(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000200", UserName: "alice", Text: "hi"},
	})

	var gotCh, gotTS string
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		gotCh = channelID
		gotTS = ts
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
	if gotCh != "C123" {
		t.Errorf("channel = %q, want C123", gotCh)
	}
	if gotTS != "1700000001.000200" {
		t.Errorf("ts = %q, want 1700000001.000200", gotTS)
	}
}

func TestCopyPermalink_FromThreadPane(t *testing.T) {
	app := NewApp()
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
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		gotCh = channelID
		gotTS = ts
		return "https://example.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999", nil
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from C key")
	}
	if !drainForPermalinkCopied(t, cmd()) {
		t.Fatal("expected PermalinkCopiedMsg")
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
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
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
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
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
