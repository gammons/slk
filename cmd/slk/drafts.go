package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/export"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// draftAPI is the part of *slackclient.Client the draft engine uses.
type draftAPI interface {
	ListDrafts(ctx context.Context) ([]slackclient.Draft, error)
	CreateDraft(ctx context.Context, w slackclient.DraftWrite) (slackclient.Draft, error)
	UpdateDraft(ctx context.Context, draftID string, w slackclient.DraftWrite) (slackclient.Draft, error)
	DeleteDraft(ctx context.Context, draftID string) error
}

// draftDest is a conversation within the engine's workspace.
type draftDest struct{ channelID, threadTS string }

// draftEntry is what the engine knows of a conversation's attached draft.
type draftEntry struct {
	id            string
	lastUpdatedTS string
	// handsOff: scheduled or with files; slk never writes it.
	handsOff bool
}

// draftEngine is one workspace's side of draft sync: which attached
// draft each conversation has, whether to create or update it, and one
// Slack call at a time per conversation, so saves and the send that
// marks a draft sent never overlap or reorder.
type draftEngine struct {
	api    draftAPI
	teamID string
	now    func() time.Time

	mu    sync.Mutex
	index map[draftDest]draftEntry
	// locks: a 1-slot semaphore per conversation, so lock can give up on
	// a cancelled/expired ctx instead of blocking on a plain Mutex.Lock.
	locks map[draftDest]chan struct{}
	busy  map[draftDest]int
	// seq and touched record write order: touched[dest] is the seq at
	// which dest's index entry was last written (put, drop or
	// markHandsOff). reload uses this to tell a write that happened
	// during its ListDrafts call from one that happened before it.
	seq     uint64
	touched map[draftDest]uint64
}

func newDraftEngine(api draftAPI, teamID string) *draftEngine {
	return &draftEngine{
		api: api, teamID: teamID, now: time.Now,
		index:   map[draftDest]draftEntry{},
		locks:   map[draftDest]chan struct{}{},
		busy:    map[draftDest]int{},
		touched: map[draftDest]uint64{},
	}
}

// lock takes dest's lock and returns its release, or ctx.Err() if ctx is
// done before the slot frees up. The caller must not call unlock when
// err is non-nil.
func (e *draftEngine) lock(ctx context.Context, dest draftDest) (unlock func(), err error) {
	e.mu.Lock()
	ch, ok := e.locks[dest]
	if !ok {
		ch = make(chan struct{}, 1)
		e.locks[dest] = ch
	}
	e.busy[dest]++
	e.mu.Unlock()

	release := func() {
		e.mu.Lock()
		if e.busy[dest]--; e.busy[dest] == 0 {
			delete(e.busy, dest)
		}
		e.mu.Unlock()
	}
	select {
	case ch <- struct{}{}:
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
	return func() {
		<-ch
		release()
	}, nil
}

func (e *draftEngine) entry(dest draftDest) (draftEntry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ent, ok := e.index[dest]
	return ent, ok
}

func (e *draftEngine) put(dest draftDest, d slackclient.Draft) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.index[dest] = draftEntry{id: d.ID, lastUpdatedTS: d.LastUpdatedTS, handsOff: !d.Writable()}
	e.seq++
	e.touched[dest] = e.seq
}

func (e *draftEngine) drop(dest draftDest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.index, dest)
	e.seq++
	e.touched[dest] = e.seq
}

// dropIfMatches removes dest's entry only if it still names id: between
// Delete locating dest and taking its lock, a concurrent Save may have
// replaced dest's draft with a different id, which Delete must then
// leave alone.
func (e *draftEngine) dropIfMatches(dest draftDest, id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cur, ok := e.index[dest]; ok && cur.id == id {
		delete(e.index, dest)
		e.seq++
		e.touched[dest] = e.seq
	}
}

// markHandsOff records dest as a slot slk won't write to (a deleted
// draft still holding it, or an update Slack refused as invalid)
// without forgetting that fact, so the next Save short-circuits instead
// of repeating the failed round trip. A later list that shows dest
// writable (or gone) replaces this entry, same as any other write.
func (e *draftEngine) markHandsOff(dest draftDest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.index[dest] = draftEntry{handsOff: true}
	e.seq++
	e.touched[dest] = e.seq
}

func destOf(d slackclient.Draft) draftDest {
	return draftDest{d.Destinations[0].ChannelID, d.Destinations[0].ThreadTS}
}

// reload lists the drafts and rebuilds the index. A conversation that is
// either busy right now or was written to while the ListDrafts call was
// in flight keeps what the engine already knew instead of the list's
// answer, because that write may postdate (or itself be invisible to)
// the snapshot the list just took. own is the caller's own conversation,
// exempted from that rule: the caller holds its lock and is the one
// making this list call, so the fresh answer is what it is adopting.
func (e *draftEngine) reload(ctx context.Context, own *draftDest) ([]slackclient.Draft, error) {
	e.mu.Lock()
	start := e.seq
	e.mu.Unlock()

	drafts, err := e.api.ListDrafts(ctx)
	if err != nil {
		return nil, err
	}
	next := map[draftDest]draftEntry{}
	for _, d := range drafts {
		if !d.Attached() || d.IsDeleted || d.IsSent {
			continue
		}
		next[destOf(d)] = draftEntry{id: d.ID, lastUpdatedTS: d.LastUpdatedTS, handsOff: !d.Writable()}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	stale := map[draftDest]bool{}
	for dest := range e.busy {
		stale[dest] = true
	}
	for dest, t := range e.touched {
		if t > start {
			stale[dest] = true
		}
	}
	for dest := range stale {
		if own != nil && dest == *own {
			continue
		}
		if old, ok := e.index[dest]; ok {
			next[dest] = old
		} else {
			delete(next, dest)
		}
	}
	e.index = next
	return drafts, nil
}

// Load lists the workspace's active drafts for the UI.
func (e *draftEngine) Load(ctx context.Context) ([]core.Draft, error) {
	drafts, err := e.reload(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]core.Draft, 0, len(drafts))
	for _, d := range drafts {
		if d.IsDeleted || d.IsSent {
			continue
		}
		out = append(out, coreDraft(e.teamID, d))
	}
	return out, nil
}

// Save makes text dest's attached draft. slk's text wins every conflict
// (spec, Conflicts). current, checked right after the per-conversation
// lock is acquired, is a last-moment "is this save still wanted?"
// check: a save built before a send or delete for the same
// conversation can take the lock only after that send/delete has
// completed and forgotten the draft, and without this check it would
// then create a new draft holding the already-sent text. current nil
// means always current.
func (e *draftEngine) Save(ctx context.Context, dest draftDest, text string, edited time.Time, current func() bool) (core.DraftSaveResult, error) {
	unlock, err := e.lock(ctx, dest)
	if err != nil {
		return core.DraftSaveResult{}, err
	}
	defer unlock()
	if current != nil && !current() {
		return core.DraftSaveResult{}, core.ErrDraftStale
	}
	w := slackclient.DraftWrite{ChannelID: dest.channelID, ThreadTS: dest.threadTS, Text: text, Edited: edited}
	ent, ok := e.entry(dest)
	switch {
	case ok && ent.handsOff:
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	case ok:
		return e.update(ctx, dest, ent.id, w)
	}
	return e.create(ctx, dest, w, true)
}

// create makes dest's draft. adopt allows the one recovery from
// attached_draft_exists: another device made the draft, so list and
// overwrite it. Without adopt that answer means a slot slk cannot use.
func (e *draftEngine) create(ctx context.Context, dest draftDest, w slackclient.DraftWrite, adopt bool) (core.DraftSaveResult, error) {
	d, err := e.api.CreateDraft(ctx, w)
	if err == nil {
		e.put(dest, d)
		return core.DraftSaveResult{Draft: coreDraft(e.teamID, d)}, nil
	}
	if slackclient.DraftRejectionCode(err) != "attached_draft_exists" {
		return core.DraftSaveResult{}, err
	}
	if !adopt {
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	}
	if _, err := e.reload(ctx, &dest); err != nil {
		return core.DraftSaveResult{}, err
	}
	ent, ok := e.entry(dest)
	if !ok || ent.handsOff {
		// A deleted draft can hold the slot without being listed.
		// Remember that so the next Save doesn't repeat this round trip.
		e.markHandsOff(dest)
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	}
	res, err := e.update(ctx, dest, ent.id, w)
	if err == nil {
		res.Replaced = true
	}
	return res, err
}

func (e *draftEngine) update(ctx context.Context, dest draftDest, id string, w slackclient.DraftWrite) (core.DraftSaveResult, error) {
	d, err := e.api.UpdateDraft(ctx, id, w)
	replaced := false
	if slackclient.DraftRejectionCode(err) == "draft_has_conflict" {
		// Another device edited it later than this text: slk's wins.
		w.Edited = e.now()
		d, err = e.api.UpdateDraft(ctx, id, w)
		replaced = true
	}
	switch slackclient.DraftRejectionCode(err) {
	case "":
		if err != nil {
			return core.DraftSaveResult{}, err
		}
	case "draft_not_found", "draft_already_deleted", "draft_already_sent":
		e.drop(dest)
		return e.create(ctx, dest, w, false)
	case "draft_update_invalid":
		// Remembered hands-off, not dropped: the next Save short-circuits
		// instead of repeating this refused update.
		e.markHandsOff(dest)
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	default:
		return core.DraftSaveResult{}, err
	}
	e.put(dest, d)
	return core.DraftSaveResult{Draft: coreDraft(e.teamID, d), Replaced: replaced}, nil
}

// draftGoneCodes are the delete refusals that mean the draft no longer
// exists (deleted elsewhere, sent, or never there): Discard treats them
// like success and forgets the entry. Any other refusal (internal_error,
// invalid_auth, …) does not mean the draft is gone, so Discard keeps the
// entry and returns the error for the caller to retry.
var draftGoneCodes = map[string]bool{
	"draft_not_found":       true,
	"draft_already_deleted": true,
	"draft_already_sent":    true,
	"draft_delete_invalid":  true,
}

// Discard deletes dest's draft if the engine knows a writable one. A
// gone-code refusal (draftGoneCodes) means it is gone already; any
// other error, refusal or transport, is returned so the caller retries.
func (e *draftEngine) Discard(ctx context.Context, dest draftDest) error {
	unlock, err := e.lock(ctx, dest)
	if err != nil {
		return err
	}
	defer unlock()
	ent, ok := e.entry(dest)
	if !ok || ent.handsOff {
		return nil
	}
	if err := e.api.DeleteDraft(ctx, ent.id); err != nil && !draftGoneCodes[slackclient.DraftRejectionCode(err)] {
		return err
	}
	e.drop(dest)
	return nil
}

// Delete deletes one draft by id, any kind (the Drafts list).
func (e *draftEngine) Delete(ctx context.Context, id string) error {
	var dest *draftDest
	e.mu.Lock()
	for d, ent := range e.index {
		if ent.id == id {
			d := d
			dest = &d
			break
		}
	}
	e.mu.Unlock()
	if dest != nil {
		unlock, err := e.lock(ctx, *dest)
		if err != nil {
			return err
		}
		defer unlock()
	}
	if err := e.api.DeleteDraft(ctx, id); err != nil {
		return err
	}
	if dest != nil {
		e.dropIfMatches(*dest, id)
	}
	return nil
}

// beginSend takes dest's lock, waiting out a save in flight up to ctx,
// and returns its writable draft (nil when none). done releases the
// lock; done(true) after the post succeeded with the ref forgets the
// draft. On a ctx error the lock was never taken: done is a no-op and
// must still be called.
func (e *draftEngine) beginSend(ctx context.Context, dest draftDest) (*slackclient.DraftRef, func(sent bool), error) {
	unlock, err := e.lock(ctx, dest)
	if err != nil {
		return nil, func(bool) {}, err
	}
	var ref *slackclient.DraftRef
	if ent, ok := e.entry(dest); ok && !ent.handsOff {
		ref = &slackclient.DraftRef{ID: ent.id, LastUpdatedTS: ent.lastUpdatedTS}
	}
	return ref, func(sent bool) {
		if sent && ref != nil {
			e.drop(dest)
		}
		unlock()
	}, nil
}

// draftSender is the part of *slackclient.Client sendWithDraft uses.
type draftSender interface {
	SendMessage(ctx context.Context, channelID, text string) (string, string, error)
	SendReply(ctx context.Context, channelID, threadTS, text string, broadcast bool) (string, string, error)
	SendDraft(ctx context.Context, channelID, threadTS, text string, broadcast bool, ref slackclient.DraftRef) (string, string, error)
}

// sendWithDraft posts text to the conversation (a thread when threadTS is
// set), marking its draft sent with the post. When Slack refuses the
// draft fields (draft_*: nothing was posted) it posts plainly and deletes
// the draft. drafts nil (sync off) is a plain send. A lock that cannot
// be taken within ctx (a save elsewhere is in flight) never blocks the
// send: it posts plainly instead of failing the send over a lock.
func sendWithDraft(ctx context.Context, client draftSender, drafts *draftEngine, channelID, threadTS, text string, broadcast bool) (string, string, error) {
	plain := func() (string, string, error) {
		if threadTS == "" {
			return client.SendMessage(ctx, channelID, text)
		}
		return client.SendReply(ctx, channelID, threadTS, text, broadcast)
	}
	if drafts == nil {
		return plain()
	}
	dest := draftDest{channelID, threadTS}
	ref, done, err := drafts.beginSend(ctx, dest)
	if err != nil {
		return plain()
	}
	if ref == nil {
		ts, mr, err := plain()
		done(false)
		return ts, mr, err
	}
	ts, mr, err := client.SendDraft(ctx, channelID, threadTS, text, broadcast, *ref)
	var refusal slack.SlackErrorResponse
	if errors.As(err, &refusal) && strings.HasPrefix(refusal.Err, "draft_") {
		debuglog.General("chat.postMessage: draft refused (%s), sending without it", refusal.Err)
		ts, mr, err = plain()
		if err == nil {
			// Best-effort: a failed delete here just means the next
			// Save/Load self-heals through the attached_draft_exists /
			// draft_already_sent recovery paths already in place.
			_ = drafts.api.DeleteDraft(ctx, ref.ID)
			drafts.drop(dest)
		}
		done(false)
		return ts, mr, err
	}
	done(err == nil)
	return ts, mr, err
}

// draftTimeout bounds one draft call from the UI's own tick. Quit uses
// its own, much shorter deadline (shutdown.go) for the flush of unsynced
// drafts; the per-conversation lock (lock) honours whichever ctx it is
// given, so a save still running under draftTimeout at quit time never
// makes the flush wait the full 15s -- it gives up at the quit deadline
// instead.
const draftTimeout = 15 * time.Second

// draftService is the App's DraftService: each call goes to the engine
// of the workspace it names, which need not be the active one.
func draftService(router *workspaceRouter) core.DraftService {
	engine := func(teamID string) (*draftEngine, error) {
		if w := router.ByID(teamID); w != nil && w.Drafts != nil {
			return w.Drafts, nil
		}
		return nil, fmt.Errorf("drafts: workspace %q is unavailable", teamID)
	}
	call := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), draftTimeout)
	}
	return core.NewDraftService(core.DraftServiceFuncs{
		Load: func(teamID string) ([]core.Draft, error) {
			e, err := engine(teamID)
			if err != nil {
				return nil, err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Load(ctx)
		},
		Save: func(k core.DraftKey, text string, edited time.Time, current func() bool) (core.DraftSaveResult, error) {
			e, err := engine(k.TeamID)
			if err != nil {
				return core.DraftSaveResult{}, err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Save(ctx, draftDest{k.ChannelID, k.ThreadTS}, text, edited, current)
		},
		Discard: func(k core.DraftKey) error {
			e, err := engine(k.TeamID)
			if err != nil {
				return err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Discard(ctx, draftDest{k.ChannelID, k.ThreadTS})
		},
		Delete: func(teamID, id string) error {
			e, err := engine(teamID)
			if err != nil {
				return err
			}
			ctx, cancel := call()
			defer cancel()
			return e.Delete(ctx, id)
		},
	})
}

// flushDrafts saves the drafts the App had not synced yet, on quit,
// within ctx. Failures are logged; the text stays in memory only.
func flushDrafts(ctx context.Context, router *workspaceRouter, changes []core.DraftChange) {
	for _, c := range changes {
		w := router.ByID(c.Key.TeamID)
		if w == nil || w.Drafts == nil {
			continue
		}
		dest := draftDest{c.Key.ChannelID, c.Key.ThreadTS}
		var err error
		if c.Text == "" {
			err = w.Drafts.Discard(ctx, dest)
		} else {
			_, err = w.Drafts.Save(ctx, dest, c.Text, c.Edited, nil)
		}
		if err != nil {
			debuglog.General("drafts: quit flush %v: %v", c.Key, err)
		}
	}
}

// coreDraft is d as the UI sees it. Text is the composer markdown the
// edit path (E) builds from the same blocks, mentions in wire form.
func coreDraft(teamID string, d slackclient.Draft) core.Draft {
	out := core.Draft{
		ID:        d.ID,
		Key:       core.DraftKey{TeamID: teamID},
		Text:      messages.EditSource(messages.MessageItem{Blocks: blockkit.Parse(d.Blocks)}),
		FileCount: len(d.FileIDs),
		Saved:     d.IsFromComposer,
	}
	if len(d.Destinations) == 1 {
		out.Key.ChannelID = d.Destinations[0].ChannelID
		out.Key.ThreadTS = d.Destinations[0].ThreadTS
	}
	if t, ok := export.TimeFromTS(d.LastUpdatedTS); ok {
		out.Updated = t
	}
	if d.DateScheduled != 0 {
		out.Scheduled = time.Unix(d.DateScheduled, 0)
	}
	return out
}
