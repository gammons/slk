// internal/ui/drafts.go
//
// Draft sync: the composer's text as Slack's own drafts (spec:
// docs/superpowers/specs/2026-11-25-slack-drafts-sync-design.md). Every
// draftTickInterval the App compares each composer slot with the text
// Slack last had from it and saves what changed, through DraftService.
// Comparing, rather than hooking each way text gets into a composer
// (keys, paste, the external editor, emoji, /msg), catches them all.
package ui

import (
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui/compose"
)

// draftTickInterval is how often the comparison runs. The visible slot is
// saved once its text stays the same for a whole interval, 2-4 s after
// the last keystroke. A var so tests may shorten it.
var draftTickInterval = 2 * time.Second

// draftRetryMax caps the backoff of a save that keeps failing.
const draftRetryMax = time.Minute

// DraftsStaleMsg asks the App to reload a workspace's drafts. cmd/slk
// sends it on every connect.
type DraftsStaleMsg struct{ TeamID string }

type (
	draftTickMsg    struct{}
	draftsLoadedMsg struct {
		teamID string
		drafts []core.Draft
		err    error
	}
	// draftSavedMsg answers a Save (text != "") or a Discard (text "").
	draftSavedMsg struct {
		key  core.DraftKey
		text string
		gen  int
		res  core.DraftSaveResult
		err  error
	}
	draftDeleteConfirmedMsg struct {
		teamID string
		draft  core.Draft
	}
	draftDeletedMsg struct {
		teamID string
		draft  core.Draft
		err    error
	}
)

type draftObserved struct {
	text string
	at   time.Time
}

type draftRetry struct {
	n  int
	at time.Time
}

// draftSync is the App's draft sync state. Text is composer form
// (mentions as @names).
type draftSync struct {
	ticking bool
	// loaded: the workspace's drafts were listed at least once. No slot
	// of a workspace is compared before, so slk never creates a draft
	// Slack already has.
	loaded map[string]bool
	// synced is the text Slack last had from slk, per slot.
	synced map[core.DraftKey]string
	// observed is the text the last comparison saw and when it first
	// saw it: the debounce, and the edit time sent to Slack.
	observed map[core.DraftKey]draftObserved
	inflight map[core.DraftKey]string
	// gen is bumped by a send and a delete, so a save answer that was
	// in flight across them is ignored.
	gen   map[core.DraftKey]int
	retry map[core.DraftKey]draftRetry
	// handsOff: Slack's draft here is one slk won't overwrite; the slot
	// is left alone until a list shows a writable draft there.
	handsOff map[core.DraftKey]bool
	// rows is each workspace's active drafts, newest first (the list).
	rows map[string][]core.Draft
}

func newDraftSync() *draftSync {
	return &draftSync{
		loaded:   map[string]bool{},
		synced:   map[core.DraftKey]string{},
		observed: map[core.DraftKey]draftObserved{},
		inflight: map[core.DraftKey]string{},
		gen:      map[core.DraftKey]int{},
		retry:    map[core.DraftKey]draftRetry{},
		handsOff: map[core.DraftKey]bool{},
		rows:     map[string][]core.Draft{},
	}
}

// SetDraftService wires draft sync. nil turns it off: no draft calls,
// drafts stay in memory, :drafts refuses.
func (a *App) SetDraftService(s core.DraftService) { a.draftSvc = s }

// composerFor is the composer holding a conversation's slot: the thread
// composer for a thread, else the channel composer.
func (a *App) composerFor(threadTS string) *compose.Model {
	if threadTS != "" {
		return &a.threadCompose
	}
	return &a.compose
}

// draftText is a slot's text as sync compares it: whitespace-only is
// empty.
func draftText(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

// eachDraftSlot calls fn for every slot of both composers and for every
// slot Slack has text for that the composers no longer hold (emptied and
// left). The composer being edited (E) reports the draft held aside.
func (a *App) eachDraftSlot(fn func(key core.DraftKey, text string, visible bool)) {
	seen := map[core.DraftKey]bool{}
	for _, s := range append(a.compose.Slots(), a.threadCompose.Slots()...) {
		key := core.DraftKey{TeamID: s.TeamID, ChannelID: s.ChannelID, ThreadTS: s.ThreadTS}
		text := s.Text
		if s.Visible && a.editing.IsActive() && (s.ThreadTS != "") == (a.editing.Panel() == PanelThread) {
			text = a.editing.StashedDraft()
		}
		seen[key] = true
		fn(key, draftText(text), s.Visible)
	}
	for key := range a.drafts.synced {
		if !seen[key] {
			fn(key, "", false)
		}
	}
}

// syncable reports a slot the comparison may act on.
func (d *draftSync) syncable(key core.DraftKey) bool {
	return key.TeamID != "" && d.loaded[key.TeamID] && !d.handsOff[key]
}

// syncDrafts runs one comparison at now and returns the saves to make.
func (a *App) syncDrafts(now time.Time) []tea.Cmd {
	if a.draftSvc == nil {
		return nil
	}
	d := a.drafts
	var cmds []tea.Cmd
	a.eachDraftSlot(func(key core.DraftKey, text string, visible bool) {
		if !d.syncable(key) {
			return
		}
		if text == d.synced[key] {
			delete(d.observed, key)
			return
		}
		if _, busy := d.inflight[key]; busy {
			return
		}
		if r, ok := d.retry[key]; ok && now.Before(r.at) {
			return
		}
		obs, ok := d.observed[key]
		if !ok || obs.text != text {
			obs = draftObserved{text: text, at: now}
			d.observed[key] = obs
			if visible {
				return // still being typed: wait for a whole interval
			}
		}
		d.inflight[key] = text
		cmds = append(cmds, a.saveDraftCmd(key, text, obs.at))
	})
	return cmds
}

func (a *App) saveDraftCmd(key core.DraftKey, text string, edited time.Time) tea.Cmd {
	svc, gen := a.draftSvc, a.drafts.gen[key]
	wire := ""
	if text != "" {
		wire = a.composerFor(key.ThreadTS).TranslateMentionsForSend(text)
	}
	return func() tea.Msg {
		if text == "" {
			return draftSavedMsg{key: key, gen: gen, err: svc.Discard(key)}
		}
		res, err := svc.Save(key, wire, edited)
		return draftSavedMsg{key: key, text: text, gen: gen, res: res, err: err}
	}
}

// UnsyncedDrafts is every slot whose text Slack does not have, for the
// save on quit. Text is wire form; "" discards.
func (a *App) UnsyncedDrafts() []core.DraftChange {
	if a.draftSvc == nil {
		return nil
	}
	d, now := a.drafts, a.now()
	var out []core.DraftChange
	a.eachDraftSlot(func(key core.DraftKey, text string, _ bool) {
		if !d.syncable(key) || text == d.synced[key] {
			return
		}
		edited := now
		if obs, ok := d.observed[key]; ok && obs.text == text {
			edited = obs.at
		}
		wire := ""
		if text != "" {
			wire = a.composerFor(key.ThreadTS).TranslateMentionsForSend(text)
		}
		out = append(out, core.DraftChange{Key: key, Text: wire, Edited: edited})
	})
	return out
}

// noteDraftSent records that the composer of key was just sent from: the
// send marks the draft sent, so its now-empty slot is not a discard, and
// a save answer in flight is ignored.
func (a *App) noteDraftSent(key core.DraftKey) {
	d := a.drafts
	d.gen[key]++
	delete(d.synced, key)
	delete(d.observed, key)
	d.removeRowsFor(key)
}

func (a *App) startDraftTick() tea.Cmd {
	if a.drafts.ticking {
		return nil
	}
	a.drafts.ticking = true
	return draftTickCmd()
}

func draftTickCmd() tea.Cmd {
	return tea.Tick(draftTickInterval, func(time.Time) tea.Msg { return draftTickMsg{} })
}

func (a *App) reduceDraftTick() tea.Cmd {
	cmds := a.syncDrafts(a.now())
	return tea.Batch(append(cmds, draftTickCmd())...)
}

func (a *App) loadDraftsCmd(teamID string) tea.Cmd {
	svc := a.draftSvc
	if svc == nil || teamID == "" {
		return nil
	}
	return func() tea.Msg {
		ds, err := svc.Load(teamID)
		return draftsLoadedMsg{teamID: teamID, drafts: ds, err: err}
	}
}

func (a *App) reduceDraftsLoaded(m draftsLoadedMsg) tea.Cmd {
	if m.err != nil {
		debuglog.General("drafts: load %s failed: %v", m.teamID, m.err)
		return nil
	}
	d := a.drafts
	d.loaded[m.teamID] = true
	d.rows[m.teamID] = sortDrafts(m.drafts)
	listed := map[core.DraftKey]bool{}
	for _, dr := range m.drafts {
		if !dr.Writable() {
			continue
		}
		listed[dr.Key] = true
		delete(d.handsOff, dr.Key)
		a.restoreRemoteDraft(dr)
	}
	for key, synced := range d.synced {
		if key.TeamID != m.teamID || listed[key] {
			continue
		}
		if _, busy := d.inflight[key]; busy {
			continue
		}
		c := a.composerFor(key.ThreadTS)
		cur, visible := c.SlotText(key.TeamID, key.ChannelID, key.ThreadTS)
		if visible || draftText(cur) != synced {
			continue
		}
		c.ApplyRemoteDraft(key.TeamID, key.ChannelID, key.ThreadTS, "")
		delete(d.synced, key)
		delete(d.observed, key)
	}
	a.draftRowsChanged()
	return a.startDraftTick()
}

// restoreRemoteDraft puts a writable draft from Slack into its slot when
// the user has typed nothing there since the last sync. The visible
// composer only takes it when empty (compose.ApplyRemoteDraft).
func (a *App) restoreRemoteDraft(dr core.Draft) {
	d, key := a.drafts, dr.Key
	if _, busy := d.inflight[key]; busy {
		return
	}
	c := a.composerFor(key.ThreadTS)
	text := draftText(c.MentionsForEdit(dr.Text))
	cur, visible := c.SlotText(key.TeamID, key.ChannelID, key.ThreadTS)
	if draftText(cur) != d.synced[key] {
		return // typed since: slk's text wins and is saved by the tick
	}
	if visible && a.editing.IsActive() && (key.ThreadTS != "") == (a.editing.Panel() == PanelThread) {
		return
	}
	if draftText(cur) == text || c.ApplyRemoteDraft(key.TeamID, key.ChannelID, key.ThreadTS, text) {
		d.synced[key] = text
		delete(d.observed, key)
	}
}

func (a *App) reduceDraftSaved(m draftSavedMsg) tea.Cmd {
	d := a.drafts
	delete(d.inflight, m.key)
	if m.gen != d.gen[m.key] {
		return nil // sent or deleted while this was in flight
	}
	switch {
	case m.err == nil:
		delete(d.retry, m.key)
		if m.text == "" {
			delete(d.synced, m.key)
			d.removeRowsFor(m.key)
		} else {
			d.synced[m.key] = m.text
			d.upsertRow(m.res.Draft)
		}
		a.draftRowsChanged()
		if m.res.Replaced {
			return toastWithClear(a, "Replaced a newer draft from another device", 3*time.Second)
		}
	case errors.Is(m.err, core.ErrDraftHandsOff):
		d.handsOff[m.key] = true
		return toastWithClear(a, "Slack already has a draft here that slk won't overwrite", 4*time.Second)
	default:
		r := d.retry[m.key]
		r.n++
		wait := min(draftTickInterval<<r.n, draftRetryMax)
		r.at = a.now().Add(wait)
		d.retry[m.key] = r
		debuglog.General("drafts: save %v failed (retry in %s): %v", m.key, wait, m.err)
	}
	return nil
}

// sortDrafts returns ds newest first.
func sortDrafts(ds []core.Draft) []core.Draft {
	out := slices.Clone(ds)
	slices.SortStableFunc(out, func(x, y core.Draft) int { return y.Updated.Compare(x.Updated) })
	return out
}

// upsertRow puts a saved writable draft into its workspace's rows,
// replacing the conversation's previous one.
func (d *draftSync) upsertRow(dr core.Draft) {
	if dr.ID == "" {
		return
	}
	rows := d.rows[dr.Key.TeamID]
	rows = slices.DeleteFunc(slices.Clone(rows), func(r core.Draft) bool {
		return r.ID == dr.ID || (r.Writable() && r.Key == dr.Key)
	})
	d.rows[dr.Key.TeamID] = sortDrafts(append(rows, dr))
}

// removeRowsFor drops a conversation's writable draft from the rows.
func (d *draftSync) removeRowsFor(key core.DraftKey) {
	d.rows[key.TeamID] = slices.DeleteFunc(slices.Clone(d.rows[key.TeamID]), func(r core.Draft) bool {
		return r.Writable() && r.Key == key
	})
}

func (d *draftSync) removeRow(teamID, id string) {
	d.rows[teamID] = slices.DeleteFunc(slices.Clone(d.rows[teamID]), func(r core.Draft) bool { return r.ID == id })
}

// draftRowsChanged redraws the Drafts list when it is open. Task 7
// gives it a body; until then the list does not exist.
func (a *App) draftRowsChanged() {}
