package main

import (
	"crypto/sha1"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	slackclient "github.com/gammons/slk/internal/slack"
)

// gridEventDedupe drops WebSocket frames a sibling session of the same
// Enterprise Grid has already delivered, so a user with the org
// workspace and one or more member workspaces connected processes each
// grid event once instead of once per socket.
//
// The grid fans every event out to each of the user's connected
// sessions - verified against a live grid, where an org-level socket
// and a member-workspace socket received 48 of the same 50 events in a
// 45-second window, most byte-identical. Byte identity is the dedup
// key, deliberately: a frame that differs in any byte (Slack stamps
// some events with a per-delivery event_ts) is treated as a distinct
// event and processed, which costs only the redundant work slk did
// before this existed. Nothing distinct is ever dropped.
//
// Two classes of frame are never deduped:
//
//   - Per-connection frames (hello, reconnect_url), frames that fail
//     to parse as events, and user_typing, which repeats
//     byte-identically while someone keeps typing - deduping it would
//     make the typing indicator flicker off between refreshes.
//   - Frames whose handlers write per-workspace state (see
//     gridPerWorkspaceFrames). Each connected session has its own
//     WorkspaceContext - its own mute store, DND, presence, section
//     store, conversation list and thread subscriptions - so whichever
//     socket's copy survived would leave its siblings stale. These run
//     on every socket, exactly as they did before the dedupe existed.
//
// Entries remember the socket that recorded them, so a byte-identical
// repeat on the SAME socket always passes: a state that flips and
// flips back inside the window (presence away-active-away) must land
// on its final value.
type gridEventDedupe struct {
	mu   sync.Mutex
	seen map[string]gridDedupEntry // sha1(frame) -> who recorded it, until when
	ttl  time.Duration
	now  func() time.Time

	// socketSeq mints the per-gate socket identity; see gridFrameGate.
	// Atomic: the connect goroutines of several workspaces run in
	// parallel, and two sessions of one grid drawing the same id would
	// make every sibling copy count as a same-socket repeat.
	socketSeq atomic.Uint64
}

type gridDedupEntry struct {
	socket  uint64
	expires time.Time
}

func newGridEventDedupe(ttl time.Duration) *gridEventDedupe {
	return &gridEventDedupe{
		seen: map[string]gridDedupEntry{},
		ttl:  ttl,
		now:  time.Now,
	}
}

// gridConnectionExempt lists frame types that are never deduped because
// they belong to the connection that received them, carry no event at
// all, or repeat byte-identically as part of their normal cadence. The
// empty type covers frames that failed to parse as an event.
// presence_change is here for a different reason: it is a cheap
// set-value on shared state, and deduping it buys a narrow staleness -
// a socket that disconnects mid-TTL can leave an entry that swallows a
// sibling's byte-identical away-active-away round trip, stranding the
// presence on the wrong value on an inactive workspace.
var gridConnectionExempt = map[string]bool{
	"":                true,
	"hello":           true,
	"reconnect_url":   true,
	"user_typing":     true,
	"presence_change": true,
}

// gridPerWorkspaceFrames lists frame types whose handlers write
// per-workspace state. These must run on every connected session of
// the grid: each session's WorkspaceContext keeps its own copy of that
// state, and deduping the frame would update only whichever socket won
// the delivery race - leaving the others stale until the next boot,
// with a stale DND flag silently unsilencing notifications. The set is
// a denylist rather than an allowlist on purpose: a frame type not
// listed here is at worst processed redundantly, the pre-dedupe
// behaviour, while a type wrongly listed as shared would silently go
// stale.
var gridPerWorkspaceFrames = map[string]bool{
	// Mute store (wsCtx.MuteStore via OnPrefChange).
	"pref_change": true,
	// Own DND (wsCtx.DNDEnabled/DNDEndTS via OnDNDChange).
	"dnd_updated": true, "dnd_updated_user": true,
	// Own presence (wsCtx.Presence via OnSelfPresenceChange).
	"manual_presence_change": true,
	// Sidebar sections (wsCtx.SectionStore via OnChannelSection*).
	"channel_section_upserted":           true,
	"channel_section_deleted":            true,
	"channel_sections_channels_upserted": true,
	"channel_sections_channels_removed":  true,
	// Conversation lists (wsCtx.Channels / finder via
	// OnConversationOpened). The dispatcher maps all five types to it.
	"mpim_open": true, "im_created": true, "im_open": true,
	"group_joined": true, "channel_joined": true,
	// Thread subscriptions: UpsertThreadSubscription keys on
	// workspace_id, so each session must persist its own row - and an
	// unsubscribe must tombstone it on every session.
	"thread_subscribed": true, "thread_unsubscribed": true,
	// Thread read position: UpdateThreadLastRead* keys on
	// (workspace_id, channel, thread).
	"thread_marked": true,
	// Channel membership cache (wsCtx.Membership).
	"member_joined_channel": true, "member_left_channel": true,
	// Peer-status refresh queues (wsCtx.PeerStatus): a per-workspace
	// set insert that schedules that workspace's refetch, so every
	// session must see the invalidation.
	"user_invalidated": true, "dnd_invalidated": true,
}

// allow records the frame for socket and reports whether this is its
// first delivery by a DIFFERENT socket inside the TTL window.
func (d *gridEventDedupe) allow(frame []byte, socket uint64) bool {
	var evt struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(frame, &evt); err != nil ||
		gridConnectionExempt[evt.Type] || gridPerWorkspaceFrames[evt.Type] {
		return true
	}
	sum := sha1.Sum(frame)
	key := string(sum[:])

	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.seen[key]; ok && now.Before(e.expires) && e.socket != socket {
		return false
	}
	// Sweep before the map can grow without bound on a busy grid; at
	// the observed ~10 frames/s a 5s TTL holds ~50 live entries, so
	// hitting the cap means the clock jumped or the TTL was raised.
	if len(d.seen) >= 4096 {
		for k, e := range d.seen {
			if !now.Before(e.expires) {
				delete(d.seen, k)
			}
		}
	}
	d.seen[key] = gridDedupEntry{socket: socket, expires: now.Add(d.ttl)}
	return true
}

// gridFrameGate builds the per-socket predicate handed to
// (*slackclient.Client).SetWSEventGate. Each gate gets its own socket
// identity, so a repeat on the same socket passes while a sibling's
// copy drops. The frame is dropped only when a sibling already
// delivered it AND this socket's workspace is not the one the user is
// looking at: the active workspace's handler is the only one that
// renders messages, so its delivery has to pass unconditionally or
// live messages would stop rendering the moment a sibling socket won
// the race. It still records the frame, so a sibling arriving after
// the active socket drops rather than redoing the durable writes.
//
// onDroppedMessage, when non-nil, runs for a MESSAGE frame this gate
// drops: message-driven conversation discovery must still happen on
// this session (a group DM created mid-session arrives only as
// messages, so the session that lost the delivery race would otherwise
// never learn it). The gate runs on the client's serialized
// event-owner goroutine, so the hook is ordered with every other
// handler callback.
func gridFrameGate(dedupe *gridEventDedupe, isActive func() bool, onDroppedMessage func(channelID string)) func([]byte) bool {
	socket := dedupe.socketSeq.Add(1)
	return func(frame []byte) bool {
		first := dedupe.allow(frame, socket)
		if isActive != nil && isActive() {
			return true
		}
		if !first && onDroppedMessage != nil {
			var evt struct {
				Type        string `json:"type"`
				Channel     string `json:"channel"`
				SubType     string `json:"subtype"`
				IsEphemeral bool   `json:"is_ephemeral"`
				Message     *struct {
					IsEphemeral bool `json:"is_ephemeral"`
				} `json:"message"`
			}
			if json.Unmarshal(frame, &evt) == nil && evt.Type == "message" && evt.Channel != "" &&
				slackclient.MessageFrameDrivesOnMessage(evt.SubType, evt.IsEphemeral,
					evt.Message != nil, evt.Message != nil && evt.Message.IsEphemeral) {
				onDroppedMessage(evt.Channel)
			}
		}
		return first
	}
}

// gridEventGates holds one dedupe per Enterprise Grid, shared by every
// session of that grid in the process. Keyed by enterprise id because
// two unrelated grids' events never share bytes, while two sessions of
// one grid deliver identical bytes constantly.
type gridEventGates struct {
	mu    sync.Mutex
	gates map[string]*gridEventDedupe
	ttl   time.Duration
}

func newGridEventGates(ttl time.Duration) *gridEventGates {
	return &gridEventGates{
		gates: map[string]*gridEventDedupe{},
		ttl:   ttl,
	}
}

// forGrid returns the shared dedupe for one enterprise id.
func (g *gridEventGates) forGrid(enterpriseID string) *gridEventDedupe {
	g.mu.Lock()
	defer g.mu.Unlock()
	if d, ok := g.gates[enterpriseID]; ok {
		return d
	}
	d := newGridEventDedupe(g.ttl)
	g.gates[enterpriseID] = d
	return d
}
