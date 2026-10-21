// internal/ui/reducer_remove_preview.go
//
// The reducer for x's link-preview removal: the prompt's confirmed answer
// (RemovePreviewMsg) and the call's result (AttachmentRemovedMsg). The
// state it reads and writes lives in remove_preview.go.
package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

var reduceRemovePreview reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case RemovePreviewMsg:
		return a.removePreview(m), true

	case AttachmentRemovedMsg:
		r, ok := a.pendingPreviewRemoval(m.ChannelID, m.TS, m.Seq)
		if !ok {
			// The answer of a removal that is over (its echo came first)
			// says nothing about the one in flight now, if any.
			return nil, true
		}
		if m.Err == nil {
			// Slack did it. The message_changed echo shows the result; if
			// it never comes the removal stays confirmed, not expired, so a
			// second x cannot name a renumbered id.
			r.confirmed = true
			a.previewRemovals[previewRemovalKey(m.ChannelID, m.TS)] = r
			return nil, true
		}
		var refused *core.AttachmentRefusedError
		if errors.As(m.Err, &refused) {
			// Certainly not done: the user may try again at once.
			a.clearPreviewRemoval(m.ChannelID, m.TS)
			return toastWithClear(a, "Could not remove the preview: "+refused.Reason, 4*time.Second), true
		}
		// A timeout, a 5xx, a lost or unreadable answer: Slack may still
		// have acted. The removal stays pending until the echo, a reload
		// or the timeout: a resend could hit an id Slack has renumbered.
		if errors.Is(m.Err, context.DeadlineExceeded) {
			return toastWithClear(a, "Removing the preview timed out, check the message", 4*time.Second), true
		}
		return toastWithClear(a, "No clear answer from Slack, check the message", 4*time.Second), true
	}
	return nil, false
}
