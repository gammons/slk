package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

type removeCall struct {
	team, channel, ts string
	id                int
}

// timeoutErr is a removal that hit its deadline, wrapped as production
// does: postForm wraps the transport's *url.Error.
func timeoutErr() error {
	return fmt.Errorf("calling chat.deleteAttachment: %w", &url.Error{Op: "Post", URL: "x", Err: context.DeadlineExceeded})
}

func card(id int, title, url string) blocks.LegacyAttachment {
	return blocks.LegacyAttachment{ID: id, Title: title, FromURL: url}
}

func ownMessage(ts string, atts ...blocks.LegacyAttachment) messages.MessageItem {
	return messages.MessageItem{TS: ts, UserID: "U1", UserName: "me", Text: "links", LegacyAttachments: atts}
}

// previewApp is an App in T1 on C1 whose first message is selected and
// focused, with the user U1. Removals are recorded and answered with err.
// Toast clear ticks are stopped, so a toast stays on the status bar.
func previewApp(t *testing.T, err error, msgs ...messages.MessageItem) (*App, *[]removeCall) {
	t.Helper()
	a := newTestApp(t, withSize(120, 40), withActiveChannel("C1"), withMessages(msgs...))
	a.currentUserID = "U1"
	a.activeTeamID = "T1"
	a.focusedPanel = PanelMessages
	a.messagepane.SelectByIndex(0)
	oldTick := toastTick
	toastTick = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	t.Cleanup(func() { toastTick = oldTick })
	calls := &[]removeCall{}
	a.setAttachmentRemoverForTest(func(team string, ch ids.ChannelID, ts ids.MessageTS, id int) core.Msg {
		*calls = append(*calls, removeCall{team, string(ch), string(ts), id})
		return AttachmentRemovedMsg{ChannelID: string(ch), TS: string(ts), Err: err}
	})
	return a, calls
}

// pressKeyAll is pressKey, but it feeds every answer back through Update,
// however deep the chain. Toast clear ticks never fire (previewApp stops
// them), so the toast a key leaves can be read.
func pressKeyAll(t *testing.T, a *App, k tea.KeyMsg) []tea.Msg {
	t.Helper()
	_, cmd := a.Update(k)
	var out []tea.Msg
	queue := drainBatch(cmd)
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		if m == nil {
			continue
		}
		out = append(out, m)
		_, next := a.Update(m)
		queue = append(queue, drainBatch(next)...)
	}
	return out
}

// toastShows reports whether the status bar currently shows text.
func toastShows(a *App, text string) bool {
	return strings.Contains(statusbarText(a), text)
}

func TestRemovePreview_OnePreviewConfirmSends(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "Example", "https://example.com")))
	pressKeyAll(t, a, keyPress('x'))
	if a.mode != ModeConfirm || !dialogShows(a, "Remove preview?") || !dialogShows(a, "Example") {
		t.Fatalf("mode = %v; want the prompt naming the preview", a.mode)
	}
	if len(*calls) != 0 {
		t.Fatal("the prompt sent before it was confirmed")
	}
	if toast := toastText(pressKeyAll(t, a, keyPress('y'))); toast != "" {
		t.Errorf("a successful removal toasted %q", toast)
	}
	want := []removeCall{{"T1", "C1", "1.0", 1}}
	if len(*calls) != 1 || (*calls)[0] != want[0] {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v after confirming", a.mode)
	}
}

func TestRemovePreview_CancelSendsNothing(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "Example", "https://example.com")))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('n'))
	if len(*calls) != 0 || a.mode != ModeNormal {
		t.Errorf("calls = %+v, mode = %v; cancel must send nothing", *calls, a.mode)
	}
}

func TestRemovePreview_UsesURLWhenThereIsNoTitle(t *testing.T) {
	a, _ := previewApp(t, nil, ownMessage("1.0", card(1, "", "https://example.com/a")))
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "https://example.com/a") {
		t.Errorf("prompt does not name the URL:\n%s", a.confirmPrompt.View())
	}
}

func TestRemovePreview_SeveralPreviewsOffersEach(t *testing.T) {
	msg := ownMessage("1.0",
		card(1, "First", "https://a.example"),
		blocks.LegacyAttachment{ID: 2, Title: "A bot card"}, // not a preview
		card(3, "Third", "https://c.example"),
	)
	a, calls := previewApp(t, nil, msg)
	pressKeyAll(t, a, keyPress('x'))
	for _, want := range []string{"[1] First", "[2] Third", "[Cancel]"} {
		if !dialogShows(a, want) {
			t.Errorf("prompt missing %q:\n%s", want, a.confirmPrompt.View())
		}
	}
	if dialogShows(a, "A bot card") {
		t.Error("a card without a URL was offered")
	}
	pressKeyAll(t, a, keyPress('2'))
	if len(*calls) != 1 || (*calls)[0].id != 3 {
		t.Errorf("calls = %+v, want one for attachment 3", *calls)
	}
}

// The chooser offers at most nine previews, one per digit key, so the
// prompt keeps Cancel on a short terminal however long the titles are.
func TestRemovePreview_ChooserCapsAtNine(t *testing.T) {
	var atts []blocks.LegacyAttachment
	for i := 1; i <= 11; i++ {
		atts = append(atts, card(i, fmt.Sprintf("Preview %d %s", i, strings.Repeat("long title ", 8)), fmt.Sprintf("https://%d.example", i)))
	}
	a, calls := previewApp(t, nil, ownMessage("1.0", atts...))
	a.width, a.height = 80, 24
	pressKeyAll(t, a, keyPress('x'))
	for _, want := range []string{"first 9 of 11", "[9] Preview 9", "[Cancel]"} {
		if !dialogShows(a, want) {
			t.Errorf("prompt missing %q:\n%s", want, a.confirmPrompt.View())
		}
	}
	if dialogShows(a, "Preview 10") {
		t.Error("a tenth preview was offered")
	}
	if n := strings.Count(a.confirmPrompt.View(), "\n") + 1; n > a.height {
		t.Errorf("prompt is %d lines, taller than the %d-line terminal", n, a.height)
	}
	pressKeyAll(t, a, keyPress('9'))
	if len(*calls) != 1 || (*calls)[0].id != 9 {
		t.Errorf("calls = %+v, want one for attachment 9", *calls)
	}
}

func TestRemovePreview_SeveralPreviewsCancelButton(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "First", "https://a.example"), card(2, "Second", "https://b.example")))
	pressKeyAll(t, a, keyPress('x'))
	clickConfirmText(t, a, "Cancel")
	if len(*calls) != 0 || a.confirmPrompt.IsVisible() {
		t.Errorf("calls = %+v, visible = %v; Cancel must close without sending", *calls, a.confirmPrompt.IsVisible())
	}
}

func TestRemovePreview_NoPreviewToasts(t *testing.T) {
	for name, msg := range map[string]messages.MessageItem{
		"no attachments": ownMessage("1.0"),
		"a bot card":     ownMessage("1.0", blocks.LegacyAttachment{ID: 1, Title: "Deploy ok"}),
	} {
		t.Run(name, func(t *testing.T) {
			a, calls := previewApp(t, nil, msg)
			if got := toastText(pressKeyAll(t, a, keyPress('x'))); got != "" {
				// toastWithClear sets the status bar directly.
				t.Errorf("unexpected ToastMsg %q", got)
			}
			if a.confirmPrompt.IsVisible() || len(*calls) != 0 {
				t.Error("prompted or sent with no preview")
			}
			if !toastShows(a, "No link preview on this message") {
				t.Errorf("toast does not show %q", "No link preview on this message")
			}
		})
	}
}

func TestRemovePreview_NotOwnMessage(t *testing.T) {
	msg := ownMessage("1.0", card(1, "Example", "https://example.com"))
	msg.UserID = "U2"
	a, calls := previewApp(t, nil, msg)
	pressKeyAll(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || len(*calls) != 0 {
		t.Error("prompted for someone else's message")
	}
	if !toastShows(a, "You can only remove previews from your own messages") {
		t.Errorf("toast does not show %q", "You can only remove previews from your own messages")
	}
}

func TestRemovePreview_RefusedOnEphemeral(t *testing.T) {
	msg := ownMessage("1.0", card(1, "Example", "https://example.com"))
	msg.Ephemeral = true
	a, calls := previewApp(t, nil, msg)
	toast := toastText(pressKeyAll(t, a, keyPress('x')))
	if toast != "Only visible to you" || a.confirmPrompt.IsVisible() || len(*calls) != 0 {
		t.Errorf("toast = %q, prompt = %v, calls = %+v; want the ephemeral refusal", toast, a.confirmPrompt.IsVisible(), *calls)
	}
}

// While a removal is in flight, a second x on that message does nothing:
// Slack renumbers the attachments, so a second id could name another one.
// A timed-out call is still in flight as far as slk can tell.
func TestRemovePreview_SecondPressWhilePendingIsRefused(t *testing.T) {
	a, calls := previewApp(t, timeoutErr(), ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('1'))
	pressKeyAll(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || len(*calls) != 1 {
		t.Errorf("prompt = %v, calls = %d; the second x must do nothing", a.confirmPrompt.IsVisible(), len(*calls))
	}
	if !toastShows(a, "Removing preview…") {
		t.Error("toast does not show \"Removing preview…\"")
	}
}

// confirmAnswer presses k in the open prompt and returns the
// RemovePreviewMsg its command delivers, without reducing it: the runtime
// delivers it later, after keys already queued.
func confirmAnswer(t *testing.T, a *App, k tea.KeyMsg) RemovePreviewMsg {
	t.Helper()
	_, cmd := a.Update(k)
	for _, m := range drainBatch(cmd) {
		if rp, ok := m.(RemovePreviewMsg); ok {
			return rp
		}
	}
	t.Fatalf("pressing %v delivered no RemovePreviewMsg", k)
	return RemovePreviewMsg{}
}

// The prompt closes before its answer is reduced, so a second x can open
// another prompt for the same message in between. The check at confirm is
// what stops the second removal: Slack renumbers after the first, so its
// id could name another preview.
func TestRemovePreview_SecondPromptConfirmedBeforeTheFirstStartsIsRefused(t *testing.T) {
	a, calls := previewApp(t, timeoutErr(), ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyAll(t, a, keyPress('x'))
	first := confirmAnswer(t, a, keyPress('1'))
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove which preview?") {
		t.Fatal("x before the first answer was reduced should prompt")
	}
	second := confirmAnswer(t, a, keyPress('2'))

	for _, m := range []tea.Msg{first, second} {
		_, cmd := a.Update(m)
		for _, res := range drainBatch(cmd) {
			a.Update(res)
		}
	}
	if len(*calls) != 1 || (*calls)[0].id != 1 {
		t.Errorf("calls = %+v, want only the first removal", *calls)
	}
	if !toastShows(a, "Removing preview…") {
		t.Errorf("toast = %q, want the pending refusal", statusbarText(a))
	}
}

// Slack answered ok: the removal is done and only its echo is late, so it
// must not expire. x says so and sends nothing, however long it takes, and
// the pane is not touched (Slack's renumbering is not verified).
func TestRemovePreview_ConfirmedStaysUntilThePreviewIsGone(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('1'))

	now = now.Add(5 * time.Minute)
	pressKeyAll(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || len(*calls) != 1 {
		t.Fatalf("prompt = %v, calls = %d; a confirmed removal must not expire", a.confirmPrompt.IsVisible(), len(*calls))
	}
	if !toastShows(a, "Preview removed, reload the channel to see it") {
		t.Error("toast does not say the preview was removed")
	}
	if got := a.messagepane.Messages()[0].LegacyAttachments; len(got) != 2 {
		t.Errorf("pane attachments = %+v; the pane must not be changed before the echo", got)
	}

	// A reload that no longer has the preview ends it.
	a.messagepane.SetMessages([]messages.MessageItem{ownMessage("1.0", card(1, "Two", "https://b.example"))})
	a.messagepane.SelectByIndex(0)
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("x after the reload should prompt again")
	}
}

// An echo that still shows the targeted preview is not the answer: a
// text edit from another client, or a second unfurl landing.
func TestRemovePreview_EchoThatKeepsThePreviewLeavesItPending(t *testing.T) {
	for name, echo := range map[string]messages.MessageItem{
		"same previews": ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")),
		"second unfurl": ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example"), card(3, "Three", "https://c.example")),
	} {
		t.Run(name, func(t *testing.T) {
			a, calls := previewApp(t, timeoutErr(), ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
			pressKeyAll(t, a, keyPress('x'))
			pressKeyAll(t, a, keyPress('1'))
			a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: echo})
			pressKeyAll(t, a, keyPress('x'))
			if a.confirmPrompt.IsVisible() || len(*calls) != 1 {
				t.Errorf("prompt = %v, calls = %d; the removal must still be pending", a.confirmPrompt.IsVisible(), len(*calls))
			}
		})
	}
}

// The echo that carries the new attachment list ends the wait.
func TestRemovePreview_EchoEndsPending(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('1'))

	a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: ownMessage("1.0", card(1, "Two", "https://b.example"))})
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Fatal("x after the echo should prompt again")
	}
	pressKeyAll(t, a, keyPress('y'))
	// Ids were renumbered by Slack: the remaining card is now 1.
	if len(*calls) != 2 || (*calls)[1].id != 1 {
		t.Errorf("calls = %+v, want a second call for the renumbered id 1", *calls)
	}
}

// Slack refused: nothing was done, so the user may try again at once.
func TestRemovePreview_RefusalToastsAndClearsPending(t *testing.T) {
	a, calls := previewApp(t, &core.AttachmentRefusedError{Reason: "cant_delete_message"}, ownMessage("1.0", card(1, "One", "https://a.example")))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	if !toastShows(a, "Could not remove the preview: cant_delete_message") {
		t.Errorf("toast does not show %q", "Could not remove the preview: cant_delete_message")
	}
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("a failed removal must not stay pending")
	}
	if len(*calls) != 1 {
		t.Errorf("calls = %d, want 1", len(*calls))
	}
}

// Any failure that is not a refusal (a 5xx, a dropped connection, an
// unreadable answer) may still have happened, like a timeout: it stays
// pending until the echo, a reload or the timeout.
func TestRemovePreview_AmbiguousFailureStaysPending(t *testing.T) {
	a, calls := previewApp(t, errors.New("calling chat.deleteAttachment: HTTP 502 Bad Gateway: <html>"), ownMessage("1.0", card(1, "One", "https://a.example")))
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	if !toastShows(a, "No clear answer from Slack, check the message") {
		t.Errorf("toast = %q, want the no-clear-answer toast", statusbarText(a))
	}
	pressKeyAll(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || len(*calls) != 1 || !toastShows(a, "Removing preview…") {
		t.Errorf("prompt = %v, calls = %d; an ambiguous failure must stay pending", a.confirmPrompt.IsVisible(), len(*calls))
	}
	now = now.Add(31 * time.Second)
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("pending must end after 30s")
	}
}

// A call that timed out may still have happened: it stays pending until
// the echo or the timeout, and the toast says to check the message.
func TestRemovePreview_TimeoutStaysPendingUntilThirtySeconds(t *testing.T) {
	a, _ := previewApp(t, timeoutErr(), ownMessage("1.0", card(1, "One", "https://a.example")))
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	if !toastShows(a, "Removing the preview timed out, check the message") {
		t.Errorf("toast does not show %q", "Removing the preview timed out, check the message")
	}

	now = now.Add(29 * time.Second)
	pressKeyAll(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || !toastShows(a, "Removing preview…") {
		t.Error("still pending at 29s")
	}
	now = now.Add(2 * time.Second)
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("pending must end after 30s")
	}
}

// A reload that replaces the message's attachments ends the wait too.
func TestRemovePreview_ReloadEndsPending(t *testing.T) {
	a, _ := previewApp(t, timeoutErr(), ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('1'))

	a.messagepane.SetMessages([]messages.MessageItem{ownMessage("1.0", card(1, "Two", "https://b.example"))})
	a.messagepane.SelectByIndex(0)
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("a reload that changed the previews must end the wait")
	}
}

// The prompt was built against one attachment list; if the message
// changed underneath it, confirming must not send the old id.
func TestRemovePreview_ConfirmAfterTheMessageChangedIsRefused(t *testing.T) {
	for name, changed := range map[string]messages.MessageItem{
		"preview gone":       ownMessage("1.0"),
		"id now has another": ownMessage("1.0", card(1, "Other", "https://other.example")),
		"renumbered":         ownMessage("1.0", card(1, "Second", "https://b.example")),
	} {
		t.Run(name, func(t *testing.T) {
			a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "First", "https://a.example"), card(2, "Second", "https://b.example")))
			pressKeyAll(t, a, keyPress('x'))
			a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: changed})
			pressKeyAll(t, a, keyPress('1')) // "First": id 1, https://a.example
			if len(*calls) != 0 {
				t.Errorf("calls = %+v; a stale confirm must not send", *calls)
			}
			if !toastShows(a, "That preview changed, try again") {
				t.Errorf("toast does not show %q", "That preview changed, try again")
			}
		})
	}
}

// The request names the workspace the key was pressed in, not the one
// that is active when the prompt is confirmed.
func TestRemovePreview_RequestCarriesTheTeamOfThePress(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example")))
	pressKeyAll(t, a, keyPress('x'))
	a.activeTeamID = "T2"
	pressKeyAll(t, a, keyPress('y'))
	if len(*calls) != 1 || (*calls)[0].team != "T1" {
		t.Errorf("calls = %+v, want team T1", *calls)
	}
}

func TestRemovePreview_ThreadReply(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("P1"))
	a.threadPanel.SetThread(ownMessage("P1"), []messages.MessageItem{ownMessage("R1", card(1, "One", "https://a.example"))}, "C1", "P1")
	a.threadVisible = true
	a.focusedPanel = PanelThread
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	if len(*calls) != 1 || (*calls)[0] != (removeCall{"T1", "C1", "R1", 1}) {
		t.Errorf("calls = %+v, want one for reply R1", *calls)
	}
}

// The thread parent is selectable in the thread pane, and its prompt must
// work when no channel window has the parent loaded (a thread opened from
// Activity, or a parent older than the loaded history).
func TestRemovePreview_ThreadParent(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("9.0"))
	a.threadPanel.SetThread(ownMessage("P1", card(1, "One", "https://a.example")), []messages.MessageItem{ownMessage("R1")}, "C1", "P1")
	a.threadVisible = true
	a.focusedPanel = PanelThread
	a.threadPanel.GoToTop()
	a.threadPanel.MoveUp() // from the first reply onto the parent
	if sel, _ := a.threadPanel.SelectedMessage(); sel.TS != "P1" {
		t.Fatalf("selected %q, want the parent", sel.TS)
	}
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	if toastShows(a, "That preview changed") {
		t.Error("the parent's confirm was refused")
	}
	if len(*calls) != 1 || (*calls)[0] != (removeCall{"T1", "C1", "P1", 1}) {
		t.Errorf("calls = %+v, want one for parent P1", *calls)
	}
}

func TestRemovePreview_IDIsThePositionWhenSlackSentNone(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0",
		blocks.LegacyAttachment{Title: "bot"},
		blocks.LegacyAttachment{Title: "Second", FromURL: "https://b.example"},
	))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	if len(*calls) != 1 || (*calls)[0].id != 2 {
		t.Errorf("calls = %+v, want attachment 2 (its position)", *calls)
	}
}

// Two previews of the same URL: after the first is removed Slack renumbers
// the twin into its id, so (id, URL) is still on the message. The echo
// with one fewer copy of that URL still ends the wait.
func TestRemovePreview_TwinURLEchoEndsPending(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "Same", "https://a.example"), card(2, "Same", "https://a.example")))
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('1'))
	if len(*calls) != 1 {
		t.Fatalf("calls = %+v, want one removal", *calls)
	}
	a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: ownMessage("1.0", card(1, "Same", "https://a.example"))})
	pressKeyAll(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Fatal("x after the echo should prompt again for the remaining twin")
	}
}

// heldRemovals swaps a's remover for one that records each call and
// answers nothing, so the test delivers each result itself, in the order
// it chooses.
func heldRemovals(a *App) *[]removeCall {
	calls := &[]removeCall{}
	a.setAttachmentRemoverForTest(func(team string, ch ids.ChannelID, ts ids.MessageTS, id int) core.Msg {
		*calls = append(*calls, removeCall{team, string(ch), string(ts), id})
		return nil
	})
	return calls
}

// A's echo ends A before A's HTTP answer arrives, and B starts on the same
// message. A's late answer is about A: it must not confirm B (a confirmed
// removal never expires) nor end it.
func TestRemovePreview_LateResultOfAnEndedRemovalIsIgnored(t *testing.T) {
	for name, late := range map[string]error{
		"ok":      nil,
		"refused": &core.AttachmentRefusedError{Reason: "cant_delete_message"},
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
			calls := heldRemovals(a)
			now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
			a.now = func() time.Time { return now }

			pressKeyAll(t, a, keyPress('x'))
			pressKeyAll(t, a, keyPress('1')) // A
			seqA := a.previewRemovals[previewRemovalKey("C1", "1.0")].seq
			a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: ownMessage("1.0", card(1, "Two", "https://b.example"))})
			pressKeyAll(t, a, keyPress('x'))
			pressKeyAll(t, a, keyPress('y')) // B
			if len(*calls) != 2 {
				t.Fatalf("calls = %+v, want A and B", *calls)
			}

			a.Update(AttachmentRemovedMsg{ChannelID: "C1", TS: "1.0", Seq: seqA, Err: late})
			b, ok := a.previewRemovals[previewRemovalKey("C1", "1.0")]
			if !ok || b.seq == seqA || b.confirmed {
				t.Fatalf("B = %+v, pending = %v; A's answer must leave B pending and unconfirmed", b, ok)
			}
			if toastShows(a, "Could not remove the preview") {
				t.Error("A's late answer toasted")
			}

			// B never hears back: it expires instead of claiming success.
			now = now.Add(31 * time.Second)
			pressKeyAll(t, a, keyPress('x'))
			if !dialogShows(a, "Remove preview?") {
				t.Error("B must expire after the timeout")
			}
		})
	}
}

// The answer of the removal in flight is the one that counts.
func TestRemovePreview_ResultOfThePendingRemovalConfirmsIt(t *testing.T) {
	a, _ := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example")))
	heldRemovals(a)
	pressKeyAll(t, a, keyPress('x'))
	pressKeyAll(t, a, keyPress('y'))
	r := a.previewRemovals[previewRemovalKey("C1", "1.0")]
	a.Update(AttachmentRemovedMsg{ChannelID: "C1", TS: "1.0", Seq: r.seq})
	if !a.previewRemovals[previewRemovalKey("C1", "1.0")].confirmed {
		t.Error("the pending removal's own ok must confirm it")
	}
}
