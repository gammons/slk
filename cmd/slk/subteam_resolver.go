package main

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/slack-go/slack"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/messages"
)

var (
	bareSubteamRe   = regexp.MustCompile(`<!subteam\^(S[A-Z0-9]+)>`)
	anySubteamRe    = regexp.MustCompile(`subteam\^(S[A-Z0-9]+)`)
	usergroupElemRe = regexp.MustCompile(`"usergroup_id":"(S[A-Z0-9]+)"`)
)

// subteamIDsInText returns the IDs of the bare <!subteam^S…> mentions in
// a message's mrkdwn text. Labeled tokens carry their own handle.
func subteamIDsInText(text string) []string {
	var ids []string
	for _, m := range bareSubteamRe.FindAllStringSubmatch(text, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// subteamIDsInItems returns the distinct usergroup IDs the items would
// render as bare mentions: from the text, and from the rich_text
// usergroup elements and legacy attachments, which the renderer turns
// into bare <!subteam^S…> tokens too.
func subteamIDsInItems(items []messages.MessageItem) []string {
	var ids []string
	for _, it := range items {
		ids = append(ids, subteamIDsInMessage(it.Text, it.Blocks, it.LegacyAttachments)...)
	}
	return dedupeIDs(ids)
}

// subteamIDsInMessage is subteamIDsInItems for one message's text and
// structured parts (blocks, attachments, in any JSON-encodable shape).
func subteamIDsInMessage(text string, structured ...any) []string {
	ids := subteamIDsInText(text)
	for _, part := range structured {
		raw, err := json.Marshal(part)
		if err != nil || len(raw) <= 4 {
			continue
		}
		for _, re := range []*regexp.Regexp{anySubteamRe, usergroupElemRe} {
			for _, m := range re.FindAllSubmatch(raw, -1) {
				ids = append(ids, string(m[1]))
			}
		}
	}
	return dedupeIDs(ids)
}

func dedupeIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

const subteamLookupTimeout = 10 * time.Second

// subteamResolver fills in the handles of usergroups that usergroups.list
// does not return: on an Enterprise Grid org-level session that call
// lists only the org's own groups, so a mention of a group owned by a
// member workspace rendered as "@group". Unknown IDs seen in messages are
// looked up one at a time through subteams.info in a single background
// worker, merged into the workspace's map, and published.
//
// An ID stays pending from Request until its handle is merged (or its
// lookup has failed), so it is never fetched twice at once. A 429
// pauses the worker for Slack's Retry-After, then retries the same ID;
// the IDs behind it stay queued.
type subteamResolver struct {
	ctx     context.Context
	lookup  func(context.Context, string) (slackclient.UserGroupInfo, error)
	known   func() map[string]string
	merge   func(map[string]string) map[string]string
	publish func(map[string]string)
	logf    func(string, ...any)
	// wait sleeps out a rate-limit error on ctx; slackclient's
	// WaitOutRateLimit, swapped in tests.
	wait func(context.Context, error) (bool, error)

	mu      sync.Mutex
	queue   []string
	pending map[string]bool
	missing map[string]bool
	running bool
}

func newSubteamResolver(ctx context.Context, lookup func(context.Context, string) (slackclient.UserGroupInfo, error), known func() map[string]string, merge func(map[string]string) map[string]string, publish func(map[string]string), logf func(string, ...any)) *subteamResolver {
	return &subteamResolver{
		ctx:     ctx,
		lookup:  lookup,
		known:   known,
		merge:   merge,
		publish: publish,
		logf:    logf,
		wait:    slackclient.WaitOutRateLimit,
		pending: map[string]bool{},
		missing: map[string]bool{},
	}
}

// Request queues the IDs not already resolved, queued, or known to be
// missing. Safe from any goroutine; never blocks on the network.
func (r *subteamResolver) Request(ids []string) {
	if r == nil || len(ids) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Read the map under r.mu: the worker merges before it clears
	// pending, so an ID is always in one or the other here.
	known := r.known()
	for _, id := range ids {
		if known[id] != "" || r.pending[id] || r.missing[id] {
			continue
		}
		r.pending[id] = true
		r.queue = append(r.queue, id)
	}
	if len(r.queue) > 0 && !r.running {
		r.running = true
		go r.run()
	}
}

// RequestItems queues the usergroups the items mention.
func (r *subteamResolver) RequestItems(items []messages.MessageItem) {
	if r == nil {
		return
	}
	r.Request(subteamIDsInItems(items))
}

func (r *subteamResolver) run() {
	for {
		r.mu.Lock()
		batch := slices.Clone(r.queue)
		r.queue = r.queue[:0]
		if len(batch) == 0 {
			r.running = false
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		if !r.resolveBatch(batch) {
			return
		}
	}
}

// resolveBatch looks the batch's IDs up in order. Found handles are
// merged and published before their IDs leave pending: at the end of
// the batch, and before sleeping out a 429 so they aren't held back for
// the cooldown. It returns false, with the worker stopped and nothing
// left pending, when the cooldown is cut short by the context.
func (r *subteamResolver) resolveBatch(batch []string) bool {
	found := map[string]string{}
	var done []string
	flush := func() {
		if len(found) > 0 {
			r.logf("resolved %d usergroups via subteams.info", len(found))
			r.publish(r.merge(found))
			found = map[string]string{}
		}
		r.mu.Lock()
		for _, id := range done {
			delete(r.pending, id)
		}
		r.mu.Unlock()
		done = done[:0]
	}

	for i := 0; i < len(batch); {
		id := batch[i]
		if r.known()[id] != "" {
			// usergroups.list (or an earlier batch) got there first.
			done = append(done, id)
			i++
			continue
		}
		handle, missing, limited := r.resolve(id)
		if limited != nil {
			flush()
			if _, err := r.wait(r.ctx, limited); err != nil {
				r.stop(batch[i:])
				return false
			}
			continue // retry the same ID
		}
		if missing {
			r.mu.Lock()
			r.missing[id] = true
			r.mu.Unlock()
		}
		if handle != "" {
			found[id] = handle
		}
		done = append(done, id)
		i++
	}
	flush()
	return true
}

// stop ends the worker after a cancelled cooldown, dropping the
// unfinished IDs and everything queued so a later Request can retry
// them.
func (r *subteamResolver) stop(unfinished []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range unfinished {
		delete(r.pending, id)
	}
	for _, id := range r.queue {
		delete(r.pending, id)
	}
	r.queue = r.queue[:0]
	r.running = false
}

// resolve looks one ID up. missing reports a definitive "no such group",
// which is remembered. limited is a 429 to wait out before retrying the
// ID. Any other failure is logged and left retryable by a later Request.
func (r *subteamResolver) resolve(id string) (handle string, missing bool, limited error) {
	if r.ctx.Err() != nil {
		return "", false, nil
	}
	ctx, cancel := context.WithTimeout(r.ctx, subteamLookupTimeout)
	defer cancel()
	g, err := r.lookup(ctx, id)
	if errors.Is(err, slackclient.ErrUserGroupNotFound) {
		return "", true, nil
	}
	var rl *slack.RateLimitedError
	if errors.As(err, &rl) {
		r.logf("subteams.info %s rate limited, retrying after %s", id, rl.RetryAfter)
		return "", false, err
	}
	if err != nil {
		r.logf("subteams.info %s failed: %v", id, err)
		return "", false, nil
	}
	handle = g.Handle
	if handle == "" {
		handle = slugifyHandle(g.Name)
	}
	if handle == "" {
		return "", true, nil
	}
	return handle, false, nil
}
