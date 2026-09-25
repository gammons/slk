// internal/ui/app_thread_broadcast_test.go
//
// Tests for Slack's "Also send to #channel" thread-reply broadcast:
// ctrl+o toggle in the thread compose, the optimistic thread_broadcast
// row in the channel feed, the authoritative swap on success, and the
// rollback on failure.
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

func newThreadBroadcastApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "1700000000.000100"}, nil, "C1", "1700000000.000100")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.SetMode(ModeInsert)
	return app
}

// TestHandleInsertMode_CtrlOTogglesThreadBroadcast pins the ctrl+o
// toggle: it flips the thread compose's broadcast flag on and off, and
// the next Enter dispatches SendThreadReplyMsg with Broadcast=true.
func TestHandleInsertMode_CtrlOTogglesThreadBroadcast(t *testing.T) {
	app := newThreadBroadcastApp(t)

	app.handleInsertMode(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !app.threadCompose.Broadcast() {
		t.Fatal("ctrl+o should turn the thread broadcast toggle on")
	}

	// The toggle must survive typing (ctrl+o is intercepted before the
	// textarea sees it).
	app.threadCompose.SetValue("heads up")
	if !app.threadCompose.Broadcast() {
		t.Fatal("plain typing should not clear the broadcast toggle")
	}

	app.handleInsertMode(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if app.threadCompose.Broadcast() {
		t.Fatal("second ctrl+o should turn the toggle off")
	}

	// Toggle back on and send; the dispatched msg must carry Broadcast.
	app.handleInsertMode(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter in insert mode should produce a send command")
	}
	msg := cmd()
	sendMsg, ok := msg.(SendThreadReplyMsg)
	if !ok {
		t.Fatalf("expected SendThreadReplyMsg, got %T", msg)
	}
	if !sendMsg.Broadcast {
		t.Errorf("SendThreadReplyMsg.Broadcast = false, want true")
	}
}

func TestThreadComposeBroadcastRender(t *testing.T) {
	styles.Apply("default", config.Theme{})
	app := newThreadBroadcastApp(t)
	app.channelNames = map[string]string{"C1": "newsroom"}
	app.threadCompose.SetChannel("newsroom")
	app.threadCompose.SetBroadcast(true)
	app.threadCompose.SetValue("draft reply")

	raw := app.threadCompose.View(40, true)
	for i, l := range strings.Split(raw, "\n") {
		if w := lipgloss.Width(l); w != 39 {
			t.Errorf("threadCompose line %d visual width = %d, want 39 (line: %q)", i, w, l)
		}
		if !strings.Contains(l, "▌") {
			t.Errorf("threadCompose line %d missing border ▌ (line: %q)", i, l)
		}
	}

	frame := panelLayoutFrame{
		ContentHeight: 30,
		ThreadWidth:   42,
		ThreadBorder:  1,
	}
	out := app.renderThreadRegion(frame, 0)
	wantWidth := frame.ThreadWidth + frame.ThreadBorder
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w != wantWidth {
			t.Errorf("thread region line %d visual width = %d, want %d", i, w, wantWidth)
		}
	}
}

// TestHandleInsertMode_AltEnterSendsThreadBroadcast asserts that Alt+Enter
// in the thread compose sends with broadcast immediately as a one-shot,
// without requiring the Ctrl+O toggle first.
func TestHandleInsertMode_AltEnterSendsThreadBroadcast(t *testing.T) {
	app := newThreadBroadcastApp(t)
	app.threadCompose.SetValue("quick broadcast")

	if app.threadCompose.Broadcast() {
		t.Fatal("setup: broadcast should be off initially")
	}

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Alt+Enter with text should return a send cmd")
	}
	msg, ok := cmd().(SendThreadReplyMsg)
	if !ok {
		t.Fatalf("expected SendThreadReplyMsg, got %T", msg)
	}
	if !msg.Broadcast {
		t.Error("SendThreadReplyMsg.Broadcast = false, want true after Alt+Enter")
	}
	if app.threadCompose.Broadcast() {
		t.Error("broadcast toggle should be false after Alt+Enter send")
	}
}

// TestHandleInsertMode_CtrlOInactiveOnChannelCompose guards the
// thread-compose-only scope: ctrl+o while composing a channel message
// must be forwarded to the textarea, not flip any broadcast state.
func TestHandleInsertMode_CtrlOInactiveOnChannelCompose(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)

	app.handleInsertMode(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if app.compose.Broadcast() {
		t.Error("channel compose must never expose a broadcast toggle")
	}
}

// TestSendThreadReply_BroadcastOptimisticChannelRowAndSwap covers the
// feed path: a broadcast reply optimistically lands in the channel
// pane as a thread_broadcast row (with the local placeholder TS), and
// ThreadReplySentMsg swaps it for the authoritative message.
func TestSendThreadReply_BroadcastOptimisticChannelRowAndSwap(t *testing.T) {
	app := newThreadBroadcastApp(t)

	app.Update(SendThreadReplyMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		Text:      "heads up",
		Broadcast: true,
	})

	// Thread pane got the optimistic reply...
	if got := app.threadPanel.ReplyCount(); got != 1 {
		t.Fatalf("thread panel replies = %d, want 1", got)
	}
	// ...and the channel feed got a thread_broadcast placeholder.
	paneMsgs := app.messagepane.Messages()
	if len(paneMsgs) != 1 {
		t.Fatalf("channel pane messages = %d, want 1 optimistic broadcast row", len(paneMsgs))
	}
	localTS := paneMsgs[0].TS
	if !strings.HasPrefix(localTS, "local:") {
		t.Fatalf("channel row TS = %q, want local:... placeholder", localTS)
	}
	if paneMsgs[0].Subtype != "thread_broadcast" {
		t.Errorf("channel row Subtype = %q, want thread_broadcast", paneMsgs[0].Subtype)
	}
	if paneMsgs[0].ThreadTS != "1700000000.000100" {
		t.Errorf("channel row ThreadTS = %q, want parent thread ts", paneMsgs[0].ThreadTS)
	}

	// Non-broadcast send must NOT touch the channel feed.
	app.Update(SendThreadReplyMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		Text:      "quiet reply",
	})
	if got := len(app.messagepane.Messages()); got != 1 {
		t.Fatalf("non-broadcast reply added a channel row: %d rows, want 1", got)
	}

	app.Update(ThreadReplySentMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		LocalTS:   localTS,
		Broadcast: true,
		Message: messages.MessageItem{
			TS: "1700000050.000400", UserID: "USELF", UserName: "you",
			Text: "heads up", ThreadTS: "1700000000.000100",
			Subtype: "thread_broadcast",
		},
	})

	paneMsgs = app.messagepane.Messages()
	if len(paneMsgs) != 1 {
		t.Fatalf("post-swap channel rows = %d, want 1", len(paneMsgs))
	}
	if paneMsgs[0].TS != "1700000050.000400" {
		t.Errorf("post-swap TS = %q, want authoritative Slack ts", paneMsgs[0].TS)
	}
	if paneMsgs[0].Subtype != "thread_broadcast" {
		t.Errorf("post-swap Subtype = %q, want thread_broadcast", paneMsgs[0].Subtype)
	}
}

// TestSendThreadReply_BroadcastFailureRollsBackChannelRow ensures a
// failed broadcast send removes the optimistic row from BOTH the
// thread panel and the channel feed.
func TestSendThreadReply_BroadcastFailureRollsBackChannelRow(t *testing.T) {
	app := newThreadBroadcastApp(t)

	app.Update(SendThreadReplyMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		Text:      "heads up",
		Broadcast: true,
	})
	localTS := app.messagepane.Messages()[0].TS

	app.Update(ThreadReplySendFailedMsg{
		ChannelID: "C1",
		ThreadTS:  "1700000000.000100",
		LocalTS:   localTS,
		Broadcast: true,
		Reason:    "network error",
	})

	if got := len(app.messagepane.Messages()); got != 0 {
		t.Errorf("channel rows after failure = %d, want 0", got)
	}
	if got := app.threadPanel.ReplyCount(); got != 0 {
		t.Errorf("thread replies after failure = %d, want 0", got)
	}
}

// TestOpenThread_ClearsBroadcastToggle locks in the per-thread scope
// of the toggle: opening a (different) thread resets "also send to
// channel" so it can't silently leak into the next thread.
func TestOpenThread_ClearsBroadcastToggle(t *testing.T) {
	app := newThreadBroadcastApp(t)
	app.threadCompose.SetBroadcast(true)
	app.channelNames = map[string]string{"C1": "general"}

	cmd := app.openThreadPanel(messages.MessageItem{TS: "1700000099.000001"}, "C1", "1700000099.000001")
	_ = cmd

	if app.threadCompose.Broadcast() {
		t.Error("opening a thread must clear the broadcast toggle")
	}
	if got := app.threadCompose.Value(); got != "" {
		t.Errorf("compose should be empty on thread open, got %q", got)
	}
	// The thread compose must learn the real channel name so the hint
	// and placeholder read "#general", not a synthetic label.
	app.threadCompose.SetBroadcast(true)
	view := app.threadCompose.View(60, true)
	if !strings.Contains(view, "also send to #general") {
		t.Errorf("hint should use the resolved channel name, got:\n%s", view)
	}
}

// TestNewMessage_InboundThreadBroadcastRendersInChannelFeed tests an
// inbound WebSocket message from another user with subtype thread_broadcast:
// 1. It must append to the parent channel's message feed as a thread_broadcast row.
// 2. It must increment the parent thread's reply count in the channel feed.
// 3. If the thread panel is open for that thread, it must also append to the thread replies.
func TestNewMessage_InboundThreadBroadcastRendersInChannelFeed(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"

	parent := messages.MessageItem{
		TS:   "1700000000.000100",
		Text: "parent discussion",
	}
	app.messagepane.AppendMessage(parent)
	app.threadPanel.SetThread(parent, nil, "C1", "1700000000.000100")
	app.threadVisible = true

	// Inbound WS message from another user with subtype="thread_broadcast"
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:        "1700000050.000200",
			UserID:    "UOTHER",
			Text:      "broadcast from another user",
			ThreadTS:  "1700000000.000100",
			Subtype:   "thread_broadcast",
			Timestamp: "12:05",
		},
	})

	// 1. Channel feed must now have 2 messages: parent + broadcast reply
	feedMsgs := app.messagepane.Messages()
	if len(feedMsgs) != 2 {
		t.Fatalf("channel feed messages = %d, want 2 (parent + broadcast)", len(feedMsgs))
	}
	if feedMsgs[0].ReplyCount != 1 {
		t.Errorf("parent reply count = %d, want 1", feedMsgs[0].ReplyCount)
	}
	if feedMsgs[1].TS != "1700000050.000200" {
		t.Errorf("broadcast row TS = %q, want 1700000050.000200", feedMsgs[1].TS)
	}
	if feedMsgs[1].Subtype != "thread_broadcast" {
		t.Errorf("broadcast row Subtype = %q, want thread_broadcast", feedMsgs[1].Subtype)
	}

	// 2. Thread panel must have received the reply as well
	if got := app.threadPanel.ReplyCount(); got != 1 {
		t.Errorf("thread panel reply count = %d, want 1", got)
	}
	if got := app.threadPanel.Replies()[0].TS; got != "1700000050.000200" {
		t.Errorf("thread reply TS = %q, want 1700000050.000200", got)
	}
}
