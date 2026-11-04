// internal/ui/remove_preview.go
//
// x in normal mode: remove a link preview from the user's own message
// (Slack's chat.deleteAttachment). The key opens a confirm prompt; the
// confirmed press comes back as RemovePreviewMsg, which re-checks the
// message and calls MessageService.RemoveAttachment. Both that message
// and the call's AttachmentRemovedMsg are reduced in
// reducer_remove_preview.go. On success nothing is done: the
// message_changed echo updates the message (and ends the pending state
// once the preview is gone, see reduceNewMessage).
//
// Slack renumbers the attachments after a removal, so an id is only
// good against the list it was read from. Four guards follow from
// that: one removal per message at a time (previewRemovals), kept until
// Slack surely refused or the message shows the result; a re-check of
// the id and its URL when the prompt is confirmed; the team the key was
// pressed in travels with the request; and each removal's seq, so the
// answer of one never lands on another. A message can also be on screen
// twice (the open thread's parent and the channel) with one copy reloaded
// and the other not, so x and the confirm refuse while the copies disagree
// (previewCopiesAgree).
package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// previewRemovalTimeout is how long a removal blocks another on the same
// message when no echo or reload says it is over.
const previewRemovalTimeout = 30 * time.Second

// previewLabelWidth is the most cells a preview's name takes in a prompt.
const previewLabelWidth = 60

// maxPreviewChoices is the most previews the chooser offers: one per digit
// key, and few enough that the prompt (a row per long label) keeps its
// Cancel button on a short terminal. The rest can be removed once some of
// these are gone.
const maxPreviewChoices = 9

// RemovePreviewMsg is a confirmed removal: the prompt button's answer.
// URL is what the preview showed when the prompt was built.
type RemovePreviewMsg struct {
	TeamID       string
	ChannelID    string
	ThreadTS     string
	TS           string
	AttachmentID int
	URL          string
}

// previewRemoval is one removal in flight: the URL of the preview it
// targets, and how many previews of that URL the message showed then.
// The wait ends when the message shows fewer of that URL (the echo, or a
// reload), on a definite refusal, or after previewRemovalTimeout.
// Counting, not looking for the (id, URL) pair, handles two previews of
// one URL: Slack renumbers the twin into the removed one's id, so the pair
// is still there after the removal.
// confirmed is set once Slack answered ok: Slack has acted and the echo
// is only late, so the timeout no longer applies.
//
// seq names the removal: the result of the call carries it back, and a
// result whose seq is not the pending one's is about a removal that is
// already over.
type previewRemoval struct {
	seq       uint64
	started   time.Time
	url       string
	copies    int
	confirmed bool
}

func previewRemovalKey(channelID, ts string) string { return channelID + "/" + ts }

// hasPreview reports whether ps lists the preview with this id and URL.
func hasPreview(ps []messages.RemovablePreview, id int, url string) bool {
	for _, p := range ps {
		if p.AttachmentID == id && p.URL == url {
			return true
		}
	}
	return false
}

// previewURLCount is how many of ps unfurl url.
func previewURLCount(ps []messages.RemovablePreview, url string) int {
	n := 0
	for _, p := range ps {
		if p.URL == url {
			n++
		}
	}
	return n
}

// previewRemovalState reports the removal in flight for the message, if
// any, given its current previews. One whose message shows fewer previews
// of the targeted URL is over and forgotten, as is an unconfirmed one past
// the timeout.
func (a *App) previewRemovalState(channelID, ts string, previews []messages.RemovablePreview) (r previewRemoval, ok bool) {
	k := previewRemovalKey(channelID, ts)
	r, ok = a.previewRemovals[k]
	if !ok {
		return previewRemoval{}, false
	}
	if previewURLCount(previews, r.url) < r.copies || (!r.confirmed && a.now().Sub(r.started) >= previewRemovalTimeout) {
		delete(a.previewRemovals, k)
		return previewRemoval{}, false
	}
	return r, true
}

// previewRemovalToast is the toast for x on a message with a removal in
// flight.
func previewRemovalToast(r previewRemoval) string {
	if r.confirmed {
		return "Preview removed, reload the channel to see it"
	}
	return "Removing preview…"
}

// startPreviewRemoval records a removal of p from a message currently
// showing previews, and returns its seq.
func (a *App) startPreviewRemoval(channelID, ts string, p messages.RemovablePreview, previews []messages.RemovablePreview) uint64 {
	if a.previewRemovals == nil {
		a.previewRemovals = map[string]previewRemoval{}
	}
	a.previewRemovalSeq++
	a.previewRemovals[previewRemovalKey(channelID, ts)] = previewRemoval{
		seq: a.previewRemovalSeq, started: a.now(), url: p.URL, copies: previewURLCount(previews, p.URL),
	}
	return a.previewRemovalSeq
}

// pendingPreviewRemoval is the removal in flight for the message if its
// seq is seq, read as is (no expiry: the answer of an expired removal
// still says what Slack did).
func (a *App) pendingPreviewRemoval(channelID, ts string, seq uint64) (previewRemoval, bool) {
	r, ok := a.previewRemovals[previewRemovalKey(channelID, ts)]
	return r, ok && r.seq == seq
}

// previewRemovalEchoed ends the wait for a message whose change echo shows
// fewer previews of the targeted URL. An echo that still shows as many (a
// text edit from another client, a second unfurl landing) leaves it
// pending.
func (a *App) previewRemovalEchoed(channelID string, echo messages.MessageItem) {
	a.previewRemovalState(channelID, echo.TS, messages.RemovablePreviews(echo))
}

// clearPreviewRemoval ends the wait for a message.
func (a *App) clearPreviewRemoval(channelID, ts string) {
	delete(a.previewRemovals, previewRemovalKey(channelID, ts))
}

// staleCopyToast is the toast for x on a message whose copies disagree.
// The stale copy is the open thread's: a channel reload refreshes every
// channel window but not the thread pane, so reopening the thread is what
// fixes it.
const staleCopyToast = "This message changed, close and reopen the thread"

// previewCopiesAgree reports whether every copy of the message the App
// holds offers the same previews: the open thread's parent or reply, and
// the message in each window on the channel. A channel window counts in
// any view: it is not necessarily newer, but it can be acted on again as
// soon as the view returns.
// Copies that disagree mean one is stale, and a stale id may now name
// another preview (Slack renumbers them after a removal).
//
// Callers check this before previewRemovalState: the stale copy must not
// end a removal that the fresh one still shows, nor the fresh one end it
// while the stale one can still be confirmed.
func (a *App) previewCopiesAgree(channelID, ts string) bool {
	var copies []messages.MessageItem
	if a.threadPanel.ChannelID() == channelID {
		// The pane is cleared when the thread closes, so a loaded thread is
		// an open one.
		if p := a.threadPanel.ParentMsg(); p.TS == ts {
			copies = append(copies, p)
		}
		for _, r := range a.threadPanel.Replies() {
			if r.TS == ts {
				copies = append(copies, r)
			}
		}
	}
	for _, mm := range a.modelsForChannel(channelID) {
		for _, m := range mm.Messages() {
			if m.TS == ts {
				copies = append(copies, m)
			}
		}
	}
	for i := 1; i < len(copies); i++ {
		if !slices.EqualFunc(messages.RemovablePreviews(copies[0]), messages.RemovablePreviews(copies[i]), samePreview) {
			return false
		}
	}
	return true
}

// samePreview reports whether p and q name the same preview by the same id.
// The title is not compared: it does not change what the id removes.
func samePreview(p, q messages.RemovablePreview) bool {
	return p.AttachmentID == q.AttachmentID && p.URL == q.URL
}

// previewLabel is p's name for a prompt, on one line.
func previewLabel(p messages.RemovablePreview) string {
	return ansi.Truncate(strings.Join(strings.Fields(p.Label()), " "), previewLabelWidth, "…")
}

// removePreviewOfSelected is x: confirm, then remove, a link preview of
// the selected message. One preview gets the yes/no prompt, several a
// prompt with a button each.
func (a *App) removePreviewOfSelected() tea.Cmd {
	msg, ref, ok := a.selectedMessageInFocus()
	if !ok {
		return nil
	}
	if !a.isOwnMessage(msg) {
		return toastWithClear(a, "You can only remove previews from your own messages", 3*time.Second)
	}
	previews := messages.RemovablePreviews(msg)
	if len(previews) == 0 {
		return toastWithClear(a, "No link preview on this message", 3*time.Second)
	}
	if !a.previewCopiesAgree(ref.channelID, ref.ts) {
		return toastWithClear(a, staleCopyToast, 3*time.Second)
	}
	if r, pending := a.previewRemovalState(ref.channelID, ref.ts, previews); pending {
		return toastWithClear(a, previewRemovalToast(r), 3*time.Second)
	}
	remove := func(p messages.RemovablePreview) func() tea.Msg {
		req := RemovePreviewMsg{
			TeamID: a.activeTeamID, ChannelID: ref.channelID, ThreadTS: ref.threadTS, TS: ref.ts,
			AttachmentID: p.AttachmentID, URL: p.URL,
		}
		return func() tea.Msg { return req }
	}
	if len(previews) == 1 {
		a.openConfirmPrompt("Remove preview?", previewLabel(previews[0]), remove(previews[0]))
		return nil
	}
	choice := confirmprompt.Choice{Title: "Remove which preview?", Default: -1}
	if len(previews) > maxPreviewChoices {
		choice.Body = "The first " + strconv.Itoa(maxPreviewChoices) + " of " + strconv.Itoa(len(previews)) +
			" previews. Remove one to reach the rest."
		previews = previews[:maxPreviewChoices]
	}
	for i, p := range previews {
		choice.Buttons = append(choice.Buttons, confirmprompt.Button{
			Label:  previewLabel(p),
			Keys:   key.NewBinding(key.WithKeys(strconv.Itoa(i + 1))),
			Action: remove(p),
		})
	}
	choice.Buttons = append(choice.Buttons, confirmprompt.Button{Label: "Cancel"})
	a.openConfirmChoice(choice)
	return nil
}

// removePreview runs a confirmed removal if the message still shows the
// preview the prompt named.
func (a *App) removePreview(m RemovePreviewMsg) tea.Cmd {
	cur, ok := a.findActionMessage(actionPromptRef{channelID: m.ChannelID, ts: m.TS, threadTS: m.ThreadTS})
	if !ok {
		return toastWithClear(a, "That preview changed, try again", 3*time.Second)
	}
	previews := messages.RemovablePreviews(cur)
	if !hasPreview(previews, m.AttachmentID, m.URL) {
		return toastWithClear(a, "That preview changed, try again", 3*time.Second)
	}
	if !a.previewCopiesAgree(m.ChannelID, m.TS) {
		return toastWithClear(a, staleCopyToast, 3*time.Second)
	}
	// x checked this too, but the prompt closes before its answer is
	// reduced: a second x can open another prompt in between.
	if r, pending := a.previewRemovalState(m.ChannelID, m.TS, previews); pending {
		return toastWithClear(a, previewRemovalToast(r), 3*time.Second)
	}
	seq := a.startPreviewRemoval(m.ChannelID, m.TS, messages.RemovablePreview{AttachmentID: m.AttachmentID, URL: m.URL}, previews)
	svc := a.messageSvc
	return func() tea.Msg {
		res := svc.RemoveAttachment(m.TeamID, ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS), m.AttachmentID)
		if r, ok := res.(AttachmentRemovedMsg); ok {
			r.Seq = seq
			return r
		}
		return res
	}
}
