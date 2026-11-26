// internal/ui/drafts_list.go
//
// The :drafts dialog's glue: rows from draft sync's per-workspace drafts,
// opening, jumping and deleting.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/draftlist"
)

func cmdDrafts(a *App, _ commandArgs) tea.Cmd {
	if a.draftSvc == nil {
		return toastWithClear(a, "Draft sync is off ([compose] sync_drafts)", 3*time.Second)
	}
	a.refreshDraftList()
	a.draftList.Open()
	a.SetMode(ModeDrafts)
	return a.loadDraftsCmd(a.activeTeamID)
}

// refreshDraftList rebuilds the dialog's rows. draftListItems keeps the
// drafts in the order shown, so a key acts on the row it highlights.
func (a *App) refreshDraftList() {
	items := a.drafts.rows[a.activeTeamID]
	a.draftListItems = items
	now := a.now()
	rows := make([]draftlist.Row, 0, len(items))
	for _, d := range items {
		rows = append(rows, draftlist.Row{
			Destination: a.draftLabel(d.Key),
			Age:         draftAge(now, d.Updated),
			Marker:      draftMarker(d),
			Preview:     a.draftRowPreview(d),
		})
	}
	a.draftList.SetRows(rows)
}

// draftRowPreview is a draft's text as the :drafts dialog shows it:
// composer form (mentions as @names, as MentionsForEdit gives back)
// collapsed onto one line. Shared by the list's Preview column and the
// delete-confirm body. Named apart from (*App).draftPreview
// (draft_preview.go), the unrelated ctrl+r compose-box preview.
func (a *App) draftRowPreview(d core.Draft) string {
	return strings.Join(strings.Fields(a.composerFor(d.Key.ThreadTS).MentionsForEdit(d.Text)), " ")
}

// draftLabel names a draft's conversation.
func (a *App) draftLabel(k core.DraftKey) string {
	if k.ChannelID == "" {
		return "(no recipient)"
	}
	label := k.ChannelID
	if name, typ, ok := a.channels.Lookup(ids.ChannelID(k.ChannelID)); ok {
		label = name
		if typ == "channel" || typ == "private" {
			label = "#" + name
		}
	}
	if k.ThreadTS != "" {
		label += " › thread"
	}
	return label
}

func draftAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return t.Format("Jan 2")
}

// draftMarker names a hands-off draft's kind: the design's hands-off
// table (docs/superpowers/specs/2026-11-25-slack-drafts-sync-design.md,
// "Behaviour") lists scheduled, file-attached and composer-saved drafts
// as ones slk will not overwrite; the list shows which kind a row is.
func draftMarker(d core.Draft) string {
	switch {
	case !d.Scheduled.IsZero():
		return "scheduled " + d.Scheduled.Format("Jan 2 3:04 PM")
	case d.FileCount == 1:
		return "1 file"
	case d.FileCount > 1:
		return fmt.Sprintf("%d files", d.FileCount)
	case d.Saved && d.Key.ChannelID != "":
		return "saved"
	}
	return ""
}

func (a *App) jumpToDraft(d core.Draft) tea.Cmd {
	if d.Key.ChannelID == "" {
		return toastWithClear(a, "This draft has no recipient", 2*time.Second)
	}
	loc := Location{TeamID: ids.TeamID(d.Key.TeamID), ChannelID: ids.ChannelID(d.Key.ChannelID)}
	if d.Key.ThreadTS != "" {
		loc.MessageTS = ids.MessageTS(d.Key.ThreadTS)
		loc.ThreadTS = ids.ThreadTS(d.Key.ThreadTS)
	}
	cmd, ok := a.applyLocation(loc, false)
	if !ok {
		return toastWithClear(a, "That conversation is unavailable", 3*time.Second)
	}
	return cmd
}

func (a *App) confirmDeleteDraft(d core.Draft) {
	teamID := a.activeTeamID
	body := a.draftLabel(d.Key) + ": " + a.draftRowPreview(d)
	a.openConfirmPrompt("Delete draft?", body, func() tea.Msg {
		return draftDeleteConfirmedMsg{teamID: teamID, draft: d}
	})
}

// reduceDraftDeleteConfirmed runs the instant the user confirms a
// delete, before deleteDraftCmd's call is even made. touch(key) runs
// for any draft with a conversation key, any kind, for row freshness
// (as it always has: a list issued earlier that still shows this
// conversation's draft must not win over the delete). The generation
// bump and deleting[key] -- which pause the composer's own sync for
// that key -- apply only when the deleted draft is the attached one
// that backs the slot (Writable()): a "Save as draft", scheduled or
// files draft can share a conversation key with the composer's
// separate attached draft, and deleting one of those must not pause
// or cancel syncing the composer's own text. For the attached draft,
// without the bump a save built earlier and still waiting on the
// engine's per-conversation lock could pass current() -- its captured
// generation still matches -- and create a new draft after the delete
// has released that lock and forgotten it; the UI would then have no
// way to tell that save's answer was stale, so Slack would be left
// holding an untracked draft while the user saw "Draft deleted". An
// unaddressed draft (Key.ChannelID == "") has no slot at all.
func (a *App) reduceDraftDeleteConfirmed(m draftDeleteConfirmedMsg) tea.Cmd {
	if k := m.draft.Key; k.ChannelID != "" {
		d := a.drafts
		d.touch(k)
		if m.draft.Writable() {
			d.genPtr(k).Add(1)
			d.deleting[k] = true
		}
	}
	return a.deleteDraftCmd(m.teamID, m.draft)
}

func (a *App) deleteDraftCmd(teamID string, d core.Draft) tea.Cmd {
	svc := a.draftSvc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		return draftDeletedMsg{teamID: teamID, draft: d, err: svc.Delete(teamID, d.ID)}
	}
}

func (a *App) reduceDraftDeleted(m draftDeletedMsg) tea.Cmd {
	d := a.drafts
	if m.draft.Writable() {
		// Only clear deleting if this delete could have been the one
		// that set it (reduceDraftDeleteConfirmed, Writable()-gated):
		// a non-writable draft's delete never set it, and must not
		// clear it out from under the attached draft's OWN delete if
		// the two happen to share a key and are in flight together.
		delete(d.deleting, m.draft.Key)
	}
	if m.err != nil {
		// Leave the slot as it is: the next tick resyncs it if its
		// text still differs from synced.
		return toastWithClear(a, "Couldn't delete draft", 3*time.Second)
	}
	d.removeRow(m.teamID, m.draft.ID)
	// Whatever kind it was, it is no longer here to block sync.
	delete(d.handsOff, m.draft.Key)
	// Freshness, any kind: the key (for a list issued earlier that
	// still shows this conversation's draft) and the id itself (for an
	// unaddressed draft, Key.ChannelID == "", whose key several
	// distinct drafts share -- see deletedAt).
	d.touch(m.draft.Key)
	d.deletedAt[m.draft.ID] = d.seq
	if k := m.draft.Key; m.draft.Writable() {
		// The user asked: the slot goes too, even the visible one.
		// No second gen bump here: reduceDraftDeleteConfirmed already
		// bumped it (and set deleting[k], which syncable excludes),
		// so no save built since could still be holding the old one.
		a.composerFor(k.ThreadTS).ClearSlot(k.TeamID, k.ChannelID, k.ThreadTS)
		delete(d.synced, k)
		delete(d.observed, k)
	}
	a.draftRowsChanged()
	return toastWithClear(a, "Draft deleted", 2*time.Second)
}
