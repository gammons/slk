// internal/ui/selfsend.go
//
// Self-originated send tracking, used to suppress Slack WebSocket echoes
// of slk-posted messages so the optimistic-instant-display path is the
// sole renderer.
//
// Phase 2b of the SOLID refactor of internal/ui/app.go: extracts the
// trio of dedup primitives that previously lived on App (selfSentTSes,
// lastSelfSendByChannel, localTSCounter) into a self-contained
// information holder.
//
// Two dedup windows cooperate:
//
//  1. InFlight(channelID) — channel-level, time-based: covers the
//     gap between SendMessageMsg dispatch (we call MarkInFlight) and
//     the matching chat.postMessage HTTP response landing. Drops
//     self-user WS echoes that arrive before we know the
//     authoritative TS.
//
//  2. IsSelfSent(ts) — TS-level, exact match: applies after we know
//     the authoritative TS (RecordSent has been called from
//     MessageSentMsg / ThreadReplySentMsg). Drops any late WS echo
//     that carries that TS.
//
// NextLocalTS mints unique "local:<counter>" placeholder IDs for the
// optimistic rows added at send time, before chat.postMessage returns.
package ui

import (
	"fmt"
	"time"
)

// selfSendWindow is the maximum time we expect between a user's slk-
// originated send (SendMessageMsg / EditMessageMsg / etc. dispatch) and
// the matching chat.postMessage HTTP response landing as MessageSentMsg.
// While MarkInFlight has been called within this window for a channel,
// NewMessageMsg suppresses self-user echoes for that channel to avoid
// the visible flicker between WS echo and HTTP response.
const selfSendWindow = 3 * time.Second

// selfSendDedup tracks self-originated sends. See package comment.
type selfSendDedup struct {
	// sentTSes records authoritative TSes the App has just observed
	// via MessageSentMsg / ThreadReplySentMsg. WS echoes carrying any
	// of these are dropped.
	sentTSes map[string]time.Time

	// lastSendByChannel records the wall clock of the last MarkInFlight
	// call per channel; checked against selfSendWindow.
	//
	// Cross-session messages (sent from the official Slack client while
	// slk is open) do NOT update this map and continue to display via
	// the normal WS-echo path.
	lastSendByChannel map[string]time.Time

	// localTSCounter is incremented per optimistic-placeholder message
	// so each one carries a unique TS-shaped ("local:<counter>") id.
	// SendMessageMsg / SendThreadReplyMsg use this to mint a placeholder
	// id; MessageSentMsg / ThreadReplySentMsg uses it to swap the
	// placeholder for the authoritative Slack-assigned TS once the
	// chat.postMessage HTTP response arrives.
	localTSCounter uint64
}

func newSelfSendDedup() *selfSendDedup {
	return &selfSendDedup{
		sentTSes:          make(map[string]time.Time),
		lastSendByChannel: make(map[string]time.Time),
	}
}

// MarkInFlight records that the user just submitted a slk-originated
// send (chat.postMessage / chat.update / thread reply) for channelID.
// While the timestamp is within selfSendWindow, the WS echo for self-
// user messages on this channel is dropped so the optimistic path is
// the sole renderer (and we don't flicker through Slack's normalised
// text).
func (d *selfSendDedup) MarkInFlight(channelID string) {
	if channelID == "" {
		return
	}
	d.lastSendByChannel[channelID] = time.Now()
}

// InFlight reports whether the user submitted an slk-originated send
// for channelID within the last selfSendWindow. Cross-session sends
// (e.g. from the official Slack client) never update the map, so
// their WS echoes are not suppressed.
func (d *selfSendDedup) InFlight(channelID string) bool {
	t, ok := d.lastSendByChannel[channelID]
	if !ok {
		return false
	}
	return time.Since(t) < selfSendWindow
}

// RecordSent marks a message TS as one the user just posted from this
// session, so the WS echo (if any) can be skipped to avoid double-
// rendering. Old entries are GC'd opportunistically; even if they
// leak, they're tiny and only checked when echoes arrive.
func (d *selfSendDedup) RecordSent(ts string) {
	if ts == "" {
		return
	}
	d.sentTSes[ts] = time.Now()
	// Opportunistic cleanup: drop entries older than 5 minutes. WS
	// echoes arrive within seconds; anything older is stale.
	if len(d.sentTSes) > 64 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for k, v := range d.sentTSes {
			if v.Before(cutoff) {
				delete(d.sentTSes, k)
			}
		}
	}
}

// IsSelfSent reports whether ts matches a message we recently posted
// from this session.
func (d *selfSendDedup) IsSelfSent(ts string) bool {
	if ts == "" {
		return false
	}
	_, ok := d.sentTSes[ts]
	return ok
}

// NextLocalTS mints a unique placeholder id for an optimistic instant-
// display message. The "local:" prefix makes it trivially distinguishable
// from a Slack-assigned TS (which is always of the form
// "<seconds>.<microseconds>").
func (d *selfSendDedup) NextLocalTS() string {
	d.localTSCounter++
	return fmt.Sprintf("local:%d", d.localTSCounter)
}
