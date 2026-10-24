package main

import (
	"testing"
	"time"
)

// twoGates builds one dedupe with two per-socket gates, the org and a
// member workspace of the same grid.
func twoGates(t *testing.T) (org, member func([]byte) bool, d *gridEventDedupe) {
	t.Helper()
	d = newGridEventDedupe(5 * time.Second)
	return gridFrameGate(d, func() bool { return false }, nil),
		gridFrameGate(d, func() bool { return false }, nil), d
}

// TestGridFrameGate_DroppedMessageRunsDiscovery pins the sibling
// discovery hook: when an inactive session's gate drops a message
// frame that a sibling already delivered, the hook still runs for that
// channel - message-driven conversation discovery must happen on every
// session, or a group DM created mid-session never reaches the session
// that lost the delivery race. Non-message frames drop silently, and
// the active gate (which never drops) never runs the hook.
func TestGridFrameGate_DroppedMessageRunsDiscovery(t *testing.T) {
	d := newGridEventDedupe(5 * time.Second)
	var discovered []string
	inactive := gridFrameGate(d, func() bool { return false }, func(channelID string) {
		discovered = append(discovered, channelID)
	})
	active := gridFrameGate(d, func() bool { return true }, func(channelID string) {
		discovered = append(discovered, "active:"+channelID)
	})

	msg := []byte(`{"type":"message","channel":"C1","ts":"1.001"}`)
	if !active(msg) {
		t.Fatal("active gate must pass its own delivery")
	}
	if len(discovered) != 0 {
		t.Fatalf("active gate ran the hook: %v", discovered)
	}
	if inactive(msg) {
		t.Fatal("inactive gate must drop the sibling's copy")
	}
	if len(discovered) != 1 || discovered[0] != "C1" {
		t.Fatalf("dropped message must run discovery for its channel: %v", discovered)
	}

	// Frames that drop without reaching OnMessage must not run the
	// hook: message_deleted, an ephemeral, and a plain non-message
	// frame.
	other := gridFrameGate(d, func() bool { return false }, func(channelID string) {
		discovered = append(discovered, channelID)
	})
	del := []byte(`{"type":"message","subtype":"message_deleted","channel":"C1","deleted_ts":"1.000"}`)
	if !other(del) || inactive(del) {
		t.Fatal("setup: message_deleted deliveries should pass and drop")
	}
	eph := []byte(`{"type":"message","channel":"C1","is_ephemeral":true}`)
	if !other(eph) || inactive(eph) {
		t.Fatal("setup: ephemeral deliveries should pass and drop")
	}
	shared := []byte(`{"type":"user_change","user":{"id":"U1"}}`)
	if !other(shared) || inactive(shared) {
		t.Fatal("setup: user_change deliveries should pass and drop")
	}
	if len(discovered) != 1 {
		t.Fatalf("dropped frames that never reach OnMessage must not run discovery: %v", discovered)
	}

	// message_changed (non-ephemeral) DOES reach OnMessage, so its
	// dropped copy runs the hook.
	chg := []byte(`{"type":"message","subtype":"message_changed","channel":"C1","message":{"ts":"1.000"}}`)
	if !other(chg) || inactive(chg) {
		t.Fatal("setup: message_changed deliveries should pass and drop")
	}
	if len(discovered) != 2 || discovered[1] != "C1" {
		t.Fatalf("a dropped message_changed must still run discovery: %v", discovered)
	}
}

func TestGridEventDedupe_SiblingCopyDrops(t *testing.T) {
	org, member, _ := twoGates(t)
	// user_change writes shared, user-keyed cache rows, so it is the
	// dedupe's bread and butter.
	frame := []byte(`{"type":"user_change","user":{"id":"U1","name":"alice"}}`)
	if !org(frame) {
		t.Fatal("first delivery must be allowed")
	}
	if member(frame) {
		t.Error("a sibling's byte-identical copy inside the TTL must be dropped")
	}
}

func TestGridEventDedupe_FrameAfterTTLIsNew(t *testing.T) {
	now := time.Unix(1790000000, 0)
	d := newGridEventDedupe(5 * time.Second)
	d.now = func() time.Time { return now }
	org := gridFrameGate(d, func() bool { return false }, nil)
	member := gridFrameGate(d, func() bool { return false }, nil)
	frame := []byte(`{"type":"user_change","user":{"id":"U1","name":"alice"}}`)
	if !org(frame) || member(frame) {
		t.Fatal("setup: first delivery passes, sibling copy drops")
	}
	now = now.Add(6 * time.Second)
	if !member(frame) {
		t.Error("the same bytes after the TTL expired must be treated as a new event")
	}
}

func TestGridEventDedupe_SameSocketRepeatPasses(t *testing.T) {
	// A state that flips and flips back inside the window (presence
	// away-active-away) has its third frame byte-identical to the
	// first. The socket that recorded the entry must not have its own
	// repeat dropped, or that workspace ends on the wrong value.
	org, _, _ := twoGates(t)
	frame := []byte(`{"type":"user_change","user":{"id":"U1","name":"alice"}}`)
	if !org(frame) {
		t.Fatal("first delivery must be allowed")
	}
	if !org(frame) {
		t.Error("a repeat on the same socket must pass")
	}
}

func TestGridEventDedupe_PerWorkspaceFramesPassEverywhere(t *testing.T) {
	// These handlers write per-workspace state (mute store, own DND,
	// own presence, sections, conversation lists, thread
	// subscriptions), so every session of the grid must process its
	// own copy: whichever socket won the race, the others' state must
	// not go stale.
	org, member, _ := twoGates(t)
	for _, typ := range []string{
		"pref_change",
		"dnd_updated",
		"dnd_updated_user",
		"manual_presence_change",
		"channel_section_upserted",
		"mpim_open",
		"im_created",
		"channel_joined",
		"thread_subscribed",
		"thread_unsubscribed",
		"thread_marked",
		"member_joined_channel",
		"user_invalidated",
	} {
		frame := []byte(`{"type":"` + typ + `","payload":"x"}`)
		if !org(frame) {
			t.Errorf("%s: first delivery must pass", typ)
		}
		if !member(frame) {
			t.Errorf("%s: sibling's copy must pass (per-workspace state)", typ)
		}
	}
}

func TestGridEventDedupe_ExemptTypesAlwaysPass(t *testing.T) {
	org, _, _ := twoGates(t)
	for _, frame := range [][]byte{
		[]byte(`{"type":"hello"}`),
		[]byte(`{"type":"reconnect_url","url":"wss://x"}`),
		[]byte(`{"type":"user_typing","channel":"C1","user":"U1"}`),
		[]byte(`{"type":"presence_change","users":["U1"],"presence":"away"}`),
		[]byte(`not json at all`),
	} {
		if !org(frame) {
			t.Errorf("exempt frame %s must be allowed on first delivery", frame)
		}
		if !org(frame) {
			t.Errorf("exempt frame %s must be allowed on repeat delivery", frame)
		}
	}
}

func TestGridEventDedupe_DistinctFramesBothPass(t *testing.T) {
	org, member, _ := twoGates(t)
	// Same user, different event_ts: Slack stamps some deliveries
	// differently, and a differing byte is a distinct event.
	a := []byte(`{"type":"user_change","user":{"id":"U1","name":"alice"}}`)
	b := []byte(`{"type":"user_change","user":{"id":"U1","name":"alice2"}}`)
	if !org(a) || !member(b) {
		t.Error("frames that differ in any byte must both be processed")
	}
}

func TestGridFrameGate_ActiveWorkspaceAlwaysPassesAndMarks(t *testing.T) {
	d := newGridEventDedupe(5 * time.Second)
	frame := []byte(`{"type":"message","channel":"C1","ts":"1.001"}`)

	active := gridFrameGate(d, func() bool { return true }, nil)
	inactive := gridFrameGate(d, func() bool { return false }, nil)

	if !active(frame) {
		t.Fatal("the active workspace's delivery must always pass")
	}
	// The active gate still recorded the frame, so the sibling that
	// holds the same event drops it instead of redoing the durable
	// writes the active handler already did.
	if inactive(frame) {
		t.Error("an inactive sibling delivering the same frame after the active workspace must drop it")
	}
}

func TestGridFrameGate_InactiveFirstActiveStillRenders(t *testing.T) {
	// The ordering that would break rendering if the active bypass
	// were missing: an inactive sibling wins the race, then the
	// active workspace's own socket delivers the same frame.
	d := newGridEventDedupe(5 * time.Second)
	frame := []byte(`{"type":"message","channel":"C1","ts":"1.001"}`)

	inactive := gridFrameGate(d, func() bool { return false }, nil)
	active := gridFrameGate(d, func() bool { return true }, nil)

	if !inactive(frame) {
		t.Fatal("first delivery must pass for the inactive sibling too")
	}
	if !active(frame) {
		t.Error("the active workspace must render the frame even though a sibling already delivered it")
	}
}

func TestGridEventGates_SharedPerGrid(t *testing.T) {
	g := newGridEventGates(5 * time.Second)
	a := g.forGrid("E1")
	b := g.forGrid("E1")
	if a != b {
		t.Error("two lookups of the same grid must return the same dedupe")
	}
	if g.forGrid("E2") == a {
		t.Error("a different grid must get its own dedupe")
	}
}
