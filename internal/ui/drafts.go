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
	"sync/atomic"
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
		// issued is d.seq at the moment the load was issued (see
		// loadDraftsCmd); a key touched since then wins over this
		// list (see reduceDraftsLoaded, finding 3).
		issued uint64
	}
	// draftSavedMsg answers a Save (text != "") or a Discard (text "").
	draftSavedMsg struct {
		key  core.DraftKey
		text string
		gen  uint64
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

// draftWireEntry is the last composer text seen for a slot of a
// workspace that is not currently active, and the wire form it
// translated to while that workspace was last active.
type draftWireEntry struct {
	text, wire string
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
	// wire holds the last translation made for a slot while its own
	// workspace was active, so a slot of a workspace that is not
	// active right now can still be saved correctly (see draftWire).
	wire map[core.DraftKey]draftWireEntry
	// gen is bumped by a send and a delete, so a save answer that was
	// in flight across them is ignored. Each pointer is created once
	// (genPtr) and never replaced, so a saveDraftCmd closure can read
	// it long after the comparison that built it, from any goroutine,
	// without a map read racing a concurrent map write: only the
	// Update goroutine writes the map itself, via genPtr.
	gen   map[core.DraftKey]*atomic.Uint64
	retry map[core.DraftKey]draftRetry
	// handsOff: Slack's draft here is one slk won't overwrite; the slot
	// is left alone until a list shows a writable draft there.
	handsOff map[core.DraftKey]bool
	// rows is each workspace's active drafts, newest first (the list).
	rows map[string][]core.Draft
	// seq and touched track local freshness (finding 3): a list answer
	// can be in flight for seconds, during which a save answer, a
	// send, a list delete or a vanish-clear can change a key more
	// recently than the list reflects. seq is bumped and touched[key]
	// set to the new value on each of those; a list captures d.seq as
	// "issued" when it is requested (loadDraftsCmd) and, on arrival,
	// skips restore/vanish/the hands-off clear and keeps the current
	// rows for any key touched since (touched[key] > issued).
	seq     uint64
	touched map[core.DraftKey]uint64
}

// touch bumps seq and records it against key, marking a local change
// more recent than any list issued before now.
func (d *draftSync) touch(key core.DraftKey) {
	d.seq++
	d.touched[key] = d.seq
}

func newDraftSync() *draftSync {
	return &draftSync{
		loaded:   map[string]bool{},
		synced:   map[core.DraftKey]string{},
		observed: map[core.DraftKey]draftObserved{},
		inflight: map[core.DraftKey]string{},
		wire:     map[core.DraftKey]draftWireEntry{},
		gen:      map[core.DraftKey]*atomic.Uint64{},
		retry:    map[core.DraftKey]draftRetry{},
		handsOff: map[core.DraftKey]bool{},
		rows:     map[string][]core.Draft{},
		touched:  map[core.DraftKey]uint64{},
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

// eachDraftSlot calls fn for every slot of both composers, for every
// slot Slack has text for that the composers no longer hold (emptied and
// left), and for every slot with a save in flight that neither of those
// covers (a key whose first-ever save -- never synced -- is still
// outstanding when its slot is emptied and left). The composer being
// edited (E) reports the draft held aside.
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
			seen[key] = true
			fn(key, "", false)
		}
	}
	for key := range a.drafts.inflight {
		if !seen[key] {
			seen[key] = true
			fn(key, "", false)
		}
	}
}

// syncable reports a slot the comparison may act on.
func (d *draftSync) syncable(key core.DraftKey) bool {
	return key.TeamID != "" && d.loaded[key.TeamID] && !d.handsOff[key]
}

// genPtr returns key's generation counter, creating it on first use.
// Called only from the Update goroutine; the returned pointer is safe
// to read and bump from any goroutine afterwards.
func (d *draftSync) genPtr(key core.DraftKey) *atomic.Uint64 {
	p, ok := d.gen[key]
	if !ok {
		p = &atomic.Uint64{}
		d.gen[key] = p
	}
	return p
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
		wire, ok := a.draftWire(key, text)
		if !ok {
			return // not translatable until that workspace is active again
		}
		d.inflight[key] = text
		cmds = append(cmds, a.saveDraftCmd(key, text, wire, obs.at))
	})
	return cmds
}

// draftWire is the wire form (mentions as <@U…>) of a slot's text, for
// saving or discarding it. A discard ("") never needs translation. For
// a slot of the active workspace it translates now with that
// workspace's own mention data and remembers the result, so a later
// call for the same text -- once another workspace is active -- can
// still answer correctly (ok=true) instead of translating with the
// wrong workspace's names. For a slot of an inactive workspace it
// answers only from that remembered translation, when the text still
// matches what produced it; otherwise ok is false (not yet
// translatable -- the caller retries once that workspace is active
// again, see rememberDraftWire).
func (a *App) draftWire(key core.DraftKey, text string) (wire string, ok bool) {
	if text == "" {
		return "", true
	}
	d := a.drafts
	if key.TeamID == a.activeTeamID {
		wire = a.composerFor(key.ThreadTS).TranslateMentionsForSend(text)
		d.wire[key] = draftWireEntry{text: text, wire: wire}
		return wire, true
	}
	if e, ok := d.wire[key]; ok && e.text == text {
		return e.wire, true
	}
	return "", false
}

// rememberDraftWire records the wire form of every unsynced slot of
// outgoingTeamID. Call it before a workspace switch replaces the
// composers' mention data (channels/users), while outgoingTeamID is
// still the active workspace, so the next sync comparison can still
// save or discard those slots correctly with the names they were typed
// against, once that workspace is active again.
func (a *App) rememberDraftWire(outgoingTeamID string) {
	if outgoingTeamID == "" {
		return
	}
	a.eachDraftSlot(func(key core.DraftKey, text string, _ bool) {
		if key.TeamID != outgoingTeamID || text == "" {
			return
		}
		a.drafts.wire[key] = draftWireEntry{text: text, wire: a.composerFor(key.ThreadTS).TranslateMentionsForSend(text)}
	})
}

// saveDraftCmd builds the save/discard call for key. The generation
// pointer is read now (g) and again inside current, from whichever
// goroutine runs the returned command: if a send or delete for key
// bumps the pointer before the engine takes key's lock, current
// reports false and the engine makes no API call (core.ErrDraftStale),
// so a save built before a send can never recreate the draft the send
// just marked sent (finding 2).
func (a *App) saveDraftCmd(key core.DraftKey, text, wire string, edited time.Time) tea.Cmd {
	svc := a.draftSvc
	ptr := a.drafts.genPtr(key)
	g := ptr.Load()
	current := func() bool { return ptr.Load() == g }
	return func() tea.Msg {
		if text == "" {
			return draftSavedMsg{key: key, gen: g, err: svc.Discard(key)}
		}
		res, err := svc.Save(key, wire, edited, current)
		return draftSavedMsg{key: key, text: text, gen: g, res: res, err: err}
	}
}

// UnsyncedDrafts is every slot whose text Slack does not have, for the
// save on quit, plus any slot whose text Slack is still mid-save on
// with different text than what is current now (save A, start saving
// B, change back to A before quitting: text equals synced ("A"), but
// the in-flight save for B must not be left as Slack's last word, so
// quit saves A again over it). Text is wire form; "" discards.
func (a *App) UnsyncedDrafts() []core.DraftChange {
	if a.draftSvc == nil {
		return nil
	}
	d, now := a.drafts, a.now()
	var out []core.DraftChange
	a.eachDraftSlot(func(key core.DraftKey, text string, _ bool) {
		inflight, busy := d.inflight[key]
		if !d.syncable(key) || (text == d.synced[key] && !(busy && inflight != text)) {
			return
		}
		wire, ok := a.draftWire(key, text)
		if !ok {
			return
		}
		edited := now
		if obs, ok := d.observed[key]; ok && obs.text == text {
			edited = obs.at
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
	d.genPtr(key).Add(1)
	delete(d.synced, key)
	delete(d.observed, key)
	d.removeRowsFor(key)
	d.touch(key)
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
	issued := a.drafts.seq
	return func() tea.Msg {
		ds, err := svc.Load(teamID)
		return draftsLoadedMsg{teamID: teamID, drafts: ds, err: err, issued: issued}
	}
}

func (a *App) reduceDraftsLoaded(m draftsLoadedMsg) tea.Cmd {
	if m.err != nil {
		debuglog.General("drafts: load %s failed: %v", m.teamID, m.err)
		return nil
	}
	d := a.drafts
	d.loaded[m.teamID] = true
	// A key touched (saved, sent, deleted or vanish-cleared) since this
	// list was issued is more current than the list: keep its current
	// row instead of the list's (present or absent), so a stale list
	// cannot clear an inactive slot that was saved during the request
	// or bring a sent or deleted draft's row back.
	d.rows[m.teamID] = mergeDraftRows(d.rows[m.teamID], sortDrafts(m.drafts), d.touched, m.issued)
	if m.teamID != a.activeTeamID {
		// Restore and vanish-clear read/write the active composers,
		// whose mention data belongs to a.activeTeamID; applying
		// another workspace's list there would restore or clear text
		// with the wrong workspace's names. The list is still kept
		// (loaded + rows above): switching to this workspace re-issues
		// loadDraftsCmd, and that load applies it with the right names.
		a.draftRowsChanged()
		return a.startDraftTick()
	}
	listed := map[core.DraftKey]bool{}
	// blocking: keys the list still shows a non-writable draft at. A
	// list is a full snapshot, so any other hands-off key of this team
	// has lost its blocker (deleted here or elsewhere) and resumes sync.
	blocking := map[core.DraftKey]bool{}
	for _, dr := range m.drafts {
		if !dr.Writable() {
			blocking[dr.Key] = true
			continue
		}
		listed[dr.Key] = true
		if d.touched[dr.Key] > m.issued {
			continue // a local change since this list was issued wins
		}
		a.restoreRemoteDraft(dr)
	}
	for key := range d.handsOff {
		if key.TeamID == m.teamID && !blocking[key] && d.touched[key] <= m.issued {
			delete(d.handsOff, key)
		}
	}
	for key, synced := range d.synced {
		if key.TeamID != m.teamID || listed[key] || d.touched[key] > m.issued {
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
		d.touch(key)
	}
	a.draftRowsChanged()
	return a.startDraftTick()
}

// mergeDraftRows combines a workspace's current rows with a freshly
// listed set for reduceDraftsLoaded: for any key touched since the
// list was issued (touched[key] > issued), the current rows' writable
// entry for that key -- present or absent -- wins over the list's.
func mergeDraftRows(current, fresh []core.Draft, touched map[core.DraftKey]uint64, issued uint64) []core.Draft {
	stale := map[core.DraftKey]bool{}
	for key, seq := range touched {
		if seq > issued {
			stale[key] = true
		}
	}
	if len(stale) == 0 {
		return fresh
	}
	out := slices.DeleteFunc(slices.Clone(fresh), func(r core.Draft) bool {
		return r.Writable() && stale[r.Key]
	})
	for _, r := range current {
		if r.Writable() && stale[r.Key] {
			out = append(out, r)
		}
	}
	return sortDrafts(out)
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
	if m.gen != d.genPtr(m.key).Load() {
		return nil // sent or deleted while this was in flight
	}
	if errors.Is(m.err, core.ErrDraftStale) {
		return nil // the engine itself refused for the same reason: nothing else to do
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
		d.touch(m.key)
		a.draftRowsChanged()
		if m.res.Replaced {
			return toastWithClear(a, "Replaced a newer draft from another device", 3*time.Second)
		}
	case errors.Is(m.err, core.ErrDraftHandsOff):
		d.handsOff[m.key] = true
		d.touch(m.key)
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

// draftRowsChanged redraws the Drafts list when it is open.
func (a *App) draftRowsChanged() {
	if a.draftList.IsVisible() {
		a.refreshDraftList()
	}
}
