package main

import (
	"context"
	"errors"
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
	locks map[draftDest]*sync.Mutex
	busy  map[draftDest]int
}

func newDraftEngine(api draftAPI, teamID string) *draftEngine {
	return &draftEngine{
		api: api, teamID: teamID, now: time.Now,
		index: map[draftDest]draftEntry{},
		locks: map[draftDest]*sync.Mutex{},
		busy:  map[draftDest]int{},
	}
}

// lock takes dest's lock and returns its release.
func (e *draftEngine) lock(dest draftDest) func() {
	e.mu.Lock()
	l, ok := e.locks[dest]
	if !ok {
		l = &sync.Mutex{}
		e.locks[dest] = l
	}
	e.busy[dest]++
	e.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		e.mu.Lock()
		if e.busy[dest]--; e.busy[dest] == 0 {
			delete(e.busy, dest)
		}
		e.mu.Unlock()
	}
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
}

func (e *draftEngine) drop(dest draftDest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.index, dest)
}

func destOf(d slackclient.Draft) draftDest {
	return draftDest{d.Destinations[0].ChannelID, d.Destinations[0].ThreadTS}
}

// reload lists the drafts and rebuilds the index. A conversation another
// caller holds the lock of keeps what the engine knew: its call in
// flight may postdate the list. own is the caller's conversation, whose
// entry the list does replace.
func (e *draftEngine) reload(ctx context.Context, own *draftDest) ([]slackclient.Draft, error) {
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
	for dest := range e.busy {
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
// (spec, Conflicts).
func (e *draftEngine) Save(ctx context.Context, dest draftDest, text string, edited time.Time) (core.DraftSaveResult, error) {
	unlock := e.lock(dest)
	defer unlock()
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
		e.drop(dest)
		return core.DraftSaveResult{}, core.ErrDraftHandsOff
	default:
		return core.DraftSaveResult{}, err
	}
	e.put(dest, d)
	return core.DraftSaveResult{Draft: coreDraft(e.teamID, d), Replaced: replaced}, nil
}

// Discard deletes dest's draft if the engine knows a writable one. A
// refusal means it is gone already; a transport error is returned so
// the caller retries.
func (e *draftEngine) Discard(ctx context.Context, dest draftDest) error {
	unlock := e.lock(dest)
	defer unlock()
	ent, ok := e.entry(dest)
	if !ok || ent.handsOff {
		return nil
	}
	if err := e.api.DeleteDraft(ctx, ent.id); err != nil && slackclient.DraftRejectionCode(err) == "" {
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
		unlock := e.lock(*dest)
		defer unlock()
	}
	if err := e.api.DeleteDraft(ctx, id); err != nil {
		return err
	}
	if dest != nil {
		e.drop(*dest)
	}
	return nil
}

// beginSend takes dest's lock, waiting out a save in flight, and returns
// its writable draft (nil when none). done releases the lock; done(true)
// after the post succeeded with the ref forgets the draft.
func (e *draftEngine) beginSend(dest draftDest) (*slackclient.DraftRef, func(sent bool)) {
	unlock := e.lock(dest)
	var ref *slackclient.DraftRef
	if ent, ok := e.entry(dest); ok && !ent.handsOff {
		ref = &slackclient.DraftRef{ID: ent.id, LastUpdatedTS: ent.lastUpdatedTS}
	}
	return ref, func(sent bool) {
		if sent && ref != nil {
			e.drop(dest)
		}
		unlock()
	}
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
// the draft. drafts nil (sync off) is a plain send.
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
	ref, done := drafts.beginSend(dest)
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
			_ = drafts.api.DeleteDraft(ctx, ref.ID)
			drafts.drop(dest)
		}
		done(false)
		return ts, mr, err
	}
	done(err == nil)
	return ts, mr, err
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
