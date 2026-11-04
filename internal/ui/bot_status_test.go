package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
)

// botStatusApp is normalOpts at 200x30 (wide enough to draw the channel
// and thread panes side by side) with typing indicators on and the bot
// UBOT named Atlas.
func botStatusApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30))...)
	a.SetTypingEnabled(true)
	a.SetUserNames(map[string]string{"UBOT": "Atlas", "U1": "alice"})
	return a
}

func atlasStatus(channelID, threadTS, status string) BotStatusMsg {
	return BotStatusMsg{WorkspaceID: "T1", ChannelID: channelID, ThreadTS: threadTS, BotUserID: "UBOT", Status: status}
}

// channelIndicator is the channel pane's typing row as plain text.
func channelIndicator(a *App) string {
	return strings.TrimSpace(ansi.Strip(a.renderTypingLine()))
}

func TestBotStatus_ChannelPaneSaysTheBotIsWorking(t *testing.T) {
	a := botStatusApp(t)
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))

	if got, want := channelIndicator(a), "Atlas is working..."; got != want {
		t.Errorf("channel indicator = %q, want %q", got, want)
	}
}

func TestBotStatus_EmptyStatusClearsIt(t *testing.T) {
	a := botStatusApp(t)
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	a.Update(atlasStatus("C1", "100.1", ""))

	if got := channelIndicator(a); got != "" {
		t.Errorf("channel indicator = %q after the bot cleared its status, want none", got)
	}
}

func TestBotStatus_OnlyShownInItsOwnChannel(t *testing.T) {
	a := botStatusApp(t)
	a.Update(atlasStatus("C2", "100.1", "Thinking..."))

	if got := channelIndicator(a); got != "" {
		t.Errorf("C1 indicator = %q for a status in C2, want none", got)
	}
}

func TestBotStatus_SharesTheRowWithPeopleTyping(t *testing.T) {
	a := botStatusApp(t)
	a.Update(UserTypingMsg{ChannelID: "C1", UserID: "U1", WorkspaceID: "T1"})
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))

	if got, want := channelIndicator(a), "alice is typing... · Atlas is working..."; got != want {
		t.Errorf("channel indicator = %q, want %q", got, want)
	}
}

func TestBotStatus_TwoBotsWorking(t *testing.T) {
	a := botStatusApp(t)
	a.SetUserNames(map[string]string{"UBOT": "Atlas", "UBOT2": "Bolt"})
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	a.Update(BotStatusMsg{ChannelID: "C1", ThreadTS: "100.2", BotUserID: "UBOT2", Status: "is searching"})

	if got, want := channelIndicator(a), "Atlas and Bolt are working..."; got != want {
		t.Errorf("channel indicator = %q, want %q", got, want)
	}
}

// One bot working on two threads in the channel is still one bot.
func TestBotStatus_OneBotInTwoThreadsIsNamedOnce(t *testing.T) {
	a := botStatusApp(t)
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	a.Update(atlasStatus("C1", "100.2", "Thinking..."))

	if got, want := channelIndicator(a), "Atlas is working..."; got != want {
		t.Errorf("channel indicator = %q, want %q", got, want)
	}
}

func TestBotStatus_IgnoredWithTypingIndicatorsOff(t *testing.T) {
	a := botStatusApp(t)
	a.SetTypingEnabled(false)
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	a.SetTypingEnabled(true)

	if got := channelIndicator(a); got != "" {
		t.Errorf("channel indicator = %q for a status that arrived while indicators were off, want none", got)
	}
}

// Slack drops a status two minutes after it was set if the bot never
// clears it; slk must not show it forever when the clear never comes.
func TestBotStatus_ExpiresAfterTwoMinutes(t *testing.T) {
	a := botStatusApp(t)
	t0 := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	now := t0
	a.typing.now = func() time.Time { return now }

	_, cmd := a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	if cmd == nil {
		t.Fatal("setting a status started no expiry tick")
	}

	now = t0.Add(119 * time.Second)
	if _, cmd = a.Update(TypingExpiredMsg{}); cmd == nil {
		t.Error("expiry tick stopped while the status was still live")
	}
	if got, want := channelIndicator(a), "Atlas is working..."; got != want {
		t.Errorf("at 1m59s indicator = %q, want %q", got, want)
	}

	now = t0.Add(121 * time.Second)
	if _, cmd = a.Update(TypingExpiredMsg{}); cmd != nil {
		t.Error("expiry tick kept running with nothing left to expire")
	}
	if got := channelIndicator(a); got != "" {
		t.Errorf("at 2m01s indicator = %q, want none", got)
	}
}

func botReply(userID string) NewMessageMsg {
	return NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "200.1", ThreadTS: "100.1", UserID: userID, UserName: "x", Text: "Hello!",
	}}
}

// The bot's reply ends its work even if the clearing event was lost.
func TestBotStatus_TheBotsReplyClearsIt(t *testing.T) {
	a := botStatusApp(t)
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	a.Update(botReply("UBOT"))

	if got := channelIndicator(a); got != "" {
		t.Errorf("channel indicator = %q after the bot replied, want none", got)
	}
}

func TestBotStatus_SomeoneElsesReplyKeepsIt(t *testing.T) {
	a := botStatusApp(t)
	a.Update(atlasStatus("C1", "100.1", "Thinking..."))
	a.Update(botReply("U1"))

	if got, want := channelIndicator(a), "Atlas is working..."; got != want {
		t.Errorf("channel indicator = %q after alice replied, want %q", got, want)
	}
}

// openThreadOnSelected opens the thread on the selected C1 message and
// returns its ts.
func openThreadOnSelected(t *testing.T, a *App) string {
	t.Helper()
	focusMessages(t, a)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible || a.threadPanel.ChannelID() != "C1" {
		t.Fatalf("precondition: thread visible=%v channel=%q, want a C1 thread open", a.threadVisible, a.threadPanel.ChannelID())
	}
	return a.threadPanel.ThreadTS()
}

func TestBotStatus_OpenThreadShowsTheBotsOwnText(t *testing.T) {
	a := botStatusApp(t)
	ts := openThreadOnSelected(t, a)

	updateAndRender(t, a, atlasStatus("C1", ts, "Thinking..."))

	screen := ansi.Strip(a.View().Content)
	if !strings.Contains(screen, "Atlas Thinking...") {
		t.Errorf("thread pane does not show the bot's status:\n%s", screen)
	}
}

// statusRows returns the screen rows containing marker.
func statusRows(screen, marker string) []string {
	var rows []string
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, marker) {
			rows = append(rows, line)
		}
	}
	return rows
}

// An app chooses its status text, and it can be longer than the pane
// is wide. It is one row, cut short, not a paragraph eating the
// thread's replies.
func TestBotStatus_LongStatusTakesOneRow(t *testing.T) {
	a := botStatusApp(t)
	ts := openThreadOnSelected(t, a)

	long := "Thinking " + strings.Repeat("very ", 80) + "hard..."
	updateAndRender(t, a, atlasStatus("C1", ts, long))
	screen := ansi.Strip(a.View().Content)

	rows := statusRows(screen, "very")
	if len(rows) != 1 {
		t.Fatalf("the status takes %d rows, want 1:\n%s", len(rows), screen)
	}
	if !strings.Contains(rows[0], "Atlas Thinking very") || !strings.Contains(rows[0], "…") {
		t.Errorf("status row = %q, want it to start with the app's name and end cut short", rows[0])
	}
}

// Line breaks in the app's text are not rows either.
func TestBotStatus_MultilineStatusTakesOneRow(t *testing.T) {
	a := botStatusApp(t)
	ts := openThreadOnSelected(t, a)

	updateAndRender(t, a, atlasStatus("C1", ts, "Thinking...\nSearching...\r\nAlmost there"))
	screen := ansi.Strip(a.View().Content)

	rows := statusRows(screen, "Atlas Thinking")
	if len(rows) != 1 {
		t.Fatalf("found %d status rows, want 1:\n%s", len(rows), screen)
	}
	if !strings.Contains(rows[0], "Atlas Thinking... Searching... Almost there") {
		t.Errorf("status row = %q, want its lines joined by spaces", rows[0])
	}
}

func TestBotStatus_OtherThreadsStatusStaysOutOfTheThreadPane(t *testing.T) {
	a := botStatusApp(t)
	ts := openThreadOnSelected(t, a)

	updateAndRender(t, a, atlasStatus("C1", ts+"9", "Thinking..."))

	screen := ansi.Strip(a.View().Content)
	if strings.Contains(screen, "Atlas Thinking...") {
		t.Errorf("thread pane shows another thread's status:\n%s", screen)
	}
}
