package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

type blockPress struct {
	channel, ts, serviceID, blockID string
	action                          blocks.ActionElement
}

// cardRow is a card's section and its row of buttons, the second asking
// for confirmation first, plus a select that cannot be pressed.
func cardRow(text, blockID string) []blocks.Block {
	return []blocks.Block{
		blocks.SectionBlock{Text: text},
		blocks.ActionsBlock{BlockID: blockID, Elements: []blocks.ActionElement{
			{Kind: "button", Label: "Verify", ActionID: "verify", Value: blockID},
			{Kind: "button", Label: "Close", ActionID: "close",
				Confirm: &blocks.ActionConfirm{Title: "Close it?", OKText: "Close", DismissText: "Keep"}},
			{Kind: "static_select", Label: "Assign"},
		}},
	}
}

// botCard is an app's message with one card per text.
func botCard(botID string, texts ...string) messages.MessageItem {
	msg := messages.MessageItem{TS: "2.000000", UserID: "UBOT", UserName: "followup-bot", BotID: botID, Text: "open cards"}
	for i, t := range texts {
		msg.Blocks = append(msg.Blocks, cardRow(t, "card"+string(rune('1'+i)))...)
	}
	return msg
}

// blockButtonApp shows msg in C1, selected in a focused messages pane,
// recording Block Kit presses.
func blockButtonApp(t *testing.T, msg messages.MessageItem) (*App, *[]blockPress) {
	t.Helper()
	a := newTestApp(t, withSize(120, 40), withActiveChannel("C1"), withMessages(msg))
	presses := &[]blockPress{}
	a.SetInteractionService(core.NewInteractionService(core.InteractionServiceFuncs{
		PressBlockAction: func(ch ids.ChannelID, ts ids.MessageTS, svc, blockID string, act blocks.ActionElement) error {
			*presses = append(*presses, blockPress{string(ch), string(ts), svc, blockID, act})
			return nil
		},
	}))
	a.focusedPanel = PanelMessages
	a.messagepane.SelectByIndex(0)
	return a, presses
}

func TestBPressesBlockKitButtonOnMessage(t *testing.T) {
	a, presses := blockButtonApp(t, botCard("B01", "*1. TASK-1 fix the thing*\n• PR merged"))
	pressKey(t, a, keyPress('b'))
	if !dialogShows(a, "[1] Verify") || !dialogShows(a, "[2] Close") || dialogShows(a, "Assign") {
		t.Fatalf("one card: the dialog should list its two buttons, not the select:\n%s", a.confirmPrompt.View())
	}
	if got := toastText(pressKey(t, a, keyPress('1'))); got != "Pressed Verify" {
		t.Errorf("toast = %q", got)
	}
	if len(*presses) != 1 {
		t.Fatalf("presses = %d, want 1", len(*presses))
	}
	p := (*presses)[0]
	if p.channel != "C1" || p.ts != "2.000000" || p.serviceID != "B01" || p.blockID != "card1" ||
		p.action.ActionID != "verify" || p.action.Value != "card1" {
		t.Errorf("press = %+v", p)
	}
}

// Several cards: b asks which card first, by its first line, then shows
// that card's buttons.
func TestBAsksForCardFirst(t *testing.T) {
	a, presses := blockButtonApp(t, botCard("B01", "*1. TASK-1 fix the thing*\n• PR merged", "*2. TASK-2 another*\n• still open"))
	pressKey(t, a, keyPress('b'))
	if !dialogShows(a, "[1] 1. TASK-1 fix the thing") || !dialogShows(a, "[2] 2. TASK-2 another") || dialogShows(a, "Verify") {
		t.Fatalf("first step should list the cards:\n%s", a.confirmPrompt.View())
	}
	pressKey(t, a, keyPress('2'))
	if !dialogShows(a, "[1] Verify") || !dialogShows(a, "still open") {
		t.Fatalf("second step should show card 2 and its buttons:\n%s", a.confirmPrompt.View())
	}
	pressKey(t, a, keyPress('1'))
	if len(*presses) != 1 || (*presses)[0].blockID != "card2" {
		t.Errorf("presses = %+v, want card2's Verify", *presses)
	}
}

func TestBlockKitButtonConfirmsFirst(t *testing.T) {
	a, presses := blockButtonApp(t, botCard("B01", "card"))
	pressKey(t, a, keyPress('b'))
	pressKey(t, a, keyPress('2'))
	if !dialogShows(a, "Close it?") || len(*presses) != 0 {
		t.Fatalf("Close should ask first (presses=%d):\n%s", len(*presses), a.confirmPrompt.View())
	}
	pressKey(t, a, keyPress('2'))
	if len(*presses) != 1 || (*presses)[0].action.ActionID != "close" {
		t.Errorf("presses = %+v", *presses)
	}
}

// A click on a button presses it directly; one that asks first opens
// only its confirmation step.
func TestPressButtonAt(t *testing.T) {
	msg := botCard("B01", "a", "b")
	a, presses := blockButtonApp(t, msg)
	ref := actionPromptRef{channelID: "C1", ts: msg.TS}
	idx := messages.ButtonIndex(msg, "card2", "verify")
	if idx != 2 {
		t.Fatalf("ButtonIndex = %d, want 2", idx)
	}
	cmd := a.pressButtonAt(ref, msg, idx)
	if cmd == nil {
		t.Fatal("no command for a plain button")
	}
	a.Update(cmd())
	if len(*presses) != 1 || (*presses)[0].blockID != "card2" {
		t.Errorf("presses = %+v", *presses)
	}
	a.pressButtonAt(ref, msg, messages.ButtonIndex(msg, "card1", "close"))
	if !dialogShows(a, "Close it?") || len(*presses) != 1 {
		t.Errorf("confirm step should open without pressing:\n%s", a.confirmPrompt.View())
	}
}

// Without the app's bot ID there is no service_id to send.
func TestBlockKitButtonsNeedBotID(t *testing.T) {
	a, _ := blockButtonApp(t, botCard("", "card"))
	if got := toastText(pressKey(t, a, keyPress('b'))); got != "No buttons on this message" {
		t.Errorf("toast = %q", got)
	}
}

func TestInteractiveHintForBlockKitButtons(t *testing.T) {
	if got := messages.InteractiveHint(botCard("B01", "card")); got != "b to respond" {
		t.Errorf("with bot ID: %q", got)
	}
	if got := messages.InteractiveHint(botCard("", "card")); got != "↗ open in Slack to interact" {
		t.Errorf("without bot ID: %q", got)
	}
	// A non-ephemeral's legacy buttons still need Slack: they press as
	// chat.attachmentAction with is_ephemeral, which is only right for
	// an ephemeral.
	legacy := mentionEphemeral("3.000000")
	legacy.Ephemeral = false
	if messages.HasButtonActions(legacy) {
		t.Error("legacy buttons on a normal message should not be pressable")
	}
}

// textCell is where the nth (0-based) occurrence of text starts on
// screen, counting only matches at or right of column minX.
func textCell(t *testing.T, a *App, text string, nth, minX int) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(a.View().Content), "\n") {
		for from := 0; ; {
			i := strings.Index(line[from:], text)
			if i < 0 {
				break
			}
			col := ansi.StringWidth(line[:from+i])
			from += i + len(text)
			if col < minX {
				continue
			}
			if nth == 0 {
				return col, row
			}
			nth--
		}
	}
	t.Fatalf("%q #%d not on screen:\n%s", text, nth, ansi.Strip(a.View().Content))
	return 0, 0
}

// verifyEdge is the screen cell of the nth Verify button's closing
// bracket, its last column: a hit rect off by a column misses it.
func verifyEdge(t *testing.T, a *App, nth, minX int) (x, y int) {
	t.Helper()
	x, y = textCell(t, a, "Verify ]", nth, minX)
	return x + len("Verify ]") - 1, y
}

// leftClick sends a left click at screen (x, y) and runs what it returns.
func leftClick(a *App, x, y int) {
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	for _, m := range drainBatch(cmd) {
		a.Update(m)
	}
}

// A click on a card's button presses that card's button, the second
// card's included: its rows sit below the first card's.
func TestClickPressesBlockKitButton(t *testing.T) {
	a, presses := blockButtonApp(t, botCard("B01", "first card", "second card"))
	x, y := verifyEdge(t, a, 1, 0)
	leftClick(a, x, y)
	if len(*presses) != 1 {
		t.Fatalf("presses = %+v, want one", *presses)
	}
	if p := (*presses)[0]; p.channel != "C1" || p.ts != "2.000000" || p.blockID != "card2" || p.action.ActionID != "verify" {
		t.Errorf("press = %+v, want card2's Verify", p)
	}
	if a.mode == ModeConfirm {
		t.Error("a plain button should press without a dialog")
	}
}

// Without the app's bot ID the buttons are drawn but a click on one
// sends nothing.
func TestClickOnButtonWithoutBotIDDoesNotPress(t *testing.T) {
	a, presses := blockButtonApp(t, botCard("", "card"))
	x, y := verifyEdge(t, a, 0, 0)
	leftClick(a, x, y)
	if len(*presses) != 0 {
		t.Errorf("presses = %+v, want none", *presses)
	}
	// No hit at all, so the click selects the message as on any text.
	if hits := a.messagepane.LastHitsForKindForTest(entrylist.HitButton); len(hits) != 0 {
		t.Errorf("button hits = %+v, want none", hits)
	}
}

// The thread pane: a reply's button and the parent's (whose rows the
// pane assembles itself) both press, in the thread's channel.
func TestClickPressesBlockKitButtonInThread(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, reply messages.MessageItem
		wantTS        string
	}{
		{"reply",
			messages.MessageItem{TS: "1.000000", UserID: "U1", UserName: "alice", Text: "parent"},
			botCard("B01", "first card", "second card"), "2.000000"},
		{"parent",
			func() messages.MessageItem {
				m := botCard("B01", "first card", "second card")
				m.TS = "1.000000"
				return m
			}(),
			messages.MessageItem{TS: "2.000000", UserID: "U1", UserName: "alice", Text: "reply"}, "1.000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, presses := blockButtonApp(t, messages.MessageItem{TS: "0.500000", UserID: "U1", UserName: "alice", Text: "hi"})
			a.width = 200
			a.threadPanel.SetThread(tc.parent, []messages.MessageItem{tc.reply}, "C9", "1.000000")
			a.threadVisible = true
			_ = a.View()
			if a.layout.threadEnd <= a.layout.msgEnd {
				t.Fatalf("precondition: thread pane not drawn (bands %+v)", *a.layout)
			}
			x, y := verifyEdge(t, a, 1, a.layout.msgEnd)
			leftClick(a, x, y)
			if len(*presses) != 1 {
				t.Fatalf("presses = %+v, want one", *presses)
			}
			if p := (*presses)[0]; p.channel != "C9" || p.ts != tc.wantTS || p.blockID != "card2" || p.action.ActionID != "verify" {
				t.Errorf("press = %+v, want card2's Verify on %s in C9", p, tc.wantTS)
			}
		})
	}
}
