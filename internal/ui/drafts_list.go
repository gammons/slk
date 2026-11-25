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
			Preview:     strings.Join(strings.Fields(a.composerFor(d.Key.ThreadTS).MentionsForEdit(d.Text)), " "),
		})
	}
	a.draftList.SetRows(rows)
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

// draftMarker names a hands-off draft's kind (spec, Behaviour).
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
	body := a.draftLabel(d.Key) + ": " + strings.Join(strings.Fields(a.composerFor(d.Key.ThreadTS).MentionsForEdit(d.Text)), " ")
	a.openConfirmPrompt("Delete draft?", body, func() tea.Msg {
		return draftDeleteConfirmedMsg{teamID: teamID, draft: d}
	})
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
	if m.err != nil {
		return toastWithClear(a, "Couldn't delete draft", 3*time.Second)
	}
	a.drafts.removeRow(m.teamID, m.draft.ID)
	if k := m.draft.Key; m.draft.Writable() {
		// The user asked: the slot goes too, even the visible one.
		a.composerFor(k.ThreadTS).ClearSlot(k.TeamID, k.ChannelID, k.ThreadTS)
		a.drafts.gen[k]++
		delete(a.drafts.synced, k)
		delete(a.drafts.observed, k)
	}
	a.draftRowsChanged()
	return toastWithClear(a, "Draft deleted", 2*time.Second)
}
