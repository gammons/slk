// internal/ui/reducer_io.go
//
// IO / toast / asset-loading reducer for App.Update (Phase 4l).
//
// Owns the leftover arms that don't belong to any domain reducer:
//
//	tea.PasteMsg              - bracketed paste from the terminal.
//	                             Try clipboard image / file path
//	                             first, else forward to the
//	                             focused compose's textarea.
//	UploadProgressMsg         - in-flight upload progress toast.
//	UploadResultMsg           - upload finished: clear compose
//	                            attachments + Sent/Failed toast.
//	EditorFinishedMsg         - Ctrl+E external-editor session ended:
//	                            read the temp file back into the
//	                            compose it came from, then remove it.
//	ConnectionStateMsg        - WS connection state changed:
//	                            push to status bar.
//	ToastMsg                  - generic toast (3s auto-clear).
//	editEmptyToastMsg         - "Edit must have text" toast.
//
//	imgrender.ImageReadyMsg   - lazy attachment-image fetch
//	                            landed: invalidate the affected
//	                            render caches.
//	imgrender.ImageFailedMsg  - lazy attachment-image fetch
//	                            permanently failed: clear
//	                            in-flight bookkeeping.
//	messages.AvatarReadyMsg   - lazy avatar fetch landed:
//	                            invalidate both pane caches.
//
//	statusbar.CopiedMsg               - "Copied N chars"
//	statusbar.CopiedClearMsg          - 2/3s expiry tick
//	statusbar.PermalinkCopiedMsg      - "Copied permalink"
//	statusbar.PermalinkCopyFailedMsg  - "Failed to copy link"
//	statusbar.MarkedUnreadMsg         - "Marked unread"
//	statusbar.MarkUnreadFailedMsg     - "Mark unread failed: ..."
//	statusbar.EditFailedMsg           - "Edit failed: ..."
//	statusbar.EditNotOwnMsg           - "Can only edit your own..."
//	statusbar.DeleteFailedMsg         - "Delete failed: ..."
//	statusbar.DeleteNotOwnMsg         - "Can only delete your own..."
//	statusbar.SendFailedMsg           - "Send failed: ..."
//
// Free reducer: these arms have no shared domain or invariant,
// only the common "push to status bar / clear after N seconds"
// shape. Grouping them here keeps the residual Update switch
// near-empty.
//
// Every toast goes through one of two helpers (the toast rule in
// AGENTS.md): toastWithClear shows a toast and schedules its clear
// (copiedClearAfter), and toastUntilReplaced shows one without a generic
// expiry, for progress or when its owning reducer schedules a custom clear.
// toastWithClear also collapses the `cmds = append(cmds, tea.Tick(Ns, ... CopiedClearMsg))`
// idiom that recurred ~11 times in the original switch.
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// toastTick schedules a toast's clear. A variable so a test can swap in a
// tick that never fires and read the toast, instead of waiting out a real
// timer.
var toastTick = tea.Tick

// copiedClearAfter schedules a CopiedClearMsg `d` from now for the
// toast now in the status bar, so call it right after setting that
// toast. The CopiedClearMsg handler clears the slot only if no other
// toast replaced this one in the meantime (statusbar ToastSeq).
func copiedClearAfter(a *App, d time.Duration) tea.Cmd {
	seq := a.statusbar.ToastSeq()
	return toastTick(d, func(time.Time) tea.Msg {
		return statusbar.CopiedClearMsg{Seq: seq}
	})
}

// emojiInvalidateDebounce is the coalesce window for EmojiImageReadyMsg
// arrivals. Chosen to absorb the typical burst of fetch completions on
// a busy channel's first render (50+ unique emoji finishing within tens
// of milliseconds of each other) into a single cache rebuild. Tuned for
// "imperceptibly delayed" rather than "instantaneous": a 100ms wait
// before image emoji appear is well within the budget where a user
// would not notice, while the savings from coalescing N rebuilds into
// 1 are dramatic.
const emojiInvalidateDebounce = 100 * time.Millisecond

// toastWithClear pushes text into the status bar's toast slot and
// schedules the clear after `d`. It is the path for every toast that
// expires: it sets the toast on the Update goroutine, never inside a
// tea.Cmd, and ties the clear to that toast.
func toastWithClear(a *App, text string, d time.Duration) tea.Cmd {
	a.statusbar.SetToast(text)
	return copiedClearAfter(a, d)
}

// toastUntilReplaced shows a toast without a generic expiry. The owning
// reducer must replace it or schedule its own clear, as for progress and
// queued download-failure notifications.
func toastUntilReplaced(a *App, text string) {
	a.statusbar.SetToast(text)
}

var reduceIO reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.PasteMsg:
		return reducePaste(a, m), true

	case statusbar.CopiedMsg:
		// No toast means no clear: a clear scheduled now would belong
		// to the toast on screen, such as an upload's progress.
		if m.N <= 0 {
			return nil, true
		}
		return toastWithClear(a, fmt.Sprintf("Copied %d chars", m.N), 2*time.Second), true

	case statusbar.CopiedClearMsg:
		if m.Seq == a.statusbar.ToastSeq() {
			a.statusbar.ClearCopied()
		}
		return nil, true

	case statusbar.PermalinkCopiedMsg:
		_ = m
		return toastWithClear(a, "Copied permalink", 2*time.Second), true

	case statusbar.PermalinkCopyFailedMsg:
		_ = m
		return toastWithClear(a, "Failed to copy link", 2*time.Second), true

	case statusbar.MarkedUnreadMsg:
		_ = m
		return toastWithClear(a, "Marked unread", 2*time.Second), true

	case statusbar.MarkUnreadFailedMsg:
		return toastWithClear(a, "Mark unread failed: "+truncateReason(m.Reason, 40), 3*time.Second), true

	case statusbar.ThreadSavedMsg:
		return toastWithClear(a, "Saved "+filepath.Base(m.Path), 2*time.Second), true

	case statusbar.ThreadSaveFailedMsg:
		return toastWithClear(a, "Save failed: "+truncateReason(m.Reason, 40), 3*time.Second), true

	case statusbar.EditFailedMsg:
		return toastWithClear(a, "Edit failed: "+truncateReason(m.Reason, 40), 3*time.Second), true

	case editEmptyToastMsg:
		_ = m
		return toastWithClear(a, "Edit must have text (use D to delete)", 3*time.Second), true

	case statusbar.DeleteFailedMsg:
		return toastWithClear(a, "Delete failed: "+truncateReason(m.Reason, 40), 3*time.Second), true

	case statusbar.SendFailedMsg:
		return toastWithClear(a, "Send failed: "+truncateReason(m.Reason, 40), 3*time.Second), true

	case statusbar.EditNotOwnMsg:
		_ = m
		return toastWithClear(a, "Can only edit your own messages", 2*time.Second), true

	case statusbar.DeleteNotOwnMsg:
		_ = m
		return toastWithClear(a, "Can only delete your own messages", 2*time.Second), true

	case ToastMsg:
		return toastWithClear(a, m.Text, 3*time.Second), true

	case UploadProgressMsg:
		toastUntilReplaced(a, fmt.Sprintf("Uploading %d/%d…", m.Done, m.Total))
		return nil, true

	case EditorFinishedMsg:
		return reduceEditorFinished(a, m), true

	case UploadResultMsg:
		// Target from the in-flight flags, not focusedPanel: focus may
		// have moved while the batch uploaded. submitWithAttachments
		// prevents overlapping uploads, so at most one flag is set.
		mainUploading := a.compose.Uploading()
		threadUploading := a.threadCompose.Uploading()
		if !mainUploading && !threadUploading {
			// Stale result: nothing in flight, so nothing to clear.
			return nil, true
		}
		if mainUploading {
			a.compose.SetUploading(false)
		}
		if threadUploading {
			a.threadCompose.SetUploading(false)
		}
		if m.Err != nil {
			// Failure preserves the source caption + attachments so
			// the user can retry; only the in-flight flag is cleared.
			if a.threadCloseAfterUpload {
				a.CloseThread()
			}
			return toastWithClear(a,
				"Upload failed: "+truncateReason(m.Err.Error(), 40),
				3*time.Second,
			), true
		}
		// Success resets only the source composer.
		if mainUploading {
			a.compose.Reset()
		}
		if threadUploading {
			a.threadCompose.Reset()
		}
		// After the reset, so a sent thread caption isn't stored as a
		// draft for the deleted thread.
		if a.threadCloseAfterUpload {
			a.CloseThread()
		}
		return toastWithClear(a, "Sent", 2*time.Second), true

	case ConnectionStateMsg:
		a.statusbar.SetConnectionState(statusbar.ConnectionState(m.State))
		return nil, true

	case imgrender.ImageReadyMsg:
		debuglog.ImgFetch("recv: kind=ready channel=%s ts=%s key=%s req_id=%d",
			m.Channel, m.TS, m.Key, m.ReqID)
		// Image attachment finished downloading; invalidate the
		// messages pane's render cache for the affected channel
		// so the next View() picks up the cached bytes inline.
		// Only the specific key's in-flight bit is cleared so
		// sibling images that are still mid-fetch don't trigger
		// fresh respawns. Fan out to every window: each model
		// self-gates by its own channel name (no-op for windows
		// viewing other channels).
		for _, mp := range a.allWinModels() {
			mp.HandleImageReady(m.Channel, m.TS, m.Key)
		}
		// Thread panel: coarse cache invalidation. If the open thread
		// draws this message — as a reply or as its parent — blow the
		// thread cache so the renderer runs again with the now-cached
		// image bytes. threadShowsMessage guards against churning the
		// thread cache on every messages-pane image arrival.
		if a.threadShowsMessage(m.Channel, m.TS) {
			a.threadPanel.InvalidateCache()
		}
		return nil, true

	case blockkit.BlockImageReadyMsg:
		// A Block Kit or link-preview image landed. Same per-message
		// invalidation as an attachment image; the URL stands in for
		// the key (these fetches are not tracked by imgrender, so
		// clearing it is a no-op).
		for _, mp := range a.allWinModels() {
			mp.HandleImageReady(m.Channel, m.TS, m.URL)
		}
		// By TS alone, deliberately: m.Channel is not comparable with
		// the thread's channel ID. See threadShowsTS.
		if a.threadShowsTS(m.TS) {
			a.threadPanel.InvalidateCache()
		}
		return nil, true

	case EmojiImageReadyMsg:
		debuglog.ImgFetch("recv: kind=emoji-ready url=%s pending=%v", m.URL, a.emojiInvalidatePending)
		// An emoji-image fetch landed. Naively each arrival would
		// trigger a full cache rebuild across every emoji-rendering
		// surface (messages, thread, picker). On a busy channel with
		// many cold-cache emoji this cascades into seconds of UI-
		// thread saturation — looks like a freeze. Debounce: schedule
		// one tick on the first arrival; absorb every subsequent
		// arrival into the pending batch and let them collapse to a
		// single invalidation when the tick fires.
		if a.emojiInvalidatePending {
			return nil, true
		}
		a.emojiInvalidatePending = true
		return tea.Tick(emojiInvalidateDebounce, func(time.Time) tea.Msg {
			return emojiInvalidateMsg{}
		}), true

	case emojiInvalidateMsg:
		_ = m
		// Debounce window closed. One wholesale invalidation across
		// every emoji-rendering surface; arrivals accumulated during
		// the window collapse to this. The URL argument is unused —
		// the surface handlers wipe their caches wholesale regardless
		// of URL in v1.
		a.emojiInvalidatePending = false
		for _, mp := range a.allWinModels() {
			mp.HandleEmojiImageReady("")
		}
		a.threadPanel.HandleEmojiImageReady("")
		a.sidebar.HandleEmojiImageReady("")
		a.reactionPicker.HandleEmojiImageReady("") // no-op in v1; future caching may use it
		// Autocomplete dropdowns have no cache; the no-op hooks on
		// a.compose.emojiPicker / a.threadCompose.emojiPicker keep
		// the surface symmetric. Listed here for the audit trail.
		return nil, true

	case messages.AvatarReadyMsg:
		// A lazy avatar fetch landed for m.UserID. Both the
		// messages pane and the thread panel cache avatar slots
		// in their render caches, so both must invalidate. The
		// handlers no-op when the userID isn't in their current
		// view, but coarse invalidation is cheap relative to the
		// cost of a missing avatar.
		for _, mp := range a.allWinModels() {
			mp.HandleAvatarReady(m.UserID)
		}
		a.threadPanel.HandleAvatarReady(m.UserID)
		return nil, true

	case imgrender.ImageFailedMsg:
		debuglog.ImgFetch("recv: kind=failed key=%s req_id=%d", m.Key, m.ReqID)
		// Image attachment fetch hit a permanent failure (all
		// auths exhausted, or some other terminal error). Clear
		// the in-flight bit so a future cache invalidation
		// doesn't keep retrying; don't trigger a re-render --
		// the placeholder is already on screen and we have no
		// new bytes to show.
		for _, mp := range a.allWinModels() {
			mp.HandleImageFailed(m.Key)
		}
		// Mirror the in-flight bookkeeping on the thread panel so
		// a permanently-failed image isn't re-attempted from the
		// thread.
		a.threadPanel.HandleImageFailed(m.Key)
		return nil, true
	}
	return nil, false
}

// reducePaste handles tea.PasteMsg. Extracted because the arm
// does three things: insert-mode gate, clipboard-image
// hit-test, and compose-textarea forward.
func reducePaste(a *App, m tea.PasteMsg) tea.Cmd {
	// Bracketed-paste from the terminal. First check the OS
	// clipboard for an image (terminals can't deliver image bytes
	// via bracketed paste -- only the text representation -- so
	// the image data is still sitting in the clipboard waiting
	// for us to read directly). Also test the bracketed text as a
	// file path. If neither matches, fall through to forwarding
	// the paste verbatim into the active compose's textarea.
	if a.mode == ModeSearch {
		// Paste into the `/` prompt: the prompt is single-line, so
		// flatten any newlines to spaces and append.
		txt := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(m.Content)
		a.searchInput += txt
		a.statusbar.SetSearch("/" + a.searchInput)
		return nil
	}
	if a.mode != ModeInsert {
		return nil
	}
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		// Same in-flight draft invariant as handleInsertMode: a paste
		// must not alter the uploading composer's caption/attachments.
		return toastWithClear(a, "Upload in progress", 2*time.Second)
	}
	if a.clipboardAvailable {
		target := &a.compose
		if a.focusedPanel == PanelThread && a.threadVisible {
			target = &a.threadCompose
		}
		if consumed, cmd := a.tryAttachFromClipboard(target, m.Content); consumed {
			return cmd
		}
	}
	if a.focusedPanel == PanelThread && a.threadVisible {
		var cmd tea.Cmd
		a.threadCompose, cmd = a.threadCompose.Update(m)
		return cmd
	}
	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(m)
	return cmd
}

// threadShowsMessage reports whether the open thread pane draws the
// message (channel, ts) — as a reply or as the thread parent — so an
// image that lands for it needs the thread's render cache dropped.
// Only for imgrender.ImageReadyMsg: each pane owns its own
// imgrender.Renderer, so the thread's own fetch always reports the
// channel ID this compares against. A Block Kit or link-preview image
// is threadShowsTS instead.
//
// The two image-ready arms above used threadPanel.HasReply alone, which
// only indexes replies: a Block Kit, link-preview or attachment image on
// an open thread's PARENT kept its loading placeholder until something
// else redrew the thread. The channel comparison is the other half:
// HasReply matched a bare TS, so an image arriving for another channel
// could drop an unrelated thread's cache.
func (a *App) threadShowsMessage(channel, ts string) bool {
	if a.threadPanel == nil || a.threadPanel.ChannelID() != channel {
		return false
	}
	return a.threadShowsTS(ts)
}

// threadShowsTS is threadShowsMessage without the channel comparison,
// for blockkit.BlockImageReadyMsg. Its Channel is not comparable with
// the thread's channel ID: the two panes key image fetches differently
// (the channel pane stamps the channel NAME, the thread pane the ID —
// plan decision P3), and blockkit dedupes in-flight fetches by URL
// across every pane, so whichever pane asks first sends the only
// message. A Block Kit image on a thread parent that the channel pane
// also draws — a permalink jump opening both panes, say — therefore
// arrives stamped with the name, and comparing it against the ID left
// the placeholder up.
//
// Matching on the TS alone can only over-invalidate, which costs one
// redundant thread render, and needs two channels to share a TS to do
// even that. Unifying the two keys is the fix that would let the
// comparison back in; it changes how the channel pane's windows
// self-gate in HandleImageReady, so it is not this change.
func (a *App) threadShowsTS(ts string) bool {
	if a.threadPanel == nil {
		return false
	}
	if a.threadPanel.HasReply(ts) {
		return true
	}
	return a.threadPanel.ParentMsg().TS == ts
}
