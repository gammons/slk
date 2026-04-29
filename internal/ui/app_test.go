// internal/ui/app_test.go
package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
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

func TestCopyPermalink_ShiftYTriggersCopy(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000200", UserName: "alice", Text: "hi"},
	})

	called := 0
	var gotCh, gotTS string
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		called++
		gotCh = channelID
		gotTS = ts
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

func TestApp_OpenSelectedThreadDedups(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	fetched := 0
	app.SetThreadFetcher(func(channelID, threadTS string) tea.Msg {
		fetched++
		return ThreadRepliesLoadedMsg{ThreadTS: threadTS, Replies: nil}
	})
	summaries := []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0"},
		{ChannelID: "C1", ThreadTS: "2.0"},
	}
	app.Update(ThreadsListLoadedMsg{TeamID: "T1", Summaries: summaries})
	app.view = ViewThreads

	// First call should fetch.
	cmd := app.openSelectedThreadCmd()
	if cmd == nil {
		t.Fatal("first call returned nil")
	}
	cmd()
	if fetched != 1 {
		t.Errorf("first call fetched=%d, want 1", fetched)
	}

	// Second call without selection change should NOT fetch.
	cmd = app.openSelectedThreadCmd()
	if cmd != nil {
		t.Errorf("second call should be a no-op, got cmd=%v", cmd)
	}

	// After moving selection, fetch should fire again.
	app.threadsView.MoveDown()
	cmd = app.openSelectedThreadCmd()
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
	app.SetThreadsListFetcher(func(teamID string) tea.Msg {
		fetched <- teamID
		return ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
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
	app.SetThreadsListFetcher(func(teamID string) tea.Msg {
		fetched <- struct{}{}
		return ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
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
	app.SetThreadsListFetcher(func(teamID string) tea.Msg {
		fetched <- teamID
		return ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
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
	app.SetThreadsListFetcher(func(teamID string) tea.Msg {
		return ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
	})

	// Make T1 the active workspace by sending the first WorkspaceReadyMsg.
	app.Update(WorkspaceReadyMsg{
		TeamID:   "T1",
		TeamName: "First",
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
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
	app.SetThreadsListFetcher(func(teamID string) tea.Msg {
		fetched <- teamID
		return ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
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
	if !app.isSelfSent("1700000050.000400") {
		t.Errorf("expected TS to be recorded as self-sent for echo dedup")
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
	if !app.isSelfSent("1700000999.000001") {
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
		TeamID:   "T1",
		TeamName: "team",
		Theme:    "dracula",
	})

	afterVer := styles.Version()
	if afterVer == beforeVer {
		t.Errorf("expected styles.Version() to advance after WorkspaceReadyMsg with non-empty Theme")
	}
}

// Defends Bug A: a duplicate of the same TS (e.g. WS echo arriving before
// the optimistic-add path can record the TS) must not produce two messages
// in the pane.
func TestApp_DuplicateMessageEventDoesNotDoubleAppend(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	// Simulate the race: WS echo arrives FIRST, before MessageSentMsg.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "hello",
		},
	})
	verAfterEcho := app.messagepane.Version()
	// Then the HTTP-response optimistic path fires.
	app.Update(MessageSentMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "1700000999.000001", UserID: "USELF", Text: "hello",
		},
	})
	if app.messagepane.Version() != verAfterEcho {
		t.Errorf("MessageSentMsg arriving after WS echo should not re-append; messagepane version advanced")
	}
	// And the model itself contains exactly one message.
	if got := len(app.messagepane.Messages()); got != 1 {
		t.Errorf("expected 1 message in pane, got %d (duplicate)", got)
	}
}

// Defends Bug B: ctrl+u / ctrl+d must move the selection, not just the
// viewport. Otherwise a subsequent j/k snaps back to the original spot.
func TestApp_HalfPageScrollAdvancesSelection(t *testing.T) {
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
	// Provide a sane layout height so halfPageSize() returns > 1.
	app.layoutMsgHeight = 20

	startIdx := app.messagepane.SelectedIndex()
	app.scrollFocusedPanel(-app.halfPageSize()) // ctrl+u
	upIdx := app.messagepane.SelectedIndex()
	if upIdx >= startIdx {
		t.Errorf("ctrl+u should decrease selection; start=%d after=%d", startIdx, upIdx)
	}
	app.scrollFocusedPanel(app.halfPageSize()) // ctrl+d
	downIdx := app.messagepane.SelectedIndex()
	if downIdx <= upIdx {
		t.Errorf("ctrl+d should increase selection; before=%d after=%d", upIdx, downIdx)
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
